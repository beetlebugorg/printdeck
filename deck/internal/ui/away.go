package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// Away from the print: while a print runs, Home on the screen of the print shows the other
// screens. They are read only for the printer: a key that moves, heats or changes the printer is
// dim, and a tap says to wait (printerKey). The print bar at the bottom of each screen shows the
// print, and a tap on it goes back. A new print, a pause, and a time with no touch also go back.

const (
	// awayIdle is the time with no touch after which the screen of the print comes back.
	awayIdle = 2 * time.Minute
	// barH is the height of the print bar.
	barH = 64
	// waitText is the answer of a key that waits for the end of the print.
	waitText = "Wait until the print ends"
)

// leavePrint leaves the screen of the print for the home screen. The print goes on.
func (a *App) leavePrint(st printer.State) {
	a.away, a.awayFile, a.pages, a.tune = true, st.FilePath, nil, false
	a.Redraw()
}

// returnToPrint closes the other screens, and shows the screen of the print.
func (a *App) returnToPrint() {
	a.away, a.pages, a.tune = false, nil, false
	a.Redraw()
}

// followPrint keeps the screens with the print: a new print and a new pause show the screen of
// the print, and so does a time with no touch while away.
func (a *App) followPrint(st printer.State) {
	busy := st.Reachable && st.Busy()
	if !a.followed {
		// The first frame: the state that this App starts with is no change.
		a.followed, a.wasBusy, a.wasPaused = true, busy, st.Paused
	}
	switch {
	case busy && !a.wasBusy, st.Paused && !a.wasPaused:
		a.away, a.pages, a.tune = false, nil, false
	case !busy:
		a.away = false
	case a.away && a.awayFile != st.FilePath:
		a.away = false // another print
	case a.away && a.Now().Sub(a.touchAt) > awayIdle:
		a.away, a.pages = false, nil
	}
	a.wasBusy, a.wasPaused = busy, st.Paused
}

// showsBar reports that the frame has the print bar: a print runs, and a page or the home screen
// covers the screen of the print.
func (a *App) showsBar(st printer.State) bool {
	return st.Reachable && st.Busy() && (a.away || len(a.pages) > 0) && !a.tune
}

// printing reports that a print runs: the keys that change the printer wait.
func (a *App) printing() bool {
	if a.Printer == nil {
		return false
	}
	st := a.Printer.State()
	return st.Reachable && st.Busy()
}

// printerKey draws a key that moves, heats or changes the printer. While a print runs, the key is
// dim with a lock, and a tap says to wait.
func (a *App) printerKey(c *gfx.Canvas, r image.Rectangle, label string, fill, fg color.RGBA, tap func()) {
	if !a.printing() {
		a.button(c, r, label, fill, fg, tap)
		return
	}
	if fill == orange {
		fill = raised
	}
	a.lockedKey(c, r, label, fill)
}

// lockedKey draws a key that waits for the end of the print: dim, with a lock at the right of its
// label. A tap says to wait. The menu of the printer display does not have it.
func (a *App) lockedKey(c *gfx.Canvas, r image.Rectangle, label string, fill color.RGBA) {
	pressed := a.isPressed(r)
	keyBeside(c, r, label, fill, dim, pressed, 26)
	y := r.Min.Y + (r.Dy()-5)/2
	if pressed {
		y += 4
	}
	drawLock(c, r.Max.X-19, y, dim)
	tap := func() { a.Toast(waitText) }
	a.addHit(kit.Hit{R: r, Tap: tap})
	a.region(label, r)
}

// drawPrintBar draws the print bar at the bottom of the screen: what the print does, its
// progress, and the way back to it.
func (a *App) drawPrintBar(c *gfx.Canvas, st printer.State) {
	r := image.Rect(0, H-barH, W, H)
	c.Fill(r, bg)
	kr := kit.Inset(r, 12, 6, 12, 8)
	tap := func() { a.returnToPrint() }
	key(c, kr, "", orange, onAcc, a.isPressed(kr))
	a.addHit(kit.Hit{R: kr, Tap: tap})
	a.addFocus("Back to the print", kr, tap)
	in := kit.Inset(kr, 16, 0, 16, 5)
	what, frac := printBarText(st)
	go1 := "BACK TO PRINT ›"
	if W < 600 {
		go1 = "BACK ›"
	}
	face := gfx.Face(true, 16)
	gw := gfx.Width(face, go1)
	c.Text(image.Rect(in.Max.X-gw, in.Min.Y, in.Max.X, in.Max.Y), go1, face, onAcc, gfx.Right)
	c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X-gw-16, in.Max.Y), gfx.Fit(face, what, in.Dx()-gw-16), face, onAcc, gfx.Left)
	// The progress: a line at the bottom of the bar.
	if frac > 0 {
		line := image.Rect(kr.Min.X+10, kr.Max.Y-9, kr.Max.X-10, kr.Max.Y-6)
		c.Fill(line, darken(orange, 0.35))
		c.Fill(image.Rect(line.Min.X, line.Min.Y, line.Min.X+int(float64(line.Dx())*min(1, frac)), line.Max.Y), onAcc)
	}
	a.region("Print bar", kr)
}

// printBarText is the text of the print bar, and the part of the print that is done (0 for none).
func printBarText(st printer.State) (string, float64) {
	left := ""
	if st.HasTimeLeft {
		left = " · " + strings.ToUpper(duration(st.TimeLeft)) + " LEFT"
	}
	switch {
	case st.Paused:
		return "PAUSED", st.Completion / 100
	case st.Finishing && st.HasBedCoolLeft:
		return "COOLING THE BED · " + strings.ToUpper(shortWait(st.BedCoolLeft)), 0
	case st.Finishing:
		return "FINISHING", 1
	case st.Preparing:
		return "STARTING THE PRINT", 0
	case st.Layers > 0 && st.Layer > 0:
		return fmt.Sprintf("LAYER %d/%d", st.Layer, st.Layers) + left, st.Completion / 100
	}
	return fmt.Sprintf("PRINTING %.0f%%", st.Completion) + left, st.Completion / 100
}
