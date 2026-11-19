// Package rtsp reads the H.264 video of an RTSP camera: the camera of a Bambu Lab printer, over
// RTSPS. It is a small client: DESCRIBE with basic or digest login, SETUP of the video track with
// RTP in the RTSP connection (interleaved TCP), and PLAY. It gives each access unit of the video in
// Annex B form (start codes), with the SPS and the PPS of the SDP before the first one.
package rtsp

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrAuth is a login that the camera refused.
var ErrAuth = errors.New("the camera refused the login")

// Conn is one RTSP session.
type Conn struct {
	c       net.Conn
	r       *bufio.Reader
	url     *url.URL // the address with no login
	user    string
	pass    string
	cseq    int
	session string
	auth    func(method, uri string) string // the Authorization header, or nil before a 401
	params  [][]byte                        // the SPS and the PPS of the SDP

	au      []byte // the access unit that the RTP packets build now
	fuStart bool   // a FU-A NAL unit is open in au
	sent    bool   // the parameters went out before the first access unit
}

// Dial connects to the camera at rawURL ("rtsps://user:pass@host:322/streaming/live/1"). TLS
// checks the certificate with check, or accepts any certificate when check is nil: a camera on
// the network has a self-signed certificate.
func Dial(ctx context.Context, rawURL string, check func(*tls.ConnectionState) error) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "554")
		if u.Scheme == "rtsps" {
			host = net.JoinHostPort(u.Hostname(), "322")
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	var nc net.Conn
	if u.Scheme == "rtsps" {
		cfg := &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
		if check != nil {
			cfg.VerifyConnection = func(cs tls.ConnectionState) error { return check(&cs) }
		}
		nc, err = (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", host)
	} else {
		nc, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	c := &Conn{c: nc, r: bufio.NewReaderSize(nc, 256<<10)}
	if u.User != nil {
		c.user = u.User.Username()
		c.pass, _ = u.User.Password()
	}
	clean := *u
	clean.User = nil
	c.url = &clean
	return c, nil
}

// Close closes the connection.
func (c *Conn) Close() error { return c.c.Close() }

// response is an RTSP answer.
type response struct {
	code   int
	header textproto.MIMEHeader
	body   []byte
}

// request sends one request and reads its answer. It logs in again after a 401.
func (c *Conn) request(method, uri string, header map[string]string) (*response, error) {
	for try := 0; try < 2; try++ {
		c.cseq++
		var b strings.Builder
		fmt.Fprintf(&b, "%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: printdeck\r\n", method, uri, c.cseq)
		if c.session != "" {
			fmt.Fprintf(&b, "Session: %s\r\n", c.session)
		}
		if c.auth != nil {
			fmt.Fprintf(&b, "Authorization: %s\r\n", c.auth(method, uri))
		}
		for k, v := range header {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
		b.WriteString("\r\n")
		c.c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := io.WriteString(c.c, b.String()); err != nil {
			return nil, err
		}
		r, err := c.readResponse()
		if err != nil {
			return nil, err
		}
		if r.code == 401 && try == 0 && c.user != "" {
			if err := c.setAuth(r.header.Get("Www-Authenticate")); err != nil {
				return nil, err
			}
			continue
		}
		if r.code == 401 {
			return nil, ErrAuth
		}
		if r.code != 200 {
			return nil, fmt.Errorf("rtsp: %s: %d", method, r.code)
		}
		return r, nil
	}
	return nil, ErrAuth
}

// readResponse reads an answer. Interleaved packets before it are skipped.
func (c *Conn) readResponse() (*response, error) {
	for {
		b, err := c.r.Peek(1)
		if err != nil {
			return nil, err
		}
		if b[0] != '$' {
			break
		}
		if _, _, err := c.readInterleaved(); err != nil {
			return nil, err
		}
	}
	tp := textproto.NewReader(c.r)
	line, err := tp.ReadLine()
	if err != nil {
		return nil, err
	}
	f := strings.Fields(line)
	if len(f) < 2 || !strings.HasPrefix(f[0], "RTSP/") {
		return nil, fmt.Errorf("rtsp: a bad status line %q", line)
	}
	code, err := strconv.Atoi(f[1])
	if err != nil {
		return nil, fmt.Errorf("rtsp: a bad status line %q", line)
	}
	h, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	r := &response{code: code, header: h}
	if n, _ := strconv.Atoi(h.Get("Content-Length")); n > 0 {
		r.body = make([]byte, n)
		if _, err := io.ReadFull(c.r, r.body); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// setAuth makes the Authorization header from the WWW-Authenticate of a 401.
func (c *Conn) setAuth(www string) error {
	switch {
	case strings.HasPrefix(www, "Digest "):
		p := authParams(www[len("Digest "):])
		realm, nonce := p["realm"], p["nonce"]
		ha1 := md5hex(c.user + ":" + realm + ":" + c.pass)
		c.auth = func(method, uri string) string {
			resp := md5hex(ha1 + ":" + nonce + ":" + md5hex(method+":"+uri))
			return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
				c.user, realm, nonce, uri, resp)
		}
	case strings.HasPrefix(www, "Basic"):
		v := "Basic " + base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass))
		c.auth = func(string, string) string { return v }
	default:
		return fmt.Errorf("rtsp: no such login %q", www)
	}
	return nil
}

// authParams reads the key="value" pairs of a WWW-Authenticate header.
func authParams(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			out[strings.ToLower(k)] = strings.Trim(v, `"`)
		}
	}
	return out
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Start runs DESCRIBE, SETUP and PLAY for the video track.
func (c *Conn) Start() error {
	base := c.url.String()
	r, err := c.request("DESCRIBE", base, map[string]string{"Accept": "application/sdp"})
	if err != nil {
		return err
	}
	if cb := r.header.Get("Content-Base"); cb != "" {
		base = cb
	}
	control, params, err := parseSDP(string(r.body))
	if err != nil {
		return err
	}
	c.params = params
	track := control
	if !strings.Contains(control, "://") {
		if control == "" || control == "*" {
			track = base
		} else {
			track = strings.TrimSuffix(base, "/") + "/" + control
		}
	}
	r, err = c.request("SETUP", track, map[string]string{"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1"})
	if err != nil {
		return err
	}
	c.session, _, _ = strings.Cut(r.header.Get("Session"), ";")
	if _, err := c.request("PLAY", base, map[string]string{"Range": "npt=0.000-"}); err != nil {
		return err
	}
	return nil
}

// parseSDP returns the control of the H.264 video track, and its SPS and PPS
// (sprop-parameter-sets).
func parseSDP(sdp string) (control string, params [][]byte, err error) {
	video, found := false, false
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "m="):
			video = strings.HasPrefix(line, "m=video")
			found = found || video
		case video && strings.HasPrefix(line, "a=control:"):
			control = strings.TrimPrefix(line, "a=control:")
		case video && strings.HasPrefix(line, "a=fmtp:"):
			for _, kv := range strings.Split(line, ";") {
				k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
				if !ok || !strings.HasSuffix(k, "sprop-parameter-sets") {
					continue
				}
				for _, s := range strings.Split(v, ",") {
					if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) > 0 {
						params = append(params, b)
					}
				}
			}
		}
	}
	if !found {
		return "", nil, errors.New("rtsp: the camera has no video track")
	}
	return control, params, nil
}

// readInterleaved reads one interleaved packet: its channel and its data.
func (c *Conn) readInterleaved() (int, []byte, error) {
	var h [4]byte
	if _, err := io.ReadFull(c.r, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0] != '$' {
		return 0, nil, errors.New("rtsp: lost the packet frames")
	}
	n := int(binary.BigEndian.Uint16(h[2:]))
	b := make([]byte, n)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return 0, nil, err
	}
	return int(h[1]), b, nil
}

// keepAliveEvery is the time between two requests that keep the session open.
const keepAliveEvery = 20 * time.Second

// Read returns the next access unit of the video in Annex B form. The first one starts with the
// SPS and the PPS of the SDP.
func (c *Conn) Read() ([]byte, error) {
	last := time.Now()
	for {
		if time.Since(last) > keepAliveEvery {
			// An answer to it comes in the stream; the loop skips it.
			c.cseq++
			fmt.Fprintf(c.c, "GET_PARAMETER %s RTSP/1.0\r\nCSeq: %d\r\nSession: %s\r\n", c.url.String(), c.cseq, c.session)
			if c.auth != nil {
				fmt.Fprintf(c.c, "Authorization: %s\r\n", c.auth("GET_PARAMETER", c.url.String()))
			}
			io.WriteString(c.c, "\r\n")
			last = time.Now()
		}
		c.c.SetReadDeadline(time.Now().Add(10 * time.Second))
		b, err := c.r.Peek(1)
		if err != nil {
			return nil, err
		}
		if b[0] != '$' {
			if _, err := c.readResponse(); err != nil {
				return nil, err
			}
			continue
		}
		ch, pkt, err := c.readInterleaved()
		if err != nil {
			return nil, err
		}
		if ch != 0 {
			continue // RTCP
		}
		if au := c.depacketize(pkt); au != nil {
			return au, nil
		}
	}
}

var startCode = []byte{0, 0, 0, 1}

// depacketize adds the RTP packet pkt to the access unit (RFC 6184: single NAL units, STAP-A and
// FU-A). It returns the access unit at the packet with the marker bit, or nil.
func (c *Conn) depacketize(pkt []byte) []byte {
	if len(pkt) < 12 || pkt[0]>>6 != 2 {
		return nil
	}
	marker := pkt[1]&0x80 != 0
	off := 12 + int(pkt[0]&0x0f)*4
	if pkt[0]&0x10 != 0 { // an extension
		if len(pkt) < off+4 {
			return nil
		}
		off += 4 + int(binary.BigEndian.Uint16(pkt[off+2:]))*4
	}
	end := len(pkt)
	if pkt[0]&0x20 != 0 { // padding
		end -= int(pkt[end-1])
	}
	if off >= end {
		return nil
	}
	p := pkt[off:end]
	switch typ := p[0] & 0x1f; {
	case typ >= 1 && typ <= 23:
		c.au = append(append(c.au, startCode...), p...)
	case typ == 24: // STAP-A
		for q := p[1:]; len(q) >= 2; {
			n := int(binary.BigEndian.Uint16(q))
			if len(q) < 2+n {
				break
			}
			c.au = append(append(c.au, startCode...), q[2:2+n]...)
			q = q[2+n:]
		}
	case typ == 28: // FU-A
		if len(p) < 2 {
			return nil
		}
		if p[1]&0x80 != 0 {
			c.au = append(append(c.au, startCode...), p[0]&0xe0|p[1]&0x1f)
			c.fuStart = true
		}
		if c.fuStart {
			c.au = append(c.au, p[2:]...)
		}
		if p[1]&0x40 != 0 {
			c.fuStart = false
		}
	}
	if !marker || len(c.au) == 0 {
		return nil
	}
	au := c.au
	c.au = nil
	if !c.sent {
		c.sent = true
		var pre []byte
		for _, ps := range c.params {
			pre = append(append(pre, startCode...), ps...)
		}
		au = append(pre, au...)
	}
	return au
}
