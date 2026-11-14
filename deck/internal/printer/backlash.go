package printer

import (
	"fmt"
	"os"
)

// The backlash of X and Y: CUBE_MEASURE of [printdeck_cube] measures it on the calibration cube,
// and [printdeck_backlash] corrects it (SET_BACKLASH).

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

// CubeResult reads the last results of [printdeck_cube]: the backlash of X and Y of
// CUBE_MEASURE, and the z_offset of CUBE_PROBE_OFFSET, in mm.
func (k *Klippy) CubeResult() (x, y, zOffset float64, err error) {
	st, err := k.Query(map[string][]string{"printdeck_cube": {"backlash_x", "backlash_y", "probe_z_offset"}})
	if err != nil {
		return 0, 0, 0, err
	}
	c := st["printdeck_cube"]
	return num(c["backlash_x"]), num(c["backlash_y"]), num(c["probe_z_offset"]), nil
}
