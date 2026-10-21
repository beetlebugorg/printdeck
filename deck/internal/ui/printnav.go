package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Print tab: a screen for the state of the printer (idle, preparing, first layer, printing,
// paused, done, error), and the pages that the idle screen opens: Files, Move, Filament,
// Maintenance and Machine. The pages are a stack: Back goes one page up.

type page int

const (
	pageFiles page = iota + 1
	pageFile       // the detail of one file (a.file)
	pageMove
	pageFilament
	pageMaintenance
	pageMachine
	pageMacros
	pageHistory
	pageConsole
	pageStats
	pageConfigs // the example configs of Klipper, for the setup of a new printer
)

// open shows a page over the current one.
func (a *App) open(p page) {
	a.pages = append(a.pages, p)
	a.Redraw()
}

// back closes the top page.
func (a *App) back() {
	if len(a.pages) > 0 {
		a.pages = a.pages[:len(a.pages)-1]
	}
	a.Redraw()
}

func (a *App) topPage() page {
	if len(a.pages) == 0 {
		return 0
	}
	return a.pages[len(a.pages)-1]
}

// backButton is the Back key at the top right of a page.
func (a *App) backButton(c *gfx.Canvas) {
	a.button(c, image.Rect(W-130, 66, W-24, 112), "Back", raised, text, a.back)
}

// material is a filament with its temperatures.
type material struct {
	Name      string
	Tool, Bed int
}

var materials = []material{{"PLA", 210, 60}, {"PETG", 240, 80}, {"ABS", 250, 100}, {"TPU", 225, 50}}

// loaded is the loaded material, or the preheat setting when no material is set.
func (a *App) loaded() material {
	for _, m := range materials {
		if m.Name == a.Material {
			return m
		}
	}
	return material{"", a.PreheatTool, a.PreheatBed}
}

// setMaterial sets the loaded material, and saves it.
func (a *App) setMaterial(name string) {
	a.Material = name
	if a.OnMaterial != nil {
		a.OnMaterial(name)
	}
}

// lcd2 makes the two lines of the character display: l at the left and r at the right of each line.
func lcd2(l1, r1, l2, r2 string) []string {
	lr := func(l, r string) string {
		n := 20 - len([]rune(r))
		ls := []rune(strings.ToUpper(l))
		if len(ls) > n-1 {
			ls = ls[:max(0, n-1)]
		}
		return string(ls) + strings.Repeat(" ", max(0, n-len(ls))) + strings.ToUpper(r)
	}
	return []string{lr(l1, r1), lr(l2, r2)}
}

var printLCD = image.Rect(16, 62, W-16, 154)

// drawHome is the idle screen: ready, the temperatures, Print, preheat, the last print, and the menus.
func (a *App) drawHome(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	m := a.loaded()
	right1 := "NO FILAMENT"
	if m.Name != "" {
		right1 = m.Name + " LOADED"
	}
	l2, r2 := "", ""
	switch {
	case st.Message != "":
		l2 = st.Message
	case st.Homed == "xyz":
		l2 = "XYZ homed"
	case st.Homed != "":
		l2 = strings.ToUpper(st.Homed) + " homed"
	default:
		l2 = "Not homed"
	}
	if st.FirmwareOld {
		l2, r2 = "Firmware is old", ""
	}
	charLCD(c, printLCD, lcd2("Ready", right1, l2, r2), 20)
	a.temps(c, 166, 84, st.Tool, st.ToolTarget, st.Bed, st.BedTarget, st.Connected)

	a.button(c, image.Rect(40, 264, W-40, 344), "Print a file", orange, onAcc, func() {
		go func() { pr.LoadFiles(); a.Redraw() }()
		a.open(pageFiles)
	})
	half := (W - 80 - 12) / 2
	pre := fmt.Sprintf("Preheat %d/%d", m.Tool, m.Bed)
	if m.Name != "" {
		pre = "Preheat " + m.Name
	}
	a.button(c, image.Rect(40, 356, 40+half, 416), pre, raised, text, func() { pr.Heat(m.Tool, m.Bed) })
	a.button(c, image.Rect(W-40-half, 356, W-40, 416), "Cool down", raised, text, func() { pr.Heat(0, 0) })

	// The last print, with Reprint.
	last := image.Rect(40, 430, W-40, 520)
	if l, ok := a.lastPrint(st); ok {
		plate(c, last)
		label(c, image.Rect(last.Min.X+16, last.Min.Y+8, last.Max.X, last.Min.Y+28), "Last print", gfx.Left)
		c.Text(image.Rect(last.Min.X+16, last.Min.Y+28, last.Max.X-150, last.Min.Y+58), gfx.Fit(gfx.Face(true, 20), l.File, last.Dx()-170), gfx.Face(true, 20), text, gfx.Left)
		c.Text(image.Rect(last.Min.X+16, last.Min.Y+56, last.Max.X-150, last.Max.Y-8), l.Summary(), gfx.Face(false, 16), dim, gfx.Left)
		path := l.Path
		a.button(c, image.Rect(last.Max.X-140, last.Min.Y+20, last.Max.X-14, last.Max.Y-16), "Reprint", raised, text, func() {
			a.confirmPrint(path)
		})
	} else {
		plate(c, last)
		c.Text(last, "No print yet", gfx.Face(false, 18), dim, gfx.Center)
	}

	for i, t := range []struct {
		name string
		p    page
	}{{"Filament", pageFilament}, {"Move", pageMove}, {"Maintenance", pageMaintenance}, {"Machine", pageMachine}} {
		x0 := 40 + (i%2)*(half+12)
		y0 := 534 + (i/2)*86
		p := t.p
		a.button(c, image.Rect(x0, y0, x0+half, y0+76), t.name, raised, text, func() { a.open(p) })
	}
}

// confirmPrint asks before it starts a file.
func (a *App) confirmPrint(path string) {
	name := path[strings.LastIndex(path, "/")+1:]
	a.confirm = &confirmBox{question: "Print " + gfx.Fit(gfx.Face(true, 28), name, 300) + "?", yes: "Print", onYes: func() {
		a.Printer.PrintFile(path)
		a.pages = nil
	}}
}

// menuRow is a row of a menu page: a title, a line under it, and an arrow. tap runs when the row
// is tapped; nil draws a row with no action.
func (a *App) menuRow(c *gfx.Canvas, r image.Rectangle, title, sub string, subCol color.RGBA, tap func()) {
	plate(c, r)
	if tap != nil && a.isPressed(r) {
		c.RoundRect(r.Inset(1), 4, raised)
	}
	c.Text(image.Rect(r.Min.X+18, r.Min.Y+8, r.Max.X-50, r.Min.Y+40), strings.ToUpper(title), gfx.Face(true, 20), text, gfx.Left)
	c.Text(image.Rect(r.Min.X+18, r.Min.Y+38, r.Max.X-50, r.Max.Y-8), gfx.Fit(gfx.Face(false, 16), sub, r.Dx()-70), gfx.Face(false, 16), subCol, gfx.Left)
	if tap == nil {
		return
	}
	cx, cy := r.Max.X-28, r.Min.Y+r.Dy()/2
	c.Polygon([]image.Point{{cx - 5, cy - 10}, {cx + 5, cy}, {cx - 5, cy + 10}, {cx - 9, cy + 6}, {cx - 3, cy}, {cx - 9, cy - 6}}, dim)
	a.addHit(hit{r: r, tap: tap})
	a.addFocus(title, r, tap)
}

// estop is the Stop key in the status bar while the printer prints, moves or heats.
func (a *App) estopVisible() bool {
	if a.Printer == nil {
		return false
	}
	st := a.Printer.State()
	return st.Reachable && st.Connected && (st.Busy() || st.ToolTarget > 0 || st.BedTarget > 0)
}

func (a *App) drawEstop(c *gfx.Canvas) {
	r := image.Rect(W-100, 6, W-10, statusH-4)
	pr := a.Printer
	a.button(c, r, "Stop", redCap, onRed, func() {
		a.confirm = &confirmBox{question: "Stop the printer now?", yes: "Stop", danger: true, onYes: func() {
			a.do(pr.EmergencyStop)
		}}
	})
}
