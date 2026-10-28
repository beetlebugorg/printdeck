package camera

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeScene is a nozzle and the boxes of a print.
type fakeScene struct {
	nozzle      [3]float64
	part, first Box
	noPrint     bool
	noNozzle    bool
}

func (s *fakeScene) Nozzle() ([3]float64, bool) { return s.nozzle, !s.noNozzle }
func (s *fakeScene) Part() (Box, bool)          { return s.part, !s.noPrint }
func (s *fakeScene) FirstLayer() (Box, bool)    { return s.first, !s.noPrint }

// texturePicture is a picture of w x h with a fine checker of 4 pixels in Y, and color in Cb and
// Cr, so a blur shows.
func texturePicture(w, h int) *image.YCbCr {
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for y := range h {
		for x := range w {
			v := byte(60)
			if (x/4+y/4)%2 == 0 {
				v = 220
			}
			p.Y[y*p.YStride+x] = v
		}
	}
	for y := range h / 2 {
		for x := range w / 2 {
			p.Cb[y*p.CStride+x], p.Cr[y*p.CStride+x] = byte(80+x%64), byte(180-y%64)
		}
	}
	return p
}

// viewHub is a hub with the test device, the calibration of the synthetic camera, and a scene.
func viewHub(t *testing.T, scene Scene) *Hub {
	h := NewHub(Settings{})
	h.scan = func() []Device { return []Device{{Path: "test", ID: "test", Name: "Test"}} }
	if scene != nil {
		h.SetScene(scene)
	}
	if t != nil {
		h.LoadCalibration(synthCalibration(t, newSynthCamera()))
	}
	return h
}

// The toolhead crop is centered on the nozzle, has the shape of the frame, holds the box of 60 x
// 60 mm, and stays in the frame.
func TestToolheadCrop(t *testing.T) {
	cam := newSynthCamera()
	c := synthCalibration(t, cam)
	sz := image.Pt(960, 540)
	proj := func(p [3]float64) (float64, float64, bool) { return c.Project(p, sz.X, sz.Y, 0) }
	n := [3]float64{140, 140, 20}
	crop, ok := toolheadTarget(proj, n, sz)
	if !ok {
		t.Fatal("no crop")
	}
	w, h := crop[2]-crop[0], crop[3]-crop[1]
	if math.Abs(w/h-16.0/9) > 1e-6 {
		t.Errorf("shape %.3f", w/h)
	}
	nx, ny := cam.project(n)
	if math.Abs((crop[0]+crop[2])/2-nx) > 0.5 || math.Abs((crop[1]+crop[3])/2-ny) > 0.5 {
		t.Errorf("crop %v is not centered on the nozzle at %.1f,%.1f", crop, nx, ny)
	}
	for _, d := range [][2]float64{{-30, -30}, {30, -30}, {30, 30}, {-30, 30}} {
		x, y := cam.project([3]float64{n[0] + d[0], n[1] + d[1], n[2]})
		if x < crop[0] || x > crop[2] || y < crop[1] || y > crop[3] {
			t.Errorf("corner %v at %.0f,%.0f is outside the crop %v", d, x, y, crop)
		}
	}
	t.Logf("nozzle at %.0f,%.0f: crop %.0f x %.0f px", nx, ny, w, h)
	// A nozzle near the edge of the picture: the crop stays in the frame.
	crop, _ = toolheadTarget(proj, [3]float64{0, 0, 5}, sz)
	if crop[0] < 0 || crop[1] < 0 || crop[2] > 960+1e-9 || crop[3] > 540+1e-9 {
		t.Errorf("crop %v is outside the frame", crop)
	}
}

// The smoothing moves the crop toward a new place over a few frames, with no jump.
func TestSmoothCrop(t *testing.T) {
	sz := image.Pt(960, 540)
	a := fitCrop(300, 270, 320, 0, sz)
	b := fitCrop(600, 270, 320, 0, sz)
	c := a
	prev := (c[0] + c[2]) / 2
	for i := range 30 {
		c = smoothCrop(c, b, 100*time.Millisecond, sz)
		x := (c[0] + c[2]) / 2
		if x < prev || x-prev > 300*0.3 {
			t.Fatalf("frame %d: center %.1f after %.1f", i, x, prev)
		}
		prev = x
	}
	if math.Abs(prev-600) > 1 {
		t.Errorf("center %.1f after 3 s, want 600", prev)
	}
	if c := smoothCrop(a, b, 5*time.Second, sz); c != b {
		t.Error("a long pause does not go to the target at once")
	}
}

// The dark blur: outside the mask the picture is darker and blurred, inside it does not change.
func TestDarkBlur(t *testing.T) {
	pic := texturePicture(960, 540)
	plan := viewPlan{crop: [4]float64{0, 0, 960, 540},
		mask: [][2]float64{{300, 150}, {660, 150}, {700, 400}, {260, 400}}}
	out := renderView(pic, plan, 960, 540)
	in := spans(plan.mask, 960, 540, 1)
	var inDiff, outN int
	var outSum, outVar, srcSum float64
	for y := range 540 {
		for x := range 960 {
			s, o := pic.Y[y*pic.YStride+x], out.Y[y*out.YStride+x]
			if x >= in[y][0] && x < in[y][1] {
				if s != o {
					inDiff++
				}
				continue
			}
			outN++
			outSum += float64(o)
			srcSum += float64(s)
		}
	}
	mean := outSum / float64(outN)
	for y := range 540 {
		for x := range 960 {
			if x < in[y][0] || x >= in[y][1] {
				d := float64(out.Y[y*out.YStride+x]) - mean
				outVar += d * d
			}
		}
	}
	sd := math.Sqrt(outVar / float64(outN))
	if inDiff != 0 {
		t.Errorf("%d pixels inside the mask changed", inDiff)
	}
	ratio := mean / (srcSum / float64(outN))
	if math.Abs(ratio-blurDark) > 0.03 {
		t.Errorf("outside: %.2f of the brightness, want %.2f", ratio, blurDark)
	}
	// The checker has a deviation of 80; the blur of 4-pixel squares at 1/8 size removes it.
	if sd > 5 {
		t.Errorf("outside: deviation %.1f, the picture is not blurred", sd)
	}
	// The color goes toward gray outside, and stays inside.
	cx, cy := 20, 20
	if d := int(out.Cb[cy*out.CStride+cx]) - 128; d > 20 || d < -20 {
		t.Errorf("Cb outside %d", out.Cb[cy*out.CStride+cx])
	}
	cx, cy = 240, 140
	if out.Cb[cy*out.CStride+cx] != pic.Cb[cy*pic.CStride+cx] {
		t.Error("Cb inside changed")
	}
}

// With no calibration, each view is the whole picture.
func TestViewFallback(t *testing.T) {
	h := viewHub(nil, &fakeScene{nozzle: [3]float64{140, 140, 20}})
	f := picFrame(texturePicture(960, 540))
	f.Seq = 1
	for _, v := range ViewNames {
		got, name := h.View(f, v, 0)
		if got != f || name != ViewFull {
			t.Errorf("%s: view %q", v, name)
		}
		got, name = h.View(f, v, 320)
		if got != f.Small(320) || name != ViewFull {
			t.Errorf("%s at 320: view %q", v, name)
		}
	}
	if v := h.Views(); len(v) != 1 || v[0] != ViewFull {
		t.Errorf("views %v", v)
	}
	if _, ok := h.Calibration(); ok {
		t.Error("calibrated")
	}
	// A calibration of another camera is not valid.
	c := synthCalibration(t, newSynthCamera())
	c.Device = "other"
	h.LoadCalibration(c)
	if _, name := h.View(f, ViewToolhead, 0); name != ViewFull {
		t.Errorf("another camera: view %q", name)
	}
	// An unknown name is the whole picture.
	h.LoadCalibration(synthCalibration(t, newSynthCamera()))
	if _, name := h.View(f, "fisheye", 0); name != ViewFull {
		t.Errorf("unknown view: %q", name)
	}
}

// With a calibration, the views have the size of the frame (or of Small), and a frame keeps
// each view.
func TestViews(t *testing.T) {
	scene := &fakeScene{nozzle: [3]float64{140, 140, 20},
		part:  Box{Min: [3]float64{110, 110, 0}, Max: [3]float64{170, 170, 45}},
		first: Box{Min: [3]float64{110, 110, 0}, Max: [3]float64{170, 170, 0.3}}}
	h := viewHub(t, scene)
	if v := h.Views(); len(v) != 4 {
		t.Errorf("views %v", v)
	}
	f := picFrame(texturePicture(960, 540))
	f.Seq, f.Time = 1, time.Now()
	for _, v := range ViewNames[1:] {
		for _, w := range []int{0, 480} {
			got, name := h.View(f, v, w)
			if name != v {
				t.Fatalf("%s: view %q", v, name)
			}
			want := image.Pt(960, 540)
			if w > 0 {
				want = image.Pt(480, 270)
			}
			if got.Size() != want {
				t.Errorf("%s at %d: size %v", v, w, got.Size())
			}
			if again, _ := h.View(f, v, w); again != got {
				t.Errorf("%s at %d: a new picture each time", v, w)
			}
		}
	}
	// The part view masks: the corners of the picture are dark.
	part, _ := h.View(f, ViewPart, 0)
	pp := part.Picture().(*image.YCbCr)
	if pp.Y[0] > 100 {
		t.Errorf("corner of the part view: Y %d", pp.Y[0])
	}
	// No print: the part views are the whole picture.
	scene.noPrint = true
	f2 := picFrame(texturePicture(960, 540))
	f2.Seq = 2
	if _, name := h.View(f2, ViewPart, 0); name != ViewFull {
		t.Errorf("no print: view %q", name)
	}
	if v := h.Views(); len(v) != 2 {
		t.Errorf("no print: views %v", v)
	}
}

// TestViewShots writes the views of a drawn scene to the folder of PRINTDECK_VIEW_SHOTS, to look at
// them: a bed with a grid of 20 mm, a part of 60 x 40 x 30 mm, and the nozzle on top of it.
func TestViewShots(t *testing.T) {
	dir := os.Getenv("PRINTDECK_VIEW_SHOTS")
	if dir == "" {
		t.Skip("PRINTDECK_VIEW_SHOTS is not set")
	}
	cam := newSynthCamera()
	pic := texturePicture(960, 540)
	for i := range pic.Y {
		pic.Y[i] = pic.Y[i]/4 + 70 // a gray wall with a texture
	}
	for i := range pic.Cb {
		pic.Cb[i], pic.Cr[i] = 128, 128
	}
	dot := func(p [3]float64, y byte, r int) {
		u, v := cam.project(p)
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				x, yy := int(u)+dx, int(v)+dy
				if x >= 0 && x < 960 && yy >= 0 && yy < 540 {
					pic.Y[yy*pic.YStride+x] = y
				}
			}
		}
	}
	// The bed, with a grid.
	for x := 0.0; x <= 280; x += 0.5 {
		for y := 0.0; y <= 280; y += 0.5 {
			l := byte(40)
			if math.Mod(x, 20) < 0.6 || math.Mod(y, 20) < 0.6 {
				l = 200
			}
			dot([3]float64{x, y, 0}, l, 0)
		}
	}
	// The part, and the nozzle on top of it.
	part := Box{Min: [3]float64{110, 120, 0}, Max: [3]float64{170, 160, 30}}
	for x := part.Min[0]; x <= part.Max[0]; x += 0.5 {
		for y := part.Min[1]; y <= part.Max[1]; y += 0.5 {
			for z := part.Min[2]; z <= part.Max[2]; z += 0.5 {
				if x == part.Min[0] || y == part.Min[1] || z == part.Max[2] || x+0.5 > part.Max[0] {
					dot([3]float64{x, y, z}, byte(120+z*3), 0)
				}
			}
		}
	}
	for z := 30.0; z < 60; z += 0.5 {
		dot([3]float64{140, 140, z}, 230, 3)
	}
	m := 5.0
	scene := &fakeScene{nozzle: [3]float64{140, 140, 30},
		part:  Box{Min: [3]float64{part.Min[0] - m, part.Min[1] - m, 0}, Max: [3]float64{part.Max[0] + m, part.Max[1] + m, part.Max[2] + m}},
		first: Box{Min: [3]float64{part.Min[0] - m, part.Min[1] - m, 0}, Max: [3]float64{part.Max[0] + m, part.Max[1] + m, 0.3}}}
	h := viewHub(t, scene)
	f := picFrame(pic)
	f.Seq, f.Time = 1, time.Now()
	for _, v := range ViewNames {
		out, _ := h.View(f, v, 0)
		if err := os.WriteFile(filepath.Join(dir, "view-"+v+".jpg"), out.JPEG(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
