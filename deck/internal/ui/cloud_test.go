package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/printer"
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

// projectCloud is a fakeCloud with the projects of the service.
type projectCloud struct {
	fakeCloud
	shared string
}

func (p *projectCloud) OwnerCode() (string, time.Time, error) {
	return "https://pd.example/owner#code=ABCD-EFGH-JKMN", time.Now().Add(10 * time.Minute), nil
}
func (p *projectCloud) ShareProject(name string, progress func(done, total int)) (string, error) {
	progress(0, 2)
	progress(2, 2)
	p.shared = name
	return "https://pd.example/app#x=abcdefghijklmnopq", nil
}

// runQRJobs runs the functions that a background job posted, until the sheet shows a link.
func runQRJobs(t *testing.T, a *App) {
	t.Helper()
	for i := 0; i < 50 && (a.qrSheet == nil || a.qrSheet.url == "" && a.qrSheet.err == ""); i++ {
		select {
		case f := <-a.posted:
			f()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// A connected deck has Owner sign-in on the Cloud page, with the QR code of the page of the
// code; the page of a project has Share, with the QR code of the page of the project.
func TestCloudProjects(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	pc := &projectCloud{fakeCloud: fakeCloud{st: cloud.LinkState{Connected: true, Domain: "pd.example", Name: "printdeck"}}}
	a.Cloud = pc
	a.pages = []page{pageMachine, pageSettings, pageCloud}
	tapLabel(t, a, cv, "Owner sign-in")
	runQRJobs(t, a)
	if a.qrSheet == nil || !strings.Contains(a.qrSheet.url, "/owner#code=") {
		t.Fatalf("owner sheet %+v", a.qrSheet)
	}
	if f := simFrame(t, a, "cloud-owner"); !strings.Contains(f, `"a"`) {
		t.Error("the QR code is no link in the browser")
	}
	tapLabel(t, a, cv, "Close")
	if a.qrSheet != nil {
		t.Fatal("the sheet stays")
	}

	pr.History_ = []printer.Record{{File: "pot.gcode", Path: "pot.gcode", Result: "complete", End: time.Unix(5000, 0), Project: "Garden planters"}}
	a.project = "Garden planters"
	a.pages = []page{pageMachine, pageStats, pageHistory, pageProjects, pageProject}
	tapLabel(t, a, cv, "Share")
	runQRJobs(t, a)
	if pc.shared != "Garden planters" || a.qrSheet == nil || !strings.Contains(a.qrSheet.url, "/app#x=") {
		t.Fatalf("share %q %+v", pc.shared, a.qrSheet)
	}
	simFrame(t, a, "project-shared")

	// A deck with no connection has neither key.
	pc.st.Connected = false
	a.qrSheet = nil
	if f := simFrame(t, a, "project-off"); strings.Contains(f, `"SHARE"`) {
		t.Error("Share with no connection")
	}
}
