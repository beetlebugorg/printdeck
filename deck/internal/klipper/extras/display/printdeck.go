package display

// The hooks of Printdeck. They are not part of Klipper: the module printdeck_display uses them to
// draw the screen of the deck and to take the keys of the knob. With no hooks, the display works
// as in Klipper.

// SetHooks sets the hooks of the display. screen draws the screen and reports true, or reports
// false to let the menu and the display group draw it. key takes a key of the menu ("up",
// "down", "fast_up", "fast_down", "click", "long_click", "back") and reports true, or reports
// false to give it to the menu.
func (d *PrinterLCD) SetHooks(screen func(eventtime float64) bool, key func(key string, eventtime float64) bool) {
	d.screenHook, d.keyHook = screen, key
}

// LCD returns the low level handler of the display.
func (d *PrinterLCD) LCD() LCDChip { return d.lcdChip }
