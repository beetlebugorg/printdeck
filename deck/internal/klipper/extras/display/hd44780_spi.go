// Support for HD44780 (20x4 text) LCD displays on a shift register on SPI.
//
// Ported to Go from klippy/extras/display/hd44780_spi.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Eric Callahan <arksine.code@gmail.com>
//	Copyright (C) 2021  Marc-Andre Denis <marcadenis@msn.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package display

import (
	"bytes"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
)

// HD44780SPI is the driver of the HD44780 displays on SPI.
type HD44780SPI struct {
	printer             *host.Printer
	hd44780ProtocolInit bool
	spi                 *bus.MCUSPI
	mcu                 *mcu.MCU
	dataMask            byte
	commandMask         byte
	enableMask          byte
	icons               map[string]Icon5x8
	lineLength          int
	textFramebuffers    [2][]byte
	glyphFramebuffer    []byte
	allFramebuffers     []framebuffer
}

func newHD44780SPI(config *configfile.Wrapper) LCDChip {
	h := &HD44780SPI{printer: host.Of(config), icons: map[string]Icon5x8{}}
	h.hd44780ProtocolInit = config.GetBool("hd44780_protocol_init", configfile.Default(true))
	// spi config
	h.spi = bus.MCUSPIFromConfig(config, 0x00, "latch_pin", 100000, "", false)
	h.mcu = h.spi.MCU()
	h.dataMask = 1 << 1
	h.commandMask = 0
	h.enableMask = 1 << 3

	h.lineLength = config.GetIntChoice("line_length", map[int]any{16: 16, 20: 20},
		configfile.Default(lineLengthDefault)).(int)

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

func (h *HD44780SPI) send4Bits(cmd int, isData bool, minclock int64) {
	mask := h.commandMask
	if isData {
		mask = h.dataMask
	}
	h.spi.SPISend([]byte{byte(cmd&0xF0) | mask}, minclock, 0)
	h.spi.SPISend([]byte{byte(cmd&0xF0) | mask | h.enableMask}, minclock, 0)
	h.spi.SPISend([]byte{byte(cmd&0xF0) | mask}, minclock, 0)
}

func (h *HD44780SPI) sendAt(cmds []byte, isData bool, minclock int64) {
	for _, data := range cmds {
		h.send4Bits(int(data), isData, minclock)
		h.send4Bits(int(data)<<4, isData, minclock)
	}
}

func (h *HD44780SPI) send(cmds []byte, isData bool) { h.sendAt(cmds, isData, 0) }

// Flush sends the changed bytes of the framebuffers to the chip.
func (h *HD44780SPI) Flush() { textFlush(h.allFramebuffers, h.send) }

// Init sends the init commands and the framebuffers.
func (h *HD44780SPI) Init() {
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
		h.sendAt(cmds, false, minclock)
	}
	h.Flush()
}

// WriteText writes text at a column and a row.
func (h *HD44780SPI) WriteText(x, y int, data []byte) {
	if x+len(data) > h.lineLength {
		data = data[:max(0, h.lineLength-min(x, h.lineLength))]
	}
	pos := x + ((y&0x02)>>1)*h.lineLength
	fb := h.textFramebuffers[y&1]
	end := min(pos+len(data), len(fb))
	copy(fb[pos:end], data)
}

// SetGlyphs sets the 5x8 icons.
func (h *HD44780SPI) SetGlyphs(glyphs map[string]Glyph) {
	for name, g := range glyphs {
		if g.Icon5x8 != nil {
			h.icons[name] = *g.Icon5x8
		}
	}
}

// WriteGlyph draws a glyph, and returns its width in columns (0 for an unknown glyph).
func (h *HD44780SPI) WriteGlyph(x, y int, glyphName string) int {
	if icon, ok := h.icons[glyphName]; ok {
		h.WriteText(x, y, []byte{byte(icon.Slot)})
		for i := 0; i < 8 && i < len(icon.Data); i++ {
			h.glyphFramebuffer[icon.Slot*8+i] = byte(icon.Data[i])
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
func (h *HD44780SPI) WriteGraphics(x, y int, data []byte) {}

// Clear clears the text.
func (h *HD44780SPI) Clear() {
	spaces := bytes.Repeat([]byte(" "), 2*h.lineLength)
	copy(h.textFramebuffers[0], spaces)
	copy(h.textFramebuffers[1], spaces)
}

// Dimensions are the columns and the rows.
func (h *HD44780SPI) Dimensions() (int, int) { return h.lineLength, 4 }
