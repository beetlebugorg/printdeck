package jpegdec

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"
	"runtime"
	"sync"
)

// The fast scan: the decode of a baseline scan with all the components of the picture, such as the
// MJPEG frames of a USB camera. It gives the same picture as the scan of image/jpeg, faster:
//
//   - It removes the stuffed bytes and finds the restart markers in one pass over the data
//     (unstuff). Then it reads the bits 64 at a time, with no test for a marker at each byte.
//   - A table of the first 9 bits gives most Huffman codes in one step.
//   - It dequantizes each coefficient when it reads it, and it notes the last coefficient of each
//     block. A block with only the DC coefficient gets no inverse DCT: each pixel has the same
//     value.
//   - The goroutine of the decode reads the bits of one row of MCUs into a buffer, and other
//     goroutines make the pixels of the rows that it read. The Huffman decode reads one bit
//     after the other, so it stays on one core. The inverse DCT of each row runs on the other
//     cores.

// fastLUTBits is the size of the code table: the codes of 9 bits or less take one step.
const fastLUTBits = 9

// fastHuff is a Huffman table of the fast scan.
type fastHuff struct {
	// lut is the table of the next fastLUTBits bits: the length of the code in the high byte and
	// the value in the low byte, or 0 for a longer code.
	lut [1 << fastLUTBits]uint16
	// maxCode[l] is the largest code of l bits, or -1. valPtr[l] is the index in vals of the
	// smallest code of l bits, minus that code.
	maxCode [maxCodeLength + 2]int32
	valPtr  [maxCodeLength + 1]int32
	vals    [maxNCodes]uint8
	// ac is the table of the next acLUTBits bits for an AC table (acEntry), or nil.
	ac *[1 << acLUTBits]uint32
}

// acLUTBits is the size of the AC table: a code and the bits of its value, with acLUTBits bits
// or less together, take one step.
const acLUTBits = 10

// An acEntry gives the next AC coefficient. With acFull, it has the value in the low 16 bits
// (signed), the run of zeros before it in bits 16 to 19, and the length of the code and the
// value bits in bits 20 to 24. Without acFull, the code is longer than acLUTBits bits (0), or
// the value bits do not fit: the low byte is the value of the code (the run and the size), and
// bits 20 to 24 are the length of the code.
const acFull = 1 << 31

// makeAC makes the combined AC table of f.
func (f *fastHuff) makeAC() {
	f.ac = new([1 << acLUTBits]uint32)
	for i := range uint32(1 << acLUTBits) {
		e := f.lut[i>>(acLUTBits-fastLUTBits)]
		if e == 0 {
			continue
		}
		l, rs := uint32(e>>8), uint32(e&0xff)
		r, sz := rs>>4, rs&0x0f
		if sz == 0 || l+sz > acLUTBits {
			f.ac[i] = l<<20 | rs
			continue
		}
		v := int32(i>>(acLUTBits-l-sz)) & (1<<sz - 1)
		if v < 1<<(sz-1) {
			v += -1<<sz + 1
		}
		f.ac[i] = acFull | (l+sz)<<20 | r<<16 | uint32(uint16(int16(v)))
	}
}

// makeFastHuff makes the fast table of the table h.
func makeFastHuff(h *huffman) *fastHuff {
	f := &fastHuff{vals: h.vals}
	for l := 1; l <= maxCodeLength; l++ {
		f.maxCode[l] = -1
		lo, hi := h.minCodes[l-1], h.maxCodes[l-1]
		if hi < 0 {
			continue
		}
		f.maxCode[l] = hi
		f.valPtr[l] = h.valsIndices[l-1] - lo
		if l > fastLUTBits {
			continue
		}
		for code := lo; code <= hi; code++ {
			v := uint16(l)<<8 | uint16(h.vals[h.valsIndices[l-1]+code-lo])
			first := code << (fastLUTBits - l)
			for k := range int32(1) << (fastLUTBits - l) {
				f.lut[first+k] = v
			}
		}
	}
	f.maxCode[maxCodeLength+1] = 1<<30 - 1 // a stop for the slow search
	return f
}

// bitReader reads the bits of one entropy-coded segment with no stuffed bytes. The unread bits
// are at the top of acc. After the end of the segment it gives zero bits.
type bitReader struct {
	data     []byte
	pos, end int
	acc      uint64
	n        uint // the count of unread bits in acc
	over     int  // the zero bytes after the end
	start    int
}

func (r *bitReader) reset(data []byte, start, end int) {
	*r = bitReader{data: data, pos: start, end: end, start: start}
}

// fill makes n 57 or more.
func (r *bitReader) fill() {
	if r.pos+8 <= r.end {
		// The bytes after the whole bytes go into the low bits of acc too. The next fill puts the
		// same bits there again.
		r.acc |= binary.BigEndian.Uint64(r.data[r.pos:]) >> r.n
		k := (63 - r.n) >> 3
		r.pos += int(k)
		r.n += k * 8
		return
	}
	for r.n <= 56 {
		var b byte
		if r.pos < r.end {
			b = r.data[r.pos]
			r.pos++
		} else {
			r.over++
		}
		r.acc |= uint64(b) << (56 - r.n)
		r.n += 8
	}
}

// fillEnd is fill near the end of the segment, with the state in values.
func fillEnd(data []byte, pos, end int, acc uint64, n uint, over int) (uint64, uint, int, int) {
	for n <= 56 {
		var b byte
		if pos < end {
			b = data[pos]
			pos++
		} else {
			over++
		}
		acc |= uint64(b) << (56 - n)
		n += 8
	}
	return acc, n, pos, over
}

// slowDecode decodes a code that is longer than the table: it returns the value and the length
// of the code, or a length of 0 for a bad code.
func slowDecode(h *fastHuff, acc uint64) (uint8, uint) {
	if e := h.lut[acc>>(64-fastLUTBits)]; e != 0 {
		return uint8(e), uint(e >> 8)
	}
	l := uint(fastLUTBits + 1)
	code := int32(acc >> (64 - l))
	for code > h.maxCode[l] {
		l++
		code = int32(acc >> (64 - l))
	}
	if l > maxCodeLength {
		return 0, 0
	}
	return h.vals[h.valPtr[l]+code], l
}

// overrun reports that the decode read more bits than the segment has: the data is short.
func (r *bitReader) overrun() bool {
	used := (r.pos-r.start+r.over)*8 - int(r.n)
	return used > (r.end-r.start)*8
}

// decode reads one Huffman code. n must be 16 or more.
func (r *bitReader) decode(h *fastHuff) (uint8, bool) {
	if e := h.lut[r.acc>>(64-fastLUTBits)]; e != 0 {
		l := uint(e >> 8)
		r.acc <<= l
		r.n -= l
		return uint8(e), true
	}
	l := uint(fastLUTBits + 1)
	code := int32(r.acc >> (64 - l))
	for code > h.maxCode[l] {
		l++
		code = int32(r.acc >> (64 - l))
	}
	if l > maxCodeLength {
		return 0, false
	}
	r.acc <<= l
	r.n -= l
	return h.vals[h.valPtr[l]+code], true
}

// receive reads s bits (1 to 16) as a signed value (section F.2.2.1). n must be s or more.
func (r *bitReader) receive(s uint8) int32 {
	v := int32(r.acc >> (64 - uint(s)))
	r.acc <<= s
	r.n -= uint(s)
	if v < 1<<(s-1) {
		v += -1<<s + 1
	}
	return v
}

// unstuff copies the entropy-coded data at the start of raw to clean: it removes the stuffed
// zero bytes and the fill bytes. segs are the starts of the segments after each restart marker
// in clean. end is the index in raw of the marker after the data, or len(raw).
func unstuff(raw, clean []byte) (out []byte, segs []int, end int) {
	i := 0
	for {
		j := bytes.IndexByte(raw[i:], 0xff)
		if j < 0 {
			return append(clean, raw[i:]...), segs, len(raw)
		}
		clean = append(clean, raw[i:i+j]...)
		i += j
		if i+1 >= len(raw) {
			return clean, segs, len(raw)
		}
		switch m := raw[i+1]; {
		case m == 0x00:
			clean = append(clean, 0xff)
			i += 2
		case m == 0xff:
			i++
		case rst0Marker <= m && m <= rst7Marker:
			segs = append(segs, len(clean))
			i += 2
		default:
			return clean, segs, i
		}
	}
}

// cleanPool keeps the buffers of the unstuffed data.
var cleanPool = sync.Pool{New: func() any { return new([]byte) }}

// unit is one block of an MCU: its tables, its component, and its index in the blocks of the
// component in the MCU.
type unit struct {
	dc, ac *fastHuff
	qt     *block
	c, j   uint8
}

// fastRow is the coefficients of one row of MCUs: 64 for each block, in the order of the
// blocks in the MCUs, dequantized and in natural order. last is the zig-zag index of the last
// coefficient of each block that is not zero, or 0.
type fastRow struct {
	my   int
	coef []int32
	last []uint8
	rows []uint8 // the rows of each block with a coefficient that is not zero (bit i for row i)
}

// canFastScan reports that the fast scan can decode the scan: a sequential scan of all the
// components, with one or three components.
func (d *decoder) canFastScan(nComp int) bool {
	if d.progressive || nComp != d.nComp || (d.nComp != 1 && d.nComp != 3) {
		return false
	}
	return d.nComp != 1 || (d.comp[0].h == 1 && d.comp[0].v == 1)
}

// fastScan decodes the data of a scan: see the comment at the top. comps are the indexes of the
// components of the scan in d.comp, with their DC and AC tables.
func (d *decoder) fastScan(comps []uint8, td, ta []uint8, mxx, myy int) error {
	// All the data of the reader, from the start of the scan.
	rest, err := io.ReadAll(d.r)
	if err != nil {
		return err
	}
	raw := append(d.bytes.buf[d.bytes.i:d.bytes.j:d.bytes.j], rest...)
	cp := cleanPool.Get().(*[]byte)
	defer cleanPool.Put(cp)
	clean, segs, end := unstuff(raw, (*cp)[:0])
	*cp = clean
	// The decoder reads the markers after the scan from raw[end:].
	d.r = bytes.NewReader(raw[end:])
	d.bytes.i, d.bytes.j, d.bytes.nUnreadable = 0, 0, 0
	d.bits = bits{}

	var dcH, acH [maxComponents]*fastHuff
	made := map[*huffman]*fastHuff{}
	get := func(h *huffman, ac bool) *fastHuff {
		f, ok := made[h]
		if !ok {
			f = makeFastHuff(h)
			made[h] = f
		}
		if ac && f.ac == nil {
			f.makeAC()
		}
		return f
	}
	nBlocks := 0 // blocks in one MCU
	for i, c := range comps {
		dcH[i], acH[i] = get(&d.huff[dcTable][td[i]], false), get(&d.huff[acTable][ta[i]], true)
		nBlocks += d.comp[c].h * d.comp[c].v
	}
	rowBlocks := nBlocks * mxx
	var units []unit
	for i, c := range comps {
		for j := range d.comp[c].h * d.comp[c].v {
			units = append(units, unit{dcH[i], acH[i], &d.quant[d.comp[c].tq], c, uint8(j)})
		}
	}
	threads := min(runtime.GOMAXPROCS(0), maxThreads)
	if d.threads > 0 {
		threads = min(d.threads, maxThreads)
	}
	if d.sparse != nil {
		return d.parallelScan(clean, units, mxx, myy, threads)
	}
	if d.ri == 0 {
		// The sparse scan: the compact words cost less than the rows of dense coefficients,
		// most of all for a camera frame with many blocks of one color.
		if len(clean) < parallelMin {
			threads = 1
		}
		return d.parallelScan(clean, units, mxx, myy, threads)
	}

	// The workers make the pixels of the rows.
	workers := 0
	if myy >= 8 {
		workers = min(threads-1, 4)
	}
	newRow := func() *fastRow {
		return &fastRow{coef: make([]int32, rowBlocks*blockSize), last: make([]uint8, rowBlocks), rows: make([]uint8, rowBlocks)}
	}
	var (
		todo, free chan *fastRow
		wg         sync.WaitGroup
		inline     *fastRow
	)
	if workers > 0 {
		todo, free = make(chan *fastRow, workers*2), make(chan *fastRow, workers*2+1)
		for range workers*2 + 1 {
			free <- newRow()
		}
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for row := range todo {
					d.fastPixels(row, comps, mxx)
					free <- row
				}
			}()
		}
		defer func() {
			if todo != nil {
				close(todo)
				wg.Wait()
			}
		}()
	} else {
		inline = newRow()
	}

	var (
		br    bitReader
		dc    [maxComponents]int32
		seg   = 0
		mcu   = 0
		total = mxx * myy
	)
	segEnd := func() int {
		if seg < len(segs) {
			return segs[seg]
		}
		return len(clean)
	}
	br.reset(clean, 0, segEnd())
	for my := range myy {
		row := inline
		if row == nil {
			row = <-free
		}
		row.my = my
		bi := 0
		for range mxx {
			for _, u := range units {
				b := (*block)(row.coef[bi*blockSize : bi*blockSize+blockSize])
				last, rows, err := decodeBlock(&br, b, u.dc, u.ac, u.qt, &dc[u.c])
				if err != nil {
					return err
				}
				row.last[bi], row.rows[bi] = uint8(last), rows
				bi++
			}
			mcu++
			if d.ri > 0 && mcu%d.ri == 0 && mcu < total {
				if br.overrun() {
					return io.ErrUnexpectedEOF
				}
				seg++
				start := segEnd()
				if seg-1 < len(segs) {
					start = segs[seg-1]
				}
				br.reset(clean, start, segEnd())
				dc = [maxComponents]int32{}
			}
		}
		if br.overrun() {
			return io.ErrUnexpectedEOF
		}
		if inline != nil {
			d.fastPixels(row, comps, mxx)
		} else {
			todo <- row
		}
	}
	return nil
}

// decodeBlock reads the coefficients of one block into b, dequantized with qt, and returns the
// zig-zag index of the last coefficient that is not zero, and the rows with a coefficient that
// is not zero (bit i for row i). dc is the DC predictor of the
// component. The state of the bits stays in local variables for the speed.
func decodeBlock(br *bitReader, b *block, dh, ah *fastHuff, qt *block, dc *int32) (int, uint8, error) {
	acc, n, pos, end, data := br.acc, br.n, br.pos, br.end, br.data
	over := br.over
	if n < 32 {
		if pos+8 <= end {
			acc |= binary.BigEndian.Uint64(data[pos:]) >> n
			k := (63 - n) >> 3
			pos += int(k)
			n += k * 8
		} else {
			acc, n, pos, over = fillEnd(data, pos, end, acc, n, over)
		}
	}
	// The DC coefficient.
	var s uint8
	if e := dh.lut[acc>>(64-fastLUTBits)]; e != 0 {
		l := uint(e >> 8)
		acc <<= l
		n -= l
		s = uint8(e)
	} else {
		v, l := slowDecode(dh, acc)
		if l == 0 {
			return 0, 0, FormatError("bad Huffman code")
		}
		acc <<= l
		n -= l
		s = v
	}
	if s > 16 {
		return 0, 0, UnsupportedError("excessive DC component")
	}
	if s > 0 {
		if n < 32 {
			if pos+8 <= end {
				acc |= binary.BigEndian.Uint64(data[pos:]) >> n
				k := (63 - n) >> 3
				pos += int(k)
				n += k * 8
			} else {
				acc, n, pos, over = fillEnd(data, pos, end, acc, n, over)
			}
		}
		v := int32(acc >> (64 - uint(s)))
		acc <<= s
		n -= uint(s)
		if v < 1<<(s-1) {
			v += -1<<s + 1
		}
		*dc += v
	}
	b[0] = *dc * qt[0]
	last, rows := 0, uint8(1)
	lut := ah.ac
	for k := 1; k < blockSize; k++ {
		if n < 32 {
			if pos+8 <= end {
				acc |= binary.BigEndian.Uint64(data[pos:]) >> n
				m := (63 - n) >> 3
				pos += int(m)
				n += m * 8
			} else {
				acc, n, pos, over = fillEnd(data, pos, end, acc, n, over)
			}
		}
		e := lut[acc>>(64-acLUTBits)]
		if e&acFull != 0 {
			l := uint(e>>20) & 31
			acc <<= l
			n -= l
			k += int(e>>16) & 15
			if k >= blockSize {
				break
			}
			z := unzig[k&63]
			b[z] = int32(int16(e)) * qt[k&63]
			rows |= 1 << (z >> 3)
			last = k
			continue
		}
		var rs uint8
		if e != 0 {
			l := uint(e>>20) & 31
			acc <<= l
			n -= l
			rs = uint8(e)
		} else {
			v, l := slowDecode(ah, acc)
			if l == 0 {
				return 0, 0, FormatError("bad Huffman code")
			}
			acc <<= l
			n -= l
			rs = v
		}
		r, s := int(rs>>4), rs&0x0f
		if s == 0 {
			if r != 0x0f {
				break // the end of the block
			}
			k += 0x0f
			continue
		}
		k += r
		if k >= blockSize {
			break
		}
		v := int32(acc >> (64 - uint(s)))
		acc <<= s
		n -= uint(s)
		if v < 1<<(s-1) {
			v += -1<<s + 1
		}
		z := unzig[k&63]
		b[z] = v * qt[k&63]
		rows |= 1 << (z >> 3)
		last = k
	}
	br.acc, br.n, br.pos, br.over = acc, n, pos, over
	return last, rows, nil
}

// fastPixels makes the pixels of the blocks of a row, and clears the coefficients of the row.
func (d *decoder) fastPixels(row *fastRow, comps []uint8, mxx int) {
	bs := 8 >> d.shift
	var planes [maxComponents][]byte
	var strides [maxComponents]int
	for _, c := range comps {
		planes[c], strides[c] = d.plane(int(c))
	}
	bi := 0
	rowIn := row.my >= d.my0 && row.my < d.my1
	for mx := range mxx {
		in := rowIn && mx >= d.mx0 && mx < d.mx1
		for _, c := range comps {
			hi, vi := d.comp[c].h, d.comp[c].v
			dst, stride := planes[c], strides[c]
			for j := range hi * vi {
				b := (*block)(row.coef[bi*blockSize : bi*blockSize+blockSize])
				if !in {
					// A block out of the region: no pixels, but the buffer is used again.
					for r := range 8 {
						if row.rows[bi]&(1<<r) != 0 {
							clear(b[r*8 : r*8+8])
						}
					}
					bi++
					continue
				}
				bx, by := hi*(mx-d.mx0)+j%hi, vi*(row.my-d.my0)+j/hi
				out := dst[bs*(by*stride+bx):]
				d.putBlock(b, row.last[bi], row.rows[bi], out, stride)
				bi++
			}
		}
	}
}

// fillScaled writes a block with only the DC coefficient dc at 1/2^shift of its size: the same
// pixels as idctScaled, with one value.
func fillScaled(out []byte, stride int, shift uint, dc int32) {
	f := float32(dc)
	var v uint8
	if shift == 3 {
		v = clip(f/8 + 128)
	} else {
		v = clip(k0*(k0*f) + 128)
	}
	n := 8 >> shift
	for y := range n {
		r := out[y*stride : y*stride+n]
		for x := range r {
			r[x] = v
		}
	}
}

// putBlock writes the pixels of the block b (dequantized, in natural order) to out, and clears b.
// last is the zig-zag index of its last coefficient that is not zero, and rows the rows with a
// coefficient that is not zero.
func (d *decoder) putBlock(b *block, last, rows uint8, out []byte, stride int) {
	switch {
	case d.shift > 0:
		if last == 0 {
			fillScaled(out, stride, d.shift, b[0])
		} else {
			idctScaled(b, d.shift, out, stride)
		}
		for r := range 8 {
			if rows&(1<<r) != 0 {
				clear(b[r*8 : r*8+8])
			}
		}
	case last == 0:
		fillBlock(out, stride, dcPixel(b[0]))
		b[0] = 0
	case rows == 1:
		// Only the first row has coefficients: after the row IDCT, each column has
		// one value, and the column IDCT of one value is the same at each pixel.
		idctRowsMask(b, 1)
		var col [8]uint8
		for x := range 8 {
			v := ((b[x]+1<<19)>>2)>>18 + 128
			if uint32(v) > 255 {
				v = ^(v >> 31) & 255
			}
			col[x] = uint8(v)
		}
		for y := range 8 {
			*(*[8]uint8)(out[y*stride : y*stride+8]) = col
		}
		clear(b[:8])
	default:
		idctRowsMask(b, rows)
		switch {
		case rows&0xfc == 0:
			idctColsPut2(b, out, stride)
		case rows&0xf0 == 0:
			idctColsPut4(b, out, stride)
		default:
			idctColsPut(b, out, stride)
		}
		for r := range 8 {
			if rows&(1<<r) != 0 {
				clear(b[r*8 : r*8+8])
			}
		}
	}
}

// fillBlock sets the 8 x 8 pixels of a block to v.
func fillBlock(out []byte, stride int, v uint8) {
	r := [8]uint8{v, v, v, v, v, v, v, v}
	for y := range 8 {
		*(*[8]uint8)(out[y*stride : y*stride+8]) = r
	}
}

// plane returns the pixels of the component c, and the stride. The first pixel is the first
// pixel of the first block of the MCU (mx0, my0).
func (d *decoder) plane(c int) ([]byte, int) {
	if d.nComp == 1 {
		m := d.img1
		return m.Pix[m.PixOffset(m.Rect.Min.X, m.Rect.Min.Y)-d.originOff(0, m.Stride, m.Rect.Min):], m.Stride
	}
	m := d.img3
	switch c {
	case 0:
		return m.Y[m.YOffset(m.Rect.Min.X, m.Rect.Min.Y)-d.originOff(0, m.YStride, m.Rect.Min):], m.YStride
	case 1:
		return m.Cb[m.COffset(m.Rect.Min.X, m.Rect.Min.Y)-d.originOff(1, m.CStride, m.Rect.Min):], m.CStride
	}
	return m.Cr[m.COffset(m.Rect.Min.X, m.Rect.Min.Y)-d.originOff(2, m.CStride, m.Rect.Min):], m.CStride
}

// originOff is the offset in the plane of the component c from its first sample (the sample of
// the pixel min) back to the sample of the first block of the MCU (mx0, my0).
func (d *decoder) originOff(c, stride int, min image.Point) int {
	bs := 8 >> d.shift
	h0, v0 := d.comp[0].h, d.comp[0].v
	// The pixel of the corner of the MCU, and its sample in the plane of c.
	px, py := bs*h0*d.mx0, bs*v0*d.my0
	fx, fy := d.comp[c].h, d.comp[c].v
	dx := (min.X*fx)/h0 - (px*fx)/h0
	dy := (min.Y*fy)/v0 - (py*fy)/v0
	return dy*stride + dx
}

// writeBlock writes the output of idct to dst, with the level shift of +128 and a clip to
// [0, 255].
func writeBlock(b *block, dst []byte, stride int) {
	for y := range 8 {
		r := dst[y*stride : y*stride+8 : y*stride+8]
		s := b[y*8 : y*8+8 : y*8+8]
		for x := range 8 {
			v := s[x] + 128
			if uint32(v) > 255 {
				v = ^(v >> 31) & 255
			}
			r[x] = uint8(v)
		}
	}
}

// dcRange is the range of the DC coefficients in dcPixels: a DC coefficient out of the range gives
// 0 or 255.
const dcRange = 4096

// dcPixels is the pixel of a block with only the DC coefficient dc - dcRange: the output of idct,
// and each pixel of the block has this value.
var dcPixels = sync.OnceValue(func() *[2 * dcRange]uint8 {
	var t [2 * dcRange]uint8
	for i := range t {
		var b block
		b[0] = int32(i - dcRange)
		idct(&b)
		v := b[0] + 128
		t[i] = uint8(max(0, min(255, v)))
	}
	return &t
})

// dcPixel returns the pixel of a block with only the DC coefficient dc.
func dcPixel(dc int32) uint8 {
	switch {
	case dc < -dcRange:
		return 0
	case dc >= dcRange:
		return 255
	}
	return dcPixels()[dc+dcRange]
}
