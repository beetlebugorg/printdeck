package gcode

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// ViewOf gets the turn and the elevation of a view back from the screen directions of its axes,
// with any scale of the steps.
func TestViewOf(t *testing.T) {
	for _, want := range [][2]float64{{35, 40}, {-60, 25}, {150, 70}, {0, 30}, {-170, 10}} {
		v := NewView(want[0], want[1])
		o := [3]float64{10, 20, 0}
		ou, ov := v.Project(o[0], o[1], o[2])
		axis := func(dx, dy, dz float64) (float64, float64) {
			u, w := v.Project(o[0]+dx, o[1]+dy, o[2]+dz)
			return (u - ou) * 3, (w - ov) * 3
		}
		xu, xv := axis(1, 0, 0)
		yu, yv := axis(0, 1, 0)
		_, zv := axis(0, 0, 1)
		got, ok := ViewOf(xu, xv, yu, yv, zv)
		if !ok || math.Abs(got.Turn-want[0]) > 1e-6 || math.Abs(got.Elevation-want[1]) > 1e-6 {
			t.Errorf("ViewOf of %v: %v %v %v", want, got.Turn, got.Elevation, ok)
		}
	}
	// A camera under the bed, and a camera that looks along the bed, give no view.
	if _, ok := ViewOf(1, 0, 0, 0.5, 1); ok {
		t.Error("a view from under the bed")
	}
	if _, ok := ViewOf(1, 0, 0, 0, -1); ok {
		t.Error("a view along the bed")
	}
}

// The lights of the default view do not turn.
func TestDefaultViewLights(t *testing.T) {
	if l := DefaultView.Light(keyLight); math.Abs(l[0]-keyLight[0]) > 1e-12 || math.Abs(l[1]-keyLight[1]) > 1e-12 {
		t.Errorf("key light %v, want %v", l, keyLight)
	}
}

// The plate from the back right shows its sides at x = Max and y = Max: the pixel just under the
// near corner is a side of the plate.
func TestDrawPlateFromBehind(t *testing.T) {
	v := NewView(-145, 40) // the viewer is at +x, +y
	p := Plate{Min: [2]float64{0, 0}, Max: [2]float64{100, 100}, Color: color.RGBA{200, 100, 50, 255}}
	minU, minV, maxU, maxV := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{0, 0}, {100, 0}, {0, 100}, {100, 100}} {
		u, w := v.Project(c[0], c[1], -plateThick)
		minU, maxU, minV, maxV = min(minU, u), max(maxU, u), min(minV, w), max(maxV, w)
	}
	cam := Fit(200, 200, 10, minU, minV, maxU, maxV)
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	DrawPlate(img, v, cam, p, color.RGBA{255, 255, 255, 255}, color.RGBA{255, 255, 255, 255})
	u, w := v.Project(100, 100, -plateThick/2)
	x, y := int(cam.X+u*cam.Scale), int(cam.Y+w*cam.Scale)
	if c := img.RGBAAt(x, y); c.R == 255 && c.G == 255 && c.B == 255 {
		t.Errorf("no side of the plate at the near corner (%d, %d)", x, y)
	}
}
