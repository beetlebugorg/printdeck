// Package wifi talks to wpa_supplicant over its control socket. It scans, lists the networks, and
// reads the state of the connection. The printdeck-wifi script of the OS image sets the network.
package wifi

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Network is one network from a scan.
type Network struct {
	SSID   string
	Signal int // dBm
	Secure bool
}

// Status is the state of the connection.
type Status struct {
	State string // the wpa_state of wpa_supplicant, like COMPLETED when connected
	SSID  string
	IP    string
}

// Connected says if the Pi is on a network and has an address.
func (s Status) Connected() bool { return s.State == "COMPLETED" && s.IP != "" }

// Client is the control socket of one interface.
type Client struct {
	Dir   string // the ctrl_interface folder
	Iface string
}

func New() *Client { return &Client{Dir: "/run/wpa_supplicant", Iface: "wlan0"} }

var seq atomic.Int64

// request sends one command and returns the reply. Each request uses its own socket, so
// requests from different goroutines do not mix their replies.
func (c *Client) request(cmd string) (string, error) {
	local := filepath.Join(os.TempDir(), fmt.Sprintf("deck-wpa-%d-%d", os.Getpid(), seq.Add(1)))
	conn, err := net.DialUnix("unixgram",
		&net.UnixAddr{Name: local, Net: "unixgram"},
		&net.UnixAddr{Name: filepath.Join(c.Dir, c.Iface), Net: "unixgram"})
	if err != nil {
		return "", err
	}
	defer os.Remove(local)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", err
	}
	buf := make([]byte, 64<<10)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// Scan starts a scan. The results come a few seconds later.
func (c *Client) Scan() error {
	r, err := c.request("SCAN")
	if err != nil {
		return err
	}
	if r = strings.TrimSpace(r); r != "OK" && r != "FAIL-BUSY" {
		return errors.New("scan: " + r)
	}
	return nil
}

// Networks returns the networks of the last scan, the strongest first.
func (c *Client) Networks() ([]Network, error) {
	r, err := c.request("SCAN_RESULTS")
	if err != nil {
		return nil, err
	}
	return parseResults(r), nil
}

// Status returns the state of the connection.
func (c *Client) Status() (Status, error) {
	r, err := c.request("STATUS")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(r), nil
}

// parseResults reads the reply to SCAN_RESULTS: a header line, then one line per access point
// with bssid, frequency, signal, flags and ssid, separated by tabs. It keeps the strongest access
// point of each network, and leaves out hidden networks.
func parseResults(r string) []Network {
	best := map[string]Network{}
	for _, line := range strings.Split(r, "\n")[1:] {
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 {
			continue
		}
		ssid := unescape(f[4])
		if ssid == "" || strings.Trim(ssid, "\x00") == "" {
			continue
		}
		sig, _ := strconv.Atoi(f[2])
		n := Network{SSID: ssid, Signal: sig, Secure: strings.Contains(f[3], "WPA") || strings.Contains(f[3], "WEP") || strings.Contains(f[3], "RSN")}
		if old, ok := best[ssid]; !ok || n.Signal > old.Signal {
			best[ssid] = n
		}
	}
	out := make([]Network, 0, len(best))
	for _, n := range best {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Signal != out[j].Signal {
			return out[i].Signal > out[j].Signal
		}
		return out[i].SSID < out[j].SSID
	})
	return out
}

// parseStatus reads the key=value lines of the reply to STATUS.
func parseStatus(r string) Status {
	var s Status
	for _, line := range strings.Split(r, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "wpa_state":
			s.State = v
		case "ssid":
			s.SSID = unescape(v)
		case "ip_address":
			s.IP = v
		}
	}
	return s
}

// unescape undoes the escapes of wpa_supplicant in an SSID: \\, \", \e, \n, \r, \t and \xNN.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'e':
			b.WriteByte(0x1b)
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'x':
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteByte(byte(v))
					i += 2
					continue
				}
			}
			b.WriteString(`\x`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
