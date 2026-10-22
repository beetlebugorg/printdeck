package printer

import (
	"image"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"
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
