package ui

import (
	"image"
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
	heading(c, "Software update")
	a.backButton(c)
	u := up.Status()
	info := image.Rect(24, 122, W-24, 250)
	window(c, info)
	latest := "—"
	if u.Latest != "" {
		latest = u.Latest
	}
	channel := "STABLE"
	if u.Beta {
		channel = "BETA"
	}
	for i, l := range [][2]string{{"THIS DECK", u.Current}, {"LATEST", latest}, {"CHANNEL", channel}} {
		y := info.Min.Y + 14 + i*36
		c.Text(image.Rect(info.Min.X+16, y, info.Max.X, y+30), l[0], gfx.Face(true, 14), lcdDim, gfx.Left)
		c.Text(image.Rect(info.Min.X+140, y, info.Max.X-16, y+30), gfx.Fit(gfx.Face(true, 19), l[1], info.Dx()-160),
			gfx.Face(true, 19), lcdInk, gfx.Right)
	}

	y := 266
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
	for i, l := range wrapText(face, msg, W-48) {
		if i >= 8 {
			break
		}
		c.Text(image.Rect(24, y, W-24, y+26), l, face, col, gfx.Left)
		y += 26
	}
	if u.Busy == "Downloading" && u.Total > 0 {
		bar := image.Rect(24, 490, W-24, 520)
		window(c, bar)
		w := int(int64(bar.Dx()-8) * u.Done / u.Total)
		c.RoundRect(image.Rect(bar.Min.X+4, bar.Min.Y+4, bar.Min.X+4+w, bar.Max.Y-4), 3, lcdInk)
	}

	half := (W - 48 - 12) / 2
	if u.Busy == "" {
		if u.Newer {
			a.button(c, image.Rect(24, 540, W-24, 620), "Install "+u.Latest, orange, onAcc, func() {
				if st.Busy() {
					a.Toast("Install the update after the print")
					return
				}
				a.confirm = &confirmBox{question: "Install " + u.Latest + "? The Printdeck restarts.", yes: "Install",
					onYes: up.Install}
			})
		}
		a.button(c, image.Rect(24, 636, 24+half, 712), "Check now", raised, text, up.Check)
		beta := "Beta updates: off"
		if u.Beta {
			beta = "Beta updates: on"
		}
		a.button(c, image.Rect(W-24-half, 636, W-24, 712), beta, raised, text, func() { up.SetChannel(!u.Beta) })
	}
}
