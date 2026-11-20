package bambu

import (
	"context"
	"net"
	"testing"
	"time"
)

const notice = "NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1990\r\nServer: UPnP/1.0\r\nLocation: 192.168.77.171\r\n" +
	"NT: urn:bambulab-com:device:3dprinter:1\r\nNTS: ssdp:alive\r\nUSN: 20P5AJ000000001\r\nCache-Control: max-age=1800\r\n" +
	"DevModel.bambu.com: N6\r\nDevName.bambu.com: X2D\r\nDevConnect.bambu.com: lan\r\nDevBind.bambu.com: free\r\n\r\n"

// A notice of a printer gives its address, serial number, name and model. Other packets are not
// printers.
func TestParseNotify(t *testing.T) {
	f, ok := ParseNotify([]byte(notice))
	if !ok || f != (Found{Addr: "192.168.77.171", Serial: "20P5AJ000000001", Name: "X2D", Model: "N6"}) {
		t.Fatalf("%+v %v", f, ok)
	}
	for _, bad := range []string{"M99999", "NOTIFY * HTTP/1.1\r\nNT: upnp:rootdevice\r\nUSN: x\r\nLocation: 1.2.3.4\r\n\r\n",
		"NOTIFY * HTTP/1.1\r\nNT: urn:bambulab-com:device:3dprinter:1\r\nLocation: not-an-ip\r\nUSN: S\r\n\r\n"} {
		if _, ok := ParseNotify([]byte(bad)); ok {
			t.Errorf("a printer from %q", bad)
		}
	}
}

// Discover returns each printer once, from the notices that come in while it listens.
func TestDiscover(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		c, err := net.Dial("udp4", addr)
		if err != nil {
			return
		}
		defer c.Close()
		for i := 0; i < 3; i++ {
			c.Write([]byte(notice))
			c.Write([]byte("noise"))
		}
	}()
	found, err := Discover(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Serial != "20P5AJ000000001" {
		t.Fatalf("%+v", found)
	}
}
