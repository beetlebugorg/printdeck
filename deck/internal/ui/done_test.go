package ui

import (
	"image"
	"strings"
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// endedPrinter is a printer after the end of a print: the model view of the running print has no
// picture, and the history has the picture of the finished print.
type endedPrinter struct{ *DemoPrinter }

func (endedPrinter) Preview() image.Image { return nil }

// The done screen shows the finished print, not "LOADING MODEL…" for ever.
func TestDoneShowsModel(t *testing.T) {
	st := printer.State{Reachable: true, Connected: true, Text: "Ready", Finished: "complete",
		FinishedFile: "benchy.gcode", FinishedPath: "benchy.gcode"}
	a := New()
	a.Printer = endedPrinter{&DemoPrinter{View: -1, State_: st, Img: image.NewRGBA(image.Rect(0, 0, 40, 30))}}
	a.Record = true
	c, rec := gfx.NewRecording(BaseW, BaseH)
	a.Draw(c)
	frame := string(rec.JSON())
	if strings.Contains(frame, "LOADING MODEL") || !strings.Contains(frame, `["i",`) {
		t.Fatal("the done screen has no picture of the model")
	}
}
