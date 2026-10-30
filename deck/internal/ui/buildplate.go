package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// drawBuildPlate is the page of the build plate of a resin printer, in the place of the toolhead:
// the plate has only Z. The keys home Z and move the plate by a step. The backend of the printer
// decides if a move goes to the printer: the ChiTu backend sends no move before its test.
func (a *App) drawBuildPlate(c *gfx.Canvas) {
	pr := a.Printer
	st := pr.State()
	f := a.navPage(c, "Build plate")
	moving := st.Connected && !st.Busy()

	body := f.body()
	if f.wide {
		body, _ = f.split()
	}
	r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Px(64), kit.Px(58), kit.Px(52), kit.Px(58), kit.Fr(1))
	pos, home, steps, zRow, note := r[0], r[1], r[2], r[3], r[4]

	window(c, pos)
	a.region("Position", pos)
	z := "Z --"
	if st.HasZ {
		z = fmt.Sprintf("Z %.2f mm", st.Z)
	}
	c.Text(kit.Inset(pos, 18, 0, 18, 0), z, gfx.Face(true, 28), lcdInk, gfx.Left)

	move := func(r image.Rectangle, label string, f func() error) {
		if !moving {
			key(c, r, label, surface, dim, false)
			a.region(label, r)
			a.addHit(kit.Hit{R: r, Tap: func() { a.Toast("Not during a print") }})
			return
		}
		a.button(c, r, label, raised, text, func() { a.do(f) })
	}
	move(home, "Home Z", func() error { return pr.Home("z") })
	for i, sr := range kit.Cols(steps, 6, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		s := []float64{0.1, 1, 10}[i]
		fill, fg := keyOff, text
		if a.zStep == s {
			fill, fg = keyOn, keyOnInk
		}
		key(c, sr, fmt.Sprint(s)+" mm", fill, fg, a.isPressed(sr))
		a.addHit(kit.Hit{R: sr, Tap: func() { a.zStep = s }})
	}
	zr := kit.Cols(zRow, 8, kit.Fr(1), kit.Fr(1))
	move(zr[0], "Plate down", func() error { return pr.Jog(0, 0, -a.zStep, 600) })
	move(zr[1], "Plate up", func() error { return pr.Jog(0, 0, a.zStep, 600) })

	face := gfx.Face(false, 16)
	lines := wrapText(face, "The deck moves the plate after a test of the moves on this printer. Until then, move the plate on the screen of the printer.", note.Dx())
	for i, l := range lines {
		c.Text(image.Rect(note.Min.X, note.Min.Y+8+i*24, note.Max.X, note.Min.Y+32+i*24), l, face, dim, gfx.Left)
	}
}
