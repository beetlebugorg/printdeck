package ui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
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
func (r *recPrinter) FirmwareRestart() error { return r.Gcode("FIRMWARE_RESTART") }

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
	a.Music, a.Printer, a.Vol = &DemoMusic{State_: spotify.State{Authorized: true}}, pr, DemoVolume(10)
	a.SetTab(TabPrint)
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
	// Hold the pad at the right edge: X+ at full speed, then let go.
	ev(a, touch.Down, joyCX+joyR-2, joyCY)
	time.Sleep(3*joyTick + joyTick/2)
	ev(a, touch.Up, joyCX+joyR-2, joyCY)
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
	ev(a, touch.Down, joyCX, joyCY-joyR+2)
	time.Sleep(joyTick + joyTick/2)
	ev(a, touch.Up, joyCX, joyCY-joyR+2)
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
	ev(a, touch.Down, joyCX+joyR-2, joyCY)
	time.Sleep(2 * joyTick)
	ev(a, touch.Up, joyCX+joyR-2, joyCY)
	tapAt(a, cv, 60, 215) // Home all
	time.Sleep(50 * time.Millisecond)
	if c := pr.Calls(); len(c) != 0 {
		t.Errorf("commands while printing: %v", c)
	}
}

func TestKeypadSetsTemperature(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.Draw(cv)
	tapAt(a, cv, 100, 200) // the nozzle card on the idle screen
	if a.keypad == nil {
		t.Fatal("no keypad")
	}
	// Type 2 4 5 with the keys, then Set. Keys: 3 columns from x 48, rows from y 274, 80 apart.
	kw := (W - 48 - 48 - 16) / 3
	key := func(col, row int) { tapAt(a, cv, 48+col*(kw+8)+kw/2, 70+204+row*80+36) }
	key(0, 3)                      // C
	key(1, 0)                      // 2
	key(0, 1)                      // 4
	key(1, 1)                      // 5
	tapAt(a, cv, W-100, 716-84+32) // Set
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
	tapAt(a, cv, W*3/4, 310) // PETG 240, the second key of the first row
	if c := pr.wait(1); c[0] != "tool 240" || a.swap.step != swapHeat {
		t.Fatalf("after PETG: %v, step %d", c, a.swap.step)
	}
	pr.State_.Tool = 239 // hot enough: the next draw goes on to unloading
	a.Draw(cv)
	a.Draw(cv)
	if a.swap.step != swapUnload {
		t.Fatalf("step %d, want unload", a.swap.step)
	}
	tapAt(a, cv, W/2, 460) // Unload
	tapAt(a, cv, W/2, 460) // Load
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
	tapAt(a, cv, W-100, 680) // Firmware restart
	if a.confirm == nil {
		t.Fatal("no confirmation")
	}
	tapAt(a, cv, 340, 500) // the Reset key of the dialog
	if c := pr.wait(1); len(c) != 1 || c[0] != "gcode FIRMWARE_RESTART" {
		t.Errorf("calls %v", c)
	}
}
