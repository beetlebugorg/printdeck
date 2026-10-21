package printer

import "printdeck/internal/lcd"

// The screens of the display of the printer, for the module printdeck_display of the Klipper
// host (package lcd draws them).
type (
	LCDScreen   = lcd.Screen
	LCDRow      = lcd.Row
	LCDStatus   = lcd.Status
	LCDMenu     = lcd.Menu
	LCDMenuItem = lcd.MenuItem
)
