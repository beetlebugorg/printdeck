package ui

import (
	"image"
	"time"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The projects of the cloud service on the deck: Owner sign-in on the Cloud page makes the
// passkey of the owner on a phone, and Share on the page of a project uploads its prints and
// shows the page of the project. Both end with a QR code of a link (qrSheet).

// CloudProjects is a CloudLink with the projects of the service. The calls wait for the service:
// the UI runs them in the background.
type CloudProjects interface {
	// OwnerCode returns the page that makes the passkey of the owner, and the end of its code.
	OwnerCode() (url string, expires time.Time, err error)
	// ShareProject uploads the prints of the project of the history with the name, which are not
	// up yet, and returns the page of the project. progress gets the prints that are up.
	ShareProject(name string, progress func(done, total int)) (url string, err error)
}

// qrSheet is a sheet over the screen with the QR code of a link, or the step that runs, or an
// error.
type qrSheet struct {
	title string
	busy  string   // the step that runs, or ""
	err   string   // the error, or ""
	url   string   // the link, at the end
	lines []string // the text next to the QR code
}

// cloudProjects returns the projects of the cloud link, when the deck is connected.
func (a *App) cloudProjects() (CloudProjects, bool) {
	cp, ok := a.Cloud.(CloudProjects)
	return cp, ok && a.Cloud.State().Connected
}

// ownerSignIn asks the service for an owner code, and shows the page of the code as a QR code.
func (a *App) ownerSignIn(cp CloudProjects) {
	sh := &qrSheet{title: "Owner sign-in", busy: "Asking the service for a code…"}
	a.qrSheet = sh
	go func() {
		url, exp, err := cp.OwnerCode()
		a.post(func() {
			sh.busy = ""
			if err != nil {
				sh.err = err.Error()
				return
			}
			sh.url = url
			sh.lines = []string{"Scan with your phone. The page makes your owner passkey, which signs in to your projects.",
				"The code works once, until " + clockText(exp.Local(), a.Clock24) + "."}
		})
	}()
}

// shareProject uploads the prints of the project with the name, and shows the page of the project.
func (a *App) shareProject(cp CloudProjects, name string) {
	sh := &qrSheet{title: name, busy: "Making the project…"}
	a.qrSheet = sh
	go func() {
		url, err := cp.ShareProject(name, func(done, total int) {
			a.post(func() { sh.busy = "Sending the prints: " + projectLine(done) + " of " + projectLine(total) })
		})
		a.post(func() {
			sh.busy = ""
			if err != nil {
				sh.err = err.Error()
				return
			}
			sh.url = url
			sh.lines = []string{"Scan to open the project. There you invite friends and turn on the public address.",
				"Sign in with your owner passkey (Settings, Cloud, Owner sign-in)."}
		})
	}()
}

// drawQRSheet draws the QR sheet over the screen.
func (a *App) drawQRSheet(c *gfx.Canvas) {
	a.in.Reset() // the sheet blocks everything below it
	c.Dim(3)
	sh := a.qrSheet
	h := 300
	narrow := min(W-40, 640) < 560 // a tall screen: the text goes under the QR code
	if sh.url != "" {
		h = 420
		if narrow {
			h = 560
		}
	}
	box := kit.Center(image.Rect(0, 0, W, contentY1), min(W-40, 640), h)
	panel(c, box, "CLOUD")
	in := kit.Inset(box, 20, 26, 20, 20)
	keys := image.Rect(in.Min.X, in.Max.Y-72, in.Max.X, in.Max.Y)
	body := image.Rect(in.Min.X, in.Min.Y, in.Max.X, keys.Min.Y-14)
	closeSheet := func() { a.qrSheet = nil }
	switch {
	case sh.busy != "":
		st := kit.NewStack(body, 8)
		c.Text(st.Next(34), gfx.Fit(gfx.Face(true, 24), sh.title, body.Dx()), gfx.Face(true, 24), text, gfx.Left)
		for _, l := range wrapText(gfx.Face(false, 17), sh.busy, body.Dx()) {
			c.Text(st.Next(26), l, gfx.Face(false, 17), dim, gfx.Left)
		}
		a.redrawSoon(300 * time.Millisecond)
	case sh.err != "":
		st := kit.NewStack(body, 8)
		c.Text(st.Next(34), gfx.Fit(gfx.Face(true, 24), sh.title, body.Dx()), gfx.Face(true, 24), text, gfx.Left)
		for _, l := range wrapText(gfx.Face(false, 17), sh.err, body.Dx()) {
			c.Text(st.Next(26), l, gfx.Face(false, 17), red, gfx.Left)
		}
	default:
		side := min(body.Dy(), 240)
		q := image.Rect(body.Min.X, body.Min.Y, body.Min.X+side, body.Min.Y+side)
		t := image.Rect(q.Max.X+20, body.Min.Y, body.Max.X, body.Max.Y)
		if narrow {
			x := body.Min.X + (body.Dx()-side)/2
			q = image.Rect(x, body.Min.Y, x+side, body.Min.Y+side)
			t = image.Rect(body.Min.X, q.Max.Y+16, body.Max.X, body.Max.Y)
		}
		drawQR(c, q, sh.url)
		a.region("QR code", q)
		c.Link(q, sh.url)
		st := kit.NewStack(t, 6)
		c.Text(st.Next(30), gfx.Fit(gfx.Face(true, 20), sh.title, t.Dx()), gfx.Face(true, 20), text, gfx.Left)
		for _, line := range sh.lines {
			for _, l := range wrapText(gfx.Face(false, 15), line, t.Dx()) {
				c.Text(st.Next(22), l, gfx.Face(false, 15), dim, gfx.Left)
			}
		}
	}
	a.button(c, keys, "Close", raised, text, closeSheet)
}
