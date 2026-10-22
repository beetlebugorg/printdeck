//go:build linux

package serialhdl

import (
	"os"
	"strings"
	"testing"
	"time"

	"printdeck/internal/klipper/fakemcu"
	"printdeck/internal/klipper/reactor"
)

// startBoard serves a fake board with the RAMBo dictionary on a pseudo-terminal, and returns the
// board and the path for the host.
func startBoard(t *testing.T) (*fakemcu.Board, string) {
	t.Helper()
	dict, err := os.ReadFile("../testdata/klipper.dict")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fakemcu.New(dict)
	if err != nil {
		t.Fatal(err)
	}
	m, path, err := fakemcu.OpenPty()
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	go b.Serve(m)
	return b, path
}

// runReactor runs f as reactor code, and returns when it is done.
func runReactor(t *testing.T, f func(r *reactor.Reactor)) {
	t.Helper()
	r := reactor.New()
	r.RegisterCallback(func(float64) any {
		defer r.End()
		f(r)
		return nil
	}, reactor.Now)
	done := make(chan struct{})
	go func() {
		r.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the reactor did not end")
	}
}

// The host connects, reads the data dictionary of the board in 40-byte pieces, and gets the clock.
func TestConnectIdentifyAndQuery(t *testing.T) {
	board, path := startBoard(t)
	var clock int64
	var commands, messages int
	runReactor(t, func(r *reactor.Reactor) {
		s := New(r, "")
		defer s.Disconnect()
		if err := s.ConnectPipe(path); err != nil {
			t.Error(err)
			return
		}
		for _, m := range s.MsgParser().Messages() {
			messages++
			if m.Kind == "command" {
				commands++
			}
		}
		params, err := s.SendWithResponse("get_clock", "clock")
		if err != nil {
			t.Error(err)
			return
		}
		clock = params.Int("clock")
	})
	// klippy logs "Loaded MCU 'mcu' 143 commands" for this firmware: all its messages.
	if messages != 143 || commands != 116 {
		t.Errorf("the dictionary has %d messages and %d commands, want 143 and 116", messages, commands)
	}
	if clock <= 0 {
		t.Errorf("clock %d", clock)
	}
	identifies := 0
	for _, c := range board.Received() {
		if strings.HasPrefix(c, "identify ") {
			identifies++
		}
	}
	if identifies < 10 {
		t.Errorf("only %d identify commands", identifies)
	}
}

// With no response of the right name, the retries end with Klipper's error.
func TestNoResponseGivesError(t *testing.T) {
	board, path := startBoard(t)
	var err error
	runReactor(t, func(r *reactor.Reactor) {
		s := New(r, "")
		defer s.Disconnect()
		if err = s.ConnectPipe(path); err != nil {
			return
		}
		// The board answers get_uptime with "uptime", never with "clock".
		_, err = s.SendWithResponse("get_uptime", "clock")
	})
	if err == nil || err.Error() != "Unable to obtain 'clock' response" {
		t.Errorf("error %v", err)
	}
	tries := 0
	for _, c := range board.Received() {
		if c == "get_uptime" {
			tries++
		}
	}
	if tries != 6 {
		t.Errorf("get_uptime was sent %d times, want 6 (one and 5 retries)", tries)
	}
}

// After a disconnect, the host connects to the same port again.
func TestReconnect(t *testing.T) {
	_, path := startBoard(t)
	runReactor(t, func(r *reactor.Reactor) {
		for i := 0; i < 2; i++ {
			s := New(r, "")
			if err := s.ConnectPipe(path); err != nil {
				t.Errorf("connect %d: %v", i+1, err)
				return
			}
			if _, err := s.SendWithResponse("get_clock", "clock"); err != nil {
				t.Errorf("query %d: %v", i+1, err)
			}
			s.Disconnect()
		}
	})
}
