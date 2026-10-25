package batch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// unsupported are the example configs that the Go host does not load yet, with the start of
// their error. Remove a config from the list when its modules are ported.
var unsupported = map[string]string{
	"printer-anet-a4-2018.cfg":                      "Section 'delta_calibrate' is not a valid config section",
	"printer-anycubic-kobra-plus-2022.cfg":          "Section 'tmc2209 stepper_x' is not a valid config section",
	"printer-anycubic-kossel-plus-2017.cfg":         "Section 'delta_calibrate' is not a valid config section",
	"printer-artillery-sidewinder-x2-2022.cfg":      "Section 'temperature_sensor mainboard' is not a valid config section",
	"printer-artillery-sidewinder-x3-plus-2024.cfg": "Section 'led led_light' is not a valid config section",
	"printer-biqu-b1-se-plus-2022.cfg":              "Section 'tmc2208 stepper_x' is not a valid config section",
	"printer-biqu-bx-2021.cfg":                      "Unknown pin chip name 'tmc2209_stepper_x'",
	"printer-creality-cr10-smart-pro-2022.cfg":      "Section 'temperature_sensor board_mcu' is not a valid config section",
	"printer-creality-cr10s-pro-v2-2020.cfg":        "Section 'screws_tilt_adjust' is not a valid config section",
	"printer-creality-cr30-2021.cfg":                "Section 'fan_generic k_fan2' is not a valid config section",
	"printer-creality-cr5pro-ht-2022.cfg":           "Section 'screws_tilt_adjust' is not a valid config section",
	"printer-creality-ender3-v2-neo-2022.cfg":       "Section 'screws_tilt_adjust' is not a valid config section",
	"printer-creality-ender5-s1-2023.cfg":           "Section 'axis_twist_compensation' is not a valid config section",
	"printer-creality-sermoonV1-2022.cfg":           "Section 'fan_generic side_fan' is not a valid config section",
	"printer-elegoo-neptune3-pro-2023.cfg":          "Section 'led top_leds' is not a valid config section",
	"printer-eryone-er20-2021.cfg":                  "Unknown pin chip name 'tmc2209_stepper_x'",
	"printer-flashforge-creator-pro-2018.cfg":       "lcd_type 'hd44780_spi' in section 'display' is not supported",
	"printer-flsun-q5-2020.cfg":                     "Section 'delta_calibrate' is not a valid config section",
	"printer-flsun-qqs-2020.cfg":                    "Section 'delta_calibrate' is not a valid config section",
	"printer-geeetech-301-2019.cfg":                 "Unknown pin chip name 'multi_pin'",
	"printer-geeetech-A10T-A20T-2021.cfg":           "lcd_type 'aip31068_spi' in section 'display' is not supported",
	"printer-hiprecy-leo-2019.cfg":                  "Section 'homing_override' is not a valid config section",
	"printer-lulzbot-mini1-2016.cfg":                "Section 'homing_override' is not a valid config section",
	"printer-makergear-m2-2012.cfg":                 "Section 'endstop_phase stepper_x' is not a valid config section",
	"printer-micromake-d1-2016.cfg":                 "Section 'delta_calibrate' is not a valid config section",
	"printer-modix-big60-2020.cfg":                  "Unknown temperature sensor 'MAX31865'",
	"printer-monoprice-mini-delta-2017.cfg":         "Section 'delta_calibrate' is not a valid config section",
	"printer-prusa-mini-plus-2020.cfg":              "tachometer_pin in section 'heater_fan hotend_fan' is not supported yet",
	"printer-ratrig-v-minion-2021.cfg":              "Section 'tmc2209 stepper_x' is not a valid config section",
	"printer-sovol-sv05-2022.cfg":                   "Section 'screws_tilt_adjust' is not a valid config section",
	"printer-sovol-sv06-2022.cfg":                   "Unknown pin chip name 'tmc2209_stepper_x'",
	"printer-sovol-sv06-plus-2023.cfg":              "Unknown pin chip name 'tmc2209_stepper_x'",
	"printer-sunlu-t3-2022.cfg":                     "Section 'filament_motion_sensor filament_sensor' is not a valid config section",
	"printer-tevo-flash-2018.cfg":                   "Section 'homing_override' is not a valid config section",
	"printer-tronxy-crux1-2022.cfg":                 "Section 'pwm_cycle_time beeper_pin' is not a valid config section",
	"printer-velleman-k8800-2017.cfg":               "Section 'delta_calibrate' is not a valid config section",
}

// chipDicts returns the firmware dictionaries to try for a chip (testdata/dict, from
// tools/klipper-reference/make-dicts.sh): the default build, then the serial build.
func chipDicts(mcu string) []string {
	switch {
	case mcu == "gd32f103":
		mcu = "stm32f103"
	case strings.HasPrefix(mcu, "gd32f4"):
		mcu = "stm32f446"
	}
	out := []string{"../testdata/dict/" + mcu + ".dict"}
	if _, err := os.Stat("../testdata/dict/" + mcu + "-serial.dict"); err == nil {
		out = append(out, "../testdata/dict/"+mcu+"-serial.dict")
	}
	return out
}

// Each example config of Klipper that the deck offers on the setup of a new printer loads in the
// Go host, with the Printdeck macros at the end, and the printer homes (testdata/load.gcode).
func TestExamplesLoad(t *testing.T) {
	macros, err := filepath.Abs("../../../../os/board/rootfs/usr/share/printdeck/klipper/printdeck.cfg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, e := range printer.Examples() {
		text, err := printer.ExampleConfig(e.File, "/dev/ttyACM0")
		if err != nil {
			t.Fatal(err)
		}
		text = strings.Replace(text, "/usr/share/printdeck/klipper/printdeck.cfg", macros, 1)
		cfg := filepath.Join(dir, e.File)
		if err := os.WriteFile(cfg, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		var result, first string
		for _, dict := range chipDicts(e.MCU) {
			var msg string
			result, msg = Run(cfg, dict, "../testdata/load.gcode", filepath.Join(dir, "out.serial"))
			first, _, _ = strings.Cut(msg, "\n")
			if !strings.Contains(first, " is reserved for ") {
				break
			}
		}
		want, known := unsupported[e.File]
		switch {
		case result == "exit" && known:
			t.Errorf("%s loads now: remove it from unsupported", e.File)
		case result != "exit" && !known:
			t.Errorf("%s: %s: %s", e.File, result, first)
		case result != "exit" && !strings.HasPrefix(first, want):
			t.Errorf("%s: the error is now %q, want %q", e.File, first, want)
		}
	}
}
