package ui

import (
	"strconv"
	"unicode"
)

// Key runs a key of a real keyboard: the keyboard of the computer of the desktop app, or of a
// browser. text is the text that the key types, or name is "Enter", "Escape" or "Backspace". The
// key goes to what is open: the keypad, a keyboard, or a question. Else Escape goes back. It is
// safe to call from any goroutine.
func (a *App) Key(text, name string) {
	a.post(func() {
		a.key(text, name)
		a.Redraw()
	})
}

func (a *App) key(text, name string) {
	switch {
	case a.keypad != nil:
		a.keypadKey(text, name)
	case a.openKeyboard() != nil:
		k := a.openKeyboard()
		switch name {
		case "Enter":
			k.done(k.value)
		case "Escape":
			k.back()
		case "Backspace":
			if r := []rune(k.value); len(r) > 0 {
				k.value = string(r[:len(r)-1])
			}
		case "":
			if k.max == 0 || len(k.value)+len(text) <= k.max {
				k.value += text
			}
		}
	case a.confirm != nil:
		cf := a.confirm
		switch name {
		case "Enter":
			a.confirm = nil
			cf.onYes()
		case "Escape":
			a.confirm = nil
			if cf.onNo != nil {
				cf.onNo()
			}
		}
	case name == "Escape" && len(a.pages) > 0:
		a.back()
	}
}

// keypadKey is a key on the keypad: a digit, Backspace, Enter (Set) or Escape (Cancel).
func (a *App) keypadKey(text, name string) {
	kp := a.keypad
	switch name {
	case "Enter":
		if v, _ := strconv.Atoi(kp.digits); v <= kp.max {
			a.keypad = nil
			kp.set(v)
		}
	case "Escape":
		a.keypad = nil
	case "Backspace":
		if len(kp.digits) > 0 {
			kp.digits = kp.digits[:len(kp.digits)-1]
		}
	case "":
		for _, r := range text {
			if unicode.IsDigit(r) && len(kp.digits) < len(strconv.Itoa(kp.max)) {
				kp.digits = trimZero(kp.digits + string(r))
			}
		}
	}
}

// openKeyboard is the keyboard that is open, or nil.
func (a *App) openKeyboard() *keyboard {
	switch {
	case a.setup != nil && a.setup.kb != nil:
		return a.setup.kb
	case a.form != nil && a.form.kb != nil:
		return a.form.kb
	case a.consoleKB != nil:
		return a.consoleKB
	case a.camKB != nil:
		return a.camKB
	}
	return nil
}
