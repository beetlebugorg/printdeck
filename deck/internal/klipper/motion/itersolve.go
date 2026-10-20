package motion

// Iterative solver for kinematic moves (itersolve.c).
//
//	Copyright (C) 2018-2020  Kevin O'Connor <kevin@koconnor.net>

import "math"

// The axis flags of a stepper.
const (
	AFX = 1 << 0
	AFY = 1 << 1
	AFZ = 1 << 2
)

// CalcPositionFunc returns the stepper position at a time in a move.
type CalcPositionFunc func(sk *StepperKinematics, m *Move, moveTime float64) float64

// StepQueue is the step compression of a stepper, as the solver uses it.
type StepQueue interface {
	StepDir() int
	Commit()
	Append(sdir int, printTime, stepTime float64) int32
}

// StepperKinematics solves the step times of one stepper (struct stepper_kinematics).
type StepperKinematics struct {
	StepDist, CommandedPos                float64
	SC                                    StepQueue
	LastFlushTime, LastMoveTime           float64
	TQ                                    *TrapQ
	ActiveFlags                           int
	GenStepsPreActive, GenStepsPostActive float64
	CalcPosition                          CalcPositionFunc
	Post                                  func(sk *StepperKinematics)
	// extruder is the pressure advance state of an extruder stepper (struct extruder_stepper).
	extruder *extruderStepper
}

type timepos struct{ time, position float64 }

const seekTimeReset = 0.000100

// genStepsRange generates the step times for a part of a move.
func (sk *StepperKinematics) genStepsRange(sc StepQueue, m *Move, absStart, absEnd float64) int32 {
	calc := sk.CalcPosition
	halfStep := .5 * sk.StepDist
	start, end := absStart-m.PrintTime, absEnd-m.PrintTime
	if start < 0. {
		start = 0.
	}
	if end > m.MoveT {
		end = m.MoveT
	}
	oldGuess := timepos{start, sk.CommandedPos}
	guess := oldGuess
	sdir := sc.StepDir()
	isDirChange, haveBracket, checkOscillate := false, false, false
	target := sk.CommandedPos - halfStep
	if sdir != 0 {
		target = sk.CommandedPos + halfStep
	}
	lastTime, lowTime, highTime := start, start, start+seekTimeReset
	if highTime > end {
		highTime = end
	}
	for {
		// Use the "secant method" to guess a new time from previous guesses
		guessDist := guess.position - target
		ogDist := oldGuess.position - target
		nextTime := (oldGuess.time*guessDist - guess.time*ogDist) / (guessDist - ogDist)
		if !(nextTime > lowTime && nextTime < highTime) { // or NaN
			// Next guess is outside bounds checks - validate it
			if haveBracket {
				// A poor guess - fall back to bisection
				nextTime = (lowTime + highTime) * .5
				checkOscillate = false
			} else if guess.time >= end {
				// No more steps present in requested time range
				break
			} else {
				// Might be a poor guess - limit to exponential search
				nextTime = highTime
				highTime = 2.*highTime - lastTime
				if highTime > end {
					highTime = end
				}
			}
		}
		// Calculate position at next_time guess
		oldGuess = guess
		guess.time = nextTime
		guess.position = calc(sk, m, nextTime)
		guessDist = guess.position - target
		if math.Abs(guessDist) > .000000001 {
			// Guess does not look close enough - update bounds
			relDist := -guessDist
			if sdir != 0 {
				relDist = guessDist
			}
			if relDist > 0. {
				// Found position past target, so step is definitely present
				if haveBracket && oldGuess.time <= lowTime {
					if checkOscillate {
						// Force bisect next to avoid persistent oscillations
						oldGuess = guess
					}
					checkOscillate = true
				}
				highTime = guess.time
				haveBracket = true
			} else if relDist < -(halfStep + halfStep + .000000010) {
				// Found direction change
				sdir = 1 - sdir
				if sdir != 0 {
					target = target + halfStep + halfStep
				} else {
					target = target - halfStep - halfStep
				}
				lowTime = lastTime
				highTime = guess.time
				isDirChange, haveBracket = true, true
				checkOscillate = false
			} else {
				lowTime = guess.time
			}
			if !haveBracket || highTime-lowTime > .000000001 {
				if !isDirChange && relDist >= -halfStep {
					// Avoid rollback if stepper fully reaches step position
					sc.Commit()
				}
				// Guess is not close enough - guess again with new time
				continue
			}
		}
		// Found next step - submit it
		if ret := sc.Append(sdir, m.PrintTime, guess.time); ret != 0 {
			return ret
		}
		if sdir != 0 {
			target = target + halfStep + halfStep
		} else {
			target = target - halfStep - halfStep
		}
		// Reset bounds checking
		seekTimeDelta := 1.5 * (guess.time - lastTime)
		if seekTimeDelta < .000000001 {
			seekTimeDelta = .000000001
		}
		if isDirChange && seekTimeDelta > seekTimeReset {
			seekTimeDelta = seekTimeReset
		}
		lastTime, lowTime = guess.time, guess.time
		highTime = guess.time + seekTimeDelta
		if highTime > end {
			highTime = end
		}
		isDirChange, haveBracket, checkOscillate = false, false, false
	}
	if sdir != 0 {
		sk.CommandedPos = target - halfStep
	} else {
		sk.CommandedPos = target + halfStep
	}
	if sk.Post != nil {
		sk.Post(sk)
	}
	return 0
}

// checkActive reports that a move is likely to move the stepper.
func (sk *StepperKinematics) checkActive(m *Move) bool {
	af := sk.ActiveFlags
	return (af&AFX != 0 && m.AxesR.X != 0.) || (af&AFY != 0 && m.AxesR.Y != 0.) ||
		(af&AFZ != 0 && m.AxesR.Z != 0.)
}

// GenerateSteps generates the step times of the moves of the queue to flushTime.
func (sk *StepperKinematics) GenerateSteps(sc StepQueue, flushTime float64) int32 {
	lastFlushTime := sk.LastFlushTime
	sk.LastFlushTime = flushTime
	if sk.TQ == nil {
		return 0
	}
	m := sk.TQ.HeadSentinel()
	for lastFlushTime >= m.PrintTime+m.MoveT {
		m = m.next
	}
	forceStepsTime := sk.LastMoveTime + sk.GenStepsPostActive
	skipCount := 0
	for {
		moveStart := m.PrintTime
		moveEnd := moveStart + m.MoveT
		if sk.checkActive(m) {
			if skipCount != 0 && sk.GenStepsPreActive != 0 {
				// Must generate steps leading up to stepper activity
				absStart := moveStart - sk.GenStepsPreActive
				if absStart < lastFlushTime {
					absStart = lastFlushTime
				}
				if absStart < forceStepsTime {
					absStart = forceStepsTime
				}
				pm := m.prev
				for {
					skipCount--
					if skipCount == 0 || !(pm.PrintTime > absStart) {
						break
					}
					pm = pm.prev
				}
				for {
					if ret := sk.genStepsRange(sc, pm, absStart, flushTime); ret != 0 {
						return ret
					}
					pm = pm.next
					if pm == m {
						break
					}
				}
			}
			// Generate steps for this move
			if ret := sk.genStepsRange(sc, m, lastFlushTime, flushTime); ret != 0 {
				return ret
			}
			if moveEnd >= flushTime {
				sk.LastMoveTime = flushTime
				return 0
			}
			skipCount = 0
			sk.LastMoveTime = moveEnd
			forceStepsTime = sk.LastMoveTime + sk.GenStepsPostActive
		} else {
			if moveStart < forceStepsTime {
				// Must generates steps just past stepper activity
				absEnd := forceStepsTime
				if absEnd > flushTime {
					absEnd = flushTime
				}
				if ret := sk.genStepsRange(sc, m, lastFlushTime, absEnd); ret != 0 {
					return ret
				}
				skipCount = 1
			} else {
				// This move doesn't impact this stepper - skip it
				skipCount++
			}
			if flushTime+sk.GenStepsPreActive <= moveEnd {
				return 0
			}
		}
		m = m.next
	}
}

// CheckActive returns the print time of the first move to flushTime that moves the stepper, or 0.
func (sk *StepperKinematics) CheckActive(flushTime float64) float64 {
	if sk.TQ == nil {
		return 0.
	}
	sk.TQ.CheckSentinels()
	m := sk.TQ.HeadSentinel()
	for sk.LastFlushTime >= m.PrintTime+m.MoveT {
		m = m.next
	}
	for {
		if sk.checkActive(m) {
			return m.PrintTime
		}
		if flushTime <= m.PrintTime+m.MoveT {
			return 0.
		}
		m = m.next
	}
}

// IsActiveAxis reports that the stepper moves on an axis ('x', 'y' or 'z').
func (sk *StepperKinematics) IsActiveAxis(axis byte) bool {
	if axis < 'x' || axis > 'z' {
		return false
	}
	return sk.ActiveFlags&(AFX<<(axis-'x')) != 0
}

// SetTrapQ sets the move queue and the step distance.
func (sk *StepperKinematics) SetTrapQ(tq *TrapQ, stepDist float64) {
	sk.TQ = tq
	sk.StepDist = stepDist
}

// CalcPositionFromCoord returns the stepper position at an XYZ position.
func (sk *StepperKinematics) CalcPositionFromCoord(x, y, z float64) float64 {
	m := &Move{StartPos: Coord{x, y, z}, MoveT: 1000.}
	return sk.CalcPosition(sk, m, 500.)
}

// SetPosition sets the stepper position from an XYZ position.
func (sk *StepperKinematics) SetPosition(x, y, z float64) {
	sk.CommandedPos = sk.CalcPositionFromCoord(x, y, z)
}

// Cartesian kinematics stepper pulse time generation (kin_cartesian.c).
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>

// NewCartesianStepper makes the solver of a stepper on axis 'x', 'y' or 'z'.
func NewCartesianStepper(axis byte) *StepperKinematics {
	sk := &StepperKinematics{}
	switch axis {
	case 'x':
		sk.CalcPosition = func(_ *StepperKinematics, m *Move, t float64) float64 { return m.GetCoord(t).X }
		sk.ActiveFlags = AFX
	case 'y':
		sk.CalcPosition = func(_ *StepperKinematics, m *Move, t float64) float64 { return m.GetCoord(t).Y }
		sk.ActiveFlags = AFY
	case 'z':
		sk.CalcPosition = func(_ *StepperKinematics, m *Move, t float64) float64 { return m.GetCoord(t).Z }
		sk.ActiveFlags = AFZ
	}
	return sk
}
