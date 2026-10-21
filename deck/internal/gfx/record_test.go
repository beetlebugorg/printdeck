package gfx

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestRecording(t *testing.T) {
	c, rec := NewRecording(480, 800)
	red := color.RGBA{0xff, 0x5a, 0x1f, 255}
	c.Fill(image.Rect(0, 0, 480, 800), color.RGBA{0x12, 0x12, 0x11, 255})
	c.RoundRect(image.Rect(10, 10, 110, 50), 6, red)
	c.Polygon([]image.Point{{0, 0}, {10, 0}, {5, 8}}, red)
	c.Text(image.Rect(0, 100, 480, 140), "PRINT", Face(true, 20), red, Left)
	c.Clipped(image.Rect(0, 0, 100, 100), func(in *Canvas) { in.Fill(image.Rect(0, 0, 200, 200), red) })
	c.Dim(3)
	got := string(rec.JSON())
	for _, want := range []string{`["f",0,0,480,800,1184273]`, `["r",10,10,110,50,6,16734751]`,
		`["p",[0,0,10,0,5,8],16734751]`, `"PRINT",1,20,16734751]`, `["clip",0,0,100,100]`,
		`["f",0,0,200,200,16734751]`, `["unclip"]`, `["d",3]`} {
		if !strings.Contains(got, want) {
			t.Errorf("the recording has no %s:\n%s", want, got)
		}
	}
	var ops []any
	if err := json.Unmarshal([]byte(got), &ops); err != nil || len(ops) != 8 {
		t.Errorf("%d commands, %v", len(ops), err)
	}
	// A recording canvas draws no pixels.
	if len(c.Pix) != 0 {
		t.Error("the recording canvas has pixels")
	}
}

// A RoundRect records one command, also on a canvas that draws pixels too.
func TestRoundRectIsOneCommand(t *testing.T) {
	c := New(100, 100)
	c.rec = &Recording{}
	c.RoundRect(image.Rect(0, 0, 50, 30), 8, color.RGBA{1, 2, 3, 255})
	if len(c.rec.Ops) != 1 {
		t.Errorf("%d commands: %s", len(c.rec.Ops), c.rec.JSON())
	}
	if c.RGBAAt(25, 15) != (color.RGBA{1, 2, 3, 255}) {
		t.Error("the pixels were not drawn")
	}
}

// A replay of a recording draws the same pixels as the drawing itself.
func TestReplay(t *testing.T) {
	draw := func(c *Canvas) {
		c.Fill(image.Rect(0, 0, 100, 60), color.RGBA{0x12, 0x12, 0x11, 255})
		c.RoundRect(image.Rect(5, 5, 60, 30), 6, color.RGBA{0xff, 0x5a, 0x1f, 255})
		c.Text(image.Rect(0, 30, 100, 60), "OK", Face(true, 20), color.RGBA{255, 255, 255, 255}, Left)
	}
	direct := New(100, 60)
	draw(direct)
	rc, rec := NewRecording(100, 60)
	draw(rc)
	replayed := New(100, 60)
	if err := Replay(replayed, rec.JSON(), func(int) image.Image { return nil }); err != nil {
		t.Fatal(err)
	}
	diff := 0
	for i := range direct.Pix {
		if direct.Pix[i] != replayed.Pix[i] {
			diff++
		}
	}
	if diff != 0 {
		t.Errorf("%d bytes differ", diff)
	}
}
