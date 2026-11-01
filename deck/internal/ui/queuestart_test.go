package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcodemeta"

	"printdeck/internal/printer"
	"printdeck/internal/queue"
)

// A file of the queue starts in the project of the file.
func TestQueueStartKeepsTheProject(t *testing.T) {
	a := New()
	d := &DemoPrinter{View: -1, State_: printer.State{Reachable: true, Connected: true, Text: "Ready"}}
	a.Printer = d
	a.Queue = &queue.Queue{Path: filepath.Join(t.TempDir(), "queue.json")}
	it, err := a.Queue.Add(queue.Item{Path: "builds/b1-plate-1.gcode", Name: "Pod panel", Project: "Robot"})
	if err != nil {
		t.Fatal(err)
	}
	a.startQueued(it)
	// The last question first: nothing starts before Start the print.
	if a.confirm == nil || a.confirm.yes != "Start the print" || d.Next_ != "" || len(a.Queue.List()) != 1 {
		t.Fatalf("before Start the print: confirm %+v, project %q, queue %d", a.confirm, d.Next_, len(a.Queue.List()))
	}
	a.confirm.onYes()
	if d.Next_ != "Robot" {
		t.Fatalf("the project of the print: %q", d.Next_)
	}
	if len(a.Queue.List()) != 0 {
		t.Error("the file stayed in the queue")
	}
}

// A file of the queue for another roll walks through the change of the roll: the swap opens with
// the matching roll, and after Done the deck asks to start. Nothing prints before Start the print.
func TestQueueGuidedRollChange(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	pr.Meta_ = gcodemeta.Meta{Material: "PLA", MaterialName: "Navy", Color: "#1f2a5a"}
	pr.Spools_ = &printer.Spools{}
	navy := pr.Spools_.AddOut("MatterHackers PLA Navy", "PLA", 1200, 200, 0, time.Unix(1, 0))
	pr.Spools_.SetColor(navy.ID, "#1f2a5a")
	black := pr.Spools_.Add("MatterHackers PLA Black", "PLA", 1200, 200, 0, time.Unix(2, 0))
	pr.Spools_.SetColor(black.ID, "#1a1a1a")
	a.Queue = &queue.Queue{Path: filepath.Join(t.TempDir(), "queue.json")}
	it, err := a.Queue.Add(queue.Item{Path: "builds/b1-plate-1.gcode", Name: "Slate frame"})
	if err != nil {
		t.Fatal(err)
	}
	a.startQueued(it)
	if a.confirm == nil || len(a.confirm.choices) != 2 || a.confirm.choices[0].title != "Change the roll" ||
		!strings.Contains(a.confirm.choices[0].sub, "Navy") {
		t.Fatalf("the question: %+v", a.confirm)
	}
	a.confirm.choices[0].tap()
	if a.swap == nil || a.swap.roll != navy.ID || a.swap.forFile != "Slate frame" || a.swap.then == nil {
		t.Fatalf("the swap: %+v", a.swap)
	}
	// Done of the swap: the navy roll is in, and the deck asks to start.
	a.swap.setAtEnd("PLA", navy.ID)
	sw := a.swap
	a.finishSwap(sw)
	a.swap = nil
	sw.then()
	if len(pr.Calls()) != 0 && strings.Contains(strings.Join(pr.Calls(), " "), "print") {
		t.Fatalf("a print before Start the print: %v", pr.Calls())
	}
	if a.confirm == nil || a.confirm.yes != "Start the print" || !strings.Contains(a.confirm.question, "Navy") {
		t.Fatalf("the last question: %+v", a.confirm)
	}
	a.confirm.onYes()
	if c := strings.Join(pr.Calls(), " "); !strings.Contains(c, "print builds/b1-plate-1.gcode") {
		t.Errorf("calls %v", pr.Calls())
	}
}
