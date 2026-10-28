package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"

	"printdeck/internal/camera"
)

// The calibration of the camera with a print, for the pictures of the manual: a drawn picture of
// a bed that moves in Y with the 5 towers on it, from a camera low and in front of the bed.

// demoCalibAxis is the travel of X and Y of the demo printer: a TAZ Workhorse.
var demoCalibAxis = [2][2]float64{{-50, -17}, {293, 308}}

// demoCalibProject is the camera of the demo picture: the pixel of 1280 x 720 of a place in the
// frame of the printer (camera.Calibration.Frame).
func demoCalibProject(p [3]float64) (float64, float64) {
	eye := [3]float64{146, -420, 110}
	fwd := norm([3]float64{0, 420, -110})
	right := norm([3]float64{fwd[1], -fwd[0], 0})
	down := [3]float64{fwd[1]*right[2] - fwd[2]*right[1], fwd[2]*right[0] - fwd[0]*right[2], fwd[0]*right[1] - fwd[1]*right[0]}
	d := [3]float64{p[0] - eye[0], p[1] - eye[1], p[2] - eye[2]}
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	z := dot(fwd, d)
	return 640 + 1150*dot(right, d)/z, 360 + 1150*dot(down, d)/z
}

func norm(a [3]float64) [3]float64 {
	l := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
	return [3]float64{a[0] / l, a[1] / l, a[2] / l}
}

// demoTowerPicture draws the bed of 280 x 280 mm with a grid of 20 mm and the towers at places,
// with the bed at the toolhead Y bedY.
func demoTowerPicture(places [][2]float64, bedY float64) *camera.Frame {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for i := range img.Pix {
		img.Pix[i] = []byte{60, 64, 70, 255}[i%4]
	}
	at := func(x, y, z float64) image.Point {
		u, v := demoCalibProject([3]float64{x, y - bedY, z})
		return image.Pt(int(u+0.5), int(v+0.5))
	}
	fillPoly(img, []image.Point{at(0, 0, 0), at(280, 0, 0), at(280, 280, 0), at(0, 280, 0)}, color.RGBA{40, 40, 44, 255})
	grid := color.RGBA{150, 150, 150, 255}
	for g := 0.0; g <= 280; g += 20 {
		line(img, at(g, 0, 0), at(g, 280, 0), grid)
		line(img, at(0, g, 0), at(280, g, 0), grid)
	}
	// The towers, the back ones first: the box, then the cap, with a lit top.
	h, ch, top := camera.TowerSide/2, camera.TowerCapSide/2, camera.TowerHeight
	for _, i := range []int{2, 3, 4, 0, 1} {
		p := places[i]
		box := func(r, z0, z1 float64, side, lid color.RGBA) {
			x0, x1, y0, y1 := p[0]-r, p[0]+r, p[1]-r, p[1]+r
			fillPoly(img, []image.Point{at(x0, y0, z0), at(x1, y0, z0), at(x1, y0, z1), at(x0, y0, z1)}, side)
			fillPoly(img, []image.Point{at(x0, y0, z1), at(x1, y0, z1), at(x1, y1, z1), at(x0, y1, z1)}, lid)
		}
		box(h, 0, top, color.RGBA{230, 110, 30, 255}, color.RGBA{250, 160, 90, 255})
		box(ch, top, camera.TowerTop, color.RGBA{200, 90, 20, 255}, color.RGBA{255, 220, 180, 255})
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	f, _ := camera.FrameOf(b.Bytes())
	return f
}

// fillPoly fills the convex polygon pts.
func fillPoly(img *image.RGBA, pts []image.Point, col color.RGBA) {
	r := image.Rectangle{Min: pts[0], Max: pts[0]}
	for _, p := range pts {
		r = r.Union(image.Rectangle{Min: p, Max: p.Add(image.Pt(1, 1))})
	}
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			pos, neg := false, false
			for i, a := range pts {
				b := pts[(i+1)%len(pts)]
				c := (b.X-a.X)*(y-a.Y) - (b.Y-a.Y)*(x-a.X)
				pos, neg = pos || c > 0, neg || c < 0
			}
			if !(pos && neg) {
				img.SetRGBA(x, y, col)
			}
		}
	}
}

// line draws a line of 1 pixel from a to b.
func line(img *image.RGBA, a, b image.Point, col color.RGBA) {
	n := max(abs(b.X-a.X), abs(b.Y-a.Y), 1)
	for i := 0; i <= n; i++ {
		p := image.Pt(a.X+(b.X-a.X)*i/n, a.Y+(b.Y-a.Y)*i/n)
		if p.In(img.Rect) {
			img.SetRGBA(p.X, p.Y, col)
		}
	}
}

// demoCalibration opens the calibration page for the manual: "calibrate" is the two ways,
// "calibrate-tower" a mark of the top of tower 2, and "calibrate-fit" the result with the towers
// of the fit over the picture.
func (a *App) demoCalibration(name string) {
	cam, in := a.shownCam()
	fl := &calibFlow{cam: cam, rotate: in.Rotate, phase: calibChoose, bedY: true}
	a.calib = fl
	a.pages = []page{pageMachine, pageSettings, pageCamera, pageCalibrate}
	if name == "calibrate" {
		return
	}
	alo, ahi := camera.PrintArea(demoCalibAxis[0], demoCalibAxis[1])
	fl.kind, fl.towers, fl.marks = camera.KindTowers, camera.TowerPlaces(alo, ahi), camera.TowerMarks(true)
	for _, m := range fl.marks {
		fl.points = append(fl.points, m.Place(fl.towers))
	}
	fl.beds = camera.TowerBedPlaces(demoCalibAxis[0][1], demoCalibAxis[1][1], alo[1], ahi[1])
	fl.bedAt = fl.beds
	for b := range fl.shots {
		fl.shots[b] = demoTowerPicture(fl.towers, fl.beds[b])
	}
	fl.status = cam.Status()
	fl.first, fl.size = fl.status, fl.shots[0].Size()
	fit := camera.Calibration{BedY: true}
	if name == "calibrate-tower" {
		fl.i, fl.phase, fl.frame = 3, calibTap, fl.shots[0]
		fl.place = fl.points[fl.i]
		u, v := demoCalibProject(fit.Frame(fl.place, fl.bedAt[0]))
		fl.tap, fl.tapped = [2]float64{u, v}, true
		return
	}
	for i, m := range fl.marks {
		u, v := demoCalibProject(fit.Frame(fl.points[i], fl.bedAt[m.Bed]))
		fl.places = append(fl.places, fit.Frame(fl.points[i], fl.bedAt[m.Bed]))
		fl.pix = append(fl.pix, [2]float64{u + float64(i%3) - 1, v + float64(i%2)})
		fl.pixBed = append(fl.pixBed, m.Bed)
	}
	m, rms, err := camera.Fit(fl.places, fl.pix, fl.size.X, fl.size.Y)
	if err != nil {
		return
	}
	fl.i, fl.phase, fl.frame = len(fl.points), calibDone, fl.shots[1]
	fl.result = &camera.Calibration{Device: fl.status.Device.ID, Width: fl.size.X, Height: fl.size.Y,
		Kind: camera.KindTowers, BedY: true, P: m, Error: rms, Points: len(fl.places), Time: a.Now()}
}
