package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// fakeCloud is a CloudLink with no service.
type fakeCloud struct {
	st                  cloud.LinkState
	domain, code        string
	checks, disconnects int
}

func (f *fakeCloud) State() cloud.LinkState { return f.st }
func (f *fakeCloud) Connect(domain, code string) {
	f.domain, f.code = domain, code
	f.st = cloud.LinkState{Connected: true, Domain: domain, Deck: "abcdefgh", Name: "printdeck", Enrolled: time.Now()}
}
func (f *fakeCloud) Check()      { f.checks++ }
func (f *fakeCloud) Disconnect() { f.disconnects++; f.st = cloud.LinkState{} }

// Settings > Cloud: Connect opens the keyboard, and the code goes to the link with the default
// domain. A connected deck shows its deck ID, Check and Disconnect.
func TestCloudPage(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	fc := &fakeCloud{}
	a.Cloud = fc
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Cloud")
	if a.topPage() != pageCloud {
		t.Fatalf("page %v", a.topPage())
	}
	if f := simFrame(t, a, "cloud-off"); !strings.Contains(f, "NOT CONNECTED") {
		t.Error("no state")
	}
	tapLabel(t, a, cv, "Connect")
	if a.textKB == nil {
		t.Fatal("no keyboard")
	}
	a.textKB.done("k7m2-q9x4-tz8w")
	if fc.code != "k7m2-q9x4-tz8w" || fc.domain != cloud.DefaultDomain {
		t.Fatalf("connect %q %q", fc.domain, fc.code)
	}
	if f := simFrame(t, a, "cloud-on"); !strings.Contains(f, "ABCDEFGH") {
		t.Error("no deck ID")
	}
	tapLabel(t, a, cv, "Check the connection")
	tapLabel(t, a, cv, "Disconnect")
	if a.confirm == nil {
		t.Fatal("Disconnect does not ask")
	}
	a.confirm.onYes()
	if fc.checks != 1 || fc.disconnects != 1 {
		t.Errorf("checks %d, disconnects %d", fc.checks, fc.disconnects)
	}
}
