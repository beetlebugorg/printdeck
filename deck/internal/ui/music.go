package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"rsc.io/qr"
	"speakerdeck/internal/gfx"
	"speakerdeck/internal/spotify"
)

func clock(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

// slider draws a fader and registers it. Dragging calls drag, release calls end.
func (a *App) slider(c *gfx.Canvas, hitRect image.Rectangle, f float64, col color.RGBA, drag, end func(float64)) {
	fader(c, hitRect, f, col)
	a.addHit(hit{r: hitRect, drag: drag, end: end})
}

func (a *App) drawMusic(c *gfx.Canvas) {
	st := a.Music.State()
	if a.eqOpen && a.Equalizer != nil {
		a.drawEQ(c)
		return
	}
	if a.tracks != nil {
		a.drawTracks(c)
		return
	}
	if a.picker != nil {
		a.drawPicker(c)
		return
	}

	if !st.Authorized {
		a.drawLink(c)
		return
	}

	// A swipe over the cover and the title: left for the next track, right for the previous one.
	a.addHit(hit{r: image.Rect(0, contentY0, W, 430), swipe: func(dir int) {
		if dir > 0 {
			a.Music.Next()
		} else {
			a.Music.Previous()
		}
	}})

	// The cover, in a display window. It follows the finger during a swipe.
	bezel := image.Rect(82, 66, W-82, 66+316)
	window(c, bezel)
	art := bezel.Inset(8)
	if img := a.Music.Cover(); img != nil && st.Active {
		// Drawn into the window only, so a swiped cover slides behind the bezel.
		inner := &gfx.Canvas{RGBA: c.RGBA.SubImage(art).(*image.RGBA)}
		inner.Cover(art.Add(image.Pt(a.swipeDX, 0)), img)
	} else {
		msg := "NO SIGNAL"
		if !st.Active {
			msg = "NO SIGNAL\n\nPRESS PLAY, CHOOSE\nSPEAKERDECK IN SPOTIFY,\nOR PICK A PLAYLIST"
		}
		for i, line := range splitLines(msg) {
			c.Text(image.Rect(art.Min.X, art.Min.Y+90+i*26, art.Max.X, art.Min.Y+116+i*26), line, gfx.Face(true, 16), lcdInk, gfx.Center)
		}
	}

	c.Text(image.Rect(24, 388, W-24, 414), st.Title, gfx.Face(true, 22), text, gfx.Center)
	c.Text(image.Rect(24, 414, W-24, 434), st.Artist, gfx.Face(false, 16), dim, gfx.Center)

	// Four small keys: playlists, like, add to a playlist, equalizer.
	kw, gap := 80, 12
	x0 := (W - 4*kw - 3*gap) / 2
	small := func(i int, draw func(cx, cy int), tap func()) {
		r := image.Rect(x0+i*(kw+gap), 440, x0+i*(kw+gap)+kw, 440+46)
		pressed := a.isPressed(r)
		key(c, r, "", raised, text, pressed)
		cy := r.Min.Y + (r.Dy()-5)/2
		if pressed {
			cy += 4
		}
		draw(r.Min.X+kw/2, cy)
		a.addHit(hit{r: r, tap: tap})
	}
	small(0, func(cx, cy int) { drawList(c, cx, cy, 13, text) }, func() { a.openPicker(pickPlay) })
	if st.Active && st.URI != "" {
		hc := text
		if st.Liked {
			hc = orange
		}
		small(1, func(cx, cy int) { drawHeart(c, cx, cy+1, 15, hc, st.Liked, raised) }, func() {
			go func() {
				if err := a.Music.ToggleLike(); err != nil {
					a.Toast(errText(err))
				} else if st.Liked {
					a.Toast("Removed from Liked Songs")
				} else {
					a.Toast("Added to Liked Songs")
				}
				a.Redraw()
			}()
		})
		small(2, func(cx, cy int) { drawPlus(c, cx, cy, 12, text) }, func() { a.openPicker(pickAdd) })
	}
	if a.Equalizer != nil {
		ec := text
		if a.Equalizer.Preset() != "Flat" {
			ec = orange // the sound is changed
		}
		small(3, func(cx, cy int) { drawSliders(c, cx, cy, 13, ec) }, func() { a.eqOpen = true })
	}

	// Progress: a slot with a playhead.
	now := a.Now()
	prog := st.Progress(now)
	f := 0.0
	if st.Duration > 0 {
		f = float64(prog) / float64(st.Duration)
	}
	seek := image.Rect(24, 494, W-24, 528)
	if a.dragFrac >= 0 && a.dragging != nil && a.dragging.r == seek {
		f = a.dragFrac
		prog = time.Duration(f * float64(st.Duration))
	}
	playhead(c, seek, f)
	dur := st.Duration
	a.addHit(hit{r: seek, drag: func(float64) {}, end: func(v float64) {
		if st.Duration > 0 {
			a.Music.Seek(time.Duration(v * float64(st.Duration)))
		}
	}})
	c.Text(image.Rect(24, 526, 160, 544), clock(prog), gfx.Face(true, 14), dim, gfx.Left)
	c.Text(image.Rect(W-160, 526, W-24, 544), "-"+clock(dur-prog), gfx.Face(true, 14), dim, gfx.Right)

	// Transport
	tp := image.Rect(12, 552, W-12, 654)
	panel(c, tp, "01  TRANSPORT")
	keys := []struct {
		w    int
		cap  color.RGBA
		draw func(r image.Rectangle, ink color.RGBA)
		tap  func()
		led  int // -1: none, 0 off, 1 on
	}{
		{70, raised, func(r image.Rectangle, ink color.RGBA) { c.Text(r, "SHUF", gfx.Face(true, 14), ink, gfx.Center) }, func() { a.Music.SetShuffle(!st.Shuffle) }, b2i(st.Shuffle)},
		{78, raised, func(r image.Rectangle, ink color.RGBA) { drawSkip(c, r, -1, ink) }, a.Music.Previous, -1},
		{104, orange, func(r image.Rectangle, ink color.RGBA) {
			if st.Playing {
				drawPause(c, r.Inset(8), ink)
			} else {
				drawPlay(c, r.Inset(8), ink)
			}
		}, a.Music.Toggle, -1},
		{78, raised, func(r image.Rectangle, ink color.RGBA) { drawSkip(c, r, 1, ink) }, a.Music.Next, -1},
		{70, raised, func(r image.Rectangle, ink color.RGBA) {
			l := "REP"
			if st.Repeat == "track" {
				l = "REP 1"
			}
			c.Text(r, l, gfx.Face(true, 14), ink, gfx.Center)
		}, a.Music.CycleRepeat, b2i(st.Repeat != "off" && st.Repeat != "")},
	}
	total := 0
	for _, k := range keys {
		total += k.w
	}
	space := (tp.Dx() - 32 - total) / (len(keys) - 1)
	x := tp.Min.X + 16
	for _, k := range keys {
		r := image.Rect(x, tp.Min.Y+22, x+k.w, tp.Max.Y-12)
		pressed := a.isPressed(r)
		key(c, r, "", k.cap, text, pressed)
		face := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-5)
		if pressed {
			face = face.Add(image.Pt(0, 4))
		}
		k.draw(face, text)
		if k.led >= 0 {
			led(c, face.Min.X+12, face.Min.Y+11, k.led == 1)
		}
		a.addHit(hit{r: r, tap: k.tap})
		x += k.w + space
	}

	// Volume: a fader and a readout.
	v := a.Vol.Volume()
	vol := image.Rect(70, 664, W-96, 716)
	if a.dragFrac >= 0 && a.dragging != nil && a.dragging.r == vol {
		v = int(a.dragFrac*float64(a.Vol.Max()) + 0.5)
	}
	vf := float64(max(v, 0)) / float64(max(a.Vol.Max(), 1))
	label(c, image.Rect(24, 664, 70, 716), "Vol", gfx.Left)
	a.slider(c, vol, vf, orange,
		func(f float64) { a.Vol.Set(int(f*float64(a.Vol.Max()) + 0.5)) },
		func(f float64) { a.Vol.Set(int(f*float64(a.Vol.Max()) + 0.5)) })
	ro := image.Rect(W-80, 672, W-20, 708)
	window(c, ro)
	c.Text(ro, fmt.Sprintf("%02d", max(v, 0)), gfx.Face(true, 22), lcdInk, gfx.Center)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	return append(out, cur)
}

func drawPlay(c *gfx.Canvas, r image.Rectangle, col color.RGBA) {
	cx, cy, s := r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2, r.Dx()/5
	c.Polygon([]image.Point{{cx - s + 3, cy - s - 4}, {cx - s + 3, cy + s + 4}, {cx + s + 5, cy}}, col)
}

func drawPause(c *gfx.Canvas, r image.Rectangle, col color.RGBA) {
	cx, cy, s := r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2, r.Dx()/5
	c.RoundRect(image.Rect(cx-s, cy-s-4, cx-s/3, cy+s+4), 2, col)
	c.RoundRect(image.Rect(cx+s/3, cy-s-4, cx+s, cy+s+4), 2, col)
}

// drawSkip draws a triangle with a bar. dir is 1 for next and -1 for previous.
func drawSkip(c *gfx.Canvas, r image.Rectangle, dir int, col color.RGBA) {
	cx, cy := r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2
	d := dir
	c.Polygon([]image.Point{{cx - 12*d, cy - 14}, {cx - 12*d, cy + 14}, {cx + 10*d, cy}}, col)
	bx := cx + 14*d
	c.RoundRect(image.Rect(bx-3, cy-14, bx+3, cy+14), 1, col)
}

func itoa(n int) string { return fmt.Sprint(n) }

func errText(err error) string { return spotify.Friendly(err) }

// drawLink shows a QR code to link the Spotify account with a phone.
func (a *App) drawLink(c *gfx.Canvas) {
	heading(c, "Link Spotify")
	url := a.Music.LinkURL()
	if url == "" {
		c.Text(image.Rect(30, 250, W-30, 420), "Run  speakerdeck auth  on the Pi", gfx.Face(false, 22), dim, gfx.Center)
		return
	}
	code, err := qr.Encode(url, qr.M)
	if err != nil {
		return
	}
	const quiet = 3 // modules of white around the code
	px := 300 / (code.Size + 2*quiet)
	size := px * (code.Size + 2*quiet)
	box := image.Rect((W-size)/2, 150, (W-size)/2+size, 150+size)
	c.RoundRect(box.Inset(-10), 14, color.RGBA{255, 255, 255, 255})
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				r := image.Rect(box.Min.X+(x+quiet)*px, box.Min.Y+(y+quiet)*px, box.Min.X+(x+quiet+1)*px, box.Min.Y+(y+quiet+1)*px)
				c.Fill(r, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	c.Text(image.Rect(24, 480, W-24, 512), "SCAN WITH YOUR PHONE", gfx.Face(true, 20), text, gfx.Center)
	label(c, image.Rect(24, 520, W-24, 540), "The phone warns about the certificate once.", gfx.Center)
	label(c, image.Rect(24, 542, W-24, 562), "Continue, then agree on Spotify.", gfx.Center)
	label(c, image.Rect(24, 600, W-24, 620), url, gfx.Center)
}
