package toolhead

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

var testLimits = Limits{MaxVelocity: 300, MaxAccel: 1000, MinCruiseRatio: 0, SquareCornerVelocity: 5}

// One move of 100 mm at 100 mm/s with 1000 mm/s²: 5 mm and 0.1 s to accelerate, the same to
// stop, and 90 mm at full speed: 1.1 s.
func TestEstimatorOneMove(t *testing.T) {
	e := NewEstimator(testLimits)
	e.Move([]float64{100, 0, 0, 0}, 100)
	if got := e.Finish(); !near(got, 1.1) {
		t.Errorf("time %v, want 1.1", got)
	}
}

// Two moves in a line take the time of one: the toolhead does not slow down between them.
func TestEstimatorStraightJunction(t *testing.T) {
	e := NewEstimator(testLimits)
	e.Move([]float64{50, 0, 0, 0}, 100)
	e.Move([]float64{100, 0, 0, 0}, 100)
	if got := e.Finish(); !near(got, 1.1) {
		t.Errorf("time %v, want 1.1", got)
	}
}

// A square corner slows the toolhead to the square corner velocity: more time than a straight
// line of the same length, less than a full stop.
func TestEstimatorCorner(t *testing.T) {
	e := NewEstimator(testLimits)
	e.Move([]float64{100, 0, 0, 0}, 100)
	e.Move([]float64{100, 100, 0, 0}, 100)
	corner := e.Finish()
	stop := 2 * 1.1
	straight := 2.1 // 200 mm: 0.2 s to accelerate and stop, 190 mm at full speed
	if corner <= straight || corner >= stop {
		t.Errorf("corner %v, want between %v and %v", corner, straight, stop)
	}
}

// The slow Z axis limits a Z move, and a dwell adds its time.
func TestEstimatorZAndDwell(t *testing.T) {
	l := testLimits
	l.MaxZVelocity, l.MaxZAccel = 10, 100
	e := NewEstimator(l)
	e.Move([]float64{0, 0, 10, 0}, 100) // 10 mm at 10 mm/s with 100 mm/s²: 0.5 mm, 0.1 s each end
	e.Dwell(2)
	if got := e.Finish(); !near(got, 1.1+2) {
		t.Errorf("time %v, want 3.1", got)
	}
}

// A mark has the time of the moves before it, also when the look-ahead holds those moves.
func TestEstimatorMarks(t *testing.T) {
	e := NewEstimator(testLimits)
	e.Mark(0)
	e.Move([]float64{100, 0, 0, 0}, 100)
	e.Mark(10)
	e.Dwell(1)
	e.Mark(20)
	e.Finish()
	want := []Mark{{0, 0}, {10, 1.1}, {20, 2.1}}
	if len(e.Marks) != len(want) {
		t.Fatalf("marks %v", e.Marks)
	}
	for i, m := range want {
		if e.Marks[i].Key != m.Key || !near(e.Marks[i].Time, m.Time) {
			t.Errorf("mark %d: %v, want %v", i, e.Marks[i], m)
		}
	}
}
