package ui

import (
	"image"
	"slices"
	"strings"
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/hostmetrics"
	"printdeck/internal/printer"
)

// heatPrinter is a printer with a heat log.
type heatPrinter struct {
	*recPrinter
	rep printer.HeatReport
}

func (p heatPrinter) HeatTimes() printer.HeatReport { return p.rep }

// testHeatReport is a heat log with the bed, the nozzle and the aux nozzle of an X2D, and the
// cool-downs after a print.
func testHeatReport() printer.HeatReport {
	room := 24.0
	return printer.HeatReport{
		Heaters: []printer.HeaterTimes{
			{Heater: printer.HeatBed, Samples: 6, Fit: &printer.HeatFit{Method: "theil-sen", Intercept: 30, PerDegree: 2, Samples: 6},
				Targets: []printer.TargetTimes{
					{Target: 60, Samples: 4, Median: 130, Low: 120, High: 145, From: 22, Room: &room},
					{Target: 100, Samples: 2, Median: 190, Low: 185, High: 195, From: 25},
				},
				Points: [][2]float64{{60, 120}, {60, 130}, {60, 130}, {60, 145}, {100, 185}, {100, 195}}},
			{Heater: printer.HeatNozzle, Samples: 3, Fit: &printer.HeatFit{Method: "rate", PerDegree: 0.4, Samples: 3},
				Targets: []printer.TargetTimes{{Target: 220, Samples: 3, Median: 78, Low: 75, High: 80, From: 25}},
				Points:  [][2]float64{{220, 75}, {220, 78}, {220, 80}}},
			{Heater: printer.HeatAux, Samples: 1,
				Targets: []printer.TargetTimes{{Target: 250, Samples: 1, Median: 95, Low: 95, High: 95, From: 30}},
				Points:  [][2]float64{{250, 95}}},
		},
		Cool: []printer.CoolTimes{
			{Heater: printer.HeatBed, To: 40, Samples: 5, Median: 900, Low: 840, High: 960, From: 60},
			{Heater: printer.HeatNozzle, To: 50, Samples: 5, Median: 300, Low: 290, High: 320, From: 220},
		},
	}
}

// The About page shows a line for each heater and target, the cool-downs, and the chart. No text
// of the page is cut, on a tall screen, with the large text, and on a wide screen.
func TestAboutPage(t *testing.T) {
	var cuts []string
	gfx.OnCut = func(s string, width int) { cuts = append(cuts, s) }
	defer func() { gfx.OnCut = nil }()
	for _, mode := range []string{"tall", "large", "wide"} {
		a, pr, cv := newPrintApp(idle)
		a.Printer = heatPrinter{pr, testHeatReport()}
		a.Wide = mode == "wide"
		if mode == "large" {
			a.TextSize = "large"
		}
		// The Machine page with many keys: the About key fits with the others, with no scroll.
		a.Setup = &DemoSetup{}
		a.Updater = &DemoUpdater{}
		a.Metrics = hostmetrics.Demo(a.Now())
		pr.Machine_ = printer.Machine{HasProbe: true, Modules: []string{"extruder", "heater_bed", "bed_mesh"}}
		a.pages = []page{pageMachine}
		cuts = nil
		simFrame(t, a, "about-key-"+mode)
		screen := image.Rect(0, 0, a.Screen().W, a.Screen().H)
		if r, ok := a.KeyRect("About"); !ok || !r.In(screen) || r.Dy() < 44 {
			t.Errorf("%s: About key at %v", mode, r)
		}
		// The large text has a list of rows that scrolls on the Machine page.
		for _, f := range a.focus {
			if f.reveal != nil && mode != "large" {
				t.Errorf("%s: %s is in a list that scrolls", mode, f.label)
			}
		}
		tapLabel(t, a, cv, "About")
		if a.topPage() != pageAbout {
			t.Fatalf("%s: page %v after a tap on About", mode, a.topPage())
		}
		// The top of the list, and its end after a scroll.
		top := strings.ReplaceAll(simFrame(t, a, "about-"+mode), "\u00a0", " ")
		*a.pageScroll() = 10000
		end := strings.ReplaceAll(simFrame(t, a, "about-end-"+mode), "\u00a0", " ")
		for _, c := range []struct{ f, s string }{{top, "HEAT-UP TIME BY TARGET"}, {top, "Bed to 60 °C:"}, {end, "Nozzle to 50 °C:"}} {
			if !strings.Contains(c.f, c.s) && !strings.Contains(c.f, strings.ToUpper(c.s)) {
				t.Errorf("%s: no %q on the page", mode, c.s)
			}
		}
		if len(cuts) > 0 {
			t.Errorf("%s: cut texts %q", mode, cuts)
		}
	}
}

// With no samples, the About page says that the deck records the heat-ups.
func TestAboutPageEmpty(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	a.Printer = heatPrinter{pr, printer.HeatReport{}}
	a.pages = []page{pageAbout}
	if f := simFrame(t, a, "about-empty"); !strings.Contains(f, "No heat-ups yet") {
		t.Error("no empty text")
	}
}

// The lines of the About page, in the words of the example of the request.
func TestHeatTimesLines(t *testing.T) {
	var all []string
	for _, l := range heatTimesLines(testHeatReport()) {
		all = append(all, strings.ReplaceAll(l.text, "\u00a0", " "))
	}
	for _, want := range []string{"BED HEAT-UPS", "Bed to 60 °C: 2 min 10 s typical, from 22 °C",
		"Middle half 2 min to 2 min 25 s · 4 heat-ups · room 24 °C", "Estimate: 30 s, then 2.0 s for each °C · from 6 heat-ups",
		"Estimate: 0.4 s for each °C · from 3 heat-ups", "AUX NOZZLE HEAT-UPS", "1 heat-up", "AFTER A PRINT",
		"Bed to 40 °C: 15 min typical, from 60 °C", "Nozzle to 50 °C: 5 min typical, from 220 °C"} {
		if !slices.Contains(all, want) {
			t.Errorf("no line %q in %q", want, all)
		}
	}
}

func TestSecondsText(t *testing.T) {
	for s, want := range map[float64]string{45: "45 s", 130: "2 min 10 s", 120: "2 min", 3900: "1 h 5 min", 7200: "2 h"} {
		if got := strings.ReplaceAll(secondsText(s), "\u00a0", " "); got != want {
			t.Errorf("%v: %q, want %q", s, got, want)
		}
	}
}
