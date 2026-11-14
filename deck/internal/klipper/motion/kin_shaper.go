package motion

// Kinematic input shapers to minimize motion vibrations in XY plane (kin_shaper.c).
//
//	Copyright (C) 2019-2020  Kevin O'Connor <kevin@koconnor.net>
//	Copyright (C) 2020-2025  Dmitry Butyugin <dmbutyugin@google.com>

/****************************************************************
 * Shaper initialization
 ****************************************************************/

var kinFlags = [3]int{AFX, AFY, AFZ}

type shaperPulse struct{ t, a float64 }

type shaperPulses struct {
	numPulses, pInd int
	pulses          [10]shaperPulse
}

// Shift pulses around 'mid-point' t=0 so that the input shaper is an identity
// transformation for constant-speed motion (i.e. input_shaper(v * T) = v * T)
func shiftPulses(sp *shaperPulses) {
	ts := 0.
	for i := 0; i < sp.numPulses; i++ {
		ts += sp.pulses[i].a * sp.pulses[i].t
	}
	for i := 0; i < sp.numPulses; i++ {
		sp.pulses[i].t -= ts
	}
	// Find the first pulse with positive time
	i := 0
	for ; i < sp.numPulses; i++ {
		if sp.pulses[i].t > 0 {
			break
		}
	}
	sp.pInd = i
}

func initShaper(n int, a, t []float64, sp *shaperPulses) int {
	if n < 0 || n > len(sp.pulses) {
		sp.numPulses = 0
		return -1
	}
	// Check that pulses go in pulse time increasing order
	for i := 1; i < n; i++ {
		if t[i-1] >= t[i] {
			return -1
		}
	}
	sumA := 0.
	for i := 0; i < n; i++ {
		sumA += a[i]
	}
	invA := 1. / sumA
	// Reverse pulses vs their traditional definition
	for i := 0; i < n; i++ {
		sp.pulses[n-i-1].a = a[i] * invA
		sp.pulses[n-i-1].t = -t[i]
	}
	sp.numPulses = n
	shiftPulses(sp)
	return 0
}

/****************************************************************
 * Generic position calculation via shaper convolution
 ****************************************************************/

func getAxisPosition(m *Move, axis int, moveTime float64) float64 {
	axisR := m.AxesR.Axis(axis)
	startPos := m.StartPos.Axis(axis)
	moveDist := m.GetDistance(moveTime)
	return startPos + axisR*moveDist
}

// Calculate the position from the convolution of the shaper with input signal
func calcShaperPosition(m *Move, axis int, moveTime float64, sp *shaperPulses) float64 {
	res := 0.
	numPulses := sp.numPulses
	prev := m
	t := moveTime
	for i := sp.pInd - 1; i >= 0; i-- {
		t += sp.pulses[i].t
		for t < 0. {
			prev = prev.prev
			t += prev.MoveT
		}
		res += sp.pulses[i].a * getAxisPosition(prev, axis, t)
		t -= sp.pulses[i].t
	}
	t = moveTime
	for i := sp.pInd; i < numPulses; i++ {
		t += sp.pulses[i].t
		for t > m.MoveT {
			t -= m.MoveT
			m = m.next
		}
		res += sp.pulses[i].a * getAxisPosition(m, axis, t)
		t -= sp.pulses[i].t
	}
	return res
}

/****************************************************************
 * Kinematics-related shaper code
 ****************************************************************/

const dummyT = 500.0

// inputShaper is the shaper state of a stepper (struct input_shaper). Its stepper kinematics are
// sk; origSK are the kinematics of the stepper without the shaper.
type inputShaper struct {
	sk     *StepperKinematics
	origSK *StepperKinematics
	m      Move
	sp     [3]shaperPulses
}

// Optimized calc_position when only x axis is needed
func shaperXCalcPosition(sk *StepperKinematics, m *Move, moveTime float64) float64 {
	is := sk.shaper
	sx := &is.sp[0]
	if sx.numPulses == 0 {
		return is.origSK.CalcPosition(is.origSK, m, moveTime)
	}
	is.m.StartPos.X = calcShaperPosition(m, 0, moveTime, sx)
	return is.origSK.CalcPosition(is.origSK, &is.m, dummyT)
}

// Optimized calc_position when only y axis is needed
func shaperYCalcPosition(sk *StepperKinematics, m *Move, moveTime float64) float64 {
	is := sk.shaper
	sy := &is.sp[1]
	if sy.numPulses == 0 {
		return is.origSK.CalcPosition(is.origSK, m, moveTime)
	}
	is.m.StartPos.Y = calcShaperPosition(m, 1, moveTime, sy)
	return is.origSK.CalcPosition(is.origSK, &is.m, dummyT)
}

// Optimized calc_position when only z axis is needed
func shaperZCalcPosition(sk *StepperKinematics, m *Move, moveTime float64) float64 {
	is := sk.shaper
	sz := &is.sp[2]
	if sz.numPulses == 0 {
		return is.origSK.CalcPosition(is.origSK, m, moveTime)
	}
	is.m.StartPos.Z = calcShaperPosition(m, 2, moveTime, sz)
	return is.origSK.CalcPosition(is.origSK, &is.m, dummyT)
}

// General calc_position for all x, y, and z axes
func shaperXYZCalcPosition(sk *StepperKinematics, m *Move, moveTime float64) float64 {
	is := sk.shaper
	if is.sp[0].numPulses == 0 && is.sp[1].numPulses == 0 && is.sp[2].numPulses == 0 {
		return is.origSK.CalcPosition(is.origSK, m, moveTime)
	}
	is.m.StartPos = m.GetCoord(moveTime)
	if is.sp[0].numPulses != 0 {
		is.m.StartPos.X = calcShaperPosition(m, 0, moveTime, &is.sp[0])
	}
	if is.sp[1].numPulses != 0 {
		is.m.StartPos.Y = calcShaperPosition(m, 1, moveTime, &is.sp[1])
	}
	if is.sp[2].numPulses != 0 {
		is.m.StartPos.Z = calcShaperPosition(m, 2, moveTime, &is.sp[2])
	}
	return is.origSK.CalcPosition(is.origSK, &is.m, dummyT)
}

// A callback that forwards post_cb call to the original kinematics
func shaperCommandedPosPostFixup(sk *StepperKinematics) {
	is := sk.shaper
	is.origSK.CommandedPos = sk.CommandedPos
	is.origSK.Post(is.origSK)
	sk.CommandedPos = is.origSK.CommandedPos
}

func shaperNoteGenerationTime(is *inputShaper) {
	preActive, postActive := 0., 0.
	sx := &is.sp[0]
	if is.sk.ActiveFlags&AFX != 0 && sx.numPulses != 0 {
		preActive = sx.pulses[sx.numPulses-1].t
		postActive = -sx.pulses[0].t
	}
	sy := &is.sp[1]
	if is.sk.ActiveFlags&AFY != 0 && sy.numPulses != 0 {
		if sy.pulses[sy.numPulses-1].t > preActive {
			preActive = sy.pulses[sy.numPulses-1].t
		}
		if -sy.pulses[0].t > postActive {
			postActive = -sy.pulses[0].t
		}
	}
	sz := &is.sp[2]
	if is.sk.ActiveFlags&AFZ != 0 && sz.numPulses != 0 {
		if sz.pulses[sz.numPulses-1].t > preActive {
			preActive = sz.pulses[sz.numPulses-1].t
		}
		if -sz.pulses[0].t > postActive {
			postActive = -sz.pulses[0].t
		}
	}
	is.sk.GenStepsPreActive = preActive
	is.sk.GenStepsPostActive = postActive
}

// InputShaperUpdateSK updates the shaper after a change of the active axes of the original
// kinematics (input_shaper_update_sk).
func InputShaperUpdateSK(sk *StepperKinematics) {
	is := sk.shaper
	kinFlags := is.origSK.ActiveFlags & (AFX | AFY | AFZ)
	switch kinFlags {
	case AFX:
		is.sk.CalcPosition = shaperXCalcPosition
	case AFY:
		is.sk.CalcPosition = shaperYCalcPosition
	case AFZ:
		is.sk.CalcPosition = shaperZCalcPosition
	default:
		is.sk.CalcPosition = shaperXYZCalcPosition
	}
	is.sk.ActiveFlags = is.origSK.ActiveFlags
	shaperNoteGenerationTime(is)
}

// InputShaperSetSK sets the original kinematics of the shaper (input_shaper_set_sk). It returns
// -1 when the kinematics have no X, Y or Z axis.
func InputShaperSetSK(sk, origSK *StepperKinematics) int {
	is := sk.shaper
	switch {
	case origSK.ActiveFlags == AFX:
		is.sk.CalcPosition = shaperXCalcPosition
	case origSK.ActiveFlags == AFY:
		is.sk.CalcPosition = shaperYCalcPosition
	case origSK.ActiveFlags == AFZ:
		is.sk.CalcPosition = shaperZCalcPosition
	case origSK.ActiveFlags&(AFX|AFY|AFZ) != 0:
		is.sk.CalcPosition = shaperXYZCalcPosition
	default:
		return -1
	}
	is.sk.ActiveFlags = origSK.ActiveFlags
	is.origSK = origSK
	is.sk.CommandedPos = origSK.CommandedPos
	is.sk.LastFlushTime = origSK.LastFlushTime
	is.sk.LastMoveTime = origSK.LastMoveTime
	if origSK.Post != nil {
		is.sk.Post = shaperCommandedPosPostFixup
	}
	return 0
}

// InputShaperSetShaperParams sets the pulses of the shaper of an axis ('x', 'y' or 'z')
// (input_shaper_set_shaper_params). It returns -1 for bad pulses.
func InputShaperSetShaperParams(sk *StepperKinematics, axis byte, n int, a, t []float64) int {
	axisInd := int(axis) - 'x'
	if axisInd < 0 || axisInd >= len(kinFlags) {
		return -1
	}
	is := sk.shaper
	sp := &is.sp[axisInd]
	status := 0
	// Ignore input shaper update if the axis is not active
	if is.origSK.ActiveFlags&kinFlags[axisInd] != 0 {
		status = initShaper(n, a, t, sp)
		shaperNoteGenerationTime(is)
	}
	return status
}

// InputShaperAlloc returns new stepper kinematics with an input shaper (input_shaper_alloc).
func InputShaperAlloc() *StepperKinematics {
	is := &inputShaper{}
	is.sk = &StepperKinematics{shaper: is}
	is.m.MoveT = 2. * dummyT
	return is.sk
}
