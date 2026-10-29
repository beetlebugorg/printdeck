package ui

import (
	"fmt"
	"image"
	"sync"
	"time"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
	"printdeck/internal/printer"
)

// The screens show the status panel (statusbox.go) at the top, as wide as the printer display at
// lcdScale times its size.

const lcdScale = 3

// LCDPicture is a picture of the bits of a printer display: dots at scale times the size of the
// display.
func LCDPicture(bits [64][16]byte, scale int) *image.RGBA {
	return lcdscreen.DotImage(bits, scale, glcd, glcdCell, glcdInk)
}

// statusPanel draws the status panel at its place on a state screen.
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

// statusPanelAt draws the status panel in r.
func (a *App) statusPanelAt(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	a.region("Display", r)
	a.drawStatusBox(c, st, r)
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
