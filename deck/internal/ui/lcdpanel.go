package ui

import (
	"fmt"
	"image"
	"image/color"
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
	gap, off, on color.RGBA
}

func lcdImage(bits [64][16]byte, gap, off, on color.RGBA) *image.RGBA {
	k := lcdKey{bits, gap, off, on}
	lcdImages.Lock()
	defer lcdImages.Unlock()
	if img, ok := lcdImages.byKey[k]; ok {
		return img
	}
	if lcdImages.byKey == nil {
		lcdImages.byKey = map[lcdKey]*image.RGBA{}
	}
	img := lcdscreen.DotImage(bits, lcdScale, gap, off, on)
	lcdImages.byKey[k] = img
	lcdImages.order = append(lcdImages.order, k)
	if len(lcdImages.order) > 24 {
		delete(lcdImages.byKey, lcdImages.order[0])
		lcdImages.order = lcdImages.order[1:]
	}
	return img
}

// lcdPanelRect is the place of the display on the idle screen.
var lcdPanelRect = image.Rect((W-128*lcdScale)/2, 20, (W+128*lcdScale)/2, 20+64*lcdScale)

// drawLCDPanel draws the status screen in a display window. A tap on the nozzle or the bed sets
// its temperature.
func (a *App) drawLCDPanel(c *gfx.Canvas, st printer.State, s *lcdscreen.Status) {
	r := lcdPanelRect
	c.RoundRect(r.Inset(-8), 8, edge)
	c.RoundRect(r.Inset(-6), 6, glcd)
	scr := &lcdscreen.Screen{Status: s}
	frame := int(a.Now().UnixMilli() / 250) // 4 frames a second, as on the display
	c.PixelImage(r, lcdImage(scr.Bits(frame), glcd, glcdCell, glcdInk))
	if scr.Moves() {
		a.redrawSoon(250 * time.Millisecond)
	}
	if !st.Reachable {
		return
	}
	pr := a.Printer
	// The gauges are the columns of the picture: 0 to 42 pixels the nozzle, 43 to 85 the bed.
	col := func(x0, x1 int) image.Rectangle {
		return image.Rect(r.Min.X+x0*lcdScale, r.Min.Y, r.Min.X+x1*lcdScale, r.Min.Y+42*lcdScale)
	}
	a.addHit(hit{r: col(0, 43), tap: func() { a.editTemp("Nozzle temperature", int(st.ToolTarget), 300, toolPresets, pr.SetToolTemp) }})
	a.addHit(hit{r: col(43, 86), tap: func() { a.editTemp("Bed temperature", int(st.BedTarget), 120, bedPresets, pr.SetBedTemp) }})
}

// statusPanel draws the status screen of the printer display at the top of a Print screen: the
// same picture as the display of the printer.
func (a *App) statusPanel(c *gfx.Canvas, st printer.State) {
	l1, l2 := StatusLines(st, a.Material, a.Now(), a.Clock24)
	s := a.lcdStatusScreen(st, l1, l2)
	s.Hint = "" // the touch screen has no knob
	if st.FirmwareOld && !st.Busy() {
		s.Message = "Firmware is old"
	}
	a.drawLCDPanel(c, st, s)
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
