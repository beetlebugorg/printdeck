package ui

import (
	"fmt"

	"printdeck/internal/gfx"
)

// The page Prints from apps (Settings > Prints from apps): "Start prints from the slicer", the
// toggle "Signed prints only", and the trusted slicers. A slicer gets trust when it pairs ("Get a
// key" in zslice). A tap on a slicer asks to remove it.

// signingLine is the line of the Prints from apps row.
func signingLine(remote, on bool, n int) string {
	start := "Prints start from the slicer"
	if !remote {
		start = "The deck asks before each print"
	}
	slicers := "no trusted slicers"
	switch {
	case n == 1:
		slicers = "1 trusted slicer"
	case n > 1:
		slicers = fmt.Sprintf("%d trusted slicers", n)
	}
	if on {
		return start + " · only signed prints · " + slicers
	}
	return start + " · " + slicers
}

// drawSigning is the Signed prints page.
func (a *App) drawSigning(c *gfx.Canvas) {
	f := a.navPage(c, "Prints from apps")
	s := a.Signing
	on := s.SignedOnly()
	remote := s.RemoteStart()
	keys := s.Keys()
	start := menuGroup{
		items: []menuItem{{"Start prints from the slicer", "Print in zslice starts the print here", dim, func() {
			if err := s.SetRemoteStart(!remote); err != nil {
				a.Toast("The setting did not save: " + err.Error())
			}
			a.Redraw()
		}}},
		checks: []bool{remote},
		note:   "On: a Print in zslice starts the print at once, after zslice asks if the bed is clear. Off (for a farm): the deck asks on its screen, and a person at the deck presses Print.",
	}
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
		note:   "With it on, a file with no good signature of a trusted slicer does not go in the queue, and apps can send no G-code commands and no printer.cfg.",
	}
	groups := []menuGroup{start, toggle}
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
