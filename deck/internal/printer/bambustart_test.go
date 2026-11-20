package printer

import (
	"context"
	"strings"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

// A start that the printer refuses tries the next address of the 3MF, and keeps the address that
// works. A start that does not begin shows in the panel. A start that each address fails gives
// the reason of the printer.
func TestBambuStartAnswers(t *testing.T) {
	f, err := bambu.NewFake("00TEST000000000", "12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.StartAnswer = func(url string) (string, string, bool) {
		if strings.HasPrefix(url, "file:///sdcard/") {
			return "success", "", true
		}
		return "fail", "the file is not on the card", true
	}
	f.Send(map[string]any{"gcode_state": "IDLE"})
	b := NewBambu(f.Addr(), f.Serial, f.Code, "", "X2D", nil)
	b.Client.Roots = f.Roots
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go b.Run(ctx, nil)
	waitFor(t, "link", func() bool { return b.State().Connected })
	defer func(d time.Duration) { startCheck = d }(startCheck)
	startCheck = 150 * time.Millisecond

	s := bambu.Start{File: "part.gcode.3mf", Name: "part", Plate: "Metadata/plate_1.gcode"}
	if err := b.start(s); err != nil {
		t.Fatal(err)
	}
	if b.startForm != 1 {
		t.Errorf("start form %d, want 1 (file:///sdcard/)", b.startForm)
	}
	waitFor(t, "the check of the start", func() bool { return strings.Contains(b.State().Message, "did not start") })

	// The next start uses the address that worked first.
	n := len(f.Requests())
	if err := b.start(s); err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, q := range f.Requests()[n:] {
		if p, ok := q["print"].(map[string]any); ok && p["command"] == "project_file" {
			starts++
			if !strings.HasPrefix(p["url"].(string), "file:///sdcard/") {
				t.Errorf("url %v", p["url"])
			}
		}
	}
	if starts != 1 {
		t.Errorf("%d starts, want 1", starts)
	}

	f.StartAnswer = func(string) (string, string, bool) { return "fail", "the plate is wrong", true }
	if err := b.start(s); err == nil || !strings.Contains(err.Error(), "the plate is wrong") {
		t.Errorf("all refused: %v", err)
	}
}
