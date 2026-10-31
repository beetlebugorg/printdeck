package printer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"printdeck/internal/chitu"
	"printdeck/internal/timelapse"
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

// SaveUpload keeps the copy of an upload and sends it to the board, which then lists it.
func TestChiTuSaveUpload(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	data := bytes.Repeat([]byte("layer"), 1000)
	tmp := filepath.Join(c.UploadDir, ".upload-1")
	os.WriteFile(tmp, data, 0o644)
	c.SaveUpload("part.ctb", tmp)
	b.Set(func(b *chitu.FakeBoard) {
		if !bytes.Equal(b.Uploaded["part.ctb"], data) {
			t.Errorf("the board got %d bytes", len(b.Uploaded["part.ctb"]))
		}
	})
	if u := c.State().Upload; u.Name != "part.ctb" || u.Result != "sent" || u.Done != u.Size || u.Size != 5000 {
		t.Errorf("upload %+v", u)
	}
	if _, err := os.Stat(filepath.Join(c.UploadDir, "part.ctb")); err != nil {
		t.Error(err)
	}
	if c.Busy() != "" {
		t.Error("busy after the upload")
	}
}

// DeleteFile sends M30 and checks the list of the card.
func TestChiTuDeleteFile(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Set(func(b *chitu.FakeBoard) { b.Files = []string{"a.ctb 100", "b.ctb 200"} })
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	if err := c.DeleteFile("a.ctb"); err != nil {
		t.Fatal(err)
	}
	if fs := c.Files(); len(fs) != 1 || fs[0].Name != "b.ctb" {
		t.Errorf("files %+v", fs)
	}
	if err := c.DeleteFile("gone.ctb"); err == nil {
		t.Error("no error for a file that is not there")
	}
}

// A second upload waits for the first, and both reach the board.
func TestChiTuUploadQueue(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	done := make(chan bool)
	for i, n := range []string{"a.cbddlp", "b.cbddlp"} {
		tmp := filepath.Join(c.UploadDir, ".upload-"+n)
		os.WriteFile(tmp, bytes.Repeat([]byte(n), 3000+i*10), 0o644)
		go func() { c.SaveUpload(n, tmp); done <- true }()
	}
	<-done
	<-done
	b.Set(func(b *chitu.FakeBoard) {
		if len(b.Uploaded["a.cbddlp"]) != 24000 || len(b.Uploaded["b.cbddlp"]) != 24080 {
			t.Errorf("the board got %d and %d bytes", len(b.Uploaded["a.cbddlp"]), len(b.Uploaded["b.cbddlp"]))
		}
	})
	if c.Busy() != "" {
		t.Error("busy after the uploads")
	}
}

// A start that ChiTuBox asks for during an upload is refused, and no print starts after the
// upload: on 2026-10-30 such a start fired 12 minutes later with no build plate in place.
func TestChiTuNoStartAfterUpload(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	tmp := filepath.Join(c.UploadDir, ".upload-a")
	os.WriteFile(tmp, bytes.Repeat([]byte("x"), 4000), 0o644)
	c.Received("a.cbddlp", 4000, true)
	if a := c.RequestStart("a.cbddlp"); !strings.HasPrefix(a, "Error:") {
		t.Fatalf("a start during the upload: %q", a)
	}
	c.SaveUpload("a.cbddlp", tmp)
	for _, q := range b.Log() {
		if strings.HasPrefix(q, "M6030") {
			t.Fatalf("the board got %q", q)
		}
	}
	if u := c.State().Upload; u.Result != "sent" || u.StartAfter {
		t.Errorf("upload %+v", u)
	}
}

// uploadStallForTest makes a stalled upload fail at once, and returns the function that sets the
// time back.
func uploadStallForTest(t *testing.T) func() {
	old := chitu.UploadStall
	chitu.UploadStall = 10 * time.Millisecond
	return func() { chitu.UploadStall = old }
}

// With no print, a move of the plate shows in the message until the plate stops.
func TestChiTuPlateMoves(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	now := time.Now()
	c.update(chitu.Status{Z: 0}, now)
	c.update(chitu.Status{Z: -20}, now.Add(10*time.Second))
	if m := c.State().Message; m != "The plate moves · Z -20.0" {
		t.Errorf("moving: %q", m)
	}
	c.update(chitu.Status{Z: -20}, now.Add(20*time.Second))
	if m := c.State().Message; m != "" {
		t.Errorf("stopped: %q", m)
	}
}

// A file on the card with another size than the copy of the deck is incomplete: the list says
// so, a print of it sends the copy again, and ChiTuBox cannot start it.
func TestChiTuIncompleteFile(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Set(func(b *chitu.FakeBoard) { b.Files = []string{"plate.cbddlp 1000"} })
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	os.WriteFile(filepath.Join(c.UploadDir, "plate.cbddlp"), bytes.Repeat([]byte("x"), 5000), 0o644)
	c.LoadFiles()
	if fs := c.Files(); len(fs) != 1 || fs[0].Name != "INCOMPLETE plate.cbddlp (1 KB of 5 KB)" || fs[0].Path != "plate.cbddlp" {
		t.Fatalf("files %+v", fs)
	}
	if a := c.RequestStart("plate.cbddlp"); !strings.HasPrefix(a, "Error:") {
		t.Errorf("a start of an incomplete file: %q", a)
	}
	c.PrintFile("plate.cbddlp")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && c.State().Upload.Result == "" {
		time.Sleep(20 * time.Millisecond)
	}
	for _, q := range b.Log() {
		if strings.HasPrefix(q, "M6030") {
			t.Errorf("the board got %q", q)
		}
	}
	b.Set(func(b *chitu.FakeBoard) {
		if len(b.Uploaded["plate.cbddlp"]) != 5000 {
			t.Errorf("the board got %d bytes of the copy", len(b.Uploaded["plate.cbddlp"]))
		}
	})
}

// Stop sending ends the upload, closes the file on the card and deletes it, and starts no print.
func TestChiTuCancelSend(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Set(func(b *chitu.FakeBoard) { b.LoseData = 1 << 30 })
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	tmp := filepath.Join(c.UploadDir, ".upload-1")
	os.WriteFile(tmp, bytes.Repeat([]byte("x"), 5000), 0o644)
	c.Received("big.cbddlp", 5000, true)
	c.RequestStart("big.cbddlp")
	done := make(chan bool)
	go func() { c.SaveUpload("big.cbddlp", tmp); done <- true }()
	time.Sleep(300 * time.Millisecond)
	c.CancelSend()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the upload did not stop")
	}
	var got []string
	for _, q := range b.Log() {
		if q == "M29" || strings.HasPrefix(q, "M30") || strings.HasPrefix(q, "M6030") {
			got = append(got, q)
		}
	}
	if strings.Join(got, ",") != "M29,M30 big.cbddlp" {
		t.Errorf("the board got %q", got)
	}
	if u := c.State().Upload; u.Result != "failed" || !strings.HasPrefix(u.Err, "Stopped") {
		t.Errorf("upload %+v", u)
	}
}

// A print of a file with a copy on the deck has the layers of the header of the copy.
func TestChiTuLayers(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.files = []chitu.File{{Name: "plate.cbddlp", Size: 1000}}
	c.headers = map[string]*chitu.Sliced{"plate.cbddlp": {Layers: 600}}
	c.update(chitu.Status{Read: 500, Size: 1000, Seconds: 60}, time.Now())
	if st := c.State(); st.File != "plate.cbddlp" || st.Layer != 300 || st.Layers != 600 {
		t.Errorf("layers %q %d/%d", st.Title, st.Layer, st.Layers)
	}
}

// The project of the next print goes to the print at its start.
func TestChiTuNextProject(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{Path: filepath.Join(t.TempDir(), "h.json")})
	if !c.SetNextProject("Bittle X") || c.NextProject() != "Bittle X" {
		t.Fatal("the project of the next print")
	}
	c.update(chitu.Status{Read: 10, Size: 1000, Seconds: 5}, time.Now())
	if p := c.NextProject(); p != "Bittle X" {
		t.Errorf("the project of the print: %q", p)
	}
	c.mu.Lock()
	next := c.nextProject
	c.mu.Unlock()
	if next != "" {
		t.Errorf("the next project stays: %q", next)
	}
}

// A deck that starts again during a print goes on with the same print: from resin-print.json, or
// from the project of the print when the deck before saved no file.
func TestChiTuResume(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	status := chitu.Status{Read: 5000, Size: 165072291, Seconds: 3600}

	first := NewChiTu("127.0.0.1:1", "Phenom", &History{Path: filepath.Join(dir, "h.json")})
	first.PrintStore = filepath.Join(dir, "resin-print.json")
	first.update(status, now)
	key := first.State().FilePath

	// The deck starts again 5 minutes later: the board read more and its clock went on.
	later := chitu.Status{Read: 9000, Size: status.Size, Seconds: 3900}
	again := NewChiTu("127.0.0.1:1", "Phenom", &History{Path: filepath.Join(dir, "h.json")})
	again.PrintStore = first.PrintStore
	again.update(later, now.Add(5*time.Minute+3*time.Second))
	if got := again.State().FilePath; got != key {
		t.Errorf("after a start of the deck: %q, want %q", got, key)
	}

	// Another file of another size: a new print.
	other := NewChiTu("127.0.0.1:1", "Phenom", &History{Path: filepath.Join(dir, "h.json")})
	other.PrintStore = first.PrintStore
	other.update(chitu.Status{Read: 10, Size: 999, Seconds: 5}, now.Add(time.Hour))
	if other.State().FilePath == key {
		t.Error("another print took the key")
	}

	// No resin-print.json, but the project of the print: the key of the project.
	h := &History{Path: filepath.Join(dir, "h2.json")}
	old := "resin-" + strconv.FormatInt(now.Add(-time.Hour).Unix(), 10)
	h.SetNextProject(old, "Bittle X")
	c := NewChiTu("127.0.0.1:1", "Phenom", h)
	c.update(status, now.Add(4*time.Second))
	if got := c.State().FilePath; got != old || c.NextProject() != "Bittle X" {
		t.Errorf("from the project: %q %q, want %q", got, c.NextProject(), old)
	}
}

// The time left of a resin print comes from the plan of the layers of the file, less the part of
// the layer now that already went. After three layers, the moves of a layer come from the clock
// of the board.
func TestChiTuTimeLeftFromPlan(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.files = []chitu.File{{Name: "plate.cbddlp", Size: 1000}}
	c.headers = map[string]*chitu.Sliced{"plate.cbddlp": {Layers: 4}}
	c.plans = map[string]filePlan{"plate.cbddlp": {light: []float64{95, 95, 25, 25}, moves: 5}}
	// Layer 2 of 4, 110 s after the start: layer 1 took 100 s, and layer 2 runs for 10 s.
	c.update(chitu.Status{Read: 500, Size: 1000, Seconds: 110}, time.Now())
	st := c.State()
	if st.Layer != 2 || !st.HasTimeLeft || st.TimeLeft != 150*time.Second {
		t.Errorf("layer %d, left %v %v", st.Layer, st.TimeLeft, st.HasTimeLeft)
	}
	if lt := c.LayerTimes(); len(lt) != 4 {
		t.Errorf("layer times %v", lt)
	}
}

// The moves of a layer as the print measures them: 10 layers of 20 s of light took 400 s, so each
// layer had 20 s of moves, in place of the 5 s of the header.
func TestChiTuMeasuredMoves(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.files = []chitu.File{{Name: "plate.cbddlp", Size: 2000}}
	light := make([]float64, 20)
	for i := range light {
		light[i] = 20
	}
	c.headers = map[string]*chitu.Sliced{"plate.cbddlp": {Layers: 20}}
	c.plans = map[string]filePlan{"plate.cbddlp": {light: light, moves: 5}}
	// Layer 11 starts: 10 layers done in 400 s.
	c.update(chitu.Status{Read: 1050, Size: 2000, Seconds: 400}, time.Now())
	if st := c.State(); st.Layer != 11 || st.TimeLeft != 400*time.Second {
		t.Errorf("layer %d, left %v, want 400s", st.Layer, st.TimeLeft)
	}
}

// An upload of a file that the board has already, with the same size, sends nothing: the deck
// keeps its copy. During a print, a new file waits, and a start is refused.
func TestChiTuUploadDuringPrint(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Set(func(b *chitu.FakeBoard) { b.Files = []string{"plate.cbddlp 4000"}; b.Read, b.Size = 100, 4000 })
	c := NewChiTu(b.Addr(), "Phenom", &History{})
	c.UploadDir = t.TempDir()
	c.update(chitu.Status{Read: 100, Size: 4000, Seconds: 10}, time.Now())
	tmp := filepath.Join(c.UploadDir, ".upload-1")
	os.WriteFile(tmp, bytes.Repeat([]byte("x"), 4000), 0o644)
	c.SaveUpload("plate.cbddlp", tmp)
	for _, q := range b.Log() {
		if strings.HasPrefix(q, "M28") {
			t.Fatalf("the board got %q for a file that it has", q)
		}
	}
	if _, err := os.Stat(filepath.Join(c.UploadDir, "plate.cbddlp")); err != nil {
		t.Error("no copy on the deck")
	}
	if a := c.RequestStart("plate.cbddlp"); !strings.HasPrefix(a, "Error:") {
		t.Errorf("a start during a print: %q", a)
	}

	// A new file waits for the end of the print.
	old := chituHoldEvery
	chituHoldEvery = 50 * time.Millisecond
	defer func() { chituHoldEvery = old }()
	tmp2 := filepath.Join(c.UploadDir, ".upload-2")
	os.WriteFile(tmp2, []byte("new file"), 0o644)
	done := make(chan bool)
	go func() { c.SaveUpload("new.cbddlp", tmp2); done <- true }()
	time.Sleep(200 * time.Millisecond)
	if u := c.State().Upload; !u.Held || u.Name != "new.cbddlp" {
		t.Errorf("the upload during the print: %+v", u)
	}
	c.mu.Lock()
	c.last = chitu.Status{}
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the upload did not go after the print")
	}
	b.Set(func(b *chitu.FakeBoard) {
		if string(b.Uploaded["new.cbddlp"]) != "new file" {
			t.Errorf("the board got %q", b.Uploaded["new.cbddlp"])
		}
	})
}

// The time lapse of a resin print asks for a frame at each new layer, when the plate is up.
func TestChiTuTimelapse(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.files = []chitu.File{{Name: "plate.cbddlp", Size: 1000}}
	c.headers = map[string]*chitu.Sliced{"plate.cbddlp": {Layers: 100}}
	c.SetTimelapse(true, false, 0, 1, 0)
	now := time.Now()
	step := func(read int64, z float64) timelapse.HostStatus {
		now = now.Add(time.Second)
		c.update(chitu.Status{Read: read, Size: 1000, Seconds: 100, Z: z}, now)
		hs, _ := c.TimelapseStatus()
		return hs
	}
	if hs := step(100, 0.5); hs.Frame != 0 {
		t.Fatalf("a frame with the plate in the vat: %+v", hs)
	}
	hs := step(100, 6)
	if hs.Frame != 1 || !hs.Waiting {
		t.Fatalf("no frame with the plate up: %+v", hs)
	}
	c.TimelapseTaken(1)
	if hs := step(100, 8); hs.Frame != 1 || hs.Waiting {
		t.Errorf("a second frame in the same layer: %+v", hs)
	}
	step(110, 0.55) // the next layer, in the vat
	if hs := step(110, 7); hs.Frame != 2 {
		t.Errorf("no frame of the next layer: %+v", hs)
	}
}

// A copy under another name than the file on the card is the copy of that file when it has its
// size.
func TestChiTuCopyBySize(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.UploadDir = t.TempDir()
	c.files = []chitu.File{{Name: "plate.cbddlp", Size: 7}}
	os.WriteFile(filepath.Join(c.UploadDir, "other.cbddlp"), []byte("1234567"), 0o644)
	os.WriteFile(filepath.Join(c.UploadDir, "small.cbddlp"), []byte("12"), 0o644)
	if p := c.copyPath("plate.cbddlp"); filepath.Base(p) != "other.cbddlp" {
		t.Errorf("copy %q", p)
	}
}

// A name for the print replaces the name of its file, and stays through a restart of the deck.
func TestChiTuTitle(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	c.PrintStore = filepath.Join(dir, "resin-print.json")
	c.files = []chitu.File{{Name: "resin_phenom_plate.cbddlp", Size: 1000}}
	c.update(chitu.Status{Read: 10, Size: 1000, Seconds: 60}, now)
	c.SetTitle("Shoulder pieces")
	if st := c.State(); st.Name() != "Shoulder pieces" {
		t.Fatalf("name %q", st.Name())
	}
	again := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	again.PrintStore = c.PrintStore
	again.files = c.files
	again.update(chitu.Status{Read: 20, Size: 1000, Seconds: 120}, now.Add(time.Minute))
	if st := again.State(); st.Name() != "Shoulder pieces" {
		t.Errorf("after a restart: %q", st.Name())
	}
}

// One print keeps its key when the read position of the board goes back a little; a print that
// starts again (its clock starts at 0) gets a new key.
func TestChiTuOneKey(t *testing.T) {
	c := NewChiTu("127.0.0.1:1", "Phenom", &History{})
	now := time.Now()
	c.update(chitu.Status{Read: 5000, Size: 9000, Seconds: 600}, now)
	key := c.State().FilePath
	c.update(chitu.Status{Read: 4900, Size: 9000, Seconds: 602}, now.Add(2*time.Second))
	if got := c.State().FilePath; got != key {
		t.Errorf("a step back of the read position gave a new key: %q, was %q", got, key)
	}
	c.update(chitu.Status{Read: 100, Size: 9000, Seconds: 3}, now.Add(time.Hour))
	if c.State().FilePath == key {
		t.Error("a new print kept the key")
	}
}
