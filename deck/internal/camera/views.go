package camera

import (
	"image"
	"math"
	"sort"
	"strconv"
	"time"
)

// The views of a calibrated camera: parts of the picture that follow the printer.
//
//	full        the whole picture
//	toolhead    a crop that follows the nozzle: about 60 x 60 mm around it
//	firstlayer  the footprint of the first layer of the print, and around it a dark blur
//	part        the box of the printed part, from the bed to its top, and around it a dark blur
//
// Each view has the shape of the whole picture, and the same size: a crop is scaled up. The
// views come from the frames of the one capture. A frame keeps the view of each name and width
// that a viewer asks for, so the viewers of one view and width share one picture and one JPEG.
// With no valid calibration, or with no place for a view (no print yet), a view is the whole
// picture. With a calibration of a bed that moves in Y (Calibration.BedY), the views of the print
// project its boxes with the bed at the live Y of the toolhead, so they also need the nozzle.

// The names of the views.
const (
	ViewFull       = "full"
	ViewToolhead   = "toolhead"
	ViewFirstLayer = "firstlayer"
	ViewPart       = "part"
)

// ViewNames are the names of the views, in the order of the apps.
var ViewNames = []string{ViewFull, ViewToolhead, ViewFirstLayer, ViewPart}

// Box is a box in the places of the printer, in mm.
type Box struct {
	Min, Max [3]float64
}

// Scene gives the places of the views. The methods are called for each new frame of a view, so
// they must be fast.
type Scene interface {
	// Nozzle returns the place of the nozzle tip, or false when it is not known, for example
	// before the homing. Its Y is also the place of a bed that moves in Y.
	Nozzle() ([3]float64, bool)
	// Part returns the box of the part of the print now, or of the last print.
	Part() (Box, bool)
	// FirstLayer returns the box of the first layer of the print now, or of the last print.
	FirstLayer() (Box, bool)
}

// The sizes of the views.
const (
	toolheadBox  = 60.0 // the toolhead view shows a box of this many mm around the nozzle
	minCropShare = 6    // a crop is at least 1/minCropShare of the width of the picture
	cropPad      = 0.06 // the space around the box of the part, as a share of its size
	// toolheadTau is the time constant of the motion of the toolhead crop, so the crop moves
	// smoothly and does not follow each small move of the nozzle.
	toolheadTau = 300 * time.Millisecond
)

// viewPlan is a crop of the frame, and the area outside the mask gets the dark blur. The
// places are in pixels of the frame.
type viewPlan struct {
	crop [4]float64   // x0, y0, x1, y1
	mask [][2]float64 // a convex polygon, or nil for no mask
}

// project is the projection of the places to the pixels of a frame of size sz.
type project func(p [3]float64) (x, y float64, ok bool)

// boxCorners are the 8 corners of a box.
func boxCorners(b Box) [][3]float64 {
	var c [][3]float64
	for i := range 8 {
		p := b.Min
		if i&1 != 0 {
			p[0] = b.Max[0]
		}
		if i&2 != 0 {
			p[1] = b.Max[1]
		}
		if i&4 != 0 {
			p[2] = b.Max[2]
		}
		c = append(c, p)
	}
	return c
}

// projectAll projects the places, and returns false when one is behind the camera.
func projectAll(proj project, pts [][3]float64) ([][2]float64, bool) {
	out := make([][2]float64, 0, len(pts))
	for _, p := range pts {
		x, y, ok := proj(p)
		if !ok {
			return nil, false
		}
		out = append(out, [2]float64{x, y})
	}
	return out, true
}

// hull is the convex hull of the points, counterclockwise on the screen (y down: clockwise).
func hull(pts [][2]float64) [][2]float64 {
	p := append([][2]float64(nil), pts...)
	sort.Slice(p, func(i, j int) bool {
		if p[i][0] != p[j][0] {
			return p[i][0] < p[j][0]
		}
		return p[i][1] < p[j][1]
	})
	if len(p) < 3 {
		return p
	}
	cr := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	var h [][2]float64
	for _, q := range p {
		for len(h) >= 2 && cr(h[len(h)-2], h[len(h)-1], q) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, q)
	}
	lower := len(h) + 1
	for i := len(p) - 2; i >= 0; i-- {
		q := p[i]
		for len(h) >= lower && cr(h[len(h)-2], h[len(h)-1], q) <= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, q)
	}
	return h[:len(h)-1]
}

// fitCrop returns a crop with the shape of the frame of size sz around the center (cx, cy), at
// least w wide and h high, in the frame. A crop larger than the frame is the frame.
func fitCrop(cx, cy, w, h float64, sz image.Point) [4]float64 {
	W, H := float64(sz.X), float64(sz.Y)
	w = max(w, h*W/H, W/minCropShare)
	w = min(w, W)
	h = w * H / W
	x0 := min(max(cx-w/2, 0), W-w)
	y0 := min(max(cy-h/2, 0), H-h)
	return [4]float64{x0, y0, x0 + w, y0 + h}
}

// bounds is the bounding rectangle of the points: x0, y0, x1, y1.
func bounds(pts [][2]float64) [4]float64 {
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		b[0], b[1] = min(b[0], p[0]), min(b[1], p[1])
		b[2], b[3] = max(b[2], p[0]), max(b[3], p[1])
	}
	return b
}

// toolheadTarget is the crop of the toolhead view for the nozzle at n, before the smoothing:
// the projection of a box of 60 x 60 mm around the nozzle, at its height.
func toolheadTarget(proj project, n [3]float64, sz image.Point) ([4]float64, bool) {
	cx, cy, ok := proj(n)
	if !ok {
		return [4]float64{}, false
	}
	d := toolheadBox / 2
	corners := [][3]float64{{n[0] - d, n[1] - d, n[2]}, {n[0] + d, n[1] - d, n[2]},
		{n[0] + d, n[1] + d, n[2]}, {n[0] - d, n[1] + d, n[2]}}
	pts, ok := projectAll(proj, corners)
	if !ok {
		return [4]float64{}, false
	}
	b := bounds(pts)
	// The crop is centered on the nozzle, so it is as wide as twice the larger side.
	w := 2 * max(cx-b[0], b[2]-cx)
	h := 2 * max(cy-b[1], b[3]-cy)
	return fitCrop(cx, cy, w, h, sz), true
}

// boxTarget is the crop and the mask of a box: the bounding rectangle of the projected box with a
// small space around it, and the silhouette of the box.
func boxTarget(proj project, b Box, sz image.Point) (crop [4]float64, mask [][2]float64, ok bool) {
	pts, ok := projectAll(proj, boxCorners(b))
	if !ok {
		return crop, nil, false
	}
	mask = hull(pts)
	r := bounds(mask)
	w, h := r[2]-r[0], r[3]-r[1]
	pad := cropPad * max(w, h)
	return fitCrop((r[0]+r[2])/2, (r[1]+r[3])/2, w+2*pad, h+2*pad, sz), mask, true
}

// smoothCrop moves the crop prev toward target, with the time constant toolheadTau, after the time
// dt. A long time (or no crop before) goes to the target at once.
func smoothCrop(prev, target [4]float64, dt time.Duration, sz image.Point) [4]float64 {
	if prev == ([4]float64{}) || dt <= 0 || dt > 2*time.Second {
		return target
	}
	a := 1 - math.Exp(-float64(dt)/float64(toolheadTau))
	pc := [3]float64{(prev[0] + prev[2]) / 2, (prev[1] + prev[3]) / 2, prev[2] - prev[0]}
	tc := [3]float64{(target[0] + target[2]) / 2, (target[1] + target[3]) / 2, target[2] - target[0]}
	for i := range pc {
		pc[i] += a * (tc[i] - pc[i])
	}
	return fitCrop(pc[0], pc[1], pc[2], 0, sz)
}

// SetScene sets the places of the printer for the views.
func (h *Hub) SetScene(s Scene) {
	h.mu.Lock()
	h.scene = s
	h.mu.Unlock()
}

// LoadCalibrations sets the calibrations from their file, and does not call OnCalibration. Each
// calibration is for the camera of its Device.
func (h *Hub) LoadCalibrations(cs []*Calibration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range cs {
		if c != nil {
			h.setCalLocked(c.Device, c)
		}
	}
}

// LoadCalibration sets the calibration of the camera c.Device, and does not call OnCalibration.
// nil removes the calibration of the main camera.
func (h *Hub) LoadCalibration(c *Calibration) {
	if c == nil {
		h.mu.Lock()
		h.setCalLocked(h.roleLocked(RoleMain), nil)
		h.mu.Unlock()
		return
	}
	h.LoadCalibrations([]*Calibration{c})
}

// setCalLocked sets the calibration of the camera id, or removes it with nil.
func (h *Hub) setCalLocked(id string, c *Calibration) {
	if c == nil {
		delete(h.cals, id)
	} else {
		h.cals[id] = c
	}
	h.calGen++
	if cam := h.cams[id]; cam != nil {
		cam.thCrop = [4]float64{}
	}
}

// calsLocked returns the calibrations, in the order of the IDs of their cameras.
func (h *Hub) calsLocked() []*Calibration {
	ids := make([]string, 0, len(h.cals))
	for id := range h.cals {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Calibration, 0, len(ids))
	for _, id := range ids {
		out = append(out, h.cals[id])
	}
	return out
}

// SetCalibration sets the calibration of the main camera, or clears it with nil, and calls
// OnCalibration.
func (h *Hub) SetCalibration(c *Calibration) { h.Role(RoleMain).SetCalibration(c) }

// Calibration returns the calibration of the main camera. See Cam.Calibration.
func (h *Hub) Calibration() (*Calibration, bool) { return h.Role(RoleMain).Calibration() }

// Views returns the views of the main camera. See Cam.Views.
func (h *Hub) Views() []string { return h.Role(RoleMain).Views() }

// View returns a view of a frame of the camera of the frame. See Cam.View.
func (h *Hub) View(f *Frame, view string, w int) (*Frame, string) {
	return h.Role(RoleMain).View(f, view, w)
}

// SetCalibration sets the calibration of the camera, or clears it with nil, and calls
// OnCalibration with all calibrations. The calibration gets the ID of the camera.
func (c *Cam) SetCalibration(cal *Calibration) {
	if c.id == "" {
		return
	}
	h := c.h
	h.mu.Lock()
	if cal != nil {
		cal.Device = c.id
	}
	h.setCalLocked(c.id, cal)
	save, all := h.OnCalibration, h.calsLocked()
	h.mu.Unlock()
	if save != nil {
		h.saveMu.Lock()
		save(all)
		h.saveMu.Unlock()
	}
}

// Calibration returns the calibration of the camera, or nil, and reports that it is valid: the
// same device, and the same size of the pictures of the camera.
func (c *Cam) Calibration() (*Calibration, bool) {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	cal := h.cals[c.id]
	if cal == nil {
		return nil, false
	}
	d, _, ok := c.deviceLocked()
	return cal, ok && cal.ValidFor(d, h.formats[d.ID])
}

// sceneAndCalibration returns the places of the printer, the calibration, and the turn of the
// camera, or false when the views have no calibration.
func (c *Cam) sceneAndCalibration() (Scene, *Calibration, int, bool) {
	cal, ok := c.Calibration()
	c.h.mu.Lock()
	scene, rotate := c.h.scene, c.h.settings.Camera(c.id).Rotate
	c.h.mu.Unlock()
	if !ok || scene == nil {
		return nil, nil, 0, false
	}
	return scene, cal, rotate, true
}

// projectAt is the projection of the calibration to the pixels of a frame of size sz with the turn
// rotate, with the bed at the toolhead Y bedY.
func projectAt(cal *Calibration, bedY float64, sz image.Point, rotate int) project {
	return func(p [3]float64) (float64, float64, bool) { return cal.ProjectAt(p, bedY, sz.X, sz.Y, rotate) }
}

// Views returns the names of the views that show a part of the picture now, with full first.
// The other views are the whole picture now.
func (c *Cam) Views() []string {
	out := []string{ViewFull}
	scene, cal, _, ok := c.sceneAndCalibration()
	if !ok {
		return out
	}
	_, nozzle := scene.Nozzle()
	if nozzle {
		out = append(out, ViewToolhead)
	}
	if cal.BedY && !nozzle {
		return out
	}
	if _, ok := scene.FirstLayer(); ok {
		out = append(out, ViewFirstLayer)
	}
	if _, ok := scene.Part(); ok {
		out = append(out, ViewPart)
	}
	return out
}

// plan returns the plan of a view for the frame f, or nil. The frame keeps it under key: the view
// and the number of the calibration.
func (c *Cam) plan(f *Frame, view, key string) *viewPlan {
	f.mu.Lock()
	p, ok := f.plans[key]
	f.mu.Unlock()
	if ok {
		return p
	}
	p = c.makePlan(f, view)
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.plans[key]; ok {
		return old // another viewer made it first
	}
	if f.plans == nil {
		f.plans = map[string]*viewPlan{}
	}
	f.plans[key] = p
	return p
}

// makePlan makes the plan of a view for the frame f, or returns nil when the view has no place
// now.
func (c *Cam) makePlan(f *Frame, view string) *viewPlan {
	sz := f.Size()
	scene, cal, rotate, ok := c.sceneAndCalibration()
	if !ok || sz.X <= 0 || sz.Y <= 0 {
		return nil
	}
	switch view {
	case ViewToolhead:
		n, ok := scene.Nozzle()
		if !ok {
			return nil
		}
		t, ok := toolheadTarget(projectAt(cal, n[1], sz, rotate), n, sz)
		if !ok {
			return nil
		}
		c.h.mu.Lock()
		defer c.h.mu.Unlock()
		if sz != c.thSize {
			c.thCrop, c.thSize = [4]float64{}, sz
		}
		if f.Seq > c.thSeq || c.thCrop == ([4]float64{}) {
			c.thCrop = smoothCrop(c.thCrop, t, f.Time.Sub(c.thAt), sz)
			c.thSeq, c.thAt = f.Seq, f.Time
		}
		return &viewPlan{crop: c.thCrop}
	case ViewFirstLayer, ViewPart:
		var b Box
		if view == ViewPart {
			b, ok = scene.Part()
		} else {
			b, ok = scene.FirstLayer()
		}
		if !ok {
			return nil
		}
		// The bed is at the Y of the toolhead.
		bedY := 0.0
		if cal.BedY {
			n, ok := scene.Nozzle()
			if !ok {
				return nil
			}
			bedY = n[1]
		}
		crop, mask, ok := boxTarget(projectAt(cal, bedY, sz, rotate), b, sz)
		if !ok {
			return nil
		}
		return &viewPlan{crop: crop, mask: mask}
	}
	return nil
}

// viewKeep is the most view pictures that a frame keeps.
const viewKeep = 8

// viewSize is the size of a view of width w (0 for the size of the frame) of a frame of size sz:
// the size of Small(w).
func viewSize(sz image.Point, w int) (int, int) {
	if w <= 0 {
		return sz.X &^ 1, sz.Y &^ 1
	}
	w = max(64, w/16*16)
	if w >= sz.X {
		return sz.X &^ 1, sz.Y &^ 1
	}
	return w, max(2, (sz.Y*w/sz.X+1)/2*2)
}

// View returns the view of the frame f at the width w (0 for the width of the frame), and the
// name of the view that it shows: full when the view has no place now, or for a name that is not
// a view. The frame keeps the picture, so the viewers of one view and width share it. The frame
// is a frame of the camera.
func (c *Cam) View(f *Frame, view string, w int) (*Frame, string) {
	h := c.h
	full := func() (*Frame, string) {
		if w > 0 {
			return f.ForWidth(w), ViewFull
		}
		return f, ViewFull
	}
	if view == ViewFull || view == "" || f == nil {
		return full()
	}
	h.mu.Lock()
	gen := strconv.Itoa(h.calGen)
	h.mu.Unlock()
	p := c.plan(f, view, view+"#"+gen)
	if p == nil {
		return full()
	}
	ow, oh := viewSize(f.Size(), w)
	key := view + "#" + gen + "/" + strconv.Itoa(ow)
	f.mu.Lock()
	v, ok := f.views[key]
	f.mu.Unlock()
	if ok {
		return v, view
	}
	pic := f.picture()
	if pic == nil {
		return full()
	}
	v = picFrame(renderView(pic, *p, ow, oh))
	v.Seq, v.Time, v.blank, v.cam = f.Seq, f.Time, f.blank, f.cam
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.views[key]; ok {
		return old, view
	}
	if f.views == nil {
		f.views = map[string]*Frame{}
	}
	f.views[key] = v
	f.viewOrder = append(f.viewOrder, key)
	if len(f.viewOrder) > viewKeep {
		delete(f.views, f.viewOrder[0])
		f.viewOrder = f.viewOrder[1:]
	}
	return v, view
}

// Crop returns the part of the frame around (cx, cy) that is cw pixels wide, with the shape of the
// frame and in the frame, at the width w. It also returns the crop: x0, y0, x1, y1 in pixels of the
// frame. The deck UI shows a crop to place a tap on the picture more exactly.
func (f *Frame) Crop(cx, cy, cw float64, w int) (*Frame, [4]float64) {
	c := fitCrop(cx, cy, cw, 0, f.Size())
	pic := f.picture()
	if pic == nil {
		return f, c
	}
	ow, oh := viewSize(f.Size(), w)
	v := picFrame(cropScale(pic, c, ow, oh))
	v.Seq, v.Time, v.cam = f.Seq, f.Time, f.cam
	return v, c
}
