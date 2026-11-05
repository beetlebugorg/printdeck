package ui

import (
	"image"
	"math"
	"printdeck/kit"
	"strconv"
	"strings"

	"printdeck/internal/gfx"
)

// keypad enters a number, over everything else. It is used for the temperatures, and for a
// position of the head. A decimal keypad has a minus key and a point key in place of the
// presets, and gives a float64 from lo to hi to setF.
type keypad struct {
	title   string
	digits  string // what was typed
	max     int
	unit    string
	presets []keypadPreset
	set     func(v int)

	decimal bool
	lo, hi  float64
	setF    func(v float64)
}

// keypadDecimals is the number of digits after the point on a decimal keypad.
const keypadDecimals = 2

// value is the typed number, and false when it is out of the range.
func (kp *keypad) value() (float64, bool) {
	if !kp.decimal {
		v, _ := strconv.Atoi(kp.digits)
		return float64(v), v <= kp.max
	}
	v, _ := strconv.ParseFloat(strings.TrimSuffix(kp.digits, "."), 64) // "", "-" and "-." are 0
	return v, v >= kp.lo && v <= kp.hi
}

// add adds a typed key: a digit, or "-" and "." on a decimal keypad. "-" changes the sign.
func (kp *keypad) add(k string) {
	if !kp.decimal {
		// As many digits as the maximum has: 3 for a temperature, 4 for grams.
		if k != "-" && k != "." && len(kp.digits) < len(strconv.Itoa(kp.max)) {
			kp.digits = trimZero(kp.digits + k)
		}
		return
	}
	sign, num := "", kp.digits
	if strings.HasPrefix(num, "-") {
		sign, num = "-", num[1:]
	}
	switch k {
	case "-":
		if sign == "" {
			sign = "-"
		} else {
			sign = ""
		}
	case ".":
		if strings.Contains(num, ".") {
			return
		}
		if num == "" {
			num = "0"
		}
		num += "."
	default:
		if i := strings.Index(num, "."); i >= 0 && len(num)-1-i >= keypadDecimals {
			return
		}
		if len(num) >= 7 {
			return
		}
		num += k
		if !strings.Contains(num, ".") {
			num = trimZero(num)
		}
	}
	kp.digits = sign + num
}

// done closes the keypad and gives the value to set or setF. An out-of-range value does not
// close it.
func (a *App) keypadDone() {
	kp := a.keypad
	v, ok := kp.value()
	if !ok {
		return
	}
	a.keypad = nil
	if kp.decimal {
		kp.setF(v)
		return
	}
	kp.set(int(v))
}

// editPosition opens a decimal keypad for a position from lo to hi mm. set is called with the
// new position.
func (a *App) editPosition(title string, current, lo, hi float64, set func(float64) error) {
	cur := strconv.FormatFloat(math.Round(current*100)/100, 'f', -1, 64)
	a.keypad = &keypad{title: title, digits: cur, unit: " mm", decimal: true, lo: lo, hi: hi,
		setF: func(v float64) {
			go func() {
				if err := set(v); err != nil {
					a.Toast(err.Error())
				}
			}()
		}}
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
	a.in.Reset() // the keypad blocks everything below it
	c.Dim(3)
	// Tall: the title, the number, the presets, the digits, and Cancel and Set under each other.
	// Wide: the digits fill the right half of the box.
	scr := kit.Screen{W: W, H: contentY1}
	var box, left, digits image.Rectangle
	if scr.Wide() {
		box = kit.Inset(scr.Bounds(), 16, 16, 16, 16)
		cols := kit.Cols(kit.Inset(box, 24, 14, 24, 20), 24, kit.Fr(1), kit.Fr(1))
		left, digits = cols[0], cols[1]
	} else {
		box = image.Rect(margin, margin, W-margin, 670)
		left = kit.Inset(box, 24, 14, 24, 20)
		digits = image.Rect(left.Min.X, box.Min.Y+204, left.Max.X, box.Min.Y+204+4*72+3*8)
	}
	panel(c, box, "")
	s := kit.NewStack(left, 0)
	title := s.Next(36)
	c.Fill(image.Rect(title.Min.X, title.Min.Y+10, title.Min.X+8, title.Min.Y+26), orange)
	c.Text(kit.Inset(title, 18, 0, 0, 0), strings.ToUpper(kp.title), gfx.Face(true, 20), text, gfx.Left)
	s.Skip(8)

	// The number. A number out of the range is shown in red, and Set does not use it.
	_, ok := kp.value()
	tooHigh := !ok
	disp := s.Next(72)
	window(c, disp)
	a.region("Value", disp)
	col := lcdInk
	if tooHigh {
		col = red
	}
	shown := kp.digits
	if shown == "" {
		shown = "0"
	}
	c.Text(disp, shown+kp.unit, gfx.Face(true, 48), col, gfx.Center)
	s.Skip(14)

	// Presets
	sizes := make([]kit.Size, len(kp.presets))
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	presets := s.Next(44)
	a.region("Presets", presets)
	if kp.decimal {
		// The minus key and the point key.
		for i, r := range kit.Cols(presets, 8, kit.Fr(1), kit.Fr(1)) {
			k := []string{"-", "."}[i]
			a.button(c, r, []string{"−", "."}[i], raised, text, func() { kp.add(k) })
		}
	}
	for i, r := range kit.Cols(presets, 8, sizes...) {
		if kp.decimal {
			break
		}
		val := kp.presets[i].v
		a.button(c, r, kp.presets[i].label, raised, text, func() { kp.digits = strconv.Itoa(val) })
	}

	// Digits: 3 x 4
	keys := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "C", "0", "Del"}
	a.region("Keys", digits)
	for i, r := range kit.Grid(digits, 3, 4, 8) {
		key := keys[i]
		a.button(c, r, key, raised, text, func() {
			switch key {
			case "C":
				kp.digits = ""
			case "Del":
				if len(kp.digits) > 0 {
					kp.digits = kp.digits[:len(kp.digits)-1]
				}
			default:
				kp.add(key)
			}
		})
	}

	// Cancel and Set, at the bottom.
	end := kit.Cols(kit.Rows(left, 0, kit.Fr(1), kit.Px(64))[1], 16, kit.Fr(1), kit.Fr(1))
	a.button(c, end[0], "Cancel", raised, text, func() { a.keypad = nil })
	setFill := green
	if tooHigh {
		setFill = raised
	}
	a.button(c, end[1], "Set", setFill, onAcc, a.keypadDone)
}

func trimZero(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}
