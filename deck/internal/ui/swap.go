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
	// paused is true for a swap during a paused print. At its end the nozzle goes back to resume,
	// the nozzle target of the pause, so that Resume prints at the temperature of the print.
	paused bool
	resume int
}

// openSwap opens the swap in a mode.
func (a *App) openSwap(mode int) {
	sw := &swapState{mode: mode}
	if st := a.Printer.State(); st.Busy() && st.Paused {
		sw.paused, sw.resume = true, int(st.ToolTarget+0.5)
	}
	a.swap = sw
}

// The modes of the flow: a change of the filament, a load, or an unload.
const (
	swapChange = iota
	swapLoadOnly
	swapUnloadOnly
)

const (
	swapChoose   = iota // the roll that goes in, or the material that comes out
	swapMaterial        // the material that goes in, with no roll
	swapHeat
	swapUnload
	swapInsert
	swapCheck
)

func (a *App) drawSwap(c *gfx.Canvas) {
	sw := a.swap
	pr := a.Printer
	st := pr.State()
	// end closes the swap. It cools the nozzle, or during a pause sets it back to the temperature of
	// the print.
	end := func() {
		a.swap = nil
		t := 0
		if sw.paused {
			t = sw.resume
			if t > 0 {
				a.Toast(fmt.Sprintf("The nozzle goes back to %d° for the print", t))
			}
		}
		a.do(func() error { return pr.SetToolTemp(t) })
	}
	// Close before the swap heats the nozzle only closes. After it, Close is the same as Cancel.
	closeSwap := func() {
		if sw.paused || sw.temp > 0 {
			end()
			return
		}
		a.swap = nil
	}
	f := a.pageBack(c, []string{"Filament swap", "Load filament", "Unload filament"}[sw.mode], "Close", closeSwap)
	// Tall: the step, its text and its keys under each other. Wide: the step and its text at the
	// top of the right column, and the keys under them, which fill the column.
	var txt *kit.Stack
	var keyArea image.Rectangle
	// One size of text on both screens: the label of the step, its title, and the text.
	lineH, face := 26, gfx.Face(false, 18)
	if f.wide {
		r := kit.Rows(f.body(), 12, kit.Px(130), kit.Fr(1))
		txt, keyArea = kit.NewStack(r[0], 0), r[1]
	} else {
		txt = kit.NewStack(kit.Inset(f.body(), 0, 10, 0, 0), 0)
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
			return kit.Rows(keyArea, 12, s...)
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
		r := txt.Next(22)
		label(c, r, fmt.Sprintf("Step %d of %d", n, total), gfx.Left)
		t := txt.Next(38)
		c.Text(t, title, gfx.Face(true, 24), orange, gfx.Left)
		a.textRegion("Step", image.Rect(r.Min.X, r.Min.Y, r.Max.X, t.Max.Y), gfx.Face(true, 24), title)
		txt.Skip(12)
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%d/%d %s", n, total, title)})
	}
	// para draws a text, with its lines broken to the width.
	para := func(s string) {
		for _, l := range wrapText(face, s, f.body().Dx()) {
			c.Text(txt.Next(lineH), l, face, text, gfx.Left)
		}
	}
	cancelLabel, doneLabel := "Cancel and cool down", "Done, cool down"
	if sw.paused {
		cancelLabel, doneLabel = "Cancel", "Done"
	}

	// heatFor heats the nozzle for a material and goes on to the heating step.
	heatFor := func(material string) {
		t := 210
		for _, m := range materials {
			if m.Name == material {
				t = m.Tool
			}
		}
		sw.temp, sw.step = t, swapHeat
		a.do(func() error { return a.swapHeat(sw, t) })
	}
	rolls := pr.SpoolStore()
	if sw.step == swapChoose && sw.mode != swapUnloadOnly && rolls == nil {
		sw.step = swapMaterial
	}

	switch sw.step {
	case swapChoose:
		if sw.mode != swapUnloadOnly {
			step(1, "Which roll goes in?")
			para("The nozzle heats for the material of the roll.")
			k := keys(size{240, 3}, size{80, 1})
			list := rolls.List()
			a.region("Rolls", k[0])
			if len(list) == 0 {
				noItems(c, k[0], "", "No rolls yet")
			} else {
				l := kit.List{Area: k[0], N: len(list), RowH: 80, Gap: 8, Scroll: &a.spoolScroll}
				in, _ := rolls.In()
				a.list(c, l, func(i int) string { return list[i].Label() }, func(c *gfx.Canvas, i int, r image.Rectangle) {
					title := list[i].Label()
					if list[i].ID == in.ID {
						title += " · loaded"
					}
					a.menuRow(c, r, title, rollLine(list[i]), dim, nil)
				}, func(i int) {
					sp := list[i]
					rolls.Put(sp.ID)
					a.setMaterial(sp.Material)
					heatFor(sp.Material)
				})
			}
			b := kit.Cols(k[1], 12, kit.Fr(1), kit.Fr(1))
			a.button(c, b[0], "New roll", orange, onAcc, func() {
				a.openRollForm(func(sp printer.Spool) { heatFor(sp.Material) })
			})
			a.button(c, b[1], "Other", raised, text, func() { sw.step = swapMaterial })
			return
		}
		step(1, "Which filament comes out?")
		para("The nozzle heats to the temperature of the loaded filament.")
		a.materialKeys(c, sw, keys(size{172, 2}, size{80, 1}))

	case swapMaterial:
		step(1, "Which filament goes in?")
		para("The nozzle heats to the temperature of the new filament.")
		a.materialKeys(c, sw, keys(size{172, 2}, size{80, 1}))
	case swapHeat:
		step(2, "Heating the nozzle")
		var temp, bar, cancel image.Rectangle
		heatText := "The next step starts when the nozzle is hot."
		if a.SwapHome && !sw.paused {
			heatText = "The printer homes while the nozzle heats. The next step starts when the nozzle is hot."
		}
		if f.wide {
			para(heatText)
			// The temperature and its bar under the text, Cancel at the bottom.
			r := kit.Rows(keyArea, 12, kit.Fr(1), kit.Px(56), kit.Px(20), kit.Fr(1), kit.Px(actH))
			temp, bar, cancel = r[1], kit.Inset(r[2], 16, 0, 16, 0), r[4]
		} else {
			// The temperature over the text.
			temp = txt.Next(64)
			txt.Skip(20)
			bar = kit.Inset(txt.Next(20), 16, 0, 16, 0)
			txt.Skip(30)
			para(heatText)
			cancel = keys(size{80, 1})[0]
		}
		c.Text(temp, fmt.Sprintf("%.0f° / %d°", st.Tool, sw.temp), gfx.Face(true, 44), text, gfx.Center)
		segments(c, bar, max(0, min(1, st.Tool/float64(max(sw.temp, 1)))))
		a.lcdNote(printer.LCDRow{Left: fmt.Sprintf("%.0f/%d°", st.Tool, sw.temp), HasBar: true, Bar: st.Tool / float64(max(sw.temp, 1))})
		if st.Tool >= float64(sw.temp)-3 {
			sw.step = swapUnload
			if sw.mode == swapLoadOnly {
				sw.step = swapInsert
			}
			a.Redraw()
		}
		a.button(c, cancel, cancelLabel, raised, text, end)

	case swapUnload:
		step(3, "Unload the old filament")
		para(fmt.Sprintf("The extruder pushes a little out, then pulls the filament back %.0f mm. Hold the filament lightly.", a.UnloadMM))
		k := keys(size{84, 1})
		a.button(c, k[0], "Unload", orange, onAcc, func() {
			sw.step = swapInsert
			if sw.mode == swapUnloadOnly {
				sw.step = swapCheck
				a.setMaterial("")
				if rolls != nil {
					rolls.Put("")
				}
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
			a.button(c, k[0], doneLabel, orange, onAcc, end)
			a.button(c, k[1], "Unload again", raised, text, func() { sw.step = swapUnload })
			return
		}
		step(5, "Check the nozzle")
		para("Clean plastic in the new colour should come out of the nozzle.")
		purge := func(r image.Rectangle) {
			a.button(c, r, "Purge 10 mm more", raised, text, func() {
				a.do(func() error { return pr.Extrude(10, 150) })
			})
		}
		if sw.paused {
			k := keys(size{80, 1}, size{84, 1.4})
			purge(k[0])
			a.button(c, k[1], doneLabel, orange, onAcc, end)
			return
		}
		k := keys(size{80, 1}, size{84, 1.4}, size{80, 1})
		purge(k[0])
		a.button(c, k[1], doneLabel, orange, onAcc, end)
		a.button(c, k[2], "Done, keep hot", raised, text, func() { a.swap = nil })
	}
}

// swapHeat heats the nozzle to t for the swap. With SwapHome, the printer then homes while the
// nozzle heats. A swap during a paused print does not home: the print goes on from its position.
func (a *App) swapHeat(sw *swapState, t int) error {
	if err := a.Printer.SetToolTemp(t); err != nil {
		return err
	}
	if a.SwapHome && !sw.paused {
		return a.Printer.Home()
	}
	return nil
}

// materialKeys draws the materials and Other temperature in k: a tap heats the nozzle for the
// filament. A material that goes in has no roll.
func (a *App) materialKeys(c *gfx.Canvas, sw *swapState, k []image.Rectangle) {
	pr := a.Printer
	a.region("Materials", k[0])
	for i, r := range kit.Grid(k[0], 2, 2, 12) {
		m := materials[i]
		t, name := m.Tool, m.Name
		a.button(c, r, fmt.Sprintf("%s  %d°", m.Name, m.Tool), raised, text, func() {
			sw.temp, sw.step = t, swapHeat
			if sw.mode != swapUnloadOnly {
				a.setMaterial(name)
				if s := pr.SpoolStore(); s != nil {
					s.Put("") // a filament with no roll
				}
			}
			a.do(func() error { return a.swapHeat(sw, t) })
		})
	}
	a.button(c, k[1], "Other temperature", raised, text, func() {
		a.editTemp("Nozzle for the new filament", 220, 300, toolPresets, func(t int) error {
			a.post(func() { sw.temp, sw.step = t, swapHeat })
			return a.swapHeat(sw, t)
		})
	})
}
