package printer

import (
	"fmt"
	"testing"
)

func TestConsole(t *testing.T) {
	var c console
	c.add("in", "G28 X")
	c.add("out", "!! Must home axis first: 0.000 0.000 0.000 [0.000]")
	c.add("out", "// Klipper state: Ready\nok")
	got := c.Console()
	want := []ConsoleLine{{"in", "G28 X"}, {"error", "Must home axis first: 0.000 0.000 0.000 [0.000]"},
		{"out", "Klipper state: Ready"}, {"out", "ok"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for i := range maxConsole + 5 {
		c.add("out", fmt.Sprint(i))
	}
	if got := c.Console(); len(got) != maxConsole || got[len(got)-1].Text != fmt.Sprint(maxConsole+4) {
		t.Errorf("%d lines, last %v", len(got), got[len(got)-1])
	}
}
