package ui

import (
	"image"
	"strings"

	"printdeck/internal/gfx"
)

// DrawSplash draws the start screen: the Printdeck name on a character display, in a plate on the chassis.
// The system shows it from the first second of the boot until the deck draws its first frame.
// version is printed at the bottom, for example "v2.1.0". It can be empty. night selects the night palette.
func DrawSplash(c *gfx.Canvas, version string, night bool) {
	setNight(night)
	c.Fill(image.Rect(0, 0, W, H), bg)

	box := image.Rect(32, 252, W-32, 548)
	panel(c, box, "00  SYSTEM")

	// The name, one letter in each cell, like the display of the printer.
	charLCD(c, image.Rect(box.Min.X+28, box.Min.Y+40, box.Max.X-28, box.Min.Y+128), []string{"PRINTDECK"}, 9)

	// The nozzle and the bed, under the name.
	cx := W / 2
	drawNozzle(c, cx, box.Min.Y+186, 26, text)
	c.RoundRect(image.Rect(box.Min.X+80, box.Min.Y+226, box.Max.X-80, box.Min.Y+234), 2, text)
	c.Fill(image.Rect(cx-36, box.Min.Y+220, cx+36, box.Min.Y+226), orange) // the first layer

	c.Text(image.Rect(box.Min.X+24, box.Max.Y-48, box.Max.X-24, box.Max.Y-20), "STARTING", gfx.Face(true, 16), dim, gfx.Center)

	// The hint for the diagnostics screen, and the version.
	face := gfx.Face(false, 14)
	c.Text(image.Rect(24, H-96, W-24, H-72), "Hold the screen for the boot messages.", face, dim, gfx.Center)
	if version != "" {
		c.Text(image.Rect(24, H-64, W-24, H-40), strings.ToUpper(version), gfx.Face(true, 13), dim, gfx.Center)
	}
}
