package main

import (
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/ui"
)

// BenchmarkDraw measures one frame of the home screen.
func BenchmarkDraw(b *testing.B) {
	a, _ := demoApp()
	c := gfx.New(ui.BaseW, ui.BaseH)
	for i := 0; i < b.N; i++ {
		a.Draw(c)
	}
}
