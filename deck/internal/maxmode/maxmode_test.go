package maxmode

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"printdeck/internal/klipper/toolhead"
)

// The limits of printer/printer.cfg, and its filament.
var testLimits = toolhead.Limits{MaxVelocity: 300, MaxAccel: 3000, MinCruiseRatio: 0.5,
	SquareCornerVelocity: 5, MaxZVelocity: 25, MaxZAccel: 100, InstantCornerVelocity: 1}

func testSettings() Settings {
	s := Defaults()
	s.Enable, s.FilamentDiameter, s.MaxVelocity = true, 2.85, testLimits.MaxVelocity
	return s
}

// ePerMM is the E per mm of a line w wide and h high with 2.85 mm filament.
func ePerMM(w, h float64) float64 { return w * h / (math.Pi * 2.85 * 2.85 / 4) }

// step is a move of a trace: its top speed (0 for the F speed) and the speeds of the planner.
type step struct {
	line               string
	length, top, f     float64
	start, cruise, end float64
}

// trace plans G-code (absolute X, Y and Z, relative E) with the motion planner, and returns the
// speeds of each move. With max false, the moves have the F speeds.
func trace(t *testing.T, s Settings, gcode string, maxMode bool) ([]step, float64) {
	t.Helper()
	e := toolhead.NewEstimator(testLimits)
	sh := NewShaper(s)
	var steps []step
	k := 0
	e.OnMove = func(m *toolhead.Move) {
		steps[k].start, steps[k].cruise, steps[k].end = m.StartV, m.CruiseV, m.EndV
		k++
	}
	pos := e.Position()
	feed := 25.0
	for _, line := range strings.Split(gcode, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexByte(line, ';'); i >= 0 {
			sh.Comment(line[i+1:])
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 || (f[0] != "G0" && f[0] != "G1") {
			continue
		}
		next := append([]float64(nil), pos...)
		next[3] = pos[3]
		for _, w := range f[1:] {
			v, _ := word(w)
			switch w[0] {
			case 'X', 'Y', 'Z':
				next[w[0]-'X'] = v
			case 'E':
				next[3] += v
			case 'F':
				feed = v / 60
			}
		}
		d := []float64{next[0] - pos[0], next[1] - pos[1], next[2] - pos[2], next[3] - pos[3]}
		top := sh.Speed(d)
		speed := feed
		if maxMode && top > 0 {
			speed = top
		}
		if l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2] + d[3]*d[3]); l > 0 {
			steps = append(steps, step{line: line, length: math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2]),
				top: top, f: feed})
		}
		e.Move(next, speed)
		pos = next
	}
	total := e.Finish()
	return steps, total
}

func logTrace(t *testing.T, name string, steps []step, total float64) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %.3f s\n  %-34s %6s %6s %6s %6s %6s %6s\n", name, total, "move", "mm", "F", "top",
		"start", "cruise", "end")
	for _, s := range steps {
		fmt.Fprintf(&b, "  %-34s %6.1f %6.1f %6.1f %6.1f %6.1f %6.1f\n", s.line, s.length, s.f, s.top, s.start,
			s.cruise, s.end)
	}
	t.Log(b.String())
}

// layer1 is the G-code of a start at layer 1: layer 0 has one travel move.
const layer1 = ";LAYER:0\nG0 X0 Y1 F6000\nG0 X0 Y0\n;LAYER:1\n"

func near(a, b float64) bool { return math.Abs(a-b) < 0.01*math.Max(1, math.Abs(b)) }

// A long infill line goes at the flow limit, slows down into a 90° corner, and speeds up again
// after it. The flow limit of a 0.5 x 0.2 mm line at 12 mm³/s is 120 mm/s.
func TestStraightRunIntoCorner(t *testing.T) {
	e := ePerMM(0.5, 0.2)
	g := layer1 + fmt.Sprintf(";TYPE:FILL\nG1 X100 Y0 E%.5f F3000\nG1 X100 Y10 E%.5f\nG1 X0 Y10 E%.5f\n",
		100*e, 10*e, 100*e)
	steps, total := trace(t, testSettings(), g, true)
	logTrace(t, "max mode", steps, total)
	_, normal := trace(t, testSettings(), g, false)
	t.Logf("F speeds: %.3f s", normal)
	run, corner, out := steps[len(steps)-3], steps[len(steps)-2], steps[len(steps)-1]
	if !near(run.top, 120) || !near(run.cruise, 120) {
		t.Errorf("long run: top %.1f cruise %.1f, want 120", run.top, run.cruise)
	}
	if run.end > 10 || corner.end > 10 {
		t.Errorf("corner speeds %.1f and %.1f, want the junction speed of a 90° corner", run.end, corner.end)
	}
	if !near(out.cruise, 120) {
		t.Errorf("after the corner: cruise %.1f, want 120", out.cruise)
	}
	if total >= normal {
		t.Errorf("max mode %.3f s, F speeds %.3f s", total, normal)
	}
}

// The moves of a short zigzag have the flow limit as their top speed, but the planner does not get
// there: the segments are too short to accelerate, and each corner slows the toolhead.
func TestShortZigzag(t *testing.T) {
	e := ePerMM(0.5, 0.2)
	var b strings.Builder
	b.WriteString(layer1 + ";TYPE:FILL\nG1 F3000\n")
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "G1 X%d Y%d E%.5f\n", 2*i, (i%2)*2, math.Hypot(2, 2)*e)
	}
	steps, total := trace(t, testSettings(), b.String(), true)
	logTrace(t, "zigzag", steps, total)
	for _, s := range steps[len(steps)-10:] {
		if !near(s.top, 120) || s.cruise > 0.6*s.top {
			t.Errorf("%s: top %.1f cruise %.1f", s.line, s.top, s.cruise)
		}
	}
}

// On the first layer the moves go at first_layer_speed. A travel move goes at the max velocity, and
// a retraction keeps its F speed.
func TestFirstLayer(t *testing.T) {
	e := ePerMM(0.5, 0.3)
	g := fmt.Sprintf(";LAYER:0\nG0 X10 Y10 Z0.3 F6000\n;TYPE:SKIRT\nG1 X110 Y10 E%.5f F1200\nG1 E-1 F2400\n"+
		"G0 X110 Y110\nG1 E1\n;TYPE:WALL-OUTER\nG1 X10 Y110 E%.5f F1500\n", 100*e, 100*e)
	steps, total := trace(t, testSettings(), g, true)
	logTrace(t, "first layer", steps, total)
	want := []float64{300, 30, 0, 300, 0, 30}
	for i, s := range steps {
		if !near(s.top, want[i]) {
			t.Errorf("%s: top %.1f, want %.1f", s.line, s.top, want[i])
		}
	}
}

// An outer wall goes at outer_wall_speed (80 mm/s), and an inner wall at its flow limit. A bridge
// goes at bridge_speed.
func TestOuterWall(t *testing.T) {
	e := ePerMM(0.45, 0.2)
	g := layer1 + fmt.Sprintf(";TYPE:WALL-OUTER\nG1 X100 Y0 E%.5f F1500\n;TYPE:WALL-INNER\nG1 X100 Y100 E%.5f\n"+
		";FEATURE: Bridge\nG1 X0 Y100 E%.5f\n", 100*e, 100*e, 100*e)
	steps, total := trace(t, testSettings(), g, true)
	logTrace(t, "walls", steps, total)
	n := len(steps)
	for i, want := range []float64{80, 12 / (0.45 * 0.2), 30} {
		if s := steps[n-3+i]; !near(s.top, want) {
			t.Errorf("%s: top %.1f, want %.1f", s.line, s.top, want)
		}
	}
}

// A small layer gets a factor for min_layer_time, down to min_speed.
func TestMinLayerTime(t *testing.T) {
	e := ePerMM(0.45, 0.2)
	square := func(size float64) string {
		return fmt.Sprintf(";TYPE:WALL-OUTER\nG1 X%[1]g Y0 E%[2]f\nG1 X%[1]g Y%[1]g E%[2]f\nG1 X0 Y%[1]g E%[2]f\n"+
			"G1 X0 Y0 E%[2]f\n", size, size*e)
	}
	// Absolute E for the scan.
	g := "M83\n;LAYER:0\n" + square(200) + ";LAYER:1\n" + square(200) + ";LAYER:2\n" + square(10)
	s := testSettings()
	f := LayerFactors(strings.NewReader(g), s, 0)
	t.Logf("factors %v", f)
	if len(f) != 3 || f[0] != 1 || f[1] != 1 {
		t.Fatalf("factors %v", f)
	}
	// 40 mm at 80 mm/s is 0.5 s: the factor is 0.5 / 8.
	if !near(f[2], 0.5/8) {
		t.Errorf("small layer factor %.4f, want %.4f", f[2], 0.5/8)
	}
	sh := NewShaper(s)
	sh.Comment("LAYER:0")
	sh.Speed([]float64{1, 0, 0, 0})
	sh.Comment("LAYER:1")
	sh.Comment("TYPE:WALL-OUTER")
	sh.SetFactor(f[2])
	if v := sh.Speed([]float64{10, 0, 0, 10 * e}); !near(v, s.MinSpeed) {
		t.Errorf("speed %.1f, want min_speed %.1f", v, s.MinSpeed)
	}
	// The scan of one layer from its layer comment, as the max_mode module does it.
	i := strings.Index(g, ";LAYER:2")
	st := StartState()
	st.AbsE, st.Layer = false, 1
	if v := NextLayerFactor(strings.NewReader(g[i:]), s, st); !near(v, f[2]) {
		t.Errorf("NextLayerFactor %.4f, want %.4f", v, f[2])
	}
}

func TestLayerComments(t *testing.T) {
	sh := NewShaper(testSettings())
	if !sh.Comment("LAYER_CHANGE") || sh.Comment("LAYER:0") || sh.Layer() != 0 {
		t.Fatalf("layer %d after two comments with no move", sh.Layer())
	}
	sh.Speed([]float64{1, 0, 0, 0})
	if !sh.Comment(" LAYER_CHANGE") || sh.Layer() != 1 {
		t.Fatalf("layer %d", sh.Layer())
	}
	if sh.Comment("LAYER_COUNT:20") {
		t.Fatal("LAYER_COUNT starts a layer")
	}
}

func TestClassifyFeature(t *testing.T) {
	for name, want := range map[string]Feature{
		"WALL-OUTER": OuterWall, "WALL-INNER": InnerWall, "FILL": Infill, "SKIN": SolidInfill,
		"SUPPORT-INTERFACE": Support, "SKIRT": Skirt, "PRIME-TOWER": Other,
		"External perimeter": OuterWall, "Perimeter": InnerWall, "Overhang perimeter": Overhang,
		"Internal infill": Infill, "Solid infill": SolidInfill, "Top solid infill": TopSurface,
		"Bridge infill": Bridge, "Gap fill": SolidInfill, "Skirt/Brim": Skirt,
		" Outer wall": OuterWall, " Inner wall": InnerWall, " Sparse infill": Infill,
		" Internal solid infill": SolidInfill, " Bottom surface": SolidInfill, " Top surface": TopSurface,
		" Internal Bridge": Bridge, " Overhang wall": Overhang, " Gap infill": SolidInfill,
		" Brim": Skirt, " Custom": Other,
	} {
		if got := ClassifyFeature(name); got != want {
			t.Errorf("%q: %s, want %s", name, got, want)
		}
	}
}
