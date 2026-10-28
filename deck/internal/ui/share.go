package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	"rsc.io/qr"

	"printdeck/internal/cloud"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The share link of a print (cloud.Sharer): the Share key on the print page and on the done page
// opens the share sheet. Nothing goes to the cloud before the tap on Make the link in the sheet.
// The sheet then shows the link as a QR code, and Stop sharing.

// ShareLink is the share link of the print now.
type ShareLink interface {
	// Ready reports that the deck is connected to its cloud service.
	Ready() bool
	State() cloud.ShareState
	// Start makes the link of the print now, or of the print that ended.
	Start()
	Stop()
	// SetReady sets the flag Ready to pick up of the share page.
	SetReady(ready bool)
}

// canShare reports that the deck shows the Share key: it is connected to its service.
func (a *App) canShare() bool { return a.Share != nil && a.Share.Ready() }

// shareKey draws the Share key. It is lit while the print has a link.
func (a *App) shareKey(c *gfx.Canvas, r image.Rectangle) {
	fill, ink, label := raised, text, "Share"
	if s := a.Share.State(); s.Active {
		fill, ink, label = keyOn, keyOnInk, "Shared"
	}
	a.button(c, r, label, fill, ink, func() { a.shareSheet = true })
}

// drawShareSheet is the share sheet over the screen.
func (a *App) drawShareSheet(c *gfx.Canvas) {
	a.in.Reset() // the sheet blocks everything below it
	c.Dim(3)
	s := a.Share.State()
	h := 300
	if s.Active && s.URL != "" {
		h = 420
	}
	box := kit.Center(image.Rect(0, 0, W, contentY1), min(W-40, 640), h)
	panel(c, box, "SHARE")
	in := kit.Inset(box, 20, 26, 20, 20)
	keys := image.Rect(in.Min.X, in.Max.Y-72, in.Max.X, in.Max.Y)
	body := image.Rect(in.Min.X, in.Min.Y, in.Max.X, keys.Min.Y-14)
	close := func() { a.shareSheet = false }
	switch {
	case s.Busy:
		c.Text(body, "Making the link…", gfx.Face(true, 24), text, gfx.Center)
		a.redrawSoon(300 * time.Millisecond)
		a.button(c, keys, "Close", raised, text, close)
	case s.Active && s.URL != "":
		side := min(body.Dy(), 240)
		q := image.Rect(body.Min.X, body.Min.Y, body.Min.X+side, body.Min.Y+side)
		url, title := s.URL, "Scan to watch the print"
		if a.shareOwner && s.OwnerURL != "" {
			url, title = s.OwnerURL, "Scan to add photos"
		}
		drawQR(c, q, url)
		a.region("QR code", q)
		t := image.Rect(q.Max.X+20, body.Min.Y, body.Max.X, body.Max.Y)
		st := kit.NewStack(t, 6)
		c.Text(st.Next(30), title, gfx.Face(true, 20), text, gfx.Left)
		if a.shareOwner {
			for _, l := range wrapText(gfx.Face(false, 15), "Your phone opens the page with the owner part: photos, a note, and Ready to pick up. Keep this code to yourself.", t.Dx()) {
				c.Text(st.Next(22), l, gfx.Face(false, 15), dim, gfx.Left)
			}
		} else {
			for _, l := range wrapText(gfx.Face(false, 15), strings.TrimPrefix(s.URL, "https://"), t.Dx()) {
				c.Text(st.Next(22), l, gfx.Face(false, 15), dim, gfx.Left)
			}
			line := "The link stops 24 hours after the print ends."
			if s.Ended && !s.Expires.IsZero() {
				line = "The link stops at " + clockText(s.Expires, a.Clock24) + " " + dayName(s.Expires, a.Now()) + "."
			}
			for _, l := range wrapText(gfx.Face(false, 15), line, t.Dx()) {
				c.Text(st.Next(22), l, gfx.Face(false, 15), dim, gfx.Left)
			}
		}
		if s.Err != "" {
			for _, l := range wrapText(gfx.Face(false, 15), s.Err, t.Dx())[:1] {
				c.Text(st.Next(22), l, gfx.Face(false, 15), red, gfx.Left)
			}
		}
		k := kit.Cols(keys, 10, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1))
		a.button(c, k[0], "Stop sharing", redCap, onRed, func() {
			a.shareSheet, a.shareOwner = false, false
			a.Share.Stop()
		})
		qrLabel := "Add photos"
		if a.shareOwner {
			qrLabel = "Watch link"
		}
		a.button(c, k[1], qrLabel, raised, text, func() { a.shareOwner = !a.shareOwner })
		fill, ink := raised, text
		if s.Ready {
			fill, ink = keyOn, keyOnInk
		}
		ready := s.Ready
		a.button(c, k[2], "Ready", fill, ink, func() { a.Share.SetReady(!ready) })
		a.button(c, k[3], "Close", raised, text, func() { a.shareSheet, a.shareOwner = false, false })
	default:
		st := kit.NewStack(body, 8)
		c.Text(st.Next(34), "Share this print?", gfx.Face(true, 26), text, gfx.Left)
		msg := "The deck makes a link to a page with the camera, the progress and the time lapse. " +
			"Anybody with the link can watch. The link stops 24 hours after the print ends."
		if s.Err != "" {
			msg = s.Err
		}
		col := dim
		if s.Err != "" {
			col = red
		}
		for _, l := range wrapText(gfx.Face(false, 17), msg, body.Dx()) {
			c.Text(st.Next(26), l, gfx.Face(false, 17), col, gfx.Left)
		}
		k := kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1))
		a.button(c, k[0], "Back", raised, text, close)
		a.button(c, k[1], "Make the link", green, onAcc, func() { a.Share.Start() })
	}
}

// dayName is "today", "tomorrow", or the weekday of t.
func dayName(t, now time.Time) string {
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today"
	case t.Sub(now) < 48*time.Hour && t.Weekday() == now.Add(24*time.Hour).Weekday():
		return "tomorrow"
	}
	return t.Weekday().String()
}

// A QR code is dark on light in both themes: some phones read only that.
var qrGround, qrInk = color.RGBA{255, 255, 255, 255}, color.RGBA{0, 0, 0, 255}

// drawQR draws the QR code of text in the square r, dark on a light ground.
func drawQR(c *gfx.Canvas, r image.Rectangle, text string) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return
	}
	c.RoundRect(r, 8, qrGround)
	n := code.Size + 8 // the quiet zone: 4 modules on each side
	cell := r.Dx() / n
	if cell < 1 {
		return
	}
	x0 := r.Min.X + (r.Dx()-cell*code.Size)/2
	y0 := r.Min.Y + (r.Dy()-cell*code.Size)/2
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				c.Fill(image.Rect(x0+x*cell, y0+y*cell, x0+(x+1)*cell, y0+(y+1)*cell), qrInk)
			}
		}
	}
}
