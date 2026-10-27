package simmcu

import (
	"encoding/binary"
	"strings"
	"sync"
)

// The SD card of the simulated board: an SDHC card in SPI mode on the chip select pin of
// [printdeck_sdcard], as in the slot of the display of the TAZ Workhorse. The card reads blocks of
// an image from SetCard. It does not write.

var (
	cardMu   sync.Mutex
	cardFunc func() []byte // the image of the card, or nil for an empty slot
)

// SetCard puts a card in the slot of the simulated board. image gives the content of the card each
// time the host starts the card, so a change of the files shows at the next start. nil empties the
// slot.
func SetCard(image func() []byte) {
	cardMu.Lock()
	cardFunc = image
	cardMu.Unlock()
}

// sdCard is the state of the card in SPI mode.
type sdCard struct {
	img  []byte
	cmd  []byte // the bytes of the command that comes in
	out  []byte // the bytes that the card sends next
	idle bool
	app  bool // the last command was CMD55: the next one is an application command
}

// transfer clocks the bytes of the host into the card, and returns the bytes that the card sends
// at the same time.
func (c *sdCard) transfer(in []byte) []byte {
	out := make([]byte, len(in))
	for i, b := range in {
		out[i] = 0xff
		if len(c.out) > 0 {
			out[i], c.out = c.out[0], c.out[1:]
		}
		if len(c.cmd) == 0 && b&0xc0 != 0x40 {
			continue
		}
		c.cmd = append(c.cmd, b)
		if len(c.cmd) == 6 {
			c.command(c.cmd[0]&0x3f, binary.BigEndian.Uint32(c.cmd[1:5]))
			c.cmd = nil
		}
	}
	return out
}

// command sets the reply to a command. The reply starts after one byte, as on a card.
func (c *sdCard) command(cmd byte, arg uint32) {
	r1 := func(extra ...byte) {
		r := byte(0)
		if c.idle {
			r = 0x01
		}
		c.out = append([]byte{0xff, r}, extra...)
	}
	app := c.app
	c.app = false
	switch {
	case cmd == 0:
		cardMu.Lock()
		f := cardFunc
		cardMu.Unlock()
		if f == nil {
			c.img = nil
			c.out = nil // no card: the line stays high
			return
		}
		c.img = f()
		c.idle = true
		r1()
	case c.img == nil:
		c.out = nil
	case cmd == 8:
		r1(0, 0, byte(arg>>8)&0x0f, byte(arg))
	case cmd == 55:
		c.app = true
		r1()
	case cmd == 41 && app:
		c.idle = false
		r1()
	case cmd == 58:
		r1(0xc0, 0xff, 0x80, 0x00) // powered up, SDHC: block addresses
	case cmd == 16 || cmd == 59 || cmd == 12:
		r1()
	case cmd == 17:
		at := int64(arg) * 512
		if c.idle || at+512 > int64(len(c.img)) {
			c.out = []byte{0xff, 0x40} // an address error
			return
		}
		r1(0xff, 0xff, 0xfe)
		c.out = append(c.out, c.img[at:at+512]...)
		c.out = append(c.out, 0, 0) // the CRC: SPI mode does not check it
	default:
		r := byte(0x04) // an illegal command: the card does not write
		if c.idle {
			r |= 0x01
		}
		c.out = []byte{0xff, r}
	}
}

// sdPin is the chip select pin of the card, from the config (Configure), or "".
func sdPin() string {
	m := theMachine()
	if m == nil {
		return ""
	}
	return m.sdPin
}

// cleanPin is a pin of the config with no pull up, invert or comment.
func cleanPin(v string) string {
	return strings.TrimLeft(strings.TrimSpace(strings.SplitN(v, "#", 2)[0]), "^~!")
}
