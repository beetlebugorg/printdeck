package motion

// Extruder stepper pulse time generation (kin_extruder.c).
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//
// Without pressure advance, the extruder stepper position is:
//
//	extruder_position(t) = nominal_position(t)
//
// When pressure advance is enabled, additional filament is pushed into the extruder during
// acceleration (and retracted during deceleration). The formula is:
//
//	pa_position(t) = (nominal_position(t) + pressure_advance * nominal_velocity(t))
//
// Which is then "smoothed" using a weighted average:
//
//	smooth_position(t) = (
//	    definitive_integral(pa_position(x) * (smooth_time/2 - abs(t-x)) * dx,
//	                        from=t-smooth_time/2, to=t+smooth_time/2)
//	    / ((smooth_time/2)**2))

type paParams struct {
	pressureAdvance, activePrintTime float64
}

type extruderStepper struct {
	paList                             []paParams
	halfSmoothTime, invHalfSmoothTime2 float64
}

// extruderIntegrate is the definitive integral of position(t) = base + t * (start_v + t * half_accel).
func extruderIntegrate(base, startV, halfAccel, start, end float64) float64 {
	halfV, sixthA := .5*startV, (1./3.)*halfAccel
	si := start * (base + start*(halfV+start*sixthA))
	ei := end * (base + end*(halfV+end*sixthA))
	return ei - si
}

// extruderIntegrateTime is the definitive integral of the time weighted position
// t * (base + t * (start_v + t * half_accel)).
func extruderIntegrateTime(base, startV, halfAccel, start, end float64) float64 {
	halfB, thirdV := .5*base, (1./3.)*startV
	eighthA := .25 * halfAccel
	si := start * start * (halfB + start*(thirdV+start*eighthA))
	ei := end * end * (halfB + end*(thirdV+end*eighthA))
	return ei - si
}

// paMoveIntegrate is the definitive integral of the extruder for a move.
func paMoveIntegrate(m *Move, paList []paParams, base, start, end, timeOffset float64) float64 {
	if start < 0. {
		start = 0.
	}
	if end > m.MoveT {
		end = m.MoveT
	}
	// Determine pressure_advance value
	pressureAdvance := 0.
	if m.AxesR.Y != 0. {
		i := len(paList) - 1
		for paList[i].activePrintTime > m.PrintTime && i > 0 {
			i--
		}
		pressureAdvance = paList[i].pressureAdvance
	}
	// Calculate base position and velocity with pressure advance
	base += pressureAdvance * m.StartV
	startV := m.StartV + pressureAdvance*2.*m.HalfAccel
	// Calculate definitive integral
	ha := m.HalfAccel
	iext := extruderIntegrate(base, startV, ha, start, end)
	wgtExt := extruderIntegrateTime(base, startV, ha, start, end)
	return wgtExt - timeOffset*iext
}

// paRangeIntegrate is the definitive integral of the extruder over a range of moves.
func paRangeIntegrate(m *Move, moveTime float64, paList []paParams, hst float64) float64 {
	// Calculate integral for the current move
	res, start, end := 0., moveTime-hst, moveTime+hst
	startBase := m.StartPos.X
	res += paMoveIntegrate(m, paList, 0., start, moveTime, start)
	res -= paMoveIntegrate(m, paList, 0., moveTime, end, end)
	// Integrate over previous moves
	prev := m
	for start < 0. {
		prev = prev.prev
		start += prev.MoveT
		base := prev.StartPos.X - startBase
		res += paMoveIntegrate(prev, paList, base, start, prev.MoveT, start)
	}
	// Integrate over future moves
	for end > m.MoveT {
		end -= m.MoveT
		m = m.next
		base := m.StartPos.X - startBase
		res -= paMoveIntegrate(m, paList, base, 0., end, end)
	}
	return res
}

func extruderCalcPosition(sk *StepperKinematics, m *Move, moveTime float64) float64 {
	es := sk.extruder
	hst := es.halfSmoothTime
	if hst == 0 {
		// Pressure advance not enabled
		return m.StartPos.X + m.GetDistance(moveTime)
	}
	// Apply pressure advance and average over smooth_time
	area := paRangeIntegrate(m, moveTime, es.paList, hst)
	return m.StartPos.X + area*es.invHalfSmoothTime2
}

// ExtruderSetPressureAdvance sets the pressure advance and its smooth time from a print time.
func ExtruderSetPressureAdvance(sk *StepperKinematics, printTime, pressureAdvance, smoothTime float64) {
	es := sk.extruder
	hst, oldHst := smoothTime*.5, es.halfSmoothTime
	es.halfSmoothTime = hst
	sk.GenStepsPreActive, sk.GenStepsPostActive = hst, hst
	// Cleanup old pressure advance parameters
	cleanupTime := sk.LastFlushTime - max(oldHst, hst)
	for len(es.paList) > 1 && es.paList[1].activePrintTime < cleanupTime {
		es.paList = es.paList[1:]
	}
	if hst == 0 {
		return
	}
	es.invHalfSmoothTime2 = 1. / (hst * hst)
	if es.paList[len(es.paList)-1].pressureAdvance == pressureAdvance {
		// Retain old pa_params
		return
	}
	// Add new pressure advance parameters
	es.paList = append(es.paList, paParams{pressureAdvance, printTime})
}

// NewExtruderStepper makes the solver of an extruder stepper.
func NewExtruderStepper() *StepperKinematics {
	return &StepperKinematics{CalcPosition: extruderCalcPosition, ActiveFlags: AFX,
		extruder: &extruderStepper{paList: []paParams{{}}}}
}
