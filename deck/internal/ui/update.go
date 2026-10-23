package ui

import (
	"fmt"
	"image"
	"printdeck/kit"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// Updater checks for new releases of Printdeck, and installs one. The Printdeck OS image has one.
type Updater interface {
	Status() UpdateStatus
	Check()               // checks now, in the background
	Install()             // downloads and installs the latest release, in the background
	SetChannel(beta bool) // the channel: stable, or beta (the betas and the stable releases)
}

// UpdateStatus is the state of the updates.
type UpdateStatus struct {
	Current  string // the version of this Printdeck
	Beta     bool   // the channel is beta
	Checked  bool   // a check is done
	Checking bool
	Latest   string // the newest release of the channel, or ""
	Notes    string // the notes of the newest release
	Newer    bool   // the newest release is newer than this Printdeck
	Busy     string // the step: "Downloading", the steps of the install (system.Step...), "Restarting", or ""
	Done     int64  // the bytes of the step, of Total; 0 of 0 for a step with no size
	Total    int64
	Err      string
}

// updateLine is the line of the Software update row of Machine.
func updateLine(u UpdateStatus) (string, bool) {
	switch {
	case u.Busy != "":
		return u.Busy + "…", true
	case u.Newer:
		return u.Latest + " is ready to install", true
	case u.Err != "":
		return "The check failed", false
	case u.Checking || !u.Checked:
		return "Checking…", false
	}
	return "Up to date · " + u.Current, false
}

// drawUpdate is the Software update page.
func (a *App) drawUpdate(c *gfx.Canvas, st printer.State) {
	up := a.Updater
	f := a.navPage(c, "Software update")
	u := up.Status()
	// Tall: the versions, the notes, the progress, Install, and the keys. Wide: the versions and the
	// keys in the left column; the notes, the progress and Install at the right.
	var info, notes, bar, install, keys image.Rectangle
	if f.wide {
		// The versions, the notes, the progress and the keys; Install at the right of the row of
		// actions.
		content, acts := f.split()
		r := kit.Rows(content, 12, kit.Px(100), kit.Fr(1), kit.Px(26), kit.Px(56))
		info, notes, bar, keys = r[0], r[1], r[2], r[3]
		install = kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 8), 16, kit.Px(128), kit.Fr(1), kit.Px(30), kit.Px(80), kit.Px(76))
		info, notes, bar, install, keys = r[0], r[1], r[2], r[3], r[4]
	}
	window(c, info)
	a.region("Versions", info)
	lineH := min(36, (info.Dy()-20)/3)
	latest := "—"
	if u.Latest != "" {
		latest = u.Latest
	}
	channel := "STABLE"
	if u.Beta {
		channel = "BETA"
	}
	for i, l := range [][2]string{{"THIS DECK", u.Current}, {"LATEST", latest}, {"CHANNEL", channel}} {
		y := info.Min.Y + 10 + i*lineH + (lineH-30)/2
		c.Text(image.Rect(info.Min.X+16, y, info.Max.X, y+30), l[0], gfx.Face(true, 14), lcdDim, gfx.Left)
		c.Text(image.Rect(info.Min.X+140, y, info.Max.X-16, y+30), l[1],
			gfx.Face(true, 19), lcdInk, gfx.Right)
	}

	face := gfx.Face(false, 17)
	msg := ""
	switch {
	case u.Busy != "":
		msg = u.Busy + "…"
		if u.Total > 0 {
			msg += fmt.Sprintf(" %d %%", u.Done*100/u.Total)
		}
		msg += " The Printdeck restarts into the new version when it is done."
	case u.Err != "":
		msg = u.Err
	case u.Newer:
		msg = strings.TrimSpace(u.Notes)
		if msg == "" {
			msg = "Release " + u.Latest + "."
		}
	case u.Checked:
		msg = "This Printdeck has the latest version."
	default:
		msg = "Checking for a new version…"
	}
	col := text
	if u.Err != "" {
		col = red
	}
	s := kit.NewStack(notes, 0)
	for _, l := range wrapText(face, msg, notes.Dx()) {
		if s.Y()+26 > notes.Max.Y {
			break
		}
		c.Text(s.Next(26), l, face, col, gfx.Left)
	}
	a.textRegion("Notes", image.Rect(notes.Min.X, notes.Min.Y, notes.Max.X, s.Y()), face, wrapText(face, msg, notes.Dx())...)
	if u.Busy != "" && u.Total > 0 {
		window(c, bar)
		w := int(int64(bar.Dx()-8) * u.Done / u.Total)
		c.RoundRect(image.Rect(bar.Min.X+4, bar.Min.Y+4, bar.Min.X+4+w, bar.Max.Y-4), 3, lcdInk)
	}

	if u.Busy == "" {
		if u.Newer {
			a.button(c, install, "Install "+u.Latest, orange, onAcc, func() {
				if st.Busy() {
					a.Toast("Install the update after the print")
					return
				}
				a.confirm = &confirmBox{question: "Install " + u.Latest + "? The Printdeck restarts.", yes: "Install",
					onYes: up.Install}
			})
		}
		k := kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1))
		a.button(c, k[0], "Check now", raised, text, up.Check)
		beta := "Beta updates: off"
		if u.Beta {
			beta = "Beta updates: on"
		}
		a.button(c, k[1], beta, raised, text, func() { up.SetChannel(!u.Beta) })
	}
}
