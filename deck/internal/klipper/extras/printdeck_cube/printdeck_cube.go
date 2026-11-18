// Package printdeck_cube is the [printdeck_cube] section, a module of Printdeck: Klipper has no
// such module. It measures the backlash of X, Y and Z with a conductive cube on the bed, as G425
// of Marlin does on the TAZ Workhorse.
//
//	[printdeck_cube]
//	pin: ^!PA1     # the cube wire: the nozzle grounds it at a touch
//	left: 261      # X of the left face of the cube
//	right: 271     # X of the right face
//	back: -14      # Y of the back face
//	top: 3         # Z of the top face
//
// The places are nozzle positions in mm. With a probe, the pin first probes the middle of the top,
// and the nozzle touch of the top starts from that height: a cube that moved in Z, for example
// after the bed came off, is found. Without a probe, the touch starts from top.
//
// Each touch starts approach mm out from the face and stops overtravel mm past it (z_overtravel on
// the top). So a face can be up to approach mm nearer than its value, and up to overtravel mm
// farther: a wire with no contact presses the nozzle into the cube no more than that. The top touch
// comes first, so a broken wire stops the measure there. After the top touch the wire is good, so
// the side touches go at least sideSearch mm past the face. The nozzle touches the top, then
// the right, the left and the back faces at side_depth under the top. After each touch the nozzle
// backs away in steps of step mm, and the deck reads the pin after each step. The distance to the
// open contact, less the travel after the trigger, is the backlash of the axis: the motor turns
// back through the play before the nozzle moves.
//
// CUBE_QUERY reports the pin. CUBE_MEASURE measures and reports the backlash. The moves of the
// measure go to the toolhead directly, so [printdeck_backlash] does not correct them.
//
// CUBE_PROBE_OFFSET [SAMPLES=3] measures the z_offset of the probe: the nozzle touches the top of
// the cube, then the probe pin probes the middle of the top. The difference of the two heights is
// the z_offset. The middle of the top comes from the faces, and the size of the cube (width and
// depth, the TAZ Workhorse cube of LulzBot: 10 by 4 mm). The descent of the probe stops 0.5 mm
// over the top: a pin that misses the cube gives an error, and the nozzle stays over the bed.
//
// CUBE_PROBE_OFFSET MOUNT=1 is for a probe on a new mount, with a z_offset that can be far off:
// the nozzle touch starts from top (no probe of the top first), the pin can go down to 1 mm under
// the top (and 0.5 mm over the bed), and the result has no check against z_offset.
//
// CUBE_SAVE_PROBE_OFFSET puts the last z_offset of CUBE_PROBE_OFFSET in the probe section for
// SAVE_CONFIG, as PROBE_CALIBRATE does.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_cube

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// Cube is the [printdeck_cube] section.
type Cube struct {
	printer                *host.Printer
	endstop                *mcu.Endstop
	left, right, back, top float64
	speed, travelSpeed     float64
	approach, overtravel   float64
	zOvertravel            float64
	sideDepth, lift        float64
	step, maxBacklash      float64
	width, depth           float64
	probeSection           string  // "bltouch" or "probe"
	probeZ                 float64 // the last result of CUBE_PROBE_OFFSET, NaN for none
	result                 map[string]any
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	c := &Cube{printer: printer, result: map[string]any{}, probeZ: math.NaN(), probeSection: "probe"}
	if config.HasSection("bltouch") {
		c.probeSection = "bltouch"
	}
	ppins := printer.LookupObject("pins").(*pins.Pins)
	c.endstop = ppins.SetupPin("endstop", config.Get("pin")).(*mcu.Endstop)
	c.left = config.GetFloat("left")
	c.right = config.GetFloat("right", configfile.Above(c.left))
	c.back = config.GetFloat("back")
	c.top = config.GetFloat("top")
	c.speed = config.GetFloat("speed", configfile.Default(1.), configfile.Above(0.), configfile.Max(5.))
	c.travelSpeed = config.GetFloat("travel_speed", configfile.Default(50.), configfile.Above(0.))
	c.approach = config.GetFloat("approach", configfile.Default(4.), configfile.Above(0.))
	c.overtravel = config.GetFloat("overtravel", configfile.Default(0.5), configfile.Above(0.), configfile.Max(2.))
	c.zOvertravel = config.GetFloat("z_overtravel", configfile.Default(0.5), configfile.Above(0.), configfile.Max(1.))
	c.sideDepth = config.GetFloat("side_depth", configfile.Default(1.), configfile.Above(0.))
	c.lift = config.GetFloat("lift", configfile.Default(5.), configfile.Above(0.))
	c.step = config.GetFloat("step", configfile.Default(0.01), configfile.Above(0.), configfile.Max(0.1))
	c.maxBacklash = config.GetFloat("max_backlash", configfile.Default(0.8), configfile.Above(0.))
	c.width = config.GetFloat("width", configfile.Default(10.), configfile.Above(0.))
	c.depth = config.GetFloat("depth", configfile.Default(4.), configfile.Above(0.))
	// The cube stops all the steppers of X, Y and Z: a touch can come in any direction.
	printer.RegisterEventHandler("klippy:mcu_identify", func(...any) {
		for _, s := range toolhead.Of(printer).Kin().Steppers() {
			ms := s.(*stepper.MCUStepper)
			if ms.IsActiveAxis("x") || ms.IsActiveAxis("y") || ms.IsActiveAxis("z") {
				c.endstop.AddStepper(ms)
			}
		}
	})
	g := gcode.Of(printer)
	g.RegisterCommand("CUBE_QUERY", c.cmdQuery, false, "Report the contact of the nozzle and the cube")
	g.RegisterCommand("CUBE_MEASURE", c.cmdMeasure, false, "Measure the backlash of X, Y and Z on the cube")
	g.RegisterCommand("CUBE_PROBE_OFFSET", c.cmdProbeOffset, false, "Measure the z_offset of the probe on the cube")
	g.RegisterCommand("CUBE_SAVE_PROBE_OFFSET", c.cmdSaveProbeOffset, false,
		"Put the z_offset of CUBE_PROBE_OFFSET in the config for SAVE_CONFIG")
	return c
}

func init() { host.RegisterModule("printdeck_cube", load, nil) }

// Status is the last result of CUBE_MEASURE: backlash_x, backlash_y, backlash_z and the faces.
func (c *Cube) Status(eventtime float64) map[string]any {
	out := make(map[string]any, len(c.result))
	for k, v := range c.result {
		out[k] = v
	}
	return out
}

// triggered reads the pin after the queued moves.
func (c *Cube) triggered() bool {
	th := toolhead.Of(c.printer)
	return c.endstop.QueryEndstop(th.LastMoveTime()) != 0
}

func (c *Cube) cmdQuery(gcmd *gcode.Command) {
	state := "open"
	if c.triggered() {
		state = "TRIGGERED"
	}
	gcmd.RespondInfo("cube: "+state, true)
}

// move goes to the place of the coordinates that are not NaN, and waits.
// sideSearch is the least travel of a side touch past its face. The top touch before it shows that
// the wire works, so the nozzle stops at the contact.
const sideSearch = 2.0

// The probe of the top before the nozzle touch: the nozzle stops topFloor mm under top, or
// minFloorZ over the bed, when the pin misses the cube. The probe height must be within topFind mm
// of top.
const (
	topFloor  = 1.0
	minFloorZ = 0.5
	topFind   = 3.0
)

func (c *Cube) move(x, y, z, speed float64) {
	th := toolhead.Of(c.printer)
	th.ManualMove([]float64{x, y, z}, speed)
}

// touch moves the axis toward to, and returns the trigger place and the travel after the
// trigger. It fails when the nozzle gets to the end with no contact.
func (c *Cube) touch(axis int, to float64, face string) (float64, float64) {
	th := toolhead.Of(c.printer)
	pos := th.Position()
	start := pos[axis]
	pos[axis] = to
	phoming := c.printer.LookupObject("homing").(*homing.PrinterHoming)
	var epos []float64
	func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(*gcode.CommandError); ok && strings.Contains(e.Msg, "No trigger") {
					panic(gcode.Errorf("No contact on the %s face of the cube. Check the cube wire, "+
						"clean the nozzle tip, and check the places in [printdeck_cube].", face))
				}
				panic(r)
			}
		}()
		// The check of the probe fails when any stepper of the endstop stays still, and a touch
		// moves one axis only: check the axis of the touch here.
		epos = phoming.ProbingMove(c.endstop, pos[:3], c.speed, false)
	}()
	if math.Abs(epos[axis]-start) < 1e-6 {
		panic(gcode.Errorf("The cube contact closed before the touch of the %s face. Check the cube wire for a short to ground.", face))
	}
	halt := th.Position()[axis]
	return epos[axis], math.Abs(halt - epos[axis])
}

// release backs the axis away in steps, in the direction dir, until the contact opens. It returns
// the distance of the steps.
func (c *Cube) release(axis int, dir float64, face string) float64 {
	th := toolhead.Of(c.printer)
	start := th.Position()[axis]
	n := int(math.Ceil(c.maxBacklash / c.step))
	for i := 1; i <= n; i++ {
		coord := []float64{math.NaN(), math.NaN(), math.NaN()}
		coord[axis] = start + dir*float64(i)*c.step
		th.ManualMove(coord, c.speed)
		if !c.triggered() {
			return float64(i) * c.step
		}
	}
	panic(gcode.Errorf("The contact on the %s face of the cube stays closed after %.2f mm. "+
		"Check the cube wire for a short to ground.", face, c.maxBacklash))
}

// side measures one face: touch it along the axis in the direction dir, then back away. It
// returns the trigger place and the backlash.
func (c *Cube) side(axis int, face float64, dir float64, name string) (float64, float64) {
	at, over := c.touch(axis, face+dir*math.Max(c.overtravel, sideSearch), name)
	r := c.release(axis, -dir, name)
	return at, math.Max(0, r-over)
}

// start checks the homing and the contact before a command.
func (c *Cube) start(name string) {
	th := toolhead.Of(c.printer)
	homed, _ := th.Kin().Status(c.printer.Reactor().Monotonic())["homed_axes"].(string)
	if !strings.Contains(homed, "x") || !strings.Contains(homed, "y") || !strings.Contains(homed, "z") {
		panic(gcode.Errorf("Home X, Y and Z before %s", name))
	}
	if c.triggered() {
		panic(gcode.Errorf("The cube contact is closed before the measure. Check the cube wire for a short to ground."))
	}
}

// touchTop touches the top of the cube with the nozzle at (midX, y), from the height top, and
// returns the trigger place and the backlash of Z.
func (c *Cube) touchTop(midX, y, top float64) (float64, float64) {
	nan := math.NaN()
	th := toolhead.Of(c.printer)
	c.move(nan, nan, math.Max(th.Position()[2], top+c.lift), c.travelSpeed/5)
	c.move(midX, y, nan, c.travelSpeed)
	c.move(nan, nan, top+c.approach, c.travelSpeed/5)
	topZ, overZ := c.touch(2, top-c.zOvertravel, "top")
	return topZ, math.Max(0, c.release(2, 1, "top")-overZ)
}

// pinPlace is the place of the probe pin over the middle of the top. The faces are nozzle places,
// so the cube edge is a nozzle radius past the face.
func (c *Cube) pinPlace() (x, y float64) {
	r := math.Max(0, (c.right-c.left-c.width)/2)
	return (c.left + c.right) / 2, c.back - r - c.depth/2
}

// probeTop probes the middle of the top with the pin, samples times, from the top height top. The
// descent stops when the nozzle gets to floor. It returns the heights of the top (nozzle Z).
func (c *Cube) probeTop(gcmd *gcode.Command, pr probe.Probe, top, floor float64, samples int) []float64 {
	off := pr.Offsets(nil)
	nan := math.NaN()
	th := toolhead.Of(c.printer)
	pinX, pinY := c.pinPlace()
	start := top + off[2] + topFind
	c.move(nan, nan, math.Max(th.Position()[2], top+off[2]+c.lift), c.travelSpeed/5)
	c.move(pinX-off[0], pinY-off[1], nan, c.travelSpeed)
	c.move(nan, nan, start, c.travelSpeed/5)
	var zs []float64
	session := pr.StartProbeSession(gcmd)
	func() {
		defer session.EndProbeSession()
		probe.WithFloor(c.printer, floor, func() {
			for i := 0; i < samples; i++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							if e, ok := r.(*gcode.CommandError); ok && strings.Contains(e.Msg, "No trigger") {
								panic(gcode.Errorf("The probe did not trigger over the cube: the pin missed the top. " +
									"Check x_offset and y_offset of the probe, and the places in [printdeck_cube]."))
							}
							panic(r)
						}
					}()
					session.RunProbe(gcmd)
				}()
				for _, res := range session.PullProbedResults() {
					zs = append(zs, res.BedZ())
				}
				c.move(nan, nan, start, c.travelSpeed/5)
			}
		})
	}()
	c.move(nan, nan, top+off[2]+c.lift, c.travelSpeed/5)
	return zs
}

// findTop returns the height of the top for the nozzle touch: one probe of the top with the pin,
// or top with no probe.
func (c *Cube) findTop(gcmd *gcode.Command) float64 {
	pr, ok := c.printer.TryLookupObject("probe").(probe.Probe)
	if !ok {
		return c.top
	}
	zs := c.probeTop(gcmd, pr, c.top, math.Max(c.top-topFloor, minFloorZ), 1)
	z := zs[0]
	if math.Abs(z-c.top) > topFind {
		panic(gcode.Errorf("The probe found the top of the cube at Z %.2f, and [printdeck_cube] has top %.2f. "+
			"Check the cube, and z_offset of the probe.", z, c.top))
	}
	gcmd.RespondInfo(fmt.Sprintf("The probe found the top of the cube at Z %.2f (top in printer.cfg: %.2f)", z, c.top), true)
	return z
}

func (c *Cube) cmdMeasure(gcmd *gcode.Command) {
	c.start("CUBE_MEASURE")
	th := toolhead.Of(c.printer)
	nan := math.NaN()
	midX := (c.left + c.right) / 2
	y := c.back - 2 // 2 mm in from the back edge: the front of the cube is out of the travel

	// The top: down onto the middle of the top face.
	topZ, bz := c.touchTop(midX, y, c.findTop(gcmd))
	sideZ := topZ - c.sideDepth
	safe := topZ + c.lift

	// The right face, from the right: the nozzle moves to -X.
	c.move(nan, nan, safe, c.travelSpeed/5)
	c.move(c.right+c.approach, y, nan, c.travelSpeed)
	c.move(nan, nan, sideZ, c.travelSpeed/5)
	rightX, bxRight := c.side(0, c.right, -1, "right")

	// The left face, from the left: the nozzle moves to +X.
	c.move(nan, nan, safe, c.travelSpeed/5)
	c.move(c.left-c.approach, y, nan, c.travelSpeed)
	c.move(nan, nan, sideZ, c.travelSpeed/5)
	leftX, bxLeft := c.side(0, c.left, 1, "left")

	// The back face, from behind: the nozzle moves to -Y.
	c.move(nan, nan, safe, c.travelSpeed/5)
	c.move(midX, c.back+c.approach, nan, c.travelSpeed)
	c.move(nan, nan, sideZ, c.travelSpeed/5)
	backY, by := c.side(1, c.back, -1, "back")

	c.move(nan, nan, safe, c.travelSpeed/5)
	th.WaitMoves()

	bx := (bxRight + bxLeft) / 2
	c.result = map[string]any{
		"backlash_x": round3(bx), "backlash_y": round3(by), "backlash_z": round3(bz),
		"top": round3(topZ), "left": round3(leftX), "right": round3(rightX), "back": round3(backY),
	}
	gcmd.RespondInfo(fmt.Sprintf("Cube: top Z %.3f, left X %.3f, right X %.3f, back Y %.3f (nozzle positions)\n"+
		"Backlash: X %.3f mm (right %.3f, left %.3f), Y %.3f mm, Z %.3f mm\n"+
		"SET_BACKLASH X=%.2f Y=%.2f sets the correction. Put the values in [printdeck_backlash] to keep them.",
		topZ, leftX, rightX, backY, bx, bxRight, bxLeft, by, bz, bx, by), true)
}

func (c *Cube) cmdProbeOffset(gcmd *gcode.Command) {
	samples := gcmd.GetInt("SAMPLES", gcode.Default(3), gcode.Min(1), gcode.Max(10))
	pr, ok := c.printer.TryLookupObject("probe").(probe.Probe)
	if !ok {
		panic(gcode.Errorf("printer.cfg has no probe"))
	}
	// MOUNT=1: the probe is on a new mount, and its height is not known. The pin does not find
	// the top first, because that uses z_offset: the nozzle touch starts from top of
	// [printdeck_cube]. The descent of the pin can go down to topFloor under the top, and minFloorZ
	// over the bed: a pin that misses the cube then reaches the floor before the bed.
	mount := gcmd.GetInt("MOUNT", gcode.Default(0), gcode.Min(0), gcode.Max(1)) == 1
	c.start("CUBE_PROBE_OFFSET")
	off := pr.Offsets(nil)
	midX := (c.left + c.right) / 2
	var topZ, floor float64
	if mount {
		topZ, _ = c.touchTop(midX, c.back-2, c.top)
		floor = math.Max(topZ-topFloor, minFloorZ)
	} else {
		topZ, _ = c.touchTop(midX, c.back-2, c.findTop(gcmd))
		floor = topZ + 0.5
	}
	var zs []float64
	for _, z := range c.probeTop(gcmd, pr, topZ, floor, samples) {
		zs = append(zs, z+off[2])
	}
	th := toolhead.Of(c.printer)
	th.WaitMoves()

	sorted := append([]float64(nil), zs...)
	sort.Float64s(sorted)
	pz := sorted[len(sorted)/2]
	zoff := pz - topZ
	if mount {
		if zoff <= 0.1 {
			panic(gcode.Errorf("The probe gave z_offset %.3f: the pin is not under the nozzle, or it missed the top "+
				"of the cube. Check x_offset and y_offset of the probe.", zoff))
		}
		if zoff < 2 {
			gcmd.RespondInfo(fmt.Sprintf("The pin is only %.2f mm under the nozzle. A BLTouch needs 2 to 4 mm: "+
				"a lower mount keeps the nozzle away from the bed when the pin probes.", zoff), true)
		}
	} else if math.Abs(zoff-off[2]) > 0.8 {
		panic(gcode.Errorf("The probe gave z_offset %.3f, and printer.cfg has %.3f. The pin can be off the top "+
			"of the cube: check x_offset and y_offset of the probe.", zoff, off[2]))
	}
	c.result["probe_z_offset"] = round3(zoff)
	c.probeZ = zoff
	gcmd.RespondInfo(fmt.Sprintf("Probe z_offset: %.3f mm (printer.cfg has %.3f)\n"+
		"The nozzle touched the top at Z %.3f. The probe triggered at Z %.3f, range %.3f in %d samples.",
		zoff, off[2], topZ, pz, sorted[len(sorted)-1]-sorted[0], len(zs)), true)
}

func (c *Cube) cmdSaveProbeOffset(gcmd *gcode.Command) {
	if math.IsNaN(c.probeZ) {
		panic(gcode.Errorf("Run CUBE_PROBE_OFFSET first"))
	}
	pconfig := c.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	pconfig.Set(c.probeSection, "z_offset", fmt.Sprintf("%.3f", c.probeZ))
	gcmd.RespondInfo(fmt.Sprintf("%s: z_offset: %.3f\nThe SAVE_CONFIG command will update the printer config file\n"+
		"with the above and restart the printer.", c.probeSection, c.probeZ), true)
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
