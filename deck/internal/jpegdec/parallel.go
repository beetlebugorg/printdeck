package jpegdec

import (
	"encoding/binary"
	"io"
	"sort"
	"sync"
)

// The parallel scan: the Huffman decode of one entropy-coded segment on many cores, with no
// restart markers (specs/parallel-jpeg-decode.md).
//
// The data has no marks at the starts of the blocks. Thread t starts at the byte start of its
// chunk, as if a block of the first unit of an MCU starts there. Its first blocks are usually
// wrong. But a Huffman parse that starts at a wrong bit soon falls onto the bit boundaries of
// the true parse, and then the units come into step too: the parse of the next bits depends only
// on the bit position and the unit only: the values have no effect. Thread t records the bit position and the
// unit of each block start. Thread t-1 decodes past the end of its chunk until it comes to a
// block start that thread t recorded. From that block on, the blocks of thread t are the true
// blocks. The DC values are differences, so each thread keeps the differences, and the pixel
// pass adds the predictor at the start of the region of the thread.
//
// The threads write the blocks in a compact form (words): one header word for each block (the
// count of coefficients and the DC difference), and one word for each AC coefficient (its
// zig-zag index and its quantized value).

// maxThreads is the most threads of the decode.
const maxThreads = 8

// parallelMin is the smallest data for the parallel scan: a smaller picture costs less than the
// start of the threads. A test changes it.
var parallelMin = 32 << 10

// blockStart is a block start that a thread recorded: the bit position, the unit, and the index
// of the header word of the block.
type blockStart struct {
	bit  int64
	unit int32
	word int32
	dc   [maxComponents]int32 // the sums of the DC differences of the thread before the block
}

// pthread is the state of one thread of the parallel scan.
type pthread struct {
	br     bitReader
	unit   int // the unit of the next block
	words  []uint32
	starts []blockStart         // the block starts, for each thread but the first
	blocks int                  // the blocks in words
	dcSum  [maxComponents]int32 // the sum of the DC differences of the blocks of the thread
	err    error

	// The region of the thread after the sync: the blocks from first, the first block at the
	// absolute index abs, to the block end (an index in the blocks of the thread).
	ok         bool
	firstDC    [maxComponents]int32 // dcSum before the first block of the region
	first, end int
	firstWord  int
	abs        int
	dc         [maxComponents]int32 // the DC predictors at the start of the region
}

// bitPos returns the bit position of the next bit in the data.
func (r *bitReader) bitPos() int64 {
	return int64(r.pos+r.over)*8 - int64(r.n)
}

// decodeSparse reads the coefficients of one block and appends its words: a header word with
// the count of AC coefficients and the DC difference, then the AC coefficients.
func decodeSparse(br *bitReader, words []uint32, u *unit) ([]uint32, error) {
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
	var s uint8
	if e := u.dc.lut[acc>>(64-fastLUTBits)]; e != 0 {
		l := uint(e >> 8)
		acc <<= l
		n -= l
		s = uint8(e)
	} else {
		v, l := slowDecode(u.dc, acc)
		if l == 0 {
			return words, FormatError("bad Huffman code")
		}
		acc <<= l
		n -= l
		s = v
	}
	if s > 16 {
		return words, UnsupportedError("excessive DC component")
	}
	var dcDiff int32
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
		dcDiff = v
	}
	head := len(words)
	words = append(words, 0)
	lut := u.ac.ac
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
			words = append(words, uint32(k)<<16|e&0xffff)
			continue
		}
		var rs uint8
		if e != 0 {
			l := uint(e>>20) & 31
			acc <<= l
			n -= l
			rs = uint8(e)
		} else {
			v, l := slowDecode(u.ac, acc)
			if l == 0 {
				return words[:head], FormatError("bad Huffman code")
			}
			acc <<= l
			n -= l
			rs = v
		}
		r, s := int(rs>>4), rs&0x0f
		if s == 0 {
			if r != 0x0f {
				break
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
		words = append(words, uint32(k)<<16|uint32(uint16(int16(v))))
	}
	words[head] = uint32(len(words)-head-1)<<16 | uint32(uint16(int16(dcDiff)))
	br.acc, br.n, br.pos, br.over = acc, n, pos, over
	return words, nil
}

// step decodes the next block of the thread. A start records the block start.
func (p *pthread) step(units []unit, record bool) error {
	if record {
		p.starts = append(p.starts, blockStart{bit: p.br.bitPos(), unit: int32(p.unit), word: int32(len(p.words)), dc: p.dcSum})
	}
	head := len(p.words)
	w, err := decodeSparse(&p.br, p.words, &units[p.unit])
	p.words = w
	if err != nil {
		return err
	}
	p.dcSum[units[p.unit].c] += int32(int16(p.words[head]))
	p.blocks++
	if p.unit++; p.unit == len(units) {
		p.unit = 0
	}
	return nil
}

// wordsPool keeps the word buffers of the threads.
var wordsPool = sync.Pool{New: func() any { return new([]uint32) }}

// parallelScan decodes the segment clean with threads: see the comment at the top.
func (d *decoder) parallelScan(clean []byte, units []unit, mxx, myy, threads int) error {
	total := len(units) * mxx * myy // the blocks of the picture
	totalBits := int64(len(clean)) * 8
	chunk := len(clean) / threads
	th := make([]pthread, threads)
	bufs := make([]*[]uint32, threads)
	for t := range th {
		bufs[t] = wordsPool.Get().(*[]uint32)
		th[t].words = (*bufs[t])[:0]
	}
	defer func() {
		for t := range th {
			*bufs[t] = th[t].words[:0]
			wordsPool.Put(bufs[t])
		}
	}()

	// Phase 1: each thread decodes its chunk. Each thread but the first records its block
	// starts. A thread that finds a bad code starts again one byte later: its start was wrong.
	var wg sync.WaitGroup
	for t := range th {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := &th[t]
			start, stop := t*chunk, int64(t+1)*int64(chunk)*8
			if t == threads-1 {
				stop = totalBits
			}
			p.br.reset(clean, start, len(clean))
			for p.br.bitPos() < stop {
				if err := p.step(units, t > 0); err != nil {
					if t == 0 {
						p.err = err
						return
					}
					start++
					if int64(start)*8 >= stop {
						p.words, p.starts, p.blocks = p.words[:0], p.starts[:0], 0
						return
					}
					p.words, p.starts, p.blocks, p.unit, p.dcSum = p.words[:0], p.starts[:0], 0, 0, [maxComponents]int32{}
					p.br.reset(clean, start, len(clean))
				}
			}
		}()
	}
	wg.Wait()
	if th[0].err != nil {
		return th[0].err
	}

	// Phase 2: the sync. cur is the thread with the true parse at the end of its chunk: it
	// decodes on until it comes to a block start of the next thread.
	cur := &th[0]
	cur.ok, cur.first, cur.firstWord, cur.abs = true, 0, 0, 0
	for t := 1; t < threads; t++ {
		next := &th[t]
		for {
			bit := cur.br.bitPos()
			if bit >= totalBits {
				break // the end of the data: the next threads have no region
			}
			i := sort.Search(len(next.starts), func(i int) bool {
				s := next.starts[i]
				return s.bit > bit || (s.bit == bit && int(s.unit) >= cur.unit)
			})
			if i == len(next.starts) {
				break // past the starts of next: cur decodes its chunk too
			}
			if s := next.starts[i]; s.bit == bit && int(s.unit) == cur.unit {
				cur.end = cur.blocks
				next.ok, next.first, next.firstWord, next.firstDC = true, i, int(s.word), s.dc
				next.abs = cur.abs + cur.end - cur.first
				cur = next
				break
			}
			if err := cur.step(units, false); err != nil {
				return err
			}
		}
		if cur != next && cur.br.bitPos() < totalBits {
			continue // next had no sync: cur takes the chunk of next
		}
		if cur != next {
			break
		}
	}
	// The last thread decodes to the end of the data.
	for cur.abs+cur.blocks-cur.first < total && cur.br.bitPos() < totalBits {
		if err := cur.step(units, false); err != nil {
			return err
		}
	}
	cur.end = cur.blocks
	if cur.abs+cur.end-cur.first < total {
		return io.ErrUnexpectedEOF
	}

	// Phase 3: the DC predictors at the start of each region, then the pixels.
	var regions []*pthread
	for t := range th {
		if th[t].ok && th[t].abs < total {
			regions = append(regions, &th[t])
		}
	}
	// The DC predictors at the start of each region: the sums of the DC differences of the
	// regions before it. The last region can have blocks after the end of the picture: they come
	// last, so the sums of the regions before it are right.
	var dc [maxComponents]int32
	for _, p := range regions {
		p.dc = dc
		for c := range dc {
			dc[c] += p.dcSum[c] - p.firstDC[c]
		}
	}
	for _, p := range regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.regionPixels(p, units, mxx, total)
		}()
	}
	wg.Wait()
	return nil
}

// regionPixels makes the pixels of the blocks of the region of the thread p.
func (d *decoder) regionPixels(p *pthread, units []unit, mxx, total int) {
	bs := 8 >> d.shift
	var planes [maxComponents][]byte
	var strides [maxComponents]int
	for c := range d.nComp {
		planes[c], strides[c] = d.plane(c)
	}
	dc := p.dc
	var b block
	w := p.firstWord
	for i := p.first; i < p.end; i++ {
		abs := p.abs + i - p.first
		if abs >= total {
			break
		}
		h := p.words[w]
		n := int(h >> 16)
		mcu, ui := abs/len(units), abs%len(units)
		u := &units[ui]
		c := u.c
		dc[c] += int32(int16(h))
		mx, my := mcu%mxx, mcu/mxx
		if mx < d.mx0 || mx >= d.mx1 || my < d.my0 || my >= d.my1 {
			w += 1 + n
			continue
		}
		qt := u.qt
		b[0] = dc[c] * qt[0]
		last, rows := uint8(0), uint8(1)
		for _, e := range p.words[w+1 : w+1+n] {
			k := e >> 16 & 63
			z := unzig[k]
			b[z] = int32(int16(e)) * qt[k]
			rows |= 1 << (z >> 3)
			last = uint8(k)
		}
		w += 1 + n
		hi, vi := int(d.comp[c].h), int(d.comp[c].v)
		j := int(u.j)
		bx, by := hi*(mx-d.mx0)+j%hi, vi*(my-d.my0)+j/hi
		stride := strides[c]
		d.putBlock(&b, last, rows, planes[c][bs*(by*stride+bx):], stride)
	}
}
