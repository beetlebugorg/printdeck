package printer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"printdeck/internal/gcode"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/maxmode"
)

// The time of a print of a G-code file before the print starts, for the Cura plugin. It is the
// time left that the deck shows at the start of the print (View.TimeLeft): the plan of the moves
// with the limits of this printer, times the plan factor of the past prints, and the steps of the
// start macro at the rates of this printer (Rates.prepLeft).
//
// The plan of a large file uses one core for some seconds. The Klipper host is a separate
// process, so the plan does not stop the steps of a print that runs. The plan runs on its own
// thread at the lowest priority (runLow), one file at a time, so that it also does not slow the
// screen of the deck.

// MaxEstimateSize is the size of the largest file that EstimateFile plans: the deck also plans
// no larger file for a print.
const MaxEstimateSize = maxModelSize

var (
	// ErrNoLimits is the error of EstimateFile before the deck has the motion limits of the
	// printer from Klipper.
	ErrNoLimits = errors.New("the deck has no motion limits of the printer")
	// ErrTooLarge is the error of EstimateFile for a file larger than MaxEstimateSize.
	ErrTooLarge = errors.New("the file is too large")
)

// FileEstimate is the time of a print of a file, in seconds.
type FileEstimate struct {
	Moves float64 `json:"moves"` // the plan of the moves times PlanFactor
	Prep  float64 `json:"prep"`  // the steps of the start macro
	Total float64 `json:"total"` // Moves and Prep
	// Planned is the plan of the moves. PlanFactor is the real time of the moves for each second
	// of the plan (Rates.PlanFactor), and PlanPrints the number of past prints in it.
	Planned    float64 `json:"planned"`
	PlanFactor float64 `json:"plan_factor"`
	PlanPrints int     `json:"plan_prints"`
	// Steps are the kinds of the steps of the start macro (PrepStep.Kind), or nil when the file
	// has no start macro of this printer.
	Steps []string `json:"steps"`
}

// startTime is the time of a print at its start: the plan of the moves times the plan factor,
// and prep, the time of the start macro.
func startTime(planned, planFactor float64, prep time.Duration) time.Duration {
	return time.Duration(planned*planFactor*float64(time.Second)) + prep
}

// estimateInput is the printer of an estimate.
type estimateInput struct {
	limits  toolhead.Limits
	maxMode *maxmode.Settings // nil when max mode is off
	rates   Rates
	macros  map[string]string // the text of each macro, by its name in lower case
	// bed and tool are the temperatures of the heaters at the start of the print.
	bed, tool float64
}

// startSteps are the kinds of the steps of the start macro of the PRINT_START line, or nil. As
// for a print (prepTracker.steps), a start macro has two steps or more, and at most the size of
// State.Prep.
func startSteps(line string, macros map[string]string) []string {
	f := strings.Fields(line)
	if len(f) == 0 {
		return nil
	}
	text, ok := macros[strings.ToLower(f[0])]
	if !ok {
		return nil
	}
	steps := prepPlan(text)
	if len(steps) < 2 {
		return nil
	}
	return steps[:min(len(steps), len(State{}.Prep))]
}

// estimateFile returns the time of a print of the G-code data on the printer in.
func estimateFile(data []byte, in estimateInput) FileEstimate {
	start := gcode.ScanStart(bytes.NewReader(data))
	open := func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	out := FileEstimate{PlanFactor: in.rates.PlanFactor(), PlanPrints: in.rates.PlanSamples}
	if pl := planGcode(open, start.End, in.limits, in.maxMode); pl != nil {
		out.Planned = pl.total
	}
	// The state at the start of the print: no step is done, and the heaters have the targets of
	// the PRINT_START line.
	st := State{Bed: in.bed, Tool: in.tool, BedTarget: start.BedTemp, ToolTarget: start.ExtTemp}
	out.Steps = startSteps(start.Line, in.macros)
	for i, kind := range out.Steps {
		st.Prep[i] = PrepStep{Kind: kind, Name: prepNames[kind], Now: i == 0}
	}
	st.PrepN = len(out.Steps)
	prep := in.rates.prepLeft(st, &timer{}, time.Time{})
	out.Moves = out.Planned * out.PlanFactor
	out.Prep = prep.Seconds()
	out.Total = startTime(out.Planned, out.PlanFactor, prep).Seconds()
	return out
}

// planSlot lets one plan of a file run at a time.
var planSlot = make(chan struct{}, 1)

// EstimateFile returns the time of a print of the G-code data on this printer. The heaters start
// at their temperatures now when the printer is idle, and at the room temperature while a print
// runs. A call waits for the plan of another file, until ctx ends.
func (k *Klippy) EstimateFile(ctx context.Context, data []byte) (FileEstimate, error) {
	if len(data) > MaxEstimateSize {
		return FileEstimate{}, ErrTooLarge
	}
	k.View.mu.Lock()
	in := estimateInput{limits: k.View.limits, maxMode: k.View.maxMode}
	k.View.mu.Unlock()
	if in.limits.MaxVelocity <= 0 {
		return FileEstimate{}, ErrNoLimits
	}
	in.rates = k.Rates()
	st := k.State()
	k.mu.Lock()
	in.macros = k.macros
	in.bed, in.tool = k.timer.roomTemp(), k.timer.roomTemp()
	k.mu.Unlock()
	if st.Reachable && !st.Busy() && st.Bed > 0 && st.Tool > 0 {
		in.bed, in.tool = st.Bed, st.Tool
	}
	select {
	case planSlot <- struct{}{}:
	case <-ctx.Done():
		return FileEstimate{}, ctx.Err()
	}
	defer func() { <-planSlot }()
	var out FileEstimate
	runLow(func() { out = estimateFile(data, in) })
	return out, nil
}
