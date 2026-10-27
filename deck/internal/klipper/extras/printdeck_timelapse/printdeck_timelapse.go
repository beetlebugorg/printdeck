// Package printdeck_timelapse parks the toolhead at the layer changes of a print, for a picture of
// the camera of the deck: a frame of the time lapse, or the picture of the first layer. It is a
// Printdeck module, with no Klipper source. The config section is [printdeck_timelapse]
// (printdeck.cfg has it).
//
// The deck turns the module on with
//
//	PRINTDECK_TIMELAPSE [ENABLE=0|1] [PARK=0|1] [FIRST_LAYER=0|1|2] [EVERY=n]
//
// ENABLE=1 adds a frame at each n-th layer change (EVERY). PARK=0 captures the frames of the time
// lapse with no park. FIRST_LAYER=1 adds a picture after the first layer, always with a park.
// FIRST_LAYER=2 also pauses the print after the picture (PRINTDECK_PAUSE), for a check.
//
// The module finds the layer changes from the comments of the slicer in the file of the print
// (";LAYER:n" of Cura, ";LAYER_CHANGE" of PrusaSlicer and OrcaSlicer). The frame goes in order with
// the moves of the file. At a frame, the module:
//
//  1. retracts the filament,
//  2. lifts the nozzle lift mm, and at least to min_z,
//  3. moves to the park position (park_x, park_y), and waits for the end of the moves (M400),
//  4. sets "waiting" in its status, and waits until the deck calls the endpoint
//     printdeck/timelapse/taken with the number of the frame, for at most wait seconds,
//  5. moves back to the same position, lowers the nozzle, and pushes the filament back.
//
// A frame with no park only sets "waiting" and waits for the picture. After 3 frames in a row with
// no picture, the module stops the frames of this print, so that a print with no deck does not
// wait at each layer.
//
//	PRINTDECK_TIMELAPSE_FRAME [PARK=0|1]
//
// captures a frame now, for the layer change G-code of a slicer with no layer comments.
//
// The options and their defaults:
//
//	[printdeck_timelapse]
//	#park_x:            # mm; the default is 5 mm inside the minimum of X
//	#park_y:            # mm; the default is the Y of the print, so a bed that moves in Y
//	                    # stays under the camera
//	lift: 1             # mm over the layer
//	min_z: 10           # mm; the lowest Z of the moves to the park position, over the
//	                    # clips, washers and probe points at the edges of the bed
//	retract: 1          # mm
//	retract_speed: 15   # mm/s, also of the push back
//	travel_speed: 100   # mm/s
//	z_speed: 10         # mm/s
//	wait: 5             # s; the longest wait for a picture
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_timelapse

import (
	"fmt"
	"log"
	"math"
	"strings"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/klipper/webhooks"
)

// maxMisses is the count of frames in a row with no picture that stops the frames of a print.
const maxMisses = 3

// The values of FIRST_LAYER.
const (
	FirstOff     = 0
	FirstPicture = 1
	FirstPause   = 2
)

// Timelapse is the [printdeck_timelapse] section.
type Timelapse struct {
	printer *host.Printer
	gcode   *gcode.Dispatch
	reactor *reactor.Reactor

	parkX, parkY              float64 // NaN is the default of the axis
	lift, minZ                float64
	retract                   float64
	retractSpeed, travelSpeed float64
	zSpeed, wait              float64

	// sdLine reports that the line of the comment comes from the file of a print, and returns the
	// file and the position in it. Tests replace it.
	sdLine func() (file string, pos int64, ok bool)

	mu        sync.Mutex
	enable    bool
	park      bool
	first     int
	every     int
	file      string // the file of the print of the layer count
	lastPos   int64
	changes   int  // the layer changes of the print so far
	misses    int  // the frames in a row with no picture
	stopped   bool // too many misses: no frames for the rest of this print
	frame     int  // the number of the last frame
	taken     int  // the number of the last frame with a picture
	waiting   bool
	kindFirst bool // the frame is the picture of the first layer
	kindLapse bool // the frame is a frame of the time lapse
	layer     int  // the layers that are done at the frame
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	t := &Timelapse{printer: printer, gcode: gcode.Of(printer), reactor: printer.Reactor(), park: true, every: 1}
	t.parkX = config.GetFloat("park_x", configfile.Default(math.NaN()))
	t.parkY = config.GetFloat("park_y", configfile.Default(math.NaN()))
	t.lift = config.GetFloat("lift", configfile.Default(1.), configfile.Min(0.))
	t.minZ = config.GetFloat("min_z", configfile.Default(10.), configfile.Min(0.))
	t.retract = config.GetFloat("retract", configfile.Default(1.), configfile.Min(0.))
	t.retractSpeed = config.GetFloat("retract_speed", configfile.Default(15.), configfile.Above(0.))
	t.travelSpeed = config.GetFloat("travel_speed", configfile.Default(100.), configfile.Above(0.))
	t.zSpeed = config.GetFloat("z_speed", configfile.Default(10.), configfile.Above(0.))
	t.wait = config.GetFloat("wait", configfile.Default(5.), configfile.Above(0.))
	t.sdLine = func() (string, int64, bool) {
		v, _ := printer.TryLookupObject("virtual_sdcard").(sdcard)
		if v == nil || !v.IsCmdFromSD() {
			return "", 0, false
		}
		f, _ := v.FilePath().(string)
		return f, v.FilePosition(), true
	}
	t.gcode.RegisterCommand("PRINTDECK_TIMELAPSE", t.cmdTimelapse, false, "Set the time lapse and the first layer picture")
	t.gcode.RegisterCommand("PRINTDECK_TIMELAPSE_FRAME", t.cmdFrame, false, "Park, and wait for a picture of the deck")
	t.gcode.RegisterCommentHandler(t.onComment)
	webhooks.Of(printer).RegisterEndpoint("printdeck/timelapse/taken", t.handleTaken)
	return t
}

func init() { host.RegisterModule("printdeck_timelapse", load, nil) }

// sdcard is the virtual_sdcard: the line that runs, and the file of the print.
type sdcard interface {
	IsCmdFromSD() bool
	FilePath() any
	FilePosition() int64
}

// Status is the state of the time lapse for the deck.
func (t *Timelapse) Status(eventtime float64) map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{"enable": t.enable, "park": t.park, "first_layer": t.first, "every": t.every,
		"frame": t.frame, "taken": t.taken, "waiting": t.waiting, "first": t.kindFirst, "lapse": t.kindLapse,
		"layer": t.layer, "stopped": t.stopped}
}

func (t *Timelapse) cmdTimelapse(gcmd *gcode.Command) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enable = gcmd.GetInt("ENABLE", gcode.Default(boolInt(t.enable)), gcode.Min(0), gcode.Max(1)) == 1
	t.park = gcmd.GetInt("PARK", gcode.Default(boolInt(t.park)), gcode.Min(0), gcode.Max(1)) == 1
	t.first = gcmd.GetInt("FIRST_LAYER", gcode.Default(t.first), gcode.Min(0), gcode.Max(2))
	t.every = gcmd.GetInt("EVERY", gcode.Default(t.every), gcode.Min(1))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (t *Timelapse) cmdFrame(gcmd *gcode.Command) {
	t.mu.Lock()
	park := gcmd.GetInt("PARK", gcode.Default(boolInt(t.park)), gcode.Min(0), gcode.Max(1)) == 1
	layer := max(0, t.changes-1)
	t.mu.Unlock()
	t.takeFrame(park, false, true, layer)
}

func (t *Timelapse) handleTaken(req *webhooks.WebRequest) {
	var n int
	switch v := req.GetInt("frame").(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	}
	t.mu.Lock()
	if n > t.taken {
		t.taken = n
	}
	t.mu.Unlock()
	req.Send(map[string]any{})
}

// isLayerChange reports that the text of a comment is a layer change of a slicer.
func isLayerChange(comment string) bool {
	c := strings.TrimSpace(comment)
	return strings.HasPrefix(c, "LAYER:") || c == "LAYER_CHANGE"
}

// plan returns what a layer change does: changes is the count of layer changes of the print with
// this one. The first change is the start of the first layer, so change n ends layer n-1.
func plan(changes int, enable, stopped bool, first, every int) (pic, lapse bool) {
	if stopped || changes < 2 {
		return false, false
	}
	done := changes - 1
	pic = first != FirstOff && done == 1
	lapse = enable && done%max(1, every) == 0
	return pic, lapse
}

func (t *Timelapse) onComment(comment string) {
	if !isLayerChange(comment) {
		return
	}
	file, pos, ok := t.sdLine()
	if !ok {
		return
	}
	t.mu.Lock()
	if file != t.file || pos < t.lastPos {
		// A new print, or the same file again.
		t.file, t.changes, t.misses, t.stopped = file, 0, 0, false
	}
	t.lastPos = pos
	t.changes++
	pic, lapse := plan(t.changes, t.enable, t.stopped, t.first, t.every)
	park, pause, done := t.park || pic, pic && t.first == FirstPause, t.changes-1
	t.mu.Unlock()
	if !pic && !lapse {
		return
	}
	t.takeFrame(park, pic, lapse, done)
	if pause {
		t.pauseForCheck()
	}
}

// pauseForCheck pauses the print after the picture of the first layer, with a message.
func (t *Timelapse) pauseForCheck() {
	if t.printer.TryLookupObject("printdeck_pause") != nil {
		t.gcode.RunScriptFromCommand(`PRINTDECK_PAUSE MESSAGE="Check the first layer"`)
		return
	}
	t.gcode.RunScriptFromCommand("PAUSE")
}

// takeFrame parks the toolhead when park is true, and waits for the picture of the deck.
func (t *Timelapse) takeFrame(park, pic, lapse bool, layer int) {
	var back string
	if park {
		var out string
		out, back = t.parkScript()
		t.gcode.RunScriptFromCommand(out)
	} else {
		t.gcode.RunScriptFromCommand("M400")
	}

	t.mu.Lock()
	t.frame++
	n := t.frame
	t.waiting, t.kindFirst, t.kindLapse, t.layer = true, pic, lapse, layer
	t.mu.Unlock()

	deadline := t.reactor.Monotonic() + t.wait
	for {
		t.mu.Lock()
		got := t.taken >= n
		t.mu.Unlock()
		now := t.reactor.Monotonic()
		if got || now >= deadline {
			break
		}
		t.reactor.Pause(math.Min(now+0.05, deadline))
	}

	t.mu.Lock()
	t.waiting = false
	if t.taken >= n {
		t.misses = 0
	} else {
		t.misses++
		log.Printf("printdeck_timelapse: no picture for frame %d in %.1f s", n, t.wait)
		if t.misses >= maxMisses && !t.stopped {
			t.stopped = true
			log.Printf("printdeck_timelapse: %d frames with no picture; no more frames in this print", t.misses)
		}
	}
	t.mu.Unlock()
	if back != "" {
		t.gcode.RunScriptFromCommand(back)
	}
}

// parkScript returns the G-code to the park position, and the G-code back to the position now.
// The moves are relative, so the way back ends at the exact position of the print.
func (t *Timelapse) parkScript() (out, back string) {
	th := toolhead.Of(t.printer)
	pos := gcode_move.Of(t.printer).Status(0)["position"].(gcode.Coord)
	lo, hi := axisLimits(th.Status(0))
	px, py, dz := parkMove([3]float64{pos.X(), pos.Y(), pos.Z()}, lo, hi, t.parkX, t.parkY, t.lift, t.minZ)
	retract := 0.
	if t.canExtrude() {
		retract = t.retract
	}
	return moveScripts(px-pos.X(), py-pos.Y(), dz, retract, t.retractSpeed, t.travelSpeed, t.zSpeed)
}

// parkMove returns the park position in X and Y, and the lift in Z, for the toolhead at pos and
// the limits lo and hi of the axes. NaN in x or y is the default: 5 mm inside the minimum of X,
// and the Y of pos. The park position stays inside the limits.
func parkMove(pos, lo, hi [3]float64, x, y, lift, minZ float64) (px, py, dz float64) {
	if math.IsNaN(x) {
		x = lo[0] + 5
	}
	if math.IsNaN(y) {
		y = pos[1]
	}
	px = math.Max(lo[0], math.Min(hi[0], x))
	py = math.Max(lo[1], math.Min(hi[1], y))
	z := math.Max(pos[2]+lift, minZ)
	z = math.Min(z, hi[2])
	dz = math.Max(0, z-pos[2])
	return px, py, dz
}

// moveScripts returns the G-code of the park and of the way back: dx and dy to the park position,
// dz up, and the retract in mm. The speeds are in mm/s.
func moveScripts(dx, dy, dz, retract, retractSpeed, travelSpeed, zSpeed float64) (out, back string) {
	var o, b []string
	o = append(o, "SAVE_GCODE_STATE NAME=printdeck_timelapse", "M83")
	if retract > 0 {
		o = append(o, fmt.Sprintf("G1 E-%.3f F%.0f", retract, retractSpeed*60))
	}
	o = append(o, "G91")
	if dz > 0 {
		o = append(o, fmt.Sprintf("G1 Z%.3f F%.0f", dz, zSpeed*60))
	}
	o = append(o, fmt.Sprintf("G1 X%.3f Y%.3f F%.0f", dx, dy, travelSpeed*60), "M400")

	b = append(b, fmt.Sprintf("G1 X%.3f Y%.3f F%.0f", -dx, -dy, travelSpeed*60))
	if dz > 0 {
		b = append(b, fmt.Sprintf("G1 Z%.3f F%.0f", -dz, zSpeed*60))
	}
	if retract > 0 {
		b = append(b, fmt.Sprintf("G1 E%.3f F%.0f", retract, retractSpeed*60))
	}
	b = append(b, "RESTORE_GCODE_STATE NAME=printdeck_timelapse")
	return strings.Join(o, "\n"), strings.Join(b, "\n")
}

// canExtrude reports that the extruder is hot enough to retract.
func (t *Timelapse) canExtrude() bool {
	e, ok := t.printer.TryLookupObject("extruder").(interface {
		Status(float64) map[string]any
	})
	if !ok {
		return false
	}
	c, _ := e.Status(t.reactor.Monotonic())["can_extrude"].(bool)
	return c
}

// axisLimits are the minimum and the maximum of the axes, from the status of the toolhead.
func axisLimits(st map[string]any) (lo, hi [3]float64) {
	lo, hi = [3]float64{0, 0, 0}, [3]float64{300, 300, 300}
	if c, ok := st["axis_minimum"].(gcode.Coord); ok {
		lo = [3]float64{c.X(), c.Y(), c.Z()}
	}
	if c, ok := st["axis_maximum"].(gcode.Coord); ok {
		hi = [3]float64{c.X(), c.Y(), c.Z()}
	}
	return lo, hi
}
