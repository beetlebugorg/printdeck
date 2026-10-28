package camera

// The printed target of a calibration of KindTowers: 5 square towers on the bed, at the 4 corners
// of the print area and in its middle. Each tower has a cap: a smaller square on its top, so the
// top is easy to see and to tap. The user taps the bottom and the top of each tower on a picture.
// The marks are on the axis of a tower: the middle of its foot on the bed, and the middle of the
// top of its cap.

// The size of the towers, in mm.
const (
	TowerSide    = 5.0  // the side of a tower
	TowerHeight  = 15.0 // the height of a tower, under its cap
	TowerCap     = 0.9  // the height of the cap: 3 layers
	TowerCapSide = 2.6  // the side of the cap
	TowerInset   = 25.0 // from the edges of the print area to the axis of a corner tower
)

// TowerTop is the height of the top of a tower with its cap, in mm.
const TowerTop = TowerHeight + TowerCap

// TowerCount is the number of towers.
const TowerCount = 5

// PrintArea is the print area of a printer with the axis limits lo and hi (X and Y, in mm): a
// square from the corner (max(lo, 0)), as large as the shorter axis. The travel of an axis is
// often longer than the bed, mostly at the maximum (for example the Y of a LulzBot Mini 2: 192
// mm of travel on a bed of 160 mm), so the deck uses the shorter axis for both.
func PrintArea(lo, hi [2]float64) (alo, ahi [2]float64) {
	x0, y0 := max(lo[0], 0), max(lo[1], 0)
	side := min(hi[0]-x0, hi[1]-y0)
	return [2]float64{x0, y0}, [2]float64{x0 + side, y0 + side}
}

// TowerPlaces are the places of the axes of the towers (X and Y, in mm) in the print area lo to
// hi, in the order of the marks: the corner of the smallest X and Y, the corner of the largest X,
// the corner of the largest X and Y, the corner of the largest Y, and the middle. A small area
// gets a smaller inset, so the towers stay apart.
func TowerPlaces(lo, hi [2]float64) [][2]float64 {
	inset := min(TowerInset, (hi[0]-lo[0])/4, (hi[1]-lo[1])/4)
	x0, x1 := lo[0]+inset, hi[0]-inset
	y0, y1 := lo[1]+inset, hi[1]-inset
	return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {(x0 + x1) / 2, (y0 + y1) / 2}}
}

// TowerMark is a mark of a calibration with towers: the bottom or the top of a tower, at one of
// the places of the bed.
type TowerMark struct {
	Tower int  // the tower: an index of TowerPlaces
	Top   bool // the top of the cap, or else the foot of the tower
	Bed   int  // the place of the bed: 0 for the first, 1 for the second
}

// TowerMarks are the marks of a calibration with towers, in their order: the bottom and then the
// top of each tower. With bedY (a bed that moves in Y), the bed then goes to a second place, and
// the marks go on with the top of each tower. The two places of the bed give the depth in Y.
func TowerMarks(bedY bool) []TowerMark {
	var out []TowerMark
	for i := range TowerCount {
		out = append(out, TowerMark{Tower: i}, TowerMark{Tower: i, Top: true})
	}
	if bedY {
		for i := range TowerCount {
			out = append(out, TowerMark{Tower: i, Top: true, Bed: 1})
		}
	}
	return out
}

// Place is the place of the mark in the toolhead coordinates, for the towers at places.
func (m TowerMark) Place(places [][2]float64) [3]float64 {
	p := places[m.Tower]
	z := 0.0
	if m.Top {
		z = TowerTop
	}
	return [3]float64{p[0], p[1], z}
}

// TowerBedPlaces are the toolhead Y of the bed for the marks of a calibration with towers, for a
// printer with the Y travel lo to hi and the print area alo to ahi in Y: the middle of the print
// area, and then a third of the travel toward the largest Y (a bed that moves to the front), or
// toward the smallest Y when the travel has no space for it.
func TowerBedPlaces(lo, hi, alo, ahi float64) [2]float64 {
	y0 := (alo + ahi) / 2
	d := (hi - lo) / 3
	y1 := y0 + d
	if y1 > hi {
		y1 = max(lo, y0-d)
	}
	return [2]float64{y0, y1}
}
