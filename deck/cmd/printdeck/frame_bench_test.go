package main

import (
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/ui"
)

// BenchmarkDrawMusic and BenchmarkDrawPrint measure one frame of each tab.
func BenchmarkDrawMusic(b *testing.B) {
	a, _, _ := demoApp()
	c := gfx.New(ui.W, ui.BaseH)
	for i := 0; i < b.N; i++ {
		a.Draw(c)
	}
}

func BenchmarkDrawPrint(b *testing.B) {
	a, _, _ := demoApp()
	a.SetTab(ui.TabPrint)
	c := gfx.New(ui.W, ui.BaseH)
	for i := 0; i < b.N; i++ {
		a.Draw(c)
	}
}
