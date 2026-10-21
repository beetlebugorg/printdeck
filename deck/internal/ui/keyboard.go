package ui

import (
	"image"
	"printdeck/kit"
	"strings"
	"unicode/utf8"

	"printdeck/internal/gfx"
)

// keyboard enters a line of text on the setup screen. It has a layer of letters and a layer of
// digits and symbols. Together the layers have all printable ASCII characters.
type keyboard struct {
	title  string
	value  string
	secret bool // a password: dots until Show is on
	show   bool
	shift  bool
	sym    bool
	max    int
	ok     string // the label of the key that takes the text
	cancel string // the label of the key that goes back
	done   func(v string)
	back   func()
}

var (
	kbLetters  = []string{"1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm"}
	kbSymbols  = []string{"1234567890", `-/:;()$&@"`, ".,?!'_#%*+", "=[]{}<>"}
	kbSymbols2 = `\|~^` + "`" // the last row of the symbol layer with Shift on
)

const (
	kbX0, kbX1 = 24, W - 24
	kbKeyH     = 56
	kbGap      = 4
	kbRowGap   = 8
)

// kbY0 is the top of the keyboard, at the bottom of the screen (setHeight).
var kbY0 = BaseH - 24 - 5*kbKeyH - 4*kbRowGap

// The touch areas of the keys reach half way into the gaps between the keys and between the rows.
var (
	kbPad       = image.Pt(kbGap/2, kbRowGap/2)
	kbBottomPad = image.Pt(4, kbRowGap/2) // #+= and space have 8 pixels between them
)

// rows returns the character rows of the current layer.
func (k *keyboard) rows() []string {
	if !k.sym {
		rows := append([]string(nil), kbLetters...)
		if k.shift {
			for i := 1; i < len(rows); i++ {
				rows[i] = strings.ToUpper(rows[i])
			}
		}
		return rows
	}
	rows := append([]string(nil), kbSymbols...)
	if k.shift {
		rows[3] = kbSymbols2
	}
	return rows
}

// typed adds one character. Shift is for one letter, like on a phone.
func (k *keyboard) typed(s string) {
	if k.max > 0 && len(k.value)+len(s) > k.max {
		return
	}
	k.value += s
	if !k.sym {
		k.shift = false
	}
}

func (a *App) drawKeyboard(c *gfx.Canvas, k *keyboard) {
	box := image.Rect(12, 12, W-12, H-12)
	panel(c, box, "")
	c.Fill(image.Rect(box.Min.X+24, box.Min.Y+24, box.Min.X+32, box.Min.Y+40), orange)
	c.Text(image.Rect(box.Min.X+42, box.Min.Y+14, box.Max.X-24, box.Min.Y+50), strings.ToUpper(gfx.Fit(gfx.Face(true, 18), k.title, box.Dx()-70)), gfx.Face(true, 18), text, gfx.Left)

	// The text, with its end in view.
	disp := image.Rect(kbX0, 70, kbX1, 134)
	window(c, disp)
	shown := k.value
	if k.secret && !k.show {
		shown = strings.Repeat("•", len([]rune(k.value)))
	}
	face := gfx.Face(true, 26)
	shown += "_"
	for gfx.Width(face, shown) > disp.Dx()-32 && len(shown) > 1 {
		_, n := utf8.DecodeRuneInString(shown)
		shown = shown[n:]
	}
	c.Text(image.Rect(disp.Min.X+16, disp.Min.Y, disp.Max.X-16, disp.Max.Y), shown, face, lcdInk, gfx.Left)

	// Back, Show, and the key that takes the text
	y := 152
	n := 2
	if k.secret {
		n = 3
	}
	bw := (kbX1 - kbX0 - (n-1)*8) / n
	x := kbX0
	a.button(c, image.Rect(x, y, x+bw, y+64), k.cancel, raised, text, func() { k.back() })
	x += bw + 8
	if k.secret {
		fill, ink := raised, text
		if k.show {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, image.Rect(x, y, x+bw, y+64), "Show", fill, ink, func() { k.show = !k.show })
		x += bw + 8
	}
	a.button(c, image.Rect(x, y, kbX1, y+64), k.ok, green, onAcc, func() { k.done(k.value) })

	// The keys
	rows := k.rows()
	kw := (kbX1 - kbX0 - 9*kbGap) / 10
	for i, row := range rows {
		ry := kbY0 + i*(kbKeyH+kbRowGap)
		chars := []rune(row)
		width := len(chars)*kw + (len(chars)-1)*kbGap
		x := kbX0 + (kbX1-kbX0-width)/2
		if i == 3 {
			// Shift and Delete at the two ends of the last row of characters. Their touch areas
			// reach to the touch areas of the letters.
			sw := 62
			shiftPad := image.Pt(x-kbGap/2-(kbX0+sw), kbRowGap/2)
			delPad := image.Pt(kbX1-sw-(x+width+kbGap/2), kbRowGap/2)
			a.textKey(c, image.Rect(kbX0, ry, kbX0+sw, ry+kbKeyH), shiftPad, "shift", k.shift, func() { k.shift = !k.shift })
			a.textKey(c, image.Rect(kbX1-sw, ry, kbX1, ry+kbKeyH), delPad, "del", false, func() {
				if r := []rune(k.value); len(r) > 0 {
					k.value = string(r[:len(r)-1])
				}
			})
		}
		for _, ch := range chars {
			s := string(ch)
			a.textKey(c, image.Rect(x, ry, x+kw, ry+kbKeyH), kbPad, s, false, func() { k.typed(s) })
			x += kw + kbGap
		}
	}
	ry := kbY0 + 4*(kbKeyH+kbRowGap)
	layer := "#+="
	if k.sym {
		layer = "abc"
	}
	a.textKey(c, image.Rect(kbX0, ry, kbX0+100, ry+kbKeyH), kbBottomPad, layer, k.sym, func() { k.sym, k.shift = !k.sym, false })
	a.textKey(c, image.Rect(kbX0+108, ry, kbX1, ry+kbKeyH), kbBottomPad, "space", false, func() { k.typed(" ") })
}

// textKey is a key with a label in its own case. key() writes all labels in capitals. The touch
// area of the key is r with pad added on each side: half the gap to the next key, so that a tap in
// a gap goes to the nearest key.
func (a *App) textKey(c *gfx.Canvas, r image.Rectangle, pad image.Point, label string, on bool, tap func()) {
	fill, ink := raised, text
	if on {
		fill, ink = keyOn, keyOnInk
	}
	hr := image.Rectangle{r.Min.Sub(pad), r.Max.Add(pad)}
	pressed := a.isPressed(hr)
	key(c, r, "", fill, ink, pressed)
	face := image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Max.Y-5)
	if pressed {
		face = face.Add(image.Pt(0, 4))
	}
	size := 22
	if len(label) > 1 {
		size = 16 // a word, like shift
	}
	c.Text(face, label, gfx.Face(false, size), ink, gfx.Center)
	a.addHit(kit.Hit{R: hr, Tap: tap})
}
