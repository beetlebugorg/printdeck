package timelapse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"sort"

	"printdeck/internal/h264"
	"printdeck/internal/turbojpeg"
)

// The H.264 track of an MP4 video, as the time lapse of a Bambu Lab printer: the samples are
// access units with a length before each NAL unit (avcC), and the sync samples (stss) are the
// frames that decode alone. Frame decodes from the sync sample before the frame, and goes on from
// the last frame when the player plays the next one.

// avc is the H.264 data of a track: the parameter sets in Annex B, the size of the length before
// each NAL unit, and the sync samples (from 0, in order; nil when each sample is one).
type avc struct {
	params  []byte
	lenSize int
	sync    []int

	dec  *h264.Decoder
	next int // the next sample for the decoder
	last *image.YCbCr
}

var startCode = []byte{0, 0, 0, 1}

// readAVC reads the avcC of the sample entry and the sync samples of the stbl data.
func readAVC(entry, stbl []byte) (*avc, error) {
	bad := errors.New("timelapse: a bad H.264 track in the video")
	// The visual sample entry: 8 bytes of header and 78 bytes of fields, then its atoms.
	if len(entry) < 86 {
		return nil, bad
	}
	c, ok := child(entry[86:], "avcC")
	if !ok || len(c) < 7 {
		return nil, bad
	}
	a := &avc{lenSize: int(c[4]&3) + 1}
	p := c[5:]
	for set := 0; set < 2; set++ {
		n := int(p[0])
		if set == 0 {
			n &= 0x1f
		}
		p = p[1:]
		for k := 0; k < n; k++ {
			if len(p) < 2 {
				return nil, bad
			}
			l := int(binary.BigEndian.Uint16(p))
			if len(p) < 2+l {
				return nil, bad
			}
			a.params = append(append(a.params, startCode...), p[2:2+l]...)
			p = p[2+l:]
		}
		if set == 0 && len(p) < 1 {
			return nil, bad
		}
	}
	if stss, ok := child(stbl, "stss"); ok && len(stss) >= 8 {
		n := int(binary.BigEndian.Uint32(stss[4:]))
		if len(stss) < 8+4*n {
			return nil, bad
		}
		for k := 0; k < n; k++ {
			a.sync = append(a.sync, int(binary.BigEndian.Uint32(stss[8+4*k:]))-1)
		}
	}
	return a, nil
}

// annexB turns the sample s (NAL units with lengths) into an access unit with start codes, with
// the parameter sets first for a sync sample.
func (a *avc) annexB(s []byte, sync bool) ([]byte, error) {
	var out []byte
	if sync {
		out = append(out, a.params...)
	}
	for len(s) > 0 {
		if len(s) < a.lenSize {
			return nil, errors.New("timelapse: a bad H.264 sample")
		}
		n := 0
		for _, b := range s[:a.lenSize] {
			n = n<<8 | int(b)
		}
		s = s[a.lenSize:]
		if n > len(s) {
			return nil, errors.New("timelapse: a bad H.264 sample")
		}
		out = append(append(out, startCode...), s[:n]...)
		s = s[n:]
	}
	return out, nil
}

// syncBefore returns the sync sample at i or before it.
func (a *avc) syncBefore(i int) int {
	if a.sync == nil {
		return i
	}
	k := sort.SearchInts(a.sync, i+1) - 1
	if k < 0 {
		return 0
	}
	return a.sync[k]
}

// isSync reports that sample i decodes alone.
func (a *avc) isSync(i int) bool {
	if a.sync == nil {
		return true
	}
	k := sort.SearchInts(a.sync, i)
	return k < len(a.sync) && a.sync[k] == i
}

// close frees the decoder.
func (a *avc) close() {
	if a.dec != nil {
		a.dec.Close()
		a.dec = nil
	}
}

// frame decodes sample i of the movie m. The decoder goes on from the last sample when i is
// after it in the same run of samples, else it starts again at the sync sample before i.
func (m *Movie) frame264(i int) (*image.YCbCr, error) {
	a := m.avc
	if i == a.next-1 && a.last != nil {
		return a.last, nil // the frame of the last call
	}
	if a.dec == nil || i < a.next || a.syncBefore(i) > a.next {
		a.close()
		d, err := h264.New(1)
		if err != nil {
			return nil, err
		}
		a.dec, a.next, a.last = d, a.syncBefore(i), nil
	}
	for a.next <= i {
		s, err := m.sample(a.next)
		if err != nil {
			return nil, err
		}
		au, err := a.annexB(s, a.isSync(a.next))
		if err != nil {
			return nil, err
		}
		pics, err := a.dec.Decode(au)
		if err != nil {
			return nil, err
		}
		if len(pics) > 0 {
			a.last = pics[len(pics)-1]
		}
		a.next++
	}
	if a.last == nil {
		return nil, errors.New("timelapse: the decoder gave no frame")
	}
	return a.last, nil
}

// sample reads sample i from the file.
func (m *Movie) sample(i int) ([]byte, error) {
	b := make([]byte, m.sizes[i])
	_, err := m.f.ReadAt(b, m.offsets[i])
	return b, err
}

// decodedPicture is the picture of a decoded frame for screen pixels: the frame at 1/2, 1/4 or
// 1/8 of its size when that is enough, as a JPEG is decoded (decodeShift). The JPEG for a remote
// screen is encoded when it asks for it.
func decodedPicture(p *image.YCbCr, screen int) *Picture {
	w, h := p.Rect.Dx(), p.Rect.Dy()
	pic := &Picture{w: w, h: h, shift: decodeShift(w, screen)}
	small := shrink(p, pic.shift)
	pic.once.Do(func() { pic.pic = small })
	pic.encode = func() []byte {
		if turbojpeg.Available {
			if b, err := turbojpeg.Encode(p, 85); err == nil {
				return b
			}
		}
		var buf bytes.Buffer
		jpeg.Encode(&buf, p, &jpeg.Options{Quality: 85})
		return buf.Bytes()
	}
	return pic
}

// shrink returns p at 1/2^shift of its size, with the mean of each block of pixels.
func shrink(p *image.YCbCr, shift int) *image.YCbCr {
	if shift == 0 {
		return p
	}
	k := 1 << shift
	w, h := (p.Rect.Dx()+k-1)/k, (p.Rect.Dy()+k-1)/k
	out := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	mean := func(src []byte, stride, sw, sh, x, y, k int) byte {
		sum, n := 0, 0
		for dy := 0; dy < k && y*k+dy < sh; dy++ {
			row := src[(y*k+dy)*stride:]
			for dx := 0; dx < k && x*k+dx < sw; dx++ {
				sum += int(row[x*k+dx])
				n++
			}
		}
		return byte(sum / max(1, n))
	}
	sw, sh := p.Rect.Dx(), p.Rect.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Y[y*out.YStride+x] = mean(p.Y, p.YStride, sw, sh, x, y, k)
		}
	}
	cw, ch := (w+1)/2, (h+1)/2
	scw, sch := (sw+1)/2, (sh+1)/2
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			out.Cb[y*out.CStride+x] = mean(p.Cb, p.CStride, scw, sch, x, y, k)
			out.Cr[y*out.CStride+x] = mean(p.Cr, p.CStride, scw, sch, x, y, k)
		}
	}
	return out
}
