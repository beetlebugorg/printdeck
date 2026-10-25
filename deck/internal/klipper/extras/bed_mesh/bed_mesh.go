// Package bed_mesh is the mesh bed leveling: it probes a grid of points, and adds the height of
// the bed under the nozzle to each move.
//
// Ported to Go from klippy/extras/bed_mesh.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019 Eric Callahan <arksine.code@gmail.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bed_mesh

import (
	"fmt"
	"log"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/gcode_move"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/mathutil"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/klipper/webhooks"
)

const ProfileVersion = 1

// ProfileOptions are the options of a stored profile, in their order, and their types.
var ProfileOptions = []struct{ name, typ string }{
	{"min_x", "float"}, {"max_x", "float"}, {"min_y", "float"}, {"max_y", "float"},
	{"x_count", "int"}, {"y_count", "int"}, {"mesh_x_pps", "int"}, {"mesh_y_pps", "int"},
	{"algo", "str"}, {"tension", "float"},
}

// meshError is a BedMeshError.
type meshError string

func (e meshError) Error() string { return string(e) }

func logf(format string, args ...any) { log.Printf(format, args...) }

// PEP 485 isclose()
func isclose(a, b, relTol, absTol float64) bool {
	return math.Abs(a-b) <= math.Max(relTol*math.Max(math.Abs(a), math.Abs(b)), absTol)
}

// return true if a coordinate is within the region
// specified by minC and maxC
func within(coord, minC, maxC [2]float64, tol float64) bool {
	return (maxC[0]+tol) >= coord[0] && coord[0] >= (minC[0]-tol) &&
		(maxC[1]+tol) >= coord[1] && coord[1] >= (minC[1]-tol)
}

// Constrain value between min and max
func constrain(val, minVal, maxVal float64) float64 {
	return math.Min(maxVal, math.Max(minVal, val))
}

// Linear interpolation between two values
func lerp(t, v0, v1 float64) float64 {
	return (1.-t)*v0 + t*v1
}

// tuple is a Python tuple of two numbers.
func tuple(p [2]float64) jinja.Tuple { return jinja.Tuple{p[0], p[1]} }

// reprPair is the text of a pair: "(x, y)" for a tuple, "[x, y]" for a list.
func reprPair(p [2]float64, list bool) string {
	if list {
		return jinja.Repr([]any{p[0], p[1]})
	}
	return jinja.Repr(tuple(p))
}

// retrieve comma separated pair from config
func parseConfigPair(config *configfile.Wrapper, option string, def int, minval, maxval *int) [2]int {
	pair := config.GetIntList(option, ",", 0, configfile.Default([]int{def, def}))
	if len(pair) != 2 {
		if len(pair) != 1 {
			configfile.Raise("bed_mesh: malformed '%s' value: %s", option, config.Get(option))
		}
		pair = []int{pair[0], pair[0]}
	}
	if minval != nil {
		if pair[0] < *minval || pair[1] < *minval {
			configfile.Raise("Option '%s' in section bed_mesh must have a minimum of %d", option, *minval)
		}
	}
	if maxval != nil {
		if pair[0] > *maxval || pair[1] > *maxval {
			configfile.Raise("Option '%s' in section bed_mesh must have a maximum of %d", option, *maxval)
		}
	}
	return [2]int{pair[0], pair[1]}
}

// retrieve comma separated pair from a g-code command
func parseGcmdPair(gcmd *gcode.Command, name string, minval *int) [2]int {
	var pair []int
	for _, v := range strings.Split(gcmd.GetString(name), ",") {
		n, err := pyfmt.ParseInt(strings.TrimSpace(v))
		if err != nil {
			panic(gcmd.Error("Unable to parse parameter '%s'", name))
		}
		pair = append(pair, int(n))
	}
	if len(pair) != 2 {
		if len(pair) != 1 {
			panic(gcmd.Error("Unable to parse parameter '%s'", name))
		}
		pair = []int{pair[0], pair[0]}
	}
	if minval != nil {
		if pair[0] < *minval || pair[1] < *minval {
			panic(gcmd.Error("Parameter '%s' must have a minimum of %d", name, *minval))
		}
	}
	return [2]int{pair[0], pair[1]}
}

// retrieve comma separated coordinate from a g-code command
func parseGcmdCoord(gcmd *gcode.Command, name string) [2]float64 {
	parts := strings.Split(gcmd.GetString(name), ",")
	if len(parts) != 2 {
		panic(gcmd.Error("Unable to parse parameter '%s'", name))
	}
	var out [2]float64
	for i, v := range parts {
		f, err := pyfmt.ParseFloat(strings.TrimSpace(v))
		if err != nil {
			panic(gcmd.Error("Unable to parse parameter '%s'", name))
		}
		out[i] = f
	}
	return out
}

func intp(v int) *int { return &v }

type offsetsProbe interface {
	Offsets(gcmd *gcode.Command) [3]float64
}

// FadeDisable is the fade height when the fade is off.
const FadeDisable = 0x7FFFFFFF

// BedMesh is the [bed_mesh] section: the move transform of the mesh.
type BedMesh struct {
	printer         *host.Printer
	lastPosition    []float64
	bmc             *BedMeshCalibrate
	zMesh           *ZMesh
	toolhead        *toolhead.ToolHead
	horizontalMoveZ float64
	fadeStart       float64
	fadeEnd         float64
	fadeDist        float64
	logFadeComplete bool
	baseFadeTarget  *float64
	fadeTarget      float64
	toolOffset      float64
	gcode           *gcode.Dispatch
	splitter        *MoveSplitter
	pmgr            *ProfileManager
	status          map[string]any
}

func load(config *configfile.Wrapper) any {
	b := &BedMesh{printer: host.Of(config)}
	b.printer.RegisterEventHandler("klippy:connect", func(...any) { b.handleConnect() })
	b.lastPosition = []float64{0., 0., 0., 0.}
	b.bmc = newBedMeshCalibrate(config, b)
	b.horizontalMoveZ = config.GetFloat("horizontal_move_z", configfile.Default(5.))
	b.fadeStart = config.GetFloat("fade_start", configfile.Default(1.))
	b.fadeEnd = config.GetFloat("fade_end", configfile.Default(0.))
	b.fadeDist = b.fadeEnd - b.fadeStart
	if b.fadeDist <= 0. {
		b.fadeStart, b.fadeEnd = FadeDisable, FadeDisable
	}
	b.logFadeComplete = false
	if v, ok := config.GetFloatOptional("fade_target", configfile.Default(nil)); ok {
		b.baseFadeTarget = &v
	}
	b.fadeTarget = 0.
	b.toolOffset = 0.
	b.gcode = gcode.Of(b.printer)
	b.splitter = &MoveSplitter{gcode: b.gcode}
	b.splitter.splitDeltaZ = config.GetFloat("split_delta_z", configfile.Default(.025), configfile.Min(0.01))
	b.splitter.moveCheckDistance = config.GetFloat("move_check_distance", configfile.Default(5.),
		configfile.Min(3.))
	// setup persistent storage
	b.pmgr = newProfileManager(config, b)
	// register gcodes
	b.gcode.RegisterCommand("BED_MESH_OUTPUT", b.cmdBedMeshOutput, false,
		"Retrieve interpolated grid of probed z-points")
	b.gcode.RegisterCommand("BED_MESH_MAP", b.cmdBedMeshMap, false,
		"Serialize mesh and output to terminal")
	b.gcode.RegisterCommand("BED_MESH_CLEAR", b.cmdBedMeshClear, false,
		"Clear the Mesh so no z-adjustment is made")
	b.gcode.RegisterCommand("BED_MESH_OFFSET", b.cmdBedMeshOffset, false,
		"Add X/Y offsets to the mesh lookup")
	// Register dump webhooks
	wh := webhooks.Of(b.printer)
	wh.RegisterEndpoint("bed_mesh/dump_mesh", b.handleDumpRequest)
	// Register transform
	gm := b.printer.LoadObject(config, "gcode_move").(*gcode_move.GCodeMove)
	gm.SetMoveTransform(b, false)
	// initialize status dict
	b.updateStatus()
	return b
}

func init() { host.RegisterModule("bed_mesh", load, nil) }

func (b *BedMesh) handleConnect() {
	b.toolhead = toolhead.Of(b.printer)
	b.bmc.printGeneratedPoints(func(s string) { logf("%s", s) }, true)
}

// SetMesh sets the mesh, or none with nil.
func (b *BedMesh) SetMesh(mesh *ZMesh) {
	if mesh != nil && b.fadeEnd != FadeDisable {
		b.logFadeComplete = true
		if b.baseFadeTarget == nil {
			b.fadeTarget = mesh.ZAverage()
		} else {
			b.fadeTarget = *b.baseFadeTarget
			minZ, maxZ := mesh.ZRange()
			if !(minZ <= b.fadeTarget && b.fadeTarget <= maxZ) && b.fadeTarget != 0. {
				// fade target is non-zero, out of mesh range
				errTarget := b.fadeTarget
				b.zMesh = nil
				b.fadeTarget = 0.
				panic(gcode.Errorf("bed_mesh: ERROR, fade_target lies outside of mesh z "+
					"range\nmin: %.4f, max: %.4f, fade_target: %.4f", minZ, maxZ, errTarget))
			}
		}
		minZ, maxZ := mesh.ZRange()
		if b.fadeDist <= math.Max(math.Abs(minZ), math.Abs(maxZ)) {
			b.zMesh = nil
			b.fadeTarget = 0.
			panic(gcode.Errorf("bed_mesh:  Mesh extends outside of the fade range, "+
				"please see the fade_start and fade_end options in"+
				"example-extras.cfg. fade distance: %.2f mesh min: %.4f"+
				"mesh max: %.4f", b.fadeDist, minZ, maxZ))
		}
	} else {
		b.fadeTarget = 0.
	}
	b.toolOffset = 0.
	b.zMesh = mesh
	b.splitter.initialize(mesh, b.fadeTarget)
	// cache the current position before a transform takes place
	b.printer.LookupObject("gcode_move").(*gcode_move.GCodeMove).ResetLastPosition()
	b.updateStatus()
}

func (b *BedMesh) zFactor(zPos float64) float64 {
	zPos += b.toolOffset
	if zPos >= b.fadeEnd {
		return 0.
	} else if zPos >= b.fadeStart {
		return (b.fadeEnd - zPos) / b.fadeDist
	}
	return 1.
}

// Position returns the last position without the mesh adjustment.
func (b *BedMesh) Position() []float64 {
	if b.zMesh == nil {
		// No mesh calibrated, so send toolhead position
		b.lastPosition = b.toolhead.Position()
		b.lastPosition[2] -= b.fadeTarget
	} else {
		// return current position minus the current z-adjustment
		curPos := b.toolhead.Position()
		x, y, z := curPos[0], curPos[1], curPos[2]
		maxAdj := b.zMesh.CalcZ(x, y)
		factor := 1.
		zAdj := maxAdj - b.fadeTarget
		fadeZPos := z + b.toolOffset
		if math.Min(fadeZPos, fadeZPos-maxAdj) >= b.fadeEnd {
			// Fade out is complete, no factor
			factor = 0.
		} else if math.Max(fadeZPos, fadeZPos-maxAdj) >= b.fadeStart {
			// Likely in the process of fading out adjustment.
			// Because we don't yet know the gcode z position, use
			// algebra to calculate the factor from the toolhead pos
			factor = (b.fadeEnd + b.fadeTarget - fadeZPos) / (b.fadeDist - zAdj)
			factor = constrain(factor, 0., 1.)
		}
		finalZAdj := factor*zAdj + b.fadeTarget
		b.lastPosition = append([]float64{x, y, z - finalZAdj}, curPos[3:]...)
	}
	return append([]float64(nil), b.lastPosition...)
}

// Move moves the toolhead with the mesh adjustment.
func (b *BedMesh) Move(newpos []float64, speed float64) {
	factor := b.zFactor(newpos[2])
	if b.zMesh == nil || factor == 0 {
		// No mesh calibrated, or mesh leveling phased out.
		x, y, z := newpos[0], newpos[1], newpos[2]
		if b.logFadeComplete {
			b.logFadeComplete = false
			logf("bed_mesh fade complete: Current Z: %.4f fade_target: %.4f ", z, b.fadeTarget)
		}
		b.toolhead.Move(append([]float64{x, y, z + b.fadeTarget}, newpos[3:]...), speed)
	} else {
		b.splitter.buildMove(b.lastPosition, newpos, factor)
		for !b.splitter.traverseComplete {
			splitMove := b.splitter.split()
			if splitMove != nil {
				b.toolhead.Move(splitMove, speed)
			} else {
				panic(gcode.Errorf("Mesh Leveling: Error splitting move "))
			}
		}
	}
	b.lastPosition = append([]float64(nil), newpos...)
}

func (b *BedMesh) Status(eventtime float64) map[string]any { return b.status }

func matrixList(m [][]float64) []any {
	out := make([]any, len(m))
	for i, row := range m {
		r := make([]any, len(row))
		for j, v := range row {
			r[j] = v
		}
		out[i] = r
	}
	return out
}

func (b *BedMesh) updateStatus() {
	b.status = map[string]any{
		"profile_name":  "",
		"mesh_min":      jinja.Tuple{0., 0.},
		"mesh_max":      jinja.Tuple{0., 0.},
		"probed_matrix": []any{[]any{}},
		"mesh_matrix":   []any{[]any{}},
		"profiles":      b.pmgr.profilesDict(),
	}
	if b.zMesh != nil {
		params := b.zMesh.MeshParams()
		b.status["profile_name"] = b.zMesh.ProfileName()
		b.status["mesh_min"] = jinja.Tuple{paramFloat(params, "min_x"), paramFloat(params, "min_y")}
		b.status["mesh_max"] = jinja.Tuple{paramFloat(params, "max_x"), paramFloat(params, "max_y")}
		b.status["probed_matrix"] = matrixList(b.zMesh.ProbedMatrix())
		b.status["mesh_matrix"] = matrixList(b.zMesh.MeshMatrix())
	}
}

// Mesh returns the mesh, or nil.
func (b *BedMesh) Mesh() *ZMesh { return b.zMesh }

func (b *BedMesh) cmdBedMeshOutput(gcmd *gcode.Command) {
	respond := func(s string) { gcmd.RespondInfo(s, true) }
	if gcmd.GetInt("PGP", gcode.Default(0)) != 0 {
		// Print Generated Points instead of mesh
		b.bmc.printGeneratedPoints(respond, false)
	} else if b.zMesh == nil {
		gcmd.RespondInfo("Bed has not been probed", true)
	} else {
		b.zMesh.printProbedMatrix(respond)
		b.zMesh.printMesh(gcmd.RespondRaw, b.horizontalMoveZ)
	}
}

func (b *BedMesh) cmdBedMeshMap(gcmd *gcode.Command) {
	if b.zMesh != nil {
		params := b.zMesh.MeshParams()
		outdict := jinja.NewDict()
		outdict.Set("mesh_min", jinja.Tuple{paramFloat(params, "min_x"), paramFloat(params, "min_y")})
		outdict.Set("mesh_max", jinja.Tuple{paramFloat(params, "max_x"), paramFloat(params, "max_y")})
		outdict.Set("z_positions", matrixList(b.zMesh.ProbedMatrix()))
		gcmd.RespondRaw("mesh_map_output " + jinja.JSONDumps(outdict))
	} else {
		gcmd.RespondInfo("Bed has not been probed", true)
	}
}

func (b *BedMesh) cmdBedMeshClear(gcmd *gcode.Command) { b.SetMesh(nil) }

func (b *BedMesh) cmdBedMeshOffset(gcmd *gcode.Command) {
	if b.zMesh != nil {
		offsets := [2]float64{math.NaN(), math.NaN()}
		for i, axis := range []string{"X", "Y"} {
			if v, ok := gcmd.GetFloatOptional(axis); ok {
				offsets[i] = v
			}
		}
		b.zMesh.setMeshOffsets(offsets)
		if toolOffset, ok := gcmd.GetFloatOptional("ZFADE"); ok {
			b.toolOffset = toolOffset
		}
		b.printer.LookupObject("gcode_move").(*gcode_move.GCodeMove).ResetLastPosition()
	} else {
		gcmd.RespondInfo("No mesh loaded to offset", true)
	}
}

func (b *BedMesh) handleDumpRequest(req *webhooks.WebRequest) {
	eventtime := b.printer.Reactor().Monotonic()
	prb, _ := b.printer.TryLookupObject("probe").(offsetsProbe)
	thSts := toolhead.Of(b.printer).Status(eventtime)
	result := jinja.NewDict()
	result.Set("current_mesh", jinja.NewDict())
	result.Set("profiles", b.pmgr.profilesDict())
	if b.zMesh != nil {
		cm := jinja.NewDict()
		cm.Set("name", b.zMesh.ProfileName())
		cm.Set("probed_matrix", matrixList(b.zMesh.ProbedMatrix()))
		cm.Set("mesh_matrix", matrixList(b.zMesh.MeshMatrix()))
		cm.Set("mesh_params", b.zMesh.MeshParams())
		result.Set("current_mesh", cm)
	}
	meshArgs := req.GetDict("mesh_args", map[string]any{})
	var gcmd *gcode.Command
	if len(meshArgs) > 0 {
		params := map[string]string{}
		for k, v := range meshArgs {
			params[k] = jinja.Str(v)
		}
		gcmd = b.gcode.CreateCommand("", "", params)
		mutex := b.gcode.Mutex()
		mutex.Lock()
		func() {
			defer mutex.Unlock()
			result.Set("calibration", b.bmc.dumpCalibration(gcmd))
		}()
	} else {
		result.Set("calibration", b.bmc.dumpCalibration(nil))
	}
	offsets := []any{0, 0, 0}
	if prb != nil {
		o := prb.Offsets(gcmd)
		offsets = []any{o[0], o[1], o[2]}
	}
	result.Set("probe_offsets", offsets)
	result.Set("axis_minimum", thSts["axis_minimum"])
	result.Set("axis_maximum", thSts["axis_maximum"])
	req.Send(result)
}

// ######################################################################
// Calibration
// ######################################################################

// The zero reference modes.
const (
	ZrefDisabled = 0 // Zero reference disabled
	ZrefInMesh   = 1 // Zero reference position within mesh
	ZrefProbe    = 2 // Zero refrennce position outside of mesh, probe needed
)

// meshConfig is the mesh configuration, in the order of its keys.
type meshConfig struct {
	xCount, yCount int
	meshXPPS       int
	meshYPPS       int
	algo           string
	tension        float64
}

func (c meshConfig) items() [][2]any {
	return [][2]any{{"x_count", c.xCount}, {"y_count", c.yCount}, {"mesh_x_pps", c.meshXPPS},
		{"mesh_y_pps", c.meshYPPS}, {"algo", c.algo}, {"tension", c.tension}}
}

func (c meshConfig) dict() *MeshParams {
	d := jinja.NewDict()
	for _, kv := range c.items() {
		d.Set(kv[0].(string), kv[1])
	}
	return d
}

type origConfig struct {
	radius  *float64
	origin  *[2]float64
	mesh    meshConfig
	meshMin [2]float64
	meshMax [2]float64
}

// BedMeshCalibrate is BED_MESH_CALIBRATE.
type BedMeshCalibrate struct {
	printer        *host.Printer
	orig           origConfig
	radius         *float64
	origin         *[2]float64
	meshMin        [2]float64
	meshMax        [2]float64
	adaptiveMargin float64
	bedmesh        *BedMesh
	meshConfig     meshConfig
	probeMgr       *ProbeManager
	profileName    *string
	gcode          *gcode.Dispatch
}

var algos = []string{"lagrange", "bicubic"}

func newBedMeshCalibrate(config *configfile.Wrapper, bedmesh *BedMesh) *BedMeshCalibrate {
	c := &BedMeshCalibrate{printer: host.Of(config), bedmesh: bedmesh}
	c.adaptiveMargin = config.GetFloat("adaptive_margin", configfile.Default(0.0))
	c.initMeshConfig(config)
	c.probeMgr = newProbeManager(config, &c.orig, c.probeFinalize)
	if err := c.probeMgr.generatePoints(c.meshConfig, c.meshMin, c.meshMax, c.radius, c.origin,
		"automatic"); err != nil {
		configfile.Raise("%s", err.Error())
	}
	name := "default"
	c.profileName = &name
	c.gcode = gcode.Of(c.printer)
	c.gcode.RegisterCommand("BED_MESH_CALIBRATE", c.cmdBedMeshCalibrate, false, "Perform Mesh Bed Leveling")
	return c
}

func (c *BedMeshCalibrate) printGeneratedPoints(printFunc func(string), truncate bool) {
	xOffset, yOffset := 0., 0.
	if prb, ok := c.printer.TryLookupObject("probe").(offsetsProbe); ok {
		o := prb.Offsets(nil)
		xOffset, yOffset = o[0], o[1]
	}
	printFunc("bed_mesh: generated points\nIndex" +
		" |  Tool Adjusted  |   Probe")
	points := c.probeMgr.baseP()
	for i, p := range points {
		x, y := p[0], p[1]
		if i >= 50 && truncate {
			end := len(points) - 1
			printFunc(fmt.Sprintf("...points %d through %d truncated", i, end))
			break
		}
		adjPt := fmt.Sprintf("(%.1f, %.1f)", x-xOffset, y-yOffset)
		meshPt := fmt.Sprintf("(%.1f, %.1f)", x, y)
		printFunc(fmt.Sprintf("  %-4d| %-16s| %s", i, adjPt, meshPt))
	}
	if zeroRefPos := c.probeMgr.zeroRefPos; zeroRefPos != nil {
		printFunc(fmt.Sprintf("bed_mesh: zero_reference_position is (%.2f, %.2f)",
			zeroRefPos[0], zeroRefPos[1]))
	}
	if subs := c.probeMgr.substitutes; len(subs.keys) > 0 {
		printFunc("bed_mesh: faulty region points")
		for _, i := range subs.keys {
			v := subs.m[i]
			pt := points[i]
			items := make([]any, len(v))
			for j, p := range v {
				items[j] = tuple(p)
			}
			printFunc(fmt.Sprintf("%d (%.2f, %.2f), substituted points: %s",
				i, pt[0], pt[1], jinja.Repr(items)))
		}
	}
}

func (c *BedMeshCalibrate) initMeshConfig(config *configfile.Wrapper) {
	var xCnt, yCnt int
	var minX, minY, maxX, maxY float64
	if r, ok := config.GetFloatOptional("mesh_radius", configfile.Default(nil), configfile.Above(0.)); ok {
		c.radius = &r
		o := config.GetFloatList("mesh_origin", ",", 2, configfile.Default([]float64{0., 0.}))
		c.origin = &[2]float64{o[0], o[1]}
		xCnt = config.GetInt("round_probe_count", configfile.Default(5), configfile.Min(3))
		yCnt = xCnt
		// round beds must have an odd number of points along each axis
		if xCnt&1 == 0 {
			configfile.Raise("bed_mesh: probe_count must be odd for round beds")
		}
		// radius may have precision to .1mm
		r = math.Floor(r*10) / 10
		c.radius = &r
		rr := r
		c.orig.radius = &rr
		oo := *c.origin
		c.orig.origin = &oo
		minX, minY = -r, -r
		maxX, maxY = r, r
	} else {
		// rectangular
		pair := parseConfigPair(config, "probe_count", 3, intp(3), nil)
		xCnt, yCnt = pair[0], pair[1]
		mn := config.GetFloatList("mesh_min", ",", 2)
		mx := config.GetFloatList("mesh_max", ",", 2)
		minX, minY = mn[0], mn[1]
		maxX, maxY = mx[0], mx[1]
		if maxX <= minX || maxY <= minY {
			configfile.Raise("bed_mesh: invalid min/max points")
		}
	}
	c.meshConfig.xCount, c.meshConfig.yCount = xCnt, yCnt
	c.meshMin = [2]float64{minX, minY}
	c.meshMax = [2]float64{maxX, maxY}
	c.orig.meshMin, c.orig.meshMax = c.meshMin, c.meshMax

	pps := parseConfigPair(config, "mesh_pps", 2, intp(0), nil)
	c.meshConfig.meshXPPS, c.meshConfig.meshYPPS = pps[0], pps[1]
	c.meshConfig.algo = strings.ToLower(strings.TrimSpace(config.Get("algorithm",
		configfile.Default("lagrange"))))
	c.meshConfig.tension = config.GetFloat("bicubic_tension", configfile.Default(.2), configfile.Min(0.),
		configfile.Max(2.))
	c.orig.mesh = c.meshConfig
	c.verifyAlgorithm(func(msg string) { configfile.Raise("%s", msg) })
}

func (c *BedMeshCalibrate) verifyAlgorithm(raise func(msg string)) {
	params := &c.meshConfig
	xPPS := params.meshXPPS
	yPPS := params.meshYPPS
	known := false
	for _, a := range algos {
		known = known || params.algo == a
	}
	if !known {
		raise(fmt.Sprintf("bed_mesh: Unknown algorithm <%s>", c.meshConfig.algo))
	}
	// Check the algorithm against the current configuration
	maxProbeCnt := max(params.xCount, params.yCount)
	minProbeCnt := min(params.xCount, params.yCount)
	if max(xPPS, yPPS) == 0 {
		// Interpolation disabled
		c.meshConfig.algo = "direct"
	} else if params.algo == "lagrange" && maxProbeCnt > 6 {
		// Lagrange interpolation tends to oscillate when using more
		// than 6 samples
		raise(fmt.Sprintf("bed_mesh: cannot exceed a probe_count of 6 when using "+
			"lagrange interpolation. Configured Probe Count: %d, %d",
			c.meshConfig.xCount, c.meshConfig.yCount))
	} else if params.algo == "bicubic" && minProbeCnt < 4 {
		if maxProbeCnt > 6 {
			raise(fmt.Sprintf("bed_mesh: invalid probe_count option when using bicubic "+
				"interpolation.  Combination of 3 points on one axis with "+
				"more than 6 on another is not permitted. "+
				"Configured Probe Count: %d, %d", c.meshConfig.xCount, c.meshConfig.yCount))
		} else {
			logf("bed_mesh: bicubic interpolation with a probe_count of "+
				"less than 4 points detected.  Forcing lagrange "+
				"interpolation. Configured Probe Count: %d, %d",
				c.meshConfig.xCount, c.meshConfig.yCount)
			params.algo = "lagrange"
		}
	}
}

type statuser interface {
	Status(eventtime float64) map[string]any
}

func anyFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	}
	return 0
}

func (c *BedMeshCalibrate) setAdaptiveMesh(gcmd *gcode.Command) bool {
	if gcmd.GetInt("ADAPTIVE", gcode.Default(0)) == 0 {
		return false
	}
	excludeObjects, ok := c.printer.TryLookupObject("exclude_object").(statuser)
	if !ok {
		gcmd.RespondInfo("Exclude objects not enabled. Using full mesh...", true)
		return false
	}
	objects, _ := excludeObjects.Status(0)["objects"].([]any)
	if len(objects) == 0 {
		return false
	}
	margin := gcmd.GetFloat("ADAPTIVE_MARGIN", gcode.Default(c.adaptiveMargin))

	// List all exclude_object points by axis and iterate over
	// all polygon points, and pick the min and max or each axis
	var listOfXs, listOfYs []float64
	gcmd.RespondInfo(fmt.Sprintf("Found %d objects", len(objects)), true)
	for _, obj := range objects {
		d := obj.(*jinja.Dict)
		polygon, ok := d.Get("polygon")
		if !ok {
			panic(gcmd.Error("KeyError: 'polygon'"))
		}
		for _, point := range polygon.([]any) {
			p := point.([]any)
			listOfXs = append(listOfXs, anyFloat(p[0]))
			listOfYs = append(listOfYs, anyFloat(p[1]))
		}
	}

	// Define bounds of adaptive mesh area
	minOf := func(l []float64) float64 {
		m := l[0]
		for _, v := range l[1:] {
			if v < m {
				m = v
			}
		}
		return m
	}
	maxOf := func(l []float64) float64 {
		m := l[0]
		for _, v := range l[1:] {
			if v > m {
				m = v
			}
		}
		return m
	}
	meshMin := [2]float64{minOf(listOfXs), minOf(listOfYs)}
	meshMax := [2]float64{maxOf(listOfXs), maxOf(listOfYs)}
	adjustedMeshMin := [2]float64{meshMin[0] - margin, meshMin[1] - margin}
	adjustedMeshMax := [2]float64{meshMax[0] + margin, meshMax[1] + margin}

	// Force margin to respect original mesh bounds
	adjustedMeshMin[0] = math.Max(adjustedMeshMin[0], c.orig.meshMin[0])
	adjustedMeshMin[1] = math.Max(adjustedMeshMin[1], c.orig.meshMin[1])
	adjustedMeshMax[0] = math.Min(adjustedMeshMax[0], c.orig.meshMax[0])
	adjustedMeshMax[1] = math.Min(adjustedMeshMax[1], c.orig.meshMax[1])

	adjustedMeshSize := [2]float64{adjustedMeshMax[0] - adjustedMeshMin[0],
		adjustedMeshMax[1] - adjustedMeshMin[1]}

	// Compute a ratio between the adapted and original sizes
	ratio := [2]float64{adjustedMeshSize[0] / (c.orig.meshMax[0] - c.orig.meshMin[0]),
		adjustedMeshSize[1] / (c.orig.meshMax[1] - c.orig.meshMin[1])}

	gcmd.RespondInfo(fmt.Sprintf("Original mesh bounds: (%s,%s)",
		reprPair(c.orig.meshMin, false), reprPair(c.orig.meshMax, false)), true)
	gcmd.RespondInfo(fmt.Sprintf("Original probe count: (%d,%d)",
		c.meshConfig.xCount, c.meshConfig.yCount), true)
	gcmd.RespondInfo(fmt.Sprintf("Adapted mesh bounds: (%s,%s)",
		reprPair(adjustedMeshMin, true), reprPair(adjustedMeshMax, true)), true)
	gcmd.RespondInfo(fmt.Sprintf("Ratio: (%s, %s)", pyfmt.Float(ratio[0]), pyfmt.Float(ratio[1])), true)

	newXProbeCount := int(math.Ceil(float64(c.meshConfig.xCount) * ratio[0]))
	newYProbeCount := int(math.Ceil(float64(c.meshConfig.yCount) * ratio[1]))

	// There is one case, where we may have to adjust the probe counts:
	// axis0 < 4 and axis1 > 6 (see _verify_algorithm).
	minNumOfProbes := 3
	if max(newXProbeCount, newYProbeCount) > 6 && min(newXProbeCount, newYProbeCount) < 4 {
		minNumOfProbes = 4
	}

	newXProbeCount = max(minNumOfProbes, newXProbeCount)
	newYProbeCount = max(minNumOfProbes, newYProbeCount)

	gcmd.RespondInfo(fmt.Sprintf("Adapted probe count: (%d,%d)", newXProbeCount, newYProbeCount), true)

	// If the adapted mesh size is too small, adjust it to something
	// useful.
	adjustedMeshSize = [2]float64{math.Max(adjustedMeshSize[0], float64(newXProbeCount)),
		math.Max(adjustedMeshSize[1], float64(newYProbeCount))}

	if c.radius != nil {
		adaptedRadius := math.Sqrt(adjustedMeshSize[0]*adjustedMeshSize[0]+
			adjustedMeshSize[1]*adjustedMeshSize[1]) / 2
		adaptedOrigin := [2]float64{adjustedMeshMin[0] + adjustedMeshSize[0]/2,
			adjustedMeshMin[1] + adjustedMeshSize[1]/2}
		toAdaptedOrigin := math.Sqrt(adaptedOrigin[0]*adaptedOrigin[0] + adaptedOrigin[1]*adaptedOrigin[1])
		// If the adapted mesh size is smaller than the default/full
		// mesh, adjust the parameters. Otherwise, just do the full mesh.
		if adaptedRadius+toAdaptedOrigin < *c.radius {
			c.radius = &adaptedRadius
			c.origin = &adaptedOrigin
			c.meshMin = [2]float64{-adaptedRadius, -adaptedRadius}
			c.meshMax = [2]float64{adaptedRadius, adaptedRadius}
			newProbeCount := max(newXProbeCount, newYProbeCount)
			// Adaptive meshes require odd number of points
			newProbeCount += 1 - (newProbeCount % 2)
			c.meshConfig.xCount, c.meshConfig.yCount = newProbeCount, newProbeCount
		}
	} else {
		c.meshMin = adjustedMeshMin
		c.meshMax = adjustedMeshMax
		c.meshConfig.xCount = newXProbeCount
		c.meshConfig.yCount = newYProbeCount
	}
	c.profileName = nil
	return true
}

func (c *BedMeshCalibrate) updateConfig(gcmd *gcode.Command) error {
	// reset default configuration
	c.radius = c.orig.radius
	c.origin = c.orig.origin
	c.meshMin = c.orig.meshMin
	c.meshMax = c.orig.meshMax
	c.meshConfig = c.orig.mesh

	params := gcmd.Params()
	has := func(k string) bool { _, ok := params[k]; return ok }
	needCfgUpdate := false
	if c.radius != nil {
		if has("MESH_RADIUS") {
			r := gcmd.GetFloat("MESH_RADIUS")
			r = math.Floor(r*10) / 10
			c.radius = &r
			c.meshMin = [2]float64{-r, -r}
			c.meshMax = [2]float64{r, r}
			needCfgUpdate = true
		}
		if has("MESH_ORIGIN") {
			o := parseGcmdCoord(gcmd, "MESH_ORIGIN")
			c.origin = &o
			needCfgUpdate = true
		}
		if has("ROUND_PROBE_COUNT") {
			cnt := gcmd.GetInt("ROUND_PROBE_COUNT", gcode.Min(3))
			c.meshConfig.xCount = cnt
			c.meshConfig.yCount = cnt
			needCfgUpdate = true
		}
	} else {
		if has("MESH_MIN") {
			c.meshMin = parseGcmdCoord(gcmd, "MESH_MIN")
			needCfgUpdate = true
		}
		if has("MESH_MAX") {
			c.meshMax = parseGcmdCoord(gcmd, "MESH_MAX")
			needCfgUpdate = true
		}
		if has("PROBE_COUNT") {
			p := parseGcmdPair(gcmd, "PROBE_COUNT", intp(3))
			c.meshConfig.xCount, c.meshConfig.yCount = p[0], p[1]
			needCfgUpdate = true
		}
	}

	if has("MESH_PPS") {
		p := parseGcmdPair(gcmd, "MESH_PPS", intp(0))
		c.meshConfig.meshXPPS, c.meshConfig.meshYPPS = p[0], p[1]
		needCfgUpdate = true
	}

	if has("ALGORITHM") {
		c.meshConfig.algo = strings.ToLower(strings.TrimSpace(gcmd.GetString("ALGORITHM")))
		needCfgUpdate = true
	}

	needCfgUpdate = c.setAdaptiveMesh(gcmd) || needCfgUpdate
	probeMethod := gcmd.GetString("METHOD", gcode.Default("automatic"))

	c.verifyAlgorithm(func(msg string) { panic(gcmd.Error("%s", msg)) })
	if err := c.probeMgr.generatePoints(c.meshConfig, c.meshMin, c.meshMax, c.radius, c.origin,
		probeMethod); err != nil {
		return err
	}
	if needCfgUpdate {
		var lines []string
		for _, kv := range c.meshConfig.items() {
			lines = append(lines, fmt.Sprintf("%s: %s", kv[0], pyStr(kv[1])))
		}
		logf("Updated Mesh Configuration:\n%s", strings.Join(lines, "\n"))
	}
	return nil
}

func (c *BedMeshCalibrate) dumpCalibration(gcmd *gcode.Command) *jinja.Dict {
	if gcmd != nil && len(gcmd.Params()) > 0 {
		if err := c.updateConfig(gcmd); err != nil {
			panic(gcode.Errorf("%s", err.Error()))
		}
	}
	cfg := c.meshConfig.dict()
	cfg.Set("mesh_min", tuple(c.meshMin))
	cfg.Set("mesh_max", tuple(c.meshMax))
	if c.origin != nil {
		cfg.Set("origin", tuple(*c.origin))
	} else {
		cfg.Set("origin", nil)
	}
	if c.radius != nil {
		cfg.Set("radius", *c.radius)
	} else {
		cfg.Set("radius", nil)
	}
	pts := func(l [][2]float64) []any {
		out := make([]any, len(l))
		for i, p := range l {
			out[i] = tuple(p)
		}
		return out
	}
	var rapid []any
	for _, rp := range c.probeMgr.iterRapidPath() {
		rapid = append(rapid, jinja.Tuple{tuple(rp.pt), rp.isProbe})
	}
	d := jinja.NewDict()
	d.Set("points", pts(c.probeMgr.baseP()))
	d.Set("config", cfg)
	d.Set("probe_path", pts(c.probeMgr.stdPath()))
	d.Set("rapid_path", rapid)
	return d
}

func (c *BedMeshCalibrate) cmdBedMeshCalibrate(gcmd *gcode.Command) {
	name := gcmd.GetString("PROFILE", gcode.Default("default"))
	c.profileName = &name
	if strings.TrimSpace(name) == "" {
		panic(gcmd.Error("Value for parameter 'PROFILE' must be specified"))
	}
	c.bedmesh.SetMesh(nil)
	if err := c.updateConfig(gcmd); err != nil {
		panic(gcmd.Error("%s", err.Error()))
	}
	c.probeMgr.startProbe(gcmd)
}

func (c *BedMeshCalibrate) probeFinalize(results [][3]float64) {
	zOffset := 0.
	positions := make([][3]float64, len(results))
	for i, p := range results {
		positions[i] = [3]float64{mathutil.Round(p[0], 2), mathutil.Round(p[1], 2), p[2]}
	}
	if c.probeMgr.zrefMode == ZrefProbe {
		refPos := positions[len(positions)-1]
		positions = positions[:len(positions)-1]
		logf("bed_mesh: z-offset replaced with probed z value at position (%.2f, %.2f, %.6f)",
			refPos[0], refPos[1], refPos[2])
		zOffset = refPos[2]
	}
	basePoints := c.probeMgr.baseP()
	params := c.meshConfig.dict()
	minX, maxX, minY, maxY := basePoints[0][0], basePoints[0][0], basePoints[0][1], basePoints[0][1]
	for _, p := range basePoints[1:] {
		if p[0] < minX {
			minX = p[0]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	params.Set("min_x", minX)
	params.Set("max_x", maxX)
	params.Set("min_y", minY)
	params.Set("max_y", maxY)
	xCnt := c.meshConfig.xCount
	yCnt := c.meshConfig.yCount

	subs := c.probeMgr.substitutes
	probedPts := positions
	if len(subs.keys) > 0 {
		// Replace substituted points with the original generated
		// point.  Its Z Value is the average probed Z of the
		// substituted points.
		var correctedPts [][3]float64
		idxOffset := 0
		startIdx := 0
		for _, i := range subs.keys {
			pts := subs.m[i]
			fpt := [3]float64{basePoints[i][0], basePoints[i][1], 0}
			// offset the index to account for additional samples
			idx := i + idxOffset
			// Add "normal" points
			correctedPts = append(correctedPts, positions[startIdx:idx]...)
			var zs []float64
			for _, p := range positions[idx : idx+len(pts)] {
				zs = append(zs, p[2])
			}
			avgZ := mathutil.Sum(zs...) / float64(len(pts))
			idxOffset += len(pts) - 1
			startIdx = idx + len(pts)
			fpt[2] = avgZ
			logf("bed_mesh: Replacing value at faulty index %d"+
				" (%.4f, %.4f): avg value = %.6f, avg w/ z_offset = %.6f",
				i, fpt[0], fpt[1], avgZ, avgZ-zOffset)
			correctedPts = append(correctedPts, fpt)
		}
		correctedPts = append(correctedPts, positions[startIdx:]...)
		positions = correctedPts
	}

	// validate length of result
	if len(basePoints) != len(positions) {
		c.dumpPoints(probedPts, positions)
		panic(gcode.Errorf("bed_mesh: invalid position list size, "+
			"generated count: %d, probed count: %d", len(basePoints), len(positions)))
	}

	var probedMatrix [][]float64
	var row []float64
	prevPos := basePoints[0]
	for i, pos := range basePoints {
		result := positions[i]
		offsetPos := pos
		if !isclose(offsetPos[0], result[0], 1e-09, .5) || !isclose(offsetPos[1], result[1], 1e-09, .5) {
			logf("bed_mesh: point deviation > .5mm: orig pt = (%.2f, %.2f)"+
				", probed pt = (%.2f, %.2f)", offsetPos[0], offsetPos[1], result[0], result[1])
		}
		zPos := result[2] - zOffset
		if !isclose(pos[1], prevPos[1], 1e-09, .1) {
			// y has changed, append row and start new
			probedMatrix = append(probedMatrix, row)
			row = nil
		}
		if pos[0] > prevPos[0] {
			// probed in the positive direction
			row = append(row, zPos)
		} else {
			// probed in the negative direction
			row = append([]float64{zPos}, row...)
		}
		prevPos = pos
	}
	// append last row
	probedMatrix = append(probedMatrix, row)

	// make sure the y-axis is the correct length
	if len(probedMatrix) != yCnt {
		panic(gcode.Errorf("bed_mesh: Invalid y-axis table length\n"+
			"Probed table length: %d Probed Table:\n%s", len(probedMatrix),
			jinja.Repr(matrixList(probedMatrix))))
	}

	if c.radius != nil {
		// round bed, extrapolate probed values to create a square mesh
		for ri, row := range probedMatrix {
			rowSize := len(row)
			if rowSize&1 == 0 {
				// an even number of points in a row shouldn't be possible
				msg := "bed_mesh: incorrect number of points sampled on X\n"
				msg += "Probed Table:\n"
				msg += jinja.Repr(matrixList(probedMatrix))
				panic(gcode.Errorf("%s", msg))
			}
			bufCnt := (xCnt - rowSize) / 2
			if bufCnt == 0 {
				continue
			}
			var out []float64
			for range bufCnt {
				out = append(out, row[0])
			}
			out = append(out, row...)
			for range bufCnt {
				out = append(out, row[rowSize-1])
			}
			probedMatrix[ri] = out
		}
	}

	//  make sure that the x-axis is the correct length
	for _, row := range probedMatrix {
		if len(row) != xCnt {
			panic(gcode.Errorf("bed_mesh: invalid x-axis table length\n"+
				"Probed table length: %d Probed Table:\n%s", len(probedMatrix),
				jinja.Repr(matrixList(probedMatrix))))
		}
	}

	name := ""
	if c.profileName != nil {
		name = *c.profileName
	}
	zMesh := NewZMesh(params, name)
	if err := zMesh.buildMesh(probedMatrix); err != nil {
		panic(gcode.Errorf("%s", err.Error()))
	}
	if c.probeMgr.zrefMode == ZrefInMesh {
		// The reference can be anywhere in the mesh, therefore
		// it is necessary to set the reference after the initial mesh
		// is generated to lookup the correct z value.
		zeroRefPos := c.probeMgr.zeroRefPos
		zMesh.setZeroReference(zeroRefPos[0], zeroRefPos[1])
	}
	c.bedmesh.SetMesh(zMesh)
	c.gcode.RespondInfo("Mesh Bed Leveling Complete", true)
	if c.profileName != nil {
		c.bedmesh.pmgr.saveProfile(*c.profileName)
	}
}

func (c *BedMeshCalibrate) dumpPoints(probedPts, correctedPts [][3]float64) {
	// logs generated points with offset applied, points received
	// from the finalize callback, and the list of corrected points
	points := c.probeMgr.baseP()
	maxLen := max(len(points), len(probedPts), len(correctedPts))
	logf("bed_mesh: calibration point dump\nIndex | %-17s| %-25s| Corrected Point",
		"Generated Point", "Probed Point")
	for i := range maxLen {
		genPt, probedPt, corrPt := "", "", ""
		if i < len(points) {
			genPt = fmt.Sprintf("(%.2f, %.2f)", points[i][0], points[i][1])
		}
		if i < len(probedPts) {
			p := probedPts[i]
			probedPt = fmt.Sprintf("(%.2f, %.2f, %.4f)", p[0], p[1], p[2])
		}
		if i < len(correctedPts) {
			p := correctedPts[i]
			corrPt = fmt.Sprintf("(%.2f, %.2f, %.4f)", p[0], p[1], p[2])
		}
		logf("  %-4d| %-17s| %-25s| %s", i, genPt, probedPt, corrPt)
	}
}
