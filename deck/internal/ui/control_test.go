package ui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/gcodemeta"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/touch"
)

// recPrinter records the commands that the screens send.
type recPrinter struct {
	*DemoPrinter
	mu    sync.Mutex
	calls []string
}

func (r *recPrinter) rec(s string) error {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
	return nil
}
func (r *recPrinter) Gcode(l ...string) error { return r.rec("gcode " + strings.Join(l, "; ")) }
func (r *recPrinter) Home(ax ...string) error { return r.rec(fmt.Sprint("home ", ax)) }
func (r *recPrinter) SetToolTemp(t int) error { return r.rec(fmt.Sprint("tool ", t)) }
func (r *recPrinter) SetBedTemp(t int) error  { return r.rec(fmt.Sprint("bed ", t)) }
func (r *recPrinter) Jog(x, y, z float64, s int) error {
	return r.rec(fmt.Sprintf("jog %.2f %.2f %.2f %d", x, y, z, s))
}
func (r *recPrinter) FirmwareRestart() error  { return r.Gcode("FIRMWARE_RESTART") }
func (r *recPrinter) MotorsOff() error        { return r.rec("motors off") }
func (r *recPrinter) SpeedFactor(p int) error { return r.rec(fmt.Sprint("speed ", p)) }
func (r *recPrinter) PrintFile(path string)   { r.rec("print " + path) }

func (r *recPrinter) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// wait waits for the commands, which run in their own goroutines.
func (r *recPrinter) wait(n int) []string {
	for i := 0; i < 100 && len(r.Calls()) < n; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	return r.Calls()
}

func newPrintApp(st printer.State) (*App, *recPrinter, *gfx.Canvas) {
	pr := &recPrinter{DemoPrinter: &DemoPrinter{View: -1, State_: st}}
	a := New()
	a.Printer = pr
	return a, pr, gfx.New(W, BaseH)
}

func tapAt(a *App, cv *gfx.Canvas, x, y int) {
	a.Draw(cv)
	ev(a, touch.Down, x, y)
	ev(a, touch.Up, x, y)
}

var idle = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 25, Bed: 25}

func TestJoySpeed(t *testing.T) {
	if joySpeed(0.1) != 0 || joySpeed(joyDead) != joyMinSpeed || math.Abs(joySpeed(1)-joyMaxSpeed) > 1e-9 || joySpeed(0.6) >= joySpeed(0.9) {
		t.Errorf("speeds %v %v %v", joySpeed(0.1), joySpeed(0.6), joySpeed(1))
	}
}

func TestJoystickMovesWhileHeld(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.pages = []page{pageMove}
	a.Draw(cv)
	jr := a.joyArea.Dx() / 2
	jx, jy := a.joyArea.Min.X+jr, a.joyArea.Min.Y+jr
	// Hold the pad at the right edge: X+ at full speed, then let go.
	ev(a, touch.Down, jx+jr-2, jy)
	time.Sleep(3*joyTick + joyTick/2)
	ev(a, touch.Up, jx+jr-2, jy)
	n := len(pr.Calls())
	time.Sleep(2 * joyTick)
	calls := pr.Calls()
	if n < 3 || len(calls) > n+1 {
		t.Fatalf("%d moves while held, %d after; %v", n, len(calls), calls)
	}
	var x, y, z float64
	var s int
	fmt.Sscanf(calls[0], "jog %f %f %f %d", &x, &y, &z, &s)
	if x < 5 || math.Abs(y) > 0.2 || z != 0 || s < 2500 {
		t.Errorf("first move %q: want about 7.5 mm in X at 3000 mm/min", calls[0])
	}
	// Up on the pad is Y+.
	a.Draw(cv)
	ev(a, touch.Down, jx, jy-jr+2)
	time.Sleep(joyTick + joyTick/2)
	ev(a, touch.Up, jx, jy-jr+2)
	calls = pr.wait(len(calls) + 1)
	fmt.Sscanf(calls[len(calls)-1], "jog %f %f", &x, &y)
	if y <= 0 || math.Abs(x) > 0.2 {
		t.Errorf("up gave %q, want +Y", calls[len(calls)-1])
	}
}

func TestNoMovesWhilePrinting(t *testing.T) {
	st := idle
	st.Printing, st.Text = true, "Printing"
	a, pr, cv := newPrintApp(st)
	a.pages = []page{pageMove}
	a.Draw(cv)
	jr := a.joyArea.Dx() / 2
	jx, jy := a.joyArea.Min.X+jr, a.joyArea.Min.Y+jr
	ev(a, touch.Down, jx+jr-2, jy)
	time.Sleep(2 * joyTick)
	ev(a, touch.Up, jx+jr-2, jy)
	tapAt(a, cv, 60, 169) // Home all
	time.Sleep(50 * time.Millisecond)
	if c := pr.Calls(); len(c) != 0 {
		t.Errorf("commands while printing: %v", c)
	}
}

func TestKeypadSetsTemperature(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.Draw(cv)
	tapAt(a, cv, 100, 74) // the nozzle gauge of the status screen on the idle screen
	if a.keypad == nil {
		t.Fatal("no keypad")
	}
	// Type 2 4 5 with the keys, then Set. Keys: 3 columns from x 48, rows from y 228, 80 apart.
	kw := (W - 48 - 48 - 16) / 3
	key := func(col, row int) { tapAt(a, cv, 48+col*(kw+8)+kw/2, 24+204+row*80+36) }
	key(0, 3)                      // C
	key(1, 0)                      // 2
	key(0, 1)                      // 4
	key(1, 1)                      // 5
	tapAt(a, cv, W-100, 670-84+32) // Set
	if c := pr.wait(1); len(c) != 1 || c[0] != "tool 245" {
		t.Errorf("calls %v, want tool 245", c)
	}
	if a.keypad != nil {
		t.Error("keypad still open")
	}
}

func TestFilamentSwapSteps(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.swap = &swapState{}
	tapLabel(t, a, cv, "PETG  240°")
	if c := pr.wait(1); c[0] != "tool 240" || a.swap.step != swapHeat {
		t.Fatalf("after PETG: %v, step %d", c, a.swap.step)
	}
	pr.State_.Tool = 239 // hot enough: the next draw goes on to unloading
	a.Draw(cv)
	a.Draw(cv)
	if a.swap.step != swapUnload {
		t.Fatalf("step %d, want unload", a.swap.step)
	}
	tapLabel(t, a, cv, "Unload")
	tapLabel(t, a, cv, "Load")
	c := pr.wait(3)
	if len(c) != 3 || c[1] != "gcode M83; G1 E10 F300; G1 E-80 F1500; M82" || c[2] != "gcode M83; G1 E70 F600; G1 E20 F150; M82" {
		t.Errorf("calls %q", c)
	}
	if a.swap.step != swapCheck {
		t.Errorf("step %d, want check", a.swap.step)
	}
}

// The firmware restart is on the Machine page, after a question.
func TestFirmwareRestartOnMachinePage(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.pages = []page{pageMachine}
	a.Draw(cv)
	tapAt(a, cv, W-100, 745) // Firmware restart
	if a.confirm == nil {
		t.Fatal("no confirmation")
	}
	tapAt(a, cv, 340, 500) // the Reset key of the dialog
	if c := pr.wait(1); len(c) != 1 || c[0] != "gcode FIRMWARE_RESTART" {
		t.Errorf("calls %v", c)
	}
}

// ev sends a touch event at x, y in the units of the UI.
func ev(a *App, k touch.Kind, x, y int) {
	a.handle(touch.Event{Kind: k, X: float64(x) / float64(a.Screen().W), Y: float64(y) / float64(a.Screen().H)})
}

// tapLabel draws a frame and taps the key with the label.
func tapLabel(t *testing.T, a *App, cv *gfx.Canvas, label string) {
	t.Helper()
	a.Draw(cv)
	for _, f := range a.focus {
		if f.label == lcdText(label) {
			p := f.r.Min.Add(f.r.Size().Div(2))
			ev(a, touch.Down, p.X, p.Y)
			ev(a, touch.Up, p.X, p.Y)
			return
		}
	}
	t.Fatalf("no key %q", label)
}

var paused = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, Paused: true,
	File: "benchy.gcode", Layer: 12, Layers: 27, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}

// tapRegion draws a frame and taps the named part of the frame.
func tapRegion(t *testing.T, a *App, cv *gfx.Canvas, name string) {
	t.Helper()
	a.Draw(cv)
	r, ok := a.KeyRect(name)
	if !ok {
		t.Fatalf("no part %q", name)
	}
	p := r.Min.Add(r.Size().Div(2))
	ev(a, touch.Down, p.X, p.Y)
	ev(a, touch.Up, p.X, p.Y)
}

// During a pause, Home and Motors off send no command.
func TestMoveDuringPause(t *testing.T) {
	a, pr, cv := newPrintApp(paused)
	a.pages = []page{pageMove}
	for _, k := range []string{"Home all", "Z", "Center", "Motors off"} {
		tapRegion(t, a, cv, k)
	}
	time.Sleep(20 * time.Millisecond)
	if c := pr.Calls(); len(c) != 0 {
		t.Errorf("calls %v, want none", c)
	}
	if len(a.posted) != 4 { // a toast for each tap
		t.Errorf("%d toasts, want 4", len(a.posted))
	}
}

// A swap during a pause ends at the nozzle target of the print.
func TestSwapDuringPauseKeepsPrintTemp(t *testing.T) {
	a, pr, cv := newPrintApp(paused)
	a.openSwap(swapChange)
	tapLabel(t, a, cv, "PETG  240°")
	pr.wait(1)
	tapLabel(t, a, cv, "Cancel")
	if c := pr.wait(2); len(c) != 2 || c[0] != "tool 240" || c[1] != "tool 210" {
		t.Errorf("calls %v, want tool 240, tool 210", c)
	}
	if a.swap != nil {
		t.Error("swap still open")
	}
}

// The tune sheet counts from the speed of the printer.
func TestTuneUsesPrinterSpeed(t *testing.T) {
	st := paused
	st.Paused, st.SpeedPct, st.FlowPct = false, 120, 100
	a, pr, cv := newPrintApp(st)
	a.tune = true
	tapLabel(t, a, cv, "+")
	if c := pr.wait(1); len(c) != 1 || c[0] != "speed 130" {
		t.Errorf("calls %v, want speed 130", c)
	}
}

// The error screen of Klipper opens Wi-Fi and help.
func TestHelpOnKlipperError(t *testing.T) {
	a, _, cv := newPrintApp(printer.State{Err: "Error in printer.cfg", ErrDetail: "Option missing"})
	a.Setup = &DemoSetup{}
	tapLabel(t, a, cv, "Wi-Fi and help")
	if a.setup == nil || !a.setup.help {
		t.Error("help is not open")
	}
}

// A new roll from the filament change goes in the printer, and the nozzle heats for its material.
func TestSwapNewRoll(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pr.Spools_ = &printer.Spools{}
	a.openSwap(swapChange)
	tapLabel(t, a, cv, "New roll")
	if a.form == nil {
		t.Fatal("no form")
	}
	tapLabel(t, a, cv, "PETG")
	a.form.name, a.form.scale, a.form.reel, a.form.price = "Clear", 1200, 200, 25
	tapLabel(t, a, cv, "Save")
	in, ok := pr.Spools_.In()
	if !ok || in.Name != "Clear" || in.Material != "PETG" || in.Left() != 1000 {
		t.Fatalf("roll %+v %v", in, ok)
	}
	if c := pr.wait(1); len(c) != 1 || c[0] != "tool 240" || a.swap.step != swapHeat {
		t.Errorf("calls %v, step %d", c, a.swap.step)
	}
}

// A saved roll from the filament change goes in the printer.
func TestSwapSavedRoll(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pr.Spools_ = &printer.Spools{}
	sp := pr.Spools_.Add("Black", "PLA", 1200, 200, 0, time.Unix(1, 0))
	pr.Spools_.Put("")
	a.openSwap(swapChange)
	tapRegion(t, a, cv, "Black")
	if in, ok := pr.Spools_.In(); !ok || in.ID != sp.ID {
		t.Errorf("roll in the printer %+v %v", in, ok)
	}
	if c := pr.wait(1); len(c) != 1 || c[0] != "tool 210" {
		t.Errorf("calls %v", c)
	}
}

// A print checks the material of the file against the roll in the printer.
func TestMaterialCheck(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	pr.Meta_ = gcodemeta.Meta{Material: "ABS", MaterialName: "Black ABS"}
	if q, _, danger, set := a.materialCheck("x.gcode"); q != "Is Black ABS in the printer?" || danger || set != "ABS" {
		t.Errorf("nothing loaded: %q %v %q", q, danger, set)
	}
	pr.Spools_ = &printer.Spools{}
	pr.Spools_.Add("White", "PLA", 1200, 200, 0, time.Unix(1, 0))
	if q, yes, danger, _ := a.materialCheck("x.gcode"); q != "The file is for Black ABS. White (PLA) is in the printer." || yes != "Print anyway" || !danger {
		t.Errorf("another material: %q %q %v", q, yes, danger)
	}
	pr.Meta_.Material = "pla"
	if q, _, _, _ := a.materialCheck("x.gcode"); q != "" {
		t.Errorf("the same material: %q", q)
	}
	// A print from a slicer starts at once when the material is in the printer.
	a.ConfirmFilePrint("x.gcode")
	for len(a.posted) > 0 {
		(<-a.posted)()
	}
	if c := pr.Calls(); a.confirm != nil || len(c) != 1 || c[0] != "print x.gcode" {
		t.Errorf("confirm %v, calls %v", a.confirm, pr.Calls())
	}
}

// While the end macro runs, the screen shows its steps, with no Cancel.
func TestFinishingScreen(t *testing.T) {
	st := printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, Finishing: true,
		Layer: 33, Layers: 33, Bed: 52, BedTarget: 45, PrepN: 1}
	st.Prep[0] = printer.PrepStep{Kind: "cool-bed", Name: "Cool the bed", Now: true}
	a, _, cv := newPrintApp(st)
	a.Draw(cv)
	if _, ok := a.KeyRect("Steps"); !ok {
		t.Error("no steps")
	}
	if _, ok := a.KeyRect("Cancel print"); ok {
		t.Error("Cancel at the end of a print")
	}
	// Home leaves the screen; the end macro still runs, and the home screen leads back.
	tapLabel(t, a, cv, "Home")
	a.Draw(cv)
	if _, ok := a.KeyRect("Steps"); ok {
		t.Error("the steps after Home")
	}
	if _, ok := a.KeyRect("Print a file"); ok {
		t.Error("Print a file while the end macro runs")
	}
	tapLabel(t, a, cv, "Back to the print")
	a.Draw(cv)
	if _, ok := a.KeyRect("Steps"); !ok {
		t.Error("no steps after Back to the print")
	}
}

// While Klipper starts, the deck shows its start screen, not the error of Klipper.
func TestStartingScreen(t *testing.T) {
	a, _, cv := newPrintApp(printer.State{Err: "Klipper is not running", Starting: true})
	a.Draw(cv)
	if _, ok := a.KeyRect("Restart Klipper"); ok {
		t.Error("the error screen shows while Klipper starts")
	}
	a.Printer.(*recPrinter).State_.Starting = false
	a.Draw(cv)
	if _, ok := a.KeyRect("Restart Klipper"); !ok {
		t.Error("no error screen after the start")
	}
}

// A pause of the file shows its message. The height check runs PRINTDECK_CHECK_HEIGHT, and
// Resume asks first while the height is not checked.
func TestPausePrompt(t *testing.T) {
	st := paused
	st.PausePrompt, st.PauseCheck = "Put the panel in the pocket on the bed, top up.", true
	a, pr, cv := newPrintApp(st)
	tapLabel(t, a, cv, "Check the height")
	if c := pr.wait(1); len(c) != 1 || c[0] != "gcode PRINTDECK_CHECK_HEIGHT" {
		t.Errorf("calls %v", c)
	}
	tapLabel(t, a, cv, "Resume")
	if a.confirm == nil {
		t.Error("Resume did not ask with no height check")
	}
}
