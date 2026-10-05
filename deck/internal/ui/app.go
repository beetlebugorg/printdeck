// Package ui is the full screen application: a Music tab and a Print tab on a 480x800 portrait canvas.
package ui

import (
	"context"
	"image"
	"image/color"
	"time"

	"speakerdeck/internal/gfx"
	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
	"speakerdeck/internal/touch"
)

const (
	W, H      = 480, 800
	statusH   = 56
	tabH      = 72
	contentY0 = statusH
	contentY1 = H - tabH
)

var (
	bg      = color.RGBA{0x0b, 0x0d, 0x10, 255}
	surface = color.RGBA{0x1a, 0x1f, 0x27, 255}
	raised  = color.RGBA{0x2a, 0x31, 0x3c, 255}
	text    = color.RGBA{0xf2, 0xf4, 0xf7, 255}
	dim     = color.RGBA{0x8b, 0x93, 0xa1, 255}
	green   = color.RGBA{0x1e, 0xd7, 0x60, 255}
	orange  = color.RGBA{0xff, 0x9f, 0x43, 255}
	red     = color.RGBA{0xff, 0x5c, 0x5c, 255}
	onAcc   = color.RGBA{0x06, 0x10, 0x08, 255}
)

// Music is what the Music tab needs from Spotify.
type Music interface {
	State() spotify.State
	Cover() image.Image
	Toggle()
	Next()
	Previous()
	Seek(time.Duration)
	SetShuffle(bool)
	CycleRepeat()
	ToggleLike() error
	LoadPlaylists() error
	Playlists() []spotify.Playlist
	AddToPlaylist(id string) error
	PlayPlaylist(id string) error
	LinkURL() string // address for the QR code of the login, or ""
}

// Printer is what the Print tab needs from OctoPrint.
type Printer interface {
	State() octoprint.State
	Files() []octoprint.File
	Preview() image.Image // picture of the model that prints, or nil
	LoadFiles()
	Pause()
	Resume()
	Cancel()
	Connect()
	Heat(tool, bed int)
	PrintFile(path string)
}

// Volume is the amplifier.
type Volume interface {
	Volume() int
	Max() int
	Set(int)
}

type Tab int

const (
	TabMusic Tab = iota
	TabPrint
)

type hit struct {
	r    image.Rectangle
	tap  func()
	drag func(frac float64) // slider: called while dragging with 0..1
	end  func(frac float64) // slider: called on release
	// swipe is called after a horizontal swipe: 1 for a swipe to the left (next), -1 to the right (previous).
	swipe func(dir int)
}

type App struct {
	Music                   Music
	Printer                 Printer
	Vol                     Volume
	PreheatTool, PreheatBed int

	Now func() time.Time // replaced in tests

	tab      Tab
	files    bool        // file list open on the Print tab
	confirm  *confirmBox // modal question
	hits     []hit
	pressed  *hit
	swiping  *hit // the swipe area that the finger is on
	swipeDX  int  // how far the finger moved, to draw the cover with it
	dragging *hit
	downAt   image.Point
	dragFrac float64 // while dragging a slider, its value; -1 otherwise
	wake     chan struct{}
	posted   chan func() // changes from other goroutines, run by the UI loop

	picker  *picker // list of playlists on the Music tab
	toast   string
	toastAt time.Time
}

type confirmBox struct {
	question, yes string
	danger        bool
	onYes         func()
}

func New() *App {
	return &App{Now: time.Now, dragFrac: -1, wake: make(chan struct{}, 1), posted: make(chan func(), 16)}
}

// Redraw asks for a new frame. It is safe to call from any goroutine.
func (a *App) Redraw() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Run handles input and draws frames until ctx ends.
func (a *App) Run(ctx context.Context, events <-chan touch.Event, present func(*gfx.Canvas)) {
	cv := gfx.New(W, H)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		a.Draw(cv)
		present(cv)
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			a.handle(ev)
		case f := <-a.posted:
			f()
		case <-a.wake:
		case <-tick.C:
		}
		// Coalesce bursts of events into one frame.
		for more := true; more; {
			select {
			case ev := <-events:
				a.handle(ev)
			default:
				more = false
			}
		}
	}
}

func (a *App) handle(ev touch.Event) {
	p := image.Pt(int(ev.X*W), int(ev.Y*H))
	switch ev.Kind {
	case touch.Down:
		a.downAt = p
		for i := len(a.hits) - 1; i >= 0; i-- {
			h := &a.hits[i]
			if p.In(h.r) {
				hc := *h // the hit list is rebuilt on each frame, so keep a copy
				if h.swipe != nil {
					a.swiping, a.swipeDX = &hc, 0
				} else if h.drag != nil {
					a.dragging = &hc
					a.dragFrac = frac(h.r, p)
					h.drag(a.dragFrac)
				} else {
					a.pressed = &hc
				}
				break
			}
		}
	case touch.Move:
		if a.swiping != nil {
			a.swipeDX = max(-80, min(80, p.X-a.downAt.X))
		} else if a.dragging != nil {
			a.dragFrac = frac(a.dragging.r, p)
			if h := a.find(a.dragging.r); h != nil {
				h.drag(a.dragFrac)
			}
		} else if a.pressed != nil && !p.In(a.pressed.r.Inset(-24)) {
			a.pressed = nil
		}
	case touch.Up:
		if a.swiping != nil {
			dx, dy := p.X-a.downAt.X, p.Y-a.downAt.Y
			h := a.find(a.swiping.r)
			a.swiping, a.swipeDX = nil, 0
			if h != nil && h.swipe != nil && abs(dx) >= 70 && abs(dx) > 2*abs(dy) {
				if dx < 0 {
					h.swipe(1)
				} else {
					h.swipe(-1)
				}
			}
		} else if a.dragging != nil {
			if h := a.find(a.dragging.r); h != nil {
				h.end(frac(h.r, p))
			}
			a.dragging, a.dragFrac = nil, -1
		} else if a.pressed != nil && p.In(a.pressed.r.Inset(-24)) {
			h := a.find(a.pressed.r)
			a.pressed = nil
			if h != nil && h.tap != nil {
				h.tap()
			}
		}
		a.pressed = nil
	}
	a.Redraw()
}

// find returns the current hit with the rectangle r.
func (a *App) find(r image.Rectangle) *hit {
	for i := len(a.hits) - 1; i >= 0; i-- {
		if a.hits[i].r == r {
			return &a.hits[i]
		}
	}
	return nil
}

func frac(r image.Rectangle, p image.Point) float64 {
	return max(0, min(1, float64(p.X-r.Min.X)/float64(r.Dx())))
}

// Draw renders one frame and rebuilds the hit list.
func (a *App) Draw(c *gfx.Canvas) {
	a.hits = a.hits[:0]
	c.Fill(c.Bounds(), bg)
	if a.tab == TabMusic {
		a.drawMusic(c)
	} else {
		a.drawPrint(c)
	}
	a.drawStatus(c)
	a.drawTabs(c)
	a.drawToast(c)
	if a.confirm != nil {
		a.drawConfirm(c)
	}
}

func (a *App) addHit(h hit) *hit {
	a.hits = append(a.hits, h)
	return &a.hits[len(a.hits)-1]
}

func (a *App) isPressed(r image.Rectangle) bool {
	return a.pressed != nil && a.pressed.r == r
}

func (a *App) drawStatus(c *gfx.Canvas) {
	c.Fill(image.Rect(0, 0, W, statusH), bg)
	c.Text(image.Rect(20, 0, 140, statusH), a.Now().Format("15:04"), gfx.Face(true, 26), text, gfx.Left)

	// A short summary of the other tab. Tapping it opens that tab.
	var s string
	var col color.RGBA
	var to Tab
	if a.tab == TabMusic {
		if p := a.Printer.State(); p.Busy() {
			s, col, to = "Printing "+pct(p.Completion), orange, TabPrint
			if p.Paused {
				s = "Paused " + pct(p.Completion)
			}
		}
	} else if m := a.Music.State(); m.Playing && m.Title != "" {
		s, col, to = m.Title, green, TabMusic
	}
	if s != "" {
		r := image.Rect(150, 8, W-12, statusH-8)
		a.addHit(hit{r: r, tap: func() { a.setTab(to) }})
		c.Text(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y), s, gfx.Face(false, 20), col, gfx.Right)
	}
}

func (a *App) setTab(t Tab) {
	a.tab, a.files, a.confirm, a.picker = t, false, nil, nil
	a.Redraw()
}

func (a *App) drawTabs(c *gfx.Canvas) {
	c.Fill(image.Rect(0, contentY1, W, H), surface)
	for i, t := range []struct {
		name string
		col  color.RGBA
	}{{"Music", green}, {"Print", orange}} {
		r := image.Rect(i*W/2, contentY1, (i+1)*W/2, H)
		tab := Tab(i)
		a.addHit(hit{r: r, tap: func() { a.setTab(tab) }})
		col := dim
		if a.tab == tab {
			col = t.col
			c.Fill(image.Rect(r.Min.X+60, r.Min.Y, r.Max.X-60, r.Min.Y+4), col)
		}
		c.Text(r, t.name, gfx.Face(true, 24), col, gfx.Center)
	}
}

func (a *App) drawConfirm(c *gfx.Canvas) {
	a.hits = a.hits[:0] // the dialog blocks everything below it
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			o := c.PixOffset(x, y)
			c.Pix[o], c.Pix[o+1], c.Pix[o+2] = c.Pix[o]/3, c.Pix[o+1]/3, c.Pix[o+2]/3
		}
	}
	box := image.Rect(30, 260, W-30, 540)
	c.RoundRect(box, 24, surface)
	c.Text(image.Rect(box.Min.X+20, box.Min.Y+30, box.Max.X-20, box.Min.Y+150), a.confirm.question, gfx.Face(true, 28), text, gfx.Center)
	cf := a.confirm
	a.button(c, image.Rect(box.Min.X+20, box.Max.Y-100, box.Min.X+(box.Dx()/2)-8, box.Max.Y-20), "Back", raised, text, func() { a.confirm = nil; a.Redraw() })
	yes := green
	if cf.danger {
		yes = red
	}
	a.button(c, image.Rect(box.Min.X+(box.Dx()/2)+8, box.Max.Y-100, box.Max.X-20, box.Max.Y-20), cf.yes, yes, onAcc, func() { a.confirm = nil; cf.onYes(); a.Redraw() })
}

// button draws a rounded button and registers it.
func (a *App) button(c *gfx.Canvas, r image.Rectangle, label string, fill, fg color.RGBA, tap func()) {
	if a.isPressed(r) {
		fill = lighten(fill)
	}
	c.RoundRect(r, 18, fill)
	c.Text(r, label, gfx.Face(true, 26), fg, gfx.Center)
	a.addHit(hit{r: r, tap: tap})
}

func lighten(c color.RGBA) color.RGBA {
	f := func(v uint8) uint8 { return v + (255-v)/4 }
	return color.RGBA{f(c.R), f(c.G), f(c.B), 255}
}

// Toast shows a short message above the tab bar for a few seconds.
func (a *App) Toast(msg string) {
	a.post(func() { a.toast, a.toastAt = msg, a.Now() })
}

// post runs f in the UI loop, which owns the screen state.
func (a *App) post(f func()) {
	select {
	case a.posted <- f:
	default:
	}
	a.Redraw()
}

func (a *App) drawToast(c *gfx.Canvas) {
	if a.toast == "" || a.Now().Sub(a.toastAt) > 3*time.Second {
		return
	}
	r := image.Rect(30, contentY1-64, W-30, contentY1-12)
	c.RoundRect(r, 26, raised)
	c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Max.X-16, r.Max.Y), a.toast, gfx.Face(true, 20), text, gfx.Center)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
