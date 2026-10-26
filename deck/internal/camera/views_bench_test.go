package camera

import (
	"testing"
	"time"
)

// benchView makes a view of a new frame of 960 x 540 each time, as the stream of one viewer
// does, and encodes its JPEG when encode is true.
func benchView(b *testing.B, view string, encode bool) {
	scene := &fakeScene{nozzle: [3]float64{140, 140, 20},
		part:  Box{Min: [3]float64{110, 110, 0}, Max: [3]float64{170, 170, 45}},
		first: Box{Min: [3]float64{110, 110, 0}, Max: [3]float64{170, 170, 0.3}}}
	h := viewHub(nil, scene)
	cam := newSynthCamera()
	pts := CalibrationPoints([3]float64{0, 0, 0}, [3]float64{280, 280, 285})
	pix := make([][2]float64, len(pts))
	for i, p := range pts {
		pix[i][0], pix[i][1] = cam.project(p)
	}
	m, _, _ := Fit(pts, pix, 960, 540)
	h.LoadCalibration(&Calibration{Device: "test", InW: 960, InH: 540, Width: 960, Height: 540, P: m})
	pic := texturePicture(960, 540)
	b.ResetTimer()
	for i := range b.N {
		f := picFrame(pic)
		f.Seq, f.Time = uint64(i+1), time.Now()
		v, name := h.View(f, view, 0)
		if name != view {
			b.Fatalf("view %q", name)
		}
		if encode {
			v.JPEG()
		}
	}
}

func BenchmarkViewFull(b *testing.B)           { benchView(b, ViewFull, true) }
func BenchmarkViewToolhead(b *testing.B)       { benchView(b, ViewToolhead, true) }
func BenchmarkViewFirstLayer(b *testing.B)     { benchView(b, ViewFirstLayer, true) }
func BenchmarkViewPart(b *testing.B)           { benchView(b, ViewPart, true) }
func BenchmarkViewToolheadNoJPEG(b *testing.B) { benchView(b, ViewToolhead, false) }
func BenchmarkViewFirstNoJPEG(b *testing.B)    { benchView(b, ViewFirstLayer, false) }
func BenchmarkViewPartNoJPEG(b *testing.B)     { benchView(b, ViewPart, false) }
