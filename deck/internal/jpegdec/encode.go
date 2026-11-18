// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jpegdec

import (
	"image"
	mbits "math/bits"
	"runtime"
	"sync"
)

// The encoder of the deck: a baseline JPEG of a YCbCr picture, with the tables of image/jpeg
// (Annex K of the standard, scaled by the quality as image/jpeg does), and the same forward DCT
// and quantization. It is faster than image/jpeg:
//
//   - It reads the samples of the planes with no color conversion and no image.Image calls.
//   - It writes the bits 64 at a time.
//   - It encodes strips of MCU rows on many cores. A restart marker starts each strip, so each
//     strip has its own Huffman state and DC predictors: the decoders put the strips together.

// encHuffSpec is a Huffman table of the standard: the count of codes of each length, and the
// values.
type encHuffSpec struct {
	count [16]byte
	value []byte
}

var encSpec = [4]encHuffSpec{
	// Luminance DC.
	{
		[16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Luminance AC.
	{
		[16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125},
		[]byte{
			0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
			0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
			0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
			0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
			0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
			0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
			0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
			0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
			0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
			0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
			0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
			0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
			0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
			0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
			0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
			0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
			0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
	// Chrominance DC.
	{
		[16]byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Chrominance AC.
	{
		[16]byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 119},
		[]byte{
			0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
			0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
			0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
			0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
			0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
			0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
			0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
			0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
			0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
			0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
			0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
			0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
			0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
			0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
			0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
}

// encUnscaledQuant are the quantization tables of the standard (Annex K), in zig-zag order:
// luminance, then chrominance.
var encUnscaledQuant = [2][blockSize]byte{
	// Luminance.
	{
		16, 11, 12, 14, 12, 10, 16, 14,
		13, 14, 18, 17, 16, 19, 24, 40,
		26, 24, 22, 22, 24, 49, 35, 37,
		29, 40, 58, 51, 61, 60, 57, 51,
		56, 55, 64, 72, 92, 78, 64, 68,
		87, 69, 55, 56, 80, 109, 81, 87,
		95, 98, 103, 104, 103, 62, 77, 113,
		121, 112, 100, 120, 92, 101, 103, 99,
	},
	// Chrominance.
	{
		17, 18, 18, 24, 21, 24, 47, 26,
		26, 47, 99, 66, 56, 66, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
	},
}

// encHuff is an encode table: the code and its length in bits for each value.
type encHuff struct {
	code [256]uint32
	size [256]uint8
}

var encTables = sync.OnceValue(func() *[4]encHuff {
	var t [4]encHuff
	for i, s := range encSpec {
		code, k := uint32(0), 0
		for l := range 16 {
			for range s.count[l] {
				t[i].code[s.value[k]] = code
				t[i].size[s.value[k]] = uint8(l + 1)
				code++
				k++
			}
			code <<= 1
		}
	}
	return &t
})

// encQuant returns the quantization tables for the quality (1 to 100), in zig-zag order.
func encQuant(quality int) (q [2][blockSize]uint8) {
	quality = max(1, min(100, quality))
	scale := 200 - quality*2
	if quality < 50 {
		scale = 5000 / quality
	}
	for i := range q {
		for j := range q[i] {
			x := (int(encUnscaledQuant[i][j])*scale + 50) / 100
			q[i][j] = uint8(max(1, min(255, x)))
		}
	}
	return q
}

// bitWriter writes the entropy-coded bits with the stuffed zero bytes.
type bitWriter struct {
	out []byte
	acc uint64 // the bits, at the bottom
	n   uint   // the count of bits in acc
}

// put writes the low size bits of bits. size is 32 or less.
func (w *bitWriter) put(bits uint32, size uint) {
	w.acc = w.acc<<size | uint64(bits)&(1<<size-1)
	w.n += size
	if w.n >= 32 {
		w.n -= 32
		v := uint32(w.acc >> w.n)
		if t := ^v; (t-0x01010101)&^t&0x80808080 == 0 {
			// No byte is 0xff (no byte of ^v is zero): a fast path.
			w.out = append(w.out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
		} else {
			for s := 24; s >= 0; s -= 8 {
				b := byte(v >> s)
				w.out = append(w.out, b)
				if b == 0xff {
					w.out = append(w.out, 0)
				}
			}
		}
	}
}

// flush writes the last bits, with one bits to fill the last byte.
func (w *bitWriter) flush() {
	for w.n >= 8 {
		w.n -= 8
		b := byte(w.acc >> w.n)
		w.out = append(w.out, b)
		if b == 0xff {
			w.out = append(w.out, 0)
		}
	}
	if w.n > 0 {
		b := byte(w.acc<<(8-w.n)) | byte(0xff>>w.n)
		w.out = append(w.out, b)
		if b == 0xff {
			w.out = append(w.out, 0)
		}
	}
	w.acc, w.n = 0, 0
}

// emitValue writes the code of the run and the size of v, then the bits of v.
func (w *bitWriter) emitValue(h *encHuff, run int32, v int32) {
	a, b := v, v
	if a < 0 {
		a, b = -v, v-1
	}
	nb := uint32(mbits.Len32(uint32(a)))
	rs := uint8(run<<4) | uint8(nb)
	w.put(h.code[rs], uint(h.size[rs]))
	if nb > 0 {
		w.put(uint32(b), uint(nb))
	}
}

// encQ is a quantizer: the divisor d (8 times the table value, for the scale of fdct) of each
// coefficient in zig-zag order, half of it, and its reciprocal for an exact division by a
// multiplication.
type encQ struct {
	d, half [blockSize]int32
	m       [blockSize]uint64 // ceil(2^40 / d)
}

func makeEncQ(q *[blockSize]uint8) *encQ {
	e := new(encQ)
	for i, v := range q {
		d := 8 * int32(v)
		e.d[i], e.half[i] = d, d/2
		e.m[i] = (1<<40 + uint64(d) - 1) / uint64(d)
	}
	return e
}

// quant returns a/d rounded to the nearest integer, as encDiv. n = |a| + d/2 is less than 2^24
// and d is less than 2^12, so (n * m) >> 40 is the floor of n/d: the error of m is less than
// 2^-16 of n/d, and the fraction of n/d is at most 1 - 1/d.
func (e *encQ) quant(a int32, i int) int32 {
	h := e.half[i]
	if a < h && a > -h {
		return 0
	}
	if a >= 0 {
		return int32(uint64(a+h) * e.m[i] >> 40)
	}
	return -int32(uint64(-a+h) * e.m[i] >> 40)
}

// encBlock encodes one block of samples, and returns its DC value.
func encBlock(w *bitWriter, b *block, q *encQ, dcH, acH *encHuff, prevDC int32) int32 {
	fdct(b)
	dc := q.quant(b[0], 0)
	w.emitValue(dcH, 0, dc-prevDC)
	run := int32(0)
	for zig := 1; zig < blockSize; zig++ {
		ac := q.quant(b[unzig[zig]], zig)
		if ac == 0 {
			run++
			continue
		}
		for run > 15 {
			w.put(acH.code[0xf0], uint(acH.size[0xf0]))
			run -= 16
		}
		w.emitValue(acH, run, ac)
		run = 0
	}
	if run > 0 {
		w.put(acH.code[0x00], uint(acH.size[0x00]))
	}
	return dc
}

// encDiv returns a/b rounded to the nearest integer (the quantizer of image/jpeg), for the test.
func encDiv(a, b int32) int32 {
	if a >= 0 {
		return (a + (b >> 1)) / b
	}
	return -((-a + (b >> 1)) / b)
}

// loadBlock copies the 8 x 8 samples at (x, y) of a plane of w x h samples into b. The samples
// past the edge repeat the last row and column.
func loadBlock(b *block, pix []byte, stride, w, h, x, y int) {
	if x+8 <= w && y+8 <= h {
		for j := range 8 {
			r := pix[(y+j)*stride+x : (y+j)*stride+x+8 : (y+j)*stride+x+8]
			d := b[j*8 : j*8+8 : j*8+8]
			for i := range 8 {
				d[i] = int32(r[i])
			}
		}
		return
	}
	for j := range 8 {
		row := pix[min(y+j, h-1)*stride:]
		for i := range 8 {
			b[j*8+i] = int32(row[min(x+i, w-1)])
		}
	}
}

// Encode returns the JPEG of the picture p at the quality (1 to 100). The picture must have the
// subsampling 4:4:4, 4:2:2 or 4:2:0. The JPEG has strips with restart markers when the picture
// is large enough for more than one core.
func Encode(p *image.YCbCr, quality int) ([]byte, error) {
	return EncodeThreads(p, quality, 0)
}

// EncodeThreads is Encode with at most threads strips (0 for GOMAXPROCS).
func EncodeThreads(p *image.YCbCr, quality, threads int) ([]byte, error) {
	var hs, vs int
	switch p.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
		hs, vs = 1, 1
	case image.YCbCrSubsampleRatio422:
		hs, vs = 2, 1
	case image.YCbCrSubsampleRatio420:
		hs, vs = 2, 2
	default:
		return nil, UnsupportedError("subsampling of the encode")
	}
	w, h := p.Rect.Dx(), p.Rect.Dy()
	if w <= 0 || h <= 0 || w > 0xffff || h > 0xffff {
		return nil, FormatError("size of the encode")
	}
	q := encQuant(quality)
	eq := [2]*encQ{makeEncQ(&q[0]), makeEncQ(&q[1])}
	tabs := encTables()
	mw, mh := 8*hs, 8*vs
	mxx, myy := (w+mw-1)/mw, (h+mh-1)/mh
	cw, ch := (w+hs-1)/hs, (h+vs-1)/vs
	yPix := p.Y[p.YOffset(p.Rect.Min.X, p.Rect.Min.Y):]
	cbPix := p.Cb[p.COffset(p.Rect.Min.X, p.Rect.Min.Y):]
	crPix := p.Cr[p.COffset(p.Rect.Min.X, p.Rect.Min.Y):]

	// The strips: rows of MCUs, one strip for each core, about 64 KB of samples at least.
	if threads <= 0 {
		threads = runtime.GOMAXPROCS(0)
	}
	strips := min(threads, maxThreads, myy, max(1, w*h/(64<<10)))
	rowsPer := (myy + strips - 1) / strips
	strips = (myy + rowsPer - 1) / rowsPer
	outs := make([][]byte, strips)
	encStrip := func(s int) {
		bw := bitWriter{out: make([]byte, 0, w*h/8/strips+1024)}
		var dc [3]int32
		var b block
		for my := s * rowsPer; my < min(myy, (s+1)*rowsPer); my++ {
			for mx := range mxx {
				for j := range vs {
					for i := range hs {
						loadBlock(&b, yPix, p.YStride, w, h, mx*mw+i*8, my*mh+j*8)
						dc[0] = encBlock(&bw, &b, eq[0], &tabs[0], &tabs[1], dc[0])
					}
				}
				loadBlock(&b, cbPix, p.CStride, cw, ch, mx*8, my*8)
				dc[1] = encBlock(&bw, &b, eq[1], &tabs[2], &tabs[3], dc[1])
				loadBlock(&b, crPix, p.CStride, cw, ch, mx*8, my*8)
				dc[2] = encBlock(&bw, &b, eq[1], &tabs[2], &tabs[3], dc[2])
			}
		}
		bw.flush()
		outs[s] = bw.out
	}
	if strips == 1 {
		encStrip(0)
	} else {
		var wg sync.WaitGroup
		for s := range strips {
			wg.Add(1)
			go func() {
				defer wg.Done()
				encStrip(s)
			}()
		}
		wg.Wait()
	}

	size := 1024
	for _, o := range outs {
		size += len(o) + 2
	}
	out := make([]byte, 0, size)
	out = append(out, 0xff, 0xd8)
	// The quantization tables.
	out = append(out, 0xff, 0xdb, 0, 2+2*65)
	for i := range q {
		out = append(out, byte(i))
		out = append(out, q[i][:]...)
	}
	// The frame: 8 bits, h x w, three components.
	out = append(out, 0xff, 0xc0, 0, 17, 8, byte(h>>8), byte(h), byte(w>>8), byte(w), 3,
		1, byte(hs<<4|vs), 0, 2, 0x11, 1, 3, 0x11, 1)
	// The Huffman tables: the DC and AC tables of luminance, then of chrominance.
	n := 2
	for _, s := range encSpec {
		n += 17 + len(s.value)
	}
	out = append(out, 0xff, 0xc4, byte(n>>8), byte(n))
	for i, s := range encSpec {
		out = append(out, []byte{0x00, 0x10, 0x01, 0x11}[i])
		out = append(out, s.count[:]...)
		out = append(out, s.value...)
	}
	if strips > 1 {
		ri := rowsPer * mxx
		out = append(out, 0xff, 0xdd, 0, 4, byte(ri>>8), byte(ri))
	}
	// The scan.
	out = append(out, 0xff, 0xda, 0, 12, 3, 1, 0x00, 2, 0x11, 3, 0x11, 0, 0x3f, 0)
	for s, o := range outs {
		out = append(out, o...)
		if s < len(outs)-1 {
			out = append(out, 0xff, byte(rst0Marker+s%8))
		}
	}
	return append(out, 0xff, 0xd9), nil
}
