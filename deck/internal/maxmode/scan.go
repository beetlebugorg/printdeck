package maxmode

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// The min_layer_time factor of a layer needs the time of the whole layer before its first move. A
// scan reads the moves of the layer ahead and adds up their time at the top speeds. The time of a
// move in the scan is its length over its top speed, with no acceleration, so the scan time is
// shorter than the real time. The factor is then a little lower than necessary: the layer gets more
// time to cool.

// State is the G-code state at the start of a scan.
type State struct {
	Pos          [4]float64 // the G-code position: X, Y, Z and E
	AbsXYZ, AbsE bool
	Speed        float64 // the F speed in mm/s
	Layer        int     // the layer before the scan, or -1
}

// StartState is the G-code state at the start of a file.
func StartState() State { return State{AbsXYZ: true, AbsE: true, Speed: 25, Layer: -1} }

// LayerFactors reads a G-code file and returns the min_layer_time factor of each layer, from layer
// 0. The comments before the offset start are ignored, as in the plan of the deck.
func LayerFactors(r io.Reader, s Settings, start int64) []float64 {
	return scan(r, s, StartState(), start, -1)
}

// NextLayerFactor reads G-code from a layer comment to the next one, and returns the
// min_layer_time factor of that layer. st is the G-code state before the layer comment.
func NextLayerFactor(r io.Reader, s Settings, st State) float64 {
	f := scan(r, s, st, 0, 1)
	if len(f) == 0 {
		return 1
	}
	return f[0]
}

// scan reads G-code and returns the factors of the layers that end in it, at most n (all for n <
// 0). A layer ends at the next layer comment or at the end of the file.
func scan(r io.Reader, s Settings, st State, start int64, n int) []float64 {
	sh := &Shaper{S: s, layer: st.Layer, moved: true, factor: 1}
	pos, feed := st.Pos, st.Speed
	absXYZ, absE := st.AbsXYZ, st.AbsE
	var factors []float64
	var extrudeT, otherT float64
	started := false // a layer comment started a layer in the scan
	endLayer := func() bool {
		if started {
			factors = append(factors, s.factor(extrudeT, otherT))
		}
		extrudeT, otherT, started = 0, 0, true
		return n >= 0 && len(factors) >= n
	}
	br := bufio.NewReaderSize(r, 1<<16)
	var off int64
	for {
		line, err := br.ReadString('\n')
		off += int64(len(line))
		if i := strings.IndexByte(line, ';'); i >= 0 {
			if off > start && sh.Comment(line[i+1:]) && endLayer() {
				return factors
			}
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) > 0 {
			switch cmd := strings.ToUpper(f[0]); cmd {
			case "G0", "G1", "G00", "G01", "G2", "G3", "G02", "G03":
				next := pos
				var ci, cj float64
				for _, w := range f[1:] {
					v, ok := word(w)
					if !ok {
						continue
					}
					switch c := w[0] | 0x20; c {
					case 'x', 'y', 'z':
						a := int(c - 'x')
						if absXYZ {
							next[a] = v
						} else {
							next[a] += v
						}
					case 'e':
						if absE {
							next[3] = v
						} else {
							next[3] += v
						}
					case 'f':
						if v > 0 {
							feed = v / 60
						}
					case 'i':
						ci = v
					case 'j':
						cj = v
					}
				}
				d := []float64{next[0] - pos[0], next[1] - pos[1], next[2] - pos[2], next[3] - pos[3]}
				if c := cmd[len(cmd)-1]; c == '2' || c == '3' {
					// An arc: its length along the arc in place of the chord, for the time and the
					// flow.
					d[0], d[1] = ArcLength(pos[0], pos[1], next[0], next[1], ci, cj, c == '2'), 0
				}
				pos = next
				if off <= start {
					continue
				}
				v := sh.Speed(d)
				if v <= 0 {
					v = feed
				}
				if s.MaxVelocity > 0 {
					v = min(v, s.MaxVelocity)
				}
				if !started || v <= 0 {
					continue
				}
				t := math.Sqrt(d[0]*d[0]+d[1]*d[1]+d[2]*d[2]) / v
				if extruding(d) {
					extrudeT += t
				} else {
					otherT += t
				}
			case "G90":
				absXYZ, absE = true, true
			case "G91":
				absXYZ, absE = false, false
			case "M82":
				absE = true
			case "M83":
				absE = false
			case "G92":
				for _, w := range f[1:] {
					if v, ok := word(w); ok {
						switch c := w[0] | 0x20; c {
						case 'x', 'y', 'z':
							pos[c-'x'] = v
						case 'e':
							pos[3] = v
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	if started {
		endLayer()
	}
	return factors
}

// ArcLength returns the length in X and Y of an arc from (ax, ay) to (bx, by) around the center at
// the offsets i and j from (ax, ay).
func ArcLength(ax, ay, bx, by, i, j float64, clockwise bool) float64 {
	cx, cy := ax+i, ay+j
	sweep := math.Atan2(by-cy, bx-cx) - math.Atan2(ay-cy, ax-cx)
	if clockwise && sweep >= 0 {
		sweep -= 2 * math.Pi
	} else if !clockwise && sweep <= 0 {
		sweep += 2 * math.Pi
	}
	return math.Abs(sweep) * math.Hypot(i, j)
}

// word reads the number of a G-code word such as X12.5.
func word(w string) (float64, bool) {
	if len(w) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(w[1:], 64)
	return v, err == nil
}
