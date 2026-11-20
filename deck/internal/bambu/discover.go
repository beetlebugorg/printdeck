package bambu

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"time"
)

// Discovery on the network: a Bambu Lab printer sends an SSDP NOTIFY a few times a minute, to the
// broadcast address on UDP port 2021. The notice has the IP address of the printer (Location), its
// serial number (USN), its name (DevName.bambu.com) and its model code (DevModel.bambu.com). The
// access code is not in it: the user types it.

// DiscoverPort is the UDP port of the notices of the printers.
const DiscoverPort = 2021

// Found is a printer that sent a notice.
type Found struct {
	Addr   string // "192.168.77.171"
	Serial string // "20P5AJ000000000"
	Name   string // the name of the printer in its settings, for example "X2D"
	Model  string // the model code, for example "N6"
}

// ParseNotify reads one SSDP notice of a Bambu Lab printer. ok is false for another packet.
func ParseNotify(b []byte) (Found, bool) {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	line, err := r.ReadLine()
	if err != nil || !strings.HasPrefix(line, "NOTIFY ") && !strings.HasPrefix(line, "HTTP/1.1 200") {
		return Found{}, false
	}
	h, err := r.ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return Found{}, false
	}
	if !strings.Contains(strings.ToLower(h.Get("NT")+h.Get("ST")), "bambulab") {
		return Found{}, false
	}
	f := Found{Addr: strings.TrimSpace(h.Get("Location")), Serial: strings.TrimSpace(h.Get("USN")),
		Name: strings.TrimSpace(h.Get("DevName.bambu.com")), Model: strings.TrimSpace(h.Get("DevModel.bambu.com"))}
	if ip := net.ParseIP(f.Addr); ip == nil || ip.To4() == nil || f.Serial == "" {
		return Found{}, false
	}
	return f, true
}

// Discover listens on addr (":2021") for the notices of the printers until ctx ends, and returns
// each printer once, in the order of their addresses.
func Discover(ctx context.Context, addr string) ([]Found, error) {
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(ctx, "udp4", addr)
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	stop := context.AfterFunc(ctx, func() { pc.SetReadDeadline(time.Now()) })
	defer stop()
	seen := map[string]Found{}
	buf := make([]byte, 4096)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			var ne net.Error
			if ctx.Err() != nil || errors.As(err, &ne) && ne.Timeout() {
				break
			}
			return nil, err
		}
		if f, ok := ParseNotify(buf[:n]); ok {
			seen[f.Serial] = f
		}
	}
	out := make([]Found, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out, nil
}
