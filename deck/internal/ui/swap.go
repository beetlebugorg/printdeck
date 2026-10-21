package ui

import (
	"fmt"
	"image"
	"printdeck/kit"

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
	f := a.pageBack(c, []string{"Filament swap", "Load filament", "Unload filament"}[sw.mode], "Close", func() { a.swap = nil })
	// Tall: the step, its text and its keys under each other. Wide: the step and its text in the
	// left column, and the keys in the right column, which they fill.
	var txt *kit.Stack
	lineH, face := 30, gfx.Face(false, 20)
	if f.wide {
		txt, lineH, face = kit.NewStack(kit.Inset(f.aside(), 0, -6, 0, 0), 0), 26, gfx.Face(false, 18)
	} else {
		txt = kit.NewStack(f.body(), 0)
	}
	// keys gives the places of the keys of the step: px high on a tall screen, a share fr of the
	// right column on a wide screen.
	type size struct {
		px int
		fr float64
	}
	keys := func(sizes ...size) []image.Rectangle {
		if f.wide {
			s := make([]kit.Size, len(sizes))
			for i, z := range sizes {
				s[i] = kit.Fr(z.fr)
			}
			return kit.Rows(f.body(), 12, s...)
		}
		// At the bottom of the screen, a little larger than their size.
		s := make([]kit.Size, len(sizes)+1)
		s[0] = kit.Fr(1)
		for i, z := range sizes {
			s[i+1] = kit.Px(z.px * 5 / 4)
		}
		r := kit.Rows(kit.Inset(txt.Rest(), 0, 16, 0, 8), 12, s...)
		return r[1:]
	}

	// The steps of the mode: a load has no unload, an unload has no load.
	total := 5
	if sw.mode != swapChange {
		total = 4
	}
	step := func(n int, title string) {
		if sw.mode == swapLoadOnly && n > 2 {
			n--
		}
		r := txt.Next(28)
		c.Text(r, fmt.Sprintf("Step %d of %d", n, total), gfx.Face(false, 17), dim, gfx.Left)
		t := txt.Next(44)
		c.Text(t, title, gfx.Face(true, 26), orange, gfx.Left)
		a.textRegion("Step", image.Rect(r.Min.X, r.Min.Y, r.Max.X, t.Max.Y), gfx.Face(true, 26), title)
		txt.Skip(16)
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%d/%d %s", n, total, title)})
	}
	// para draws a text, with its lines broken to the width.
	para := func(s string) {
		for _, l := range wrapText(face, s, f.body().Dx()) {
			c.Text(txt.Next(lineH), l, face, text, gfx.Left)
		}
	}
	coolDown := func() {
		a.swap = nil
		a.do(func() error { return pr.SetToolTemp(0) })
	}

	switch sw.step {
	case swapChoose:
		if sw.mode == swapUnloadOnly {
			step(1, "Which filament comes out?")
			para("The nozzle heats to the temperature of the loaded filament.")
		} else {
			step(1, "Which filament goes in?")
			para("The nozzle heats to the temperature of the new filament.")
		}
		k := keys(size{172, 2}, size{80, 1})
		a.region("Materials", k[0])
		for i, r := range kit.Grid(k[0], 2, 2, 12) {
			m := materials[i]
			t, name := m.Tool, m.Name
			a.button(c, r, fmt.Sprintf("%s  %d°", m.Name, m.Tool), raised, text, func() {
				sw.temp, sw.step = t, swapHeat
				if sw.mode != swapUnloadOnly {
					a.setMaterial(name)
				}
				a.do(func() error { return pr.SetToolTemp(t) })
			})
		}
		a.button(c, k[1], "Other temperature", raised, text, func() {
			a.editTemp("Nozzle for the new filament", 220, 300, toolPresets, func(t int) error {
				a.post(func() { sw.temp, sw.step = t, swapHeat })
				return pr.SetToolTemp(t)
			})
		})

	case swapHeat:
		step(2, "Heating the nozzle")
		var temp, bar, cancel image.Rectangle
		if f.wide {
			para("The next step starts when the nozzle is hot.")
			r := kit.Rows(f.body(), 12, kit.Fr(1), kit.Px(80), kit.Px(20), kit.Fr(1), kit.Px(80))
			temp, bar, cancel = r[1], kit.Inset(r[2], 16, 0, 16, 0), r[4]
		} else {
			// The temperature over the text.
			temp = txt.Next(80)
			txt.Skip(20)
			bar = kit.Inset(txt.Next(20), 16, 0, 16, 0)
			txt.Skip(30)
			para("The next step starts when the nozzle is hot.")
			cancel = keys(size{80, 1})[0]
		}
		c.Text(temp, fmt.Sprintf("%.0f° / %d°", st.Tool, sw.temp), gfx.Face(true, 56), text, gfx.Center)
		segments(c, bar, max(0, min(1, st.Tool/float64(max(sw.temp, 1)))))
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%.0f/%d°", st.Tool, sw.temp), HasBar: true, Bar: st.Tool / float64(max(sw.temp, 1))})
		if st.Tool >= float64(sw.temp)-3 {
			sw.step = swapUnload
			if sw.mode == swapLoadOnly {
				sw.step = swapInsert
			}
			a.Redraw()
		}
		a.button(c, cancel, "Cancel and cool down", raised, text, coolDown)

	case swapUnload:
		step(3, "Unload the old filament")
		para(fmt.Sprintf("The extruder pushes a little out, then pulls the filament back %.0f mm. Hold the filament lightly.", a.UnloadMM))
		k := keys(size{84, 1})
		a.button(c, k[0], "Unload", orange, onAcc, func() {
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
		para("Pull the old filament out. Push the new filament into the extruder until it stops, then tap Load.")
		k := keys(size{84, 2}, size{70, 1})
		a.button(c, k[0], "Load", orange, onAcc, func() {
			sw.step = swapCheck
			a.do(func() error {
				return pr.Gcode("M83", fmt.Sprintf("G1 E%.0f F600", a.LoadMM), "G1 E20 F150", "M82")
			})
		})
		a.button(c, k[1], "Unload again", raised, text, func() { sw.step = swapUnload })

	case swapCheck:
		if sw.mode == swapUnloadOnly {
			step(4, "Take the filament out")
			para("Pull the filament out of the extruder, and store it dry.")
			k := keys(size{84, 2}, size{80, 1})
			a.button(c, k[0], "Done, cool down", orange, onAcc, coolDown)
			a.button(c, k[1], "Unload again", raised, text, func() { sw.step = swapUnload })
			return
		}
		step(5, "Check the nozzle")
		para("Clean plastic in the new colour should come out of the nozzle.")
		k := keys(size{80, 1}, size{84, 1.4}, size{80, 1})
		a.button(c, k[0], "Purge 10 mm more", raised, text, func() {
			a.do(func() error { return pr.Extrude(10, 150) })
		})
		a.button(c, k[1], "Done, cool down", orange, onAcc, coolDown)
		a.button(c, k[2], "Done, keep hot", raised, text, func() { a.swap = nil })
	}
}
