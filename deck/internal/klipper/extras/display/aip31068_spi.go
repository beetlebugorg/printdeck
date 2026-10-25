// Support for YHCB2004 (20x4 text) LCD displays based on the AiP31068 controller.
//
// Ported to Go from klippy/extras/display/aip31068_spi.py of Klipper
// (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2018  Eric Callahan <arksine.code@gmail.com>
//	Copyright (C) 2021  Marc-Andre Denis <marcadenis@msn.com>
//	Copyright (C) 2024  Alexander Bazarov <oss@bazarov.dev>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package display

// This file is a modified version of hd44780_spi.go, introducing slightly
// different protocol as implemented in Marlin FW (based on
// https://github.com/red-scorp/LiquidCrystal_AIP31068 ).
// In addition, a hack is used to send 8 commands, each 9 bits, at once,
// allowing the transmission of a full 9 bytes.
// This helps avoid modifying the SW_SPI driver to handle non-8-bit data.

import (
	"bytes"
	"fmt"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
)

// Each command is 9 bits long:
//
//	1 bit for RS (Register Select) - 0 for command, 1 for data
//	8 bits for the command/data
const (
	cmndClr        = 1      // Clear display
	cmndHome       = 2      // Return home
	cmndEnteryMode = 1 << 2 // Entry mode set
	cmndDisplay    = 1 << 3 // Display on/off control
	cmndShift      = 1 << 4 // Cursor or display shift
	cmndFunction   = 1 << 5 // Function set
	cmndCGRAM      = 1 << 6 // Character Generator RAM
	cmndDDRAM      = 1 << 7 // Display Data RAM
	cmndWriteRAM   = 1 << 8 // Write to RAM

	flgEnteryModeInc    = 1 << 1 // Increment
	flgDisplayOn        = 1 << 2 // Display ON
	flgShiftRight       = 1 << 2 // Shift right
	flgFunctionTwoLines = 1 << 3 // 2-line display mode
	flgCGRAMMask        = 0b00111111
	flgDDRAMMask        = 0b01111111
)

var displayInitCmnds = []int{
	// CMND.CLR - no need as framebuffer will rewrite all
	cmndHome,                           // move cursor to home (0x00)
	cmndEnteryMode | flgEnteryModeInc,  // increment cursor and no shift
	cmndDisplay | flgDisplayOn,         // keep cursor and blinking off
	cmndShift | flgShiftRight,          // shift right cursor only
	cmndFunction | flgFunctionTwoLines, // 2-line display mode, 5x8 dots
}

// AIP31068SPI is the driver of the AiP31068 displays.
type AIP31068SPI struct {
	printer          *host.Printer
	spi              *bus.MCUSPI
	mcu              *mcu.MCU
	icons            map[string]Icon5x8
	lineLength       int
	textFramebuffers [2][]byte
	glyphFramebuffer []byte
	allFramebuffers  []framebuffer
}

func newAIP31068SPI(config *configfile.Wrapper) LCDChip {
	a := &AIP31068SPI{printer: host.Of(config), icons: map[string]Icon5x8{}}
	// spi config
	a.spi = bus.MCUSPIFromConfig(config, 0x00, "latch_pin", 100000, "", false)
	a.mcu = a.spi.MCU()
	a.lineLength = config.GetIntChoice("line_length", map[int]any{16: 16, 20: 20},
		configfile.Default(lineLengthDefault)).(int)
	// each controller's line is 2 lines on the display and hence twice
	// line length
	a.textFramebuffers = [2][]byte{bytes.Repeat([]byte(" "), 2*a.lineLength),
		bytes.Repeat([]byte(" "), 2*a.lineLength)}
	a.glyphFramebuffer = make([]byte, 64)
	a.allFramebuffers = []framebuffer{
		// Text framebuffers
		{a.textFramebuffers[0], bytes.Repeat([]byte("~"), 2*a.lineLength), cmndDDRAM | (flgDDRAMMask & 0x00)},
		{a.textFramebuffers[1], bytes.Repeat([]byte("~"), 2*a.lineLength), cmndDDRAM | (flgDDRAMMask & 0x40)},
		// Glyph framebuffer
		{a.glyphFramebuffer, bytes.Repeat([]byte("~"), 64), cmndCGRAM | (flgCGRAMMask & 0x00)},
	}
	return a
}

// aipEncode packs numbers of width bits into bytes, the first bits first.
func aipEncode(data []int, width int) []byte {
	var encoded []byte
	accumulator := 0 // To accumulate bits
	accBits := 0     // Count of bits in the accumulator
	for _, num := range data {
		// check that num will fit in width bits
		if num >= 1<<width {
			panic(fmt.Errorf("ValueError: Number %d does not fit in %d bits", num, width))
		}
		// Shift the current number into the accumulator from the right
		accumulator = (accumulator << width) | num
		accBits += width // Update the count of bits in the accumulator
		// While we have at least 8 bits, form a byte and append it
		for accBits >= 8 {
			accBits -= 8 // Decrease bit count by 8
			// Extract the 8 most significant bits to form a byte
			encoded = append(encoded, byte((accumulator>>accBits)&0xFF))
			// Remove msb 8 bits from the accumulator
			accumulator &= (1 << accBits) - 1
		}
	}
	// Handle any remaining bits by padding them on the right to byte
	if accBits > 0 {
		encoded = append(encoded, byte(accumulator<<(8-accBits)))
	}
	return encoded
}

func (a *AIP31068SPI) send(data []int, minclock int64) {
	// different commands have different processing time
	// to avoid timing violation pad with some fast command, e.g. ENTRY_MODE
	// that has execution time of 39us (for comparison CLR is 1.53ms)
	pad := cmndEnteryMode | flgEnteryModeInc
	for i := 0; i < len(data); i += 8 {
		// Take a slice of 8 numbers
		group := append([]int(nil), data[i:min(i+8, len(data))]...)
		// Pad the group if it has fewer than 8 elements
		for len(group) < 8 {
			group = append(group, pad)
		}
		a.spi.SPISend(aipEncode(group, 9), minclock, 0)
	}
}

// Flush sends the changed bytes of the framebuffers to the chip.
func (a *AIP31068SPI) Flush() {
	for _, fb := range a.allFramebuffers {
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
			a.send([]int{fb.fbID + chipPos}, 0)
			var data []int
			for _, b := range fb.newData[pos : pos+count] {
				data = append(data, cmndWriteRAM|int(b))
			}
			a.send(data, 0)
		}
		copy(fb.oldData, fb.newData)
	}
}

// Init sends the init commands and the framebuffers.
func (a *AIP31068SPI) Init() {
	curtime := a.printer.Reactor().Monotonic()
	printTime := a.mcu.EstimatedPrintTime(curtime)
	for i, cmds := range displayInitCmnds {
		minclock := a.mcu.PrintTimeToClock(printTime + float64(i)*.100)
		a.send([]int{cmds}, minclock)
	}
	a.Flush()
}

// WriteText writes text at a column and a row.
func (a *AIP31068SPI) WriteText(x, y int, data []byte) {
	if x+len(data) > a.lineLength {
		data = data[:max(0, a.lineLength-min(x, a.lineLength))]
	}
	pos := x + ((y&0x02)>>1)*a.lineLength
	fb := a.textFramebuffers[y&1]
	end := min(pos+len(data), len(fb))
	copy(fb[pos:end], data)
}

// SetGlyphs sets the 5x8 icons.
func (a *AIP31068SPI) SetGlyphs(glyphs map[string]Glyph) {
	for name, g := range glyphs {
		if g.Icon5x8 != nil {
			a.icons[name] = *g.Icon5x8
		}
	}
}

// WriteGlyph draws a glyph, and returns its width in columns (0 for an unknown glyph).
func (a *AIP31068SPI) WriteGlyph(x, y int, glyphName string) int {
	if icon, ok := a.icons[glyphName]; ok {
		a.WriteText(x, y, []byte{byte(icon.Slot)})
		for i := 0; i < 8 && i < len(icon.Data); i++ {
			a.glyphFramebuffer[icon.Slot*8+i] = byte(icon.Data[i])
		}
		return 1
	}
	if char, ok := hd44780TextGlyphs[glyphName]; ok {
		// Draw character
		a.WriteText(x, y, char)
		return 1
	}
	return 0
}

// WriteGraphics does nothing: this display supports only hardcoded or 8 user defined glyphs.
func (a *AIP31068SPI) WriteGraphics(x, y int, data []byte) {}

// Clear clears the text.
func (a *AIP31068SPI) Clear() {
	spaces := bytes.Repeat([]byte(" "), 2*a.lineLength)
	copy(a.textFramebuffers[0], spaces)
	copy(a.textFramebuffers[1], spaces)
}

// Dimensions are the columns and the rows.
func (a *AIP31068SPI) Dimensions() (int, int) { return a.lineLength, 4 }
