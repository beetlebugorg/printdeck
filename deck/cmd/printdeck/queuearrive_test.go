package main

import (
	"testing"

	"printdeck/internal/queue"
)

// A new file from a slicer or from the service asks; a file from the screen and a known file do not.
func TestArrived(t *testing.T) {
	seen := map[string]bool{}
	if !arrived(seen, []queue.Item{{ID: "a", From: "build"}}) {
		t.Fatal("a new plate of the service")
	}
	if arrived(seen, []queue.Item{{ID: "a", From: "build"}}) {
		t.Fatal("a known file")
	}
	if arrived(seen, []queue.Item{{ID: "a", From: "build"}, {ID: "b", From: "deck"}}) {
		t.Fatal("a file from the screen")
	}
	if !arrived(seen, []queue.Item{{ID: "c", From: "slicer"}}) {
		t.Fatal("a new file of a slicer")
	}
	if len(seen) != 1 || !seen["c"] {
		t.Fatalf("seen %v", seen)
	}
}
