package ui

import (
	"fmt"
	"image"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/timelapse"
	"printdeck/kit"
)

// The player of a time lapse video (pagePlayer), and the still of a video on the record page and
// the done page. The player reads one frame at a time from the file, and decodes it only at the
// size of the screen. A remote screen gets the JPEG of the file. The video plays only while the
// player is the top page: the deck closes the file when the page closes.

// lapsePlayer is the state of the player.
type lapsePlayer struct {
	name  string // the name of the video
	title string // the name of the print

	mov    *timelapse.Movie // nil until the page draws, and after the page closes
	err    error            // the open error of the video
	n, fps int              // the count of frames and the frames a second

	pos      int  // the frame that shows
	playing  bool // the video plays
	autoplay bool // the video plays when the file opens
	at       int  // while the video plays: the frame at the time from
	from     time.Time

	pic       *timelapse.Picture // the picture of the frame picPos at the width picW
	picPos    int
	picW      int
	picFailed bool // the frame picPos cannot be read
}

// play starts the video at the frame that shows, or at the start after the last frame.
func (p *lapsePlayer) play(now time.Time) {
	if p.n < 2 {
		return
	}
	if p.pos >= p.n-1 {
		p.pos = 0
	}
	p.playing, p.at, p.from = true, p.pos, now
}

// pause stops the video at the frame of the time now.
func (p *lapsePlayer) pause(now time.Time) {
	p.update(now)
	p.playing = false
}

// update moves the video to the frame of the time now. The video stops at its last frame.
func (p *lapsePlayer) update(now time.Time) {
	if !p.playing || p.fps < 1 {
		return
	}
	p.pos = p.at + int(now.Sub(p.from)*time.Duration(p.fps)/time.Second)
	if p.pos >= p.n-1 {
		p.pos, p.playing = max(0, p.n-1), false
	}
}

// seek shows the frame at the place f of the video, from 0 to 1. A video that plays goes on from
// there.
func (p *lapsePlayer) seek(f float64, now time.Time) {
	if p.n < 1 {
		return
	}
	p.pos = max(0, min(p.n-1, int(f*float64(p.n-1)+0.5)))
	p.at, p.from = p.pos, now
}

// wait is the time from now to the next frame of a video that plays.
func (p *lapsePlayer) wait(now time.Time) time.Duration {
	if !p.playing || p.fps < 1 {
		return 0
	}
	next := p.from.Add(time.Duration(p.pos-p.at+1) * time.Second / time.Duration(p.fps))
	return max(time.Millisecond, next.Sub(now))
}

// frac is the place of the frame that shows, from 0 to 1.
func (p *lapsePlayer) frac() float64 {
	if p.n < 2 {
		return 0
	}
	return float64(p.pos) / float64(p.n-1)
}

// close closes the file and stops the video.
func (p *lapsePlayer) close() {
	if p.mov != nil {
		p.mov.Close()
	}
	p.mov, p.err, p.playing, p.pic = nil, nil, false, nil
}

// clock is a time of a video as minutes and seconds, for example "0:21".
func clock(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// openPlayer opens the player with the video name of the print file.
func (a *App) openPlayer(name, file string) {
	if a.player != nil {
		a.player.close()
	}
	a.player = &lapsePlayer{name: name, title: file, autoplay: true}
	a.open(pagePlayer)
}

// closeHiddenPlayer closes the file of the player when its page is not the top page.
func (a *App) closeHiddenPlayer() {
	if a.player != nil && a.player.mov != nil && a.topPage() != pagePlayer {
		a.player.close()
	}
}

// loadPlayer opens the file of the player. It reports false when the video is gone or bad.
func (a *App) loadPlayer(p *lapsePlayer) bool {
	if p.mov != nil {
		return true
	}
	if p.err != nil {
		return false
	}
	path, ok := "", false
	if a.TimeLapse != nil {
		path, ok = a.TimeLapse.VideoPath(p.name)
	}
	if !ok {
		p.err = errVideoGone
		return false
	}
	m, err := timelapse.OpenMovie(path)
	if err != nil {
		p.err = err
		return false
	}
	p.mov, p.n, p.fps = m, m.Len(), m.FPS
	p.pos = min(p.pos, p.n-1)
	if p.autoplay {
		p.autoplay = false
		p.play(a.Now())
	}
	return true
}

// errVideoGone is the error of a video that the deck removed.
var errVideoGone = fmt.Errorf("the video is gone")

// framePicture returns the picture of frame i at the width w of screen pixels, and keeps it. It
// returns nil for a frame that cannot be read.
func (p *lapsePlayer) framePicture(i, w int) *timelapse.Picture {
	if p.pic != nil && p.picPos == i && p.picW == w || p.picFailed && p.picPos == i {
		return p.pic
	}
	pic, err := p.mov.Frame(i, w)
	p.pic, p.picPos, p.picW, p.picFailed = pic, i, w, err != nil
	return pic
}

// drawPlayer is the player page: the video as large as it fits, the position bar, the frames and
// the length, and Play or Pause.
func (a *App) drawPlayer(c *gfx.Canvas) {
	p := a.player
	head := "Time lapse"
	if p != nil && p.title != "" {
		head = fitTitle(p.title, a.navFrame().head.Dx()-200)
	}
	f := a.navPage(c, head)
	var view, bar, info, keys image.Rectangle
	if f.wide {
		content, acts := f.split()
		r := kit.Rows(content, 8, kit.Fr(1), kit.Px(36))
		view = r[0]
		k := kit.Cols(acts, 12, kit.Fr(3), kit.Fr(2))
		info, keys = k[0], k[1]
		bar = r[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 12, 0, 8), 12, kit.Fr(1), kit.Px(40), kit.Px(28), kit.Px(84))
		view, bar, info, keys = r[0], r[1], r[2], r[3]
	}
	window(c, view)
	a.region("Video", view)
	pic := view.Inset(4)
	if p == nil || !a.loadPlayer(p) {
		msg := "THE VIDEO IS GONE"
		if p != nil && p.err != nil && p.err != errVideoGone {
			msg = "THE VIDEO CANNOT BE READ"
		}
		c.Text(pic, msg, gfx.Face(true, 18), lcdDim, gfx.Center)
		key(c, keys, "Play", surface, dim, false)
		a.region("Play", keys)
		return
	}
	now := a.Now()
	p.update(now)

	// The picture, as large as it fits. The decode is at the width of the picture in pixels of
	// the screen of the deck.
	k := min(float64(pic.Dx())/float64(p.mov.W), float64(pic.Dy())/float64(p.mov.H))
	r := kit.Center(pic, int(float64(p.mov.W)*k), int(float64(p.mov.H)*k))
	w := a.stillWidth(r)
	if dw := timelapse.DecodeWidth(p.mov.W, w); dw < w {
		// A decode that is a little narrower than the picture: the picture gets the size of the
		// decode, and the deck draws it with no scale.
		r = kit.Center(pic, r.Dx()*dw/w, r.Dy()*dw/w)
		w = dw
	}
	if fr := p.framePicture(p.pos, w); fr != nil {
		c.Cover(r, fr)
	} else {
		c.Text(pic, "THE FRAME CANNOT BE READ", gfx.Face(true, 18), lcdDim, gfx.Center)
	}

	// The position bar: a tap or a drag shows that frame.
	slider := kit.Inset(bar, 14, 0, 14, 0)
	a.region("Position", slider)
	playhead(c, slider, p.frac())
	seek := func(f float64) {
		p.seek(f, a.Now())
		a.Redraw()
	}
	a.addHit(kit.Hit{R: slider, Drag: seek, End: seek})

	line := fmt.Sprintf("Frame %d of %d · %s of %s", p.pos+1, p.n, clock(time.Duration(p.pos)*time.Second/time.Duration(p.fps)),
		clock(p.mov.Length()))
	face := gfx.Face(false, 16)
	c.Text(info, gfx.Fit(face, line, info.Dx()), face, dim, gfx.Left)
	a.region("Frames", info)

	if p.playing {
		a.button(c, keys, "Pause", raised, text, func() { p.pause(a.Now()) })
		a.redrawSoon(p.wait(now))
	} else {
		a.button(c, keys, "Play", orange, onAcc, func() { p.play(a.Now()); a.Redraw() })
	}
}

// lapseStill is a video for the record page and the done page: its frames, its length and its
// last frame as a still picture. The App keeps the still of one video.
type lapseStill struct {
	name   string
	gone   bool // the deck removed the video
	frames int
	length time.Duration
	still  *timelapse.Picture // nil for a bad video
	w      int                // the width of the still in screen pixels
}

// videoStill returns the still of the video name at the width w of screen pixels.
func (a *App) videoStill(name string, w int) lapseStill {
	path, ok := a.TimeLapse.VideoPath(name)
	if !ok {
		return lapseStill{name: name, gone: true}
	}
	if s := a.still; s.name == name && s.w == w && !s.gone {
		return s
	}
	s := lapseStill{name: name, w: w}
	if m, err := timelapse.OpenMovie(path); err == nil {
		s.frames, s.length = m.Len(), m.Length()
		s.still, _ = m.Frame(m.Len()-1, w)
		m.Close()
	}
	a.still = s
	return s
}

// stillLine is the line of a video for the details of a print: its length and its frames.
func stillLine(s lapseStill) string {
	switch {
	case s.gone:
		return "Removed"
	case s.still == nil:
		return "Cannot be read"
	}
	return fmt.Sprintf("%s · %d frames", clock(s.length), s.frames)
}

// drawStill draws the still of a video in a window r.
func (a *App) drawStill(c *gfx.Canvas, r image.Rectangle, s lapseStill) {
	window(c, r)
	a.region("Still", r)
	pic := r.Inset(4)
	if s.gone || s.still == nil {
		c.Text(pic, map[bool]string{true: "REMOVED", false: "NO PICTURE"}[s.gone], gfx.Face(true, 14), lcdDim, gfx.Center)
		return
	}
	b := s.still.Bounds()
	k := min(float64(pic.Dx())/float64(b.Dx()), float64(pic.Dy())/float64(b.Dy()))
	c.Cover(kit.Center(pic, int(float64(b.Dx())*k), int(float64(b.Dy())*k)), s.still)
}

// stillWidth is the width of a picture of r in pixels of the screen of the deck.
func (a *App) stillWidth(r image.Rectangle) int {
	w := r.Dx()
	if a.Scale[1] > 0 {
		w = w * a.Scale[0] / a.Scale[1]
	}
	return w
}
