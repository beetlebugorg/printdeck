package printer

import (
	"strings"
	"testing"

	"printdeck/internal/gcode"
)

// A paused print has the layer of the pause.
func TestActivityPausedLayer(t *testing.T) {
	src := ";LAYER_COUNT:3\n;LAYER:0\nG1 X1 Y1 Z0.2 E1 F600\n;LAYER:1\nG1 X2 Y2 Z0.4 E2\n;LAYER:2\nG1 X3 Y3 Z0.6 E3\n"
	gc, err := gcode.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	st := State{Reachable: true, Connected: true, Text: "Paused", Printing: true, Paused: true,
		File: "a.gcode", FilePath: "a.gcode", FilePos: int64(strings.Index(src, "G1 X2"))}
	v := NewView(nil)
	v.gc, v.file = gc, fileKey(st)
	v.Activity(&st)
	if st.Layers != 3 || st.Layer != gc.LayerAt(st.FilePos) || st.Layer < 1 || st.Activity != "Paused" {
		t.Errorf("layer %d of %d, %q", st.Layer, st.Layers, st.Activity)
	}
}
