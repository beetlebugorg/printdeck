// Package all loads the ported modules of klippy/extras and the kinematics: each one registers
// itself with the printer core in its init function. Import it once in the program.
package all

import (
	_ "printdeck/internal/klipper/extras/error_mcu"
	_ "printdeck/internal/klipper/extras/force_move"
	_ "printdeck/internal/klipper/extras/heaters"
	_ "printdeck/internal/klipper/extras/motion_queuing"
	_ "printdeck/internal/klipper/extras/motion_report"
	_ "printdeck/internal/klipper/extras/query_endstops"
	_ "printdeck/internal/klipper/extras/stepper_enable"
	_ "printdeck/internal/klipper/gcode"
	_ "printdeck/internal/klipper/mcu"
	_ "printdeck/internal/klipper/pins"
	_ "printdeck/internal/klipper/webhooks"
)
