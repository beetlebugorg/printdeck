package gcode

import (
	"math"
	"sort"
	"strings"
)

// Kind is the kind of a move, from the ";TYPE:" comments of the slicer.
type Kind uint8

const (
	KindOther Kind = iota
	KindOuterWall
	KindInnerWall
	KindInfill
	KindSkin // top and bottom
	KindSupport
	KindSkirt
	kinds
)

var kindNames = [kinds]string{"Other", "Outer wall", "Inner wall", "Infill", "Top/bottom", "Support", "Skirt"}

func (k Kind) String() string { return kindNames[k] }

// kindOf reads the type names of Cura, PrusaSlicer, OrcaSlicer, and SuperSlicer.
func kindOf(t string) Kind {
	t = strings.ToLower(t)
	switch {
	case t == "wall-outer" || strings.Contains(t, "external perimeter") || t == "outer wall" || t == "overhang perimeter":
		return KindOuterWall
	case t == "wall-inner" || t == "perimeter" || t == "inner wall":
		return KindInnerWall
	case t == "skin" || strings.Contains(t, "solid infill") || strings.Contains(t, "surface") || t == "bridge infill" || t == "ironing":
		return KindSkin
	case t == "fill" || t == "internal infill" || t == "sparse infill":
		return KindInfill
	case strings.HasPrefix(t, "support"):
		return KindSupport
	case strings.Contains(t, "skirt") || t == "brim":
		return KindSkirt
	}
	return KindOther
}

// Speed is the typical speed of a part of the print, in mm/s.
type Speed struct {
	Name string
	MMs  float64
}

// The names of the extra speeds in Model.Speeds.
const (
	FirstLayer = "First layer"
	Travel     = "Travel"
)

// speedStats keeps, for each kind, how many mm were printed at each speed.
type speedStats struct {
	kinds  [kinds]map[float64]float64
	first  map[float64]float64
	travel map[float64]float64
}

func newSpeedStats() *speedStats {
	s := &speedStats{first: map[float64]float64{}, travel: map[float64]float64{}}
	for i := range s.kinds {
		s.kinds[i] = map[float64]float64{}
	}
	return s
}

func (s *speedStats) add(k Kind, firstLayer bool, mms, length float64) {
	if mms <= 0 {
		return
	}
	mms = math.Round(mms*10) / 10
	s.kinds[k][mms] += length
	if firstLayer {
		s.first[mms] += length
	}
}

func (s *speedStats) addTravel(mms, length float64) {
	if mms > 0 {
		s.travel[math.Round(mms*10)/10] += length
	}
}

// median is the speed at which half of the length is printed: the speed of most of it.
func median(m map[float64]float64) (float64, bool) {
	total := 0.0
	keys := make([]float64, 0, len(m))
	for k, v := range m {
		keys = append(keys, k)
		total += v
	}
	if total == 0 {
		return 0, false
	}
	sort.Float64s(keys)
	acc := 0.0
	for _, k := range keys {
		acc += m[k]
		if acc >= total/2 {
			return k, true
		}
	}
	return keys[len(keys)-1], true
}

func (s *speedStats) result() []Speed {
	var out []Speed
	for _, k := range []Kind{KindOuterWall, KindInnerWall, KindInfill, KindSkin, KindSupport, KindSkirt} {
		if v, ok := median(s.kinds[k]); ok {
			out = append(out, Speed{k.String(), v})
		}
	}
	if v, ok := median(s.first); ok {
		out = append(out, Speed{FirstLayer, v})
	}
	if v, ok := median(s.travel); ok {
		out = append(out, Speed{Travel, v})
	}
	return out
}
