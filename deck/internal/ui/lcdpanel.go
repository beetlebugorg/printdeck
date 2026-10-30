package ui

import (
	"fmt"
	"image"
	"sync"
	"time"

	"printdeck/internal/gfx"
	lcdscreen "printdeck/internal/lcd"
	"printdeck/internal/printer"
	"printdeck/kit"
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
	// During an upload to a resin printer, a tap on the panel can stop it or its print start.
	if st.Upload.Name != "" && st.Upload.Result == "" && !st.Busy() {
		a.addHit(kit.Hit{R: r, Tap: func() { a.askUpload(st.Upload) }})
	}
}

// askUpload asks what to do with the upload u to the printer: cancel the print start after it, or
// stop it. Each choice asks again.
func (a *App) askUpload(u printer.Upload) {
	type control interface {
		CancelStart()
		CancelSend()
	}
	pr, ok := a.Printer.(control)
	if !ok {
		return
	}
	var choices []menuItem
	if u.StartAfter {
		where := "No project yet"
		if p := a.Printer.NextProject(); p != "" {
			where = "Project · " + fitText(p, 260)
		}
		choices = append(choices, menuItem{"Pick a project", where, dim, func() {
			a.askProject(u.Name, a.Printer.NextProject(), func(p string) { a.Printer.SetNextProject(p) })
		}})
		choices = append(choices, menuItem{"Cancel the print start", "The file still goes to the printer", dim, func() {
			a.confirm = &confirmBox{question: "Cancel the print start of " + u.Name + "?", yes: "Cancel start", danger: true, onYes: pr.CancelStart}
		}})
	}
	choices = append(choices, menuItem{"Stop sending", "Deletes the part on the printer", dim, func() {
		a.confirm = &confirmBox{question: "Stop sending " + u.Name + "?", yes: "Stop sending", danger: true, onYes: pr.CancelSend}
	}})
	a.confirm = &confirmBox{question: u.Name, choices: choices}
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
