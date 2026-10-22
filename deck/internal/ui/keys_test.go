package ui

import "testing"

// The keys of a real keyboard type into the keyboard that is open, and Enter sets the text.
func TestKeysTypeInKeyboard(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	got := ""
	a.consoleKB = &keyboard{max: 20, done: func(v string) { got = v }, back: func() {}}
	for _, s := range []string{"G", "2", "8", "x"} {
		a.key(s, "")
	}
	a.key("", "Backspace")
	a.key("", "Enter")
	if got != "G28" {
		t.Errorf("typed %q, want G28", got)
	}
}

// The keypad types digits, and Enter sets the value when it is not too high.
func TestKeysOnKeypad(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	set := -1
	a.keypad = &keypad{max: 300, set: func(v int) { set = v }}
	a.key("2", "")
	a.key("1a", "")
	a.key("5", "")
	a.key("", "Enter")
	if set != 215 || a.keypad != nil {
		t.Errorf("set %d, keypad open %v; want 215 and closed", set, a.keypad != nil)
	}
	a.keypad = &keypad{max: 300, set: func(v int) { set = v }}
	a.key("9", "")
	a.key("9", "")
	a.key("9", "")
	a.key("", "Enter")
	if a.keypad == nil {
		t.Error("Enter took a value over the maximum")
	}
	a.key("", "Escape")
	if a.keypad != nil {
		t.Error("Escape did not close the keypad")
	}
}
