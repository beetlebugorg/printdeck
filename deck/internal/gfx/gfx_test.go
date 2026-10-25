package gfx

import "testing"

func TestFinePrint(t *testing.T) {
	defer SetFinePrint(0, 0)
	size := func(px int) int { return faceInfo[Face(false, px)][0] }
	SetFinePrint(21, 28)
	for px, want := range map[int]int{12: 21, 14: 21, 16: 22, 20: 24, 24: 26, 28: 28, 48: 48} {
		if got := size(px); got != want {
			t.Errorf("size %d: got %d, want %d", px, got, want)
		}
	}
	// A larger face shrinks to six sevenths of its size, and no further.
	s := "0123456789"
	if got := faceInfo[Shrink(Face(false, 12), s, Width(face(false, 19), s))][0]; got != 19 {
		t.Errorf("shrink: got size %d, want 19", got)
	}
	if got := faceInfo[Shrink(Face(false, 12), s, Width(face(false, 10), s))][0]; got != 21 {
		t.Errorf("shrink below the floor: got size %d, want 21", got)
	}
	SetFinePrint(0, 0)
	if got := size(12); got != 12 {
		t.Errorf("off: got size %d, want 12", got)
	}
	// The normal size shrinks to three quarters.
	if got := faceInfo[Shrink(Face(false, 16), s, Width(face(false, 12), s))][0]; got != 12 {
		t.Errorf("normal shrink: got size %d, want 12", got)
	}
}
