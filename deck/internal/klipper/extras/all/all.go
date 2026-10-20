// Package all loads the ported modules of klippy/extras and the kinematics: each one registers
// itself with the printer core in its init function. Import it once in the program.
package all

import (
	_ "printdeck/internal/klipper/extras/heaters"
	_ "printdeck/internal/klipper/gcode"
	_ "printdeck/internal/klipper/mcu"
	_ "printdeck/internal/klipper/pins"
)
