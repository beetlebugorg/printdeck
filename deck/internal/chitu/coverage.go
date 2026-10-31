package chitu

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Coverage returns the share of resin in each cell of the part of the .cbddlp file r with the
// header h, for the 3D view of a page (part.bin, before gzip). A cell is cellPx x cellPx pixels of
// the screen and the layers of about the same height. Its value is the share of its pixels in all
// its layers that are lit, from 0 to 255. The page puts the surface where the share crosses one
// half, so an edge sits at its place in the cell, and the view is as fine as the slices.
//
// The format, little-endian:
//
//	"PDC1", float32 the cell in mm (X and Y), float32 the slice in mm (Z),
//	uint16 W, H, D, uint16 0, uint32 the first layer of each of the D slices,
//	then each slice: uint32 the count of its bytes, then pairs of a value and a run (a varint
//	count of the cells with that value), for the W x H cells in rows (Y to the front).
//
// It is the mirror of the screen of a printer with projector 1, as the slicer shows the part.
func Coverage(r io.ReaderAt, h *Sliced, cellPx int) ([]byte, error) {
	if h.Magic != MagicCBDDLP || h.Layers == 0 || h.ResX == 0 || h.ResY == 0 || cellPx < 1 {
		return nil, errors.New("chitu: no layers that the deck reads")
	}
	pixelMM := float64(h.BedX) / float64(h.ResX)
	if pixelMM <= 0 || pixelMM > 1 {
		pixelMM = 0.072
	}
	step := 1
	if h.LayerHeight > 0 {
		step = max(1, int(math.Round(pixelMM*float64(cellPx)/float64(h.LayerHeight))))
	}
	resX, resY := int(h.ResX), int(h.ResY)
	W, H := (resX+cellPx-1)/cellPx, (resY+cellPx-1)/cellPx
	D := (int(h.Layers) + step - 1) / step
	out := []byte("PDC1")
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(pixelMM*float64(cellPx))))
	out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(float64(h.LayerHeight)*float64(step))))
	for _, v := range []int{W, H, D, 0} {
		out = binary.LittleEndian.AppendUint16(out, uint16(v))
	}
	for z := 0; z < D; z++ {
		out = binary.LittleEndian.AppendUint32(out, uint32(z*step+1))
	}
	def := make([]byte, layerDefSize)
	count := make([]uint32, W*H)
	var data []byte
	for z := 0; z < D; z++ {
		clear(count)
		layers := 0
		for n := z * step; n < min((z+1)*step, int(h.Layers)); n++ {
			if _, err := r.ReadAt(def, int64(h.layerTable)+int64(n)*layerDefSize); err != nil {
				return nil, err
			}
			addr, size := binary.LittleEndian.Uint32(def[12:]), binary.LittleEndian.Uint32(def[16:])
			if size == 0 || size > 64<<20 {
				continue
			}
			if cap(data) < int(size) {
				data = make([]byte, size)
			}
			data = data[:size]
			if _, err := r.ReadAt(data, int64(addr)); err != nil {
				return nil, err
			}
			addRuns(data, resX, resY, cellPx, W, count)
			layers++
		}
		full := uint32(max(1, layers) * cellPx * cellPx)
		// The slice: runs of the same value. Most cells are empty or full.
		var body []byte
		val, run := -1, 0
		flush := func() {
			if run > 0 {
				body = append(body, byte(val))
				body = binary.AppendUvarint(body, uint64(run))
			}
		}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				// The mirror of the screen, for a printer with projector 1.
				sx := x
				if h.Mirror {
					sx = W - 1 - x
				}
				v := int(math.Round(float64(count[y*W+sx]) * 255 / float64(full)))
				if v != val {
					flush()
					val, run = v, 0
				}
				run++
			}
		}
		flush()
		out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
		out = append(out, body...)
	}
	return out, nil
}

// addRuns adds the lit pixels of the 1-bit run-length code of a layer to the count of each cell
// of cellPx x cellPx pixels.
func addRuns(data []byte, resX, resY, cellPx, W int, count []uint32) {
	pos, total := 0, resX*resY
	for _, b := range data {
		run := int(b & 0x7F)
		if b&0x80 != 0 {
			for end := min(pos+run, total); pos < end; {
				x, y := pos%resX, pos/resX
				n := min(end-pos, resX-x)
				row := (y / cellPx) * W
				for i := 0; i < n; {
					cell := (x + i) / cellPx
					next := min(n, (cell+1)*cellPx-x)
					count[row+cell] += uint32(next - i)
					i = next
				}
				pos += n
			}
		} else {
			pos += run
		}
		if pos >= total {
			return
		}
	}
}
