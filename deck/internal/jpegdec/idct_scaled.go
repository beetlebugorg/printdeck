package jpegdec

// The reduced inverse DCT. The 8 x 8 coefficients F(u, v) of a block are those of the orthonormal
// 2D DCT. The picture at 1/2, 1/4 or 1/8 of the size has n x n pixels per block (n = 4, 2 or 1),
// and its pixels are about the means of the pixels of the whole picture. They come from the
// lowest n x n frequencies with an n-point inverse DCT:
//
//	p(x, y) = 1/4 Σ_u Σ_v C(u) C(v) F(u, v) cos((2x+1)uπ/2n) cos((2y+1)vπ/2n),  u, v < n
//
// with C(0) = 1/√2 and C(u) = 1 for u > 0. With n = 1, p is F(0, 0)/8: the mean of the block.

// The factors of the butterflies: 1/2 C(u) cos(kπ/8).
const (
	k0 = 0.35355339 // 1/2 · 1/√2, and 1/2 cos(π/4)
	k1 = 0.46193977 // 1/2 cos(π/8)
	k3 = 0.19134172 // 1/2 cos(3π/8)
)

// idct4 is the 4-point inverse DCT of f0..f3 with the factors 1/2 C(u): 6 multiplications.
func idct4(f0, f1, f2, f3 float32) (p0, p1, p2, p3 float32) {
	a, b := k0*f0, k0*f2
	o1, o2 := k1*f1+k3*f3, k3*f1-k1*f3
	return a + b + o1, a - b + o2, a - b - o2, a + b - o1
}

// idctScaled writes the block b (dequantized, in natural order) at 1/2^shift of its size to dst,
// with the level shift of +128 and a clip to [0, 255]. shift is 1, 2 or 3.
func idctScaled(b *block, shift uint, dst []byte, stride int) {
	switch shift {
	case 1:
		// Across each row of frequencies, and then down each column.
		var t [4][4]float32
		for v := range 4 {
			r := b[v*8 : v*8+4]
			t[v][0], t[v][1], t[v][2], t[v][3] = idct4(float32(r[0]), float32(r[1]), float32(r[2]), float32(r[3]))
		}
		for x := range 4 {
			p0, p1, p2, p3 := idct4(t[0][x], t[1][x], t[2][x], t[3][x])
			dst[x] = clip(p0 + 128)
			dst[stride+x] = clip(p1 + 128)
			dst[2*stride+x] = clip(p2 + 128)
			dst[3*stride+x] = clip(p3 + 128)
		}
	case 2:
		// The 2-point inverse DCT: k0 (f0 ± f1), in two directions.
		f00, f01, f10, f11 := float32(b[0]), float32(b[1]), float32(b[8]), float32(b[9])
		a0, a1 := k0*(f00+f01), k0*(f00-f01)
		c0, c1 := k0*(f10+f11), k0*(f10-f11)
		dst[0] = clip(k0*(a0+c0) + 128)
		dst[1] = clip(k0*(a1+c1) + 128)
		dst[stride] = clip(k0*(a0-c0) + 128)
		dst[stride+1] = clip(k0*(a1-c1) + 128)
	default:
		dst[0] = clip(float32(b[0])/8 + 128)
	}
}

// clip rounds v to a byte in [0, 255].
func clip(v float32) byte {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return byte(v + 0.5)
}
