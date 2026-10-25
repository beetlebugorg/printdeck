// Support for UC1701 (and similar) 128x64 graphics LCD displays.
//
// Ported to Go from klippy/extras/display/uc1701.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Eric Callahan  <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package display

import (
	"bytes"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/serialqueue"
)

var uc1701TextGlyphs = map[string][]byte{"right_arrow": {0x1a}, "degrees": {0xf8}}

// swizzleBits converts 16 rows of 8 pixels from "rows of pixels" format to "columns of pixels":
// the top 8 columns and the bottom 8 columns.
func swizzleBits(data []byte) ([]byte, []byte) {
	var top, bot uint64
	for row := 0; row < 8; row++ {
		spaced := (uint64(data[row]) * 0x8040201008040201) & 0x8080808080808080
		top |= spaced >> (7 - row)
		spaced = (uint64(data[row+8]) * 0x8040201008040201) & 0x8080808080808080
		bot |= spaced >> (7 - row)
	}
	bitsTop := make([]byte, 8)
	bitsBot := make([]byte, 8)
	for i, s := 0, 0; s < 64; i, s = i+1, s+8 {
		bitsTop[i] = byte(top >> s)
		bitsBot[i] = byte(bot >> s)
	}
	return bitsTop, bitsBot
}

// sender sends commands or data to a display.
type sender interface {
	send(cmds []byte, isData bool)
}

// pageDisplay is the framebuffer of a display with 8 pages of columns (DisplayBase of
// uc1701.py).
type pageDisplay struct {
	io              sender
	columns         int
	xOffset         int
	vram            [][]byte
	allFramebuffers []framebuffer
	font            [256][2][]byte
	icons           map[string][2][]byte
}

func newPageDisplay(io sender, columns, xOffset int) *pageDisplay {
	d := &pageDisplay{io: io, columns: columns, xOffset: xOffset, icons: map[string][2][]byte{}}
	for i := 0; i < 8; i++ {
		d.vram = append(d.vram, make([]byte, columns))
		d.allFramebuffers = append(d.allFramebuffers,
			framebuffer{d.vram[i], bytes.Repeat([]byte("~"), columns), i})
	}
	// Cache fonts and icons in display byte order
	for c := range VGAFont {
		top, bot := swizzleBits(VGAFont[c][:])
		d.font[c] = [2][]byte{top, bot}
	}
	return d
}

// Flush sends the changed bytes of the framebuffers to the chip.
func (d *pageDisplay) Flush() {
	for _, fb := range d.allFramebuffers {
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
			if pos+5 >= nextpos && nextcount < 16 {
				diffs[i][1] = nextcount + (nextpos - pos)
				diffs = append(diffs[:i+1], diffs[i+2:]...)
			}
		}
		// Transmit changes
		page := fb.fbID
		for _, df := range diffs {
			colPos, count := df[0], df[1]
			// Set Position registers
			ra := byte(0xb0 | (page & 0x0F))
			caMSB := byte(0x10 | ((colPos >> 4) & 0x0F))
			caLSB := byte(colPos & 0x0F)
			d.io.send([]byte{ra, caMSB, caLSB}, false)
			// Send Data
			d.io.send(append([]byte(nil), fb.newData[colPos:colPos+count]...), true)
		}
		copy(fb.oldData, fb.newData)
	}
}

// SetGlyphs sets the 16x16 icons.
func (d *pageDisplay) SetGlyphs(glyphs map[string]Glyph) {
	for name, g := range glyphs {
		if g.Icon16x16 != nil {
			top1, bot1 := swizzleBits(g.Icon16x16[0])
			top2, bot2 := swizzleBits(g.Icon16x16[1])
			d.icons[name] = [2][]byte{append(top1, top2...), append(bot1, bot2...)}
		}
	}
}

// WriteText writes text at a column and a row.
func (d *pageDisplay) WriteText(x, y int, data []byte) {
	if x+len(data) > 16 {
		data = data[:max(0, 16-min(x, 16))]
	}
	pixX := x * 8
	pixX += d.xOffset
	pageTop := d.vram[y*2]
	pageBot := d.vram[y*2+1]
	for _, c := range data {
		bits := d.font[c]
		copy(pageTop[pixX:pixX+8], bits[0])
		copy(pageBot[pixX:pixX+8], bits[1])
		pixX += 8
	}
}

// WriteGraphics writes 16 rows of 8 pixels at a column and a row.
func (d *pageDisplay) WriteGraphics(x, y int, data []byte) {
	if x >= 16 || y >= 4 || len(data) != 16 {
		return
	}
	bitsTop, bitsBot := swizzleBits(data)
	pixX := x * 8
	pixX += d.xOffset
	pageTop := d.vram[y*2]
	pageBot := d.vram[y*2+1]
	for i := 0; i < 8; i++ {
		pageTop[pixX+i] ^= bitsTop[i]
		pageBot[pixX+i] ^= bitsBot[i]
	}
}

// WriteGlyph draws a glyph, and returns its width in columns (0 for an unknown glyph).
func (d *pageDisplay) WriteGlyph(x, y int, glyphName string) int {
	if icon, ok := d.icons[glyphName]; ok && x < 15 {
		// Draw icon in graphics mode
		pixX := x * 8
		pixX += d.xOffset
		pageIdx := y * 2
		copy(d.vram[pageIdx][pixX:pixX+16], icon[0])
		copy(d.vram[pageIdx+1][pixX:pixX+16], icon[1])
		return 2
	}
	if char, ok := uc1701TextGlyphs[glyphName]; ok {
		// Draw character
		d.WriteText(x, y, char)
		return 1
	}
	return 0
}

// Clear clears the framebuffers.
func (d *pageDisplay) Clear() {
	for _, page := range d.vram {
		clear(page)
	}
}

// Dimensions are the columns and the rows.
func (d *pageDisplay) Dimensions() (int, int) { return 16, 4 }

// spi4wire is the IO of a "4 wire" spi bus (spi bus with an extra data/control line).
type spi4wire struct {
	spi   *bus.MCUSPI
	mcuDC *bus.MCUBusDigitalOut
}

func newSPI4wire(config *configfile.Wrapper, dataPinName string) *spi4wire {
	s := &spi4wire{spi: bus.MCUSPIFromConfig(config, 0, "cs_pin", 10000000, "", false)}
	dcPin := config.Get(dataPinName)
	s.mcuDC = bus.NewMCUBusDigitalOut(s.spi.MCU(), dcPin, s.spi.CommandQueue(), 0)
	return s
}

func (s *spi4wire) send(cmds []byte, isData bool) {
	s.mcuDC.UpdateDigitalOut(isData, 0, backgroundPriorityClock)
	s.spi.SPISend(cmds, 0, backgroundPriorityClock)
}

// i2cIO is the IO of an i2c bus.
type i2cIO struct{ i2c *bus.MCUI2C }

func newI2CIO(config *configfile.Wrapper, defaultAddr int) *i2cIO {
	return &i2cIO{bus.MCUI2CFromConfig(config, defaultAddr, 400000, true)}
}

func (c *i2cIO) send(cmds []byte, isData bool) {
	hdr := byte(0x00)
	if isData {
		hdr = 0x40
	}
	c.i2c.I2CWriteNoack(append([]byte{hdr}, cmds...), 0, backgroundPriorityClock)
}

type ioBus interface {
	MCU() *mcu.MCU
	CommandQueue() *serialqueue.CommandQueue
}

// resetHelper toggles a reset pin on startup.
type resetHelper struct{ mcuReset *bus.MCUBusDigitalOut }

func newResetHelper(pinDesc string, hasPin bool, ioBus ioBus) *resetHelper {
	r := &resetHelper{}
	if !hasPin {
		return r
	}
	r.mcuReset = bus.NewMCUBusDigitalOut(ioBus.MCU(), pinDesc, ioBus.CommandQueue(), 0)
	return r
}

func (r *resetHelper) init() {
	if r.mcuReset == nil {
		return
	}
	m := r.mcuReset.MCU()
	curtime := m.Printer().Reactor().Monotonic()
	printTime := m.EstimatedPrintTime(curtime)
	// Toggle reset
	minclock := m.PrintTimeToClock(printTime + .100)
	r.mcuReset.UpdateDigitalOut(false, minclock, 0)
	minclock = m.PrintTimeToClock(printTime + .200)
	r.mcuReset.UpdateDigitalOut(true, minclock, 0)
	// Force a delay to any subsequent commands on the command queue
	minclock = m.PrintTimeToClock(printTime + .300)
	r.mcuReset.UpdateDigitalOut(true, minclock, 0)
}

// UC1701 is a "4-wire" SPI display device.
type UC1701 struct {
	*pageDisplay
	io       *spi4wire
	contrast int
	reset    *resetHelper
}

func newUC1701(config *configfile.Wrapper) LCDChip {
	io := newSPI4wire(config, "a0_pin")
	u := &UC1701{io: io, pageDisplay: newPageDisplay(io, 128, 0)}
	u.contrast = config.GetInt("contrast", configfile.Default(40), configfile.Min(0), configfile.Max(63))
	rst, ok := config.GetOptional("rst_pin", configfile.Default(nil))
	u.reset = newResetHelper(rst, ok, io.spi)
	return u
}

// Init sends the init commands and the framebuffers.
func (u *UC1701) Init() {
	u.reset.init()
	initCmds := []byte{0xE2, // System reset
		0x40,             // Set display to start at line 0
		0xA0,             // Set SEG direction
		0xC8,             // Set COM Direction
		0xA2,             // Set Bias = 1/9
		0x2C,             // Boost ON
		0x2E,             // Voltage regulator on
		0x2F,             // Voltage follower on
		0xF8,             // Set booster ratio
		0x00,             // Booster ratio value (4x)
		0x23,             // Set resistor ratio (3)
		0x81,             // Set Electronic Volume
		byte(u.contrast), // Electronic Volume value
		0xAC,             // Set static indicator off
		0x00,             // NOP
		0xA6,             // Disable Inverse
		0xAF}             // Set display enable
	u.io.send(initCmds, false)
	u.io.send([]byte{0xA5}, false) // display all
	u.io.send([]byte{0xA4}, false) // normal display
	u.Flush()
}

// SSD1306 is a display on i2c or on a "4-wire" spi bus.
type SSD1306 struct {
	*pageDisplay
	io       sender
	reset    *resetHelper
	contrast int
	vcomh    int
	invert   bool
}

func newSSD1306Columns(config *configfile.Wrapper, columns, xOffset int) *SSD1306 {
	s := &SSD1306{}
	var bus ioBus
	if _, ok := config.GetOptional("cs_pin", configfile.Default(nil)); !ok {
		io := newI2CIO(config, 60)
		s.io, bus = io, io.i2c
	} else {
		io := newSPI4wire(config, "dc_pin")
		s.io, bus = io, io.spi
	}
	rst, ok := config.GetOptional("reset_pin", configfile.Default(nil))
	s.reset = newResetHelper(rst, ok, bus)
	s.pageDisplay = newPageDisplay(s.io, columns, xOffset)
	s.contrast = config.GetInt("contrast", configfile.Default(239), configfile.Min(0), configfile.Max(255))
	s.vcomh = config.GetInt("vcomh", configfile.Default(0), configfile.Min(0), configfile.Max(63))
	s.invert = config.GetBool("invert", configfile.Default(false))
	return s
}

func newSSD1306(config *configfile.Wrapper) LCDChip { return newSSD1306Columns(config, 128, 0) }

// Init sends the init commands and the framebuffers.
func (s *SSD1306) Init() {
	s.reset.init()
	invert := byte(0xA6)
	if s.invert {
		invert = 0xA7
	}
	initCmds := []byte{
		0xAE,       // Display off
		0xD5, 0x80, // Set oscillator frequency
		0xA8, 0x3f, // Set multiplex ratio
		0xD3, 0x00, // Set display offset
		0x40,       // Set display start line
		0x8D, 0x14, // Charge pump setting
		0x20, 0x02, // Set Memory addressing mode
		0xA1,       // Set Segment re-map
		0xC8,       // Set COM output scan direction
		0xDA, 0x12, // Set COM pins hardware configuration
		0x81, byte(s.contrast), // Set contrast control
		0xD9, 0xA1, // Set pre-charge period
		0xDB, byte(s.vcomh), // Set VCOMH deselect level
		0x2E,   // Deactivate scroll
		0xA4,   // Output ram to display
		invert, // Set normal/invert
		0xAF,   // Display on
	}
	s.io.send(initCmds, false)
	s.Flush()
}

// newSH1106 is the SH1106: SSD1306 compatible with up to 132 columns.
func newSH1106(config *configfile.Wrapper) LCDChip {
	xOffset := config.GetInt("x_offset", configfile.Default(0), configfile.Min(0), configfile.Max(3))
	return newSSD1306Columns(config, 132, xOffset)
}
