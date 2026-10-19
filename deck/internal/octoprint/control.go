package octoprint

import (
	"fmt"
	"strings"
)

// Control of the printer, like the menus of its own display. The printer runs Klipper, behind OctoPrint:
// homing and jogging use the OctoPrint API, the rest are G-code commands. Each call waits for OctoPrint,
// so the screen can show an error (for example 409: the printer is busy or not connected).

// send posts a command and returns an error if OctoPrint returns an error status.
func (c *Client) send(path string, body any) error {
	b, code, err := c.req("POST", path, body)
	defer c.Poke()
	switch {
	case err != nil:
		return err
	case code == 409:
		return fmt.Errorf("The printer is busy or not connected")
	case code >= 300:
		return fmt.Errorf("OctoPrint answered %d: %s", code, strings.TrimSpace(string(b)))
	}
	return nil
}

// Gcode sends G-code lines.
func (c *Client) Gcode(lines ...string) error {
	return c.send("/api/printer/command", map[string]any{"commands": lines})
}

// Home homes the axes ("x", "y", "z"); none means all.
func (c *Client) Home(axes ...string) error {
	if len(axes) == 0 {
		axes = []string{"x", "y", "z"}
	}
	return c.send("/api/printer/printhead", map[string]any{"command": "home", "axes": axes})
}

// Jog moves the head relative to where it is, in mm, at speed mm/min.
func (c *Client) Jog(x, y, z float64, speed int) error {
	return c.send("/api/printer/printhead", map[string]any{"command": "jog", "x": x, "y": y, "z": z, "absolute": false, "speed": speed})
}

func (c *Client) SetToolTemp(t int) error {
	return c.send("/api/printer/tool", map[string]any{"command": "target", "targets": map[string]int{"tool0": t}})
}

func (c *Client) SetBedTemp(t int) error {
	return c.send("/api/printer/bed", map[string]any{"command": "target", "target": t})
}

// Extrude pushes filament (or pulls it back when mm is negative) at speed mm/min. The extruder is put back
// into absolute mode afterwards, as the slicer expects.
func (c *Client) Extrude(mm float64, speed int) error {
	return c.Gcode("M83", fmt.Sprintf("G1 E%.2f F%d", mm, speed), "M82")
}

// Fan sets the part cooling fan, in percent.
func (c *Client) Fan(percent int) error {
	if percent <= 0 {
		return c.Gcode("M107")
	}
	return c.Gcode(fmt.Sprintf("M106 S%d", min(255, percent*255/100)))
}

// MotorsOff lets the axes move freely by hand. They must be homed again afterwards.
func (c *Client) MotorsOff() error { return c.Gcode("M84") }

// SpeedFactor and FlowFactor change a running print, in percent (100 is as sliced).
func (c *Client) SpeedFactor(p int) error { return c.Gcode(fmt.Sprintf("M220 S%d", p)) }
func (c *Client) FlowFactor(p int) error  { return c.Gcode(fmt.Sprintf("M221 S%d", p)) }

// BabyStep moves the nozzle up (positive) or down during a print, in mm. Klipper keeps it as a G-code offset.
func (c *Client) BabyStep(mm float64) error {
	return c.Gcode(fmt.Sprintf("SET_GCODE_OFFSET Z_ADJUST=%.3f MOVE=1", mm))
}

// EmergencyStop stops the printer at once. Klipper must be restarted afterwards (FIRMWARE_RESTART).
func (c *Client) EmergencyStop() error { return c.Gcode("M112") }

// FirmwareRestart restarts Klipper after an emergency stop or an error.
func (c *Client) FirmwareRestart() error { return c.Gcode("FIRMWARE_RESTART") }
