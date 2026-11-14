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
// The places are nozzle positions in mm. Each touch starts approach mm out from the face and stops
// overtravel mm past it (z_overtravel on the top). So a face can be up to approach mm nearer than
// its value, and up to overtravel mm farther: a wire with no contact presses the nozzle into the
// cube no more than that. The top touch comes first, so a broken wire stops the measure there. The nozzle touches the top, then
// the right, the left and the back faces at side_depth under the top. After each touch the nozzle
// backs away in steps of step mm, and the deck reads the pin after each step. The distance to the
// open contact, less the travel after the trigger, is the backlash of the axis: the motor turns
// back through the play before the nozzle moves.
//
// CUBE_QUERY reports the pin. CUBE_MEASURE measures and reports the backlash. The module does not
// correct the backlash.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_cube

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
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
	result                 map[string]any
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	c := &Cube{printer: printer, result: map[string]any{}}
	ppins := printer.LookupObject("pins").(*pins.Pins)
	c.endstop = ppins.SetupPin("endstop", config.Get("pin")).(*mcu.Endstop)
	c.left = config.GetFloat("left")
	c.right = config.GetFloat("right", configfile.Above(c.left))
	c.back = config.GetFloat("back")
	c.top = config.GetFloat("top")
	c.speed = config.GetFloat("speed", configfile.Default(1.), configfile.Above(0.), configfile.Max(5.))
	c.travelSpeed = config.GetFloat("travel_speed", configfile.Default(50.), configfile.Above(0.))
	c.approach = config.GetFloat("approach", configfile.Default(4.), configfile.Above(0.))
	c.overtravel = config.GetFloat("overtravel", configfile.Default(1.), configfile.Above(0.), configfile.Max(2.))
	c.zOvertravel = config.GetFloat("z_overtravel", configfile.Default(0.5), configfile.Above(0.), configfile.Max(1.))
	c.sideDepth = config.GetFloat("side_depth", configfile.Default(1.), configfile.Above(0.))
	c.lift = config.GetFloat("lift", configfile.Default(5.), configfile.Above(0.))
	c.step = config.GetFloat("step", configfile.Default(0.01), configfile.Above(0.), configfile.Max(0.1))
	c.maxBacklash = config.GetFloat("max_backlash", configfile.Default(0.8), configfile.Above(0.))
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
	at, over := c.touch(axis, face+dir*c.overtravel, name)
	r := c.release(axis, -dir, name)
	return at, math.Max(0, r-over)
}

func (c *Cube) cmdMeasure(gcmd *gcode.Command) {
	th := toolhead.Of(c.printer)
	homed, _ := th.Kin().Status(c.printer.Reactor().Monotonic())["homed_axes"].(string)
	if !strings.Contains(homed, "x") || !strings.Contains(homed, "y") || !strings.Contains(homed, "z") {
		panic(gcode.Errorf("Home X, Y and Z before CUBE_MEASURE"))
	}
	if c.triggered() {
		panic(gcode.Errorf("The cube contact is closed before the measure. Check the cube wire for a short to ground."))
	}
	nan := math.NaN()
	midX := (c.left + c.right) / 2
	y := c.back - 2 // 2 mm in from the back edge: the front of the cube is out of the travel
	safe := c.top + c.lift

	// The top: down onto the middle of the top face.
	c.move(nan, nan, safe, c.travelSpeed/5)
	c.move(midX, y, nan, c.travelSpeed)
	c.move(nan, nan, c.top+c.approach, c.travelSpeed/5)
	topZ, overZ := c.touch(2, c.top-c.zOvertravel, "top")
	bz := math.Max(0, c.release(2, 1, "top")-overZ)
	sideZ := topZ - c.sideDepth
	safe = topZ + c.lift

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
		"Backlash: X %.3f mm (right %.3f, left %.3f), Y %.3f mm, Z %.3f mm",
		topZ, leftX, rightX, backY, bx, bxRight, bxLeft, by, bz), true)
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
