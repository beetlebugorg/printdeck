package gfx

import "testing"

func TestFinePrint(t *testing.T) {
	defer SetFinePrint(0, 0)
	size := func(px int) int { return faceInfo[Face(false, px)][0] }
	SetFinePrint(18, 24)
	for px, want := range map[int]int{12: 18, 14: 19, 16: 20, 18: 21, 22: 23, 24: 24, 48: 48} {
		if got := size(px); got != want {
			t.Errorf("size %d: got %d, want %d", px, got, want)
		}
	}
	// A larger face can shrink to three quarters of the size that was asked for.
	s := "0123456789"
	f := Shrink(Face(false, 12), s, Width(face(false, 10), s))
	if got := faceInfo[f][0]; got != 10 {
		t.Errorf("shrink: got size %d, want 10", got)
	}
	SetFinePrint(0, 0)
	if got := size(12); got != 12 {
		t.Errorf("off: got size %d, want 12", got)
	}
}
