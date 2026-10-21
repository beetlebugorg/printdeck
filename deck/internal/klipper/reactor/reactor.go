// Package reactor is the event loop of the Klipper host: timers, callbacks, completions and mutexes,
// with cooperative pauses. One piece of code runs at a time. Code that waits (Pause, a completion,
// a mutex) lets the loop run the other timers, and goes on later from the same place.
//
// Ported to Go from klippy/reactor.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The greenlets of reactor.py are goroutines here. Only
// one of them runs at a time: a switch sends a value to the next goroutine and waits for a value
// back, as greenlet.switch does. The file descriptors of reactor.py are callbacks that other
// goroutines queue with RegisterAsyncCallback (the serial reader, the API socket).
//
//	Copyright (C) 2016-2026  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package reactor

import (
	"errors"
	"math"
	"os"
	monotonic_ "printdeck/internal/klipper/monotonic"
	"sync"
	"time"
)

const (
	// Now is a wake time that is due at once.
	Now = 0.
	// Never is a wake time that never comes.
	Never = 9999999999999999.
)

// ErrPauseDisabled is the panic value of a pause inside AssertNoPause.
var ErrPauseDisabled = errors.New("Internal error - reactor pause disabled")

// Timer is a timer of the reactor. Its callback returns the next wake time.
type Timer struct {
	callback func(eventtime float64) float64
	waketime float64
	running  bool
}

// greenlet is a goroutine that runs reactor code. Only one runs at a time.
type greenlet struct {
	wake  chan any
	timer *Timer
}

// Reactor is the event loop.
type Reactor struct {
	// Monotonic returns the host time in seconds. Tests replace it.
	Monotonic func() float64

	process   bool
	timers    []*Timer // replaced, never changed in place, so a loop over it is safe
	nextTimer float64

	startBusyTime float64
	idleCallback  func(eventtime, startBusyTime float64) bool

	asyncMu sync.Mutex
	async   []func()
	asyncCh chan struct{}

	root     *greenlet // the goroutine of Run
	cur      *greenlet // the goroutine that runs now
	dispatch *greenlet // the goroutine of the dispatch loop
	cached   []*greenlet

	preventPause int

	latencyWarning  float64
	latencyCallback func(eventtime, prevEventtime float64)
	recentEventtime float64

	fds []*FileHandler // replaced, never changed in place, so a loop over it is safe
}

// monotonic is get_monotonic of chelper: CLOCK_MONOTONIC_RAW.
func monotonic() float64 { return monotonic_.Now() }

func New() *Reactor {
	return &Reactor{Monotonic: monotonic, nextTimer: Never, asyncCh: make(chan struct{}, 1),
		idleCallback:    func(float64, float64) bool { return false },
		latencyWarning:  Never,
		latencyCallback: func(float64, float64) {}}
}

// switchTo runs g with the value v, and waits until some goroutine switches back to this one.
// It returns the value of that switch (greenlet.switch).
func (r *Reactor) switchTo(g *greenlet, v any) any {
	self := r.cur
	r.cur = g
	g.wake <- v
	return <-self.wake
}

// newGreenlet makes a goroutine that waits for its first switch, then runs run. When run returns,
// the goroutine switches to the goroutine of Run, as a greenlet goes back to its parent.
func (r *Reactor) newGreenlet(run func()) *greenlet {
	g := &greenlet{wake: make(chan any)}
	go func() {
		<-g.wake
		run()
		r.cur = r.root
		r.root.wake <- nil
	}()
	return g
}

// Timers

// UpdateTimer changes the wake time of a timer. It does nothing while the timer runs: the return
// value of its callback sets the next wake time.
func (r *Reactor) UpdateTimer(t *Timer, waketime float64) {
	if t.running {
		return
	}
	t.waketime = waketime
	r.nextTimer = math.Min(r.nextTimer, waketime)
}

// RegisterTimer adds a timer.
func (r *Reactor) RegisterTimer(callback func(eventtime float64) float64, waketime float64) *Timer {
	t := &Timer{callback: callback, waketime: waketime}
	timers := make([]*Timer, len(r.timers), len(r.timers)+1)
	copy(timers, r.timers)
	r.timers = append(timers, t)
	r.nextTimer = math.Min(r.nextTimer, waketime)
	return t
}

// UnregisterTimer removes a timer.
func (r *Reactor) UnregisterTimer(t *Timer) {
	t.waketime = Never
	timers := make([]*Timer, 0, len(r.timers))
	for _, o := range r.timers {
		if o != t {
			timers = append(timers, o)
		}
	}
	r.timers = timers
}

func (r *Reactor) checkTimers(eventtime float64) {
	r.nextTimer = Never
	gDispatch := r.dispatch
	for _, t := range r.timers {
		waketime := t.waketime
		if eventtime >= waketime {
			t.waketime = Never
			t.running = true
			waketime = t.callback(eventtime)
			t.waketime = waketime
			t.running = false
			if gDispatch != r.dispatch {
				r.nextTimer = math.Min(r.nextTimer, waketime)
				r.endGreenlet(gDispatch)
				return
			}
		}
		r.nextTimer = math.Min(r.nextTimer, waketime)
	}
}

// Idle notifiers

// SetIdleNotifier sets a callback that runs before the loop sleeps. When it returns true, the loop
// does not sleep.
func (r *Reactor) SetIdleNotifier(callback func(eventtime, startBusyTime float64) bool) {
	r.idleCallback = callback
}

func (r *Reactor) calcSleepTime(eventtime float64) float64 {
	r.preventPause++
	busy := r.idleCallback(eventtime, r.startBusyTime)
	r.preventPause--
	if busy {
		return 0
	}
	return math.Min(1., math.Max(.001, r.nextTimer-eventtime))
}

// Completions and callbacks

// Completion is a result that other code waits for.
type Completion struct {
	r       *Reactor
	done    bool
	result  any
	waiting []*greenlet
}

// NewCompletion makes a completion.
func (r *Reactor) NewCompletion() *Completion { return &Completion{r: r} }

// Test reports that the completion has a result.
func (c *Completion) Test() bool { return c.done }

// Complete sets the result and wakes the waiting code.
func (c *Completion) Complete(result any) {
	c.done, c.result = true, result
	for _, g := range c.waiting {
		c.r.UpdateTimer(g.timer, Now)
	}
}

// Wait waits for the result until waketime. It returns timeoutResult when the time comes first.
func (c *Completion) Wait(waketime float64, timeoutResult any) any {
	if !c.done {
		g := c.r.cur
		c.waiting = append(c.waiting, g)
		c.r.Pause(waketime)
		for i, w := range c.waiting {
			if w == g {
				c.waiting = append(c.waiting[:i], c.waiting[i+1:]...)
				break
			}
		}
		if !c.done {
			return timeoutResult
		}
	}
	return c.result
}

// RegisterCallback runs callback once at waketime, and returns the completion of its result.
func (r *Reactor) RegisterCallback(callback func(eventtime float64) any, waketime float64) *Completion {
	c := r.NewCompletion()
	var t *Timer
	t = r.RegisterTimer(func(eventtime float64) float64 {
		r.UnregisterTimer(t)
		c.Complete(callback(eventtime))
		return Never
	}, waketime)
	return c
}

// Asynchronous callbacks, from other goroutines

func (r *Reactor) queueAsync(f func()) {
	r.asyncMu.Lock()
	r.async = append(r.async, f)
	r.asyncMu.Unlock()
	select {
	case r.asyncCh <- struct{}{}:
	default:
	}
}

// RegisterAsyncCallback is RegisterCallback for another goroutine.
func (r *Reactor) RegisterAsyncCallback(callback func(eventtime float64) any, waketime float64) {
	r.queueAsync(func() { r.RegisterCallback(callback, waketime) })
}

// AsyncComplete is Completion.Complete for another goroutine.
func (r *Reactor) AsyncComplete(c *Completion, result any) {
	r.queueAsync(func() { c.Complete(result) })
}

// takeAsync waits up to timeout for asynchronous callbacks, and returns them.
func (r *Reactor) takeAsync(timeout float64) []func() {
	r.asyncMu.Lock()
	fs := r.async
	r.async = nil
	r.asyncMu.Unlock()
	if len(fs) > 0 || timeout <= 0 {
		return fs
	}
	t := time.NewTimer(time.Duration(timeout * float64(time.Second)))
	defer t.Stop()
	select {
	case <-r.asyncCh:
	case <-t.C:
	}
	r.asyncMu.Lock()
	fs = r.async
	r.async = nil
	r.asyncMu.Unlock()
	return fs
}

// Pauses

func (r *Reactor) sysPause(waketime float64) float64 {
	if delay := waketime - r.Monotonic(); delay > 0 {
		time.Sleep(time.Duration(delay * float64(time.Second)))
	}
	return r.Monotonic()
}

// Pause lets the loop run other code until waketime, and returns the time when this code goes on.
// Before Run, it sleeps.
func (r *Reactor) Pause(waketime float64) float64 {
	if r.dispatch == nil {
		// The reactor is not running - use a system pause instead
		return r.sysPause(waketime)
	}
	if r.preventPause > 0 {
		r.VerifyCanPause()
	}
	g := r.cur
	if g != r.dispatch {
		// This goroutine has paused before and has a timer, so go back to checkTimers (through
		// the return of g.timer's callback).
		return r.switchTo(r.dispatch, waketime).(float64)
	}
	// Pausing the dispatch goroutine: a timer resumes it.
	g.timer = r.RegisterTimer(func(eventtime float64) float64 {
		return r.switchTo(g, eventtime).(float64)
	}, waketime)
	r.nextTimer = Now
	var v any
	if n := len(r.cached); n > 0 {
		// Go on with a cached dispatch goroutine
		next := r.cached[n-1]
		r.cached = r.cached[:n-1]
		v = r.switchTo(next, nil)
	} else {
		// No cached goroutines: Run makes a new dispatch goroutine
		v = r.switchTo(r.root, nil)
	}
	// This goroutine goes on from g.timer's callback (through checkTimers)
	return v.(float64)
}

// endGreenlet ends the wait of a goroutine whose timer or event callback paused and is now done.
func (r *Reactor) endGreenlet(old *greenlet) {
	r.UnregisterTimer(old.timer)
	old.timer = nil
	// Cache this goroutine for later use
	r.cached = append(r.cached, old)
	// Go back to checkTimers (through the return of old.timer's callback)
	r.switchTo(r.dispatch, Never)
	// This goroutine goes on from Pause: it is the dispatch loop again
	r.dispatch = old
}

// AssertNoPause runs f with pauses disabled. A pause in f panics with ErrPauseDisabled.
func (r *Reactor) AssertNoPause(f func()) {
	r.preventPause++
	defer func() { r.preventPause-- }()
	f()
}

// VerifyCanPause panics with ErrPauseDisabled inside AssertNoPause.
func (r *Reactor) VerifyCanPause() {
	if r.preventPause > 0 {
		panic(ErrPauseDisabled)
	}
}

// Mutex is a lock for reactor code: Lock pauses until the lock is free. Code that waits gets the
// lock in the order of its Lock calls.
type Mutex struct {
	r           *Reactor
	locked      bool
	nextPending bool
	queue       []*greenlet
}

// NewMutex makes a mutex.
func (r *Reactor) NewMutex(locked bool) *Mutex { return &Mutex{r: r, locked: locked} }

// Test reports that the mutex is locked.
func (m *Mutex) Test() bool { return m.locked }

func (m *Mutex) Lock() {
	if !m.locked {
		m.locked = true
		return
	}
	g := m.r.cur
	m.queue = append(m.queue, g)
	for {
		m.r.Pause(Never)
		if m.nextPending && m.queue[0] == g {
			m.nextPending = false
			m.queue = m.queue[1:]
			return
		}
	}
}

func (m *Mutex) Unlock() {
	if len(m.queue) == 0 {
		m.locked = false
		return
	}
	m.nextPending = true
	m.r.UpdateTimer(m.queue[0].timer, Now)
}

// High latency checking

// SetLatencyNotifier sets a callback for when the loop does not run for latency seconds.
func (r *Reactor) SetLatencyNotifier(latency float64, callback func(eventtime, prevEventtime float64)) {
	r.latencyWarning = latency
	r.latencyCallback = callback
}

// File descriptors

// FileHandler is a file that the loop reads (ReactorFileHandler).
type FileHandler struct {
	File         *os.File
	readCallback func(eventtime float64)
}

// RegisterFd calls readCallback in the loop while the file is readable. Only files that are
// always readable are supported: regular files, as the G-code file of the batch mode. poll()
// reports these files as readable at all times.
func (r *Reactor) RegisterFd(f *os.File, readCallback func(eventtime float64)) *FileHandler {
	h := &FileHandler{File: f, readCallback: readCallback}
	r.fds = append(append([]*FileHandler(nil), r.fds...), h)
	return h
}

// UnregisterFd stops the calls of a file handler.
func (r *Reactor) UnregisterFd(h *FileHandler) {
	var fds []*FileHandler
	for _, x := range r.fds {
		if x != h {
			fds = append(fds, x)
		}
	}
	r.fds = fds
}

// dispatchFdEvents calls the read callbacks of the files. It returns true when one of them
// paused.
func (r *Reactor) dispatchFdEvents(eventtime float64, hdls []*FileHandler) bool {
	gDispatch := r.dispatch
	for _, h := range hdls {
		h.readCallback(eventtime)
		if gDispatch != r.dispatch {
			r.endGreenlet(gDispatch)
			return true
		}
	}
	return false
}

// The main loop

// dispatchAsync runs the asynchronous callbacks. It returns true when one of them paused, and puts
// the callbacks that did not run back first in the queue.
func (r *Reactor) dispatchAsync(fs []func()) bool {
	gDispatch := r.dispatch
	for i, f := range fs {
		f()
		if gDispatch != r.dispatch {
			rest := fs[i+1:]
			r.asyncMu.Lock()
			r.async = append(append([]func(){}, rest...), r.async...)
			r.asyncMu.Unlock()
			r.endGreenlet(gDispatch)
			return true
		}
	}
	return false
}

func (r *Reactor) dispatchLoop() {
	eventtime := 0.
	busy := true
	for r.process {
		// Check if can sleep
		timeout := 0.
		if !busy {
			timeout = r.calcSleepTime(eventtime)
		}
		// Check for file activity: a registered file is always readable
		hdls := r.fds
		wait := timeout
		if len(hdls) > 0 {
			wait = 0
		}
		// Check for asynchronous callbacks
		fs := r.takeAsync(wait)
		eventtime = r.Monotonic()
		if timeout != 0 {
			r.startBusyTime = eventtime
		}
		busy = false
		// Check for high latency
		prev := r.recentEventtime
		r.recentEventtime = eventtime
		if timeout == 0 && eventtime-prev >= r.latencyWarning {
			busy = true
			r.preventPause++
			r.latencyCallback(eventtime, prev)
			r.preventPause--
		}
		// Dispatch asynchronous callbacks (Klipper's pipe, the first file of the loop), then the
		// file events
		if len(fs) > 0 {
			busy = true
			if r.dispatchAsync(fs) {
				continue
			}
		}
		if len(hdls) > 0 {
			busy = true
			if r.dispatchFdEvents(eventtime, hdls) {
				continue
			}
		}
		// Dispatch pending timers
		if eventtime >= r.nextTimer {
			busy = true
			r.checkTimers(eventtime)
		}
	}
}

// Run runs the loop until End. It must not run twice at the same time.
func (r *Reactor) Run() {
	r.root = &greenlet{wake: make(chan any)}
	r.cur = r.root
	r.process = true
	r.preventPause = 0
	r.recentEventtime = r.Monotonic()
	r.startBusyTime = r.recentEventtime
	for r.process {
		// Make a new goroutine to dispatch timers and events
		g := r.newGreenlet(r.dispatchLoop)
		r.dispatch = g
		r.switchTo(g, nil)
		// Control comes back here on End, or from a pause with no cached goroutine
	}
	r.dispatch = nil
}

// End stops the loop after the current callback.
func (r *Reactor) End() { r.process = false }
