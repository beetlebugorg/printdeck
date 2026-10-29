package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/kit"
	"sync"
	"time"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
	"printdeck/internal/printer"
)

// The touch screen shows the status screen of the printer display: the same pixels as the
// display, at 3 times its size, as the dots of a dot-matrix display.

const lcdScale = 3

// lcdImages keeps the last pictures of the display, so that a frame that comes again (the fan
// turns in a few steps) is the same image for the browsers.
var lcdImages struct {
	sync.Mutex
	byKey map[lcdKey]*image.RGBA
	order []lcdKey
}

type lcdKey struct {
	bits         [64][16]byte
	scale        int
	gap, off, on color.RGBA
}

func lcdImage(bits [64][16]byte, scale int, gap, off, on color.RGBA) *image.RGBA {
	k := lcdKey{bits, scale, gap, off, on}
	lcdImages.Lock()
	defer lcdImages.Unlock()
	if img, ok := lcdImages.byKey[k]; ok {
		return img
	}
	if lcdImages.byKey == nil {
		lcdImages.byKey = map[lcdKey]*image.RGBA{}
	}
	img := lcdscreen.DotImage(bits, scale, gap, off, on)
	lcdImages.byKey[k] = img
	lcdImages.order = append(lcdImages.order, k)
	if len(lcdImages.order) > 24 {
		delete(lcdImages.byKey, lcdImages.order[0])
		lcdImages.order = lcdImages.order[1:]
	}
	return img
}

// LCDPicture is a picture of the bits of a printer display, as the touch screen shows it: dots
// at scale times the size of the display.
func LCDPicture(bits [64][16]byte, scale int) *image.RGBA {
	return lcdscreen.DotImage(bits, scale, glcd, glcdCell, glcdInk)
}

// drawLCDPanel draws the status screen in a display window, at the scale of r (r is 128 x 64 times
// the scale). A tap on the nozzle or the bed sets its temperature. Away from the print, the taps
// wait for its end.
func (a *App) drawLCDPanel(c *gfx.Canvas, st printer.State, s *lcdscreen.Status, r image.Rectangle) {
	scale := max(1, r.Dx()/128)
	c.RoundRect(r.Inset(-8), 8, edge)
	c.RoundRect(r.Inset(-6), 6, glcd)
	scr := &lcdscreen.Screen{Status: s}
	frame := int(a.Now().UnixMilli() / 250) // 4 frames a second, as on the display
	c.PixelImage(r, lcdImage(scr.Bits(frame), scale, glcd, glcdCell, glcdInk))
	if scr.Moves() {
		a.redrawSoon(250 * time.Millisecond)
	}
	if !st.Reachable {
		return
	}
	pr := a.Printer
	// The gauges are the columns of the picture: 0 to 42 pixels the nozzle, 43 to 85 the bed.
	col := func(x0, x1 int) image.Rectangle {
		return image.Rect(r.Min.X+x0*scale, r.Min.Y, r.Min.X+x1*scale, r.Min.Y+42*scale)
	}
	nozzle := func() { a.editTemp("Nozzle temperature", int(st.ToolTarget), 300, toolPresets, pr.SetToolTemp) }
	bed := func() { a.editTemp("Bed temperature", int(st.BedTarget), 120, bedPresets, pr.SetBedTemp) }
	if a.bar {
		nozzle = func() { a.Toast(waitText) }
		bed = nozzle
	}
	a.addHit(kit.Hit{R: col(0, 43), Tap: nozzle})
	a.addHit(kit.Hit{R: col(43, 86), Tap: bed})
	a.region("Nozzle", col(0, 43))
	a.region("Bed", col(43, 86))
	a.region("Fan", col(86, 128))
	a.region("Status", image.Rect(r.Min.X, r.Min.Y+42*scale, r.Max.X, r.Max.Y))
}

// statusPanel draws the status screen of the printer display at its place on a state screen.
func (a *App) statusPanel(c *gfx.Canvas, st printer.State) {
	a.statusPanelAt(c, st, a.stateFrame().lcd)
}

// StatusScreen is the status screen of the printer display as the touch screen shows it: the
// state, the message, the heaters, the fan, and the progress. For the apps (the Printdeck API).
// It is safe to call from any goroutine.
func (a *App) StatusScreen() *printer.LCDStatus {
	st := a.Printer.State()
	l1, l2 := StatusLines(st, a.Material, a.Now(), a.Clock24)
	s := a.lcdStatusScreen(st, l1, l2)
	s.Hint = "" // the apps have no knob
	return s
}

// statusPanelAt draws the status screen of the printer display in r: the same picture as the
// display of the printer.
func (a *App) statusPanelAt(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	l1, l2 := StatusLines(st, a.Material, a.Now(), a.Clock24)
	s := a.lcdStatusScreen(st, l1, l2)
	s.Hint = "" // the touch screen has no knob
	if st.FirmwareOld && !st.Busy() {
		s.Message = "Firmware is old"
	}
	a.region("Display", r)
	if !a.Remote {
		a.drawStatusBox(c, st, r)
		return
	}
	a.drawLCDPanel(c, st, s, r)
}

// The places of the Print screens under the status display.
const (
	belowPanel = 232 // the top of the first row under the display
	keyRowH    = 72  // the height of a row of keys
)

// redrawSoon draws a new frame after d, for a picture that moves. It asks once for each frame.
func (a *App) redrawSoon(d time.Duration) {
	if a.redrawPending.Swap(true) {
		return
	}
	time.AfterFunc(d, func() {
		a.redrawPending.Store(false)
		a.Redraw()
	})
}

// textImages keeps the last pictures of the character displays, as lcdImages.
var textImages struct {
	sync.Mutex
	byKey map[string]*image.RGBA
	order []string
}

// textImage is the picture of lines of text on a character display, in the colors of the theme.
func textImage(lines []string, cols, scale int) *image.RGBA {
	k := fmt.Sprint(lines, cols, scale, glcd, glcdCell, glcdInk)
	textImages.Lock()
	defer textImages.Unlock()
	if img, ok := textImages.byKey[k]; ok {
		return img
	}
	if textImages.byKey == nil {
		textImages.byKey = map[string]*image.RGBA{}
	}
	img := lcdscreen.DotLines(lines, cols, "6x10", scale, glcd, glcdCell, glcdInk)
	textImages.byKey[k] = img
	textImages.order = append(textImages.order, k)
	if len(textImages.order) > 24 {
		delete(textImages.byKey, textImages.order[0])
		textImages.order = textImages.order[1:]
	}
	return img
}
