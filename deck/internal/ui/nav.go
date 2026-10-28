package ui

import (
	"image"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The main pages on a wide screen have fixed zones, as the panel of a machine:
//
//   - The left column is the same on each page: the display of the printer, and under it the key
//     panel of the main pages, with the page that is open lit.
//   - The right column has the head (the title and Back), the content, and one row of actions at
//     the bottom: the main action at the right, a destructive action at the left.
//
// A tall screen has no key panel: the head, the content and the actions are under each other.

// navKey is a key of the key panel: a main page.
type navKey struct {
	name string
	p    page
}

// Main is the home screen (no page): not "Home", which is the homing of the axes on a printer. The
// rolls are a part of Filament (its head key Rolls), and
// History and Maintenance a part of Machine, so the panel has no keys for them. A deck with a
// camera has Camera in the last place of the panel.
var navKeys = []navKey{{"Main", 0}, {"Files", pageFiles}, {"Filament", pageFilament},
	{"Toolhead", pageMove}, {"Machine", pageMachine}}

// keys is the key panel of the app: navKeys, and Camera on a deck with a camera.
func (a *App) keys() []navKey {
	if a.Camera == nil {
		return navKeys
	}
	return append(navKeys[:len(navKeys):len(navKeys)], navKey{"Camera", pageCamera})
}

// actH is the height of the row of actions at the bottom of a page.
const actH = 72

// navFrame is the frame of a main page, with no drawing. A tall screen has the frame of a page.
func (a *App) navFrame() frame {
	if !(kit.Screen{W: W, H: contentY1}).Wide() {
		return a.pageFrame()
	}
	left, right := wideZones(wideScale)
	lcd, under := displayIn(left, wideScale)
	head := image.Rect(right.Min.X, right.Min.Y, right.Max.X, right.Min.Y+keyH)
	main := image.Rect(right.Min.X, head.Max.Y+gridM, right.Max.X, right.Max.Y)
	return frame{wide: true, nav: true, lcd: lcd, side: under, head: head, main: main}
}

// navPage draws a main page: on a wide screen the display, the key panel, the title and Back; on
// a tall screen the title and Back. It returns the frame.
func (a *App) navPage(c *gfx.Canvas, title string) frame {
	f := a.navFrame()
	if !f.wide {
		return a.page(c, title)
	}
	a.statusPanelAt(c, a.Printer.State(), f.lcd)
	a.drawNavKeys(c, f.side)
	headingAt(c, f.head, title)
	a.button(c, backRect(f.head), "Back", raised, text, a.back)
	return f
}

// drawNavKeys draws the key panel: the main pages in two columns. The page that is open is lit,
// and a tap opens a page at its top. During a print, Toolhead waits for its end.
func (a *App) drawNavKeys(c *gfx.Canvas, r image.Rectangle) {
	a.region("Pages", r)
	// The lit key is the deepest page of the stack that has a key: Filament for Rolls, opened from
	// Filament; Main with no page.
	keys := a.keys()
	open := page(0)
	for _, p := range a.pages {
		for _, k := range keys {
			if k.p == p {
				open = p
			}
		}
	}
	for i, cell := range kit.Grid(r, 2, 3, 10) {
		if i >= len(keys) {
			break
		}
		k := keys[i]
		tap := func() {
			if k.p == pageFiles {
				go func() { a.Printer.LoadFiles(); a.Redraw() }()
			}
			a.pages, a.away = nil, false // Main during a print is the print
			if k.p != 0 {
				a.pages = []page{k.p}
			}
			a.Redraw()
		}
		switch {
		case k.p == open:
			a.button(c, cell, k.name, keyOn, keyOnInk, tap)
		case k.p == pageMove:
			a.printerKey(c, cell, k.name, keyOff, text, tap)
		default:
			a.button(c, cell, k.name, keyOff, text, tap)
		}
	}
}

// split divides the body of a page into the content and the row of actions at its bottom.
func (f frame) split() (content, actions image.Rectangle) {
	r := kit.Rows(f.body(), 12, kit.Fr(1), kit.Px(actH))
	return r[0], r[1]
}
