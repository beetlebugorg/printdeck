package printer

import (
	"image"
	"io"
	"io/fs"
	"math"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcode"
)

func TestMatchPicksClosest(t *testing.T) {
	stls := []string{"base.stl", "body.stl", "cap.stl", "frame.stl", "inky_frame.stl", "pad.stl"}
	for gc, want := range map[string]string{
		"printdeck_inky_frame.gcode": "inky_frame.stl",
		"CE3E3V2_frame.gcode":        "frame.stl",
		"Body.gcode":                 "body.stl",
		"printdeck_cap.gcode":        "", // too short to match
	} {
		best, score := "", 0
		for _, s := range stls {
			if n := match(gc, s); n > score {
				best, score = s, n
			}
		}
		if best != want {
			t.Errorf("%s: got %q, want %q", gc, best, want)
		}
	}
}

// The title of a print names its model before the file does: the job in Cura can keep the name of
// an earlier model.
func TestMatchModelTitle(t *testing.T) {
	stls := []string{"pod_body.stl", "shoulder_fdm.stl"}
	for _, c := range []struct{ file, title, want string }{
		{"LTAZW_pod_body.gcode", "Shoulder fdm", "shoulder_fdm.stl"},
		{"LTAZW_pod_body.gcode", "", "pod_body.stl"},
		{"LTAZW_pod_body.gcode", "Gift box", "pod_body.stl"},
	} {
		best, score := "", 0
		for _, s := range stls {
			if n := matchModel(c.file, c.title, s); n > score {
				best, score = s, n
			}
		}
		if best != c.want {
			t.Errorf("%s %q: got %q, want %q", c.file, c.title, best, c.want)
		}
	}
}

// oneFile is a Source with one G-code file.
type oneFile struct{ path, body string }

func (o oneFile) Open(p string) (io.ReadCloser, error) {
	if p != o.path {
		return nil, fs.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(o.body)), nil
}
func (oneFile) Models() []File { return nil }

// The picture of a finished print is drawn once in the background. A file that is gone has none.
func TestRecordPicture(t *testing.T) {
	var g strings.Builder
	g.WriteString("G90\nM82\nG1 Z0.2 F600\n")
	for l := 1; l <= 10; l++ {
		g.WriteString(";LAYER:" + string(rune('0'+l%10)) + "\n")
		for i := 0; i < 4; i++ {
			x, y := []string{"10", "30", "30", "10"}[i], []string{"10", "10", "30", "30"}[i]
			g.WriteString("G1 X" + x + " Y" + y + " E" + string(rune('1'+i)) + "\n")
		}
		g.WriteString("G1 Z" + []string{"0.4", "0.6", "0.8", "1.0", "1.2", "1.4", "1.6", "1.8", "2.0", "2.2"}[l-1] + "\n")
	}
	v := NewView(oneFile{"box.gcode", g.String()})
	r := Record{File: "box.gcode", Path: "box.gcode", Result: "cancelled", Pos: 200, End: time.Unix(1, 0)}
	if img, loading := v.RecordPicture(r); img != nil || !loading {
		t.Fatalf("first call: %v %v, want loading", img, loading)
	}
	var img image.Image
	for i := 0; i < 200; i++ {
		var loading bool
		if img, loading = v.RecordPicture(r); !loading {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if img == nil || img.Bounds().Dx() != PreviewW {
		t.Fatalf("picture %v", img)
	}
	r.Path = "gone.gcode"
	v.RecordPicture(r)
	for i := 0; i < 200; i++ {
		if img, loading := v.RecordPicture(r); !loading {
			if img != nil {
				t.Error("a picture for a file that is gone")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("still loading")
}

// PrintBox has the extents of the extruding moves, the first layer, and the height up to the file
// position.
func TestPrintBox(t *testing.T) {
	src := `G90
M83
G1 X0 Y0 Z5 F3000
G1 X5 Y5 Z0.3
;LAYER:0
G1 X10 Y10 F1200
G1 X40 Y10 E1
G1 X40 Y30 E1
;LAYER:1
G1 Z0.6
G1 X20 Y30 E1
G1 X50 Y50 Z0.9 E1
;LAYER:2
G1 X60 Y60 Z1.2 E1
`
	gc, err := gcode.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	st := State{Printing: true, FilePath: "a.gcode", FileSize: int64(len(src))}
	v := &View{gc: gc, file: fileKey(st), view: -1}
	part, first, ok, fok := v.PrintBox(st, int64(len(src)))
	if !ok || !fok {
		t.Fatal("no box")
	}
	if part.Min[0] != 10 || part.Min[1] != 10 || math.Abs(part.Min[2]-0.3) > 1e-6 || part.Max[0] != 60 || part.Max[1] != 60 || math.Abs(part.Max[2]-1.2) > 1e-6 {
		t.Errorf("part %+v", part)
	}
	if first.Min[0] != 10 || first.Max[0] != 40 || first.Max[1] != 30 || math.Abs(first.Max[2]-0.3) > 1e-6 {
		t.Errorf("first layer %+v", first)
	}
	// In the middle of the print the top is the height of the move there.
	part, _, _, _ = v.PrintBox(st, int64(strings.Index(src, ";LAYER:2")))
	if math.Abs(part.Max[2]-0.9) > 1e-6 {
		t.Errorf("top %v, want 0.9", part.Max[2])
	}
	// Another print that loads: no box.
	st.FilePath = "b.gcode"
	if _, _, ok, _ := v.PrintBox(st, 0); ok {
		t.Error("a box of another print")
	}
	// After the print: the last print.
	if _, _, ok, _ := v.PrintBox(State{}, math.MaxInt64); !ok {
		t.Error("no box after the print")
	}
}

// PartBox has the moves from the second layer on: no purge line before the first layer, and no
// skirt or brim of the first layer. A file of one layer has the box of its first layer, and a
// file with no layer marks has the moves over the first height.
func TestPartBox(t *testing.T) {
	src := `G90
M83
G1 X0 Y0 Z0.3 F3000
G1 X0 Y200 E10
;LAYER:0
G1 X5 Y5 F1200
G1 X95 Y5 E3
G1 X95 Y95 E3
G1 X20 Y20
G1 X80 Y20 E2
;LAYER:1
G1 Z0.5
G1 X20 Y30 E1
G1 X80 Y70 Z0.7 E1
;LAYER:2
G1 X70 Y60 Z0.9 E1
`
	box := func(src string) (Box, bool) {
		gc, err := gcode.Parse(strings.NewReader(src))
		if err != nil {
			t.Fatal(err)
		}
		st := State{Printing: true, FilePath: "a.gcode", FileSize: int64(len(src))}
		v := &View{gc: gc, file: fileKey(st), view: -1}
		return v.PartBox(st)
	}
	b, ok := box(src)
	if !ok || b.Min[0] != 20 || b.Min[1] != 20 || b.Max[0] != 80 || b.Max[1] != 70 || math.Abs(b.Max[2]-0.9) > 1e-6 {
		t.Errorf("part %+v, want x 20..80, y 20..70, top 0.9", b)
	}
	// One layer: the first layer, with no purge line.
	one := src[:strings.Index(src, ";LAYER:1")]
	b, ok = box(one)
	if !ok || b.Min[0] != 5 || b.Max[0] != 95 || b.Max[1] != 95 || b.Min[1] != 5 {
		t.Errorf("one layer %+v, want x 5..95, y 5..95", b)
	}
	// No layer marks: the moves over the height of the first move.
	b, ok = box("G90\nM83\nG1 X0 Y0 Z0.3\nG1 X100 Y0 E5\nG1 Z0.6\nG1 X10 Y10 F600\nG1 X30 Y40 E1\n")
	if !ok || b.Min[0] != 10 || b.Max[0] != 30 || b.Max[1] != 40 {
		t.Errorf("no marks %+v", b)
	}
	if _, ok := (&View{}).PartBox(State{}); ok {
		t.Error("a box with no G-code")
	}
}
