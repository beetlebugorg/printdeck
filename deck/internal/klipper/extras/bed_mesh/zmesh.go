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
	"strings"
	"unsafe"

	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/mathutil"
)

// MeshParams are the parameters of a mesh, in the order of their keys (an OrderedDict).
type MeshParams = jinja.Dict

func paramInt(p *MeshParams, key string) int {
	v, _ := p.Get(key)
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func paramFloat(p *MeshParams, key string) float64 {
	v, _ := p.Get(key)
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

func paramStr(p *MeshParams, key string) string {
	v, _ := p.Get(key)
	s, _ := v.(string)
	return s
}

func copyParams(p *MeshParams) *MeshParams {
	out := jinja.NewDict()
	for _, k := range p.Keys() {
		v, _ := p.Get(k)
		out.Set(k, v)
	}
	return out
}

// pyStr is Python's str() of a parameter value.
func pyStr(v any) string { return jinja.Str(v) }

// ######################################################################
// Move splitting
// ######################################################################

// MoveSplitter splits a move into short moves that follow the mesh.
type MoveSplitter struct {
	splitDeltaZ       float64
	moveCheckDistance float64
	zMesh             *ZMesh
	fadeOffset        float64
	gcode             *gcode.Dispatch

	prevPos, nextPos, currentPos []float64
	zFactor                      float64
	zOffset                      float64
	traverseComplete             bool
	distanceChecked              float64
	totalMoveLength              float64
	axisMove                     []bool
}

func (s *MoveSplitter) initialize(mesh *ZMesh, fadeOffset float64) {
	s.zMesh = mesh
	s.fadeOffset = fadeOffset
}

func (s *MoveSplitter) buildMove(prevPos, nextPos []float64, factor float64) {
	s.prevPos = append([]float64(nil), prevPos...)
	s.nextPos = append([]float64(nil), nextPos...)
	s.currentPos = append([]float64(nil), prevPos...)
	s.zFactor = factor
	s.zOffset = s.calcZOffset(prevPos)
	s.traverseComplete = false
	s.distanceChecked = 0.
	n := min(len(s.nextPos), len(s.prevPos))
	axesD := make([]float64, n)
	for i := range axesD {
		axesD[i] = s.nextPos[i] - s.prevPos[i]
	}
	var sq []float64
	for _, d := range axesD[:min(3, n)] {
		sq = append(sq, d*d)
	}
	s.totalMoveLength = math.Sqrt(mathutil.Sum(sq...))
	s.axisMove = make([]bool, n)
	for i, d := range axesD {
		s.axisMove[i] = !isclose(d, 0., 1e-09, 1e-10)
	}
}

func (s *MoveSplitter) calcZOffset(pos []float64) float64 {
	z := s.zMesh.CalcZ(pos[0], pos[1])
	offset := s.fadeOffset
	return s.zFactor*(z-offset) + offset
}

func (s *MoveSplitter) setNextMove(distanceFromPrev float64) {
	t := distanceFromPrev / s.totalMoveLength
	if t > 1. || t < 0. {
		panic(gcode.Errorf("bed_mesh: Slice distance is negative " +
			"or greater than entire move length"))
	}
	for i := range s.nextPos {
		if s.axisMove[i] {
			s.currentPos[i] = lerp(t, s.prevPos[i], s.nextPos[i])
		}
	}
}

// split returns the next move, or nil when the move is complete.
func (s *MoveSplitter) split() []float64 {
	if !s.traverseComplete {
		if s.axisMove[0] || s.axisMove[1] {
			// X and/or Y axis move, traverse if necessary
			for s.distanceChecked+s.moveCheckDistance < s.totalMoveLength {
				s.distanceChecked += s.moveCheckDistance
				s.setNextMove(s.distanceChecked)
				nextZ := s.calcZOffset(s.currentPos)
				if math.Abs(nextZ-s.zOffset) >= s.splitDeltaZ {
					s.zOffset = nextZ
					newpos := append([]float64(nil), s.currentPos...)
					newpos[2] += s.zOffset
					return newpos
				}
			}
		}
		// end of move reached
		s.currentPos = append([]float64(nil), s.nextPos...)
		s.zOffset = s.calcZOffset(s.currentPos)
		// Its okay to add Z-Offset to the final move, since it will not be
		// used again.
		s.currentPos[2] += s.zOffset
		s.traverseComplete = true
		return s.currentPos
	}
	// Traverse complete
	return nil
}

// ######################################################################
// Z mesh
// ######################################################################

// ZMesh is a mesh: the probed points and the interpolated grid.
type ZMesh struct {
	profileName              string
	probedMatrix, meshMatrix [][]float64
	meshParams               *MeshParams
	meshOffsets              [2]float64
	meshXMin, meshXMax       float64
	meshYMin, meshYMax       float64
	sample                   func(zMatrix [][]float64) error
	meshXCount, meshYCount   int
	xMult, yMult             int
	meshXDist, meshYDist     float64
}

// NewZMesh makes a mesh of the parameters. name "" is an adaptive mesh.
func NewZMesh(params *MeshParams, name string) *ZMesh {
	m := &ZMesh{meshParams: params}
	m.profileName = name
	if name == "" {
		m.profileName = fmt.Sprintf("adaptive-%X", uintptr(unsafe.Pointer(m)))
	}
	m.meshXMin = paramFloat(params, "min_x")
	m.meshXMax = paramFloat(params, "max_x")
	m.meshYMin = paramFloat(params, "min_y")
	m.meshYMax = paramFloat(params, "max_y")
	// Set the interpolation algorithm
	switch paramStr(params, "algo") {
	case "lagrange":
		m.sample = m.sampleLagrange
	case "bicubic":
		m.sample = m.sampleBicubic
	case "direct":
		m.sample = m.sampleDirect
	}
	// Number of points to interpolate per segment
	meshXPPS := paramInt(params, "mesh_x_pps")
	meshYPPS := paramInt(params, "mesh_y_pps")
	pxCnt := paramInt(params, "x_count")
	pyCnt := paramInt(params, "y_count")
	m.meshXCount = (pxCnt-1)*meshXPPS + pxCnt
	m.meshYCount = (pyCnt-1)*meshYPPS + pyCnt
	m.xMult = meshXPPS + 1
	m.yMult = meshYPPS + 1
	m.meshXDist = (m.meshXMax - m.meshXMin) / float64(m.meshXCount-1)
	m.meshYDist = (m.meshYMax - m.meshYMin) / float64(m.meshYCount-1)
	return m
}

func roundMatrix(matrix [][]float64) [][]float64 {
	if matrix == nil {
		return [][]float64{{}}
	}
	out := make([][]float64, len(matrix))
	for i, line := range matrix {
		out[i] = make([]float64, len(line))
		for j, z := range line {
			out[i][j] = mathutil.Round(z, 6)
		}
	}
	return out
}

func (m *ZMesh) MeshMatrix() [][]float64   { return roundMatrix(m.meshMatrix) }
func (m *ZMesh) ProbedMatrix() [][]float64 { return roundMatrix(m.probedMatrix) }
func (m *ZMesh) MeshParams() *MeshParams   { return m.meshParams }
func (m *ZMesh) ProfileName() string       { return m.profileName }

func (m *ZMesh) printProbedMatrix(printFunc func(string)) {
	if m.probedMatrix != nil {
		var msg strings.Builder
		msg.WriteString("Mesh Leveling Probed Z positions:\n")
		for _, line := range m.probedMatrix {
			for _, x := range line {
				fmt.Fprintf(&msg, " %f", x)
			}
			msg.WriteString("\n")
		}
		printFunc(msg.String())
	} else {
		printFunc("bed_mesh: bed has not been probed")
	}
}

// printMesh prints the mesh; moveZ is NaN for none.
func (m *ZMesh) printMesh(printFunc func(string), moveZ float64) {
	matrix := m.MeshMatrix()
	var msg strings.Builder
	fmt.Fprintf(&msg, "Mesh X,Y: %d,%d\n", m.meshXCount, m.meshYCount)
	if !math.IsNaN(moveZ) {
		fmt.Fprintf(&msg, "Search Height: %d\n", int64(moveZ))
	}
	fmt.Fprintf(&msg, "Mesh Offsets: X=%.4f, Y=%.4f\n", m.meshOffsets[0], m.meshOffsets[1])
	fmt.Fprintf(&msg, "Mesh Average: %.2f\n", m.ZAverage())
	lo, hi := m.ZRange()
	fmt.Fprintf(&msg, "Mesh Range: min=%.4f max=%.4f\n", lo, hi)
	fmt.Fprintf(&msg, "Interpolation Algorithm: %s\n", paramStr(m.meshParams, "algo"))
	msg.WriteString("Measured points:\n")
	for yLine := m.meshYCount - 1; yLine >= 0; yLine-- {
		for _, z := range matrix[yLine] {
			fmt.Fprintf(&msg, "  %f", z)
		}
		msg.WriteString("\n")
	}
	printFunc(msg.String())
}

// buildMesh interpolates the mesh of the probed points.
func (m *ZMesh) buildMesh(zMatrix [][]float64) error {
	m.probedMatrix = zMatrix
	return m.sample(zMatrix)
}

func (m *ZMesh) setZeroReference(xpos, ypos float64) {
	offset := m.CalcZ(xpos, ypos)
	logf("bed_mesh: setting zero reference at (%.2f, %.2f, %.6f)", xpos, ypos, offset)
	for _, matrix := range [][][]float64{m.probedMatrix, m.meshMatrix} {
		for yidx := range matrix {
			for xidx := range matrix[yidx] {
				matrix[yidx][xidx] -= offset
			}
		}
	}
}

// setMeshOffsets sets the offsets; NaN keeps an offset.
func (m *ZMesh) setMeshOffsets(offsets [2]float64) {
	for i, o := range offsets {
		if !math.IsNaN(o) {
			m.meshOffsets[i] = o
		}
	}
}

func (m *ZMesh) xCoordinate(index int) float64 { return m.meshXMin + m.meshXDist*float64(index) }
func (m *ZMesh) yCoordinate(index int) float64 { return m.meshYMin + m.meshYDist*float64(index) }

// CalcZ returns the Z adjustment at a position.
func (m *ZMesh) CalcZ(x, y float64) float64 {
	if m.meshMatrix != nil {
		tbl := m.meshMatrix
		tx, xidx := m.linearIndex(x+m.meshOffsets[0], 0)
		ty, yidx := m.linearIndex(y+m.meshOffsets[1], 1)
		z0 := lerp(tx, tbl[yidx][xidx], tbl[yidx][xidx+1])
		z1 := lerp(tx, tbl[yidx+1][xidx], tbl[yidx+1][xidx+1])
		return lerp(ty, z0, z1)
	}
	// No mesh table generated, no z-adjustment
	return 0.
}

// ZRange returns the minimum and the maximum of the mesh.
func (m *ZMesh) ZRange() (float64, float64) {
	if m.meshMatrix != nil {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, row := range m.meshMatrix {
			for _, z := range row {
				lo = math.Min(lo, z)
				hi = math.Max(hi, z)
			}
		}
		return lo, hi
	}
	return 0., 0.
}

// ZAverage returns the average of the mesh, rounded to the nearest 100th.
func (m *ZMesh) ZAverage() float64 {
	if m.meshMatrix != nil {
		var rowSums []float64
		count := 0
		for _, row := range m.meshMatrix {
			rowSums = append(rowSums, mathutil.Sum(row...))
			count += len(row)
		}
		total := mathutil.Sum(rowSums...)
		// Round average to the nearest 100th.  This
		// should produce an offset that is divisible by common
		// z step distances
		return mathutil.Round(total/float64(count), 2)
	}
	return 0.
}

func (m *ZMesh) linearIndex(coord float64, axis int) (float64, int) {
	var meshMin, meshDist float64
	var meshCnt int
	var cfunc func(int) float64
	if axis == 0 {
		// X-axis
		meshMin, meshCnt, meshDist, cfunc = m.meshXMin, m.meshXCount, m.meshXDist, m.xCoordinate
	} else {
		// Y-axis
		meshMin, meshCnt, meshDist, cfunc = m.meshYMin, m.meshYCount, m.meshYDist, m.yCoordinate
	}
	idx := int(math.Floor((coord - meshMin) / meshDist))
	idx = max(0, min(meshCnt-2, idx))
	t := (coord - cfunc(idx)) / meshDist
	return constrain(t, 0., 1.), idx
}

func (m *ZMesh) sampleDirect(zMatrix [][]float64) error {
	m.meshMatrix = make([][]float64, len(zMatrix))
	for i, y := range zMatrix {
		m.meshMatrix[i] = append([]float64(nil), y...)
	}
	return nil
}

func (m *ZMesh) initMatrix(zMatrix [][]float64) {
	m.meshMatrix = make([][]float64, m.meshYCount)
	for j := range m.meshMatrix {
		m.meshMatrix[j] = make([]float64, m.meshXCount)
		for i := range m.meshMatrix[j] {
			if i%m.xMult == 0 && j%m.yMult == 0 {
				m.meshMatrix[j][i] = zMatrix[j/m.yMult][i/m.xMult]
			}
		}
	}
}

func (m *ZMesh) sampleLagrange(zMatrix [][]float64) error {
	xMult, yMult := m.xMult, m.yMult
	m.initMatrix(zMatrix)
	xpts, ypts := m.lagrangeCoords()
	// Interpolate X coordinates
	for i := 0; i < m.meshYCount; i++ {
		// only interpolate X-rows that have probed coordinates
		if i%yMult != 0 {
			continue
		}
		for j := 0; j < m.meshXCount; j++ {
			if j%xMult == 0 {
				continue
			}
			x := m.xCoordinate(j)
			m.meshMatrix[i][j] = m.calcLagrange(xpts, x, i, 0)
		}
	}
	// Interpolate Y coordinates
	for i := 0; i < m.meshXCount; i++ {
		for j := 0; j < m.meshYCount; j++ {
			if j%yMult == 0 {
				continue
			}
			y := m.yCoordinate(j)
			m.meshMatrix[j][i] = m.calcLagrange(ypts, y, i, 1)
		}
	}
	return nil
}

func (m *ZMesh) lagrangeCoords() ([]float64, []float64) {
	var xpts, ypts []float64
	for i := 0; i < paramInt(m.meshParams, "x_count"); i++ {
		xpts = append(xpts, m.xCoordinate(i*m.xMult))
	}
	for j := 0; j < paramInt(m.meshParams, "y_count"); j++ {
		ypts = append(ypts, m.yCoordinate(j*m.yMult))
	}
	return xpts, ypts
}

func (m *ZMesh) calcLagrange(lpts []float64, c float64, vec, axis int) float64 {
	ptCnt := len(lpts)
	total := 0.
	for i := 0; i < ptCnt; i++ {
		n := 1.
		d := 1.
		for j := 0; j < ptCnt; j++ {
			if j == i {
				continue
			}
			n *= c - lpts[j]
			d *= lpts[i] - lpts[j]
		}
		var z float64
		if axis == 0 {
			// Calc X-Axis
			z = m.meshMatrix[vec][i*m.xMult]
		} else {
			// Calc Y-Axis
			z = m.meshMatrix[i*m.yMult][vec]
		}
		total += z * n / d
	}
	return total
}

func (m *ZMesh) sampleBicubic(zMatrix [][]float64) error {
	// should work for any number of probe points above 3x3
	xMult, yMult := m.xMult, m.yMult
	c := paramFloat(m.meshParams, "tension")
	m.initMatrix(zMatrix)
	// Interpolate X values
	for y := 0; y < m.meshYCount; y++ {
		if y%yMult != 0 {
			continue
		}
		for x := 0; x < m.meshXCount; x++ {
			if x%xMult == 0 {
				continue
			}
			pts, err := m.xCtlPts(x, y)
			if err != nil {
				return err
			}
			m.meshMatrix[y][x] = cardinalSpline(pts, c)
		}
	}
	// Interpolate Y values
	for x := 0; x < m.meshXCount; x++ {
		for y := 0; y < m.meshYCount; y++ {
			if y%yMult == 0 {
				continue
			}
			pts, err := m.yCtlPts(x, y)
			if err != nil {
				return err
			}
			m.meshMatrix[y][x] = cardinalSpline(pts, c)
		}
	}
	return nil
}

// xCtlPts fetches the control points and t for a X value in the mesh.
func (m *ZMesh) xCtlPts(x, y int) ([5]float64, error) {
	xMult := m.xMult
	xRow := m.meshMatrix[y]
	lastPt := m.meshXCount - 1 - xMult
	switch {
	case x < xMult:
		return [5]float64{xRow[0], xRow[0], xRow[xMult], xRow[2*xMult], float64(x) / float64(xMult)}, nil
	case x > lastPt:
		return [5]float64{xRow[lastPt-xMult], xRow[lastPt], xRow[lastPt+xMult], xRow[lastPt+xMult],
			float64(x-lastPt) / float64(xMult)}, nil
	}
	for i := xMult; i < lastPt; i += xMult {
		if x > i && x < i+xMult {
			return [5]float64{xRow[i-xMult], xRow[i], xRow[i+xMult], xRow[i+2*xMult],
				float64(x-i) / float64(xMult)}, nil
		}
	}
	return [5]float64{}, meshError("bed_mesh: Error finding x control points")
}

// yCtlPts fetches the control points and t for a Y value in the mesh.
func (m *ZMesh) yCtlPts(x, y int) ([5]float64, error) {
	yMult := m.yMult
	lastPt := m.meshYCount - 1 - yMult
	yCol := m.meshMatrix
	switch {
	case y < yMult:
		return [5]float64{yCol[0][x], yCol[0][x], yCol[yMult][x], yCol[2*yMult][x],
			float64(y) / float64(yMult)}, nil
	case y > lastPt:
		return [5]float64{yCol[lastPt-yMult][x], yCol[lastPt][x], yCol[lastPt+yMult][x],
			yCol[lastPt+yMult][x], float64(y-lastPt) / float64(yMult)}, nil
	}
	for i := yMult; i < lastPt; i += yMult {
		if y > i && y < i+yMult {
			return [5]float64{yCol[i-yMult][x], yCol[i][x], yCol[i+yMult][x], yCol[i+2*yMult][x],
				float64(y-i) / float64(yMult)}, nil
		}
	}
	return [5]float64{}, meshError("bed_mesh: Error finding y control points")
}

func cardinalSpline(p [5]float64, tension float64) float64 {
	t := p[4]
	t2 := t * t
	t3 := t2 * t
	m1 := tension * (p[2] - p[0])
	m2 := tension * (p[3] - p[1])
	a := p[1] * (2*t3 - 3*t2 + 1)
	b := p[2] * (-2*t3 + 3*t2)
	c := m1 * (t3 - 2*t2 + t)
	d := m2 * (t3 - t2)
	return a + b + c + d
}
