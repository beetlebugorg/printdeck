package printer

import (
	"fmt"
	"os"
)

// The backlash of X and Y: CUBE_MEASURE of [printdeck_cube] measures it on the calibration cube,
// and [printdeck_backlash] corrects it (SET_BACKLASH).

// MeasureBacklash homes, measures the backlash on the cube, and returns the backlash of X and Y
// in mm.
func (k *Klippy) MeasureBacklash() (x, y float64, err error) {
	if !k.Machine().Has("printdeck_cube") {
		return 0, 0, fmt.Errorf("printer.cfg has no [printdeck_cube] section")
	}
	if err := k.script(HomeGcode(), "CUBE_MEASURE"); err != nil {
		return 0, 0, err
	}
	st, err := k.Query(map[string][]string{"printdeck_cube": {"backlash_x", "backlash_y"}})
	if err != nil {
		return 0, 0, err
	}
	return num(st["printdeck_cube"]["backlash_x"]), num(st["printdeck_cube"]["backlash_y"]), nil
}

// SetBacklash writes the backlash correction of X and Y in mm to [printdeck_backlash] of
// printer.cfg. With the section, the correction changes at once (SET_BACKLASH). Without it,
// Klipper restarts to load it.
func (k *Klippy) SetBacklash(x, y float64) error {
	if k.ConfigPath == "" {
		return fmt.Errorf("the deck does not know where printer.cfg is")
	}
	b, err := os.ReadFile(k.ConfigPath)
	if err != nil {
		return err
	}
	cfg := setConfigOption(string(b), "printdeck_backlash", "x", fmt.Sprintf("%.2f", x))
	cfg = setConfigOption(cfg, "printdeck_backlash", "y", fmt.Sprintf("%.2f", y))
	if err := os.WriteFile(k.ConfigPath, []byte(cfg), 0o644); err != nil {
		return err
	}
	if k.Machine().Has("printdeck_backlash") {
		k.mu.Lock()
		k.settings.BacklashX, k.settings.BacklashY = x, y
		k.mu.Unlock()
		return k.script(fmt.Sprintf("SET_BACKLASH X=%.2f Y=%.2f", x, y))
	}
	return k.script("RESTART")
}
