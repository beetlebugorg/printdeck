package printer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcode"
	"printdeck/internal/maxmode"
)

// estimateMacros has a start macro with four steps: home, heat-bed, heat-nozzle and probe.
var estimateMacros = map[string]string{
	"print_start": "G28\nM190 S{params.BED}\nM109 S{params.EXTRUDER}\nBED_MESH_CALIBRATE\n",
}

// The estimate of a file is the time left that the deck shows at the start of its print, with
// the same plan, plan factor and start macro.
func TestEstimateFileIsTheTimeLeftAtTheStart(t *testing.T) {
	src, _ := curaFile()
	rates := ComputeRates(nil)
	rates.PlanError, rates.PlanSamples = 0.1, 5
	in := estimateInput{limits: printerLimits, rates: rates, macros: estimateMacros, bed: 22, tool: 22}
	est := estimateFile([]byte(src), in)

	if strings.Join(est.Steps, " ") != "home heat-bed heat-nozzle probe" {
		t.Errorf("steps %v", est.Steps)
	}
	// Home 20 s, the bed 38 degrees at 6 s, the nozzle 188 degrees at 0.7 s, and the probe 90 s.
	if want := 20 + 38*6 + 188*0.7 + 90; math.Abs(est.Prep-want) > 1e-6 {
		t.Errorf("prep %.3f s, want %.3f s", est.Prep, want)
	}
	if est.Planned <= 0 || est.PlanFactor != 1.1 || est.PlanPrints != 5 ||
		math.Abs(est.Moves-1.1*est.Planned) > 1e-6 || math.Abs(est.Total-est.Moves-est.Prep) > 1e-6 {
		t.Errorf("estimate %+v", est)
	}

	// The deck at the start of the print of the same file.
	gc, _ := gcode.Parse(strings.NewReader(src))
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(src)), nil }
	st := State{Reachable: true, Printing: true, Preparing: true, Text: "Printing", FilePath: "a.gcode",
		FileSize: int64(len(src)), Bed: 22, Tool: 22, BedTarget: 60, ToolTarget: 210}
	for i, kind := range est.Steps {
		st.Prep[i] = PrepStep{Kind: kind, Now: i == 0}
	}
	st.PrepN = len(est.Steps)
	v := &View{gc: gc, file: fileKey(st), view: -1, plan: planGcode(open, gc.StartEnd, printerLimits, nil)}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	v.TimeLeft(&st, now, rates.prepLeft(st, &timer{}, now), rates.PlanFactor())
	if d := st.TimeLeft.Seconds() - est.Total; !st.HasTimeLeft || math.Abs(d) > 1e-6 {
		t.Errorf("deck %v, estimate %.3f s", st.TimeLeft, est.Total)
	}
}

// A file with no start macro of the printer has no steps: the estimate is the moves.
func TestEstimateFileNoMacro(t *testing.T) {
	src, _ := curaFile()
	est := estimateFile([]byte(src), estimateInput{limits: printerLimits, rates: ComputeRates(nil), bed: 22, tool: 22})
	if est.Steps != nil || est.Prep != 0 || est.Planned <= 0 || math.Abs(est.Total-est.Planned) > 1e-6 {
		t.Errorf("estimate %+v", est)
	}
}

// With max mode, the estimate has the plan of the deck for a print: the speeds and the layer
// factors of max mode.
func TestEstimateFileMaxMode(t *testing.T) {
	body, err := os.ReadFile("testdata/two-layers.gcode")
	if err != nil {
		t.Fatal(err)
	}
	src := planSetup[len("SET_KINEMATIC_POSITION X=0 Y=0 Z=0\n"):] + string(body)
	s := maxModeSettings()
	est := estimateFile([]byte(src), estimateInput{limits: printerLimits, maxMode: &s, rates: ComputeRates(nil)})
	s.MaxVelocity = printerLimits.MaxVelocity
	want := planFile(strings.NewReader(src), 0, printerLimits, &s, maxmode.LayerFactors(strings.NewReader(src), s, 0))
	normal := planFile(strings.NewReader(src), 0, printerLimits, nil, nil)
	if est.Planned != want.total || est.Planned >= normal.total {
		t.Errorf("plan %.3f s, want %.3f s (F speeds %.3f s)", est.Planned, want.total, normal.total)
	}
}

// The deck plans a file only with the motion limits of the printer, and only up to
// MaxEstimateSize. With no connection to the printer, the heaters start at the room temperature.
func TestKlippyEstimateFile(t *testing.T) {
	k := NewKlippy("", emptyStore{})
	src, _ := curaFile()
	ctx := context.Background()
	if _, err := k.EstimateFile(ctx, []byte(src)); !errors.Is(err, ErrNoLimits) {
		t.Errorf("no limits: %v", err)
	}
	k.SetLimits(printerLimits, nil)
	k.macros = estimateMacros
	est, err := k.EstimateFile(ctx, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := estimateFile([]byte(src), estimateInput{limits: printerLimits, rates: ComputeRates(nil),
		macros: estimateMacros, bed: defaultRoom, tool: defaultRoom})
	if est.Total != want.Total || est.Prep == 0 {
		t.Errorf("estimate %+v, want %+v", est, want)
	}
	if _, err := k.EstimateFile(ctx, bytes.Repeat([]byte{'\n'}, MaxEstimateSize+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a large file: %v", err)
	}
	// A call waits for the plan of another file until its context ends.
	planSlot <- struct{}{}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = k.EstimateFile(short, []byte(src))
	<-planSlot
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a second plan: %v", err)
	}
}
