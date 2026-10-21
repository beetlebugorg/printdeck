package ui

import (
	"image"
	"image/color"
	"printdeck/kit"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
)

// The flash of the Klipper firmware to the printer's board, over the whole screen until it ends.

type flashState struct {
	step        string
	done, total int
	finished    bool
	err         string
}

// askFlash asks, then writes the firmware to the board on device ("" for the board of printer.cfg).
func (a *App) askFlash(device string) {
	a.confirm = &confirmBox{question: "Write the Klipper firmware to the board?", yes: "Write", onYes: func() {
		f := &flashState{step: "Stopping Klipper", total: 1}
		a.flash = f
		pr := a.Printer
		go func() {
			err := pr.FlashBoard(device, func(step string, done, total int) {
				a.post(func() { f.step, f.done, f.total = step, done, total })
			})
			a.post(func() {
				f.finished = true
				if err != nil {
					f.err = err.Error()
				}
				pr.ProbeBoards()
			})
		}()
	}}
}

func (a *App) drawFlash(c *gfx.Canvas) {
	f := a.flash
	area, ok := a.takeover(c, "Firmware")
	s := kit.NewStack(kit.Inset(area, 0, 6, 0, 0), 0)
	title := func(t string, col color.RGBA) {
		r := s.Next(36)
		c.Text(r, t, gfx.Face(true, 24), col, gfx.Left)
		a.textRegion("Step", r, gfx.Face(true, 24), t)
		s.Skip(20)
	}
	switch {
	case !f.finished:
		title(f.step+"…", text)
		bar := s.Next(40)
		window(c, bar)
		a.region("Progress", bar)
		if f.total > 0 {
			w := (bar.Dx() - 8) * f.done / f.total
			c.RoundRect(image.Rect(bar.Min.X+4, bar.Min.Y+4, bar.Min.X+4+w, bar.Max.Y-4), 3, lcdInk)
		}
		s.Skip(30)
		paragraph(c, s, "Do not switch off the printer, and do not disconnect the USB cable. This takes about one minute.", dim)
	case f.err != "":
		title("The flash failed", red)
		paragraph(c, s, f.err, text)
		paragraph(c, s, "The bootloader of the board does not change, so you can try again. Klipper restarts.", dim)
	default:
		title("The firmware is on the board", text)
		paragraph(c, s, "Klipper "+board.FirmwareVersion()+" runs on the board now. Klipper restarts and connects to it.", dim)
	}
	if f.finished {
		a.button(c, ok, "OK", orange, onAcc, func() { a.flash = nil })
	}
}
