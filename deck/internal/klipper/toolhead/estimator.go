package toolhead

import "math"

// Estimator computes the time of a print with the motion planner of the toolhead: the same
// moves, junction speeds, look-ahead and trapezoids as ToolHead, with no MCU and no printer.
// The time of a move depends only on the limits of the printer, so the time of a file is known
// before it prints. The deck uses it for the time left of a print.

// Limits are the motion limits of a printer: the [printer] and [extruder] sections of its config.
type Limits struct {
	MaxVelocity, MaxAccel                 float64
	MinCruiseRatio, SquareCornerVelocity  float64 // in Klipper by default 0.5 and 5
	MaxZVelocity, MaxZAccel               float64 // 0: the max velocity and the max accel
	InstantCornerVelocity                 float64 // [extruder] instantaneous_corner_velocity, default 1
	MaxExtrudeOnlyVelocity, MaxExtrudeAcc float64 // 0: no limit of extrude only moves
}

// Mark is the time at a point of the file: Key is the place (a file offset), Time the seconds of
// the moves before it.
type Mark struct {
	Key  int64
	Time float64
}

type pendingMark struct {
	move *Move
	key  int64
}

// Estimator plans moves and adds up their time.
type Estimator struct {
	th      *ToolHead
	limits  Limits
	time    float64 // the time of the moves that left the look-ahead queue
	pending []pendingMark
	Marks   []Mark

	// OnMove, when set, gets each move after the look-ahead plans it: its start, cruise and end
	// speeds are known then.
	OnMove func(m *Move)
}

// NewEstimator returns an Estimator for a printer with the limits l.
func NewEstimator(l Limits) *Estimator {
	if l.MaxZVelocity <= 0 {
		l.MaxZVelocity = l.MaxVelocity
	}
	if l.MaxZAccel <= 0 {
		l.MaxZAccel = l.MaxAccel
	}
	if l.InstantCornerVelocity <= 0 {
		l.InstantCornerVelocity = 1
	}
	th := &ToolHead{commandedPos: []float64{0, 0, 0, 0}, maxVelocity: l.MaxVelocity, maxAccel: l.MaxAccel,
		minCruiseRatio: l.MinCruiseRatio, squareCornerVelocity: l.SquareCornerVelocity}
	th.calcJunctionDeviation()
	th.lookahead.reset()
	th.lookahead.setFlushTime(bufferTimeHigh)
	e := &Estimator{th: th, limits: l}
	th.extraAxes = []ExtraAxis{estimatorExtruder{e}}
	return e
}

// Position is the position of the toolhead: X, Y, Z and E.
func (e *Estimator) Position() []float64 { return append([]float64(nil), e.th.commandedPos...) }

// SetPosition sets the position with no move (G92, or the end of homing).
func (e *Estimator) SetPosition(pos []float64) {
	e.flush(false)
	copy(e.th.commandedPos, pos)
}

// Move plans a move to pos (X, Y, Z, E) at speed mm/s, as ToolHead.Move with the checks of the
// cartesian kinematics and the extruder.
func (e *Estimator) Move(pos []float64, speed float64) {
	th := e.th
	if speed <= 0 {
		return
	}
	move := NewMove(th, th.commandedPos, pos, speed)
	if move.MoveD == 0 {
		return
	}
	if move.IsKinematicMove && move.AxesD[2] != 0 {
		// A Z move: the slower Z axis limits it (CartKinematics.CheckMove).
		zRatio := move.MoveD / math.Abs(move.AxesD[2])
		move.LimitSpeed(e.limits.MaxZVelocity*zRatio, e.limits.MaxZAccel*zRatio)
	}
	if move.AxesD[3] != 0 {
		th.extraAxes[0].CheckMove(move, 3)
	}
	copy(th.commandedPos, move.EndPos)
	if th.lookahead.addMove(move) {
		e.flush(true)
	}
}

// Dwell waits seconds after the planned moves (G4).
func (e *Estimator) Dwell(seconds float64) {
	e.flush(false)
	e.time += max(0, seconds)
}

// SetLimits changes the limits that are not NaN, as SET_VELOCITY_LIMIT and M204.
func (e *Estimator) SetLimits(velocity, accel, squareCornerVelocity, minCruiseRatio float64) {
	e.th.SetMaxVelocities(velocity, accel, squareCornerVelocity, minCruiseRatio)
}

// Mark records the time at key, after the moves planned so far.
func (e *Estimator) Mark(key int64) {
	if last := e.th.lookahead.last(); last != nil {
		e.pending = append(e.pending, pendingMark{last, key})
		return
	}
	e.Marks = append(e.Marks, Mark{key, e.time})
}

// Finish plans the last moves, and returns the time of all the moves.
func (e *Estimator) Finish() float64 {
	e.flush(false)
	return e.time
}

// flush takes the moves that the look-ahead finished, as ToolHead.processLookahead, adds their
// time, and records the marks after them.
func (e *Estimator) flush(lazy bool) {
	moves := e.th.lookahead.flush(lazy)
	for _, m := range moves {
		e.time += m.AccelT + m.CruiseT + m.DecelT
		if e.OnMove != nil {
			e.OnMove(m)
		}
		for len(e.pending) > 0 && e.pending[0].move == m {
			e.Marks = append(e.Marks, Mark{e.pending[0].key, e.time})
			e.pending = e.pending[1:]
		}
	}
	if !lazy {
		e.th.lookahead.setFlushTime(bufferTimeHigh)
	}
}

// estimatorExtruder is the extruder of an Estimator: the limits of PrinterExtruder, with no
// heater and no steppers.
type estimatorExtruder struct{ e *Estimator }

func (x estimatorExtruder) Name() string                                     { return "extruder" }
func (x estimatorExtruder) ProcessMove(printTime float64, move *Move, i int) {}

// CheckMove limits an extrude only move, or a retraction, as PrinterExtruder.CheckMove.
func (x estimatorExtruder) CheckMove(move *Move, i int) {
	l := x.e.limits
	axisR := move.AxesR[i]
	if (move.AxesD[0] == 0 && move.AxesD[1] == 0) || axisR < 0 {
		if l.MaxExtrudeOnlyVelocity > 0 && l.MaxExtrudeAcc > 0 {
			inv := 1 / math.Abs(axisR)
			move.LimitSpeed(l.MaxExtrudeOnlyVelocity*inv, l.MaxExtrudeAcc*inv)
		}
	}
}

// CalcJunction limits the speed at a change of the extrusion, as PrinterExtruder.CalcJunction.
func (x estimatorExtruder) CalcJunction(prevMove, move *Move, i int) float64 {
	if diffR := move.AxesR[i] - prevMove.AxesR[i]; diffR != 0 {
		return math.Pow(x.e.limits.InstantCornerVelocity/math.Abs(diffR), 2)
	}
	return move.MaxCruiseV2
}
