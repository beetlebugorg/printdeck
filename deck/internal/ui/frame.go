package ui

import (
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The frame of a screen: where the display of the printer, the title and the content go, on a
// tall and on a wide screen.
//
// Tall (480 x 800): the display is at the top of a state screen, and the content is under it. A
// page has its title and Back at the top, and the content under them.
//
// Wide (800 x 480): the left column has the display at its top. Under the display, a state screen
// has its main keys, and a page has its title, Back, and its keys. The content is in the right
// column.
type frame struct {
	wide bool
	lcd  image.Rectangle // the display of the printer; empty on a tall page
	head image.Rectangle // the title and Back of a page
	side image.Rectangle // wide: the free space of the left column, under the display or the title
	main image.Rectangle // the content
}

const (
	sideW    = 416 // the left column of a wide screen
	margin   = 24  // the space between the content and the edges of the screen
	headH    = 66  // the title and Back of a page
	lcdTop   = 20  // the top of the display
	lcdBelow = 232 // the top of the space under the display
)

// wideIn and wideOut are the margins of the two columns of a wide screen: at the edges of the
// screen, and between the columns.
const wideIn, wideOut = 8, 16

// lcdRect is the display of the printer in a pane: at the top, in the middle.
func lcdRect(pane image.Rectangle) image.Rectangle {
	w, h := 128*lcdScale, 64*lcdScale
	x := pane.Min.X + (pane.Dx()-w)/2
	return image.Rect(x, pane.Min.Y+lcdTop, x+w, pane.Min.Y+lcdTop+h)
}

// stateFrame is the frame of a state screen (idle, printing, paused...).
func (a *App) stateFrame() frame {
	scr := kit.Screen{W: W, H: contentY1}
	if scr.Wide() {
		side, main := scr.Split(sideW)
		return frame{wide: true, lcd: lcdRect(side), side: kit.Inset(side, 0, lcdBelow, 0, 0), main: main}
	}
	top, main := scr.Split(lcdBelow)
	return frame{lcd: lcdRect(top), main: main}
}

// page draws the title and the Back key of a page, and on a wide screen the display of the printer
// over them. It returns the frame of the page.
func (a *App) page(c *gfx.Canvas, title string) frame {
	f := a.pageFrame()
	if f.wide {
		a.statusPanelAt(c, a.Printer.State(), f.lcd)
	}
	headingAt(c, f.head, title)
	a.button(c, backRect(f.head), "Back", raised, text, a.back)
	return f
}

// pageFrame is the frame of a page, with no drawing.
func (a *App) pageFrame() frame {
	scr := kit.Screen{W: W, H: contentY1}
	if scr.Wide() {
		side, main := scr.Split(sideW)
		r := kit.Rows(side, 0, kit.Px(lcdBelow), kit.Px(headH), kit.Fr(1))
		return frame{wide: true, lcd: lcdRect(side), head: r[1], side: r[2], main: main}
	}
	head, main := kit.Screen{W: W, H: contentY1}.Split(headH)
	return frame{head: head, main: main}
}

// backRect is the place of the Back key in the head of a page: at the right.
func backRect(head image.Rectangle) image.Rectangle {
	return image.Rect(head.Max.X-130, head.Min.Y+20, head.Max.X-margin, head.Min.Y+66)
}

// headingAt draws the title of a page in its head: an orange mark and the title.
func headingAt(c *gfx.Canvas, head image.Rectangle, title string) {
	setHeading(title)
	x, y := head.Min.X, head.Min.Y
	c.Fill(image.Rect(x+margin, y+30, x+34, y+50), orange)
	c.Text(image.Rect(x+44, y+18, head.Max.X-150, y+62), strings.ToUpper(title), gfx.Face(true, 24), text, gfx.Left)
}
