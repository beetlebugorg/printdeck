package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"printdeck/internal/printer"
)

// A long file name shows whole in two lines, with no "…".
func TestFileNamesWrap(t *testing.T) {
	a := New()
	long := "Pod body screw fine 0.2mm PLA Bambu Lab X2D 1h48m.gcode"
	a.Printer = filesPrinter{&DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"},
		Img: image.NewRGBA(image.Rect(0, 0, 40, 30))},
		[]printer.File{{Path: long, Name: long, Date: time.Now()}, {Path: "a.gcode", Name: "a.gcode", Date: time.Now()}}}
	a.pages = []page{pageFiles}
	f := frameText(a)
	if strings.Contains(f, "…") {
		t.Errorf("a name ends in …: %s", f)
	}
	for _, w := range []string{"Pod body screw fine 0.2mm", "1h48m"} {
		if !strings.Contains(f, w) {
			t.Errorf("no %q", w)
		}
	}
	shot(t, a, "files-long")
}

// filesPrinter is a demo printer with its own files.
type filesPrinter struct {
	*DemoPrinter
	files []printer.File
}

func (p filesPrinter) Files() []printer.File { return p.files }

// A long name of the last print shows whole on the home screen, in two lines.
func TestLastPrintNameWraps(t *testing.T) {
	a := New()
	long := "Ledges 0.2mm PLA LulzBot TAZ Workhorse SE 0.5mm.gcode"
	a.Printer = &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"},
		Img:      image.NewRGBA(image.Rect(0, 0, 40, 30)),
		History_: []printer.Record{{Path: long, File: long, Result: "complete", End: time.Now(), Duration: 32 * time.Minute}}}
	f := frameText(a)
	if strings.Contains(f, "Lulz…") || !strings.Contains(f, "Workhorse SE") {
		t.Errorf("the name of the last print: %s", f)
	}
	shot(t, a, "lastprint-long")
}

// The question of a print from a slicer has the whole name of the file.
func TestPrintQuestionWholeName(t *testing.T) {
	a := New()
	a.Printer = &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"},
		Img: image.NewRGBA(image.Rect(0, 0, 40, 30))}
	a.confirm = a.sentFileBox("part.gcode", "Part 0.2mm PLA Bambu Lab X2D 3h28m", func() {})
	f := frameText(a)
	if strings.Contains(f, "…") || !strings.Contains(f, "3h28m") {
		t.Errorf("the question: %s", f)
	}
	shot(t, a, "question-long")
}
