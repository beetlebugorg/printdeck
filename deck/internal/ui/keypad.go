package ui

import (
	"image"
	"strconv"
	"strings"

	"printdeck/internal/gfx"
)

// keypad enters a number, over everything else. It is used for the temperatures.
type keypad struct {
	title   string
	digits  string // what was typed
	max     int
	unit    string
	presets []keypadPreset
	set     func(v int)
}

type keypadPreset struct {
	label string
	v     int
}

// editTemp opens the keypad for a heater. set is called with the new target.
func (a *App) editTemp(title string, current, maxT int, presets []keypadPreset, set func(int) error) {
	a.keypad = &keypad{title: title, digits: strconv.Itoa(current), max: maxT, unit: "°", presets: presets,
		set: func(v int) {
			go func() {
				if err := set(v); err != nil {
					a.Toast(err.Error())
				}
			}()
		}}
}

var (
	toolPresets = []keypadPreset{{"Off", 0}, {"PLA", 210}, {"PETG", 240}, {"ABS", 250}}
	bedPresets  = []keypadPreset{{"Off", 0}, {"PLA", 60}, {"PETG", 80}, {"ABS", 100}}
)

func (a *App) drawKeypad(c *gfx.Canvas) {
	kp := a.keypad
	a.hits = a.hits[:0] // the keypad blocks everything below it
	c.Dim(3)
	box := image.Rect(24, 24, W-24, 670)
	panel(c, box, "")
	c.Fill(image.Rect(box.Min.X+24, box.Min.Y+24, box.Min.X+32, box.Min.Y+40), orange)
	c.Text(image.Rect(box.Min.X+42, box.Min.Y+14, box.Max.X-24, box.Min.Y+50), strings.ToUpper(kp.title), gfx.Face(true, 20), text, gfx.Left)

	// The number. Too high a number is shown in red, and Set does not take it.
	v, _ := strconv.Atoi(kp.digits)
	tooHigh := v > kp.max
	disp := image.Rect(box.Min.X+24, box.Min.Y+58, box.Max.X-24, box.Min.Y+130)
	window(c, disp)
	col := lcdInk
	if tooHigh {
		col = red
	}
	shown := kp.digits
	if shown == "" {
		shown = "0"
	}
	c.Text(disp, shown+kp.unit, gfx.Face(true, 48), col, gfx.Center)

	// Presets
	n := len(kp.presets)
	pw := (box.Dx() - 48 - 8*(n-1)) / n
	for i, p := range kp.presets {
		r := image.Rect(box.Min.X+24+i*(pw+8), box.Min.Y+144, box.Min.X+24+i*(pw+8)+pw, box.Min.Y+188)
		val := p.v
		a.button(c, r, p.label, raised, text, func() { kp.digits = strconv.Itoa(val) })
	}

	// Digits: 3 x 4
	keys := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "C", "0", "Del"}
	kw, kh := (box.Dx()-48-16)/3, 72
	for i, k := range keys {
		x := box.Min.X + 24 + (i%3)*(kw+8)
		y := box.Min.Y + 204 + (i/3)*(kh+8)
		key := k
		a.button(c, image.Rect(x, y, x+kw, y+kh), k, raised, text, func() {
			switch key {
			case "C":
				kp.digits = ""
			case "Del":
				if len(kp.digits) > 0 {
					kp.digits = kp.digits[:len(kp.digits)-1]
				}
			default:
				if len(kp.digits) < 3 {
					kp.digits = trimZero(kp.digits + key)
				}
			}
		})
	}

	// Cancel and Set
	y := box.Max.Y - 84
	half := (box.Dx() - 48 - 16) / 2
	a.button(c, image.Rect(box.Min.X+24, y, box.Min.X+24+half, y+64), "Cancel", raised, text, func() { a.keypad = nil })
	setFill := green
	if tooHigh {
		setFill = raised
	}
	a.button(c, image.Rect(box.Max.X-24-half, y, box.Max.X-24, y+64), "Set", setFill, onAcc, func() {
		if tooHigh {
			return
		}
		a.keypad = nil
		kp.set(v)
	})
}

func trimZero(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}
