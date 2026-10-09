package ui

import (
	"fmt"
	"image"

	"speakerdeck/internal/gfx"
)

// The filament swap, step by step: choose the material, heat, unload, insert, load and purge.

type swapState struct {
	step int
	temp int
}

const (
	swapChoose = iota
	swapHeat
	swapUnload
	swapInsert
	swapCheck
)

func (a *App) drawSwap(c *gfx.Canvas) {
	sw := a.swap
	pr := a.Printer
	st := pr.State()
	c.Text(image.Rect(24, 64, W-150, 108), "Filament swap", gfx.Face(true, 28), text, gfx.Left)
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Close", raised, text, func() { a.swap = nil })

	step := func(n int, title string) {
		c.Text(image.Rect(24, 112, W-24, 140), fmt.Sprintf("Step %d of 5", n), gfx.Face(false, 17), dim, gfx.Left)
		c.Text(image.Rect(24, 140, W-24, 184), title, gfx.Face(true, 26), orange, gfx.Left)
	}
	para := func(y int, lines ...string) {
		for i, l := range lines {
			c.Text(image.Rect(24, y+i*30, W-24, y+i*30+30), l, gfx.Face(false, 20), text, gfx.Left)
		}
	}
	bigButton := func(y int, label string, f func()) {
		a.button(c, image.Rect(24, y, W-24, y+84), label, orange, onAcc, f)
	}

	switch sw.step {
	case swapChoose:
		step(1, "Which filament goes in?")
		para(200, "The nozzle heats to the temperature", "of the new filament.")
		for i, m := range []struct {
			name string
			t    int
		}{{"PLA", 210}, {"PETG", 240}, {"ABS", 250}} {
			t := m.t
			a.button(c, image.Rect(24, 280+i*92, W-24, 280+i*92+80), fmt.Sprintf("%s  %d°", m.name, m.t), raised, text, func() {
				sw.temp, sw.step = t, swapHeat
				a.do(func() error { return pr.SetToolTemp(t) })
			})
		}
		a.button(c, image.Rect(24, 556, W-24, 636), "Other temperature", raised, text, func() {
			a.editTemp("Nozzle for the new filament", 220, 300, toolPresets, func(t int) error {
				a.post(func() { sw.temp, sw.step = t, swapHeat })
				return pr.SetToolTemp(t)
			})
		})

	case swapHeat:
		step(2, "Heating the nozzle")
		c.Text(image.Rect(24, 220, W-24, 300), fmt.Sprintf("%.0f° / %d°", st.Tool, sw.temp), gfx.Face(true, 56), text, gfx.Center)
		bar := image.Rect(40, 320, W-40, 340)
		c.RoundRect(bar, 10, raised)
		f := max(0, min(1, st.Tool/float64(max(sw.temp, 1))))
		fill := bar
		fill.Max.X = bar.Min.X + int(f*float64(bar.Dx()))
		c.RoundRect(fill, 10, orange)
		para(370, "The next step starts when the nozzle", "is hot.")
		if st.Tool >= float64(sw.temp)-3 {
			sw.step = swapUnload
			a.Redraw()
		}
		a.button(c, image.Rect(24, 620, W-24, 700), "Cancel and cool down", raised, text, func() {
			a.swap = nil
			a.do(func() error { return pr.SetToolTemp(0) })
		})

	case swapUnload:
		step(3, "Unload the old filament")
		para(200, "The extruder pushes a little out,", fmt.Sprintf("then pulls the filament back %.0f mm.", a.UnloadMM), "", "Hold the filament lightly.")
		bigButton(420, "Unload", func() {
			sw.step = swapInsert
			a.do(func() error {
				return pr.Gcode("M83", "G1 E10 F300", fmt.Sprintf("G1 E-%.0f F1500", a.UnloadMM), "M82")
			})
		})

	case swapInsert:
		step(4, "Put the new filament in")
		para(200, "Pull the old filament out.", "Push the new filament into the extruder", "until it stops, then tap Load.")
		bigButton(420, "Load", func() {
			sw.step = swapCheck
			a.do(func() error {
				return pr.Gcode("M83", fmt.Sprintf("G1 E%.0f F600", a.LoadMM), "G1 E20 F150", "M82")
			})
		})
		a.button(c, image.Rect(24, 520, W-24, 590), "Unload again", raised, text, func() { sw.step = swapUnload })

	case swapCheck:
		step(5, "Check the nozzle")
		para(200, "Clean plastic in the new colour", "should come out of the nozzle.")
		a.button(c, image.Rect(24, 300, W-24, 380), "Purge 10 mm more", raised, text, func() {
			a.do(func() error { return pr.Extrude(10, 150) })
		})
		bigButton(400, "Done, cool down", func() {
			a.swap = nil
			a.do(func() error { return pr.SetToolTemp(0) })
		})
		a.button(c, image.Rect(24, 500, W-24, 580), "Done, keep hot", raised, text, func() { a.swap = nil })
	}
}
