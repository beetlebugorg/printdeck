package printer

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestGcodeGz(t *testing.T) {
	src := ";LAYER:0\nG1 X10 Y10 E1\n"
	v := NewView(oneFile{"a.gcode", src})
	b := v.GcodeGz("a.gcode")
	if b == nil {
		t.Fatal("no file")
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != src {
		t.Fatalf("got %q", got)
	}
	if v.GcodeGz("missing.gcode") != nil || v.GcodeGz("") != nil {
		t.Fatal("a missing file gave bytes")
	}
}
