package ui

import (
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// A question from outside shows on each screen of the deck, and an answer on one screen closes it
// on the others.
func TestSharedQuestion(t *testing.T) {
	a, _, cv := newPrintApp(printer.State{Reachable: true, Connected: true, Text: "Ready"})
	s := a.Session(800, 480)
	cs := gfx.New(800, 480)
	answer := make(chan bool, 1)
	a.Ask("Give Cura access to the printer?", "Allow", "Deny", func(yes bool) { answer <- yes })
	a.Draw(cv)
	s.Draw(cs)
	if a.confirm == nil || s.confirm == nil {
		t.Fatal("the question is not on both screens")
	}
	tapLabel(t, s, cs, "Allow")
	if yes := <-answer; !yes {
		t.Fatal("the answer is not yes")
	}
	a.Draw(cv)
	if a.confirm != nil {
		t.Error("the question stays on the other screen")
	}
}
