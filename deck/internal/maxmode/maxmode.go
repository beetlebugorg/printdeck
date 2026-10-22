// Package maxmode computes the speed of each move in max mode, an experiment of Printdeck. In max
// mode the printer ignores the F speeds of the G-code. Each move gets a top speed from the limits
// of the hotend and the printer, and from the feature type comments of the slicer.
//
// The top speed replaces the F speed as the cruise speed of the move in the motion planner of the
// toolhead. The planner is the same as for a normal print. Its look-ahead slows the toolhead into
// each corner by the junction deviation (square_corner_velocity) and accelerates it out again. A
// move also cannot go faster than its length and max_accel permit. So a long straight run gets to
// the top speed, and a short segment between two sharp corners stays slow. Max mode does not look
// ahead along the path itself: the look-ahead of the planner already uses the lengths of the next
// moves and the angles of the next corners.
//
// The top speed of an extruding move is the lowest of these speeds:
//   - The flow limit: max_flow (mm³/s) divided by the cross-section of the line (mm²). The
//     cross-section is the filament area times the E per mm of the move, so it includes the line
//     width and the layer height.
//   - The speed of the feature type of the move (outer wall, bridge, ...), when it has one.
//   - first_layer_speed, on the first layer.
//   - The max velocity of the printer.
//
// A layer that prints in less than min_layer_time gets a factor below 1 on its extruding moves, so
// that the layer has time to cool. The factor does not make a move slower than min_speed.
//
// A travel move goes at travel_speed, or at the max velocity when travel_speed is 0. A move with no
// X or Y (a Z move, a retraction) keeps the F speed of the G-code. The moves before the first layer
// comment (the start G-code) also keep it.
package maxmode

import (
	"math"
	"strings"
)

// Feature is the feature type of an extrusion, from the comments of the slicer.
type Feature int

const (
	Other Feature = iota
	OuterWall
	InnerWall
	Overhang
	Bridge
	TopSurface
	SolidInfill
	Infill
	Support
	Skirt
	numFeatures
)

// featureNames are the names of the features in the status and in the config options
// (<name>_speed).
var featureNames = [numFeatures]string{"other", "outer_wall", "inner_wall", "overhang", "bridge",
	"top_surface", "solid_infill", "infill", "support", "skirt"}

func (f Feature) String() string { return featureNames[f] }

// ClassifyFeature returns the feature of a type name of Cura (;TYPE:WALL-OUTER), PrusaSlicer
// (;TYPE:External perimeter) or OrcaSlicer (;FEATURE: Outer wall).
func ClassifyFeature(name string) Feature {
	n := strings.ToLower(strings.TrimSpace(name))
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(n, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("overhang"):
		return Overhang
	case has("bridge"):
		return Bridge
	case has("wall-outer", "outer wall", "external perimeter"):
		return OuterWall
	case has("wall-inner", "inner wall", "perimeter"):
		return InnerWall
	case has("top", "ironing"):
		return TopSurface
	case has("support"):
		return Support
	case has("skirt", "brim"):
		return Skirt
	case has("skin", "solid", "bottom", "gap"):
		return SolidInfill
	case has("fill"):
		return Infill
	}
	return Other
}

// Settings are the options of the [max_mode] config section.
type Settings struct {
	Enable           bool
	MaxFlow          float64              // mm³/s: the melt rate of the hotend
	FilamentDiameter float64              // mm
	FeatureSpeed     [numFeatures]float64 // mm/s for each feature; 0 is no limit
	FirstLayerSpeed  float64              // mm/s; 0 is no limit
	TravelSpeed      float64              // mm/s; 0 is the max velocity
	MinLayerTime     float64              // s; 0 is no minimum
	MinSpeed         float64              // mm/s: the lowest speed of the min_layer_time factor

	// MaxVelocity is the max velocity of the printer, in mm/s. It is not a config option: the
	// toolhead (or the plan) sets it.
	MaxVelocity float64
}

// Defaults are the settings of a [max_mode] section with no options.
func Defaults() Settings {
	s := Settings{MaxFlow: 12, FilamentDiameter: 1.75, FirstLayerSpeed: 30, MinLayerTime: 8, MinSpeed: 10}
	s.FeatureSpeed[OuterWall] = 80
	s.FeatureSpeed[Overhang] = 30
	s.FeatureSpeed[Bridge] = 30
	s.FeatureSpeed[TopSurface] = 100
	return s
}

// Load sets the number options from get, which returns the value of an option or the default def.
func (s *Settings) Load(get func(option string, def float64) float64) {
	s.MaxFlow = get("max_flow", s.MaxFlow)
	s.FirstLayerSpeed = get("first_layer_speed", s.FirstLayerSpeed)
	s.TravelSpeed = get("travel_speed", s.TravelSpeed)
	s.MinLayerTime = get("min_layer_time", s.MinLayerTime)
	s.MinSpeed = get("min_speed", s.MinSpeed)
	for f := range numFeatures {
		s.FeatureSpeed[f] = get(featureNames[f]+"_speed", s.FeatureSpeed[f])
	}
}

// Shaper follows the feature and layer comments of a G-code stream, and returns the speed of each
// move.
type Shaper struct {
	S       Settings
	feature Feature
	layer   int     // the layer, from 0; -1 before the first layer comment
	moved   bool    // an X or Y move since the last layer comment
	factor  float64 // the min_layer_time factor of the layer
}

// NewShaper returns a Shaper before the first layer.
func NewShaper(s Settings) *Shaper { return &Shaper{S: s, layer: -1, factor: 1} }

// Layer returns the layer from 0, or -1 before the first layer comment.
func (sh *Shaper) Layer() int { return sh.layer }

// Feature returns the feature of the last type comment.
func (sh *Shaper) Feature() Feature { return sh.feature }

// Factor returns the min_layer_time factor of the layer.
func (sh *Shaper) Factor() float64 { return sh.factor }

// SetFactor sets the min_layer_time factor of the layer.
func (sh *Shaper) SetFactor(f float64) { sh.factor = f }

// Comment reads a G-code comment: the text after the ';'. It returns true at the start of a new
// layer, and then the factor is 1 until SetFactor. Two layer comments with no X or Y move between
// them start one layer, because some slicers write two comments for a layer change.
func (sh *Shaper) Comment(c string) bool {
	c = strings.TrimSpace(c)
	u := strings.ToUpper(c)
	switch {
	case strings.HasPrefix(u, "TYPE:"):
		sh.feature = ClassifyFeature(c[len("TYPE:"):])
	case strings.HasPrefix(u, "FEATURE:"):
		sh.feature = ClassifyFeature(c[len("FEATURE:"):])
	case strings.HasPrefix(u, "LAYER:") || u == "LAYER_CHANGE":
		if sh.layer < 0 || sh.moved {
			sh.layer++
			sh.moved = false
			sh.factor = 1
			return true
		}
	}
	return false
}

// Speed returns the top speed of a move by d (X, Y, Z and E, in mm) in mm/s, or 0 to keep the F
// speed of the G-code.
func (sh *Shaper) Speed(d []float64) float64 {
	if d[0] != 0 || d[1] != 0 {
		sh.moved = true
	}
	return sh.speed(d, sh.factor)
}

// extruding reports a move in X or Y that extrudes.
func extruding(d []float64) bool { return (d[0] != 0 || d[1] != 0) && d[3] > 0 }

func (sh *Shaper) speed(d []float64, factor float64) float64 {
	s := &sh.S
	if sh.layer < 0 || (d[0] == 0 && d[1] == 0) {
		return 0
	}
	vmax := s.MaxVelocity
	if vmax <= 0 {
		vmax = math.Inf(1)
	}
	if !extruding(d) {
		if s.TravelSpeed > 0 {
			return min(s.TravelSpeed, vmax)
		}
		if math.IsInf(vmax, 1) {
			return 0
		}
		return vmax
	}
	v := vmax
	if s.MaxFlow > 0 && s.FilamentDiameter > 0 {
		// The cross-section of the line: the filament volume per mm of the move.
		area := math.Pi * s.FilamentDiameter * s.FilamentDiameter / 4
		cross := area * d[3] / math.Sqrt(d[0]*d[0]+d[1]*d[1]+d[2]*d[2])
		v = min(v, s.MaxFlow/cross)
	}
	if c := s.FeatureSpeed[sh.feature]; c > 0 {
		v = min(v, c)
	}
	if sh.layer == 0 && s.FirstLayerSpeed > 0 {
		v = min(v, s.FirstLayerSpeed)
	}
	if math.IsInf(v, 1) {
		return 0
	}
	if factor < 1 {
		v = max(v*factor, min(v, s.MinSpeed))
	}
	return v
}

// factor returns the min_layer_time factor of a layer from the seconds of its extruding moves and
// of its other moves at the top speeds.
func (s *Settings) factor(extrude, other float64) float64 {
	if s.MinLayerTime <= 0 || extrude <= 0 || extrude+other >= s.MinLayerTime {
		return 1
	}
	return extrude / (s.MinLayerTime - other)
}
