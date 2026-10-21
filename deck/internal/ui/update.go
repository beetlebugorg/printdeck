package ui

import (
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
	Busy     string // "Downloading", "Installing", or ""
	Done     int64  // the bytes of the download
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
	f := a.page(c, "Software update")
	u := up.Status()
	// Tall: the versions, the notes, the progress, Install, and the keys. Wide: the versions and the
	// keys in the left column; the notes, the progress and Install at the right.
	var info, notes, bar, install, keys image.Rectangle
	if f.wide {
		l := kit.Rows(f.aside(), 12, kit.Fr(1), kit.Px(60))
		info, keys = l[0], l[1]
		r := kit.Rows(f.body(), 16, kit.Fr(1), kit.Px(30), kit.Px(80))
		notes, bar, install = r[0], r[1], r[2]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 0), 16, kit.Px(128), kit.Px(208), kit.Px(30), kit.Px(80), kit.Px(76))
		info, notes, bar, install, keys = r[0], r[1], r[2], r[3], r[4]
	}
	window(c, info)
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
		c.Text(image.Rect(info.Min.X+140, y, info.Max.X-16, y+30), gfx.Fit(gfx.Face(true, 19), l[1], info.Dx()-160),
			gfx.Face(true, 19), lcdInk, gfx.Right)
	}

	face := gfx.Face(false, 17)
	msg := ""
	switch {
	case u.Busy != "":
		msg = u.Busy + "… The Printdeck restarts into the new version when it is done."
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
	if u.Busy == "Downloading" && u.Total > 0 {
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
