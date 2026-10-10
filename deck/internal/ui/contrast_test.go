package ui

import (
	"image/color"
	"math"
	"testing"
)

// contrast is the WCAG contrast ratio of two colours, from 1 to 21.
func contrast(a, b color.RGBA) float64 {
	lum := func(c color.RGBA) float64 {
		ch := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	l1, l2 := lum(a), lum(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// The panel has a narrow viewing angle: text must keep a lot of contrast. At night 7:1 (WCAG AAA),
// by day 4.5:1 (AA) for the secondary ink and the dim LCD text.
// Red cannot have 7:1 on a dark key without turning pink, so red text and red keys need 4.5:1 (AA) in both themes.
func TestRedContrast(t *testing.T) {
	for name, p := range map[string]palette{"night": nightPalette, "day": dayPalette} {
		if r := contrast(p.red, p.raised); r < 4.5 {
			t.Errorf("%s: red text on a key has contrast %.1f", name, r)
		}
		if r := contrast(p.onRed, p.redCap); r < 4.5 {
			t.Errorf("%s: lettering on a red key has contrast %.1f", name, r)
		}
	}
}

func TestContrast(t *testing.T) {
	for _, c := range []struct {
		name string
		p    palette
		min  float64
	}{{"night", nightPalette, 7}, {"day", dayPalette, 4.5}} {
		p := c.p
		pairs := []struct {
			what   string
			fg, bg color.RGBA
		}{
			{"ink on plate", p.text, p.surface},
			{"ink on key", p.text, p.raised},
			{"labels on plate", p.dim, p.surface},
			{"labels on chassis", p.dim, p.bg},
			{"labels on key", p.dim, p.raised},
			{"LCD digits", p.lcdInk, p.lcd},
			{"dim LCD text", p.lcdDim, p.lcd},
			{"ink on orange key", p.onAcc, p.orange},
		}
		for _, pr := range pairs {
			if r := contrast(pr.fg, pr.bg); r < c.min {
				t.Errorf("%s: %s has contrast %.1f, want at least %.1f", c.name, pr.what, r, c.min)
			}
		}
	}
}
