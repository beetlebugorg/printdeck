package ui

import (
	"testing"

	"printdeck/internal/cloud"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// readyShare is a share link of a deck with a connection to its service, in the state st.
type readyShare struct{ st cloud.ShareState }

func (readyShare) Ready() bool               { return true }
func (r readyShare) State() cloud.ShareState { return r.st }
func (readyShare) Start()                    {}
func (readyShare) Stop()                     {}
func (readyShare) SetReady(bool)             {}

// The Share key is on the pages of a print from its start: the start macro and the first layer,
// on the tall and the wide screen, with and without Help.
func TestShareFromStart(t *testing.T) {
	prep := printer.State{Reachable: true, Connected: true, Text: "Printing", Printing: true, Preparing: true,
		File: "clip.gcode", Bed: 40, BedTarget: 60}
	first := printer.State{Reachable: true, Connected: true, Text: "Printing", Printing: true,
		File: "clip.gcode", Layer: 1, Layers: 20, Bed: 60, BedTarget: 60, Tool: 210, ToolTarget: 210}
	for _, st := range []printer.State{prep, first} {
		for _, wide := range []bool{false, true} {
			for _, help := range []bool{false, true} {
				a := New()
				a.Printer = &DemoPrinter{View: -1, State_: st}
				a.Wide = wide
				a.Share = readyShare{}
				if help {
					a.Setup = &DemoSetup{}
				}
				cv := gfx.New(a.Screen().W, a.Screen().H)
				tapLabel(t, a, cv, "Share")
				if !a.shareSheet {
					t.Errorf("preparing %v, wide %v, help %v: no share sheet", st.Preparing, wide, help)
				}
			}
		}
	}
}

// In the browser, the QR code and the address of an open share are links to the page.
func TestShareSheetLinks(t *testing.T) {
	const url = "https://printdeck.example/s/abc"
	a := New()
	a.Printer = &DemoPrinter{View: -1, State_: printingNow}
	a.Share = readyShare{cloud.ShareState{Active: true, URL: url}}
	a.shareSheet = true
	cv, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(cv)
	n := 0
	for _, o := range rec.Ops {
		if op := o.([]any); op[0] == "a" && op[5] == url {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d links to the share page, want 2", n)
	}
}
