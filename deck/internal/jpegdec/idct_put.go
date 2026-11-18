package jpegdec

// The column pass of idct with the output in the pixels (code from idctCols, made by a
// script): one pass less over the block. The versions for blocks with zero rows give the same
// pixels, with less work.

// idctColsPut is idctCols, then the level shift of +128 and the clip to [0, 255] into dst.
func idctColsPut(b *block, dst []byte, stride int) {
	for i := range 8 {
		x0 := b[0*8+i]
		x7 := b[1*8+i]
		x2 := b[2*8+i]
		x5 := b[3*8+i]
		x1 := b[4*8+i]
		x6 := b[5*8+i]
		x3 := b[6*8+i]
		x4 := b[7*8+i]

		// x[012345678] are Q9.20.

		// Start by adding 0.5 to x0 (the incoming DC signal).
		// The butterflies will add it to all the other values,
		// and then the final shifts will round properly.
		x0 += 1 << 19

		// Stages 4, 3, 2: x0, x1, x2, x3.

		x0, x1 = (x0+x1)>>2, (x0-x1)>>2
		// x[01] now Q9.19.
		// Note: (1/sqrt 2)*((cos 6*pi/16)+(sin 6*pi/16)) < 1, so no new high bit.
		x2, x3 = dctBox(x2>>13, x3>>13, c(sqrt2inv_cos6, 12), -c(sqrt2inv_sin6, 12))
		// x[0123] now Q9.19.

		x1, x2 = x1+x2, x1-x2
		x0, x3 = x0+x3, x0-x3
		// x[0123] now Q9.20.

		// Stages 4, 3, 2: x4, x5, x6, x7.

		x7, x4 = x7+x4, x7-x4
		// x[47] now Q9.21.

		x5 = (x5 >> 13) * c(sqrt2inv, 14)
		x6 = (x6 >> 13) * c(sqrt2inv, 14)
		// x[56] now Q9.21.

		x7, x5 = x7+x5, x7-x5
		x4, x6 = x4+x6, x4-x6
		// x[4567] now Q9.22.

		x4, x7 = dctBox(x4>>14, x7>>14, c(cos3, 12), -c(sin3, 12))
		x5, x6 = dctBox(x5>>14, x6>>14, c(cos1, 12), -c(sin1, 12))
		// x[4567] now Q10.20.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[01234567] now Q10.21.

		x0 >>= 18
		x1 >>= 18
		x2 >>= 18
		x3 >>= 18
		x4 >>= 18
		x5 >>= 18
		x6 >>= 18
		x7 >>= 18
		// x[01234567] now Q10.3.

		dst[0*stride+i] = clamp128(x0)
		dst[1*stride+i] = clamp128(x1)
		dst[2*stride+i] = clamp128(x2)
		dst[3*stride+i] = clamp128(x3)
		dst[4*stride+i] = clamp128(x4)
		dst[5*stride+i] = clamp128(x5)
		dst[6*stride+i] = clamp128(x6)
		dst[7*stride+i] = clamp128(x7)
	}
}

// idctColsPut4 is idctColsPut for a block with zeros in rows 4 to 7.
func idctColsPut4(b *block, dst []byte, stride int) {
	for i := range 8 {
		x0 := b[0*8+i]
		x7 := b[1*8+i]
		x2 := b[2*8+i]
		x5 := b[3*8+i]
		x1 := int32(0)
		x6 := int32(0)
		x3 := int32(0)
		x4 := int32(0)

		// x[012345678] are Q9.20.

		// Start by adding 0.5 to x0 (the incoming DC signal).
		// The butterflies will add it to all the other values,
		// and then the final shifts will round properly.
		x0 += 1 << 19

		// Stages 4, 3, 2: x0, x1, x2, x3.

		x0, x1 = (x0+x1)>>2, (x0-x1)>>2
		// x[01] now Q9.19.
		// Note: (1/sqrt 2)*((cos 6*pi/16)+(sin 6*pi/16)) < 1, so no new high bit.
		x2, x3 = dctBox(x2>>13, x3>>13, c(sqrt2inv_cos6, 12), -c(sqrt2inv_sin6, 12))
		// x[0123] now Q9.19.

		x1, x2 = x1+x2, x1-x2
		x0, x3 = x0+x3, x0-x3
		// x[0123] now Q9.20.

		// Stages 4, 3, 2: x4, x5, x6, x7.

		x7, x4 = x7+x4, x7-x4
		// x[47] now Q9.21.

		x5 = (x5 >> 13) * c(sqrt2inv, 14)
		x6 = (x6 >> 13) * c(sqrt2inv, 14)
		// x[56] now Q9.21.

		x7, x5 = x7+x5, x7-x5
		x4, x6 = x4+x6, x4-x6
		// x[4567] now Q9.22.

		x4, x7 = dctBox(x4>>14, x7>>14, c(cos3, 12), -c(sin3, 12))
		x5, x6 = dctBox(x5>>14, x6>>14, c(cos1, 12), -c(sin1, 12))
		// x[4567] now Q10.20.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[01234567] now Q10.21.

		x0 >>= 18
		x1 >>= 18
		x2 >>= 18
		x3 >>= 18
		x4 >>= 18
		x5 >>= 18
		x6 >>= 18
		x7 >>= 18
		// x[01234567] now Q10.3.

		dst[0*stride+i] = clamp128(x0)
		dst[1*stride+i] = clamp128(x1)
		dst[2*stride+i] = clamp128(x2)
		dst[3*stride+i] = clamp128(x3)
		dst[4*stride+i] = clamp128(x4)
		dst[5*stride+i] = clamp128(x5)
		dst[6*stride+i] = clamp128(x6)
		dst[7*stride+i] = clamp128(x7)
	}
}

// idctColsPut2 is idctColsPut for a block with zeros in rows 2 to 7.
func idctColsPut2(b *block, dst []byte, stride int) {
	for i := range 8 {
		x0 := b[0*8+i]
		x7 := b[1*8+i]
		x2 := int32(0)
		x5 := int32(0)
		x1 := int32(0)
		x6 := int32(0)
		x3 := int32(0)
		x4 := int32(0)

		// x[012345678] are Q9.20.

		// Start by adding 0.5 to x0 (the incoming DC signal).
		// The butterflies will add it to all the other values,
		// and then the final shifts will round properly.
		x0 += 1 << 19

		// Stages 4, 3, 2: x0, x1, x2, x3.

		x0, x1 = (x0+x1)>>2, (x0-x1)>>2
		// x[01] now Q9.19.
		// Note: (1/sqrt 2)*((cos 6*pi/16)+(sin 6*pi/16)) < 1, so no new high bit.
		x2, x3 = dctBox(x2>>13, x3>>13, c(sqrt2inv_cos6, 12), -c(sqrt2inv_sin6, 12))
		// x[0123] now Q9.19.

		x1, x2 = x1+x2, x1-x2
		x0, x3 = x0+x3, x0-x3
		// x[0123] now Q9.20.

		// Stages 4, 3, 2: x4, x5, x6, x7.

		x7, x4 = x7+x4, x7-x4
		// x[47] now Q9.21.

		x5 = (x5 >> 13) * c(sqrt2inv, 14)
		x6 = (x6 >> 13) * c(sqrt2inv, 14)
		// x[56] now Q9.21.

		x7, x5 = x7+x5, x7-x5
		x4, x6 = x4+x6, x4-x6
		// x[4567] now Q9.22.

		x4, x7 = dctBox(x4>>14, x7>>14, c(cos3, 12), -c(sin3, 12))
		x5, x6 = dctBox(x5>>14, x6>>14, c(cos1, 12), -c(sin1, 12))
		// x[4567] now Q10.20.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[01234567] now Q10.21.

		x0 >>= 18
		x1 >>= 18
		x2 >>= 18
		x3 >>= 18
		x4 >>= 18
		x5 >>= 18
		x6 >>= 18
		x7 >>= 18
		// x[01234567] now Q10.3.

		dst[0*stride+i] = clamp128(x0)
		dst[1*stride+i] = clamp128(x1)
		dst[2*stride+i] = clamp128(x2)
		dst[3*stride+i] = clamp128(x3)
		dst[4*stride+i] = clamp128(x4)
		dst[5*stride+i] = clamp128(x5)
		dst[6*stride+i] = clamp128(x6)
		dst[7*stride+i] = clamp128(x7)
	}
}

// clamp128 returns v + 128 in [0, 255].
func clamp128(v int32) uint8 {
	v += 128
	if uint32(v) > 255 {
		v = ^(v >> 31) & 255
	}
	return uint8(v)
}
