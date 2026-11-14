package printer

import (
	"fmt"
	"os"
	"time"
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

// MeasureProbeOffset heats the bed and the nozzle to the temperatures, homes, and measures the
// z_offset of the probe on the cube (CUBE_PROBE_OFFSET). The heaters go off at the end.
func (k *Klippy) MeasureProbeOffset(bedTemp, toolTemp int) (float64, error) {
	if !k.Machine().Has("printdeck_cube") {
		return 0, fmt.Errorf("printer.cfg has no [printdeck_cube] section")
	}
	defer k.script("M104 S0", "M140 S0")
	if err := k.script(fmt.Sprintf("M140 S%d", bedTemp), fmt.Sprintf("M104 S%d", toolTemp)); err != nil {
		return 0, err
	}
	// The wait polls the state: a heat-up of the bed can take longer than one call.
	for end := time.Now().Add(20 * time.Minute); ; time.Sleep(2 * time.Second) {
		st := k.State()
		if !st.Connected {
			return 0, ErrNotReady
		}
		if st.BedTarget != float64(bedTemp) || st.ToolTarget != float64(toolTemp) {
			return 0, fmt.Errorf("the heater targets changed: the measure stopped")
		}
		if st.Bed >= float64(bedTemp)-1 && st.Tool >= float64(toolTemp)-2 {
			break
		}
		if time.Now().After(end) {
			return 0, fmt.Errorf("the heaters did not get to %d° and %d° in 20 minutes", bedTemp, toolTemp)
		}
	}
	if err := k.script(HomeGcode(), "CUBE_PROBE_OFFSET"); err != nil {
		return 0, err
	}
	st, err := k.Query(map[string][]string{"printdeck_cube": {"probe_z_offset"}})
	if err != nil {
		return 0, err
	}
	return num(st["printdeck_cube"]["probe_z_offset"]), nil
}

// SaveProbeOffset saves the last z_offset of the cube in printer.cfg (CUBE_SAVE_PROBE_OFFSET,
// then SAVE_CONFIG). Klipper restarts.
func (k *Klippy) SaveProbeOffset() error {
	err := k.script("CUBE_SAVE_PROBE_OFFSET", "SAVE_CONFIG")
	// SAVE_CONFIG ends with a restart of Klipper, so its call can fail.
	if err != nil && k.State().Connected {
		return err
	}
	return nil
}
