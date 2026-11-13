package printer

import "testing"

// A print that ends in its start macro (heating, probing, priming) gets no record. A print that
// got to the model, or completed, does. The end macro and a pause do not change it.
func TestStartEnded(t *testing.T) {
	start := State{Printing: true, Preparing: true}
	model := State{Printing: true}
	finishing := State{Printing: true, Finishing: true}
	paused := State{Paused: true}
	ended := func(result string) State { return State{Finished: result} }
	cases := []struct {
		name   string
		states []State
		skip   bool
	}{
		{"cancelled in the start macro", []State{start, finishing, ended("cancelled")}, true},
		{"failed in the start macro", []State{start, ended("error")}, true},
		{"paused in the start macro, then cancelled", []State{start, paused, finishing, ended("cancelled")}, true},
		{"cancelled on the model", []State{start, model, finishing, ended("cancelled")}, false},
		{"complete", []State{start, model, finishing, ended("complete")}, false},
		{"the deck came back during the print", []State{model, ended("cancelled")}, false},
	}
	for _, c := range cases {
		k := &Klippy{}
		var skip bool
		for _, st := range c.states {
			skip = k.startEnded(st)
		}
		if skip != c.skip {
			t.Errorf("%s: skip %v, want %v", c.name, skip, c.skip)
		}
	}
	// The next print starts the follow again: a print to the model after a skipped one counts.
	k := &Klippy{}
	for _, st := range []State{start, ended("cancelled"), start, model, ended("cancelled")} {
		k.startEnded(st)
	}
	if k.startEnded(ended("cancelled")) {
		t.Error("a print to the model after a skipped print gets no record")
	}
}
