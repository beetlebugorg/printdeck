//go:build linux

package clocksync

import (
	"math"
	"os"
	"testing"
	"time"

	"printdeck/internal/klipper/fakemcu"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
)

// The estimate follows the clock of a fake board at 16 MHz, and the periodic queries go on.
func TestClockSyncFollowsTheBoard(t *testing.T) {
	dict, err := os.ReadFile("../testdata/klipper.dict")
	if err != nil {
		t.Fatal(err)
	}
	board, err := fakemcu.New(dict)
	if err != nil {
		t.Fatal(err)
	}
	m, path, err := fakemcu.OpenPty()
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	defer m.Close()
	go board.Serve(m)

	r := reactor.New()
	var est Estimate
	var clockErr float64
	var active bool
	r.RegisterCallback(func(float64) any {
		defer r.End()
		s := serialhdl.New(r, "")
		defer s.Disconnect()
		if err := s.ConnectPipe(path); err != nil {
			t.Error(err)
			return nil
		}
		cs := New(r)
		if err := cs.Connect(s); err != nil {
			t.Error(err)
			return nil
		}
		// Let the periodic queries run.
		r.Pause(r.Monotonic() + 2.5)
		est = cs.Estimate()
		clockErr = float64(cs.GetClock(r.Monotonic())) - float64(board.Clock())
		active = cs.IsActive()
		return nil
	}, reactor.Now)
	done := make(chan struct{})
	go func() {
		r.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the reactor did not end")
	}

	if math.Abs(est.Freq-16e6)/16e6 > 0.01 {
		t.Errorf("frequency estimate %.0f, want 16000000 within 1%%", est.Freq)
	}
	// 5 ms of error is 80000 ticks: the pseudo-terminal and the scheduler add some.
	if math.Abs(clockErr) > 80000 {
		t.Errorf("GetClock is %.0f ticks from the board", clockErr)
	}
	if !active {
		t.Error("the clock sync is not active")
	}
	queries := 0
	for _, c := range board.Received() {
		if c == "get_clock" {
			queries++
		}
	}
	// 8 at connect, and about one each second after it.
	if queries < 10 {
		t.Errorf("only %d get_clock queries", queries)
	}
}

func TestPrintTimeConversions(t *testing.T) {
	cs := &ClockSync{mcuFreq: 16e6}
	if c := cs.PrintTimeToClock(1.5); c != 24000000 {
		t.Errorf("clock of 1.5 s: %d", c)
	}
	if pt := cs.ClockToPrintTime(24000000); pt != 1.5 {
		t.Errorf("print time of 24000000: %v", pt)
	}
	cs.lastClock = 0x1fffffff0
	if c := cs.Clock32ToClock64(0x10); c != 0x200000010 {
		t.Errorf("clock32 after wrap: %#x", c)
	}
}
