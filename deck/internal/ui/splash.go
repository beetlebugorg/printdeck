package ui

import (
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// DrawSplash draws the start screen: the Printdeck name on a character display, in a plate on the chassis.
// The system shows it from the first second of the boot until the deck draws its first frame.
// version is printed at the bottom, for example "v2.1.0". It can be empty. night selects the night palette.
func DrawSplash(c *gfx.Canvas, version string, night bool) {
	setNight(night)
	scr := c.Bounds()
	c.Fill(scr, bg)

	// The plate in the middle of the screen, as wide as the tall screen.
	box := kit.Center(scr, BaseW-64, 296)
	panel(c, box, "00  SYSTEM")

	// The name, one letter in each cell, like the display of the printer.
	charLCD(c, kit.Inset(box, 28, 40, 28, box.Dy()-128), []string{"PRINTDECK"}, 9)

	// The nozzle and the bed, under the name.
	cx := box.Min.X + box.Dx()/2
	drawNozzle(c, cx, box.Min.Y+186, 26, text)
	c.RoundRect(image.Rect(box.Min.X+80, box.Min.Y+226, box.Max.X-80, box.Min.Y+234), 2, text)
	c.Fill(image.Rect(cx-36, box.Min.Y+220, cx+36, box.Min.Y+226), orange) // the first layer

	c.Text(kit.Inset(box, 24, box.Dy()-48, 24, 20), "STARTING", gfx.Face(true, 16), dim, gfx.Center)

	// The hint for the diagnostics screen, and the version, at the bottom.
	foot := kit.Rows(kit.Inset(scr, 24, scr.Dy()-96, 24, 40), 8, kit.Px(24), kit.Px(24))
	c.Text(foot[0], "Hold the screen for the boot messages.", gfx.Face(false, 14), dim, gfx.Center)
	if version != "" {
		c.Text(foot[1], strings.ToUpper(version), gfx.Face(true, 13), dim, gfx.Center)
	}
}
