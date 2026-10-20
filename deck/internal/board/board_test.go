package board

import "testing"

func TestName(t *testing.T) {
	for _, c := range []struct{ dev, want string }{
		{"/dev/serial/by-id/usb-UltiMachine__ultimachine.com__RAMBo_75935323230351802211-if00", "UltiMachine ultimachine.com RAMBo"},
		{"/dev/serial/by-id/usb-Klipper_stm32f103xe_32FFD7054E4B373022621857-if00", "Klipper stm32f103xe"},
		{"/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0", "1a86 USB"},
		{"/dev/ttyACM0", "ttyACM0"},
	} {
		if got := Name(c.dev); got != c.want {
			t.Errorf("Name(%q) = %q, want %q", c.dev, got, c.want)
		}
	}
}

func TestSameVersion(t *testing.T) {
	host := "v0.13.0-777-g7bc4d0946"
	for _, c := range []struct {
		fw   string
		want bool
	}{
		// The version of the RAMBo on the test printer.
		{"v0.13.0-777-g7bc4d0946-prind-20260930_185514-10f1cdde21e6", true},
		{"v0.13.0-777-g7bc4d0946-dirty-20260930_185514-host", true},
		{"v0.12.0-123-gabcdef012-20240101_120000-host", false},
		{"", false},
		{"?", false},
	} {
		if got := SameVersion(c.fw, host); got != c.want {
			t.Errorf("SameVersion(%q) = %v, want %v", c.fw, got, c.want)
		}
	}
}
