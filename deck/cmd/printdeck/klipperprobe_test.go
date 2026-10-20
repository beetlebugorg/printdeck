package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"printdeck/internal/klipper/fakemcu"
	"printdeck/internal/klipper/reactor"
)

// The probe connects to a fake RAMBo and sends only read-only commands.
func TestKlipperProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("the probe syncs the clock for 5 seconds")
	}
	dict, err := os.ReadFile("../../internal/klipper/testdata/klipper.dict")
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
	var perr error
	r.RegisterCallback(func(float64) any {
		defer r.End()
		perr = probe(r, path, 250000)
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
		t.Fatal("the probe did not end")
	}
	if perr != nil {
		t.Fatal(perr)
	}
	allowed := map[string]bool{"identify": true, "get_config": true, "get_uptime": true, "get_clock": true}
	for _, c := range board.Received() {
		if name, _, _ := strings.Cut(c, " "); !allowed[name] {
			t.Errorf("the probe sent %q", c)
		}
	}
}
