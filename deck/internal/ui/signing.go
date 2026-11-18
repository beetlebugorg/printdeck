package ui

import (
	"fmt"

	"printdeck/internal/gfx"
)

// The Signed prints page (Settings > Signed prints): the toggle "Signed prints only", and the
// trusted slicers. A slicer gets trust when it pairs ("Get a key" in zslice). A tap on a slicer
// asks to remove it.

// signingLine is the line of the Signed prints row.
func signingLine(on bool, n int) string {
	slicers := "no trusted slicers"
	switch {
	case n == 1:
		slicers = "1 trusted slicer"
	case n > 1:
		slicers = fmt.Sprintf("%d trusted slicers", n)
	}
	if on {
		return "Only signed prints · " + slicers
	}
	return "Off · each slicer can send prints · " + slicers
}

// drawSigning is the Signed prints page.
func (a *App) drawSigning(c *gfx.Canvas) {
	f := a.navPage(c, "Signed prints")
	s := a.Signing
	on := s.SignedOnly()
	keys := s.Keys()
	toggle := menuGroup{
		items: []menuItem{{"Signed prints only", "Queue only the files of the trusted slicers", dim, func() {
			if !on && len(keys) == 0 {
				a.confirm = infoBox("SIGNED PRINTS", "No slicer is trusted yet. Pair zslice first: Get a key in its printer settings.")
				return
			}
			if err := s.SetSignedOnly(!on); err != nil {
				a.Toast("The setting did not save: " + err.Error())
			}
			a.Redraw()
		}}},
		checks: []bool{on},
		note:   "With it on, a file with no good signature of a trusted slicer does not go in the queue, and the deck refuses G-code commands from apps.",
	}
	groups := []menuGroup{toggle}
	if len(keys) > 0 {
		list := menuGroup{heading: "TRUSTED SLICERS"}
		for _, k := range keys {
			k := k
			list.items = append(list.items, menuItem{k.App, "Key " + k.ID + " · paired " + fileDate(k.Added.Local(), a.Now(), a.Clock24), dim, func() {
				a.confirm = &confirmBox{question: "Stop the trust of " + gfx.Fit(gfx.Face(true, 28), k.App, 300) + " (key " + k.ID + ")?",
					yes: "Remove", danger: true, onYes: func() {
						if err := s.Remove(k.ID); err != nil {
							a.Toast(err.Error())
						}
					}}
			}})
		}
		groups = append(groups, list)
	}
	a.menuGroups(c, a.listArea(f), f.wide, 60, 12, groups)
}

// ShowRejected shows on each screen of the deck that the deck did not queue a file from an app,
// and why. It is safe to call from any goroutine.
func (a *App) ShowRejected(name, reason string) {
	a.hub.ask(infoBox("FILE REJECTED", name+"\n\n"+reason))
}
