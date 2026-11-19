package bambu

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// FTPPort is the port of the implicit FTPS server of the printer: the files on its card.
const FTPPort = 990

// ErrNoFile is a file that the printer does not have.
var ErrNoFile = errors.New("the printer does not have the file")

// ftpConn is one FTPS session: the control connection, in TLS from the start.
type ftpConn struct {
	c    *tls.Conn
	tp   *textproto.Conn
	host string
	cfg  *tls.Config
}

// dialFTP logs in to the FTPS server of the printer. The data connections reuse the TLS session of
// the control connection, as the server asks.
func (c *Client) dialFTP(ctx context.Context) (*ftpConn, error) {
	host := c.Addr
	if h, _, err := net.SplitHostPort(c.Addr); err == nil {
		host = h
	}
	cfg := c.tlsConfig()
	cfg.ClientSessionCache = tls.NewLRUClientSessionCache(4)
	cfg.ServerName = c.Serial // the same name for each connection, so the session is reused
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: cfg}
	nc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(FTPPort)))
	if err != nil {
		return nil, err
	}
	tc := nc.(*tls.Conn)
	f := &ftpConn{c: tc, tp: textproto.NewConn(tc), host: host, cfg: cfg}
	tc.SetDeadline(time.Now().Add(30 * time.Second))
	if _, _, err := f.tp.ReadResponse(220); err != nil {
		f.Close()
		return nil, err
	}
	steps := []struct {
		cmd  string
		code int
	}{{"USER " + User, 331}, {"PASS " + c.Code, 230}, {"TYPE I", 200}, {"PBSZ 0", 200}, {"PROT P", 200}}
	for _, s := range steps {
		if _, err := f.cmd(s.code, s.cmd); err != nil {
			f.Close()
			if s.code == 230 {
				return nil, ErrRefused
			}
			return nil, err
		}
	}
	return f, nil
}

// cmd sends a command and reads its answer, which must have the code.
func (f *ftpConn) cmd(code int, line string) (string, error) {
	id, err := f.tp.Cmd("%s", line)
	if err != nil {
		return "", err
	}
	f.tp.StartResponse(id)
	defer f.tp.EndResponse(id)
	_, msg, err := f.tp.ReadResponse(code)
	return msg, err
}

// Close ends the session.
func (f *ftpConn) Close() {
	f.tp.Cmd("QUIT")
	f.c.Close()
}

// data opens a passive data connection, with TLS from the first read.
func (f *ftpConn) data(ctx context.Context) (*tls.Conn, error) {
	msg, err := f.cmd(227, "PASV")
	if err != nil {
		return nil, err
	}
	// "Entering Passive Mode (h1,h2,h3,h4,p1,p2)": the deck uses the address of the printer, and
	// the port of the answer.
	i, j := strings.IndexByte(msg, '('), strings.IndexByte(msg, ')')
	if i < 0 || j < i {
		return nil, fmt.Errorf("ftps: a bad PASV answer %q", msg)
	}
	p := strings.Split(msg[i+1:j], ",")
	if len(p) != 6 {
		return nil, fmt.Errorf("ftps: a bad PASV answer %q", msg)
	}
	hi, _ := strconv.Atoi(p[4])
	lo, _ := strconv.Atoi(p[5])
	d := &net.Dialer{Timeout: 10 * time.Second}
	nc, err := d.DialContext(ctx, "tcp", net.JoinHostPort(f.host, strconv.Itoa(hi*256+lo)))
	if err != nil {
		return nil, err
	}
	// The TLS handshake waits for the first read: the server starts TLS on the data connection
	// only after the command (vsftpd).
	return tls.Client(nc, f.cfg), nil
}

// transfer runs a command with a data connection, and copies the data to w, at most max bytes.
func (f *ftpConn) transfer(ctx context.Context, line string, w io.Writer, max int64) error {
	dc, err := f.data(ctx)
	if err != nil {
		return err
	}
	defer dc.Close()
	id, err := f.tp.Cmd("%s", line)
	if err != nil {
		return err
	}
	f.tp.StartResponse(id)
	code, msg, err := f.tp.ReadResponse(1)
	f.tp.EndResponse(id)
	if err != nil {
		if code == 550 {
			return ErrNoFile
		}
		return fmt.Errorf("ftps: %s: %d %s", line, code, msg)
	}
	dc.SetDeadline(time.Now().Add(5 * time.Minute))
	n, err := io.Copy(w, io.LimitReader(bufio.NewReader(dc), max+1))
	dc.Close()
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("ftps: the file is larger than %d bytes", max)
	}
	f.c.SetDeadline(time.Now().Add(30 * time.Second))
	_, _, err = f.tp.ReadResponse(226)
	return err
}

// Fetch copies the file path on the card of the printer to w, at most max bytes. It only reads.
func (c *Client) Fetch(ctx context.Context, path string, w io.Writer, max int64) error {
	f, err := c.dialFTP(ctx)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.transfer(ctx, "RETR "+path, w, max)
}

// List returns the names in the folder dir of the card of the printer.
func (c *Client) List(ctx context.Context, dir string) ([]string, error) {
	f, err := c.dialFTP(ctx)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b strings.Builder
	if err := f.transfer(ctx, "NLST "+dir, &b, 1<<20); err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// Upload writes r to the file name at the top of the card of the printer. progress gets the bytes
// sent so far, or is nil.
func (c *Client) Upload(ctx context.Context, name string, r io.Reader, progress func(n int64)) error {
	f, err := c.dialFTP(ctx)
	if err != nil {
		return err
	}
	defer f.Close()
	dc, err := f.data(ctx)
	if err != nil {
		return err
	}
	defer dc.Close()
	id, err := f.tp.Cmd("STOR /%s", name)
	if err != nil {
		return err
	}
	f.tp.StartResponse(id)
	code, msg, err := f.tp.ReadResponse(1)
	f.tp.EndResponse(id)
	if err != nil {
		return fmt.Errorf("ftps: STOR %s: %d %s", name, code, msg)
	}
	dc.SetDeadline(time.Now().Add(30 * time.Minute))
	w := io.Writer(dc)
	if progress != nil {
		w = &countWriter{w: dc, f: progress}
	}
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	if err := dc.Close(); err != nil {
		return err
	}
	f.c.SetDeadline(time.Now().Add(2 * time.Minute))
	_, _, err = f.tp.ReadResponse(226)
	return err
}

// countWriter reports the bytes written to f.
type countWriter struct {
	w io.Writer
	n int64
	f func(int64)
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.f(c.n)
	return n, err
}
