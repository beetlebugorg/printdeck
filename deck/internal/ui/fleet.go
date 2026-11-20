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
// controls: the status bar at the top of each page, with a tab for each printer, and the label of
// the printer in each page title and in each dialog. A tap on another tab shows that printer. A
// tap on the tab of this printer opens Settings, Printers, with the state of each printer and Stop
// all. A message of another printer shows as a banner, and the deck never switches the printer by
// itself.

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
	// StopAll stops each printer that prints, is paused or heats, with an emergency stop.
	StopAll() error
}

// stoppable reports that Stop all stops a printer in the state st: it prints, is paused, or
// heats. A stop of an idle printer only makes it need a firmware restart.
func stoppable(st printer.State) bool {
	return st.Busy() || st.ToolTarget > 0 || st.BedTarget > 0
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

// stopCount is the number of the printers of the deck that Stop all stops.
func (a *App) stopCount() int {
	n := 0
	for _, card := range a.Fleet.Cards() {
		if stoppable(card.State) {
			n++
		}
	}
	return n
}

// askStopAll asks to stop each printer that prints, is paused or heats, with an emergency stop.
func (a *App) askStopAll() {
	n := a.stopCount()
	if n == 0 {
		a.Toast("No printer prints or heats")
		return
	}
	a.confirm = &confirmBox{title: "STOP ALL",
		question: fmt.Sprintf("Stop %d printers that print or heat, now? Each one needs a firmware restart after it.", n),
		yes:      "Stop all", danger: true, allPrinters: true,
		onYes: func() { a.do(a.Fleet.StopAll) }}
	if n == 1 {
		a.confirm.question = "Stop the printer that prints or heats, now? It needs a firmware restart after it."
	}
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
	r := image.Rect(12, fleetStatusH+8, W-12, fleetStatusH+8+128)
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
	if a.Record {
		c.Link(keys[1], "/?printer="+id)
	}
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
		a.form == nil && a.textKB == nil && a.qrSheet == nil && !a.shareSheet && a.swap == nil && !a.tune && a.flash == nil &&
		a.setup == nil
}

// drawFleet draws the parts of a deck with many printers over the frame: the status bar and a
// banner of another printer, or the list of the printers.
func (a *App) drawFleet(c *gfx.Canvas) {
	if a.Fleet == nil {
		return
	}
	// A keyboard, a keypad or a dialog covers the top of the screen with its title: the status
	// bar waits until it closes, so the title shows and a tap cannot switch the printer under it.
	if !a.fleetQuiet() {
		return
	}
	a.drawFleetStatus(c)
	if !a.jogging() {
		a.drawFleetBanner(c)
	}
}

// fleetStatusH is the height of the status bar of a deck with many printers: the band at the top
// of each page, over the frame of the page. It is as tall as a finger, because a tap on it
// switches the printer.
const fleetStatusH = 56

// shortState is the state of a printer in a word or two, for the status bar.
func shortState(st printer.State) string {
	switch {
	case !st.Reachable && st.Starting:
		return "Starting"
	case !st.Reachable && st.Err == "" && !st.ErrFinal:
		return "Connecting"
	case !st.Reachable || !st.Connected:
		return "Offline"
	case st.Err != "":
		return "Error"
	case st.Paused:
		return fmt.Sprintf("Paused %.0f%%", st.Completion)
	case st.Printing:
		s := fmt.Sprintf("%.0f%%", st.Completion)
		if st.HasTimeLeft {
			s += " · " + shortDuration(st.TimeLeft)
		}
		return s
	case st.Finished == "complete":
		return "Done"
	}
	return "Ready"
}

// drawFleetStatus draws the status bar at the top of each page: a tab for each printer, with its
// label and its state. The tab of the printer of this screen is in the colour of the printer. A
// tap on another tab shows that printer. A tap on the tab of this screen opens the list of the
// printers, with Stop all.
func (a *App) drawFleetStatus(c *gfx.Canvas) {
	bar := image.Rect(0, 0, W, fleetStatusH)
	c.Fill(bar, lcd)
	self := a.Fleet.Self()
	c.Fill(image.Rect(0, fleetStatusH-3, W, fleetStatusH), self.Color)
	cards := a.Fleet.Cards()
	if len(cards) == 0 {
		return
	}
	row := image.Rect(6, 5, W-6, fleetStatusH-7)
	cols := make([]kit.Size, len(cards))
	for i := range cols {
		cols[i] = kit.Fr(1)
	}
	tabs := kit.Cols(row, 6, cols...)
	for i, card := range cards {
		a.drawFleetTab(c, tabs[i], card)
	}
	a.region("Status bar", bar)
}

// drawFleetTab draws the tab of a printer in the status bar, in r.
func (a *App) drawFleetTab(c *gfx.Canvas, r image.Rectangle, card FleetCard) {
	pressed := a.isPressed(r)
	fill, ink, sub := lcdCell, lcdInk, lcdDim
	if card.Active {
		fill = card.Color
		ink = inkOn(fill)
		sub = ink
	}
	if pressed {
		fill = color.RGBA{fill.R / 4 * 3, fill.G / 4 * 3, fill.B / 4 * 3, 255}
	}
	c.RoundRect(r, 8, fill)
	in := kit.Inset(r, 10, 3, 8, 3)
	if !card.Active {
		// The colour of the printer, at the left of the tab.
		dot := image.Rect(in.Min.X, in.Min.Y+in.Dy()/2-6, in.Min.X+12, in.Min.Y+in.Dy()/2+6)
		c.RoundRect(dot, 6, card.Color)
		in.Min.X += 18
	}
	lf, sf := gfx.Face(true, 18), gfx.Face(false, 14)
	state := shortState(card.State)
	if card.Note != "" {
		// The note in a word or two, in full ink: "Asks", "Print done", "Error".
		state, _, _ = strings.Cut(card.Note, " · ")
		if !card.Active {
			sub = lcdInk
		}
	}
	half := in.Min.Y + in.Dy()/2 + 1
	c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, half), gfx.Fit(lf, card.Label, in.Dx()), lf, ink, gfx.Left)
	c.Text(image.Rect(in.Min.X, half, in.Max.X, in.Max.Y), gfx.Fit(sf, state, in.Dx()), sf, sub, gfx.Left)
	id := card.ID
	tap := func() {
		switch {
		case !card.Active:
			a.Fleet.Switch(id)
		case a.Printers != nil:
			a.open(pagePrinters) // the printers of the deck, with Stop all
		case a.stopCount() > 0:
			a.askStopAll()
		}
		a.Redraw()
	}
	if a.Record && !card.Active {
		// A remote screen: the tab opens the remote screen of that printer.
		c.Link(r, "/?printer="+id)
	}
	a.addHit(kit.Hit{R: r.Inset(-3), Tap: tap})
	name := card.Name
	if card.Active {
		name = "Printers"
	}
	a.addFocus(name, r, tap)
	a.region(name, r)
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

// sessionFleet is the Fleet of a remote screen: it shows the printers as the touch screen does,
// but a switch there does not change the printer of the touch screen. On a remote screen each card
// of the list is a link to the remote screen of its printer (/?printer=<id>).
type sessionFleet struct {
	Fleet
	a *App
}

func (f sessionFleet) Switch(id string) {}

// askCameraPrinter asks which printer the camera id watches.
func (a *App) askCameraPrinter(id string) {
	var choices []menuItem
	for _, card := range a.Fleet.Cards() {
		card := card
		sub := ""
		if card.Active {
			sub = "Now"
		}
		choices = append(choices, menuItem{card.Label + " · " + card.Name, sub, card.Color, func() {
			if card.Active {
				return
			}
			if err := a.OnCameraPrinter(id, card.ID); err != nil {
				a.Toast("The camera was not moved: " + err.Error())
				return
			}
			a.confirm = &confirmBox{question: "The camera moves to " + card.Name + " after a restart of the deck.", yes: "OK", onYes: func() {}}
		}})
	}
	a.confirm = &confirmBox{question: "Which printer does this camera watch?", choices: choices}
}
