package printer

import (
	"fmt"
	"log"
	"strings"

	"printdeck/internal/bambu"
)

// The controls of a Bambu Lab printer from the deck: G-code lines (gcode_line, which needs
// Developer Mode on the printer), the speed level, the lights and the fans. Each control runs on a
// press on the deck. The printer checks its own limits, for example a cold extrusion.

// gcode runs the lines, logs them, and shows an error in the panel.
func (b *Bambu) gcode(lines ...string) error {
	err := b.Client.Gcode(lines...)
	log.Printf("bambu: G-code %q: %v", strings.Join(lines, "; "), err)
	if err != nil {
		b.mu.Lock()
		b.st.Message = "The command did not go to the printer: " + err.Error()
		b.mu.Unlock()
	}
	return err
}

// Gcode runs G-code lines on the printer.
func (b *Bambu) Gcode(lines ...string) error { return b.gcode(lines...) }

// SetToolTemp sets the temperature of the nozzle that is in use.
func (b *Bambu) SetToolTemp(t int) error { return b.gcode(fmt.Sprintf("M104 S%d", max(0, t))) }

// SetBedTemp sets the temperature of the bed.
func (b *Bambu) SetBedTemp(t int) error { return b.gcode(fmt.Sprintf("M140 S%d", max(0, t))) }

// SetChamberTemp sets the temperature of the chamber heat (M141 of an H2D or an X2D).
func (b *Bambu) SetChamberTemp(t int) error { return b.gcode(fmt.Sprintf("M141 S%d", max(0, t))) }

// Heat sets the nozzle and the bed; 0 turns a heater off.
func (b *Bambu) Heat(tool, bed int) {
	go b.gcode(fmt.Sprintf("M104 S%d", max(0, tool)), fmt.Sprintf("M140 S%d", max(0, bed)))
}

// The fans of M106: the part fan, the aux fan and the chamber fan.
const (
	FanPart    = 1
	FanAux     = 2
	FanChamber = 3
)

// Fan sets the part fan in percent.
func (b *Bambu) Fan(percent int) error { return b.SetFan(FanPart, percent) }

// SetFan sets the fan (FanPart, FanAux or FanChamber) in percent.
func (b *Bambu) SetFan(fan, percent int) error {
	percent = max(0, min(100, percent))
	return b.gcode(fmt.Sprintf("M106 P%d S%d", fan, percent*255/100))
}

// Extrude pushes mm of filament through the nozzle (negative pulls it back) at speed mm/min.
func (b *Bambu) Extrude(mm float64, speed int) error {
	return b.gcode("M83", fmt.Sprintf("G1 E%.2f F%d", mm, max(60, speed)))
}

// Home homes the axes ("x", "y", "z"), or all axes with none. The deck counts the axes as homed
// after the command: the printer reports no homed axes that the deck reads.
func (b *Bambu) Home(axes ...string) error {
	line := "G28"
	for _, a := range axes {
		line += " " + strings.ToUpper(a)
	}
	if err := b.gcode(line); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(axes) == 0 {
		b.homed = "xyz"
	}
	for _, a := range axes {
		if a = strings.ToLower(a); !strings.Contains(b.homed, a) {
			b.homed += a
		}
	}
	b.st.Homed = b.homed
	return nil
}

// Jog moves the head by x, y and z mm at speed mm/min.
func (b *Bambu) Jog(x, y, z float64, speed int) error {
	return b.gcode("G91", fmt.Sprintf("G1 X%.2f Y%.2f Z%.2f F%d", x, y, z, max(60, speed)), "G90")
}

// MotorsOff turns the motors off. The axes need a home again.
func (b *Bambu) MotorsOff() error {
	if err := b.gcode("M18"); err != nil {
		return err
	}
	b.mu.Lock()
	b.homed, b.st.Homed = "", ""
	b.mu.Unlock()
	return nil
}

// bambuVolumes is the build volume of each model in mm, by the product name of get_version.
var bambuVolumes = map[string][3]float64{
	"Bambu Lab X2D":       {256, 256, 260},
	"Bambu Lab X1":        {256, 256, 256},
	"Bambu Lab X1 Carbon": {256, 256, 256},
	"Bambu Lab X1E":       {256, 256, 256},
	"Bambu Lab P1S":       {256, 256, 256},
	"Bambu Lab P1P":       {256, 256, 256},
	"Bambu Lab P2S":       {256, 256, 256},
	"Bambu Lab A1":        {256, 256, 256},
	"Bambu Lab A1 mini":   {180, 180, 180},
	"Bambu Lab H2D":       {350, 320, 325},
	"Bambu Lab H2S":       {340, 320, 340},
}

// volume returns the build volume of the printer, or false for a model that the deck does not
// know.
func (b *Bambu) volume() ([3]float64, bool) {
	model, _ := b.Client.Version()
	v, ok := bambuVolumes[model]
	return v, ok
}

// SendConsole runs a line of the Console page as G-code, and keeps it and its result.
func (b *Bambu) SendConsole(line string) {
	err := b.gcode(line)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.console = append(b.console, ConsoleLine{Kind: "in", Text: line})
	if err != nil {
		b.console = append(b.console, ConsoleLine{Kind: "error", Text: err.Error()})
	}
	if n := len(b.console); n > 200 {
		b.console = b.console[n-200:]
	}
}

// Console returns the lines that the Console page sent. The printer sends no answer to G-code.
func (b *Bambu) Console() []ConsoleLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ConsoleLine(nil), b.console...)
}

// EmergencyStop stops the print now. The printer keeps its power: its own stop ends the print
// and parks the head.
func (b *Bambu) EmergencyStop() error { return b.Client.Stop() }

// The speed levels of the printer, with the speed in percent that the printer reports for each.
var bambuSpeeds = []struct {
	level int
	name  string
	pct   int
}{{bambu.SpeedSilent, "Silent", 50}, {bambu.SpeedStandard, "Standard", 100}, {bambu.SpeedSport, "Sport", 124},
	{bambu.SpeedLudicrous, "Ludicrous", 166}}

// SpeedFactor sets the speed level nearest to the percent p.
func (b *Bambu) SpeedFactor(p int) error {
	best := bambuSpeeds[0]
	for _, s := range bambuSpeeds {
		if abs(s.pct-p) < abs(best.pct-p) {
			best = s
		}
	}
	return b.SetSpeedLevel(best.level)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// SetSpeedLevel sets the speed level (bambu.SpeedSilent to bambu.SpeedLudicrous).
func (b *Bambu) SetSpeedLevel(level int) error {
	err := b.Client.SetSpeed(level)
	log.Printf("bambu: speed level %d: %v", level, err)
	return err
}

// SpeedLevels returns the names of the speed levels, from 1, and the level now (0 when it is
// not known).
func (b *Bambu) SpeedLevels() (names []string, now int) {
	for _, s := range bambuSpeeds {
		names = append(names, s.name)
	}
	r, _ := b.Report()
	return names, r.SpeedLevel.Int()
}

// Lights returns the lights of the printer and whether each is on.
func (b *Bambu) Lights() (chamber, work, hasChamber, hasWork bool) {
	r, _ := b.Report()
	return r.Light(bambu.ChamberLight), r.Light(bambu.WorkLight), r.HasLight(bambu.ChamberLight), r.HasLight(bambu.WorkLight)
}

// SetLight turns the chamber light (or the work light with work) on or off.
func (b *Bambu) SetLight(work, on bool) error {
	node := bambu.ChamberLight
	if work {
		node = bambu.WorkLight
	}
	err := b.Client.SetLight(node, on)
	log.Printf("bambu: light %s %v: %v", node, on, err)
	return err
}

// Fans returns the part, aux and chamber fans in percent.
func (b *Bambu) Fans() (part, aux, chamber float64) {
	r, _ := b.Report()
	return r.FanPercent(), float64(r.AuxFan) / 15 * 100, float64(r.ChamberFan) / 15 * 100
}
