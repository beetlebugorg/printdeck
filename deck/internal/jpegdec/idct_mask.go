package jpegdec

// idctRowsMask is idctRows for the rows in mask only (bit i for row i). The other rows must be
// zero: the 1D IDCT of a row of zeros is zeros.
// Inputs are UQ8.0; outputs are Q9.20.
func idctRowsMask(b *block, mask uint8) {
	for i := range 8 {
		if mask&(1<<i) == 0 {
			continue
		}
		x := b[8*i : 8*i+8 : 8*i+8]
		x0 := x[0]
		x7 := x[1]
		x2 := x[2]
		x5 := x[3]
		x1 := x[4]
		x6 := x[5]
		x3 := x[6]
		x4 := x[7]

		// Run FDCT backward.
		// Independent operations have been reordered somewhat
		// to make precision tracking easier.
		//
		// Note that “x0, x1 = x0+x1, x0-x1” is now a reverse butterfly
		// and carries with it an implicit divide by two: the extra bit
		// is added to the precision, not the value size.

		// x[01234567] are UQ8.0 in [0, 255].

		// Stages 4, 3, 2: x0, x1, x2, x3.

		x0 <<= 17
		x1 <<= 17
		// x0, x1 now UQ8.17.
		x0, x1 = x0+x1, x0-x1
		// x0 now UQ8.18 in [0, 255].
		// x1 now Q7.18 in [-127½, 127½].

		// Note: (1/sqrt 2)*((cos 6*pi/16)+(sin 6*pi/16)) < 0.924, so no new high bit.
		x2, x3 = dctBox(x2, x3, c(sqrt2inv_cos6, 18), -c(sqrt2inv_sin6, 18))
		// x[23] now Q8.18 in [-236, 236].
		x1, x2 = x1+x2, x1-x2
		x0, x3 = x0+x3, x0-x3
		// x[0123] now Q8.19 in [-246, 246].

		// Stages 4, 3, 2: x4, x5, x6, x7.

		x4 <<= 7
		x7 <<= 7
		// x[47] now UQ8.7
		x7, x4 = x7+x4, x7-x4
		// x7 now UQ8.8 in [0, 255].
		// x4 now Q7.8 in [-127½, 127½].

		x6 = x6 * c(sqrt2inv, 8)
		x5 = x5 * c(sqrt2inv, 8)
		// x[56] now UQ8.8 in [0, 181].
		// Note that 1/√2 has five 0s in its binary representation after
		// the 8th bit, so this multipliy is actually producing 12 bits of precision.

		x7, x5 = x7+x5, x7-x5
		x4, x6 = x4+x6, x4-x6
		// x[4567] now Q8.9 in [-218, 218].

		x4, x7 = dctBox(x4>>2, x7>>2, c(cos3, 12), -c(sin3, 12))
		x5, x6 = dctBox(x5>>2, x6>>2, c(cos1, 12), -c(sin1, 12))
		// x[4567] now Q9.19 in [-303, 303].

		// Stage 1.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[01234567] now Q9.20 in [-275, 275].

		// Note: we don't need all 20 bits of “precision”,
		// but it is faster to let idctCols shift it away as part
		// of other operations rather than downshift here.

		x[0] = x0
		x[1] = x1
		x[2] = x2
		x[3] = x3
		x[4] = x4
		x[5] = x5
		x[6] = x6
		x[7] = x7
	}
}
