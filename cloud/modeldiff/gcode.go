package modeldiff

import (
	"bufio"
	"bytes"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// segment is an extrusion move of a G-code file.
type segment struct{ a, b [3]float64 }

// readMoves returns the extrusion moves of a G-code file: G0 and G1 with more filament, in
// absolute or relative positions (G90, G91) and filament (M82, M83).
func readMoves(data []byte) []segment {
	var out []segment
	var pos [3]float64
	var e float64
	absXYZ, absE := true, true
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, ';'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "G90":
			absXYZ, absE = true, true
		case "G91":
			absXYZ, absE = false, false
		case "M82":
			absE = true
		case "M83":
			absE = false
		case "G92":
			for _, w := range f[1:] {
				if v, err := strconv.ParseFloat(w[1:], 64); err == nil && len(w) > 1 && (w[0] == 'E' || w[0] == 'e') {
					e = v
				}
			}
		case "G0", "G1":
			next, ext := pos, false
			for _, w := range f[1:] {
				if len(w) < 2 {
					continue
				}
				v, err := strconv.ParseFloat(w[1:], 64)
				if err != nil {
					continue
				}
				k := -1
				switch w[0] {
				case 'X', 'x':
					k = 0
				case 'Y', 'y':
					k = 1
				case 'Z', 'z':
					k = 2
				case 'E', 'e':
					if absE {
						ext, e = v > e+1e-9, v
					} else {
						ext = v > 1e-9
					}
				}
				if k >= 0 {
					if absXYZ {
						next[k] = v
					} else {
						next[k] += v
					}
				}
			}
			if ext && next != pos {
				out = append(out, segment{pos, next})
			}
			pos = next
		}
	}
	return out
}

// RenderGcode draws the extrusion moves of a G-code file in the isometric view of Render, w x h
// pixels, in the color c, lighter higher up, on the grid of a bed of bedX x bedY mm (none for 0).
// The view fits the moves. It returns nil for a file with no extrusion.
func RenderGcode(data []byte, c color.RGBA, bedX, bedY float64, w, h int) *image.RGBA {
	segs := readMoves(data)
	if len(segs) == 0 {
		return nil
	}
	const ss = 2
	W, H := w*ss, h*ss
	view := norm([3]float64{-1, 1, -1})
	right := norm(cross(view, [3]float64{0, 0, 1}))
	up := cross(right, view)
	proj := func(p [3]float64) (float64, float64, float64) { return dot(p, right), dot(p, up), dot(p, view) }
	// The box: the moves, and the bed.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	maxZ := 0.0
	grow := func(p [3]float64) {
		sx, sy, _ := proj(p)
		minX, maxX = math.Min(minX, sx), math.Max(maxX, sx)
		minY, maxY = math.Min(minY, sy), math.Max(maxY, sy)
	}
	for _, s := range segs {
		grow(s.a)
		grow(s.b)
		maxZ = math.Max(maxZ, math.Max(s.a[2], s.b[2]))
	}
	// The view fits the moves. The bed shows behind them, as far as the picture goes.
	bed := [4][3]float64{{0, 0, 0}, {bedX, 0, 0}, {bedX, bedY, 0}, {0, bedY, 0}}
	pad := 0.1
	scale := math.Min(float64(W)*(1-2*pad)/(maxX-minX), float64(H)*(1-2*pad)/(maxY-minY))
	ox := float64(W)/2 - (minX+maxX)/2*scale
	oy := float64(H)/2 + (minY+maxY)/2*scale
	screen := func(p [3]float64) (float64, float64, float64) {
		sx, sy, d := proj(p)
		return ox + sx*scale, oy - sy*scale, d
	}
	depth := make([]float64, W*H)
	col := make([][3]float64, W*H)
	bg := [3]float64{float64(Background.R), float64(Background.G), float64(Background.B)}
	for i := range depth {
		depth[i], col[i] = math.Inf(1), bg
	}
	plot := func(x, y int, d float64, c [3]float64) {
		if x < 0 || y < 0 || x >= W || y >= H {
			return
		}
		i := y*W + x
		if d < depth[i] {
			depth[i], col[i] = d, c
		}
	}
	line := func(a, b [3]float64, c [3]float64, thick int, behind float64) {
		ax, ay, ad := screen(a)
		bx, by, bd := screen(b)
		n := int(math.Max(math.Abs(bx-ax), math.Abs(by-ay))) + 1
		for i := 0; i <= n; i++ {
			t := float64(i) / float64(n)
			x, y, d := ax+(bx-ax)*t, ay+(by-ay)*t, ad+(bd-ad)*t+behind
			for dy := 0; dy < thick; dy++ {
				for dx := 0; dx < thick; dx++ {
					plot(int(x)+dx, int(y)+dy, d, c)
				}
			}
		}
	}
	// The bed: its outline and a grid of 10 mm, behind the moves.
	if bedX > 0 && bedY > 0 {
		gridC := [3]float64{0xd8, 0xdc, 0xe0}
		for x := 10.0; x < bedX; x += 10 {
			line([3]float64{x, 0, 0}, [3]float64{x, bedY, 0}, gridC, 1, 1e6)
		}
		for y := 10.0; y < bedY; y += 10 {
			line([3]float64{0, y, 0}, [3]float64{bedX, y, 0}, gridC, 1, 1e6)
		}
		for i := range bed {
			line(bed[i], bed[(i+1)%4], [3]float64{0xa0, 0xa6, 0xad}, 2, 1e6)
		}
	}
	base := [3]float64{float64(c.R), float64(c.G), float64(c.B)}
	for _, s := range segs {
		// Darker at the bottom and lighter at the top, so the layers show.
		k := 0.55 + 0.45*(s.b[2]/math.Max(maxZ, 1e-6))
		sc := [3]float64{}
		for j := range sc {
			sc[j] = math.Min(255, base[j]*k+(1-k)*40+k*60)
		}
		line(s.a, s.b, sc, 2, 0)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s [3]float64
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					c := col[(y*ss+dy)*W+x*ss+dx]
					s[0], s[1], s[2] = s[0]+c[0], s[1]+c[1], s[2]+c[2]
				}
			}
			img.SetRGBA(x, y, color.RGBA{uint8(s[0] / 4), uint8(s[1] / 4), uint8(s[2] / 4), 0xff})
		}
	}
	return img
}
