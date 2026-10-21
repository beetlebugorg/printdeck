package ui

import (
	"image"
	"image/color"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
)

// The flash of the Klipper firmware to the printer's board, over the whole Print tab until it ends.

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
	heading(c, "Firmware")
	y := 130
	face := gfx.Face(false, 20)
	title := func(t string, col color.RGBA) {
		c.Text(image.Rect(24, y, W-24, y+36), t, gfx.Face(true, 24), col, gfx.Left)
		y += 56
	}
	para := func(t string, col color.RGBA) {
		for _, l := range wrapText(face, t, W-48) {
			c.Text(image.Rect(24, y, W-24, y+30), l, face, col, gfx.Left)
			y += 30
		}
		y += 20
	}
	switch {
	case !f.finished:
		title(f.step+"…", text)
		bar := image.Rect(24, y, W-24, y+40)
		window(c, bar)
		if f.total > 0 {
			w := (bar.Dx() - 8) * f.done / f.total
			c.RoundRect(image.Rect(bar.Min.X+4, bar.Min.Y+4, bar.Min.X+4+w, bar.Max.Y-4), 3, lcdInk)
		}
		y += 70
		para("Do not switch off the printer, and do not disconnect the USB cable. This takes about one minute.", dim)
	case f.err != "":
		title("The flash failed", red)
		para(f.err, text)
		para("The bootloader of the board does not change, so you can try again. Klipper restarts.", dim)
	default:
		title("The firmware is on the board", text)
		para("Klipper "+board.FirmwareVersion()+" runs on the board now. Klipper restarts and connects to it.", dim)
	}
	if f.finished {
		a.button(c, image.Rect(24, contentY1-96, W-24, contentY1-16), "OK", orange, onAcc, func() { a.flash = nil })
	}
}
