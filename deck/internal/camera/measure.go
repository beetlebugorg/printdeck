package camera

import (
	"context"
	"errors"
	"image"
	"math"
	"time"
)

// The measurement of the PTZ map of a camera (MeasurePTZ). The deck gets a frame at the wide
// position, and frames at a few zoom values and pan and tilt values. It finds the crop of each
// frame in the wide frame by image matching on small gray copies: the normalized cross-correlation
// of the frame, scaled to each size of a range, at each place in the wide frame. A coarse search
// on a copy 120 pixels wide finds the size and the place, and a fine search on a copy 480 pixels
// wide refines them to a fraction of a pixel. The measurement runs once, for a few seconds.

// The widths of the gray copies of the match.
const (
	matchCoarseW = 120
	matchFineW   = 480
	matchRatio   = matchFineW / matchCoarseW
)

// minMatchScore is the least correlation of a good match.
const minMatchScore = 0.6

// grayPic is a gray picture with float samples.
type grayPic struct {
	w, h int
	p    []float32
}

func (g *grayPic) at(x, y int) float32 { return g.p[y*g.w+x] }

// areaTaps are the source samples of each output sample of a line, with the share of each.
type areaTaps struct {
	start []int
	w     [][]float32
}

// newAreaTaps returns the taps of n output samples over the source range [s0, s1) of a line of
// src samples: each output sample is the mean of the source samples under it.
func newAreaTaps(n int, s0, s1 float64, src int) areaTaps {
	t := areaTaps{start: make([]int, n), w: make([][]float32, n)}
	step := (s1 - s0) / float64(n)
	for i := range n {
		a, b := s0+float64(i)*step, s0+float64(i+1)*step
		a, b = max(0, min(float64(src), a)), max(0, min(float64(src), b))
		first := int(math.Floor(a))
		var ws []float32
		sum := 0.0
		for x := first; float64(x) < b && x < src; x++ {
			cover := min(b, float64(x+1)) - max(a, float64(x))
			if cover <= 0 {
				cover = 0
			}
			ws = append(ws, float32(cover))
			sum += cover
		}
		if sum <= 0 {
			// A range of less than a sample: the nearest sample.
			first = max(0, min(src-1, int(a)))
			ws, sum = []float32{1}, 1
		}
		for j := range ws {
			ws[j] /= float32(sum)
		}
		t.start[i], t.w[i] = first, ws
	}
	return t
}

// resample returns the source rectangle r (x0, y0, x1, y1 in samples) of the plane src of
// sw x sh samples as a gray picture of ow x oh: the mean of the area of each sample.
func resample(src []float32, stride, sw, sh int, r [4]float64, ow, oh int) *grayPic {
	tx := newAreaTaps(ow, r[0], r[2], sw)
	ty := newAreaTaps(oh, r[1], r[3], sh)
	y0 := ty.start[0]
	y1 := ty.start[oh-1] + len(ty.w[oh-1])
	tmp := make([]float32, (y1-y0)*ow)
	for y := y0; y < y1; y++ {
		row := src[y*stride:]
		out := tmp[(y-y0)*ow:]
		for i := range ow {
			s := float32(0)
			for j, w := range tx.w[i] {
				s += w * row[tx.start[i]+j]
			}
			out[i] = s
		}
	}
	g := &grayPic{w: ow, h: oh, p: make([]float32, ow*oh)}
	for i := range oh {
		out := g.p[i*ow:]
		for j, w := range ty.w[i] {
			row := tmp[(ty.start[i]+j-y0)*ow:]
			for x := range ow {
				out[x] += w * row[x]
			}
		}
	}
	return g
}

// grayOf returns the Y plane of the picture as a gray picture w samples wide, with the shape of
// the picture.
func grayOf(pic *image.YCbCr, w int) *grayPic {
	pw, ph := pic.Rect.Dx(), pic.Rect.Dy()
	src := make([]float32, pw*ph)
	for y := range ph {
		row := pic.Y[pic.YOffset(pic.Rect.Min.X, pic.Rect.Min.Y+y):]
		for x := range pw {
			src[y*pw+x] = float32(row[x])
		}
	}
	h := max(2, int(math.Round(float64(w)*float64(ph)/float64(pw))))
	return resample(src, pw, pw, ph, [4]float64{0, 0, float64(pw), float64(ph)}, w, h)
}

// shrink returns the gray picture at the width w.
func (g *grayPic) shrink(w int) *grayPic {
	h := max(2, int(math.Round(float64(w)*float64(g.h)/float64(g.w))))
	return resample(g.p, g.w, g.w, g.h, [4]float64{0, 0, float64(g.w), float64(g.h)}, w, h)
}

// integral is the table of the sums of the samples and of their squares of a gray picture.
type integral struct {
	w      int
	s, sq  []float64
	pic    *grayPic
	stride int
}

func newIntegral(g *grayPic) *integral {
	st := g.w + 1
	in := &integral{w: g.w, s: make([]float64, st*(g.h+1)), sq: make([]float64, st*(g.h+1)), pic: g, stride: st}
	for y := range g.h {
		rs, rq := 0.0, 0.0
		for x := range g.w {
			v := float64(g.at(x, y))
			rs += v
			rq += v * v
			in.s[(y+1)*st+x+1] = in.s[y*st+x+1] + rs
			in.sq[(y+1)*st+x+1] = in.sq[y*st+x+1] + rq
		}
	}
	return in
}

// sums returns the sum of the samples and of their squares in the rectangle at (x, y) of w x h.
func (in *integral) sums(x, y, w, h int) (float64, float64) {
	st := in.stride
	a, b, c, d := y*st+x, y*st+x+w, (y+h)*st+x, (y+h)*st+x+w
	return in.s[d] - in.s[b] - in.s[c] + in.s[a], in.sq[d] - in.sq[b] - in.sq[c] + in.sq[a]
}

// template is a gray copy of a frame scaled to tw samples of width, with its mean subtracted.
type template struct {
	g    *grayPic
	norm float64 // the root of the sum of the squares
}

// makeTemplate scales the zoomed frame z to the width tw. The height is the height of the frame
// at the same scale, rounded down, so the scale is the same in x and y.
func makeTemplate(z *grayPic, tw int) template {
	s := float64(tw) / float64(z.w)
	th := max(2, int(float64(z.h)*s))
	g := resample(z.p, z.w, z.w, z.h, [4]float64{0, 0, float64(z.w), float64(th) / s}, tw, th)
	mean := 0.0
	for _, v := range g.p {
		mean += float64(v)
	}
	mean /= float64(len(g.p))
	n := 0.0
	for i, v := range g.p {
		d := float64(v) - mean
		g.p[i] = float32(d)
		n += d * d
	}
	return template{g: g, norm: math.Sqrt(n)}
}

// ncc returns the normalized cross-correlation of the template at (x, y) of the picture.
func ncc(in *integral, t template, x, y int) float64 {
	g, tg := in.pic, t.g
	s := 0.0
	for j := range tg.h {
		row := g.p[(y+j)*g.w+x:]
		trow := tg.p[j*tg.w:]
		acc := float32(0)
		for i, v := range trow[:tg.w] {
			acc += v * row[i]
		}
		s += float64(acc)
	}
	n := float64(tg.w * tg.h)
	sum, sq := in.sums(x, y, tg.w, tg.h)
	v := sq - sum*sum/n
	if v <= 1e-9 || t.norm <= 1e-9 {
		return 0
	}
	return s / (t.norm * math.Sqrt(v))
}

// cropMatch is a match of a zoomed frame in the wide frame: the place of its top left corner and
// its width, in samples of the gray copy of the wide frame, and the correlation.
type cropMatch struct {
	x, y, tw float64
	score    float64
}

// bestAt returns the best place of the template of width tw in the box of places
// [x0, x1] x [y0, y1], and the scores around it for the sub-sample fit.
func bestAt(in *integral, z *grayPic, tw, x0, x1, y0, y1 int) (cropMatch, [3]float64, [3]float64) {
	t := makeTemplate(z, tw)
	g := in.pic
	x0, y0 = max(0, x0), max(0, y0)
	x1, y1 = min(g.w-t.g.w, x1), min(g.h-t.g.h, y1)
	best := cropMatch{score: math.Inf(-1)}
	if x1 < x0 || y1 < y0 {
		return best, [3]float64{}, [3]float64{}
	}
	bx, by := x0, y0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if s := ncc(in, t, x, y); s > best.score {
				best.score, bx, by = s, x, y
			}
		}
	}
	best.x, best.y, best.tw = float64(bx), float64(by), float64(tw)
	var sx, sy [3]float64
	for d := -1; d <= 1; d++ {
		sx[d+1], sy[d+1] = best.score, best.score
		if x := bx + d; d != 0 && x >= 0 && x <= g.w-t.g.w {
			sx[d+1] = ncc(in, t, x, by)
		}
		if y := by + d; d != 0 && y >= 0 && y <= g.h-t.g.h {
			sy[d+1] = ncc(in, t, bx, y)
		}
	}
	return best, sx, sy
}

// peak returns the place of the top of the parabola through the scores at -1, 0 and 1, from -0.5
// to 0.5.
func peak(s [3]float64) float64 {
	d := s[0] - 2*s[1] + s[2]
	if d >= 0 {
		return 0
	}
	return max(-0.5, min(0.5, 0.5*(s[0]-s[2])/d))
}

// findCrop finds the crop of the zoomed frame z in the wide frame: the zoom factor from kLo to kHi,
// and the center within rad of (cu, cv). The places are in normalized coordinates of the wide
// frame. wide and z are gray copies matchFineW samples wide.
func findCrop(wide, z *grayPic, kLo, kHi, cu, cv, rad float64) (u, v, k, score float64) {
	coarse, zc := wide.shrink(matchCoarseW), z.shrink(matchCoarseW)
	ic := newIntegral(coarse)
	W, H := float64(coarse.w), float64(coarse.h)
	twHi := min(coarse.w, int(math.Ceil(W/max(1, kLo))))
	twLo := max(8, int(math.Floor(W/kHi)))
	best := cropMatch{score: math.Inf(-1)}
	for tw := twLo; tw <= twHi; tw++ {
		th := float64(tw) * H / W
		x0, x1 := int((cu-rad)*W-float64(tw)/2), int(math.Ceil((cu+rad)*W-float64(tw)/2))
		y0, y1 := int((cv-rad)*H-th/2), int(math.Ceil((cv+rad)*H-th/2))
		if m, _, _ := bestAt(ic, zc, tw, x0, x1, y0, y1); m.score > best.score {
			best = m
		}
	}
	if math.IsInf(best.score, -1) {
		return 0, 0, 0, 0
	}
	// The fine search around the coarse match.
	in := newIntegral(wide)
	Wf, Hf := float64(wide.w), float64(wide.h)
	ctw := int(best.tw) * matchRatio
	cx, cy := int(best.x)*matchRatio, int(best.y)*matchRatio
	const near = 6
	type fine struct {
		m      cropMatch
		sx, sy [3]float64
	}
	var all []fine
	bi := -1
	for tw := max(8, ctw-near); tw <= min(wide.w, ctw+near); tw++ {
		m, sx, sy := bestAt(in, z, tw, cx-near, cx+near, cy-near, cy+near)
		all = append(all, fine{m, sx, sy})
		if bi < 0 || m.score > all[bi].m.score {
			bi = len(all) - 1
		}
	}
	f := all[bi]
	tw := f.m.tw
	if bi > 0 && bi < len(all)-1 {
		tw += peak([3]float64{all[bi-1].m.score, f.m.score, all[bi+1].m.score})
	}
	x := f.m.x + peak(f.sx)
	y := f.m.y + peak(f.sy)
	k = Wf / tw
	u = (x + tw/2) / Wf
	v = (y + tw*Hf/Wf/2) / Hf
	return u, v, k, f.m.score
}

// ErrNoPTZ is the error of a measurement of a camera with no zoom control.
var ErrNoPTZ = errors.New("camera: the camera has no zoom control")

// errNoMatch is the error of a measurement with frames that do not match.
var errNoMatch = errors.New("the zoomed pictures do not match the wide picture: aim the camera at the printer, with light, and measure again")

// ptzSettle is the time from the report of a new PTZ position to the frame of the measurement: a
// camera sends a few frames of the old crop. The tests make it short.
var ptzSettle = 700 * time.Millisecond

// MeasurePTZ measures the PTZ map of the camera id (PTZMap), saves it in the settings of the
// camera, and returns it. progress gets the number of the frames taken and the number of all
// frames, or is nil. The camera goes through the positions of the measurement, and then back to
// its position. It needs a still scene with detail in the picture.
func (h *Hub) MeasurePTZ(ctx context.Context, id string, progress func(done, all int)) (*PTZMap, error) {
	c := h.Cam(id)
	stop := c.Watch()
	defer stop()
	ctl, err := c.waitPTZ(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	prev, hadPrev := h.follow[id]
	rotate := h.settings.Camera(id).Rotate
	h.mu.Unlock()
	defer func() {
		if hadPrev {
			h.Follow(id, &prev)
		} else {
			h.Follow(id, nil)
		}
	}()
	wide := ctl.Wide()
	zooms := []int{}
	for i := 1; i <= 4; i++ {
		z := ctl.Zoom.Clamp(ctl.Zoom.Min + (ctl.Zoom.Max-ctl.Zoom.Min)*i/4)
		if z > ctl.Zoom.Min && (len(zooms) == 0 || z > zooms[len(zooms)-1]) {
			zooms = append(zooms, z)
		}
	}
	all := 1 + len(zooms)
	if ctl.Pan.Valid() {
		all += 2
	}
	if ctl.Tilt.Valid() {
		all += 2
	}
	done := 0
	shot := func(p PTZPos) (*grayPic, error) {
		g, err := c.ptzShot(ctx, ctl, p)
		if err == nil {
			done++
			if progress != nil {
				progress(done, max(all, done))
			}
		}
		return g, err
	}
	wg, err := shot(wide)
	if err != nil {
		return nil, err
	}
	m := &PTZMap{Rotate: ValidTurn(rotate), Zoom: [][2]float64{{float64(ctl.Zoom.Min), 1}},
		PanZero: wide.Pan, TiltZero: wide.Tilt}
	type sample struct {
		z      int
		k      float64
		cu, cv float64
	}
	var samples []sample
	kPrev := 1.0
	for _, z := range zooms {
		g, err := shot(PTZPos{Zoom: z, Pan: wide.Pan, Tilt: wide.Tilt})
		if err != nil {
			return nil, err
		}
		u, v, k, score := findCrop(wg, g, max(1, kPrev*0.95), 8, 0.5, 0.5, 0.25)
		if score < minMatchScore {
			return nil, errNoMatch
		}
		samples = append(samples, sample{z, k, u, v})
		m.Zoom = append(m.Zoom, [2]float64{float64(z), max(k, kPrev)})
		kPrev = max(k, kPrev)
	}
	if kPrev < 1.05 {
		return nil, errors.New("the zoom does not change the picture")
	}
	// The pan and the tilt at the zoom nearest to 2×, where the crop can move by a quarter of the
	// wide frame to each side.
	at := samples[0]
	for _, s := range samples {
		if math.Abs(s.k-2) < math.Abs(at.k-2) {
			at = s
		}
	}
	axis := func(r PTZRange, zero int, set func(p *PTZPos, v int)) ([2]float64, error) {
		d := max(r.Step, (r.Max-r.Min)/20)
		var out [2]float64
		for try := 0; try < 5; try++ {
			hi, lo := r.Clamp(zero+d), r.Clamp(zero-d)
			if hi == lo {
				return out, nil
			}
			var cs [2][2]float64
			for i, val := range []int{hi, lo} {
				p := PTZPos{Zoom: at.z, Pan: wide.Pan, Tilt: wide.Tilt}
				set(&p, val)
				if try > 0 {
					all++
				}
				g, err := shot(p)
				if err != nil {
					return out, err
				}
				u, v, _, score := findCrop(wg, g, at.k*0.97, at.k*1.03, at.cu, at.cv, 0.5)
				if score < minMatchScore {
					return out, errNoMatch
				}
				cs[i] = [2]float64{u, v}
			}
			da := math.Hypot(cs[0][0]-at.cu, cs[0][1]-at.cv)
			db := math.Hypot(cs[1][0]-at.cu, cs[1][1]-at.cv)
			moved := math.Hypot(cs[0][0]-cs[1][0], cs[0][1]-cs[1][1])
			switch {
			case moved > 0.01 && math.Abs(da-db) > 0.25*max(da, db) && d > r.Step:
				// The crop stopped at a side of the wide frame: a smaller step.
				d = max(r.Step, d/2)
			case moved < 0.02 && d < (r.Max-r.Min)/2:
				d = min((r.Max-r.Min)/2, d*3)
			default:
				span := float64(hi - lo)
				return [2]float64{(cs[0][0] - cs[1][0]) / span, (cs[0][1] - cs[1][1]) / span}, nil
			}
		}
		return out, errors.New("the pan or the tilt does not move the picture evenly")
	}
	if ctl.Pan.Valid() {
		if m.Pan, err = axis(ctl.Pan, wide.Pan, func(p *PTZPos, v int) { p.Pan = v }); err != nil {
			return nil, err
		}
	}
	if ctl.Tilt.Valid() {
		if m.Tilt, err = axis(ctl.Tilt, wide.Tilt, func(p *PTZPos, v int) { p.Tilt = v }); err != nil {
			return nil, err
		}
	}
	m.sortZoom()
	h.SetPTZMap(id, m)
	return m, nil
}

// waitPTZ waits for the PTZ control of the camera, for at most 5 seconds.
func (c *Cam) waitPTZ(ctx context.Context) (PTZ, error) {
	end := time.Now().Add(5 * time.Second)
	for {
		c.h.mu.Lock()
		p, ok := c.status.PTZ, c.status.State == "on"
		c.h.mu.Unlock()
		if p != nil && ok {
			return *p, nil
		}
		if time.Now().After(end) {
			if p == nil && ok {
				return PTZ{}, ErrNoPTZ
			}
			return PTZ{}, ErrNoFrame
		}
		select {
		case <-ctx.Done():
			return PTZ{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// ptzShot sets the position p, waits for a frame of that position, and returns its gray copy
// matchFineW samples wide.
func (c *Cam) ptzShot(ctx context.Context, ctl PTZ, p PTZPos) (*grayPic, error) {
	c.h.Follow(c.id, &p)
	want := ctl.Target(&p)
	end := time.Now().Add(3 * time.Second)
	for {
		c.h.mu.Lock()
		st := c.status.PTZ
		c.h.mu.Unlock()
		if st != nil && st.Pos == want || time.Now().After(end) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	var after uint64
	if f := c.Peek(); f != nil {
		after = f.Seq
	}
	if ptzSettle > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(ptzSettle):
		}
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, err := c.Next(wctx, after)
	if err != nil {
		return nil, ErrNoFrame
	}
	pic := f.pictureFor(matchFineW)
	if pic == nil {
		return nil, ErrNoFrame
	}
	return grayOf(pic, matchFineW), nil
}
