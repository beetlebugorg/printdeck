// Package bambu talks to a Bambu Lab printer on the local network: the state and the commands over
// MQTT on port 8883, and the address of the camera (RTSPS on port 322).
//
// The login is the user "bblp" and the access code of the printer (its screen shows the code in
// the network settings). The certificate of the printer comes from a device CA of Bambu Lab, and
// names the serial number of the printer: the client checks both on each connection (Check), so
// another device on the network cannot take the place of the printer.
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
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
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

// Client is the MQTT link to one printer.
type Client struct {
	Addr   string // "192.168.1.50" or "host:port"
	Serial string // the serial number, for the topics
	Code   string // the access code
	// Pin is the SHA-256 of the last certificate of the printer in hex, or "". Check sets it.
	// Roots are the root certificates for the check, or nil for the roots of Bambu Lab (a test
	// sets its own).
	Pin   string
	Roots *x509.CertPool

	mu     sync.Mutex
	conn   *mqttConn
	seen   string
	inter  []*x509.Certificate // the intermediate certificates of the last full chain of the printer
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

// Seen returns the SHA-256 of the certificate of the last connection in hex, or "".
func (c *Client) Seen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

// tlsConfig checks the certificate of the printer (Check). Go cannot check it with its own check:
// the certificate names the serial number in its CN, and has no host name.
func (c *Client) tlsConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, // Check in VerifyConnection checks the chain and the serial
		MinVersion:         tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return c.Check(&cs)
		},
	}
}

// bblCA has the root certificates of Bambu Lab: BBL CA, and BBL CA2 RSA and BBL CA2 ECC under it.
// They come from resources/cert/printer.cer of Bambu Studio and OrcaSlicer. Each printer has a
// certificate of a BBL Device CA under them, with its serial number as its CN.
//
//go:embed bbl-ca.pem
var bblCA []byte

// bblDeviceCA has the BBL Device CA certificates that the deck knows. The camera of a printer
// sends only the certificate of the printer, with no BBL Device CA: the check then uses these,
// and the BBL Device CA that the other connections to the printer sent.
//
//go:embed bbl-device-ca.pem
var bblDeviceCA []byte

var bblRoots = func() *x509.CertPool {
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(bblCA) {
		panic("bambu: no certificates in bbl-ca.pem")
	}
	return p
}()

// ErrCert is a printer certificate that is not a certificate of Bambu Lab for the serial number.
var ErrCert = errors.New("the certificate of the printer is not from Bambu Lab for this serial number")

// bblIntermediates are the certificates of bblDeviceCA.
var bblIntermediates = func() []*x509.Certificate {
	var out []*x509.Certificate
	rest := bblDeviceCA
	for {
		var b *pem.Block
		if b, rest = pem.Decode(rest); b == nil {
			break
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			panic("bambu: bbl-device-ca.pem: " + err.Error())
		}
		out = append(out, c)
	}
	return out
}()

// verifyPrinter checks the certificates that the printer sent: a chain to the roots of Bambu Lab,
// and the serial number as the CN of the certificate of the printer. known are more
// intermediate certificates for a chain with none. An intermediate certificate only helps to
// find a chain: the chain must still end at a root.
func verifyPrinter(certs []*x509.Certificate, serial string, roots *x509.CertPool, known ...*x509.Certificate) error {
	if len(certs) == 0 {
		return errors.New("the printer sent no certificate")
	}
	leaf := certs[0]
	inter := x509.NewCertPool()
	for _, ic := range certs[1:] {
		inter.AddCert(ic)
	}
	for _, ic := range known {
		inter.AddCert(ic)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return fmt.Errorf("%w: %v", ErrCert, err)
	}
	if serial == "" || !strings.EqualFold(leaf.Subject.CommonName, serial) {
		return fmt.Errorf("%w: it is for %q", ErrCert, leaf.Subject.CommonName)
	}
	return nil
}

// Check checks the certificates of a connection to the printer: MQTT, the files and the camera.
// The chain goes to the roots of Bambu Lab, also for the camera, which sends no intermediate
// certificate, and the certificate names the serial number of the
// printer (verifyPrinter). Seen then gives its SHA-256. A certificate other than Pin is a new
// certificate of the same printer, for example after a firmware update: Check takes it as the
// new Pin, and logs the change.
func (c *Client) Check(cs *tls.ConnectionState) error {
	roots, known := c.Roots, []*x509.Certificate(nil)
	if roots == nil {
		roots, known = bblRoots, bblIntermediates
	}
	c.mu.Lock()
	known = append(slices.Clip(known), c.inter...)
	c.mu.Unlock()
	if err := verifyPrinter(cs.PeerCertificates, c.Serial, roots, known...); err != nil {
		return err
	}
	fp := Fingerprint(cs.PeerCertificates[0])
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(cs.PeerCertificates) > 1 {
		c.inter = slices.Clone(cs.PeerCertificates[1:])
	}
	c.seen = fp
	if c.Pin != "" && !strings.EqualFold(fp, c.Pin) {
		log.Printf("bambu: %s has a new certificate %s (it was %s)", c.Serial, fp, c.Pin)
	}
	c.Pin = fp
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

// Gcode runs G-code lines on the printer (gcode_line). The printer needs Developer Mode for it.
func (c *Client) Gcode(lines ...string) error {
	return c.send("print", "gcode_line", map[string]any{"param": strings.Join(lines, "\n") + "\n"})
}

// SetNozzleTemp sets the target of the extruder id of a printer with two extruders, in °C
// (set_nozzle_temp, as Bambu Studio sends it).
func (c *Client) SetNozzleTemp(id, temp int) error {
	return c.send("print", "set_nozzle_temp", map[string]any{"extruder_index": id, "target_temp": max(0, temp)})
}

// SelectExtruder makes the extruder id the extruder in use, on a printer with two extruders
// (select_extruder). The printer moves the toolhead to change it.
func (c *Client) SelectExtruder(id int) error {
	return c.send("print", "select_extruder", map[string]any{"extruder_index": id})
}

// The speed levels of print_speed.
const (
	SpeedSilent    = 1
	SpeedStandard  = 2
	SpeedSport     = 3
	SpeedLudicrous = 4
)

// SetSpeed sets the speed level of the print (SpeedSilent to SpeedLudicrous).
func (c *Client) SetSpeed(level int) error {
	if level < SpeedSilent || level > SpeedLudicrous {
		return fmt.Errorf("no speed level %d", level)
	}
	return c.send("print", "print_speed", map[string]any{"param": strconv.Itoa(level)})
}

// The lights of ledctrl.
const (
	ChamberLight = "chamber_light"
	WorkLight    = "work_light"
)

// SetLight turns the light node on or off.
func (c *Client) SetLight(node string, on bool) error {
	mode := "off"
	if on {
		mode = "on"
	}
	return c.send("system", "ledctrl", map[string]any{"led_node": node, "led_mode": mode,
		"led_on_time": 500, "led_off_time": 500, "loop_times": 0, "interval_time": 0})
}

// ChangeFilament loads the filament of the tray slot (Unit*4+Index, or an external spool) into the
// nozzle, as Bambu Studio does with ams_change_filament: the printer unloads the filament in the
// nozzle first. from and to are the nozzle temperatures of the old and the new filament, in °C.
func (c *Client) ChangeFilament(slot, from, to int) error {
	amsID, slotID := slot/4, slot%4
	if slot == ExternalMain || slot == ExternalDeputy {
		amsID, slotID = slot, 0
	}
	return c.send("print", "ams_change_filament", map[string]any{"target": slot, "curr_temp": from, "tar_temp": to,
		"ams_id": amsID, "slot_id": slotID})
}

// UnloadFilament pulls the filament out of the nozzle, back to its tray (ams_change_filament to
// 255).
func (c *Client) UnloadFilament(from int) error {
	return c.send("print", "ams_change_filament", map[string]any{"target": 255, "curr_temp": from, "tar_temp": from,
		"ams_id": 255, "slot_id": 255})
}

// ReadRFID asks the AMS to read the RFID tag of the spool in the tray slot again (ams_get_rfid).
func (c *Client) ReadRFID(slot int) error {
	return c.send("print", "ams_get_rfid", map[string]any{"ams_id": slot / 4, "slot_id": slot % 4})
}

// The checks of the camera AI of xcam_control_set.
const (
	CheckFirstLayer  = "first_layer_inspector"
	CheckSpaghetti   = "spaghetti_detector"
	CheckPlateMarker = "buildplate_marker_detector"
)

// SetCheck turns a check of the camera AI on or off (xcam_control_set, as Bambu Studio sends it).
// sensitivity is "low", "medium" or "high" for the spaghetti check, or "".
func (c *Client) SetCheck(module string, on bool, sensitivity string) error {
	f := map[string]any{"module_name": module, "control": on, "enable": on, "print_halt": true}
	if sensitivity != "" {
		f["halt_print_sensitivity"] = sensitivity
	}
	return c.send("xcam", "xcam_control_set", f)
}

// SetTimelapse turns the time lapse of the prints on or off (ipcam_timelapse).
func (c *Client) SetTimelapse(on bool) error {
	return c.send("camera", "ipcam_timelapse", map[string]any{"control": enable(on)})
}

// SetRecord turns the recording of the prints on or off (ipcam_record_set).
func (c *Client) SetRecord(on bool) error {
	return c.send("camera", "ipcam_record_set", map[string]any{"control": enable(on)})
}

// enable is "enable" for on, else "disable".
func enable(on bool) string {
	if on {
		return "enable"
	}
	return "disable"
}

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
