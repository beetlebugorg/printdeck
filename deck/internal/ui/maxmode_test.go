package ui

import (
	"testing"
	"time"

	"printdeck/internal/maxmode"
)

// mmPrinter is a printer with max mode. It keeps the settings of each save.
type mmPrinter struct {
	*recPrinter
	s     maxmode.Settings
	saved []maxmode.Settings
}

func (p *mmPrinter) MaxMode() (maxmode.Settings, bool) { return p.s, false }
func (p *mmPrinter) SetMaxMode(s maxmode.Settings) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.saved = append(p.saved, s)
	return nil
}

// The Max mode page changes a copy of the settings. Save writes the copy after a question.
func TestMaxModePage(t *testing.T) {
	a, rp, cv := newPrintApp(idle)
	pr := &mmPrinter{recPrinter: rp, s: maxmode.Defaults()}
	a.Printer = pr
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Max mode")
	if a.pages[len(a.pages)-1] != pageMaxMode {
		t.Fatalf("pages %v, want the Max mode page", a.pages)
	}
	tapLabel(t, a, cv, "Save")
	if a.confirm != nil {
		t.Fatal("a question with no changes")
	}
	tapLabel(t, a, cv, "Max mode") // the check row
	tapLabel(t, a, cv, "Max flow")
	if a.keypad == nil || a.keypad.digits != "12" {
		t.Fatalf("keypad %+v", a.keypad)
	}
	for _, k := range []string{"C", "1", "5", ".", "5", "Set"} {
		tapLabel(t, a, cv, k)
	}
	tapLabel(t, a, cv, "Save")
	if a.confirm == nil {
		t.Fatal("no question")
	}
	tapLabel(t, a, cv, "Save")
	for i := 0; i < 100; i++ {
		pr.mu.Lock()
		n := len(pr.saved)
		pr.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if len(pr.saved) != 1 || !pr.saved[0].Enable || pr.saved[0].MaxFlow != 15.5 || pr.saved[0].MinLayerTime != 8 {
		t.Errorf("saved %+v", pr.saved)
	}
}

func TestMaxModeLine(t *testing.T) {
	s := maxmode.Defaults()
	if got := maxModeLine(s, true); got != "Off · the speeds of the G-code" {
		t.Error(got)
	}
	s.Enable = true
	if got := maxModeLine(s, true); got != "On · max flow 12 mm³/s" {
		t.Error(got)
	}
}
