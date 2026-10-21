package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The filament swap, step by step: choose the material, heat, unload, insert, load and purge.

type swapState struct {
	step int
	temp int
	mode int // swapChange, swapLoadOnly or swapUnloadOnly
}

// The modes of the flow: a change of the filament, a load, or an unload.
const (
	swapChange = iota
	swapLoadOnly
	swapUnloadOnly
)

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
	heading(c, []string{"Filament swap", "Load filament", "Unload filament"}[sw.mode])
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Close", raised, text, func() { a.swap = nil })

	// The steps of the mode: a load has no unload, an unload has no load.
	total := 5
	if sw.mode != swapChange {
		total = 4
	}
	step := func(n int, title string) {
		if sw.mode == swapLoadOnly && n > 2 {
			n--
		}
		c.Text(image.Rect(24, 112, W-24, 140), fmt.Sprintf("Step %d of %d", n, total), gfx.Face(false, 17), dim, gfx.Left)
		c.Text(image.Rect(24, 140, W-24, 184), title, gfx.Face(true, 26), orange, gfx.Left)
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%d/%d %s", n, total, title)})
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
		if sw.mode == swapUnloadOnly {
			step(1, "Which filament comes out?")
			para(200, "The nozzle heats to the temperature", "of the loaded filament.")
		} else {
			step(1, "Which filament goes in?")
			para(200, "The nozzle heats to the temperature", "of the new filament.")
		}
		for i, m := range materials {
			t, name := m.Tool, m.Name
			r := image.Rect(24+(i%2)*((W-48)/2+6), 270+(i/2)*92, 24+(i%2)*((W-48)/2+6)+(W-48)/2-6, 270+(i/2)*92+80)
			a.button(c, r, fmt.Sprintf("%s  %d°", m.Name, m.Tool), raised, text, func() {
				sw.temp, sw.step = t, swapHeat
				if sw.mode != swapUnloadOnly {
					a.setMaterial(name)
				}
				a.do(func() error { return pr.SetToolTemp(t) })
			})
		}
		a.button(c, image.Rect(24, 470, W-24, 550), "Other temperature", raised, text, func() {
			a.editTemp("Nozzle for the new filament", 220, 300, toolPresets, func(t int) error {
				a.post(func() { sw.temp, sw.step = t, swapHeat })
				return pr.SetToolTemp(t)
			})
		})

	case swapHeat:
		step(2, "Heating the nozzle")
		c.Text(image.Rect(24, 220, W-24, 300), fmt.Sprintf("%.0f° / %d°", st.Tool, sw.temp), gfx.Face(true, 56), text, gfx.Center)
		segments(c, image.Rect(40, 320, W-40, 340), max(0, min(1, st.Tool/float64(max(sw.temp, 1)))))
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%.0f/%d°", st.Tool, sw.temp), HasBar: true, Bar: st.Tool / float64(max(sw.temp, 1))})
		para(370, "The next step starts when the nozzle", "is hot.")
		if st.Tool >= float64(sw.temp)-3 {
			sw.step = swapUnload
			if sw.mode == swapLoadOnly {
				sw.step = swapInsert
			}
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
			if sw.mode == swapUnloadOnly {
				sw.step = swapCheck
				a.setMaterial("")
			}
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
		if sw.mode == swapUnloadOnly {
			step(4, "Take the filament out")
			para(200, "Pull the filament out of the", "extruder, and store it dry.")
			bigButton(400, "Done, cool down", func() {
				a.swap = nil
				a.do(func() error { return pr.SetToolTemp(0) })
			})
			a.button(c, image.Rect(24, 500, W-24, 580), "Unload again", raised, text, func() { sw.step = swapUnload })
			return
		}
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
