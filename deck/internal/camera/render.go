package camera

import (
	"image"
	"math"
)

// The pictures of the views: a crop of the frame, scaled to the size of the view, and outside the
// mask a dark blur. The dark blur is cheap: the picture goes to 1/8 of its size, gets a blur and
// 35 % of its brightness there, and comes back to the full size only outside the mask.

// The dark blur.
const (
	blurShrink = 8    // the blur works on a picture of 1/blurShrink of the size
	blurDark   = 0.35 // the brightness outside the mask
)

// renderView makes the picture of the plan from the picture of the frame, at ow x oh (even
// numbers).
func renderView(pic *image.YCbCr, plan viewPlan, ow, oh int) *image.YCbCr {
	out := cropScale(pic, plan.crop, ow, oh)
	if plan.mask != nil {
		c := plan.crop
		sx, sy := float64(ow)/(c[2]-c[0]), float64(oh)/(c[3]-c[1])
		poly := make([][2]float64, len(plan.mask))
		for i, p := range plan.mask {
			poly[i] = [2]float64{(p[0] - c[0]) * sx, (p[1] - c[1]) * sy}
		}
		darkBlur(out, poly)
	}
	return out
}

// cropScale scales the crop c (x0, y0, x1, y1 in pixels) of the picture to a picture of ow x oh.
// A crop that is twice the size of the picture or more gets the mean of the samples, and a
// smaller crop gets the bilinear mix of the 4 nearest samples.
func cropScale(p *image.YCbCr, c [4]float64, ow, oh int) *image.YCbCr {
	if c[2]-c[0] >= 2*float64(ow) {
		r := image.Rect(int(c[0]+0.5), int(c[1]+0.5), int(c[2]+0.5), int(c[3]+0.5)).Intersect(p.Rect)
		return scaleYCbCr(p.SubImage(r).(*image.YCbCr), ow, oh, 0)
	}
	out := image.NewYCbCr(image.Rect(0, 0, ow, oh), image.YCbCrSubsampleRatio420)
	y, cb, cr := ycbcrPlanes(p)
	bilinear(out.Y, out.YStride, ow, oh, y, c)
	fx, fy := float64(cb.w)/float64(y.w), float64(cb.h)/float64(y.h)
	cc := [4]float64{c[0] * fx, c[1] * fy, c[2] * fx, c[3] * fy}
	bilinear(out.Cb, out.CStride, ow/2, oh/2, cb, cc)
	bilinear(out.Cr, out.CStride, ow/2, oh/2, cr, cc)
	return out
}

// lerpTable is the source samples of each place of a scaled row or column: the first sample (a
// byte offset), the next sample, and the weight of the next sample from 0 to 256.
type lerpTable struct {
	a, b []int
	f    []int
}

// newLerpTable is the table of n places over the source range [s0, s1) of a line of src samples,
// with step bytes from one sample to the next.
func newLerpTable(n int, s0, s1 float64, src, step int) lerpTable {
	t := lerpTable{make([]int, n), make([]int, n), make([]int, n)}
	k := (s1 - s0) / float64(n)
	for i := range n {
		s := s0 + (float64(i)+0.5)*k - 0.5
		s = max(0, min(float64(src-1), s))
		j := int(s)
		f := int((s - float64(j)) * 256)
		nj := min(j+1, src-1)
		t.a[i], t.b[i], t.f[i] = j*step, nj*step, f
	}
	return t
}

// bilinear scales the crop c of the plane src into dst, a plane of dw x dh bytes with stride
// bytes a row.
func bilinear(dst []byte, stride, dw, dh int, src plane, c [4]float64) {
	xs := newLerpTable(dw, c[0], c[2], src.w, src.step)
	ys := newLerpTable(dh, c[1], c[3], src.h, src.stride)
	p := src.pix
	for y := range dh {
		r0, r1, fy := src.off+ys.a[y], src.off+ys.b[y], ys.f[y]
		row := dst[y*stride : y*stride+dw]
		for x := range row {
			a, b, fx := xs.a[x], xs.b[x], xs.f[x]
			top := int(p[r0+a])*(256-fx) + int(p[r0+b])*fx
			bot := int(p[r1+a])*(256-fx) + int(p[r1+b])*fx
			row[x] = byte((top*(256-fy) + bot*fy + 32768) >> 16)
		}
	}
}

// spans returns the inside of a convex polygon on each row of a plane of w x h: the first pixel
// and the pixel after the last. A row with no inside has lo == hi. scale is the size of a pixel of
// the plane in the units of the polygon.
func spans(poly [][2]float64, w, h int, scale float64) [][2]int {
	out := make([][2]int, h)
	for y := range h {
		yc := (float64(y) + 0.5) * scale
		lo, hi := math.Inf(1), math.Inf(-1)
		for i := range poly {
			a, b := poly[i], poly[(i+1)%len(poly)]
			if (a[1] <= yc) == (b[1] <= yc) {
				continue // the edge does not cross the row
			}
			x := a[0] + (yc-a[1])*(b[0]-a[0])/(b[1]-a[1])
			lo, hi = min(lo, x), max(hi, x)
		}
		if lo > hi {
			continue
		}
		// The pixels with their middle inside.
		l := int(math.Ceil(lo/scale - 0.5))
		r := int(math.Floor(hi/scale-0.5)) + 1
		l, r = max(0, min(w, l)), max(0, min(w, r))
		if l < r {
			out[y] = [2]int{l, r}
		}
	}
	return out
}

// darkBlur puts the dark blur of the picture outside the convex polygon poly (in pixels of p).
func darkBlur(p *image.YCbCr, poly [][2]float64) {
	w, h := p.Rect.Dx(), p.Rect.Dy()
	cw, ch := (w+1)/2, (h+1)/2
	sw, sh := (w+blurShrink-1)/blurShrink, (h+blurShrink-1)/blurShrink
	dark := func(v int) byte { return byte(float64(v)*blurDark + 0.5) }
	darkC := func(v int) byte { return byte(128 + float64(v-128)*blurDark + 0.5) }

	ys := shrinkBlur(p.Y, p.YStride, w, h, blurShrink, sw, sh)
	for i, v := range ys {
		ys[i] = int(dark(v))
	}
	cbs := shrinkBlur(p.Cb, p.CStride, cw, ch, blurShrink/2, sw, sh)
	crs := shrinkBlur(p.Cr, p.CStride, cw, ch, blurShrink/2, sw, sh)
	for i := range cbs {
		cbs[i], crs[i] = int(darkC(cbs[i])), int(darkC(crs[i]))
	}
	fill(p.Y, p.YStride, w, h, ys, sw, sh, blurShrink, spans(poly, w, h, 1))
	cs := spans(poly, cw, ch, 2)
	fill(p.Cb, p.CStride, cw, ch, cbs, sw, sh, blurShrink/2, cs)
	fill(p.Cr, p.CStride, cw, ch, crs, sw, sh, blurShrink/2, cs)
}

// shrinkBlur returns the mean of each block of k x k samples of a plane of w x h, as a picture of
// sw x sh, after two passes of a 3 x 3 box blur.
func shrinkBlur(pix []byte, stride, w, h, k, sw, sh int) []int {
	s := make([]int, sw*sh)
	for y := range h {
		row := pix[y*stride : y*stride+w]
		out := s[(y/k)*sw : (y/k)*sw+sw]
		for bx := range out {
			sum := 0
			for _, v := range row[bx*k : min(bx*k+k, w)] {
				sum += int(v)
			}
			out[bx] += sum
		}
	}
	for by := range sh {
		rows := min(k, h-by*k)
		for bx := range sw {
			s[by*sw+bx] /= rows * min(k, w-bx*k)
		}
	}
	tmp := make([]int, len(s))
	for range 2 {
		for y := range sh {
			for x := range sw {
				l, r := max(0, x-1), min(sw-1, x+1)
				tmp[y*sw+x] = (s[y*sw+l] + s[y*sw+x] + s[y*sw+r]) / 3
			}
		}
		for y := range sh {
			u, d := max(0, y-1), min(sh-1, y+1)
			for x := range sw {
				s[y*sw+x] = (tmp[u*sw+x] + tmp[y*sw+x] + tmp[d*sw+x]) / 3
			}
		}
	}
	return s
}

// fill puts the small picture s (sw x sh, each sample k samples of the plane) back at the size of
// the plane, with the bilinear mix, outside the spans of each row.
func fill(pix []byte, stride, w, h int, s []int, sw, sh, k int, in [][2]int) {
	xs := newLerpTable(w, 0, float64(w)/float64(k), sw, 1)
	ys := newLerpTable(h, 0, float64(h)/float64(k), sh, sw)
	for y := range h {
		r0, r1, fy := ys.a[y], ys.b[y], ys.f[y]
		row := pix[y*stride : y*stride+w]
		put := func(x0, x1 int) {
			for x := x0; x < x1; x++ {
				a, b, fx := xs.a[x], xs.b[x], xs.f[x]
				top := s[r0+a]*(256-fx) + s[r0+b]*fx
				bot := s[r1+a]*(256-fx) + s[r1+b]*fx
				row[x] = byte((top*(256-fy) + bot*fy + 32768) >> 16)
			}
		}
		lo, hi := in[y][0], in[y][1]
		if lo >= hi {
			put(0, w)
			continue
		}
		put(0, lo)
		put(hi, w)
	}
}
