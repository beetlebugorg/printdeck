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
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/pyfmt"
	"printdeck/internal/klipper/toolhead"
)

// substitutes are the points of each faulty point, in the order of the points (an OrderedDict).
type substitutes struct {
	keys []int
	m    map[int][][2]float64
}

func (s *substitutes) clear() { s.keys, s.m = nil, map[int][][2]float64{} }

func (s *substitutes) set(i int, pts [][2]float64) {
	if _, ok := s.m[i]; !ok {
		s.keys = append(s.keys, i)
	}
	s.m[i] = pts
}

func (s *substitutes) has(i int) bool {
	_, ok := s.m[i]
	return ok
}

type faultyRegion struct{ c1, c3 [2]float64 }

type rapidPoint struct {
	pt      [2]float64
	isProbe bool
}

// ProbeManager makes the probe points of the mesh and starts the probing.
type ProbeManager struct {
	printer         *host.Printer
	cfgOvershoot    float64
	orig            *origConfig
	faultyRegions   []faultyRegion
	overshoot       float64
	zeroRefPos      *[2]float64
	zrefMode        int
	basePoints      [][2]float64
	substitutes     substitutes
	isRound         bool
	probeHelper     *probe.PointsHelper
	rapidScanHelper *RapidScanHelper
}

func newProbeManager(config *configfile.Wrapper, orig *origConfig, finalize func([][3]float64)) *ProbeManager {
	m := &ProbeManager{printer: host.Of(config), orig: orig}
	m.cfgOvershoot = config.GetFloat("scan_overshoot", configfile.Default(0.), configfile.Min(1.))
	m.overshoot = m.cfgOvershoot
	if zr := config.GetFloatList("zero_reference_position", ",", 2, configfile.Default(nil)); zr != nil {
		m.zeroRefPos = &[2]float64{zr[0], zr[1]}
	}
	m.zrefMode = ZrefDisabled
	m.substitutes.clear()
	m.isRound = orig.radius != nil
	m.probeHelper = probe.NewPointsHelper(config, func(results []*manual_probe.ProbeResult) string {
		finalize(resultPoints(results))
		return ""
	}, [][]float64{})
	m.probeHelper.UseXYOffsets(true)
	m.rapidScanHelper = newRapidScanHelper(config, m, finalize)
	m.initFaultyRegions(config)
	return m
}

func resultPoints(results []*manual_probe.ProbeResult) [][3]float64 {
	out := make([][3]float64, len(results))
	for i, r := range results {
		out[i] = [3]float64{r.BedX(), r.BedY(), r.BedZ()}
	}
	return out
}

func reprRegion(c1, c3 [2]float64) string {
	return jinja.Repr([]any{[]any{c1[0], c1[1]}, []any{c3[0], c3[1]}})
}

func (m *ProbeManager) initFaultyRegions(config *configfile.Wrapper) {
	for i := 1; i < 100; i++ {
		start := config.GetFloatList(fmt.Sprintf("faulty_region_%d_min", i), ",", 2, configfile.Default(nil))
		if start == nil {
			break
		}
		end := config.GetFloatList(fmt.Sprintf("faulty_region_%d_max", i), ",", 2)
		// Validate the corners.  If necessary reorganize them.
		// c1 = min point, c3 = max point
		//  c4 ---- c3
		//  |        |
		//  c1 ---- c2
		c1 := [2]float64{math.Min(start[0], end[0]), math.Min(start[1], end[1])}
		c3 := [2]float64{math.Max(start[0], end[0]), math.Max(start[1], end[1])}
		c2 := [2]float64{c1[0], c3[1]}
		c4 := [2]float64{c3[0], c1[1]}
		// Check for overlapping regions
		for j, prev := range m.faultyRegions {
			prevC1, prevC3 := prev.c1, prev.c3
			prevC2 := [2]float64{prevC1[0], prevC3[1]}
			prevC4 := [2]float64{prevC3[0], prevC1[1]}
			// Validate that no existing corner is within the new region
			for _, coord := range [][2]float64{prevC1, prevC2, prevC3, prevC4} {
				if within(coord, c1, c3, 0) {
					configfile.Raise("bed_mesh: Existing faulty_region_%d %s overlaps "+
						"added faulty_region_%d %s", j+1, reprRegion(prevC1, prevC3), i, reprRegion(c1, c3))
				}
			}
			// Validate that no new corner is within an existing region
			for _, coord := range [][2]float64{c1, c2, c3, c4} {
				if within(coord, prevC1, prevC3, 0) {
					configfile.Raise("bed_mesh: Added faulty_region_%d %s overlaps "+
						"existing faulty_region_%d %s", i, reprRegion(c1, c3), j+1, reprRegion(prevC1, prevC3))
				}
			}
		}
		m.faultyRegions = append(m.faultyRegions, faultyRegion{c1, c3})
	}
}

type namedProbe interface {
	Status(eventtime float64) map[string]any
}

func (m *ProbeManager) startProbe(gcmd *gcode.Command) {
	method := strings.ToLower(gcmd.GetString("METHOD", gcode.Default("automatic")))
	canScan := false
	if pprobe, ok := m.printer.TryLookupObject("probe").(namedProbe); ok {
		probeName, _ := pprobe.Status(0)["name"].(string)
		canScan = strings.HasPrefix(probeName, "probe_eddy_current")
	}
	if method == "rapid_scan" && canScan {
		m.rapidScanHelper.performRapidScan(gcmd)
	} else {
		m.probeHelper.StartProbe(gcmd)
	}
}

func (m *ProbeManager) baseP() [][2]float64 { return m.basePoints }

func (m *ProbeManager) generatePoints(meshConfig meshConfig, meshMin, meshMax [2]float64, radius *float64,
	origin *[2]float64, probeMethod string) error {
	xCnt := meshConfig.xCount
	yCnt := meshConfig.yCount
	minX, minY := meshMin[0], meshMin[1]
	maxX, maxY := meshMax[0], meshMax[1]
	xDist := (maxX - minX) / float64(xCnt-1)
	yDist := (maxY - minY) / float64(yCnt-1)
	// floor distances down to next hundredth
	xDist = math.Floor(xDist*100) / 100
	yDist = math.Floor(yDist*100) / 100
	if xDist < 1. || yDist < 1. {
		return meshError("bed_mesh: min/max points too close together")
	}

	if radius != nil {
		// round bed, min/max needs to be recalculated
		yDist = xDist
		newR := float64(xCnt/2) * xDist
		minX, minY = -newR, -newR
		maxX, maxY = newR, newR
	} else {
		// rectangular bed, only re-calc max_x
		maxX = minX + xDist*float64(xCnt-1)
	}
	posY := minY
	var points [][2]float64
	for i := 0; i < yCnt; i++ {
		for j := 0; j < xCnt; j++ {
			var posX float64
			if i%2 == 0 {
				// move in positive direction
				posX = minX + float64(j)*xDist
			} else {
				// move in negative direction
				posX = maxX - float64(j)*xDist
			}
			if radius == nil {
				// rectangular bed, append
				points = append(points, [2]float64{posX, posY})
			} else {
				// round bed, check distance from origin
				distFromOrigin := math.Sqrt(posX*posX + posY*posY)
				if distFromOrigin <= *radius {
					points = append(points, [2]float64{origin[0] + posX, origin[1] + posY})
				}
			}
		}
		posY += yDist
	}
	if m.zeroRefPos == nil || probeMethod == "manual" {
		// Zero Reference Disabled
		m.zrefMode = ZrefDisabled
	} else if within(*m.zeroRefPos, meshMin, meshMax, 0) {
		// Zero Reference position within mesh
		m.zrefMode = ZrefInMesh
	} else {
		// Zero Reference position outside of mesh
		m.zrefMode = ZrefProbe
	}
	m.basePoints = points
	m.substitutes.clear()
	// adjust overshoot
	ogMinX := m.orig.meshMin[0]
	ogMaxX := m.orig.meshMax[0]
	addOvs := math.Min(math.Max(0, minX-ogMinX), math.Max(0, ogMaxX-maxX))
	m.overshoot = m.cfgOvershoot + math.Floor(addOvs)
	minPt, maxPt := [2]float64{minX, minY}, [2]float64{maxX, maxY}
	if err := m.processFaultyRegions(minPt, maxPt, radius); err != nil {
		return err
	}
	path := m.stdPath()
	pts := make([][]float64, len(path))
	for i, p := range path {
		pts[i] = []float64{p[0], p[1]}
	}
	m.probeHelper.UpdateProbePoints(pts, 3)
	return nil
}

func (m *ProbeManager) processFaultyRegions(minPt, maxPt [2]float64, radius *float64) error {
	if len(m.faultyRegions) == 0 {
		return nil
	}
	// Cannot probe a reference within a faulty region
	if m.zrefMode == ZrefProbe {
		for _, fr := range m.faultyRegions {
			if within(*m.zeroRefPos, fr.c1, fr.c3, 0) {
				opt := "zero_reference_position"
				return meshError(fmt.Sprintf("bed_mesh: Cannot probe zero reference position at "+
					"(%.2f, %.2f) as it is located within a faulty region."+
					" Check the value for option '%s'", m.zeroRefPos[0], m.zeroRefPos[1], opt))
			}
		}
	}
	// Check to see if any points fall within faulty regions
	lastY := m.basePoints[0][1]
	isReversed := false
	for i, coord := range m.basePoints {
		if !isclose(coord[1], lastY, 1e-09, 0) {
			isReversed = !isReversed
		}
		lastY = coord[1]
		var adjCoords [][2]float64
		for _, fr := range m.faultyRegions {
			minC, maxC := fr.c1, fr.c3
			if within(coord, minC, maxC, .00001) {
				// Point lies within a faulty region
				adjCoords = [][2]float64{
					{minC[0], coord[1]}, {coord[0], minC[1]},
					{coord[0], maxC[1]}, {maxC[0], coord[1]}}
				if isReversed {
					// Swap first and last points for zig-zag pattern
					adjCoords[0], adjCoords[len(adjCoords)-1] = adjCoords[len(adjCoords)-1], adjCoords[0]
				}
				break
			}
		}
		if len(adjCoords) == 0 {
			// coord is not located within a faulty region
			continue
		}
		var validCoords [][2]float64
		for _, ac := range adjCoords {
			// make sure that coordinates are within the mesh boundary
			if radius == nil {
				if within(ac, minPt, maxPt, .000001) {
					validCoords = append(validCoords, ac)
				}
			} else {
				distFromOrigin := math.Sqrt(ac[0]*ac[0] + ac[1]*ac[1])
				if distFromOrigin <= *radius {
					validCoords = append(validCoords, ac)
				}
			}
		}
		if len(validCoords) == 0 {
			return meshError(fmt.Sprintf("bed_mesh: Unable to generate coordinates"+
				" for faulty region at index: %d", i))
		}
		m.substitutes.set(i, validCoords)
	}
	return nil
}

func (m *ProbeManager) stdPath() [][2]float64 {
	var path [][2]float64
	for idx, pt := range m.basePoints {
		if m.substitutes.has(idx) {
			path = append(path, m.substitutes.m[idx]...)
		} else {
			path = append(path, pt)
		}
	}
	if m.zrefMode == ZrefProbe {
		path = append(path, *m.zeroRefPos)
	}
	return path
}

func (m *ProbeManager) iterRapidPath() []rapidPoint {
	var out []rapidPoint
	ascndX := true
	lastBasePt := m.basePoints[0]
	lastMvPt := lastBasePt
	// Generate initial move point
	if m.overshoot != 0 {
		overshoot := math.Min(8, m.overshoot)
		lastMvPt = [2]float64{lastBasePt[0] - overshoot, lastBasePt[1]}
		out = append(out, rapidPoint{lastMvPt, false})
	}
	for idx, pt := range m.basePoints {
		// increasing Y indicates direction change
		dirChange := !isclose(pt[1], lastBasePt[1], 1e-09, 1e-6)
		if m.substitutes.has(idx) {
			for _, sp := range m.genFaultyPath(lastMvPt, idx, ascndX, dirChange) {
				out = append(out, sp)
				lastMvPt = sp.pt
			}
		} else {
			if dirChange {
				for _, dpt := range m.genDirChange(lastMvPt, pt, ascndX) {
					out = append(out, rapidPoint{dpt, false})
				}
			}
			out = append(out, rapidPoint{pt, true})
			lastMvPt = pt
		}
		lastBasePt = pt
		ascndX = ascndX != dirChange
	}
	if m.zrefMode == ZrefProbe {
		if m.overshoot != 0 {
			ovs := math.Min(4, m.overshoot)
			if !ascndX {
				ovs = -ovs
			}
			out = append(out, rapidPoint{[2]float64{lastMvPt[0] + ovs, lastMvPt[1]}, false})
		}
		out = append(out, rapidPoint{*m.zeroRefPos, true})
	}
	return out
}

func (m *ProbeManager) genFaultyPath(lastPt [2]float64, idx int, ascndX, dirChange bool) []rapidPoint {
	var out []rapidPoint
	yield := func(p [2]float64, isSmp bool) { out = append(out, rapidPoint{p, isSmp}) }
	subs := m.substitutes.m[idx]
	subCnt := len(subs)
	if dirChange {
		for _, dpt := range m.genDirChange(lastPt, subs[0], ascndX) {
			yield(dpt, false)
		}
	}
	if m.isRound {
		// No faulty region path handling for round beds
		for _, pt := range subs {
			yield(pt, true)
		}
		return out
	}
	// Check to see if this is the first corner
	firstCorner := false
	sortedSubIdx := append([]int(nil), m.substitutes.keys...)
	sort.Ints(sortedSubIdx)
	if subCnt == 2 && idx < len(sortedSubIdx) {
		firstCorner = sortedSubIdx[idx] == idx
	}
	yield(subs[0], true)
	if subCnt == 1 {
		return out
	}
	lastPt, nextPt := subs[0], subs[1]
	if subCnt == 2 {
		if firstCorner || dirChange {
			// horizontal move first
			yield([2]float64{nextPt[0], lastPt[1]}, false)
		} else {
			yield([2]float64{lastPt[0], nextPt[1]}, false)
		}
		yield(nextPt, true)
	} else if subCnt >= 3 {
		if dirChange {
			// first move should be a vertical switch up.  If overshoot
			// is available, simulate another direction change.  Otherwise
			// move inward 2 mm, then up through the faulty region.
			if m.overshoot != 0 {
				for _, dpt := range m.genDirChange(lastPt, nextPt, ascndX) {
					yield(dpt, false)
				}
			} else {
				shift := 2.
				if ascndX {
					shift = -2
				}
				yield([2]float64{lastPt[0] + shift, lastPt[1]}, false)
				yield([2]float64{lastPt[0] + shift, nextPt[1]}, false)
			}
			yield(nextPt, true)
			lastPt, nextPt = subs[1], subs[2]
		} else {
			// vertical move
			yield([2]float64{lastPt[0], nextPt[1]}, false)
			yield(nextPt, true)
			lastPt, nextPt = subs[1], subs[2]
			if subCnt == 4 {
				// Vertical switch up within faulty region
				shift := -2.
				if ascndX {
					shift = 2
				}
				yield([2]float64{lastPt[0] + shift, lastPt[1]}, false)
				yield([2]float64{nextPt[0] - shift, nextPt[1]}, false)
				yield(nextPt, true)
				lastPt, nextPt = subs[2], subs[3]
			}
		}
		// horizontal move before final point
		yield([2]float64{nextPt[0], lastPt[1]}, false)
		yield(nextPt, true)
	}
	return out
}

// pyMod is Python's % for integers: the result has the sign of the divisor.
func pyMod(a, b int) int {
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r
}

// pyFloorDiv is Python's // for integers.
func pyFloorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func (m *ProbeManager) genDirChange(lastPt, nextPt [2]float64, ascndX bool) [][2]float64 {
	if m.overshoot == 0 {
		return nil
	}
	var out [][2]float64
	// overshoot X beyond the outer point
	xdir := -1
	if ascndX {
		xdir = 1
	}
	overshoot := m.overshoot
	if m.overshoot >= 3. {
		overshoot = 2.
	}
	ovrPt := [2]float64{lastPt[0] + overshoot*float64(xdir), lastPt[1]}
	out = append(out, ovrPt)
	if m.overshoot < 3. {
		// No room to generate an arc, move up to next y
		out = append(out, [2]float64{nextPt[0] + overshoot*float64(xdir), nextPt[1]})
		return out
	}
	// generate arc
	const stepAngle = 3
	const startAngle = 270
	ydiff := math.Abs(nextPt[1] - lastPt[1])
	xdiff := math.Abs(nextPt[0] - lastPt[0])
	maxRadius := math.Min(m.overshoot-2, 8)
	radius := math.Min(ydiff/2, maxRadius)
	origin := [2]float64{ovrPt[0], lastPt[1] + radius}
	nextOriginY := nextPt[1] - radius
	// determine angle
	var angle int
	if xdiff < .01 {
		// Move is aligned on the x-axis
		angle = 90
		if nextOriginY-origin[1] < .05 {
			// The move can be completed in a single arc
			angle = 180
		}
	} else {
		angle = int(math.Atan(ydiff/xdiff) * (180. / math.Pi))
		if (ascndX && nextPt[0] < lastPt[0]) || (!ascndX && nextPt[0] > lastPt[0]) {
			angle = 180 - angle
		}
	}
	count := pyFloorDiv(angle, stepAngle)
	// Gen first arc
	step := stepAngle * xdir
	start := startAngle + step
	out = append(out, genArc(origin, radius, start, step, count)...)
	if angle == 180 {
		// arc complete
		return out
	}
	// generate next arc
	origin = [2]float64{nextPt[0] + overshoot*float64(xdir), nextOriginY}
	// start at the angle where the last arc finished
	start = startAngle + count*step
	// recalculate the count to make sure we generate a full 180
	// degrees.  Add a step for the repeated connecting angle
	count = 61 - count
	out = append(out, genArc(origin, radius, start, step, count)...)
	return out
}

func genArc(origin [2]float64, radius float64, start, step, count int) [][2]float64 {
	var out [][2]float64
	end := start + step*count
	// create a segent for every 3 degrees of travel
	for angle := start; (step > 0 && angle < end) || (step < 0 && angle > end); angle += step {
		rad := float64(pyMod(angle, 360)) * (math.Pi / 180.)
		opp := math.Sin(rad) * radius
		adj := math.Cos(rad) * radius
		out = append(out, [2]float64{origin[0] + adj, origin[1] + opp})
	}
	return out
}

// ######################################################################
// Rapid scan
// ######################################################################

const (
	MaxHitDist = 2.
	MMWinSpeed = 125
)

type scanProbe interface {
	ProbeParams(gcmd *gcode.Command) probe.Params
	Offsets(gcmd *gcode.Command) [3]float64
	StartProbeSession(gcmd *gcode.Command) homing.ProbeSession
}

// RapidScanHelper scans the bed with a probe that reads while it moves.
type RapidScanHelper struct {
	printer          *host.Printer
	probeManager     *ProbeManager
	speed            float64
	scanHeight       float64
	finalizeCallback func([][3]float64)
}

func newRapidScanHelper(config *configfile.Wrapper, probeMgr *ProbeManager,
	finalize func([][3]float64)) *RapidScanHelper {
	return &RapidScanHelper{printer: host.Of(config), probeManager: probeMgr,
		speed:            config.GetFloat("speed", configfile.Default(50.), configfile.Above(0.)),
		scanHeight:       config.GetFloat("horizontal_move_z", configfile.Default(5.)),
		finalizeCallback: finalize}
}

func (r *RapidScanHelper) performRapidScan(gcmd *gcode.Command) {
	speed := gcmd.GetFloat("SCAN_SPEED", gcode.Default(r.speed))
	scanHeight := gcmd.GetFloat("HORIZONTAL_MOVE_Z", gcode.Default(r.scanHeight))
	gcmd.RespondInfo(fmt.Sprintf("Beginning rapid surface scan at height %.2f...", scanHeight), true)
	pprobe := r.printer.LookupObject("probe").(scanProbe)
	th := toolhead.Of(r.printer)
	// Calculate time window around which a sample is valid.  Current
	// assumption is anything within 2mm is usable, so:
	// window = 2 / max_speed
	//
	// TODO: validate maximum speed allowed based on sample rate of probe
	// Scale the hit distance window for speeds lower than 125mm/s.  The
	// lower the speed the less the window shrinks.
	scale := math.Max(0, 1-speed/MMWinSpeed) + 1
	hitDist := math.Min(MaxHitDist, scale*speed/MMWinSpeed)
	halfWindow := hitDist / speed
	gcmd.RespondInfo(fmt.Sprintf("Sample hit distance +/- %.4fmm, time window +/- ms %.4f",
		hitDist, halfWindow*1000), true)
	gcmd.Params()["SAMPLE_TIME"] = pyfmt.Float(halfWindow * 2)
	r.raiseTool(gcmd, scanHeight)
	probeSession := pprobe.StartProbeSession(gcmd)
	offsets := pprobe.Offsets(gcmd)
	initialMove := true
	for _, rp := range r.probeManager.iterRapidPath() {
		pos := []float64{rp.pt[0] - offsets[0], rp.pt[1] - offsets[1]}
		th.ManualMove(pos, speed)
		if initialMove {
			initialMove = false
			r.moveToScanHeight(gcmd, scanHeight)
		}
		if rp.isProbe {
			probeSession.RunProbe(gcmd)
		}
	}
	results := probeSession.PullProbedResults()
	th.LastMoveTime()
	pts := make([]*manual_probe.ProbeResult, len(results))
	for i, res := range results {
		pts[i] = res.(*manual_probe.ProbeResult)
	}
	r.finalizeCallback(resultPoints(pts))
	probeSession.EndProbeSession()
}

func (r *RapidScanHelper) raiseTool(gcmd *gcode.Command, scanHeight float64) {
	// If the nozzle is below scan height raise the tool
	th := toolhead.Of(r.printer)
	pprobe := r.printer.LookupObject("probe").(scanProbe)
	curPos := th.Position()
	if curPos[2] >= scanHeight {
		return
	}
	pparams := pprobe.ProbeParams(gcmd)
	liftSpeed := pparams.LiftSpeed
	curPos[2] = r.scanHeight + .5
	th.ManualMove(curPos, liftSpeed)
}

func (r *RapidScanHelper) moveToScanHeight(gcmd *gcode.Command, scanHeight float64) {
	timeWindow := gcmd.GetFloat("SAMPLE_TIME")
	th := toolhead.Of(r.printer)
	pprobe := r.printer.LookupObject("probe").(scanProbe)
	curPos := th.Position()
	pparams := pprobe.ProbeParams(gcmd)
	liftSpeed := pparams.LiftSpeed
	probeSpeed := pparams.ProbeSpeed
	curPos[2] = scanHeight + .5
	th.ManualMove(curPos, liftSpeed)
	curPos[2] = scanHeight
	th.ManualMove(curPos, probeSpeed)
	th.Dwell(timeWindow/2 + .01)
}
