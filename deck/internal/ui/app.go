// Package ui is the full screen application: the screens of the printer on a 480x800 portrait canvas.
package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log"
	"printdeck/kit"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/font"

	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/touch"
)

const (
	BaseW = 480 // the width of the tall touch screen
	BaseH = 800 // the height of the touch screen
	MaxH  = 1200
	wideW = 800 // the size of a wide screen: the touch screen turned to landscape
	wideH = 480
)

// W and H are the size of the screen of the App that draws or handles a touch now: BaseW x BaseH
// for the tall touch screen, more height for a tall phone (App.Height), and wideW x wideH for a
// wide screen (App.Wide). The keyboard is at the bottom, and the rest of the layout is at the
// top. drawMu protects these values.
var (
	W         = BaseW
	H         = BaseH
	contentY1 = H
)

// setSize sets W, H and the positions that depend on them. The caller holds drawMu.
func setSize(s kit.Screen) {
	W, H, contentY1 = s.W, s.H, s.H
	kbX1 = W - 24
	kbY0 = H - 24 - 5*kbKeyH - 4*kbRowGap
	printLCD = image.Rect(16, 16, W-16, 108)
	logoRect = image.Rect(16, 236, W-16, H-236)
}

// Screen is the size of the screen of the App.
func (a *App) Screen() kit.Screen {
	if a.Wide {
		return kit.Screen{W: wideW, H: wideH}
	}
	return kit.Screen{W: BaseW, H: max(BaseH, min(MaxH, a.Height))}
}

// Printer is what the screens need from the printer.
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
	Rates() printer.Rates // the timings of this printer, from its history
	History() []printer.Record
	// RecordPicture is the picture of a finished print. loading is true while it is drawn.
	RecordPicture(r printer.Record) (img image.Image, loading bool)
	// FilePicture is a picture of the G-code of a file with no thumbnail, or nil while it is drawn.
	FilePicture(f printer.File) image.Image
	// SpoolStore is the rolls of filament, or nil.
	SpoolStore() *printer.Spools
	// What the slicer wrote about a file, and the removal of a file.
	FileMeta(path string) gcodemeta.Meta
	DeleteFile(path string) error
	// The G-code terminal of Klipper: its last lines, and a command typed on the Console page.
	Console() []printer.ConsoleLine
	SendConsole(line string)

	// RunAction runs a maintenance action and waits for its end. State shows it while it runs.
	RunAction(name string, lines ...string) error

	// The setup of a new printer: the boards on the USB ports, and a restart of the Klipper service.
	Boards() (boards []board.Info, probing bool)
	ProbeBoards()
	RestartKlipper() error
	// UseConfig writes one of Klipper's example configs (printer.Examples) as printer.cfg, for the
	// board on the port serial, and restarts Klipper.
	UseConfig(file, serial string) error
	// FlashBoard writes the Klipper firmware of the image to the board on device, or to the board
	// of printer.cfg when device is "".
	FlashBoard(device string, progress board.FlashProgress) error
}

type App struct {
	Printer                 Printer
	Setup                   SetupHost // nil when the system has no setup screen, as in Docker
	PreheatTool, PreheatBed int

	Now func() time.Time // replaced in tests

	pages         []page         // the pages over the state screen, the top last
	file          printer.File   // the file of the file page
	record        printer.Record // the print of the record page
	spoolID       string         // the roll of the roll page
	spoolScroll   int            // the scroll position of the Rolls page
	printsScroll  int            // the scroll position of the prints of a roll
	picker        *curaPicker    // the list of the materials of Cura, over everything
	colors        *colorPicker   // the palette of the color of a roll, over everything
	form          *rollForm      // the form of a new roll, over everything
	fileList      fileList       // the scroll position of the Files page
	macroScroll   int            // the scroll position of the Macros page
	menuScroll    map[page]*int  // the scroll positions of the menu pages with more rows than fit
	consoleScroll int            // how far the Console page shows above its newest line
	consoleKB     *keyboard      // the keyboard of the Console page, while it is open
	consoleLast   string         // the last typed command, for the next one
	configScroll  int            // the scroll position of the printer list of the setup
	configAll     bool           // the printer list shows all printers, not only those for the board
	configBoard   board.Info     // the board that the printer list is for
	flash         *flashState    // the flash of the firmware, while it runs and until OK
	created       time.Time      // the start of this App: older action results do not show
	actionSeen    time.Time      // the end of the last action result that this screen showed
	historyScroll int            // the scroll position of the history
	doneSeen      string         // the done screen of this print was closed (its history key)
	away          bool           // Menu on the screen of the print awayFile showed the other screens (away.go)
	awayFile      string
	touchAt       time.Time       // the last touch
	followed      bool            // followPrint saw a frame
	hub           *hub            // shared with the sessions of this App (hub.go)
	ackR          image.Rectangle // the key that the finger tapped last, and when (drawAck)
	ackAt         time.Time
	bar           bool            // the frame has the print bar
	wasBusy       bool            // the printer printed at the last frame
	wasPaused     bool            // the print was paused at the last frame
	setupProbed   bool            // the printer setup started a probe of the USB ports
	confirm       *confirmBox     // modal question
	in            kit.Input       // the touch areas of the frame, and the finger on them
	names         []focusItem     // the keys and the named parts of the frame, for the manual (KeyRect)
	joyArea       image.Rectangle // the joystick pad of the last frame of the Move page
	wake          chan struct{}
	posted        chan func() // changes from other goroutines, run by the UI loop

	tune   bool       // the tune sheet of a running print is open
	swap   *swapState // the filament swap is open
	keypad *keypad    // number entry, over everything
	setup  *setupFlow // the setup screen, over everything

	// Settings of the control screens.
	UnloadMM, LoadMM float64 // filament lengths of the swap

	// Theme is "day", "night", or "auto": night from NightFrom to NightTo o'clock. The Theme row of the Settings page changes it,
	// and OnTheme is told, to save it.
	Theme              string
	NightFrom, NightTo int
	OnTheme            func(string)
	// Clock24 is true for a 24-hour clock, and false for a 12-hour clock. The Clock row of the
	// Settings page changes it, and OnClock is told, to save it.
	Clock24    bool
	OnClock    func(bool)
	JogInvertY bool // up on the joystick moves Y back instead of forward
	// SwapHome is true when a filament change homes the printer while the nozzle heats. The
	// Settings page changes it, and OnSwapHome is told, to save it.
	SwapHome   bool
	OnSwapHome func(bool)
	// Rotate is the turn of the UI on the panel in degrees: 0, 90, 180 or 270. OnRotate saves a new
	// turn and restarts the deck in it; with no OnRotate, the Settings page has no Screen row.
	Rotate   int
	OnRotate func(int)

	joy   *joystick
	zStep float64 // mm per tap of the Z buttons
	zBaby float64 // mm per tap of the Z offset on the first layer screen

	trace       []traceSample // the live speed of the last traceSpan of a print (speedTrace)
	notesScroll int           // the scroll position of the release notes (drawNotes)

	toast   string
	toastAt time.Time

	// Material is the loaded filament, for example "PLA", or "". OnMaterial is told when it changes,
	// to save it.
	Material   string
	OnMaterial func(string)

	// The menu of the printer display (focus.go): the keys of the frame, the heading of the page,
	// the focus of the knob, and the screen of the display.
	focus    []focusItem
	collect  bool // the frame adds its keys to focus
	lcdTitle string
	lcdPage  string
	lcdSel   int
	lcdSaved map[string]int // the cursor of each menu, for the way back
	lcdDepth int            // the depth of the menu: the open pages and dialogs
	knobAt   time.Time
	lcd      printer.LCDScreen
	lcdNotes []printer.LCDRow // rows under the heading of the page (lcdNote)
	// Updater checks for new releases and installs them, or nil.
	Updater Updater

	redrawPending atomic.Bool // redrawSoon asked for a frame

	// LCDFont is the font of the printer display: "5x8" (the default), "6x10", or "" for the
	// large font of Klipper.
	LCDFont string

	// Record makes Run draw each frame on a recording canvas (gfx.NewRecording), for a browser.
	Record bool
	// Height is the height of the tall screen, from BaseH to MaxH. 0 is BaseH.
	Height int
	// Wide draws the screen in landscape, wideW x wideH, for a display that is mounted wide.
	Wide bool
}

type confirmBox struct {
	question, yes string
	no            string // the label of the other button; "" is "Back"
	danger        bool
	onYes         func()
	onNo          func() // or nil
	shared        bool   // a question of the hub, on each screen
}

// Ask shows a question with two answers over the screen, for example when an app asks for access
// to the printer. answer gets true for yes. It is safe to call from any goroutine.
func (a *App) Ask(question, yes, no string, answer func(bool)) {
	a.hub.ask(&confirmBox{question: question, yes: yes, no: no,
		onYes: func() { answer(true) }, onNo: func() { answer(false) }})
}

// Session returns a new App on the same printer and settings, with its own screens: the UI
// for one browser, with the shape of its window. A window wider than high gets the wide screen.
// Its theme changes are saved as those of a.
func (a *App) Session(width, height int) *App {
	s := New()
	s.Wide = width > height
	s.Height = max(BaseH, min(MaxH, height))
	s.Printer, s.Setup = a.Printer, a.Setup
	s.PreheatTool, s.PreheatBed, s.UnloadMM, s.LoadMM, s.JogInvertY = a.PreheatTool, a.PreheatBed, a.UnloadMM, a.LoadMM, a.JogInvertY
	s.Theme, s.NightFrom, s.NightTo, s.OnTheme = a.Theme, a.NightFrom, a.NightTo, a.OnTheme
	s.Clock24, s.OnClock = a.Clock24, a.OnClock
	s.Material, s.OnMaterial = a.Material, a.OnMaterial
	s.Updater = a.Updater
	s.hub = a.hub
	return s
}

// drawMu lets one App draw at a time: the palette of the theme is shared by all.
var drawMu sync.Mutex

func New() *App {
	return &App{hub: newHub(), Now: time.Now, created: time.Now(), LCDFont: "5x8", wake: make(chan struct{}, 1), posted: make(chan func(), 16),
		joy: &joystick{}, zStep: 1, UnloadMM: 80, LoadMM: 70, SwapHome: true, Theme: "auto", NightFrom: 20, NightTo: 7}
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
	a.hub.join(a)
	defer a.hub.leave(a)
	cv := gfx.New(a.Screen().W, a.Screen().H)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		t0 := time.Now()
		if a.Record {
			cv, _ = gfx.NewRecording(a.Screen().W, a.Screen().H)
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
	setSize(a.Screen())
	a.touchAt = a.Now()
	p := image.Pt(int(ev.X*float64(W)+0.5), int(ev.Y*float64(H)+0.5))
	kind := map[touch.Kind]kit.Kind{touch.Down: kit.Down, touch.Move: kit.Move, touch.Up: kit.Up}[ev.Kind]
	a.in.Handle(kit.Event{Kind: kind, P: p, At: a.Now()})
	a.Redraw()
}

// Draw renders one frame and rebuilds the hit list.
func (a *App) Draw(c *gfx.Canvas) {
	drawMu.Lock()
	defer drawMu.Unlock()
	setSize(a.Screen())
	// The touch screen is the screen that the display of the printer follows: its keys are the
	// menu of the display.
	a.showShared()
	drawing, a.focus, a.lcdTitle, a.lcdNotes = a, a.focus[:0], "", a.lcdNotes[:0]
	a.names, a.bar = a.names[:0], false
	defer func() { drawing, a.collect = nil, false }()
	if !a.Record && a.Printer != nil {
		defer a.endFocus(c, a.Printer.State())
	}
	if n := a.isNight(); n != night {
		setNight(n)
	}
	// A flicked list goes on: the fling moves it before the touch areas of this frame replace those
	// of the last one, and the next frame comes soon.
	if a.in.Fling(a.Now()) {
		a.redrawSoon(33 * time.Millisecond)
	}
	a.in.Reset()
	c.Fill(c.Bounds(), bg)
	if a.setup != nil {
		a.drawSetup(c)
		return
	}
	a.collect = !a.Record
	a.drawPrint(c)
	a.collect = false
	a.drawToast(c)
	if a.keypad != nil {
		a.collect, a.focus = !a.Record, a.focus[:0]
		a.drawKeypad(c)
	}
	if a.confirm != nil {
		a.collect, a.focus = !a.Record, a.focus[:0]
		a.drawConfirm(c)
	}
}

func (a *App) addHit(h kit.Hit) { a.in.Add(h) }

func (a *App) isPressed(r image.Rectangle) bool {
	return a.in.Pressed(r)
}

func (a *App) drawConfirm(c *gfx.Canvas) {
	a.in.Reset() // the dialog blocks everything below it
	c.Dim(3)
	box := kit.Center(image.Rect(0, 0, W, contentY1), min(W-60, 560), 280) // in the middle of the screen
	panel(c, box, "CONFIRM")
	cf := a.confirm
	// The question, in lines that fit the box: smaller when it has more than three lines.
	q := image.Rect(box.Min.X+20, box.Min.Y+26, box.Max.X-20, box.Min.Y+170)
	face, lineH := gfx.Face(true, 28), 38
	lines := wrapText(face, cf.question, q.Dx())
	if len(lines) > 3 {
		face, lineH = gfx.Face(true, 22), 30
		lines = wrapText(face, cf.question, q.Dx())
	}
	y := q.Min.Y + (q.Dy()-len(lines)*lineH)/2
	for _, l := range lines {
		c.Text(image.Rect(q.Min.X, y, q.Max.X, y+lineH), l, face, text, gfx.Center)
		y += lineH
	}
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
	a.drawAck(c, r)
	key(c, r, label, fill, fg, a.isPressed(r))
	// The key that the finger tapped flashes, so a key that acts and stays in view shows that it
	// took the tap.
	acked := func() {
		a.ackR, a.ackAt = r, a.Now()
		tap()
	}
	a.addHit(kit.Hit{R: r, Tap: acked})
	a.addFocus(label, r, acked)
	a.region(label, r)
}

// ackTime is how long a tapped key flashes.
const ackTime = 450 * time.Millisecond

// drawAck draws the flash of a key that the finger tapped a moment ago: an orange ring around the
// key that fades out. Draw it before the key, which covers its inside.
func (a *App) drawAck(c *gfx.Canvas, r image.Rectangle) {
	if r != a.ackR || a.ackAt.IsZero() {
		return
	}
	e := a.Now().Sub(a.ackAt)
	if e >= ackTime {
		return
	}
	k := 1 - float64(e)/float64(ackTime)
	mix := func(x, y uint8) uint8 { return uint8(float64(y) + (float64(x)-float64(y))*k) }
	col := color.RGBA{mix(orange.R, bg.R), mix(orange.G, bg.G), mix(orange.B, bg.B), 255}
	c.RoundRect(r.Inset(-4), 7, col)
	a.redrawSoon(33 * time.Millisecond)
}

// grams is the weight of mm millimetres of filament, with the filament diameter of the printer.
// material is the material of the filament; "" is the loaded filament.
func (a *App) grams(mm float64, material string) float64 {
	d := 1.75
	var store *printer.Spools
	if a.Printer != nil {
		d = a.Printer.Machine().Diameter()
		store = a.Printer.SpoolStore()
	}
	if material == "" {
		// The loaded filament: the roll in the printer, with the density of its Cura material.
		if store != nil {
			if sp, ok := store.In(); ok {
				return printer.GramsAt(mm, d, store.Density(sp))
			}
		}
		material = a.Material
	}
	return printer.FilamentGrams(mm, d, material)
}

// weight is the weight of mm millimetres of the loaded filament, for the screen: "12 g".
func (a *App) weight(mm float64) string { return printer.Weight(a.grams(mm, "")) }

// region names a part of the frame: a key, or a part that is no key, such as a display or a list.
// The manual places its callouts with the names (KeyRect).
func (a *App) region(name string, r image.Rectangle) {
	a.names = append(a.names, focusItem{label: lcdText(name), r: r})
}

// textRegion names lines of text at the left of r, in face: the part is as wide as the longest
// line, with 8 pixels more at each side.
func (a *App) textRegion(name string, r image.Rectangle, face font.Face, lines ...string) {
	w := 0
	for _, l := range lines {
		w = max(w, gfx.Width(face, l))
	}
	a.region(name, image.Rect(r.Min.X-8, r.Min.Y, r.Min.X+w+8, r.Max.Y))
}

func lighten(c color.RGBA) color.RGBA {
	f := func(v uint8) uint8 { return v + (255-v)/4 }
	return color.RGBA{f(c.R), f(c.G), f(c.B), 255}
}

// Toast shows a short message at the bottom of the screen for a few seconds.
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

// themeLine tells the theme: day, night, or auto with the hours of the night.
func themeLine(theme string, from, to int) string {
	switch theme {
	case "day":
		return "Day mode"
	case "night":
		return "Night mode"
	}
	return fmt.Sprintf("Auto: night from %d:00 to %d:00", from, to)
}

// clockText is a time on the touch screen: "2:24 PM" on a 12-hour clock, or "14:24".
func clockText(t time.Time, clock24 bool) string {
	if clock24 {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// clockLine tells the clock of the screens.
func clockLine(clock24 bool) string {
	if clock24 {
		return "24 hours: 14:24"
	}
	return "12 hours: 2:24 PM"
}

// toggleClock changes between the 12-hour and the 24-hour clock.
func (a *App) toggleClock() {
	a.Clock24 = !a.Clock24
	a.Toast(clockLine(a.Clock24))
	if a.OnClock != nil {
		a.OnClock(a.Clock24)
	}
}

// cycleTheme goes day, night, auto, day.
func (a *App) cycleTheme() {
	a.Theme = map[string]string{"day": "night", "night": "auto", "auto": "day"}[a.Theme]
	if a.Theme == "" {
		a.Theme = "auto"
	}
	a.Toast(themeLine(a.Theme, a.NightFrom, a.NightTo))
	if a.OnTheme != nil {
		a.OnTheme(a.Theme)
	}
}
