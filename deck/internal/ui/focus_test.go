package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
)

// knob sends a key of the knob, runs it as the UI loop does, and draws the next frame.
func knob(a *App, key string) {
	a.LCDKey(key)
	(<-a.posted)()
	a.Draw(gfx.New(W, BaseH))
}

// lcdLines is the menu of the display: the title, then the keys, with "> " at the selected key.
func lcdLines(a *App) []string {
	m := a.LCD().Menu
	if m == nil {
		return nil
	}
	out := []string{m.Title}
	for i, it := range m.Items {
		l := "  " + it.Label
		if i == m.Selected {
			l = "> " + it.Label
		}
		out = append(out, l)
	}
	return out
}

// TestKnobMenu walks the menu of the printer display with the knob: from the idle screen to
// Machine, and back.
func TestKnobMenu(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	a.Draw(cv)
	if s := a.LCD().Status; s == nil || !strings.HasPrefix(s.State, "READY") || s.Hint != "Click for menu" {
		t.Fatalf("status screen: %+v", s)
	}
	// The first click opens the menu, and taps nothing.
	knob(a, "click")
	lines := lcdLines(a)
	if lines[0] != "Printdeck" || !a.LCD().Menu.Carousel || lines[1] != "  Status" || lines[2] != "> Print a file" || a.topPage() != 0 {
		t.Fatalf("menu: %q, page %v", lines, a.topPage())
	}
	// Turn the knob down to Machine, and click.
	for i := 0; i < 20 && a.focus[a.lcdSel].label != "Machine"; i++ {
		knob(a, "down")
	}
	if a.focus[a.lcdSel].label != "Machine" {
		t.Fatalf("no Machine key in %q", lcdLines(a))
	}
	knob(a, "click")
	if a.topPage() != pageMachine {
		t.Fatalf("page %v after the click", a.topPage())
	}
	lines = lcdLines(a)
	if lines[0] != "Machine" || lines[1] != "  Back" || !strings.HasPrefix(lines[2], "> ") || a.LCD().Menu.Items[0].Icon != "back" {
		t.Errorf("Machine: %q", lines)
	}
	// A turn up goes to Back, and a click goes back, to the key that opened the page.
	knob(a, "up")
	knob(a, "click")
	if a.topPage() != 0 {
		t.Errorf("page %v after Back", a.topPage())
	}
	if l := a.focus[a.lcdSel].label; l != "Machine" {
		t.Errorf("the cursor is at %q after Back, want Machine", l)
	}
	// The touch screen shows the focus with a ring after a turn.
	if a.knobAt.IsZero() {
		t.Error("no time of the last turn")
	}
	// Status at the top of the home menu goes back to the status screen.
	for i := 0; i < 10; i++ {
		knob(a, "up")
	}
	knob(a, "click")
	if a.LCD().Status == nil {
		t.Errorf("after Status: %q", lcdLines(a))
	}
}

// TestKnobAction shows an action as a full screen animation on the display.
func TestKnobAction(t *testing.T) {
	st := idle
	st.Action = "Bed tilt"
	a, _, cv := newPrintApp(st)
	st.ActionStart = a.Now().Add(-42 * time.Second)
	a.Printer.(*recPrinter).State_ = st
	a.Draw(cv)
	if s := a.LCD(); s.Anim != "probe" || s.Title != "Bed tilt" || s.Caption != "0:42" {
		t.Errorf("screen %+v", s)
	}
}
