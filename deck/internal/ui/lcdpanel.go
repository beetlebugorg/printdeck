package ui

import (
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
var lcdPanelRect = image.Rect((W-128*lcdScale)/2, 66, (W+128*lcdScale)/2, 66+64*lcdScale)

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
