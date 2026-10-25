package tmc_uart

import (
	"slices"
	"testing"
)

// The messages of the real Klipper code (_encode_read, _encode_write and _decode_read of
// tmc_uart.py) for some registers and values.
func TestUartMessages(t *testing.T) {
	cases := []struct {
		addr, reg int
		val       int64
		read, wr  []byte
		resp      []byte
	}{
		{0, 108, 268435539, []byte{234, 3, 136, 173, 198}, []byte{234, 3, 136, 61, 136, 0, 2, 104, 170, 178},
			[]byte{10, 250, 143, 45, 136, 0, 2, 104, 170, 168}},
		{3, 16, 532255, []byte{234, 27, 8, 34, 237}, []byte{234, 27, 8, 50, 128, 16, 250, 232, 163, 229},
			[]byte{10, 250, 15, 34, 128, 16, 250, 232, 163, 178}},
		{1, 111, 4294967295, []byte{234, 11, 232, 173, 186}, []byte{234, 11, 232, 189, 255, 254, 251, 239, 63, 224},
			[]byte{10, 250, 239, 173, 255, 254, 251, 239, 63, 140}},
		{2, 0, 0, []byte{234, 19, 8, 32, 170}, []byte{234, 19, 8, 48, 128, 0, 2, 8, 32, 227},
			[]byte{10, 250, 15, 32, 128, 0, 2, 8, 32, 194}},
	}
	for _, c := range cases {
		if got := encodeRead(0xf5, byte(c.addr), byte(c.reg)); !slices.Equal(got, c.read) {
			t.Errorf("read %d %d: %v, want %v", c.addr, c.reg, got, c.read)
		}
		if got := encodeWrite(0xf5, byte(c.addr), byte(c.reg|0x80), c.val); !slices.Equal(got, c.wr) {
			t.Errorf("write %d %d: %v, want %v", c.addr, c.reg, got, c.wr)
		}
		if v, ok := decodeRead(byte(c.reg), c.resp); !ok || v != c.val {
			t.Errorf("decode %d: %d %v, want %d", c.reg, v, ok, c.val)
		}
		bad := slices.Clone(c.resp)
		bad[9] ^= 0x10
		if _, ok := decodeRead(byte(c.reg), bad); ok {
			t.Errorf("decode %d of a bad message is ok", c.reg)
		}
	}
}
