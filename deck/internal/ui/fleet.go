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

// A deck with many printers (specs/many-printers-on-one-deck.md). Each printer has its own App.
// The Switcher shows one App on the touch screen at a time. Each App shows which printer it
// controls: a bar in the colour of the printer at the top of each page, the label of the printer
// in each page title and in each dialog, and the printer chip in the status panel. A tap on the
// chip opens the list of the printers. A message of another printer shows as a banner, and the
// deck never switches the printer by itself.

// Identity is how the screens show a printer: its name, a short label, its colour, and its kind.
type Identity struct {
	ID    string
	Name  string // "TAZ Workhorse"
	Label string // "TAZ": 2 to 4 characters
	Color color.RGBA
	Resin bool
}

// PrinterColors are the colours of the printers of a deck, in the order that the deck gives
// them to new printers, and PrinterColorNames their names. None is orange or red: orange is the
// accent of the deck, and red is the colour of a stop.
var (
	PrinterColors     = []color.RGBA{rgb(0x14b8a6), rgb(0x8b5cf6), rgb(0xeab308), rgb(0xec4899), rgb(0x0ea5e9), rgb(0x84cc16)}
	PrinterColorNames = []string{"Teal", "Violet", "Yellow", "Pink", "Blue", "Lime"}
)

// inkOn returns the colour of text on the colour c: black on a light colour, white on a dark one.
func inkOn(c color.RGBA) color.RGBA {
	l := 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
	if l > 140 {
		return rgb(0x111111)
	}
	return rgb(0xffffff)
}

// FleetCard is a printer in the list of the printers.
type FleetCard struct {
	Identity
	State  printer.State
	Active bool   // the printer of this screen
	Note   string // what the printer needs from the user, or ""
}

// FleetNote is a message of a printer that is not on the screen: a print that ended, a pause, an
// error. Key is unique for each event.
type FleetNote struct {
	ID, Key, Text string
}

// Fleet is the link of an App to the other printers of its deck. An App with no Fleet is the
// only printer of its deck, and shows no bar, chip or list.
type Fleet interface {
	Self() Identity
	Cards() []FleetCard
	Notes() []FleetNote // of the printers that are not on this screen
	Dismiss(key string)
	Switch(id string) // shows the printer id on this screen
}

// fleetSelf is the printer of the App that draws now, for the page titles and the dialogs, or
// nil. Draw sets it under drawMu.
var fleetSelf *Identity

// fleetBarH is the height of the colour bar at the top of each page.
const fleetBarH = 6

// setFleetSelf sets fleetSelf for the frame of a.
func (a *App) setFleetSelf() {
	fleetSelf = nil
	if a.Fleet != nil {
		id := a.Fleet.Self()
		fleetSelf = &id
	}
}

// fleetTitle is the title of a page or a dialog with the label of the printer: "TAZ · FILES".
func fleetTitle(title string) string {
	if fleetSelf == nil || fleetSelf.Label == "" {
		return title
	}
	return fleetSelf.Label + " · " + title
}

// accentMark is the colour of the mark of a page title: the colour of the printer, or orange.
func accentMark() color.RGBA {
	if fleetSelf != nil {
		return fleetSelf.Color
	}
	return orange
}

// drawFleetBar draws the bar in the colour of the printer at the top of the screen.
func drawFleetBar(c *gfx.Canvas) {
	if fleetSelf != nil {
		c.Fill(image.Rect(0, 0, W, fleetBarH), fleetSelf.Color)
	}
}

// chipWidth is the width of the printer chip with the label.
func chipWidth(label string) int {
	return 12 + 12 + 8 + gfx.Width(gfx.Face(true, 16), label) + 8 + 12 + 12
}

// drawChip draws the printer chip in r: the colour, the label and a down arrow. A tap opens the
// list of the printers.
func (a *App) drawChip(c *gfx.Canvas, r image.Rectangle) {
	id := a.Fleet.Self()
	pressed := a.isPressed(r)
	fill := id.Color
	if pressed {
		fill = color.RGBA{fill.R / 4 * 3, fill.G / 4 * 3, fill.B / 4 * 3, 255}
	}
	c.RoundRect(r, r.Dy()/2, fill)
	ink := inkOn(fill)
	x := r.Min.X + 12
	c.Text(image.Rect(x, r.Min.Y, r.Max.X-28, r.Max.Y), id.Label, gfx.Face(true, 16), ink, gfx.Left)
	// The down arrow.
	ax, ay := r.Max.X-20, r.Min.Y+r.Dy()/2-2
	c.Polygon([]image.Point{{ax - 6, ay - 3}, {ax + 6, ay - 3}, {ax, ay + 4}}, ink)
	open := func() { a.fleetList = true; a.Redraw() }
	a.addHit(kit.Hit{R: r.Inset(-6), Tap: open})
	a.addFocus("Printers", r, open)
	a.region("Printer", r)
}

// fleetLine is the state of a printer in one line, for the list and the banners.
func fleetLine(st printer.State) string {
	switch {
	case !st.Reachable || !st.Connected:
		return "Offline"
	case st.Err != "":
		return "Error · " + st.Err
	case st.Paused:
		return fmt.Sprintf("Paused · %.0f %%", st.Completion)
	case st.Printing:
		s := fmt.Sprintf("Printing · %.0f %%", st.Completion)
		if st.HasTimeLeft {
			s += " · " + shortDuration(st.TimeLeft) + " left"
		}
		return s
	case st.Finished == "complete":
		return "Print done · " + st.FinishedName()
	}
	if st.Text != "" {
		return st.Text
	}
	return "Ready"
}

// shortDuration is d as "1:12" (hours and minutes) or "12 min".
func shortDuration(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// drawFleetList draws the list of the printers over the screen. A tap on a card shows that
// printer.
func (a *App) drawFleetList(c *gfx.Canvas) {
	a.in.Reset() // the list blocks everything below it
	c.Fill(c.Bounds(), bg)
	drawFleetBar(c)
	head := image.Rect(margin, 18, W-margin, 18+keyH)
	setHeading("Printers")
	c.Fill(image.Rect(head.Min.X, head.Min.Y+13, head.Min.X+10, head.Min.Y+33), orange)
	c.Text(image.Rect(head.Min.X+20, head.Min.Y, head.Max.X-118, head.Max.Y), "PRINTERS", gfx.Face(true, 24), text, gfx.Left)
	a.button(c, backRect(head), "Close", raised, text, func() { a.fleetList = false; a.Redraw() })
	cards := a.Fleet.Cards()
	y := head.Max.Y + 20
	cardH := 124
	if avail := contentY1 - 24 - y; len(cards) > 0 && cardH*len(cards)+14*(len(cards)-1) > avail {
		cardH = max(96, (avail-14*(len(cards)-1))/len(cards))
	}
	for _, card := range cards {
		r := image.Rect(margin, y, W-margin, y+cardH)
		a.drawFleetCard(c, r, card)
		y += cardH + 14
	}
}

// drawFleetCard draws one printer of the list in r.
func (a *App) drawFleetCard(c *gfx.Canvas, r image.Rectangle, card FleetCard) {
	pressed := a.isPressed(r)
	border := edge
	if card.Active {
		border = card.Color
	}
	c.RoundRect(r, 10, border)
	inner := r.Inset(3)
	fill := surface
	if pressed {
		fill = raised
	}
	c.RoundRect(inner, 8, fill)
	// The colour edge at the left, as wide as a finger mark.
	c.RoundRect(image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+14, inner.Max.Y), 8, card.Color)
	c.Fill(image.Rect(inner.Min.X+8, inner.Min.Y, inner.Min.X+14, inner.Max.Y), card.Color)
	in := kit.Inset(inner, 30, 14, 18, 14)
	// Line 1: the label chip, the name, and "On screen" or the kind.
	lf := gfx.Face(true, 16)
	lw := gfx.Width(lf, card.Label) + 20
	chip := image.Rect(in.Min.X, in.Min.Y, in.Min.X+lw, in.Min.Y+30)
	c.RoundRect(chip, 15, card.Color)
	c.Text(chip, card.Label, lf, inkOn(card.Color), gfx.Center)
	tag, tagCol := "", dim
	switch {
	case card.Active:
		tag, tagCol = "ON SCREEN", text
	case card.Resin:
		tag = "RESIN"
	}
	tf := gfx.Face(true, 13)
	tw := gfx.Width(tf, tag)
	nf := gfx.Face(true, 22)
	name := image.Rect(chip.Max.X+12, in.Min.Y, in.Max.X-tw-12, in.Min.Y+30)
	c.Text(name, gfx.Fit(nf, card.Name, name.Dx()), nf, text, gfx.Left)
	if tag != "" {
		c.Text(image.Rect(in.Max.X-tw, in.Min.Y, in.Max.X, in.Min.Y+30), tag, tf, tagCol, gfx.Right)
	}
	// Line 2: the state. A note of the printer is in its colour, with a dot.
	sf := gfx.Face(false, 18)
	line := image.Rect(in.Min.X, in.Min.Y+40, in.Max.X, in.Min.Y+66)
	st := fleetLine(card.State)
	if card.Note != "" {
		dot := image.Rect(line.Min.X, line.Min.Y+7, line.Min.X+12, line.Min.Y+19)
		c.RoundRect(dot, 6, card.Color)
		line.Min.X += 20
		st = card.Note
	}
	c.Text(line, gfx.Fit(sf, st, line.Dx()), sf, text, gfx.Left)
	// Line 3: the progress of a print, or the temperatures.
	if card.State.Busy() && in.Max.Y-line.Max.Y >= 18 {
		bar := image.Rect(in.Min.X, in.Max.Y-12, in.Max.X, in.Max.Y-2)
		c.RoundRect(bar, 5, edge)
		if w := int(float64(bar.Dx()) * max(0, min(1, card.State.Completion/100))); w > 0 {
			c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+w, bar.Max.Y), 5, card.Color)
		}
	} else if card.State.Reachable && !card.Resin && in.Max.Y-line.Max.Y >= 20 {
		temps := fmt.Sprintf("Nozzle %.0f°  ·  Bed %.0f°", card.State.Tool, card.State.Bed)
		c.Text(image.Rect(in.Min.X, line.Max.Y+4, in.Max.X, in.Max.Y), temps, gfx.Face(false, 15), dim, gfx.Left)
	}
	id := card.ID
	tap := func() {
		a.fleetList = false
		if !card.Active {
			a.Fleet.Switch(id)
		}
		a.Redraw()
	}
	a.addHit(kit.Hit{R: r, Tap: tap})
	a.addFocus(card.Name, r, tap)
	a.region(card.Name, r)
}

// drawFleetBanner draws the newest message of another printer at the top of the screen, with
// Show and Later. It returns false when there is none.
func (a *App) drawFleetBanner(c *gfx.Canvas) bool {
	notes := a.Fleet.Notes()
	if len(notes) == 0 {
		return false
	}
	n := notes[0]
	var who Identity
	for _, card := range a.Fleet.Cards() {
		if card.ID == n.ID {
			who = card.Identity
		}
	}
	r := image.Rect(12, fleetBarH+8, W-12, fleetBarH+8+128)
	c.RoundRect(r.Inset(-2), 12, edge)
	c.RoundRect(r, 10, raised)
	c.RoundRect(image.Rect(r.Min.X, r.Min.Y, r.Min.X+14, r.Max.Y), 10, who.Color)
	c.Fill(image.Rect(r.Min.X+8, r.Min.Y, r.Min.X+14, r.Max.Y), who.Color)
	in := kit.Inset(r, 30, 12, 16, 12)
	lf := gfx.Face(true, 16)
	lw := gfx.Width(lf, who.Label) + 20
	chip := image.Rect(in.Min.X, in.Min.Y, in.Min.X+lw, in.Min.Y+30)
	c.RoundRect(chip, 15, who.Color)
	c.Text(chip, who.Label, lf, inkOn(who.Color), gfx.Center)
	tf := gfx.Face(true, 20)
	tr := image.Rect(chip.Max.X+12, in.Min.Y, in.Max.X, in.Min.Y+30)
	c.Text(tr, gfx.Fit(tf, n.Text, tr.Dx()), tf, text, gfx.Left)
	keys := kit.Cols(image.Rect(in.Min.X, in.Max.Y-56, in.Max.X, in.Max.Y), 12, kit.Fr(1), kit.Fr(1))
	key := n.Key
	id := n.ID
	a.button(c, keys[0], "Later", raised, text, func() { a.Fleet.Dismiss(key); a.Redraw() })
	a.button(c, keys[1], "Show "+who.Label, orange, onAcc, func() {
		a.Fleet.Dismiss(key)
		a.Fleet.Switch(id)
		a.Redraw()
	})
	return true
}

// fleetQuiet reports that the App shows no dialog, keypad, keyboard or sheet: a banner of another
// printer can show.
func (a *App) fleetQuiet() bool {
	return a.confirm == nil && a.keypad == nil && a.consoleKB == nil && a.picker == nil && a.colors == nil &&
		a.form == nil && a.qrSheet == nil && !a.shareSheet && a.swap == nil && !a.tune && a.flash == nil &&
		a.setup == nil && !a.fleetList
}

// drawFleet draws the parts of a deck with many printers over the frame: the colour bar, and the
// list of the printers or a banner of another printer.
func (a *App) drawFleet(c *gfx.Canvas) {
	if a.Fleet == nil {
		return
	}
	if a.fleetList {
		a.collect, a.focus = !a.Record, a.focus[:0]
		a.drawFleetList(c)
		return
	}
	drawFleetBar(c)
	if a.fleetQuiet() && !a.jogging() {
		a.drawFleetBanner(c)
	}
}

// labelFrom suggests the short label of a printer from its name: the first word, at most 4
// characters, in capitals ("TAZ Workhorse" is "TAZ", "Phenom" is "PHEN").
func LabelFrom(name string) string {
	w := strings.Fields(name)
	if len(w) == 0 {
		return "P"
	}
	l := strings.ToUpper(w[0])
	if r := []rune(l); len(r) > 4 {
		l = string(r[:4])
	}
	return l
}

// jogging reports that the finger holds a jog key: a banner waits until the move ends.
func (a *App) jogging() bool {
	if a.jog == nil {
		return false
	}
	a.jog.mu.Lock()
	defer a.jog.mu.Unlock()
	return a.jog.held || a.jog.running
}

// sessionFleet is the Fleet of a remote screen: it shows the printers like the touch screen, but
// a switch there does not change the printer of the touch screen. The remote screen of another
// printer is its own address (?printer=<id>), which OnOpenPrinter opens.
type sessionFleet struct {
	Fleet
	a *App
}

func (f sessionFleet) Switch(id string) {
	if f.a.OnOpenPrinter != nil {
		f.a.OnOpenPrinter(id)
		return
	}
	f.a.Toast("Open this printer on the deck")
}
