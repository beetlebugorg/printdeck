package display

// Support for ST7920 (128x64 graphics) LCD displays (st7920.py).
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>

import (
	"bytes"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialqueue"
)

const (
	// Spec says 72us, but faster is possible in practice
	st7920CmdDelay  = .000020
	st7920SyncDelay = .000045
)

var backgroundPriorityClock = int64(serialqueue.BackgroundPriorityClock)

var textGlyphs = map[string][]byte{"right_arrow": {0x1a}}

func charGlyph(name string) []byte {
	if name == "degrees" {
		return VGAFont[0xf8][:]
	}
	return nil
}

type framebuffer struct {
	newData, oldData []byte
	fbID             int
}

type cachedGlyph struct {
	base string
	text []byte
}

// displayBase are the framebuffers of an ST7920 (DisplayBase). send sends commands or data.
type displayBase struct {
	textFramebuffer     []byte
	glyphFramebuffer    []byte
	graphicsFramebuffer [][]byte
	allFramebuffers     []framebuffer
	cachedGlyphs        map[string]cachedGlyph
	icons               map[string][2][]byte
	send                func(cmds []byte, isData, isExtended bool)
}

func newDisplayBase(send func(cmds []byte, isData, isExtended bool)) *displayBase {
	d := &displayBase{textFramebuffer: bytes.Repeat([]byte(" "), 64), glyphFramebuffer: make([]byte, 128),
		cachedGlyphs: map[string]cachedGlyph{}, icons: map[string][2][]byte{}, send: send}
	for i := 0; i < 32; i++ {
		d.graphicsFramebuffer = append(d.graphicsFramebuffer, make([]byte, 32))
	}
	d.allFramebuffers = []framebuffer{
		// Text framebuffer
		{d.textFramebuffer, bytes.Repeat([]byte("~"), 64), 0x80},
		// Glyph framebuffer
		{d.glyphFramebuffer, bytes.Repeat([]byte("~"), 128), 0x40},
	}
	// Graphics framebuffers
	for i := 0; i < 32; i++ {
		d.allFramebuffers = append(d.allFramebuffers, framebuffer{d.graphicsFramebuffer[i], bytes.Repeat([]byte("~"), 32), i})
	}
	return d
}

// Flush sends the changed bytes of the framebuffers to the chip.
func (d *displayBase) Flush() {
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
		for _, df := range diffs {
			pos, count := df[0], df[1]
			count += pos & 0x01
			count += count & 0x01
			pos = pos &^ 0x01
			chipPos := pos >> 1
			if fb.fbID < 0x40 {
				// Graphics framebuffer update
				d.send([]byte{byte(0x80 + fb.fbID), byte(0x80 + chipPos)}, false, true)
			} else {
				d.send([]byte{byte(fb.fbID + chipPos)}, false, false)
			}
			end := min(pos+count, len(fb.newData))
			d.send(append([]byte(nil), fb.newData[pos:end]...), true, false)
		}
		copy(fb.oldData, fb.newData)
	}
}

// Init sends the init commands and the framebuffers.
func (d *displayBase) Init() {
	cmds := []byte{
		0x24, // Enter extended mode
		0x40, // Clear vertical scroll address
		0x02, // Enable CGRAM access
		0x26, // Enable graphics
		0x22, // Leave extended mode
		0x02, // Home the display
		0x06, // Set positive update direction
		0x0c, // Enable display and hide cursor
	}
	d.send(cmds, false, false)
	d.Flush()
}

func (d *displayBase) cacheGlyph(glyphName, baseGlyphName string, glyphID int) {
	icon, ok1 := d.icons[glyphName]
	base, ok2 := d.icons[baseGlyphName]
	if !ok1 || !ok2 {
		return
	}
	for i := 0; i < len(icon[0]) && i < len(icon[1]) && i < len(base[0]) && i < len(base[1]); i++ {
		x1, x2 := icon[0][i]^base[0][i], icon[1][i]^base[1][i]
		pos := glyphID*32 + i*2
		d.glyphFramebuffer[pos], d.glyphFramebuffer[pos+1] = x1, x2
		d.allFramebuffers[1].oldData[pos], d.allFramebuffers[1].oldData[pos+1] = x1^1, x2^1
	}
	d.cachedGlyphs[glyphName] = cachedGlyph{baseGlyphName, []byte{0, byte(glyphID * 2)}}
}

// SetGlyphs sets the 16x16 icons, and the animated glyphs.
func (d *displayBase) SetGlyphs(glyphs map[string]Glyph) {
	for name, g := range glyphs {
		if g.Icon16x16 != nil {
			d.icons[name] = *g.Icon16x16
		}
	}
	// Setup animated glyphs
	d.cacheGlyph("fan2", "fan1", 0)
	d.cacheGlyph("bed_heat2", "bed_heat1", 1)
}

// WriteText writes text at a column and a row.
func (d *displayBase) WriteText(x, y int, data []byte) {
	if x+len(data) > 16 {
		data = data[:max(0, 16-min(x, 16))]
	}
	pos := []int{0, 32, 16, 48}[y] + x
	copy(d.textFramebuffer[pos:], data)
}

// WriteGraphics writes 16 rows of 8 pixels at a column and a row.
func (d *displayBase) WriteGraphics(x, y int, data []byte) {
	if x >= 16 || y >= 4 || len(data) != 16 {
		return
	}
	gfxFB := y * 16
	if gfxFB >= 32 {
		gfxFB -= 32
		x += 16
	}
	for i, bits := range data {
		d.graphicsFramebuffer[gfxFB+i][x] = bits
	}
}

// WriteGlyph draws a glyph, and returns its width in columns (0 for an unknown glyph).
func (d *displayBase) WriteGlyph(x, y int, glyphName string) int {
	if cg, ok := d.cachedGlyphs[glyphName]; ok && x&1 == 0 {
		// Render cached icon using character generator
		glyphName = cg.base
		d.WriteText(x, y, cg.text)
	}
	if icon, ok := d.icons[glyphName]; ok {
		// Draw icon in graphics mode
		d.WriteGraphics(x, y, icon[0])
		d.WriteGraphics(x+1, y, icon[1])
		return 2
	}
	if char, ok := textGlyphs[glyphName]; ok {
		// Draw character
		d.WriteText(x, y, char)
		return 1
	}
	if font := charGlyph(glyphName); font != nil {
		// Draw single width character
		d.WriteGraphics(x, y, font)
		return 1
	}
	return 0
}

// Clear clears the text and the graphics.
func (d *displayBase) Clear() {
	copy(d.textFramebuffer, bytes.Repeat([]byte(" "), 64))
	for _, g := range d.graphicsFramebuffer {
		clear(g)
	}
}

// Dimensions are the columns and the rows.
func (d *displayBase) Dimensions() (int, int) { return 16, 4 }

// ST7920 is the display driver of stock ST7920 displays.
type ST7920 struct {
	*displayBase
	mcu         *mcu.MCU
	pins        []string
	oid         int
	sendDataCmd *mcu.CommandWrapper
	sendCmdsCmd *mcu.CommandWrapper
	isExtended  bool
}

func newST7920(config *configfile.Wrapper) LCDChip {
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	s := &ST7920{}
	var chip pins.Chip
	for _, name := range []string{"cs", "sclk", "sid"} {
		p := ppins.LookupPin(config.Get(name+"_pin"), false, false, "")
		if chip != nil && p.Chip != chip {
			panic(&pins.Error{Msg: "st7920 all pins must be on same mcu"})
		}
		chip = p.Chip
		s.pins = append(s.pins, p.Pin)
	}
	s.mcu = chip.(*mcu.MCU)
	// prepare send functions
	s.oid = s.mcu.CreateOID()
	s.mcu.RegisterConfigCallback(s.buildConfig)
	// init display base
	s.displayBase = newDisplayBase(s.sendCmds)
	return s
}

func (s *ST7920) buildConfig() {
	// configure send functions
	s.mcu.AddConfigCmd(fmtf("config_st7920 oid=%d cs_pin=%s sclk_pin=%s sid_pin=%s sync_delay_ticks=%d cmd_delay_ticks=%d",
		s.oid, s.pins[0], s.pins[1], s.pins[2], s.mcu.SecondsToClock(st7920SyncDelay),
		s.mcu.SecondsToClock(st7920CmdDelay)), false, false)
	cmdQueue := s.mcu.AllocCommandQueue()
	s.sendCmdsCmd = s.mcu.LookupCommand("st7920_send_cmds oid=%c cmds=%*s", cmdQueue)
	s.sendDataCmd = s.mcu.LookupCommand("st7920_send_data oid=%c data=%*s", cmdQueue)
}

func (s *ST7920) sendCmds(cmds []byte, isData, isExtended bool) {
	cmdType := s.sendCmdsCmd
	if isData {
		cmdType = s.sendDataCmd
	} else if s.isExtended != isExtended {
		addCmd := byte(0x22)
		if isExtended {
			addCmd = 0x26
		}
		cmds = append([]byte{addCmd}, cmds...)
		s.isExtended = isExtended
	}
	cmdType.Send([]any{s.oid, cmds}, 0, backgroundPriorityClock)
}

// enableHelper toggles the en pin of an emulated ST7920 at the start.
type enableHelper struct{ enPin *bus.MCUBusDigitalOut }

func (e *enableHelper) init() {
	m := e.enPin.MCU()
	curtime := m.Printer().Reactor().Monotonic()
	printTime := m.EstimatedPrintTime(curtime)
	// Toggle enable pin
	e.enPin.UpdateDigitalOut(false, m.PrintTimeToClock(printTime+.100), 0)
	e.enPin.UpdateDigitalOut(true, m.PrintTimeToClock(printTime+.200), 0)
	// Force a delay to any subsequent commands on the command queue
	e.enPin.UpdateDigitalOut(true, m.PrintTimeToClock(printTime+.300), 0)
}

// EmulatedST7920 is the driver of displays that emulate the ST7920 in software, on a software
// SPI with an unused pin as the MISO pin.
type EmulatedST7920 struct {
	*displayBase
	spi        *bus.MCUSPI
	enHelper   *enableHelper
	enSet      bool
	isExtended bool
}

func newEmulatedST7920(config *configfile.Wrapper) LCDChip {
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	var chip pins.Chip
	var swPins []string
	for _, name := range []string{"miso", "mosi", "sclk"} {
		option := "spi_software_" + name + "_pin"
		p := ppins.LookupPin(config.Get(option), false, false, option)
		if chip != nil && p.Chip != chip {
			panic(&pins.Error{Msg: config.Name() + ": spi pins must be on same mcu"})
		}
		chip = p.Chip
		swPins = append(swPins, p.Pin)
	}
	speed := config.GetInt("spi_speed", configfile.Default(1000000), configfile.Min(100000))
	m := chip.(*mcu.MCU)
	e := &EmulatedST7920{spi: bus.NewMCUSPI(m, "", "", 0, speed, swPins, false)}
	e.enHelper = &enableHelper{bus.NewMCUBusDigitalOut(m, config.Get("en_pin"), e.spi.CommandQueue(), 0)}
	e.displayBase = newDisplayBase(e.sendCmds)
	return e
}

func (e *EmulatedST7920) sendCmds(cmds []byte, isData, isExtended bool) {
	// setup sync byte and check for exten mode switch
	syncByte := byte(0xfa)
	if !isData {
		syncByte = 0xf8
		if e.isExtended != isExtended {
			addCmd := byte(0x22)
			if isExtended {
				addCmd = 0x26
			}
			cmds = append([]byte{addCmd}, cmds...)
			e.isExtended = isExtended
		}
	}
	// copy data to ST7920 data format
	spiData := make([]byte, 2*len(cmds)+1)
	spiData[0] = syncByte
	for i, b := range cmds {
		spiData[1+2*i] = b & 0xF0
		spiData[2+2*i] = (b & 0x0F) << 4
	}
	// check if enable pin has been set
	if !e.enSet {
		e.enHelper.init()
		e.enSet = true
	}
	// send data
	e.spi.SPISend(spiData, 0, backgroundPriorityClock)
}
