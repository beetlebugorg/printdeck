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
	nav  bool            // a main page with the key panel (navPage)
	lcd  image.Rectangle // the display of the printer; empty on a tall page
	head image.Rectangle // the title and Back of a page, inside the margins
	side image.Rectangle // wide: the free space of the left column, under the display or the title
	main image.Rectangle // the content; inside the margins on a wide screen
}

const (
	margin   = 24 // tall: the space between the content and the edges of the screen
	headH    = 66 // tall: the top of the content of a page, under the title and Back
	lcdTop   = 20 // tall: the top of the display
	lcdBelow = 232
	keyH     = 46 // the height of the head of a page: the height of Back
)

// The grid of a wide screen: a margin of gridM at the edges, and gridM between the zones. The left
// column is as wide as the display with its bezel (at wideScale), so the keys under the display
// line up with it. The display has the same size and place on each wide screen.
const (
	gridM     = 16
	wideScale = 2 // the scale of the display on a wide screen
)

// bezelW is the width of the display at a scale, with its bezel of 8 pixels.
func bezelW(scale int) int { return 128*scale + 16 }

// wideZones are the two columns of a wide screen, inside the margins, down to the bottom of the
// content (above the print bar when it shows). The left column has the width of the display at
// scale.
func wideZones(scale int) (left, right image.Rectangle) {
	bottom := H - gridM
	if contentY1 < H {
		bottom = contentY1 - 8 // the key of the print bar is 8 pixels under contentY1
	}
	left = image.Rect(gridM, gridM, gridM+bezelW(scale), bottom)
	right = image.Rect(left.Max.X+gridM, gridM, W-gridM, bottom)
	return left, right
}

// displayIn is the picture of the display at the top of the left column, in the middle, and the
// space under its bezel.
func displayIn(left image.Rectangle, scale int) (lcd, under image.Rectangle) {
	w, h := 128*scale, 64*scale
	x := left.Min.X + (left.Dx()-w)/2
	lcd = image.Rect(x, left.Min.Y+8, x+w, left.Min.Y+8+h)
	return lcd, image.Rect(left.Min.X, lcd.Max.Y+8+gridM, left.Max.X, left.Max.Y)
}

// lcdRect is the display of the printer in a pane of a tall screen: at the top, in the middle.
func lcdRect(pane image.Rectangle) image.Rectangle {
	w, h := 128*lcdScale, 64*lcdScale
	x := pane.Min.X + (pane.Dx()-w)/2
	return image.Rect(x, pane.Min.Y+lcdTop, x+w, pane.Min.Y+lcdTop+h)
}

// divider draws the seam between the two columns of a wide screen: a groove in the middle of the
// gap, a dark line and a light line, as the joint of two panels.
func divider(c *gfx.Canvas) {
	left, _ := wideZones(wideScale)
	x := left.Max.X + gridM/2 - 1
	c.Fill(image.Rect(x, left.Min.Y, x+1, left.Max.Y), edge)
	c.Fill(image.Rect(x+1, left.Min.Y, x+2, left.Max.Y), shine)
}

// systemKeyList are the keys of systemKeys, each a function that draws it in a place.
func (a *App) systemKeyList(c *gfx.Canvas) []func(image.Rectangle) {
	var keys []func(image.Rectangle)
	if a.Setup != nil {
		keys = append(keys, func(r image.Rectangle) { a.button(c, r, "Wi-Fi and help", raised, text, a.OpenHelp) })
	}
	if a.Updater != nil {
		keys = append(keys, func(r image.Rectangle) {
			a.button(c, r, "Software update", raised, text, func() { a.open(pageUpdate) })
		})
	}
	return keys
}

// tallHead is the head of a page on a tall screen, inside the margins.
func tallHead() image.Rectangle { return image.Rect(margin, lcdTop, W-margin, lcdTop+keyH) }

// stateFrame is the frame of a state screen (idle, printing, paused...).
func (a *App) stateFrame() frame {
	scr := kit.Screen{W: W, H: contentY1}
	if scr.Wide() {
		left, right := wideZones(wideScale)
		lcd, under := displayIn(left, wideScale)
		return frame{wide: true, lcd: lcd, side: under, main: right}
	}
	top, main := scr.Split(lcdBelow)
	return frame{lcd: lcdRect(top), main: main}
}

// page draws the title and the Back key of a page, and on a wide screen the display of the printer
// over them. It returns the frame of the page.
func (a *App) page(c *gfx.Canvas, title string) frame { return a.pageBack(c, title, "Back", a.back) }

// pageBack is page with another key in the place of Back: its label and its action.
func (a *App) pageBack(c *gfx.Canvas, title, label string, back func()) frame {
	f := a.pageFrame()
	if f.wide {
		a.statusPanelAt(c, a.Printer.State(), f.lcd)
	}
	headingAt(c, f.head, title)
	a.button(c, backRect(f.head), label, raised, text, back)
	return f
}

// takeover draws the head of a screen that takes over the deck (an action, the firmware flash):
// its title, and on a wide screen the display. It has no Back. It returns the area of the content
// and the place of the key at the bottom.
func (a *App) takeover(c *gfx.Canvas, title string) (content, key image.Rectangle) {
	f := a.pageFrame()
	headingAt(c, f.head, title)
	if f.wide {
		a.statusPanelAt(c, a.Printer.State(), f.lcd)
		return f.body(), kit.Rows(f.aside(), 0, kit.Fr(1), kit.Px(80))[1]
	}
	r := kit.Rows(kit.Inset(f.main, margin, 12, margin, 16), 16, kit.Fr(1), kit.Px(80))
	return r[0], r[1]
}

// plain draws the title of a screen with no display (the printer setup, an error of Klipper). It
// returns two areas. On a wide screen, text is the left column under the title, and info is the
// right column. On a tall screen both are the one column under the title.
func (a *App) plain(c *gfx.Canvas, title string) (wide bool, text, info image.Rectangle) {
	scr := kit.Screen{W: W, H: contentY1}
	if scr.Wide() {
		// The title at the top of the right column, as on each wide screen.
		left, right := wideZones(wideScale)
		head := image.Rect(right.Min.X, right.Min.Y, right.Max.X, right.Min.Y+keyH)
		headingAt(c, head, title)
		return true, left, image.Rect(right.Min.X, head.Max.Y+gridM, right.Max.X, right.Max.Y)
	}
	headingAt(c, tallHead(), title)
	body := image.Rect(margin, headH+8, W-margin, contentY1-8)
	return false, body, body
}

// pageFrame is the frame of a page that is a step of a task (Tune, the filament swap, a form),
// with no drawing. A wide screen has the display in the left column, and the title and Back at the
// top of the right column, as each wide screen.
func (a *App) pageFrame() frame {
	scr := kit.Screen{W: W, H: contentY1}
	if scr.Wide() {
		left, right := wideZones(wideScale)
		lcd, under := displayIn(left, wideScale)
		head := image.Rect(right.Min.X, right.Min.Y, right.Max.X, right.Min.Y+keyH)
		main := image.Rect(right.Min.X, head.Max.Y+gridM, right.Max.X, right.Max.Y)
		return frame{wide: true, lcd: lcd, head: head, side: under, main: main}
	}
	_, main := kit.Screen{W: W, H: contentY1}.Split(headH)
	return frame{head: tallHead(), main: main}
}

// body is the content area of a page: in the main pane, inside the margins.
func (f frame) body() image.Rectangle {
	if f.wide {
		return f.main
	}
	return kit.Inset(f.main, margin, 0, margin, 8)
}

// aside is the free space of the left column of a wide page, inside the margins. It is empty on a
// tall screen.
func (f frame) aside() image.Rectangle {
	if !f.wide {
		return image.Rectangle{}
	}
	return f.side
}

// backRect is the place of the Back key in the head of a page: at the right.
func backRect(head image.Rectangle) image.Rectangle {
	return image.Rect(head.Max.X-106, head.Min.Y, head.Max.X, head.Min.Y+keyH)
}

// headingAt draws the title of a page in its head: an orange mark at the left edge of the head, and
// the title, in the middle of the height of Back.
func headingAt(c *gfx.Canvas, head image.Rectangle, title string) {
	setHeading(title)
	x, y := head.Min.X, head.Min.Y
	c.Fill(image.Rect(x, y+13, x+10, y+33), orange)
	// A long title gets a smaller font, down to 18 pixels, and then loses its end.
	title, w := strings.ToUpper(title), head.Max.X-118-(x+20)
	size := 24
	for size > 18 && gfx.Width(gfx.Face(true, size), title) > w {
		size--
	}
	c.Text(image.Rect(x+20, y, head.Max.X-118, y+keyH), gfx.Fit(gfx.Face(true, size), title, w), gfx.Face(true, size), text, gfx.Left)
}

// helpKey draws a Help key in the head of a page, at the left of Back. It opens Wi-Fi and help,
// which shows the address and the password. The key is not drawn when the deck has no SetupHost.
func (a *App) helpKey(c *gfx.Canvas, head image.Rectangle) {
	if a.Setup != nil {
		a.headKey(c, head, "Help", a.OpenHelp)
	}
}

// headKey draws a key in the head of a page, at the left of Back.
func (a *App) headKey(c *gfx.Canvas, head image.Rectangle, label string, tap func()) {
	a.headKeyOn(c, head, label, false, tap)
}

// headKeyOn is headKey with its LED: lit (on) on the page that the key opened, where it stays in
// its place.
func (a *App) headKeyOn(c *gfx.Canvas, head image.Rectangle, label string, on bool, tap func()) {
	b := backRect(head)
	fill, ink := raised, text
	if on {
		fill, ink = keyOn, keyOnInk
	}
	a.button(c, b.Sub(image.Pt(b.Dx()+12, 0)), label, fill, ink, tap)
}

// systemKeys draws Wi-Fi and help, and Software update, side by side in r. They are the way out of
// a screen that has no pages, such as an error of Klipper. A key is not drawn when the deck has no
// SetupHost or no Updater.
func (a *App) systemKeys(c *gfx.Canvas, r image.Rectangle) {
	keys := a.systemKeyList(c)
	sizes := make([]kit.Size, len(keys))
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	// Side by side, or under each other in a narrow column.
	cells := kit.Cols(r, 12, sizes...)
	if r.Dx() < 320 {
		cells = kit.Rows(image.Rect(r.Min.X, r.Max.Y-len(keys)*keyH-(len(keys)-1)*12, r.Max.X, r.Max.Y), 12, sizes...)
	}
	for i, k := range cells {
		keys[i](k)
	}
}
