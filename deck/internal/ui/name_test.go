package ui

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// namedSetup is a DemoSetup with a name.
type namedSetup struct {
	DemoSetup
	mu   sync.Mutex
	name string
}

func (n *namedSetup) Name() string { n.mu.Lock(); defer n.mu.Unlock(); return n.name }
func (n *namedSetup) SetName(s string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.name = s
	return nil
}

// Settings has the row Name with the address, and a new name goes to the system.
func TestSettingsName(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	nm := &namedSetup{name: "Printdeck"}
	a.Setup = nm
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-name"); !strings.Contains(f, "printdeck.local") {
		t.Error("no address in the Name row")
	}
	tapLabel(t, a, cv, "Name")
	if a.textKB == nil {
		t.Fatal("no keyboard")
	}
	a.textKB.done("!!")
	if a.textKB == nil {
		t.Fatal("a name with no letter closes the keyboard")
	}
	a.textKB.done(" Workhorse ")
	for i := 0; i < 100 && nm.Name() != "Workhorse"; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if nm.Name() != "Workhorse" {
		t.Errorf("name %q", nm.Name())
	}
}
