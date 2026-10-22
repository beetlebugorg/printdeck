package simmcu

import "sort"

// segment is one queue_step: count steps, the first at first, each next one add ticks later than
// the step before it.
type segment struct {
	first         uint64
	interval, add int64
	count         int64
	dir           bool
}

// time is the clock of step k (1 to count).
func (g segment) time(k int64) uint64 {
	return g.first + uint64((k-1)*g.interval+g.add*k*(k-1)/2)
}

// done is the number of steps of the segment at the clock.
func (g segment) done(clock uint64) int64 {
	if clock < g.time(1) {
		return 0
	}
	return int64(sort.Search(int(g.count), func(i int) bool { return g.time(int64(i)+1) > clock }))
}

func (g segment) sign() int64 {
	if g.dir {
		return 1
	}
	return -1
}

// stepper is a stepper motor: its queued steps and its position.
type stepper struct {
	pin       string
	base      float64 // the steps of the axis before this session of the board
	pos       int64   // the position of the steps that are done (stepper_get_position)
	last      uint64  // the clock of the last queued step
	nextDir   bool
	segs      []segment
	trsync    int64 // the trsync that stops it, or -1
	needReset bool  // stopped by a trigger: steps wait for reset_step_clock
}

func newStepper(pin string) *stepper {
	return &stepper{pin: pin, base: world.physicalSteps(pin), trsync: -1}
}

// queue adds a queue_step. The firmware adds the interval to the time of the last step in 32 bits,
// so an interval after "reset_step_clock clock=0" wraps to the right time: the first step is that
// sum, on the 64-bit clock near now (clock64).
func (s *stepper) queue(interval, count, add int64, clock64 func(int64) uint64) {
	if s.needReset || count <= 0 {
		return
	}
	first := clock64(int64(uint32(s.last) + uint32(interval)))
	g := segment{first: first, interval: interval, count: count, add: add, dir: s.nextDir}
	s.segs = append(s.segs, g)
	s.last = g.time(count)
}

// advance counts the segments that are done at the clock into the position.
func (s *stepper) advance(clock uint64) {
	for len(s.segs) > 0 && s.segs[0].time(s.segs[0].count) <= clock {
		g := s.segs[0]
		s.pos += g.sign() * g.count
		s.segs = s.segs[1:]
	}
}

// at is the position at the clock, with the steps of a segment that runs.
func (s *stepper) at(clock uint64) int64 {
	s.advance(clock)
	pos := s.pos
	if len(s.segs) > 0 {
		pos += s.segs[0].sign() * s.segs[0].done(clock)
	}
	return pos
}

// physical is the place of the axis at the clock, in steps since the start of the World.
func (s *stepper) physical(clock uint64) float64 { return s.base + float64(s.at(clock)) }

// stop stops the stepper at the clock: the steps after it do not run.
func (s *stepper) stop(clock uint64) {
	s.pos = s.at(clock)
	s.segs, s.needReset, s.last = nil, true, clock
}

// endstop is a switch: an endstop or a probe.
type endstop struct {
	pin        string
	homing     bool
	pinValue   int64 // the value of the pin when the switch triggers
	trsync     int64
	reason     int64
	start      uint64
	checked    uint64  // the clock of the last check
	startPlace float64 // the place of the stepper at the start, once known
	started    bool
}

func (e *endstop) home(b *Board, on bool, pinValue, trsyncOID, reason int64, clock uint64) {
	e.homing = on
	if !on {
		return
	}
	e.pinValue, e.trsync, e.reason, e.start, e.checked, e.started = pinValue, trsyncOID, reason, clock, clock, false
}

// check triggers the switch when its stepper reaches the place of the switch. The first homing of a
// switch has no place yet: it triggers in its move, and that place is the
// place of the switch from then on. b.mu is locked.
func (e *endstop) check(b *Board, now uint64) {
	if !e.homing || now < e.start {
		return
	}
	var s *stepper
	var best int64 = -1
	for oid, st := range b.steppers {
		if st.trsync == e.trsync && (best < 0 || oid < best) {
			s, best = st, oid
		}
	}
	if s == nil {
		return
	}
	if !e.started {
		e.startPlace, e.started = s.physical(e.start), true
		debugf("home %s: start at %.0f, %d segments, last step %d, now %d", e.pin, e.startPlace, len(s.segs), s.last, now)
	}
	world.mu.Lock()
	place, known := world.switches[e.pin]
	world.mu.Unlock()
	if !known {
		// 3/4 of the time to the last queued step: the host queues the steps of a move up to
		// about 2 s ahead, so this is late in a short move, and in the middle of a long one.
		if s.last <= e.start {
			return
		}
		at := e.start + (s.last-e.start)*3/4
		if now < at {
			return
		}
		world.mu.Lock()
		world.switches[e.pin] = s.physical(at)
		world.mu.Unlock()
		b.trigger(e.trsync, e.reason, at, true)
		return
	}
	if now-e.checked > uint64(b.freq) { // once a second
		debugf("home %s: at %.0f, switch at %.0f, %d segments", e.pin, s.physical(now), place, len(s.segs))
	}
	crossed := func(clock uint64) bool {
		p := s.physical(clock)
		return p != e.startPlace && (e.startPlace-place)*(p-place) <= 0
	}
	if !crossed(now) {
		e.checked = now
		return
	}
	// The clock of the crossing, between the last check and now.
	lo, hi := e.checked, now
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if crossed(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	b.trigger(e.trsync, e.reason, hi, true)
}

// trsync is a trigger synchronization: the switches and the steppers that stop together.
type trsync struct {
	active       bool
	reportAt     uint64
	reportTicks  uint64
	expireAt     uint64
	expireReason int64
	reason       int64 // the reason of the trigger
}

// adc is an analog input: a thermistor.
type adc struct {
	pin         string
	heater      int // the heater that heats it (config order)
	oid         int64
	next        uint64
	count       int64
	rest        uint64
	minV, maxV  int64
	checks, bad int64
	bytes       int64
}

// change is a scheduled value of an output.
type change struct {
	clock uint64
	value float64
}

// output is a digital or PWM output: a heater, a fan, a LED.
type output struct {
	pin        string
	value, max float64
	pending    []change
}

func (o *output) apply(now uint64) {
	n := 0
	for n < len(o.pending) && o.pending[n].clock <= now {
		o.value = o.pending[n].value
		n++
	}
	o.pending = o.pending[n:]
}

// duty is the part of the time that the output is on, from 0 to 1.
func (o *output) duty() float64 { return max(0, min(1, o.value/max(1, o.max))) }
