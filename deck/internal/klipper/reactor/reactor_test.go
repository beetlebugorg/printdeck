package reactor

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// runFor runs the reactor with setup, until end is called or the test times out.
func runFor(t *testing.T, setup func(r *Reactor, end func())) {
	t.Helper()
	r := New()
	setup(r, r.End)
	done := make(chan struct{})
	go func() {
		r.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reactor did not end")
	}
}

// log is a list of events, safe for the goroutines of the reactor.
type log struct {
	mu sync.Mutex
	ev []string
}

func (l *log) add(s string) {
	l.mu.Lock()
	l.ev = append(l.ev, s)
	l.mu.Unlock()
}

func (l *log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.ev, " ")
}

func TestTimersRunAndReschedule(t *testing.T) {
	var l log
	runFor(t, func(r *Reactor, end func()) {
		n := 0
		r.RegisterTimer(func(et float64) float64 {
			n++
			l.add("tick")
			if n == 3 {
				end()
				return Never
			}
			return et + 0.01
		}, Now)
	})
	if got := l.String(); got != "tick tick tick" {
		t.Errorf("events %q", got)
	}
}

// Code that pauses inside a timer goes on later from the same place, and other timers run while it
// waits.
func TestPauseLetsOtherTimersRun(t *testing.T) {
	var l log
	runFor(t, func(r *Reactor, end func()) {
		r.RegisterTimer(func(et float64) float64 {
			l.add("a-start")
			r.Pause(r.Monotonic() + 0.05)
			l.add("a-end")
			end()
			return Never
		}, Now)
		r.RegisterTimer(func(et float64) float64 {
			l.add("b")
			return Never
		}, r.Monotonic()+0.01)
	})
	if got := l.String(); got != "a-start b a-end" {
		t.Errorf("events %q", got)
	}
}

// Two pauses at the same time go on in the order of their wake times.
func TestNestedPauses(t *testing.T) {
	var l log
	runFor(t, func(r *Reactor, end func()) {
		r.RegisterTimer(func(et float64) float64 {
			l.add("long-start")
			r.Pause(r.Monotonic() + 0.08)
			l.add("long-end")
			end()
			return Never
		}, Now)
		r.RegisterTimer(func(et float64) float64 {
			l.add("short-start")
			r.Pause(r.Monotonic() + 0.02)
			l.add("short-end")
			return Never
		}, r.Monotonic()+0.01)
	})
	if got := l.String(); got != "long-start short-start short-end long-end" {
		t.Errorf("events %q", got)
	}
}

// A completion wakes the code that waits for it, also from another goroutine.
func TestCompletionFromAnotherGoroutine(t *testing.T) {
	var got any
	runFor(t, func(r *Reactor, end func()) {
		c := r.NewCompletion()
		r.RegisterCallback(func(et float64) any {
			got = c.Wait(Never, nil)
			end()
			return nil
		}, Now)
		go func() {
			time.Sleep(20 * time.Millisecond)
			r.AsyncComplete(c, "done")
		}()
	})
	if got != "done" {
		t.Errorf("result %v", got)
	}
}

func TestCompletionTimeout(t *testing.T) {
	var got any
	runFor(t, func(r *Reactor, end func()) {
		c := r.NewCompletion()
		r.RegisterCallback(func(et float64) any {
			got = c.Wait(r.Monotonic()+0.02, "timeout")
			end()
			return nil
		}, Now)
	})
	if got != "timeout" {
		t.Errorf("result %v", got)
	}
}

// RegisterCallback returns the result of the callback through its completion.
func TestRegisterCallbackResult(t *testing.T) {
	var got any
	runFor(t, func(r *Reactor, end func()) {
		c := r.RegisterCallback(func(et float64) any { return 42 }, Now)
		r.RegisterCallback(func(et float64) any {
			got = c.Wait(Never, nil)
			end()
			return nil
		}, Now)
	})
	if got != 42 {
		t.Errorf("result %v", got)
	}
}

// Code that waits for a mutex gets it in the order of its Lock calls.
func TestMutexOrder(t *testing.T) {
	var l log
	runFor(t, func(r *Reactor, end func()) {
		m := r.NewMutex(false)
		r.RegisterCallback(func(et float64) any {
			m.Lock()
			l.add("1-lock")
			r.Pause(r.Monotonic() + 0.03)
			l.add("1-unlock")
			m.Unlock()
			return nil
		}, Now)
		for _, name := range []string{"2", "3"} {
			name := name
			r.RegisterCallback(func(et float64) any {
				m.Lock()
				l.add(name + "-lock")
				m.Unlock()
				if name == "3" {
					end()
				}
				return nil
			}, r.Monotonic()+0.01)
		}
	})
	if got := l.String(); got != "1-lock 1-unlock 2-lock 3-lock" {
		t.Errorf("events %q", got)
	}
}

func TestAssertNoPause(t *testing.T) {
	var recovered any
	runFor(t, func(r *Reactor, end func()) {
		r.RegisterCallback(func(et float64) any {
			defer end()
			defer func() { recovered = recover() }()
			r.AssertNoPause(func() { r.Pause(r.Monotonic() + 0.01) })
			return nil
		}, Now)
	})
	if recovered != ErrPauseDisabled {
		t.Errorf("recovered %v, want ErrPauseDisabled", recovered)
	}
}

// Before Run, Pause sleeps.
func TestPauseBeforeRun(t *testing.T) {
	r := New()
	start := r.Monotonic()
	if got := r.Pause(start + 0.02); got-start < 0.019 {
		t.Errorf("paused %.3f s", got-start)
	}
}
