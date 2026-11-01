package ui

import (
	"path/filepath"
	"testing"

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
	if d.Next_ != "Robot" {
		t.Fatalf("the project of the print: %q", d.Next_)
	}
	if len(a.Queue.List()) != 0 {
		t.Error("the file stayed in the queue")
	}
}
