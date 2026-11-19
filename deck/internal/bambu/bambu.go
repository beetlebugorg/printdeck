// Package bambu talks to a Bambu Lab printer on the local network: the state and the commands over
// MQTT on port 8883, and the address of the camera (RTSPS on port 322).
//
// The login is the user "bblp" and the access code of the printer (its screen shows the code in
// the network settings). The printer has a self-signed certificate. The client pins the SHA-256 of
// that certificate: the first connection learns it (Client.Seen), and the next connections refuse
// another certificate.
//
// The printer sends a report on device/<serial>/report about once a second. An X1 or an X2D sends
// the full state each time; a P1 or an A1 sends only the values that changed. The client merges
// each report into the state that it has, so both work.
package bambu

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The ports of the printer.
const (
	MQTTPort   = 8883
	CameraPort = 322
)

// User is the user name of the LAN login.
const User = "bblp"

// keepAlive is the MQTT keep alive. The client sends PINGREQ at half of it.
const keepAlive = 30 * time.Second

// ErrPin is a printer certificate that is not the pinned certificate.
var ErrPin = errors.New("the certificate of the printer changed")

// Client is the MQTT link to one printer.
type Client struct {
	Addr   string // "192.168.1.50" or "host:port"
	Serial string // the serial number, for the topics
	Code   string // the access code
	// Pin is the SHA-256 of the certificate of the printer in hex, or "" to accept the first
	// certificate (Seen gives it).
	Pin string

	mu     sync.Mutex
	conn   *mqttConn
	seen   string
	seq    int
	merged map[string]any
	model  string // the product name of the printer from get_version, for example "Bambu Lab X2D"
	fw     string // the firmware version of the printer
}

// Version returns the product name and the firmware version of the printer, or "" before the
// printer answered get_version.
func (c *Client) Version() (model, firmware string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.model, c.fw
}

// readVersion keeps the answer to get_version: the module "ota" is the printer.
func (c *Client) readVersion(payload []byte) {
	var msg struct {
		Info struct {
			Command string `json:"command"`
			Module  []struct {
				Name    string `json:"name"`
				Product string `json:"product_name"`
				SW      string `json:"sw_ver"`
			} `json:"module"`
		} `json:"info"`
	}
	if json.Unmarshal(payload, &msg) != nil || msg.Info.Command != "get_version" {
		return
	}
	for _, m := range msg.Info.Module {
		if m.Name == "ota" {
			c.mu.Lock()
			c.model, c.fw = m.Product, m.SW
			c.mu.Unlock()
		}
	}
}

// addr returns Addr with the MQTT port.
func (c *Client) addr() string {
	if _, _, err := net.SplitHostPort(c.Addr); err == nil {
		return c.Addr
	}
	return net.JoinHostPort(c.Addr, strconv.Itoa(MQTTPort))
}

// HasPin reports that Pin is set.
func (c *Client) HasPin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Pin != ""
}

// SetPin sets Pin after the first connection.
func (c *Client) SetPin(pin string) {
	c.mu.Lock()
	c.Pin = pin
	c.mu.Unlock()
}

// Seen returns the SHA-256 of the certificate of the last connection in hex, or "".
func (c *Client) Seen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

// tlsConfig checks the certificate of the printer against Pin. The certificate has no host name
// that the deck can check, so the pin is the check.
func (c *Client) tlsConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the printer sent no certificate")
			}
			fp := Fingerprint(cs.PeerCertificates[0])
			c.mu.Lock()
			c.seen = fp
			c.mu.Unlock()
			c.mu.Lock()
			pin := c.Pin
			c.mu.Unlock()
			if pin != "" && !strings.EqualFold(fp, pin) {
				return ErrPin
			}
			return nil
		},
	}
}

// Check checks the certificate of the camera against Pin: the printer has one certificate for
// MQTT, the files and the camera. With no Pin it accepts the certificate.
func (c *Client) Check(cs *tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("the printer sent no certificate")
	}
	c.mu.Lock()
	pin := c.Pin
	c.mu.Unlock()
	if pin != "" && !strings.EqualFold(Fingerprint(cs.PeerCertificates[0]), pin) {
		return ErrPin
	}
	return nil
}

// Fingerprint returns the SHA-256 of a certificate in hex.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Run connects, subscribes to the reports, and calls onReport with the merged state after each
// report, until ctx ends or the link fails. It returns the error of the link. The caller connects
// again after a wait.
func (c *Client) Run(ctx context.Context, onReport func(Report)) error {
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: c.tlsConfig()}
	nc, err := d.DialContext(ctx, "tcp", c.addr())
	if err != nil {
		return err
	}
	defer nc.Close()
	stop := context.AfterFunc(ctx, func() { nc.Close() })
	defer stop()
	return c.serve(ctx, nc, onReport)
}

// serve runs the MQTT session on nc.
func (c *Client) serve(ctx context.Context, nc net.Conn, onReport func(Report)) error {
	m := &mqttConn{c: nc, r: bufio.NewReaderSize(nc, 64<<10)}
	nc.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := m.connect("printdeck-"+strconv.FormatInt(time.Now().UnixNano()%1e9, 36), User, c.Code, keepAlive); err != nil {
		return err
	}
	if err := m.subscribe("device/" + c.Serial + "/report"); err != nil {
		return err
	}
	c.mu.Lock()
	c.conn, c.merged = m, nil
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn == m {
			c.conn = nil
		}
		c.mu.Unlock()
	}()
	if err := c.PushAll(); err != nil {
		return err
	}
	if err := c.send("info", "get_version", nil); err != nil {
		return err
	}
	pings := time.NewTicker(keepAlive / 2)
	defer pings.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pings.C:
				if m.writePacket(pktPingreq, 0, nil) != nil {
					return
				}
			}
		}
	}()
	for {
		// The printer reports each second. No packet for twice the keep alive is a lost link.
		nc.SetReadDeadline(time.Now().Add(2 * keepAlive))
		typ, flags, body, err := m.readPacket()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if typ != pktPublish {
			continue
		}
		_, id, payload, err := parsePublish(flags, body)
		if err != nil {
			return err
		}
		if id != 0 {
			m.writePacket(pktPuback, 0, []byte{byte(id >> 8), byte(id)})
		}
		r, ok := c.merge(payload)
		if !ok {
			c.readVersion(payload)
		}
		if ok && onReport != nil {
			onReport(r)
		}
	}
}

// merge puts the report payload into the merged state, and returns the merged state. ok is false
// for a payload with no print state, for example the answer to a command.
func (c *Client) merge(payload []byte) (Report, bool) {
	var msg map[string]any
	if json.Unmarshal(payload, &msg) != nil {
		return Report{}, false
	}
	p, ok := msg["print"].(map[string]any)
	if !ok {
		return Report{}, false
	}
	c.mu.Lock()
	if c.merged == nil {
		c.merged = map[string]any{}
	}
	mergeMap(c.merged, p)
	b, _ := json.Marshal(c.merged)
	c.mu.Unlock()
	var r Report
	if json.Unmarshal(b, &r) != nil {
		return Report{}, false
	}
	return r, true
}

// mergeMap puts src into dst. Maps merge key by key. Lists and values replace.
func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeMap(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

// ErrOffline is a command with no link to the printer.
var ErrOffline = errors.New("no link to the printer")

// send publishes a request of the kind ("print", "pushing", "info") with the command and the
// fields.
func (c *Client) send(kind, command string, fields map[string]any) error {
	c.mu.Lock()
	m := c.conn
	c.seq++
	seq := c.seq
	c.mu.Unlock()
	if m == nil {
		return ErrOffline
	}
	body := map[string]any{"sequence_id": strconv.Itoa(seq), "command": command}
	for k, v := range fields {
		body[k] = v
	}
	b, err := json.Marshal(map[string]any{kind: body})
	if err != nil {
		return err
	}
	return m.publish("device/"+c.Serial+"/request", b)
}

// PushAll asks the printer for its full state.
func (c *Client) PushAll() error {
	return c.send("pushing", "pushall", map[string]any{"version": 1, "push_target": 1})
}

// Pause pauses the print.
func (c *Client) Pause() error { return c.send("print", "pause", map[string]any{"param": ""}) }

// Resume resumes the print.
func (c *Client) Resume() error { return c.send("print", "resume", map[string]any{"param": ""}) }

// Stop stops the print.
func (c *Client) Stop() error { return c.send("print", "stop", map[string]any{"param": ""}) }

// Start is a print of a 3MF on the card of the printer.
type Start struct {
	File  string // the name of the 3MF at the top of the card
	Name  string // the name of the print for the screen of the printer
	Plate string // the G-code of the plate in the 3MF (Plate.GcodePath)
	// Trays is the tray (Unit*4+Index) of each filament of the slicer, by filament number less 1,
	// or -1 for a filament that the plate does not use. An empty Trays prints from the external
	// spool.
	Trays     []int
	Timelapse bool
	// The checks of the printer before the print.
	BedLevel, FlowCali, VibrationCali, LayerInspect bool
}

// StartPrint starts the print s. The printer needs the 3MF on its card first (Upload). The
// fields are those of Bambu Studio for a print over LAN; ams_mapping2 is the form of the H2D and
// the X2D, which have more than one nozzle.
func (c *Client) StartPrint(s Start) error {
	mapping := make([]int, len(s.Trays))
	mapping2 := make([]map[string]int, len(s.Trays))
	for i, t := range s.Trays {
		mapping[i] = t
		if t < 0 {
			mapping2[i] = map[string]int{"ams_id": 255, "slot_id": 255}
		} else {
			mapping2[i] = map[string]int{"ams_id": t / 4, "slot_id": t % 4}
		}
	}
	return c.send("print", "project_file", map[string]any{
		"param":          s.Plate,
		"project_id":     "0",
		"profile_id":     "0",
		"task_id":        "0",
		"subtask_id":     "0",
		"subtask_name":   s.Name,
		"file":           s.File,
		"url":            "ftp:///" + s.File,
		"md5":            "",
		"bed_type":       "auto",
		"timelapse":      s.Timelapse,
		"bed_levelling":  s.BedLevel,
		"flow_cali":      s.FlowCali,
		"vibration_cali": s.VibrationCali,
		"layer_inspect":  s.LayerInspect,
		"use_ams":        len(s.Trays) > 0,
		"ams_mapping":    mapping,
		"ams_mapping2":   mapping2,
	})
}

// CameraURL returns the RTSPS address of the camera: the address in the report, or the usual
// address of an X1, an H2D or an X2D.
func (c *Client) CameraURL(r Report) string {
	if r.IPCam.RTSP != "" && strings.HasPrefix(r.IPCam.RTSP, "rtsps://") {
		return r.IPCam.RTSP
	}
	host := c.Addr
	if h, _, err := net.SplitHostPort(c.Addr); err == nil {
		host = h
	}
	return fmt.Sprintf("rtsps://%s:%d/streaming/live/1", host, CameraPort)
}
