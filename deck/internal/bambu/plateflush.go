package bambu

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// FlushPlan is the flush that the G-code of a plate plans for each filament change, in mm³, by
// {from, to} filament number (from 1). The printer reports no flow, so these values are planned
// and the deck does not measure them.
//
// The source of a value is the "; VFLUSH_START" block of each "; filament change" of the file: the
// sum of its ";VG1 E" lengths, with the diameter of the new filament. The sum is equal to the L
// value of M620.10 in the two X2D files that were checked. A pair with more than one change has
// the median of its changes.
//
// flush_volumes_matrix is the source only for a pair with no block, and only when its length fits
// the filaments of the file. In the two X2D files that were checked, the matrix has 8 values for 5
// filaments, and its values differ from the blocks.
type FlushPlan map[[2]int]float64

// flushScan reads a FlushPlan from the comment lines of a G-code file.
type flushScan struct {
	lengths     map[[2]int][]float64 // the mm of each change, by pair
	pair        [2]int               // the change now, or zero
	inBlock     bool
	sum         float64
	seen        bool // the block has a length
	diameters   []float64
	diametersEq []float64 // the "=" form of the header, for a file with no ":" form
	matrix      []float64
	multipliers []float64
	nozzleMap   []float64
	filaments   int
}

// line reads one comment line.
func (s *flushScan) line(l string) {
	switch {
	case strings.HasPrefix(l, "; filament change "):
		s.pair, s.inBlock = [2]int{}, false
		var n, from, to int
		if _, err := fmt.Sscanf(l, "; filament change %d: filament %d to filament %d", &n, &from, &to); err == nil && from > 0 && to > 0 {
			s.pair = [2]int{from, to}
		}
	case strings.HasPrefix(l, "; VFLUSH_START"):
		s.inBlock, s.sum, s.seen = s.pair != [2]int{}, 0, false
	case strings.HasPrefix(l, "; VFLUSH_END"):
		if s.inBlock && s.seen {
			if s.lengths == nil {
				s.lengths = map[[2]int][]float64{}
			}
			s.lengths[s.pair] = append(s.lengths[s.pair], s.sum)
		}
		s.inBlock = false
	case s.inBlock && strings.HasPrefix(l, ";VG1 "):
		if v, ok := word(strings.Fields(l), 'E'); ok && v > 0 {
			s.sum += v
			s.seen = true
		}
	case strings.HasPrefix(l, "; filament_diameter: "):
		s.diameters = numberList(l[len("; filament_diameter: "):])
	case strings.HasPrefix(l, "; filament_diameter = "):
		s.diametersEq = numberList(l[len("; filament_diameter = "):])
	case strings.HasPrefix(l, "; flush_volumes_matrix = "):
		s.matrix = numberList(l[len("; flush_volumes_matrix = "):])
	case strings.HasPrefix(l, "; flush_multiplier = "):
		s.multipliers = numberList(l[len("; flush_multiplier = "):])
	case strings.HasPrefix(l, "; filament_map = "):
		s.nozzleMap = numberList(l[len("; filament_map = "):])
	case strings.HasPrefix(l, "; filament_type = "):
		s.filaments = len(strings.Split(l[len("; filament_type = "):], ";"))
	}
}

// numberList reads the numbers of a list with "," or ";" between them. A value that is not a
// number is NaN, so that each value keeps its index.
func numberList(v string) []float64 {
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' })
	out := make([]float64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			n = math.NaN()
		}
		out[i] = n
	}
	return out
}

// diameter returns the diameter of the filament (from 1), or 0 when the file does not give it.
func (s *flushScan) diameter(filament int) float64 {
	list := s.diameters
	if len(list) == 0 {
		list = s.diametersEq
	}
	if filament < 1 || filament > len(list) || !(list[filament-1] > 0) {
		return 0
	}
	return list[filament-1]
}

// plan returns the flush of each pair, or nil when the file gives none.
func (s *flushScan) plan() FlushPlan {
	out := FlushPlan{}
	for pair, mm := range s.lengths {
		if d := s.diameter(pair[1]); d > 0 {
			out[pair] = medianOf(mm) * math.Pi * d * d / 4
		}
	}
	s.addMatrix(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// addMatrix adds the value of flush_volumes_matrix for each pair with no block. The matrix has
// one table of filaments × filaments for each nozzle. A length that does not fit adds no value.
func (s *flushScan) addMatrix(out FlushPlan) {
	n := s.filaments
	if n < 2 || len(s.matrix) == 0 || len(s.matrix)%(n*n) != 0 {
		return
	}
	tables := len(s.matrix) / (n * n)
	for from := 1; from <= n; from++ {
		for to := 1; to <= n; to++ {
			pair := [2]int{from, to}
			if from == to {
				continue
			}
			if _, ok := out[pair]; ok {
				continue
			}
			table := 0
			if tables > 1 {
				if to > len(s.nozzleMap) || !(s.nozzleMap[to-1] >= 1) {
					continue
				}
				table = int(s.nozzleMap[to-1]) - 1
				if table >= tables {
					continue
				}
			}
			v := s.matrix[table*n*n+(from-1)*n+to-1]
			if table < len(s.multipliers) && s.multipliers[table] > 0 {
				v *= s.multipliers[table]
			}
			if v > 0 {
				out[pair] = v
			}
		}
	}
}

// medianOf returns the median of v, which has one value or more.
func medianOf(v []float64) float64 {
	v = slices.Sorted(slices.Values(v))
	m := len(v) / 2
	if len(v)%2 == 1 {
		return v[m]
	}
	return (v[m-1] + v[m]) / 2
}
