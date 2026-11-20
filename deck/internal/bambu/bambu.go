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

	mu    sync.Mutex
	conn  *mqttConn
	seen  string
	inter []*x509.Certificate // the intermediate certificates of the last full chain of the printer
	seq   int
	// waiters are the commands that wait for their answer, by sequence_id (sendWait).
	waiters map[string]chan Reply
	merged  map[string]any
	model   string // the product name of the printer from get_version, for example "Bambu Lab X2D"
	fw      string // the firmware version of the printer
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
		c.deliver(payload)
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
	_, err := c.sendSeq(kind, command, fields, nil)
	return err
}

// Reply is the answer of the printer to a command: Result is "success" or "fail" (any case), and
// Reason the text of a failure.
type Reply struct {
	Command, Result, Reason string
	ErrCode                 int64
}

// Failed reports a reply with a failure.
func (r Reply) Failed() bool {
	return strings.EqualFold(r.Result, "fail") || strings.EqualFold(r.Result, "failed")
}

// sendWait sends the command and waits up to wait for the answer of the printer. ok is false when
// no answer came: the printer gives no answer to some commands.
func (c *Client) sendWait(kind, command string, fields map[string]any, wait time.Duration) (r Reply, ok bool, err error) {
	ch := make(chan Reply, 1)
	seq, err := c.sendSeq(kind, command, fields, ch)
	defer func() {
		c.mu.Lock()
		delete(c.waiters, seq)
		c.mu.Unlock()
	}()
	if err != nil {
		return Reply{}, false, err
	}
	select {
	case r := <-ch:
		return r, true, nil
	case <-time.After(wait):
		return Reply{}, false, nil
	}
}

// deliver gives an answer in payload to the command that waits for it.
func (c *Client) deliver(payload []byte) {
	var msg map[string]map[string]any
	if json.Unmarshal(payload, &msg) != nil {
		return
	}
	for _, body := range msg {
		seq, _ := body["sequence_id"].(string)
		cmd, _ := body["command"].(string)
		result, has := body["result"].(string)
		if seq == "" || !has || cmd == "push_status" {
			continue
		}
		c.mu.Lock()
		ch := c.waiters[seq]
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		r := Reply{Command: cmd, Result: result}
		r.Reason, _ = body["reason"].(string)
		if e, ok := body["err_code"].(float64); ok {
			r.ErrCode = int64(e)
		}
		select {
		case ch <- r:
		default:
		}
	}
}

// sendSeq sends the command with a new sequence_id, and returns it. A non-nil ch gets the answer
// of the printer (deliver).
func (c *Client) sendSeq(kind, command string, fields map[string]any, ch chan Reply) (string, error) {
	c.mu.Lock()
	m := c.conn
	c.seq++
	seq := strconv.Itoa(c.seq)
	if ch != nil {
		if c.waiters == nil {
			c.waiters = map[string]chan Reply{}
		}
		c.waiters[seq] = ch
	}
	c.mu.Unlock()
	if m == nil {
		return seq, ErrOffline
	}
	body := map[string]any{"sequence_id": seq, "command": command}
	for k, v := range fields {
		body[k] = v
	}
	b, err := json.Marshal(map[string]any{kind: body})
	if err != nil {
		return seq, err
	}
	return seq, m.publish("device/"+c.Serial+"/request", b)
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

// SetNozzleTemp sets the target of the extruder id of a printer with two extruders, in °C, with
// M104 T<id> (as bambuddy sends it; T is the extruder id of the reports).
func (c *Client) SetNozzleTemp(id, temp int) error {
	return c.Gcode(fmt.Sprintf("M104 T%d S%d", id, max(0, temp)))
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

// The AMS commands follow Bambu Studio, as bambuddy (an open farm program for Bambu Lab printers)
// documents them from captures of Studio: backend/app/services/bambu_mqtt.py.

// ChangeFilament loads the filament of the tray slot (Unit*4+Index, or an external spool) into its
// nozzle (ams_change_filament): the printer unloads the filament in the nozzle first. For a tray of
// an AMS the temperatures are -1: the printer takes those of the filaments. For an external spool
// they are temp, the nozzle temperature in °C.
func (c *Client) ChangeFilament(slot, temp int) error {
	f := map[string]any{"target": slot, "ams_id": slot / 4, "slot_id": slot % 4, "curr_temp": -1, "tar_temp": -1}
	if slot == ExternalMain || slot == ExternalDeputy {
		f["ams_id"], f["slot_id"], f["curr_temp"], f["tar_temp"] = 255, 0, temp, temp
		if slot == ExternalDeputy {
			f["slot_id"] = 254
		}
	}
	return c.answered("The load of the filament", "ams_change_filament", f)
}

// UnloadFilament pulls the filament out of the nozzle back to the AMS amsID (255 for an external
// spool), at the nozzle temperature temp in °C (ams_change_filament to 255).
func (c *Client) UnloadFilament(amsID, temp int) error {
	return c.answered("The unload of the filament", "ams_change_filament", map[string]any{"target": 255,
		"ams_id": amsID, "slot_id": 255, "curr_temp": temp, "tar_temp": temp})
}

// DryAMS starts the drying of the AMS amsID at temp °C for hours hours, for the filament type
// (ams_filament_drying, mode 1).
func (c *Client) DryAMS(amsID, temp, hours int, filament string) error {
	return c.answered("The drying", "ams_filament_drying", map[string]any{"ams_id": amsID, "temp": temp,
		"cooling_temp": 20, "duration": hours, "humidity": 0, "mode": 1, "rotate_tray": false,
		"filament": filament, "close_power_conflict": false})
}

// StopDryAMS stops the drying of the AMS amsID (ams_filament_drying, mode 0).
func (c *Client) StopDryAMS(amsID int) error {
	return c.answered("The stop of the drying", "ams_filament_drying", map[string]any{"ams_id": amsID, "temp": 0,
		"cooling_temp": 0, "duration": 0, "humidity": 0, "mode": 0, "rotate_tray": false, "filament": "",
		"close_power_conflict": false})
}

// answered sends a print command and waits for the answer of the printer: a failure is an error
// with the reason of the printer. No answer is not an error: the printer gives none to some
// commands.
func (c *Client) answered(what, command string, fields map[string]any) error {
	r, ok, err := c.sendWait("print", command, fields, 10*time.Second)
	if err != nil {
		return err
	}
	if ok && r.Failed() {
		reason := r.Reason
		if reason == "" {
			reason = fmt.Sprintf("error %d", r.ErrCode)
		}
		return fmt.Errorf("%s: the printer refused it: %s", what, reason)
	}
	return nil
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

// calibration is the value of a calibration of a start: 2 (auto: the printer decides) when on,
// 0 when off. Bambu Studio sends 0 off, 1 on or 2 auto.
func calibration(on bool) int {
	if on {
		return 2
	}
	return 0
}

// startWait is how long StartPrint waits for the answer of the printer. A test makes it shorter.
var startWait = 15 * time.Second

// StartURLs are the addresses of the 3MF name on the card for project_file, in the order to try:
// printers and firmware versions take different ones. The deck tries the next one only after the
// printer answers the start with a failure.
func StartURLs(name string) []string {
	return []string{"ftp://" + name, "ftp:///" + name, "file:///sdcard/" + name, "file:///mnt/sdcard/" + name}
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
	// Task is the id of the task, for task_id and subtask_id: a new number for each start, or ""
	// for "0". A printer can ignore a start with the id of the task before it (bambuddy saw a P1S
	// stay IDLE with a repeated "0"; the X2D took two starts with SUCCESS and did not begin them).
	Task string
	// The checks of the printer before the print.
	BedLevel, FlowCali, VibrationCali, LayerInspect bool
}

// StartPrint starts the print s, with the address url of its 3MF on the card (StartURLs). The
// printer needs the 3MF on its card first (Upload). The fields are those of Bambu Studio for a
// print over LAN; ams_mapping2 is the form of the H2D and the X2D, which have more than one
// nozzle. It returns the answer of the printer, and ok false when no answer came in startWait.
func (c *Client) StartPrint(s Start, url string) (Reply, bool, error) {
	mapping := make([]int, len(s.Trays))
	mapping2 := make([]map[string]int, len(s.Trays))
	for i, t := range s.Trays {
		mapping[i] = t
		switch {
		case t < 0:
			mapping2[i] = map[string]int{"ams_id": 255, "slot_id": 255}
		case t == ExternalMain || t == ExternalDeputy:
			// The external spool: -1 in the flat ams_mapping and the spool in ams_mapping2, as
			// Bambu Studio sends it. A raw 254 or 255 in the flat list makes the firmware of the
			// H2D fail (bambuddy, bambu_mqtt.py), and the X2D failed with 05004047.
			mapping[i] = -1
			mapping2[i] = map[string]int{"ams_id": t, "slot_id": 0}
		default:
			mapping2[i] = map[string]int{"ams_id": t / 4, "slot_id": t % 4}
		}
	}
	// The fields, their names and their types are those of the stock network plugin of Bambu
	// Studio (open-bamboo-networking, print_job.cpp). The printer parses them strictly: a field
	// that is missing or of another type made the X2D crash its MQTT handler ("forward coredump",
	// HMS 0500-0300-0002-0010) on 2026-11-19.
	task := s.Task
	if task == "" {
		task = "0"
	}
	return c.sendWait("print", "project_file", map[string]any{
		"param":              s.Plate,
		"project_id":         "0",
		"profile_id":         "0",
		"task_id":            task,
		"subtask_id":         task,
		"subtask_name":       s.Name,
		"file":               s.File,
		"url":                url,
		"md5":                "from_sd_card",
		"bed_type":           "auto",
		"bed_leveling":       s.BedLevel,
		"flow_cali":          s.FlowCali,
		"vibration_cali":     s.VibrationCali,
		"layer_inspect":      s.LayerInspect,
		"timelapse":          s.Timelapse,
		"use_ams":            len(s.Trays) > 0,
		"ams_mapping":        mapping,
		"ams_mapping2":       mapping2,
		"auto_bed_leveling":  calibration(s.BedLevel),
		"nozzle_offset_cali": calibration(true),
		"extrude_cali_flag":  calibration(s.FlowCali),
		"cfg":                "0",
	}, startWait)
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

// Connected reports that the MQTT link is up.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}
