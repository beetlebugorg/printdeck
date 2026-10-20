// Package motion is the motion code of the Klipper host: the queue of trapezoid moves, the step
// time solver of each stepper, the step compression, and the step sync of the boards.
//
// Ported to Go from klippy/chelper (trapq.c, itersolve.c, kin_*.c, stepcompress.c,
// steppersync.c) of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. Klipper's Python code calls this C code through
// cffi. The Go code calls the Go functions directly.
//
//	Copyright (C) 2018-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package motion

// Coord is an XYZ position or direction.
type Coord struct{ X, Y, Z float64 }

// Axis returns the coordinate of axis 0 (x), 1 (y) or 2 (z).
func (c Coord) Axis(i int) float64 {
	switch i {
	case 0:
		return c.X
	case 1:
		return c.Y
	}
	return c.Z
}

// Move is one part of a trapezoid move with a constant acceleration (struct move).
type Move struct {
	PrintTime, MoveT  float64
	StartV, HalfAccel float64
	StartPos, AxesR   Coord
	prev, next        *Move
}

// Prev and Next are the moves before and after this move in its list.
func (m *Move) Prev() *Move { return m.prev }
func (m *Move) Next() *Move { return m.next }

// GetDistance returns the distance moved at a time in the move.
func (m *Move) GetDistance(moveTime float64) float64 {
	return (m.StartV + m.HalfAccel*moveTime) * moveTime
}

// GetCoord returns the XYZ position at a time in the move.
func (m *Move) GetCoord(moveTime float64) Coord {
	d := m.GetDistance(moveTime)
	return Coord{m.StartPos.X + m.AxesR.X*d, m.StartPos.Y + m.AxesR.Y*d, m.StartPos.Z + m.AxesR.Z*d}
}

// moveList is a doubly linked list of moves with a root node (list.h).
type moveList struct{ root Move }

func (l *moveList) init()               { l.root.next, l.root.prev = &l.root, &l.root }
func (l *moveList) empty() bool         { return l.root.next == &l.root }
func (l *moveList) first() *Move        { return l.root.next }
func (l *moveList) last() *Move         { return l.root.prev }
func (l *moveList) isEnd(m *Move) bool  { return m == &l.root }
func (l *moveList) addHead(m *Move)     { insertBetween(m, &l.root, l.root.next) }
func (l *moveList) addTail(m *Move)     { insertBetween(m, l.root.prev, &l.root) }
func addBefore(m, before *Move)         { insertBetween(m, before.prev, before) }
func insertBetween(m, prev, next *Move) { m.prev, m.next = prev, next; prev.next = m; next.prev = m }
func del(m *Move)                       { m.prev.next = m.next; m.next.prev = m.prev; m.prev, m.next = nil, nil }

// NeverTime is a print time after all moves.
const NeverTime = 9999999999999999.9

const maxNullMove = 1.0

// TrapQ is a queue of trapezoid moves, and the history of the moves that are done (struct trapq).
type TrapQ struct {
	moves, history moveList
}

// NewTrapQ makes a queue with its head and tail sentinel moves (trapq_alloc).
func NewTrapQ() *TrapQ {
	tq := &TrapQ{}
	tq.moves.init()
	tq.history.init()
	head, tail := &Move{PrintTime: -1.0}, &Move{PrintTime: NeverTime, MoveT: NeverTime}
	tq.moves.addHead(head)
	tq.moves.addTail(tail)
	return tq
}

// HeadSentinel and TailSentinel are the first and last moves of the queue. They are not real
// moves: the solver uses them to find the ends of the queue.
func (tq *TrapQ) HeadSentinel() *Move { return tq.moves.first() }
func (tq *TrapQ) TailSentinel() *Move { return tq.moves.last() }

// CheckSentinels updates the tail sentinel to the end of the last move.
func (tq *TrapQ) CheckSentinels() {
	tail := tq.moves.last()
	if tail.PrintTime != 0 {
		// Already up to date
		return
	}
	m := tail.prev
	if m == tq.moves.first() {
		// No moves at all on this list
		tail.PrintTime = NeverTime
		return
	}
	tail.PrintTime = m.PrintTime + m.MoveT
	tail.StartPos = m.GetCoord(m.MoveT)
}

// AddMove adds a move, after a null move for a time gap.
func (tq *TrapQ) AddMove(m *Move) {
	tail := tq.moves.last()
	prev := tail.prev
	if prev.PrintTime+prev.MoveT < m.PrintTime {
		// Add a null move to fill time gap
		null := &Move{StartPos: m.StartPos}
		if prev.PrintTime <= 0. && m.PrintTime > maxNullMove {
			// Limit the first null move to improve numerical stability
			null.PrintTime = m.PrintTime - maxNullMove
		} else {
			null.PrintTime = prev.PrintTime + prev.MoveT
		}
		null.MoveT = m.PrintTime - null.PrintTime
		addBefore(null, tail)
	}
	addBefore(m, tail)
	tail.PrintTime = 0.
}

// Append adds a trapezoid move: its acceleration, cruise and deceleration parts (trapq_append).
func (tq *TrapQ) Append(printTime, accelT, cruiseT, decelT, startX, startY, startZ,
	axesRX, axesRY, axesRZ, startV, cruiseV, accel float64) {
	startPos := Coord{startX, startY, startZ}
	axesR := Coord{axesRX, axesRY, axesRZ}
	if accelT != 0 {
		m := &Move{PrintTime: printTime, MoveT: accelT, StartV: startV, HalfAccel: .5 * accel,
			StartPos: startPos, AxesR: axesR}
		tq.AddMove(m)
		printTime += accelT
		startPos = m.GetCoord(accelT)
	}
	if cruiseT != 0 {
		m := &Move{PrintTime: printTime, MoveT: cruiseT, StartV: cruiseV, StartPos: startPos, AxesR: axesR}
		tq.AddMove(m)
		printTime += cruiseT
		startPos = m.GetCoord(cruiseT)
	}
	if decelT != 0 {
		m := &Move{PrintTime: printTime, MoveT: decelT, StartV: cruiseV, HalfAccel: -.5 * accel,
			StartPos: startPos, AxesR: axesR}
		tq.AddMove(m)
	}
}

// FinalizeMoves moves the moves that end before printTime to the history, and removes the history
// before clearHistoryTime.
func (tq *TrapQ) FinalizeMoves(printTime, clearHistoryTime float64) {
	head, tail := tq.moves.first(), tq.moves.last()
	// Move expired moves from main "moves" list to "history" list
	for {
		m := head.next
		if m == tail {
			tail.PrintTime = NeverTime
			break
		}
		if m.PrintTime+m.MoveT > printTime {
			break
		}
		del(m)
		if m.StartV != 0 || m.HalfAccel != 0 {
			tq.history.addHead(m)
		}
	}
	// Free old moves from history list
	if tq.history.empty() {
		return
	}
	latest := tq.history.first()
	for {
		m := tq.history.last()
		if m == latest || m.PrintTime+m.MoveT > clearHistoryTime {
			break
		}
		del(m)
	}
}

// SetPosition notes a position change in the history.
func (tq *TrapQ) SetPosition(printTime, x, y, z float64) {
	// Flush all moves from trapq
	tq.FinalizeMoves(NeverTime, 0)
	// Prune any moves in the trapq history that were interrupted
	for !tq.history.empty() {
		m := tq.history.first()
		if m.PrintTime < printTime {
			if m.PrintTime+m.MoveT > printTime {
				m.MoveT = printTime - m.PrintTime
			}
			break
		}
		del(m)
	}
	// Add a marker to the trapq history
	tq.history.addHead(&Move{PrintTime: printTime, StartPos: Coord{x, y, z}})
}

// PullMove is a move of the history, for the motion reports (struct pull_move).
type PullMove struct {
	PrintTime, MoveT       float64
	StartV, Accel          float64
	StartX, StartY, StartZ float64
	XR, YR, ZR             float64
}

func pullMove(m *Move) PullMove {
	return PullMove{m.PrintTime, m.MoveT, m.StartV, 2. * m.HalfAccel, m.StartPos.X, m.StartPos.Y,
		m.StartPos.Z, m.AxesR.X, m.AxesR.Y, m.AxesR.Z}
}

// ExtractOld returns up to max moves between startTime and endTime, the newest first.
func (tq *TrapQ) ExtractOld(max int, startTime, endTime float64) []PullMove {
	var res []PullMove
	tail := tq.moves.last()
	for m := tq.moves.last(); !tq.moves.isEnd(m); m = m.prev {
		if m == tail {
			continue
		}
		if startTime >= m.PrintTime+m.MoveT || len(res) >= max {
			break
		}
		if endTime <= m.PrintTime || (m.StartV == 0 && m.HalfAccel == 0) {
			continue
		}
		res = append(res, pullMove(m))
	}
	for m := tq.history.first(); !tq.history.isEnd(m); m = m.next {
		if startTime >= m.PrintTime+m.MoveT || len(res) >= max {
			break
		}
		if endTime <= m.PrintTime {
			continue
		}
		res = append(res, pullMove(m))
	}
	return res
}
