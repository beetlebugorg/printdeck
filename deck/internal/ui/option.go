package ui

import (
	"printdeck/internal/gfx"
)

// A row of a menu page opens a page. It does not change a setting. The option page shows one
// setting: a toggle row for a setting that is on or off, or a check row for each choice with a
// check at the choice now. The rows of a setting with settings of its own, such as Time lapse,
// open a page of their own with the toggle at the top.

// option is a setting on the option page.
type option struct {
	title string
	note  string // a line under the rows, or ""
	// For a setting that is on or off: the title and the line of the toggle row, and get and set.
	toggle    string
	toggleSub func() string
	get       func() bool
	set       func(on bool)
	// For a setting with choices: the choices, the choice now, and pick.
	choices []menuItem // the title and the line of each choice: tap is not used
	current func() int
	pick    func(i int)
}

// openOption opens the option page for the setting o.
func (a *App) openOption(o *option) {
	a.option = o
	a.open(pageOption)
}

// drawOption is the option page.
func (a *App) drawOption(c *gfx.Canvas) {
	o := a.option
	if o == nil {
		a.back()
		return
	}
	f := a.navPage(c, o.title)
	var g menuGroup
	if o.choices != nil {
		cur := o.current()
		for i, ch := range o.choices {
			i := i
			g.items = append(g.items, menuItem{ch.title, ch.sub, dim, func() { o.pick(i); a.Redraw() }})
			g.checks = append(g.checks, i == cur)
		}
	} else {
		sub := ""
		if o.toggleSub != nil {
			sub = o.toggleSub()
		}
		on := o.get()
		g.items = []menuItem{{o.toggle, sub, dim, func() { o.set(!on); a.Redraw() }}}
		g.checks = []bool{on}
	}
	g.note = o.note
	a.menuGroups(c, a.listArea(f), f.wide, 86, 12, []menuGroup{g})
}

// onOff is "On" or "Off".
func onOff(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}
