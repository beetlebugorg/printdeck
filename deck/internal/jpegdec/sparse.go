package jpegdec

import (
	"bytes"
	"errors"
	"image"
	"sync"
)

// errNoSparse is the error of DecodeSparse for a JPEG that the fast scan does not decode: a
// progressive JPEG, a JPEG with restart markers, or four components.
var errNoSparse = errors.New("jpegdec: no sparse decode of this JPEG")

// Sparse is the result of the Huffman decode of a JPEG: the coefficients of each block in the
// compact form of the parallel scan (specs/parallel-jpeg-decode.md), with no pixels. A frame
// keeps it, and each user of the frame makes the pixels of its scale and region from it
// (Render), with no second Huffman decode. A Sparse is safe for use by many goroutines.
type Sparse struct {
	d               *decoder
	units           []unit
	mxx, myy, total int
	regions         []sparseRegion
	bufs            []*[]uint32
	release         sync.Once
}

// sparseRegion is the region of one thread of the parallel scan: the blocks from first (the block
// at the absolute index abs) to end, from the word firstWord, with the DC predictors dc.
type sparseRegion struct {
	words      []uint32
	first, end int
	firstWord  int
	abs        int
	dc         [maxComponents]int32
}

// DecodeSparse does the Huffman decode of the JPEG b on all cores, and keeps the coefficients. It
// gives an error for a JPEG that it does not decode in this way: then use DecodeRegion.
func DecodeSparse(b []byte) (*Sparse, error) { return DecodeSparseThreads(b, 0) }

// DecodeSparseThreads is DecodeSparse with at most threads threads (0 for GOMAXPROCS).
func DecodeSparseThreads(b []byte, threads int) (*Sparse, error) {
	s := &Sparse{}
	d := &decoder{sparse: s, threads: threads}
	if _, err := d.decode(bytes.NewReader(b), false); err != nil {
		return nil, err
	}
	return s, nil
}

// Bounds returns the bounds of the picture at the whole size.
func (s *Sparse) Bounds() image.Rectangle { return image.Rect(0, 0, s.d.width, s.d.height) }

// Words returns the count of words of the coefficients.
func (s *Sparse) Words() int {
	n := 0
	for _, r := range s.regions {
		n += len(r.words)
	}
	return n
}

// Render makes the picture of the region rect (in the pixels of the whole size; empty for the
// whole picture) at 1/2^shift of the size, as DecodeRegion does.
func (s *Sparse) Render(shift int, rect image.Rectangle) (image.Image, error) {
	d := *s.d
	d.sparse, d.r = nil, nil
	d.img1, d.img3 = nil, nil
	d.shift = uint(max(0, min(3, shift)))
	d.region = rect
	d.makeImg(s.mxx, s.myy, true)
	var wg sync.WaitGroup
	for i := range s.regions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.regionPixels(&s.regions[i], s.units, s.mxx, s.total)
		}()
	}
	wg.Wait()
	img, err := d.finish()
	if err != nil || rect.Empty() {
		return img, err
	}
	k := 1 << d.shift
	sr := image.Rect(rect.Min.X/k, rect.Min.Y/k, (rect.Max.X+k-1)/k, (rect.Max.Y+k-1)/k).Intersect(img.Bounds())
	if m, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return m.SubImage(sr), nil
	}
	return img, nil
}

// Release gives the buffers of the coefficients back for the next decode. The Sparse must not
// be used after it.
func (s *Sparse) Release() {
	s.release.Do(func() {
		for _, b := range s.bufs {
			*b = (*b)[:0]
			wordsPool.Put(b)
		}
		s.regions = nil
	})
}
