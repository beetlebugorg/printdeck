// Package ui is the full screen application: a Music tab and a Print tab on a 480x800 portrait canvas.
package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log"
	"strings"
	"sync"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
)

const (
	W         = 480
	BaseH     = 800 // the height of the touch screen
	MaxH      = 1200
	statusH   = 56
	tabH      = 72
	contentY0 = statusH
)

// H is the height of the screen of the App that draws or handles a touch now: BaseH for the touch
// screen, more for a tall phone (App.Height). The tab bar and the keyboard are at the bottom, and
// the rest of the layout is at the top. drawMu protects these values.
var (
	H         = BaseH
	contentY1 = H - tabH
)

// setHeight sets H and the positions that depend on it. The caller holds drawMu.
func setHeight(h int) {
	if h < BaseH {
		h = BaseH
	}
	H, contentY1 = h, h-tabH
	kbY0 = h - 24 - 5*kbKeyH - 4*kbRowGap
	listBottom = contentY1 - 8
}

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
	PlayPlaylist(uri string) error
	LoadTracks(uri string, offset int) ([]spotify.Track, int, error)
	PlayTrack(context, track string) error
	LinkURL() string // address for the QR code of the login, or ""
}

// Printer is what the Print tab needs from the printer.
type Printer interface {
	State() printer.State
	Files() []printer.File
	Preview() image.Image   // picture of the model that prints, or nil
	SetViewLayer(layer int) // shows the model up to a layer; a negative layer follows the print
	ViewLayer() int         // the layer of the scrubber, or -1 when it follows the print
	LoadFiles()
	Pause()
	Resume()
	Cancel()
	Connect()
	Heat(tool, bed int)
	PrintFile(path string)

	// Control, like the menus of the printer's own display. These wait for Klipper and return its refusal.
	Gcode(lines ...string) error
	Home(axes ...string) error
	Jog(x, y, z float64, speed int) error
	SetToolTemp(t int) error
	SetBedTemp(t int) error
	Extrude(mm float64, speed int) error
	Fan(percent int) error
	MotorsOff() error
	SpeedFactor(p int) error
	FlowFactor(p int) error
	BabyStep(mm float64) error
	EmergencyStop() error
	FirmwareRestart() error

	// What the printer has (version, modules, macros), and the finished prints, newest first.
	Machine() printer.Machine
	History() []printer.Record
	// What the slicer wrote about a file, and the removal of a file.
	FileMeta(path string) gcodemeta.Meta
	DeleteFile(path string) error

	// The setup of a new printer: the boards on the USB ports, and a restart of the Klipper service.
	Boards() (boards []board.Info, probing bool)
	ProbeBoards()
	RestartKlipper() error
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
	// vertical makes a slider (drag) follow the finger up and down.
	vertical bool
	// scroll is called while the finger drags on a list, with the movement in pixels (positive is
	// down). A touch that does not move calls tapAt with the position instead.
	scroll func(dy int)
	tapAt  func(p image.Point)
	// pad follows the finger from Down to Up, for the joystick: phase is padDown, padMove, or padUp.
	pad func(p image.Point, phase int)
}

const (
	padDown = iota
	padMove
	padUp
)

type App struct {
	Music                   Music
	Printer                 Printer
	Vol                     Volume
	Equalizer               Equalizer
	Setup                   SetupHost // nil when the system has no setup screen, as in Docker
	PreheatTool, PreheatBed int

	Now func() time.Time // replaced in tests

	tab           Tab
	pages         []page       // the pages of the Print tab over its state screen, the top last
	file          printer.File // the file of the file page
	fileList      fileList     // the scroll position of the Files page
	macroScroll   int          // the scroll position of the Macros page
	historyScroll int          // the scroll position of the history
	doneSeen      string       // the done screen of this print was closed (its history key)
	setupProbed   bool         // the printer setup started a probe of the USB ports
	confirm       *confirmBox  // modal question
	hits          []hit
	pressed       *hit
	scrolling     *hit // the list that the finger is on
	padding       *hit // the joystick that the finger is on
	lastY         int
	scrollMoved   bool
	swiping       *hit // the swipe area that the finger is on
	swipeDX       int  // how far the finger moved, to draw the cover with it
	dragging      *hit
	downAt        image.Point
	dragFrac      float64 // while dragging a slider, its value; -1 otherwise
	wake          chan struct{}
	posted        chan func() // changes from other goroutines, run by the UI loop

	picker *picker    // list of playlists on the Music tab
	tracks *trackList // the songs of one playlist
	eqOpen bool       // the equalizer is open on the Music tab
	tune   bool       // the tune sheet of a running print is open
	swap   *swapState // the filament swap is open
	keypad *keypad    // number entry, over everything
	setup  *setupFlow // the setup screen, over everything

	// Settings of the control screens.
	UnloadMM, LoadMM float64 // filament lengths of the swap

	// Theme is "day", "night", or "auto": night from NightFrom to NightTo o'clock. Tapping the clock changes it,
	// and OnTheme is told, to save it.
	Theme              string
	NightFrom, NightTo int
	OnTheme            func(string)
	JogInvertY         bool // up on the joystick moves Y back instead of forward

	joy    *joystick
	zStep  float64 // mm per tap of the Z buttons
	zBaby  float64 // mm per tap of the Z offset on the first layer screen
	fanSet int     // the last fan setting from the screen, -1 if none

	// The tune sheet, counted from the start of the print in tuneFile.
	tuneFile            string
	tuneSpeed, tuneFlow int
	tuneZ               float64
	toast               string
	toastAt             time.Time

	// Material is the loaded filament, for example "PLA", or "". OnMaterial is told when it changes,
	// to save it.
	Material   string
	OnMaterial func(string)

	// Record makes Run draw each frame on a recording canvas (gfx.NewRecording), for a browser.
	Record bool
	// Height is the height of the screen, from BaseH to MaxH. 0 is BaseH.
	Height int
}

type confirmBox struct {
	question, yes string
	no            string // the label of the other button; "" is "Back"
	danger        bool
	onYes         func()
	onNo          func() // or nil
}

// Ask shows a question with two answers over the screen, for example when an app asks for access
// to the printer. answer gets true for yes. It is safe to call from any goroutine.
func (a *App) Ask(question, yes, no string, answer func(bool)) {
	a.post(func() {
		a.confirm = &confirmBox{question: question, yes: yes, no: no,
			onYes: func() { answer(true) }, onNo: func() { answer(false) }}
	})
}

// Session returns a new App on the same printer, music and settings, with its own screens: the UI
// for one browser. Its theme changes are saved as those of a.
func (a *App) Session(height int) *App {
	s := New()
	s.Height = max(BaseH, min(MaxH, height))
	s.Music, s.Printer, s.Vol, s.Equalizer, s.Setup = a.Music, a.Printer, a.Vol, a.Equalizer, a.Setup
	s.PreheatTool, s.PreheatBed, s.UnloadMM, s.LoadMM, s.JogInvertY = a.PreheatTool, a.PreheatBed, a.UnloadMM, a.LoadMM, a.JogInvertY
	s.Theme, s.NightFrom, s.NightTo, s.OnTheme = a.Theme, a.NightFrom, a.NightTo, a.OnTheme
	s.Material, s.OnMaterial = a.Material, a.OnMaterial
	s.tab = a.tab
	return s
}

// drawMu lets one App draw at a time: the palette of the theme is shared by all.
var drawMu sync.Mutex

func New() *App {
	return &App{Now: time.Now, dragFrac: -1, wake: make(chan struct{}, 1), posted: make(chan func(), 16),
		joy: &joystick{}, zStep: 1, fanSet: -1, UnloadMM: 80, LoadMM: 70, tuneSpeed: 100, tuneFlow: 100, Theme: "auto", NightFrom: 20, NightTo: 7}
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
	cv := gfx.New(W, max(BaseH, a.Height))
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		t0 := time.Now()
		if a.Record {
			cv, _ = gfx.NewRecording(W, max(BaseH, a.Height))
		}
		a.Draw(cv)
		t1 := time.Now()
		present(cv)
		// A slow frame shows as a pause on the screen. Log it, to find where the time goes.
		if d := time.Since(t0); d > 100*time.Millisecond {
			log.Printf("slow frame: draw %v, present %v", t1.Sub(t0).Round(time.Millisecond), time.Since(t1).Round(time.Millisecond))
		}
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
	// The handlers use the positions of the layout, which depend on the height of this App.
	drawMu.Lock()
	defer drawMu.Unlock()
	setHeight(a.Height)
	p := image.Pt(int(ev.X*W+0.5), int(ev.Y*float64(H)+0.5))
	switch ev.Kind {
	case touch.Down:
		a.downAt = p
		for i := len(a.hits) - 1; i >= 0; i-- {
			h := &a.hits[i]
			if p.In(h.r) {
				hc := *h // the hit list is rebuilt on each frame, so keep a copy
				if h.pad != nil {
					a.padding = &hc
					h.pad(p, padDown)
				} else if h.swipe != nil {
					a.swiping, a.swipeDX = &hc, 0
				} else if h.scroll != nil {
					a.scrolling, a.lastY, a.scrollMoved = &hc, p.Y, false
				} else if h.drag != nil {
					a.dragging = &hc
					a.dragFrac = fracOf(&hc, p)
					h.drag(a.dragFrac)
				} else {
					a.pressed = &hc
				}
				break
			}
		}
	case touch.Move:
		if a.padding != nil {
			if h := a.find(a.padding.r); h != nil {
				h.pad(p, padMove)
			}
		} else if a.scrolling != nil {
			if abs(p.Y-a.downAt.Y) > 12 {
				a.scrollMoved = true
			}
			if a.scrollMoved {
				if h := a.find(a.scrolling.r); h != nil {
					h.scroll(a.lastY - p.Y)
				}
			}
			a.lastY = p.Y
		} else if a.swiping != nil {
			a.swipeDX = max(-80, min(80, p.X-a.downAt.X))
		} else if a.dragging != nil {
			a.dragFrac = fracOf(a.dragging, p)
			if h := a.find(a.dragging.r); h != nil {
				h.drag(a.dragFrac)
			}
		} else if a.pressed != nil && !p.In(a.pressed.r.Inset(-24)) {
			a.pressed = nil
		}
	case touch.Up:
		if a.padding != nil {
			// The finger may lift after the screen changed: the joystick must stop all the same.
			a.padding.pad(p, padUp)
			a.padding = nil
		} else if a.scrolling != nil {
			h := a.find(a.scrolling.r)
			moved := a.scrollMoved
			a.scrolling = nil
			if h != nil && !moved && h.tapAt != nil {
				h.tapAt(p)
			}
		} else if a.swiping != nil {
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
				h.end(fracOf(h, p))
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

// fracOf is where a finger is on a slider, from 0 to 1.
func fracOf(h *hit, p image.Point) float64 {
	if h.vertical {
		return max(0, min(1, float64(p.Y-h.r.Min.Y)/float64(h.r.Dy())))
	}
	return max(0, min(1, float64(p.X-h.r.Min.X)/float64(h.r.Dx())))
}

// Draw renders one frame and rebuilds the hit list.
func (a *App) Draw(c *gfx.Canvas) {
	drawMu.Lock()
	defer drawMu.Unlock()
	setHeight(a.Height)
	if n := a.isNight(); n != night {
		setNight(n)
	}
	a.hits = a.hits[:0]
	c.Fill(c.Bounds(), bg)
	if a.setup != nil {
		a.drawSetup(c)
		return
	}
	if a.tab == TabMusic {
		a.drawMusic(c)
	} else {
		a.drawPrint(c)
	}
	a.drawStatus(c)
	a.drawTabs(c)
	a.drawToast(c)
	if a.keypad != nil {
		a.drawKeypad(c)
	}
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

// drawStatus is the display strip at the top: the time, and what the other tab is doing.
func (a *App) drawStatus(c *gfx.Canvas) {
	c.Fill(image.Rect(0, 0, W, statusH), bg)
	win := image.Rect(10, 8, W-10, statusH-6)
	// The Stop key while the printer prints, moves or heats, on every screen.
	estop := a.estopVisible()
	if estop {
		win.Max.X = W - 108
	}
	window(c, win)
	if estop {
		defer a.drawEstop(c)
	}
	c.Text(image.Rect(win.Min.X+12, win.Min.Y, win.Min.X+90, win.Max.Y), a.Now().Format("15:04"), gfx.Face(true, 24), lcdInk, gfx.Left)
	// The clock is also the key for the theme: day, night, auto.
	c.Text(image.Rect(win.Min.X+92, win.Min.Y, win.Min.X+150, win.Max.Y), strings.ToUpper(a.Theme), gfx.Face(true, 13), lcdDim, gfx.Left)
	a.addHit(hit{r: image.Rect(0, 0, 160, statusH), tap: a.cycleTheme})

	// A short summary of the other tab. Tapping it opens that tab.
	var s string
	var to Tab
	if a.tab == TabMusic {
		if p := a.Printer.State(); p.Busy() {
			s, to = "PRINT "+pct(p.Completion), TabPrint
			if p.Paused {
				s = "PAUSED " + pct(p.Completion)
			}
		}
	} else if m := a.Music.State(); m.Playing && m.Title != "" {
		s, to = "♪ "+m.Title, TabMusic
	}
	if s != "" {
		r := image.Rect(160, win.Min.Y, win.Max.X-12, win.Max.Y)
		a.addHit(hit{r: r, tap: func() { a.setTab(to) }})
		c.Text(r, s, gfx.Face(false, 18), lcdInk, gfx.Right)
	}
}

func (a *App) setTab(t Tab) {
	a.tab, a.pages, a.confirm, a.picker, a.tracks, a.eqOpen = t, nil, nil, nil, nil, false
	a.tune, a.swap, a.keypad = false, nil, nil
	a.Redraw()
}

// drawTabs is the mode switch at the bottom: two keys, the LED of the active one is lit.
func (a *App) drawTabs(c *gfx.Canvas) {
	c.Fill(image.Rect(0, contentY1, W, H), bg)
	c.Fill(image.Rect(0, contentY1, W, contentY1+1), edge)
	for i, name := range []string{"Music", "Print"} {
		r := image.Rect(16+i*(W/2-8), contentY1+12, (i+1)*(W/2-8), H-10)
		tab := Tab(i)
		active := a.tab == tab
		cap, ink := raised, text
		if active {
			cap, ink = keyOn, keyOnInk
		}
		key(c, r, "", cap, ink, a.isPressed(r))
		face := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-5)
		led(c, face.Min.X+22, face.Min.Y+face.Dy()/2, active)
		c.Text(image.Rect(face.Min.X+36, face.Min.Y, face.Max.X-12, face.Max.Y), fmt.Sprintf("%02d %s", i+1, strings.ToUpper(name)), gfx.Face(true, 18), ink, gfx.Center)
		a.addHit(hit{r: r, tap: func() { a.setTab(tab) }})
	}
}

func (a *App) drawConfirm(c *gfx.Canvas) {
	a.hits = a.hits[:0] // the dialog blocks everything below it
	c.Dim(3)
	box := image.Rect(30, 260, W-30, 540)
	panel(c, box, "CONFIRM")
	cf := a.confirm
	c.Text(image.Rect(box.Min.X+20, box.Min.Y+30, box.Max.X-20, box.Min.Y+150), cf.question, gfx.Face(true, 28), text, gfx.Center)
	no := cf.no
	if no == "" {
		no = "Back"
	}
	a.button(c, image.Rect(box.Min.X+20, box.Max.Y-100, box.Min.X+(box.Dx()/2)-8, box.Max.Y-20), no, raised, text, func() {
		a.confirm = nil
		if cf.onNo != nil {
			cf.onNo()
		}
		a.Redraw()
	})
	yes, ink := green, onAcc
	if cf.danger {
		yes, ink = redCap, onRed
	}
	a.button(c, image.Rect(box.Min.X+(box.Dx()/2)+8, box.Max.Y-100, box.Max.X-20, box.Max.Y-20), cf.yes, yes, ink, func() { a.confirm = nil; cf.onYes(); a.Redraw() })
}

// button draws a key and registers it. fill is the colour of the cap.
func (a *App) button(c *gfx.Canvas, r image.Rectangle, label string, fill, fg color.RGBA, tap func()) {
	key(c, r, label, fill, fg, a.isPressed(r))
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
	window(c, r)
	c.Text(image.Rect(r.Min.X+16, r.Min.Y, r.Max.X-16, r.Max.Y), strings.ToUpper(a.toast), gfx.Face(true, 16), lcdInk, gfx.Center)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// isNight says if the night palette is wanted now.
func (a *App) isNight() bool {
	switch a.Theme {
	case "night":
		return true
	case "day":
		return false
	}
	h := a.Now().Hour()
	if a.NightFrom > a.NightTo { // over midnight, like 20 to 7
		return h >= a.NightFrom || h < a.NightTo
	}
	return h >= a.NightFrom && h < a.NightTo
}

// cycleTheme goes day, night, auto, day.
func (a *App) cycleTheme() {
	a.Theme = map[string]string{"day": "night", "night": "auto", "auto": "day"}[a.Theme]
	if a.Theme == "" {
		a.Theme = "auto"
	}
	msg := map[string]string{"day": "Day mode", "night": "Night mode", "auto": fmt.Sprintf("Auto: night from %d:00 to %d:00", a.NightFrom, a.NightTo)}[a.Theme]
	a.Toast(msg)
	if a.OnTheme != nil {
		a.OnTheme(a.Theme)
	}
}
