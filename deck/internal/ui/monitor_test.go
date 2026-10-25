package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/hostmetrics"
	"printdeck/internal/printer"
)

// monitorFrame draws the System monitor page and returns the recording of the frame.
func monitorFrame(t *testing.T, a *App) string {
	t.Helper()
	a.pages = []page{pageMachine, pageMonitor}
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	return string(rec.JSON())
}

func monitorApp(wide bool) *App {
	a := New()
	a.Printer = &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"}}
	now := time.Date(2026, 10, 20, 14, 24, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	a.Wide = wide
	return a
}

func TestMonitorRanges(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a := monitorApp(wide)
		a.Metrics = hostmetrics.Demo(a.Now())
		for i := range monitorRanges {
			a.monitorRange = i
			f := monitorFrame(t, a)
			for _, want := range []string{"CPU", "MEMORY", "CARD WRITES", "/ MIN"} {
				if !strings.Contains(f, want) {
					t.Errorf("wide %v, range %s: no %q", wide, monitorRanges[i].key, want)
				}
			}
			for _, name := range []string{"CPU", "Memory", "Writes", "10 min", "1 h", "24 h"} {
				if _, ok := a.KeyRect(name); !ok {
					t.Errorf("wide %v: no part %q", wide, name)
				}
			}
		}
	}
}

// A computer with no /proc, as the desktop app on a Mac: the page shows that it has no data.
func TestMonitorNoData(t *testing.T) {
	a := monitorApp(false)
	a.Metrics = hostmetrics.New()
	a.Metrics.ReadFile = func(string) ([]byte, error) { return nil, errNoFile }
	if f := monitorFrame(t, a); !strings.Contains(f, "Waiting for data") {
		t.Error("an empty recorder before a read: no Waiting for data")
	}
	a.Metrics.Sample(a.Now())
	a.Metrics.Sample(a.Now().Add(hostmetrics.FineInterval))
	if f := monitorFrame(t, a); !strings.Contains(f, "No data") {
		t.Error("a recorder with no /proc files: no No data")
	}
	a.Metrics = nil
	if f := monitorFrame(t, a); !strings.Contains(f, "No data") {
		t.Error("no recorder: no No data")
	}
}

// The Machine page has the System monitor row only with a recorder.
func TestMonitorRow(t *testing.T) {
	a := monitorApp(false)
	a.pages = []page{pageMachine}
	c, rec := gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	if strings.Contains(string(rec.JSON()), "SYSTEM MONITOR") {
		t.Error("a System monitor row with no recorder")
	}
	a.Metrics = hostmetrics.Demo(a.Now())
	a.DemoScroll(9999)
	c, rec = gfx.NewRecording(a.Screen().W, a.Screen().H)
	a.Draw(c)
	if !strings.Contains(string(rec.JSON()), "SYSTEM MONITOR") {
		t.Error("no System monitor row")
	}
}

func TestMonitorText(t *testing.T) {
	for _, c := range []struct {
		got, want string
	}{
		{bytesText(512), "512 B"}, {bytesText(40 * 1024), "40 KiB"}, {bytesText(1.5 * 1024 * 1024), "1.5 MiB"},
		{bytesText(4045 * 1024 * 1024), "4.0 GiB"}, {rate(3.25), "3.2"}, {rate(5), "5"}, {rate(12.4), "12"},
		{pct(0.234), "23%"},
	} {
		if c.got != c.want {
			t.Errorf("got %q; want %q", c.got, c.want)
		}
	}
}

var errNoFile = errors.New("no file")
