package jpegdec

import (
	"errors"
	"image"
	"sync"
)

// GPUInput is the input of the inverse DCT of a whole picture on another device (package
// gpujpeg, a compute shader): the blocks, the coefficients, the quantization tables, and the
// layout of the planes of the output.
type GPUInput struct {
	// Blocks has 4 words for each block: the index of its first AC word in the words of all the
	// regions; the count of AC words, with the component in bits 16 to 17 and the quantization
	// table in bits 18 to 19; the DC coefficient, dequantized; and the byte offset of its first
	// pixel in the output.
	Blocks []uint32
	// Words are the AC words of each region, in order.
	Words [][]uint32
	// Quant are the quantization tables, in zig-zag order.
	Quant [maxTq + 1][blockSize]int32
	// The output: the planes (Y, Cb, Cr; or Y only for gray) at PlaneOff with Strides, OutLen
	// bytes in all. The planes cover whole MCUs.
	Strides, PlaneOff [3]int
	Planes            int
	OutLen            int
	Width, Height     int
	Ratio             image.YCbCrSubsampleRatio
	planeH            [3]int
}

// GPUInput returns the input of the inverse DCT of the whole picture at the whole size.
func (s *Sparse) GPUInput() (*GPUInput, error) {
	d := s.d
	if d.nComp == 3 && d.isRGB() {
		return nil, errors.New("jpegdec: an RGB JPEG")
	}
	in := &GPUInput{Width: d.width, Height: d.height, Planes: d.nComp}
	for i := range d.quant {
		in.Quant[i] = d.quant[i]
	}
	h0, v0 := d.comp[0].h, d.comp[0].v
	off := 0
	for c := range d.nComp {
		w := 8 * d.comp[c].h * s.mxx
		h := 8 * d.comp[c].v * s.myy
		in.Strides[c], in.PlaneOff[c], in.planeH[c] = w, off, h
		off += w * h
	}
	in.OutLen = off
	if d.nComp == 3 {
		switch (h0/d.comp[1].h)<<4 | v0/d.comp[1].v {
		case 0x11:
			in.Ratio = image.YCbCrSubsampleRatio444
		case 0x12:
			in.Ratio = image.YCbCrSubsampleRatio440
		case 0x21:
			in.Ratio = image.YCbCrSubsampleRatio422
		case 0x22:
			in.Ratio = image.YCbCrSubsampleRatio420
		case 0x41:
			in.Ratio = image.YCbCrSubsampleRatio411
		case 0x42:
			in.Ratio = image.YCbCrSubsampleRatio410
		}
	}
	in.Blocks = make([]uint32, 4*s.total)
	base := 0
	var wg sync.WaitGroup
	for i := range s.regions {
		r := &s.regions[i]
		in.Words = append(in.Words, r.words)
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			dc := r.dc
			w := r.firstWord
			for b := r.first; b < r.end; b++ {
				abs := r.abs + b - r.first
				if abs >= s.total {
					break
				}
				h := r.words[w]
				n := int(h >> 16)
				mcu, ui := abs/len(s.units), abs%len(s.units)
				u := &s.units[ui]
				c := int(u.c)
				dc[c] += int32(int16(h))
				hi, vi := d.comp[c].h, d.comp[c].v
				bx, by := hi*(mcu%s.mxx)+int(u.j)%hi, vi*(mcu/s.mxx)+int(u.j)/hi
				o := in.Blocks[4*abs : 4*abs+4 : 4*abs+4]
				o[0] = uint32(base + w + 1)
				o[1] = uint32(n) | uint32(c)<<16 | uint32(d.comp[c].tq)<<18
				o[2] = uint32(dc[c] * u.qt[0])
				o[3] = uint32(in.PlaneOff[c] + 8*by*in.Strides[c] + 8*bx)
				w += 1 + n
			}
		}(base)
		base += len(r.words)
	}
	wg.Wait()
	return in, nil
}

// Picture returns the picture of the output out of the inverse DCT of in (OutLen bytes). The
// picture uses out.
func (in *GPUInput) Picture(out []byte) image.Image {
	if in.Planes == 1 {
		return &image.Gray{Pix: out[:in.OutLen], Stride: in.Strides[0], Rect: image.Rect(0, 0, in.Width, in.Height)}
	}
	plane := func(c int) []byte { return out[in.PlaneOff[c] : in.PlaneOff[c]+in.Strides[c]*in.planeH[c]] }
	return &image.YCbCr{Y: plane(0), Cb: plane(1), Cr: plane(2), YStride: in.Strides[0], CStride: in.Strides[1],
		SubsampleRatio: in.Ratio, Rect: image.Rect(0, 0, in.Width, in.Height)}
}

// RenderCPU makes the output of in on the CPU, block by block, as the compute shader does: for
// the tests of a GPU.
func (in *GPUInput) RenderCPU() []byte {
	out := make([]byte, in.OutLen)
	var all []uint32
	for _, w := range in.Words {
		all = append(all, w...)
	}
	for i := 0; i < len(in.Blocks); i += 4 {
		var b block
		first, n := int(in.Blocks[i]), int(in.Blocks[i+1]&0xffff)
		c, q := int(in.Blocks[i+1]>>16&3), int(in.Blocks[i+1]>>18&3)
		b[0] = int32(in.Blocks[i+2])
		for _, e := range all[first : first+n] {
			k := e >> 16 & 63
			b[unzig[k]] = int32(int16(e)) * in.Quant[q][k]
		}
		idct(&b)
		writeBlock(&b, out[in.Blocks[i+3]:], in.Strides[c])
	}
	return out
}
