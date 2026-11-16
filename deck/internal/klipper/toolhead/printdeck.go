// The hold move of Printdeck: Klipper has no such move.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.

package toolhead

import (
	"math"

	"printdeck/internal/klipper/extras/motion_queuing"
)

// holdPause is the drip completion of a hold move. It never completes: the move ends when the
// trapq ends.
type holdPause struct{ th *ToolHead }

func (h holdPause) Test() bool { return false }

func (h holdPause) Wait(waketime float64, timeoutResult any) any {
	h.th.reactor.Pause(waketime)
	return timeoutResult
}

// HoldMove moves the toolhead in the direction dir (X, Y and Z, length 1) while held returns true,
// and returns the distance. It accelerates to speed, keeps the speed, and decelerates when held
// returns false. It stops at maxDist at the latest.
//
// A queued G-code move cannot stop early, and the toolhead keeps about 1 s of moves in the queue.
// HoldMove sends the steps in small parts, as a homing move does. The trapq gets the move in parts
// of motion_queuing.DripSegment, a short time before the steps. Thus the toolhead starts to
// decelerate about 0.2 s after held changes.
//
// The kinematics check the move to the point at maxDist: the axes must be homed, and the point
// must be in the limits. The speed and the acceleration are those of a G-code move to that point.
func (th *ToolHead) HoldMove(dir [3]float64, speed, maxDist float64, held func() bool) float64 {
	if maxDist < 0.001 {
		return 0
	}
	pos0 := append([]float64(nil), th.commandedPos...)
	target := append([]float64(nil), pos0...)
	for i := range 3 {
		target[i] += dir[i] * maxDist
	}
	move := NewMove(th, pos0, target, speed)
	th.kin.CheckMove(move)
	v, a := math.Min(speed, math.Sqrt(move.MaxCruiseV2)), move.Accel
	if v*v/a > maxDist {
		v = math.Sqrt(a * maxDist) // accelerate to the middle, then decelerate
	}
	decelT := v / a
	decelD := v * decelT / 2

	kinFlushDelay := th.motionQueuing.KinFlushDelay()
	th.Dwell(kinFlushDelay)
	th.processLookahead(false)
	th.calcPrintTime()
	start := th.printTime
	end, dist := start, 0.
	add := func(accelT, cruiseT, decelT, startV float64) {
		th.trapq.Append(end, accelT, cruiseT, decelT, pos0[0]+dir[0]*dist, pos0[1]+dir[1]*dist,
			pos0[2]+dir[2]*dist, dir[0], dir[1], dir[2], startV, v, a)
		end += accelT + cruiseT + decelT
		dist += startV*accelT + a*accelT*accelT/2 + v*cruiseT + v*decelT - a*decelT*decelT/2
	}
	add(v/a, 0, 0, 0)
	stopped := false
	// The trapq stays one part ahead of the steps, and the kinematics see kinFlushDelay more.
	ahead := 2*motion_queuing.DripSegment + kinFlushDelay
	grow := func(flushTime float64) float64 {
		for !stopped && end < flushTime+ahead {
			room := maxDist - dist - decelD
			if room < 0.0001 || !held() {
				add(0, 0, decelT, v)
				stopped = true
				break
			}
			add(0, math.Min(motion_queuing.DripSegment, room/v), 0, v)
		}
		return end
	}
	th.motionQueuing.DripStream(start, grow, holdPause{th})
	th.motionQueuing.WipeTrapQ(th.trapq)
	th.advanceMoveTime(end)
	newpos := append([]float64(nil), pos0...)
	for i := range 3 {
		newpos[i] += dir[i] * dist
	}
	th.SetPosition(newpos, "")
	return dist
}
