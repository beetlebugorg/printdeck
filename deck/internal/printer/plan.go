package printer

import (
	"bufio"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/maxmode"
)

// The plan of a print: the time of each part of the G-code file on this printer, from the motion
// planner of Klipper (toolhead.Estimator) with the limits of the printer. The moves of the file
// alone give the time of the print. Only the heating, the pauses and a change of the speed add to
// it.

// planMarkEvery is the distance in bytes between two time marks of the plan.
const planMarkEvery = 16 << 10

// plan is the timeline of a file: the seconds of the moves before each offset.
type plan struct {
	pos   []int64
	sec   []float64
	total float64
}

// at returns the seconds of the moves before the offset pos, and the seconds of all the moves.
func (p *plan) at(pos int64) (elapsed, total float64) {
	n := len(p.pos)
	i := sort.Search(n, func(i int) bool { return p.pos[i] > pos })
	switch {
	case i == 0:
		return 0, p.total
	case i == n:
		return p.sec[n-1], p.total
	}
	f := float64(pos-p.pos[i-1]) / float64(max(1, p.pos[i]-p.pos[i-1]))
	return p.sec[i-1] + f*(p.sec[i]-p.sec[i-1]), p.total
}

// planFile plans the moves of a G-code file after the offset start (the end of the start macro):
// G0 to G3, G4, the absolute and relative modes, G92, M204, M220 and SET_VELOCITY_LIMIT. The
// commands that wait for heaters or that home take a time that the file does not tell.
//
// With mm, the moves have the speeds of max mode, as the max_mode module of the Klipper host gives
// them. factors are the min_layer_time factors of the layers (maxmode.LayerFactors).
func planFile(r io.Reader, start int64, l toolhead.Limits, mm *maxmode.Settings, factors []float64) *plan {
	e := toolhead.NewEstimator(l)
	var sh *maxmode.Shaper
	if mm != nil {
		sh = maxmode.NewShaper(*mm)
		sh.S.MaxVelocity = l.MaxVelocity
	}
	br := bufio.NewReaderSize(r, 1<<16)
	pos, lastMark := int64(0), int64(0)
	absXYZ, absE := true, true
	feed := 25.0 // mm/s, Klipper's default speed of gcode_move
	speedFactor := 1.0
	cur := e.Position() // the G-code position: X, Y, Z and E
	base := []float64{0, 0, 0, 0}
	e.Mark(start)
	for {
		line, err := br.ReadString('\n')
		pos += int64(len(line))
		if pos <= start {
			if err != nil {
				break
			}
			continue
		}
		if i := strings.IndexByte(line, ';'); i >= 0 {
			if sh != nil && sh.Comment(line[i+1:]) && sh.Layer() < len(factors) {
				sh.SetFactor(factors[sh.Layer()])
			}
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) > 0 {
			cmd := strings.ToUpper(f[0])
			switch cmd {
			case "G0", "G1", "G00", "G01", "G2", "G3", "G02", "G03":
				next := append([]float64(nil), cur...)
				var i, j float64
				for _, w := range f[1:] {
					v, ok := planWord(w)
					if !ok {
						continue
					}
					switch w[0] | 0x20 {
					case 'x', 'y', 'z':
						a := int(w[0]|0x20) - 'x'
						if absXYZ {
							next[a] = base[a] + v
						} else {
							next[a] = cur[a] + v
						}
					case 'e':
						if absE {
							next[3] = base[3] + v
						} else {
							next[3] = cur[3] + v
						}
					case 'f':
						if v > 0 {
							feed = v / 60
						}
					case 'i':
						i = v
					case 'j':
						j = v
					}
				}
				speed := feed * speedFactor
				arc := cmd == "G2" || cmd == "G3" || cmd == "G02" || cmd == "G03"
				if sh != nil {
					d := []float64{next[0] - cur[0], next[1] - cur[1], next[2] - cur[2], next[3] - cur[3]}
					if arc {
						d[0], d[1] = maxmode.ArcLength(cur[0], cur[1], next[0], next[1], i, j, cmd == "G2" || cmd == "G02"), 0
					}
					if v := sh.Speed(d); v > 0 {
						speed = v * speedFactor
					}
				}
				if arc {
					planArc(e, cur, next, i, j, cmd == "G2" || cmd == "G02", speed)
				} else {
					e.Move(next, speed)
				}
				cur = next
			case "G4":
				for _, w := range f[1:] {
					if v, ok := planWord(w); ok {
						switch w[0] | 0x20 {
						case 'p':
							e.Dwell(v / 1000)
						case 's':
							e.Dwell(v)
						}
					}
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
				// A new position with no move: the planner keeps its position, the G-code gets an
				// offset (gcode_move base_position).
				for _, w := range f[1:] {
					if v, ok := planWord(w); ok {
						switch c := w[0] | 0x20; c {
						case 'x', 'y', 'z':
							a := int(c) - 'x'
							base[a] = cur[a] - v
						case 'e':
							base[3] = cur[3] - v
						}
					}
				}
			case "M204":
				var s, p, t float64
				for _, w := range f[1:] {
					if v, ok := planWord(w); ok {
						switch w[0] | 0x20 {
						case 's':
							s = v
						case 'p':
							p = v
						case 't':
							t = v
						}
					}
				}
				if s == 0 && p > 0 && t > 0 {
					s = min(p, t)
				}
				if s > 0 {
					e.SetLimits(math.NaN(), s, math.NaN(), math.NaN())
				}
			case "M220":
				for _, w := range f[1:] {
					if v, ok := planWord(w); ok && w[0]|0x20 == 's' && v > 0 {
						speedFactor = v / 100
					}
				}
			case "SET_VELOCITY_LIMIT":
				get := func(name string) float64 {
					for _, w := range f[1:] {
						if k, v, ok := strings.Cut(w, "="); ok && strings.EqualFold(k, name) {
							if x, err := strconv.ParseFloat(v, 64); err == nil && x > 0 {
								return x
							}
						}
					}
					return math.NaN()
				}
				e.SetLimits(get("VELOCITY"), get("ACCEL"), get("SQUARE_CORNER_VELOCITY"), get("MINIMUM_CRUISE_RATIO"))
				if v := get("VELOCITY"); sh != nil && !math.IsNaN(v) {
					sh.S.MaxVelocity = v
				}
			}
		}
		if pos-lastMark >= planMarkEvery {
			e.Mark(pos)
			lastMark = pos
		}
		if err != nil {
			break
		}
	}
	e.Mark(pos)
	total := e.Finish()
	p := &plan{total: total}
	for _, m := range e.Marks {
		p.pos, p.sec = append(p.pos, m.Key), append(p.sec, m.Time)
	}
	return p
}

// planArc plans an arc as Klipper's gcode_arcs does: lines of at most 1 mm along the arc, from
// the center at the offsets i and j.
func planArc(e *toolhead.Estimator, from, to []float64, i, j float64, clockwise bool, speed float64) {
	cx, cy := from[0]+i, from[1]+j
	r := math.Hypot(i, j)
	a0 := math.Atan2(from[1]-cy, from[0]-cx)
	a1 := math.Atan2(to[1]-cy, to[0]-cx)
	sweep := a1 - a0
	if clockwise && sweep >= 0 {
		sweep -= 2 * math.Pi
	} else if !clockwise && sweep <= 0 {
		sweep += 2 * math.Pi
	}
	n := max(1, int(math.Ceil(math.Abs(sweep)*r)))
	p := append([]float64(nil), from...)
	for k := 1; k <= n; k++ {
		t := float64(k) / float64(n)
		a := a0 + sweep*t
		p[0], p[1] = cx+r*math.Cos(a), cy+r*math.Sin(a)
		p[2] = from[2] + (to[2]-from[2])*t
		p[3] = from[3] + (to[3]-from[3])*t
		if k == n {
			p[0], p[1] = to[0], to[1]
		}
		e.Move(append([]float64(nil), p...), speed)
	}
}

// planWord reads the number of a G-code word such as X12.5.
func planWord(w string) (float64, bool) {
	if len(w) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(w[1:], 64)
	return v, err == nil
}
