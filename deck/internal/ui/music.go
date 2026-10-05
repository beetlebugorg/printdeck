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

// slider draws a bar and registers it. Dragging calls drag, release calls end.
func (a *App) slider(c *gfx.Canvas, hitRect image.Rectangle, f float64, col color.RGBA, drag, end func(float64)) {
	track := image.Rect(hitRect.Min.X, hitRect.Min.Y+hitRect.Dy()/2-5, hitRect.Max.X, hitRect.Min.Y+hitRect.Dy()/2+5)
	c.RoundRect(track, 5, raised)
	fill := track
	fill.Max.X = track.Min.X + int(f*float64(track.Dx()))
	c.RoundRect(fill, 5, col)
	cx, cy := fill.Max.X, track.Min.Y+5
	c.RoundRect(image.Rect(cx-11, cy-11, cx+11, cy+11), 11, text)
	a.addHit(hit{r: hitRect, drag: drag, end: end})
}

func (a *App) drawMusic(c *gfx.Canvas) {
	st := a.Music.State()
	mid := W / 2
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
	a.addHit(hit{r: image.Rect(0, contentY0, W, 460), swipe: func(dir int) {
		if dir > 0 {
			a.Music.Next()
		} else {
			a.Music.Previous()
		}
	}})

	// Cover art. It follows the finger during a swipe.
	art := image.Rect(80, 64, 80+320, 64+320).Add(image.Pt(a.swipeDX, 0))
	if img := a.Music.Cover(); img != nil && st.Active {
		c.Cover(art, img)
	} else {
		c.RoundRect(art, 12, surface)
		msg := "Nothing is playing"
		if !st.Active {
			msg = "Press play, or choose\n“Speakerdeck” in Spotify"
		}
		if !st.Active {
			msg = "Press play, or choose\n“Speakerdeck” in Spotify,\nor pick a playlist below"
		}
		for i, line := range splitLines(msg) {
			c.Text(image.Rect(art.Min.X, art.Min.Y+130+i*36, art.Max.X, art.Min.Y+166+i*36), line, gfx.Face(false, 22), dim, gfx.Center)
		}
	}

	c.Text(image.Rect(30, 392, W-30, 430), st.Title, gfx.Face(true, 28), text, gfx.Center)
	c.Text(image.Rect(30, 430, W-30, 458), st.Artist, gfx.Face(false, 20), dim, gfx.Center)

	// Choose a playlist to play.
	list := image.Rect(mid-132, 462, mid-132+72, 462+48)
	drawList(c, list.Min.X+list.Dx()/2, list.Min.Y+list.Dy()/2, 16, dim)
	a.addHit(hit{r: list, tap: func() { a.openPicker(pickPlay) }})

	// Like and add to a playlist.
	if st.Active && st.URI != "" {
		heart := image.Rect(mid-36, 462, mid-36+72, 462+48)
		hc := dim
		if st.Liked {
			hc = green
		}
		drawHeart(c, heart.Min.X+heart.Dx()/2, heart.Min.Y+heart.Dy()/2+2, 21, hc, st.Liked, bg)
		a.addHit(hit{r: heart, tap: func() {
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
		}})
		plus := image.Rect(mid+60, 462, mid+60+72, 462+48)
		drawPlus(c, plus.Min.X+plus.Dx()/2, plus.Min.Y+plus.Dy()/2, 16, dim)
		a.addHit(hit{r: plus, tap: func() { a.openPicker(pickAdd) }})
	}

	// Progress
	now := a.Now()
	prog := st.Progress(now)
	f := 0.0
	if st.Duration > 0 {
		f = float64(prog) / float64(st.Duration)
	}
	if a.dragFrac >= 0 && a.dragging != nil && a.dragging.r.Min.Y < 560 {
		f = a.dragFrac
		prog = time.Duration(f * float64(st.Duration))
	}
	dur := st.Duration
	a.slider(c, image.Rect(40, 512, W-40, 552), f, green,
		func(float64) {},
		func(v float64) {
			if st.Duration > 0 {
				a.Music.Seek(time.Duration(v * float64(st.Duration)))
			}
		})
	c.Text(image.Rect(40, 552, 160, 578), clock(prog), gfx.Face(false, 18), dim, gfx.Left)
	c.Text(image.Rect(W-160, 552, W-40, 578), "-"+clock(dur-prog), gfx.Face(false, 18), dim, gfx.Right)

	// Transport
	const cy = 626
	icon := func(cx int, w int) image.Rectangle { return image.Rect(cx-w/2, cy-w/2, cx+w/2, cy+w/2) }
	press := func(r image.Rectangle) color.RGBA {
		if a.isPressed(r) {
			return text
		}
		return color.RGBA{0xc8, 0xcd, 0xd6, 255}
	}

	shuf := icon(mid-190, 64)
	col := dim
	if st.Shuffle {
		col = green
	}
	c.Text(shuf, "Shuf", gfx.Face(true, 20), col, gfx.Center)
	a.addHit(hit{r: shuf, tap: func() { a.Music.SetShuffle(!st.Shuffle) }})

	prev := icon(mid-100, 72)
	drawSkip(c, prev, -1, press(prev))
	a.addHit(hit{r: prev, tap: a.Music.Previous})

	pp := icon(mid, 84)
	fill := green
	if a.isPressed(pp) {
		fill = lighten(fill)
	}
	c.RoundRect(pp, 42, fill)
	if st.Playing {
		drawPause(c, pp, onAcc)
	} else {
		drawPlay(c, pp, onAcc)
	}
	a.addHit(hit{r: pp, tap: a.Music.Toggle})

	next := icon(mid+100, 72)
	drawSkip(c, next, 1, press(next))
	a.addHit(hit{r: next, tap: a.Music.Next})

	rep := icon(mid+190, 64)
	col, label := dim, "Rep"
	switch st.Repeat {
	case "context":
		col = green
	case "track":
		col, label = green, "Rep 1"
	}
	c.Text(rep, label, gfx.Face(true, 20), col, gfx.Center)
	a.addHit(hit{r: rep, tap: a.Music.CycleRepeat})

	// Volume
	v := a.Vol.Volume()
	if a.dragFrac >= 0 && a.dragging != nil && a.dragging.r.Min.Y > 660 {
		v = int(a.dragFrac*float64(a.Vol.Max()) + 0.5)
	}
	vf := float64(max(v, 0)) / float64(max(a.Vol.Max(), 1))
	c.Text(image.Rect(24, 672, 100, 724), "Vol", gfx.Face(true, 20), dim, gfx.Left)
	a.slider(c, image.Rect(80, 672, W-90, 724), vf, text,
		func(f float64) { a.Vol.Set(int(f*float64(a.Vol.Max()) + 0.5)) },
		func(f float64) { a.Vol.Set(int(f*float64(a.Vol.Max()) + 0.5)) })
	c.Text(image.Rect(W-84, 672, W-20, 724), fmt.Sprintf("%d", max(v, 0)), gfx.Face(true, 20), text, gfx.Right)
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
	c.Text(image.Rect(30, 76, W-30, 124), "Link Spotify", gfx.Face(true, 32), text, gfx.Center)
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
	c.Text(image.Rect(30, 480, W-30, 516), "Scan with your phone camera", gfx.Face(true, 22), text, gfx.Center)
	c.Text(image.Rect(30, 520, W-30, 552), "The phone warns about the certificate once.", gfx.Face(false, 18), dim, gfx.Center)
	c.Text(image.Rect(30, 548, W-30, 580), "Choose to continue, then agree on Spotify.", gfx.Face(false, 18), dim, gfx.Center)
	c.Text(image.Rect(30, 600, W-30, 630), url, gfx.Face(false, 16), dim, gfx.Center)
}
