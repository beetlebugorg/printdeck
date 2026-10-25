// Support for HD44780 (20x4 text) LCD displays.
//
// Ported to Go from klippy/extras/display/hd44780.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package display

import (
	"bytes"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

const (
	lineLengthDefault = 20
	hd44780Delay      = .000040
)

var hd44780TextGlyphs = map[string][]byte{"right_arrow": {0x7e}}

// textFlush sends the changed bytes of text framebuffers, with the batching of the HD44780 and
// its SPI and I2C versions: changes 4 bytes apart or closer go in one send.
func textFlush(fbs []framebuffer, send func(cmds []byte, isData bool)) {
	for _, fb := range fbs {
		if bytes.Equal(fb.newData, fb.oldData) {
			continue
		}
		// Find the position of all changed bytes in this framebuffer
		var diffs [][2]int
		for i := range fb.newData {
			if fb.newData[i] != fb.oldData[i] {
				diffs = append(diffs, [2]int{i, 1})
			}
		}
		// Batch together changes that are close to each other
		for i := len(diffs) - 2; i >= 0; i-- {
			pos := diffs[i][0]
			nextpos, nextcount := diffs[i+1][0], diffs[i+1][1]
			if pos+4 >= nextpos && nextcount < 16 {
				diffs[i][1] = nextcount + (nextpos - pos)
				diffs = append(diffs[:i+1], diffs[i+2:]...)
			}
		}
		// Transmit changes
		for _, df := range diffs {
			pos, count := df[0], df[1]
			chipPos := pos
			send([]byte{byte(fb.fbID + chipPos)}, false)
			send(append([]byte(nil), fb.newData[pos:pos+count]...), true)
		}
		copy(fb.oldData, fb.newData)
	}
}

// HD44780 is the driver of the HD44780 displays on 4 data pins.
type HD44780 struct {
	printer             *host.Printer
	pins                []string
	hd44780ProtocolInit bool
	lineLength          int
	mcu                 *mcu.MCU
	oid                 int
	sendDataCmd         *mcu.CommandWrapper
	sendCmdsCmd         *mcu.CommandWrapper
	icons               map[string]Icon5x8
	textFramebuffers    [2][]byte
	glyphFramebuffer    []byte
	allFramebuffers     []framebuffer
}

func newHD44780(config *configfile.Wrapper) LCDChip {
	h := &HD44780{printer: host.Of(config), icons: map[string]Icon5x8{}}
	// pin config
	ppins := h.printer.LookupObject("pins").(*pins.Pins)
	var params []*pins.Params
	for _, name := range []string{"rs", "e", "d4", "d5", "d6", "d7"} {
		params = append(params, ppins.LookupPin(config.Get(name+"_pin"), false, false, ""))
	}
	h.hd44780ProtocolInit = config.GetBool("hd44780_protocol_init", configfile.Default(true))
	h.lineLength = config.GetIntChoice("line_length", map[int]any{16: 16, 20: 20},
		configfile.Default(lineLengthDefault)).(int)
	var chip pins.Chip
	for _, p := range params {
		if chip != nil && p.Chip != chip {
			panic(&pins.Error{Msg: "hd44780 all pins must be on same mcu"})
		}
		chip = p.Chip
	}
	for _, p := range params {
		h.pins = append(h.pins, p.Pin)
	}
	h.mcu = chip.(*mcu.MCU)
	h.oid = h.mcu.CreateOID()
	h.mcu.RegisterConfigCallback(h.buildConfig)
	// framebuffers
	h.textFramebuffers = [2][]byte{bytes.Repeat([]byte(" "), 2*h.lineLength),
		bytes.Repeat([]byte(" "), 2*h.lineLength)}
	h.glyphFramebuffer = make([]byte, 64)
	h.allFramebuffers = []framebuffer{
		// Text framebuffers
		{h.textFramebuffers[0], bytes.Repeat([]byte("~"), 2*h.lineLength), 0x80},
		{h.textFramebuffers[1], bytes.Repeat([]byte("~"), 2*h.lineLength), 0xc0},
		// Glyph framebuffer
		{h.glyphFramebuffer, bytes.Repeat([]byte("~"), 64), 0x40},
	}
	return h
}

func (h *HD44780) buildConfig() {
	h.mcu.AddConfigCmd(fmtf("config_hd44780 oid=%d rs_pin=%s e_pin=%s"+
		" d4_pin=%s d5_pin=%s d6_pin=%s d7_pin=%s delay_ticks=%d",
		h.oid, h.pins[0], h.pins[1], h.pins[2], h.pins[3], h.pins[4], h.pins[5],
		h.mcu.SecondsToClock(hd44780Delay)), false, false)
	cmdQueue := h.mcu.AllocCommandQueue()
	h.sendCmdsCmd = h.mcu.LookupCommand("hd44780_send_cmds oid=%c cmds=%*s", cmdQueue)
	h.sendDataCmd = h.mcu.LookupCommand("hd44780_send_data oid=%c data=%*s", cmdQueue)
}

func (h *HD44780) send(cmds []byte, isData bool) {
	cmdType := h.sendCmdsCmd
	if isData {
		cmdType = h.sendDataCmd
	}
	cmdType.Send([]any{h.oid, cmds}, 0, backgroundPriorityClock)
}

// Flush sends the changed bytes of the framebuffers to the chip.
func (h *HD44780) Flush() { textFlush(h.allFramebuffers, h.send) }

// Init sends the init commands and the framebuffers.
func (h *HD44780) Init() {
	curtime := h.printer.Reactor().Monotonic()
	printTime := h.mcu.EstimatedPrintTime(curtime)
	// Program 4bit / 2-line mode and then issue 0x02 "Home" command
	var init [][]byte
	if h.hd44780ProtocolInit {
		init = [][]byte{{0x33}, {0x33}, {0x32}, {0x28, 0x28, 0x02}}
	} else {
		init = [][]byte{{0x02}}
	}
	// Reset (set positive direction ; enable display and hide cursor)
	init = append(init, []byte{0x06, 0x0c})
	for i, cmds := range init {
		minclock := h.mcu.PrintTimeToClock(printTime + float64(i)*.100)
		h.sendCmdsCmd.Send([]any{h.oid, cmds}, minclock, 0)
	}
	h.Flush()
}

// WriteText writes text at a column and a row.
func (h *HD44780) WriteText(x, y int, data []byte) {
	if x+len(data) > h.lineLength {
		data = data[:max(0, h.lineLength-min(x, h.lineLength))]
	}
	pos := x + ((y&0x02)>>1)*h.lineLength
	fb := h.textFramebuffers[y&1]
	end := min(pos+len(data), len(fb))
	copy(fb[pos:end], data)
}

// SetGlyphs sets the 5x8 icons.
func (h *HD44780) SetGlyphs(glyphs map[string]Glyph) {
	for name, g := range glyphs {
		if g.Icon5x8 != nil {
			h.icons[name] = *g.Icon5x8
		}
	}
}

// WriteGlyph draws a glyph, and returns its width in columns (0 for an unknown glyph).
func (h *HD44780) WriteGlyph(x, y int, glyphName string) int {
	if icon, ok := h.icons[glyphName]; ok {
		slot, bits := icon.Slot, icon.Data
		h.WriteText(x, y, []byte{byte(slot)})
		for i := 0; i < 8 && i < len(bits); i++ {
			h.glyphFramebuffer[slot*8+i] = byte(bits[i])
		}
		return 1
	}
	if char, ok := hd44780TextGlyphs[glyphName]; ok {
		// Draw character
		h.WriteText(x, y, char)
		return 1
	}
	return 0
}

// WriteGraphics does nothing: the HD44780 shows text only.
func (h *HD44780) WriteGraphics(x, y int, data []byte) {}

// Clear clears the text.
func (h *HD44780) Clear() {
	spaces := bytes.Repeat([]byte(" "), 2*h.lineLength)
	copy(h.textFramebuffers[0], spaces)
	copy(h.textFramebuffers[1], spaces)
}

// Dimensions are the columns and the rows.
func (h *HD44780) Dimensions() (int, int) { return h.lineLength, 4 }
