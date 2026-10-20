// Package all loads the ported modules of klippy/extras and the kinematics: each one registers
// itself with the printer core in its init function. Import it once in the program.
package all

import (
	_ "printdeck/internal/klipper/extras/adc_temperature"
	_ "printdeck/internal/klipper/extras/error_mcu"
	_ "printdeck/internal/klipper/extras/force_move"
	_ "printdeck/internal/klipper/extras/garbage_collection"
	_ "printdeck/internal/klipper/extras/gcode_macro"
	_ "printdeck/internal/klipper/extras/gcode_move"
	_ "printdeck/internal/klipper/extras/heaters"
	_ "printdeck/internal/klipper/extras/homing"
	_ "printdeck/internal/klipper/extras/idle_timeout"
	_ "printdeck/internal/klipper/extras/motion_queuing"
	_ "printdeck/internal/klipper/extras/motion_report"
	_ "printdeck/internal/klipper/extras/query_adc"
	_ "printdeck/internal/klipper/extras/query_endstops"
	_ "printdeck/internal/klipper/extras/statistics"
	_ "printdeck/internal/klipper/extras/stepper_enable"
	_ "printdeck/internal/klipper/extras/thermistor"
	_ "printdeck/internal/klipper/gcode"
	_ "printdeck/internal/klipper/kinematics/cartesian"
	_ "printdeck/internal/klipper/kinematics/extruder"
	_ "printdeck/internal/klipper/mcu"
	_ "printdeck/internal/klipper/pins"
	_ "printdeck/internal/klipper/toolhead"
	_ "printdeck/internal/klipper/webhooks"
)
