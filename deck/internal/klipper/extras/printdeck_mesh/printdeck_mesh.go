// Package printdeck_mesh makes the bed mesh at the start of a print. It is a Printdeck module:
// Klipper has no such module. The config section is [printdeck_mesh].
//
//	PRINTDECK_MESH [METHOD=map|adaptive|grid]
//
// makes the mesh with the method of the deck setting (PRINTDECK_MESH_METHOD), or with METHOD:
//
//   - map: the saved map of the last full probe, corrected with a few probe points. The shape of a
//     bed stays almost the same from print to print. Mostly its height and its tilt change. The
//     module probes 3 points and fits a plane to the differences from the saved map. Then it probes
//     the point that is the farthest from the probed points, and checks it against the fit. It stops
//     when the last checks check points and all the points are within tolerance of the fit. Else it probes the
//     next point, up to max_points, and then it falls back to the grid. The new mesh is the saved
//     map plus the plane: it has the points of the full probe. A bed changes shape when it heats,
//     so the module uses the map only when the bed is within max_temp_diff (10 °C) of the bed of
//     the map. Else it falls back.
//   - adaptive: BED_MESH_CALIBRATE ADAPTIVE=1, a grid under the objects of the print only.
//   - grid: BED_MESH_CALIBRATE with grid_count points, on all the bed.
//
// With extend: True, the mesh gets one more row and column on each side (see extend).
//
// The map and adaptive methods probe only under the objects of the print, when the file defines
// them (EXCLUDE_OBJECT_DEFINE). The deck adds the definitions to the files from Cura.
//
// Each BED_MESH_CALIBRATE with min_base_points or more points (the Probe key of the Bed mesh page
// of the deck) becomes the saved map. The saved map and the method are in state_file.
//
//	PRINTDECK_MESH_METHOD METHOD=map|adaptive|grid
//
// sets the method, and keeps it in state_file.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_mesh

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bed_mesh"
	"printdeck/internal/klipper/extras/manual_probe"
	"printdeck/internal/klipper/extras/probe"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/jinja"
	"printdeck/internal/klipper/toolhead"
)

// Methods are the names of the methods, in the order of the deck setting.
var Methods = []string{"map", "adaptive", "grid"}

// Base is the saved map: the parameters and the probed points of a full probe.
type Base struct {
	Time time.Time `json:"time"`
	// BedTemp is the temperature of the bed at the probe, in °C. A bed changes shape when it heats,
	// so the map is good only near this temperature. 0 is not known (a map of an older version).
	BedTemp float64          `json:"bed_temp,omitempty"`
	Params  []bed_mesh.Param `json:"params"`
	Probed  [][]float64      `json:"probed"`
}

type state struct {
	Method string `json:"method"`
	Base   *Base  `json:"base,omitempty"`
}

// PrintdeckMesh is the [printdeck_mesh] section.
type PrintdeckMesh struct {
	printer *host.Printer
	gcode   *gcode.Dispatch
	path    string
	st      state

	gridCount     [2]int
	gridAlgorithm string
	tolerance     float64 // mm
	checks        int     // check points in a row that must fit
	maxPoints     int
	minBase       int     // the points of a mesh that becomes the saved map
	margin        float64 // mm around the objects
	minSide       float64 // mm: the smallest side of the probed area
	moveZ, speed  float64
	maxTempDiff   float64 // °C between the bed now and the bed of the saved map
	extendEdges   bool    // extend the mesh past the probed area (see extend)

	last map[string]any // the result of the last PRINTDECK_MESH
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	p := &PrintdeckMesh{printer: printer, gcode: gcode.Of(printer), last: map[string]any{}}
	method := config.Get("method", configfile.Default("map"))
	if !slices.Contains(Methods, method) {
		configfile.Raise("printdeck_mesh: method must be one of %s", strings.Join(Methods, ", "))
	}
	counts := config.GetIntList("grid_count", ",", 2, configfile.Default("3,3"))
	p.gridCount = [2]int{counts[0], counts[1]}
	p.gridAlgorithm = config.Get("grid_algorithm", configfile.Default("lagrange"))
	p.tolerance = config.GetFloat("tolerance", configfile.Default(.04), configfile.Above(0.))
	p.checks = config.GetInt("checks", configfile.Default(2), configfile.Min(1))
	p.maxPoints = config.GetInt("max_points", configfile.Default(8), configfile.Min(4))
	p.minBase = config.GetInt("min_base_points", configfile.Default(25), configfile.Min(9))
	p.margin = config.GetFloat("margin", configfile.Default(10.), configfile.Min(0.))
	p.minSide = config.GetFloat("min_side", configfile.Default(60.), configfile.Above(0.))
	bm := config.GetSection("bed_mesh")
	p.moveZ = config.GetFloat("horizontal_move_z", configfile.Default(bm.GetFloat("horizontal_move_z", configfile.Default(5.))))
	p.speed = config.GetFloat("speed", configfile.Default(bm.GetFloat("speed", configfile.Default(50.))), configfile.Above(0.))
	p.extendEdges = config.GetBool("extend", configfile.Default(false))
	p.maxTempDiff = config.GetFloat("max_temp_diff", configfile.Default(10.), configfile.Above(0.))
	p.path = config.Get("state_file", configfile.Default(""))
	if p.path == "" {
		if cfg, ok := printer.StartArgs().Get("config_file"); ok {
			p.path = filepath.Join(filepath.Dir(cfg), "printdeck_mesh.json")
		}
	}
	p.st.Method = method
	p.read()
	printer.RegisterEventHandler("klippy:connect", func(...any) {
		if bm, ok := printer.TryLookupObject("bed_mesh").(*bed_mesh.BedMesh); ok {
			bm.OnCalibrate(p.seeMesh)
		}
	})
	p.gcode.RegisterCommand("PRINTDECK_MESH", p.cmdMesh, false, "Make the bed mesh with the method of the deck")
	p.gcode.RegisterCommand("PRINTDECK_MESH_METHOD", p.cmdMethod, false, "Set the method of PRINTDECK_MESH")
	return p
}

func init() { host.RegisterModule("printdeck_mesh", load, nil) }

// Status is the method, the saved map, and the result of the last PRINTDECK_MESH.
func (p *PrintdeckMesh) Status(eventtime float64) map[string]any {
	s := map[string]any{"method": p.st.Method, "methods": slices.Clone(Methods), "base_points": 0, "last": p.last}
	if b := p.st.Base; b != nil && len(b.Probed) > 0 {
		s["base_points"] = len(b.Probed) * len(b.Probed[0])
		s["base_time"] = b.Time.Unix()
		s["base_temp"] = b.BedTemp
	}
	return s
}

func (p *PrintdeckMesh) read() {
	if p.path == "" {
		return
	}
	b, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		log.Printf("printdeck_mesh: %s: %v", p.path, err)
		return
	}
	if slices.Contains(Methods, st.Method) {
		p.st.Method = st.Method
	}
	p.st.Base = st.Base
}

func (p *PrintdeckMesh) write() {
	if p.path == "" {
		return
	}
	b, _ := json.Marshal(p.st)
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Printf("printdeck_mesh: %v", err)
		return
	}
	if err := os.Rename(tmp, p.path); err != nil {
		log.Printf("printdeck_mesh: %v", err)
	}
}

// seeMesh keeps the mesh of a full probe as the saved map.
func (p *PrintdeckMesh) seeMesh(m *bed_mesh.ZMesh) {
	probed := m.Probed()
	if len(probed) == 0 || len(probed)*len(probed[0]) < p.minBase || strings.HasPrefix(m.ProfileName(), "adaptive-") {
		return
	}
	t := p.bedTemp()
	p.st.Base = &Base{Time: time.Now(), BedTemp: t, Params: m.Params(), Probed: probed}
	p.write()
	p.gcode.RespondInfo(fmt.Sprintf("printdeck_mesh: the %d x %d mesh at bed %.0f °C is the saved map", len(probed[0]),
		len(probed), t), true)
}

func (p *PrintdeckMesh) cmdMethod(gcmd *gcode.Command) {
	m := strings.ToLower(gcmd.GetString("METHOD"))
	if !slices.Contains(Methods, m) {
		panic(gcmd.Error("METHOD must be one of %s", strings.Join(Methods, ", ")))
	}
	p.st.Method = m
	p.write()
	gcmd.RespondInfo("printdeck_mesh: method "+m, true)
}

func (p *PrintdeckMesh) cmdMesh(gcmd *gcode.Command) {
	m := strings.ToLower(gcmd.GetString("METHOD", gcode.Default(p.st.Method)))
	if !slices.Contains(Methods, m) {
		panic(gcmd.Error("METHOD must be one of %s", strings.Join(Methods, ", ")))
	}
	start := p.printer.Reactor().Monotonic()
	used, points, worst := m, 0, math.NaN()
	switch m {
	case "map":
		var ok bool
		if points, worst, ok = p.fromMap(gcmd); !ok {
			used = p.fallback(gcmd)
		}
	case "adaptive":
		used = p.fallback(gcmd)
	default:
		p.grid()
	}
	if p.extendEdges {
		p.extend(gcmd)
	}
	p.last = map[string]any{"method": m, "used": used, "points": points, "error": worst,
		"seconds": math.Round((p.printer.Reactor().Monotonic()-start)*10) / 10}
}

// fallback probes a grid under the objects of the print, or on all the bed when the file has no
// objects. It returns the method that it used.
func (p *PrintdeckMesh) fallback(gcmd *gcode.Command) string {
	if _, ok := p.objectArea(); ok {
		p.gcode.RunScriptFromCommand(fmt.Sprintf("BED_MESH_CALIBRATE ADAPTIVE=1 ADAPTIVE_MARGIN=%g", p.margin))
		return "adaptive"
	}
	p.grid()
	return "grid"
}

func (p *PrintdeckMesh) grid() {
	p.gcode.RunScriptFromCommand(fmt.Sprintf("BED_MESH_CALIBRATE PROBE_COUNT=%d,%d ALGORITHM=%s",
		p.gridCount[0], p.gridCount[1], p.gridAlgorithm))
}

// objectArea returns the area of the objects of the print, with the margin. ok is false when the
// file defines no objects.
func (p *PrintdeckMesh) objectArea() (Area, bool) {
	eo, ok := p.printer.TryLookupObject("exclude_object").(interface {
		Status(float64) map[string]any
	})
	if !ok {
		return Area{}, false
	}
	objects, _ := eo.Status(0)["objects"].([]any)
	a := Area{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, o := range objects {
		d, ok := o.(*jinja.Dict)
		if !ok {
			continue
		}
		poly, _ := d.Get("polygon")
		pts, _ := poly.([]any)
		for _, pt := range pts {
			xy, _ := pt.([]any)
			if len(xy) < 2 {
				continue
			}
			x, y := num(xy[0]), num(xy[1])
			a = Area{math.Min(a.MinX, x), math.Min(a.MinY, y), math.Max(a.MaxX, x), math.Max(a.MaxY, y)}
		}
	}
	if a.MinX > a.MaxX {
		return Area{}, false
	}
	return Area{a.MinX - p.margin, a.MinY - p.margin, a.MaxX + p.margin, a.MaxY + p.margin}, true
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return math.NaN()
}

// fromMap makes the mesh of the saved map and a few probe points. ok is false when there is no
// saved map, or when the bed does not fit the map within the tolerance.
func (p *PrintdeckMesh) fromMap(gcmd *gcode.Command) (points int, worst float64, ok bool) {
	b := p.st.Base
	if b == nil {
		gcmd.RespondInfo("printdeck_mesh: no saved map. Probe the full bed on the Bed mesh page of the deck.", true)
		return 0, math.NaN(), false
	}
	if t := p.bedTemp(); b.BedTemp == 0 || math.Abs(t-b.BedTemp) > p.maxTempDiff {
		gcmd.RespondInfo(fmt.Sprintf("printdeck_mesh: the saved map is of a bed at %.0f °C, and the bed is at %.0f °C. "+
			"Probe the full bed at the print temperature on the Bed mesh page of the deck.", b.BedTemp, t), true)
		return 0, math.NaN(), false
	}
	base, err := bed_mesh.NewMesh(b.Params, b.Probed, "printdeck-base")
	if err != nil {
		gcmd.RespondInfo("printdeck_mesh: the saved map is bad: "+err.Error(), true)
		return 0, math.NaN(), false
	}
	pr, isProbe := p.printer.TryLookupObject("probe").(probe.Probe)
	bm, isMesh := p.printer.TryLookupObject("bed_mesh").(*bed_mesh.BedMesh)
	if !isProbe || !isMesh {
		return 0, math.NaN(), false
	}
	x0, y0, x1, y1 := base.Area()
	full := Area{x0, y0, x1, y1}
	area := full
	if a, has := p.objectArea(); has {
		area = a.clip(full)
	}
	area = area.widen(p.minSide, full)

	bm.SetMesh(nil)
	th := toolhead.Of(p.printer)
	params := pr.ProbeParams(gcmd)
	off := pr.Offsets(gcmd)
	nan := math.NaN()
	session := pr.StartProbeSession(gcmd)
	first := true
	probeAt := func(x, y float64) Point {
		speed := params.LiftSpeed
		if first {
			speed = p.speed
		}
		th.ManualMove([]float64{nan, nan, p.moveZ}, speed)
		th.ManualMove([]float64{x - off[0], y - off[1]}, p.speed)
		first = false
		session.RunProbe(gcmd)
		r := session.PullProbedResults()[0].(*manual_probe.ProbeResult)
		return Point{r.BedX_, r.BedY_, r.BedZ_ - base.CalcZ(r.BedX_, r.BedY_)}
	}
	fit, pts, worst, ok := refine(area, probeAt, p.tolerance, p.checks, p.maxPoints)
	th.ManualMove([]float64{nan, nan, p.moveZ}, params.LiftSpeed)
	session.EndProbeSession()
	if !ok {
		gcmd.RespondInfo(fmt.Sprintf("printdeck_mesh: the bed differs from the saved map by %.3f mm after %d points. "+
			"Probe the full bed again on the Bed mesh page.", worst, len(pts)), true)
		return len(pts), worst, false
	}
	m, err := shifted(b, fit)
	if err != nil {
		panic(gcmd.Error("printdeck_mesh: %s", err.Error()))
	}
	bm.SetMesh(m)
	gcmd.RespondInfo(fmt.Sprintf("printdeck_mesh: mesh from the saved map and %d points, largest error %.3f mm, "+
		"tilt %.3f mm per 100 mm in X, %.3f in Y", len(pts), worst, fit.B*100, fit.C*100), true)
	return len(pts), worst, true
}

// shifted returns the mesh of the saved map plus the plane.
func shifted(b *Base, fit Plane) (*bed_mesh.ZMesh, error) {
	base, err := bed_mesh.NewMesh(b.Params, b.Probed, "printdeck-base")
	if err != nil {
		return nil, err
	}
	x0, y0, x1, y1 := base.Area()
	ny, nx := len(b.Probed), len(b.Probed[0])
	out := make([][]float64, ny)
	for j := range out {
		out[j] = make([]float64, nx)
		y := y0 + (y1-y0)*float64(j)/float64(max(1, ny-1))
		for i := range out[j] {
			x := x0 + (x1-x0)*float64(i)/float64(max(1, nx-1))
			out[j][i] = b.Probed[j][i] + fit.At(x, y)
		}
	}
	return bed_mesh.NewMesh(b.Params, out, "printdeck")
}

// extend replaces the mesh with a mesh that has one more row and one more column on each side, on
// the slope of the last two. The probe cannot reach the edges of the bed (the TAZ: Y above 230), and
// Klipper keeps the z of the edge of the mesh beyond it. A bed that slopes down to the back then
// prints high at the back.
func (p *PrintdeckMesh) extend(gcmd *gcode.Command) {
	bm, ok := p.printer.TryLookupObject("bed_mesh").(*bed_mesh.BedMesh)
	if !ok || bm.Mesh() == nil {
		return
	}
	m, err := extended(bm.Mesh())
	if err != nil {
		gcmd.RespondInfo("printdeck_mesh: the mesh is not extended: "+err.Error(), true)
		return
	}
	bm.SetMesh(m)
	x0, y0, x1, y1 := m.Area()
	gcmd.RespondInfo(fmt.Sprintf("printdeck_mesh: the mesh goes from %.0f,%.0f to %.0f,%.0f", x0, y0, x1, y1), true)
}

// extended returns the mesh with one more row and column on each side. The new points continue
// the line of the last two points. The mesh then uses bicubic interpolation: a Lagrange polynomial
// through the new points can swing between them.
func extended(m *bed_mesh.ZMesh) (*bed_mesh.ZMesh, error) {
	probed := m.Probed()
	ny := len(probed)
	if ny < 2 || len(probed[0]) < 2 {
		return m, nil
	}
	nx := len(probed[0])
	x0, y0, x1, y1 := m.Area()
	dx, dy := (x1-x0)/float64(nx-1), (y1-y0)/float64(ny-1)
	out := make([][]float64, ny+2)
	for j := range ny {
		r := probed[j]
		row := make([]float64, 0, nx+2)
		row = append(row, 2*r[0]-r[1])
		row = append(row, r...)
		row = append(row, 2*r[nx-1]-r[nx-2])
		out[j+1] = row
	}
	out[0] = make([]float64, nx+2)
	out[ny+1] = make([]float64, nx+2)
	for i := range nx + 2 {
		out[0][i] = 2*out[1][i] - out[2][i]
		out[ny+1][i] = 2*out[ny][i] - out[ny-1][i]
	}
	params := m.Params()
	for i, kv := range params {
		switch kv.Key {
		case "min_x":
			params[i].Value = x0 - dx
		case "max_x":
			params[i].Value = x1 + dx
		case "min_y":
			params[i].Value = y0 - dy
		case "max_y":
			params[i].Value = y1 + dy
		case "x_count":
			params[i].Value = nx + 2
		case "y_count":
			params[i].Value = ny + 2
		case "algo":
			params[i].Value = "bicubic"
		}
	}
	return bed_mesh.NewMesh(params, out, m.ProfileName())
}

// bedTemp is the temperature of the bed now, or 0 with no heated bed.
func (p *PrintdeckMesh) bedTemp() float64 {
	h, ok := p.printer.TryLookupObject("heater_bed").(interface {
		Status(float64) map[string]any
	})
	if !ok {
		return 0
	}
	return num(h.Status(p.printer.Reactor().Monotonic())["temperature"])
}
