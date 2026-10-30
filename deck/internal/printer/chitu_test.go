package printer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/chitu"
)

// The backend of a ChiTu board against the fake board: a print, its pause, its end in the history,
// a print that stops, and a board that does not answer. The backend sends only M4000.
func TestChiTuBackend(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	hist := &History{Path: filepath.Join(t.TempDir(), "history.json")}
	c := NewChiTu(b.Addr(), "Phenom", hist)
	c.Client.Timeout = 200 * time.Millisecond
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	if wait := c.poll(ctx, now); wait != chituIdleEvery || c.State().Printing || !c.State().Reachable {
		t.Fatalf("a board with no print: %v %+v", wait, c.State())
	}

	// The files of the card: the deck reads them with no print.
	b.Set(func(b *chitu.FakeBoard) {
		b.Files = []string{"._part.cbddlp 4096", "part.cbddlp 1000", "other.cbddlp 2000", "CHITUBOX 0"}
	})
	c.LoadFiles()
	if f := c.Files(); len(f) != 2 || f[0].Name != "part.cbddlp" {
		t.Fatalf("the files %+v", f)
	}

	// A print, at a quarter of its file after 10 minutes. Its name is the file of its size.
	b.Set(func(b *chitu.FakeBoard) { b.Read, b.Size, b.Z, b.Seconds = 250, 1000, 5.25, 600 })
	if wait := c.poll(ctx, now); wait != chituPrintingEvery {
		t.Fatalf("the wait during a print: %v", wait)
	}
	st := c.State()
	if !st.Printing || st.Completion != 25 || st.Z != 5.25 || st.Elapsed != 10*time.Minute || st.TimeLeft != 30*time.Minute ||
		st.FilePath == "" || st.Name() != "part" {
		t.Fatalf("the print %+v (name %q)", st, st.Name())
	}
	path := st.FilePath
	if !c.SetNextProject("Garden planters") || c.NextProject() != "Garden planters" {
		t.Error("the project of the print now")
	}
	b.Set(func(b *chitu.FakeBoard) { b.Flag = 1 })
	if c.poll(ctx, now.Add(5*time.Second)); !c.State().Paused || c.State().FilePath != path {
		t.Errorf("the pause %+v", c.State())
	}

	// The end: the board read the whole file, then shows no print.
	b.Set(func(b *chitu.FakeBoard) { b.Flag, b.Read, b.Seconds = 0, 1000, 2400 })
	c.poll(ctx, now.Add(30*time.Minute))
	b.Set(func(b *chitu.FakeBoard) { b.Read, b.Size, b.Seconds = 0, 0, 0 })
	c.poll(ctx, now.Add(31*time.Minute))
	recs := c.History()
	if len(recs) != 1 || recs[0].Result != "complete" || recs[0].Path != path || recs[0].Project != "Garden planters" ||
		recs[0].Duration != 40*time.Minute {
		t.Fatalf("the history after the print %+v", recs)
	}
	if c.State().Printing {
		t.Error("printing after the end")
	}

	// A print that stops at half of its file.
	b.Set(func(b *chitu.FakeBoard) { b.Read, b.Size, b.Seconds = 500, 1000, 300 })
	c.poll(ctx, now.Add(time.Hour))
	b.Set(func(b *chitu.FakeBoard) { b.Read, b.Size, b.Seconds = 0, 0, 0 })
	c.poll(ctx, now.Add(time.Hour+time.Minute))
	if recs := c.History(); len(recs) != 2 || recs[0].Result != "cancelled" {
		t.Fatalf("the history after a stop %+v", recs)
	}

	// A board that does not answer: offline after 3 queries.
	b.Drop(3)
	for i := range 3 {
		c.poll(ctx, now.Add(2*time.Hour+time.Duration(i)*time.Second))
	}
	if st := c.State(); st.Reachable || st.Text != "Offline" {
		t.Fatalf("offline %+v", st)
	}
	if c.poll(ctx, now.Add(3*time.Hour)); !c.State().Reachable {
		t.Error("back after an answer")
	}

	if m := c.Machine(); m.Board != "CBD V1.4.1" {
		t.Errorf("the board %q", m.Board)
	}

	// G-code from the screens goes nowhere.
	if err := c.Gcode("M6030 'x'"); err != ErrNotTested {
		t.Errorf("a command: %v", err)
	}
	for _, q := range b.Log() {
		if q != "M4000" && q != "M99999" && q != "M20" && !strings.HasPrefix(q, "G") {
			t.Errorf("the backend sent %q", q)
		}
	}
	if c.Machine().Process != ProcessResin {
		t.Error("the process")
	}

	// The plate: up by the step, at 300 mm/min at most, with relative moves; down 10 mm at most.
	n := len(b.Log())
	if err := c.Jog(0, 0, 50, 600); err != nil {
		t.Fatal(err)
	}
	if got := b.Log()[n:]; len(got) != 3 || got[0] != "G91" || got[1] != "G1 Z50.00 F300" || got[2] != "G90" {
		t.Errorf("the move up %q", got)
	}
	n = len(b.Log())
	if err := c.Jog(0, 0, -50, 600); err == nil || len(b.Log()) != n {
		t.Errorf("a move down of 50 mm: %v, %q", err, b.Log()[n:])
	}
	if err := c.Home("z"); err != nil || b.Log()[len(b.Log())-1] != "G28 Z0" {
		t.Errorf("home %v %q", err, b.Log())
	}
	if !strings.Contains(c.State().Message, "G28 Z0: ok") {
		t.Errorf("the answer in the panel %q", c.State().Message)
	}
}
