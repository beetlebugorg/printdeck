package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/hostapi"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/system"
	"printdeck/internal/ui"
)

// fakeUpdateSystem is an updateSystem with no helper.
type fakeUpdateSystem struct {
	mu        sync.Mutex
	staged    system.Staged
	hasStaged bool
	stageErr  error
	rebootErr error
	reboots   int
	stages    int
	host      int
	next      int
	exe       string
}

func (f *fakeUpdateSystem) Stage(p system.Progress) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stages++
	if p != nil {
		p(system.StepWrite, 50, 100)
	}
	if f.stageErr != nil {
		return f.stageErr
	}
	f.staged, f.hasStaged = system.Staged{Slot: "b", Version: "v9.1.0", Written: time.Now()}, true
	return nil
}
func (f *fakeUpdateSystem) Staged() (system.Staged, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.staged, f.hasStaged
}
func (f *fakeUpdateSystem) Reboot() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reboots++
	return f.rebootErr
}
func (f *fakeUpdateSystem) HostAPI(context.Context) (int, error) {
	if f.host == 0 {
		return 0, errors.New("no version")
	}
	return f.host, nil
}
func (f *fakeUpdateSystem) NextAPI(context.Context) (int, error) {
	if f.next == 0 {
		return 0, errors.New("no version")
	}
	return f.next, nil
}
func (f *fakeUpdateSystem) NextExe() string  { return f.exe }
func (f *fakeUpdateSystem) NextRoot() string { return filepath.Dir(f.exe) }
func (f *fakeUpdateSystem) count() (stages, reboots int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stages, f.reboots
}

// testUpdater is an updater on a fake system and a printer state that the test sets.
type testUpdater struct {
	*updater
	sys      *fakeUpdateSystem
	mu       sync.Mutex
	st       printer.State
	mach     printer.Machine
	exits    int
	handoffs int
	input    time.Time
}

func (t *testUpdater) setState(st printer.State) { t.mu.Lock(); t.st = st; t.mu.Unlock() }

func newTestUpdater(t *testing.T) *testUpdater {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "printdeck")
	os.WriteFile(exe, []byte("program"), 0o755)
	tu := &testUpdater{sys: &fakeUpdateSystem{host: hostapi.Version, next: hostapi.Version, exe: exe}}
	tu.st = idleState()
	tu.mach = printer.Machine{HostAPI: hostapi.Version}
	dir := t.TempDir()
	u := &updater{stateFile: filepath.Join(dir, "update-channel"), redraw: func() {}, sys: tu.sys,
		handoffDir: dir, started: time.Now()}
	u.state = func() printer.State { tu.mu.Lock(); defer tu.mu.Unlock(); return tu.st }
	u.machine = func() printer.Machine { tu.mu.Lock(); defer tu.mu.Unlock(); return tu.mach }
	u.lastInput = func() time.Time { tu.mu.Lock(); defer tu.mu.Unlock(); return tu.input }
	u.exit = func() { tu.mu.Lock(); tu.exits++; tu.mu.Unlock() }
	u.handoff = func() error { tu.mu.Lock(); tu.handoffs++; tu.mu.Unlock(); return nil }
	tu.updater = u
	return tu
}

// idleState is a printer with no print, the heaters off and a cool bed.
func idleState() printer.State {
	return printer.State{Reachable: true, Connected: true, Bed: 24, Tool: 26}
}

func printingState() printer.State {
	st := idleState()
	st.Printing, st.Bed, st.BedTarget, st.Tool, st.ToolTarget = true, 60, 60, 210, 210
	return st
}

// With no print, an install restarts at once. During a print, it waits.
func TestStageIdleRestarts(t *testing.T) {
	tu := newTestUpdater(t)
	waits, err := tu.stage(nil)
	if err != nil || waits {
		t.Fatalf("stage = %v, %v; want a restart", waits, err)
	}
	if _, reboots := tu.sys.count(); reboots != 1 {
		t.Errorf("%d restarts, want 1", reboots)
	}
	if s := tu.Status(); s.Busy != "Restarting" || !s.HasStaged || s.Staged != "v9.1.0" {
		t.Errorf("status %+v", s)
	}
}

func TestStageDuringPrintWaits(t *testing.T) {
	tu := newTestUpdater(t)
	tu.setState(printingState())
	var steps []string
	waits, err := tu.stage(func(step string, done, total int64) { steps = append(steps, step) })
	if err != nil || !waits {
		t.Fatalf("stage = %v, %v; want a wait", waits, err)
	}
	if _, reboots := tu.sys.count(); reboots != 0 {
		t.Errorf("%d restarts during a print", reboots)
	}
	if len(steps) == 0 {
		t.Error("no progress")
	}
	s := tu.Status()
	if s.Busy != "" || !s.HasStaged || !s.CanSwitch {
		t.Errorf("status %+v", s)
	}
}

// A failed write does not restart.
func TestStageFails(t *testing.T) {
	tu := newTestUpdater(t)
	tu.sys.stageErr = errors.New("The bundle is damaged: rootfs.erofs")
	if _, err := tu.stage(nil); err == nil {
		t.Fatal("no error")
	}
	if _, reboots := tu.sys.count(); reboots != 0 {
		t.Errorf("%d restarts after a failed write", reboots)
	}
	if s := tu.Status(); s.HasStaged || s.Busy != "" {
		t.Errorf("status %+v", s)
	}
}

// The helper refuses the restart (a print started): the update waits.
func TestStageRestartRefused(t *testing.T) {
	tu := newTestUpdater(t)
	tu.sys.rebootErr = errors.New("A print runs.")
	waits, err := tu.stage(nil)
	if err != nil || !waits {
		t.Fatalf("stage = %v, %v", waits, err)
	}
	if s := tu.Status(); s.Busy != "" || !s.HasStaged {
		t.Errorf("status %+v", s)
	}
}

func TestRestartDue(t *testing.T) {
	q := restartQuiet
	hot := idleState()
	hot.Bed = 50
	for _, c := range []struct {
		name   string
		st     func(*printer.State)
		saving bool
		quiet  time.Duration
		want   bool
	}{
		{"idle and cool", nil, false, q, true},
		{"too soon", nil, false, q - time.Second, false},
		{"printing", func(s *printer.State) { s.Printing = true }, false, q, false},
		{"paused", func(s *printer.State) { s.Printing, s.Paused = true, true }, false, q, false},
		{"bed heats", func(s *printer.State) { s.BedTarget = 60 }, false, q, false},
		{"nozzle heats", func(s *printer.State) { s.ToolTarget = 200 }, false, q, false},
		{"action", func(s *printer.State) { s.Action = "Bed tilt" }, false, q, false},
		{"manual probe", func(s *printer.State) { s.ManualProbe = true }, false, q, false},
		{"SD copy", func(s *printer.State) { s.SDCopy.Path = "a.gcode" }, false, q, false},
		{"no host", func(s *printer.State) { s.Reachable = false }, false, q, false},
		{"video", nil, true, q, false},
		{"hot bed", func(s *printer.State) { s.Bed = 50 }, false, q, false},
		{"hot bed for long", func(s *printer.State) { s.Bed = 50 }, false, restartCoolMax, true},
		{"bed at the release temperature", func(s *printer.State) { s.Bed = printer.ReleaseBed }, false, q, true},
	} {
		st := idleState()
		if c.st != nil {
			c.st(&st)
		}
		if got := restartDue(st, c.saving, c.quiet); got != c.want {
			t.Errorf("%s: restartDue = %v, want %v", c.name, got, c.want)
		}
	}
}

// After the print, the deck restarts into the update when the printer is quiet. A touch
// restarts the wait.
func TestWatchRestartsAfterPrint(t *testing.T) {
	tu := newTestUpdater(t)
	tu.setState(printingState())
	if waits, err := tu.stage(nil); !waits || err != nil {
		t.Fatalf("stage = %v, %v", waits, err)
	}
	ctx := context.Background()
	t0 := time.Now()
	tu.step(ctx, t0)
	tu.setState(idleState())
	tu.step(ctx, t0.Add(time.Minute))
	if _, r := tu.sys.count(); r != 0 {
		t.Fatal("a restart one minute after the print")
	}
	tu.mu.Lock()
	tu.input = t0.Add(90 * time.Second)
	tu.mu.Unlock()
	tu.step(ctx, t0.Add(restartQuiet+time.Second))
	if _, r := tu.sys.count(); r != 0 {
		t.Fatal("a restart 30 s after a touch")
	}
	tu.step(ctx, t0.Add(90*time.Second+restartQuiet+time.Second))
	if _, r := tu.sys.count(); r != 1 {
		t.Fatalf("%d restarts, want 1", r)
	}
	// One request only.
	tu.step(ctx, t0.Add(4*time.Minute))
	if _, r := tu.sys.count(); r != 1 {
		t.Errorf("%d restarts, want 1", r)
	}
	// No restart in restartGiveUp: the deck asks again after restartQuiet.
	later := t0.Add(90*time.Second + restartQuiet + time.Second + restartGiveUp + time.Second)
	tu.step(ctx, later)
	if s := tu.Status(); s.Busy != "" {
		t.Errorf("status after the give up %+v", s)
	}
	tu.step(ctx, later.Add(restartQuiet+time.Second))
	if _, r := tu.sys.count(); r != 2 {
		t.Errorf("%d restarts, want 2", r)
	}
}

func TestSwitchCheck(t *testing.T) {
	no := errors.New("no")
	for _, c := range []struct {
		host, next     int
		herr, nerr     error
		want           bool
		noteHasKlipper bool
	}{
		{1, 1, nil, nil, true, false},
		{1, 2, nil, nil, false, true},
		{2, 1, nil, nil, false, true},
		{0, 1, no, nil, false, true},
		{1, 0, nil, no, false, false},
		{1, 1, nil, no, false, false},
	} {
		ok, note := switchCheck(c.host, c.next, c.herr, c.nerr)
		if ok != c.want || (ok && note != "") || (!ok && note == "") {
			t.Errorf("switchCheck(%d, %d, %v, %v) = %v, %q", c.host, c.next, c.herr, c.nerr, ok, note)
		}
		if c.noteHasKlipper && !strings.Contains(note, "Klipper host") {
			t.Errorf("switchCheck(%d, %d): %q", c.host, c.next, note)
		}
	}
}

// The switch saves the state, writes the marker of the next deck program, and ends the deck. The
// marker has a copy of the program in the handoff folder. The deck program of the update can
// install again and switch to the new install.
func TestSwitchDeck(t *testing.T) {
	tu := newTestUpdater(t)
	release := filepath.Join(tu.sys.NextRoot(), "usr/lib/os-release")
	os.MkdirAll(filepath.Dir(release), 0o755)
	os.WriteFile(release, []byte("PRINTDECK_VERSION=v9.1.0\n"), 0o644)
	tu.setState(printingState())
	tu.stage(nil)
	if err := tu.switchDeck(); err != nil {
		t.Fatal(err)
	}
	if tu.exits != 1 || tu.handoffs != 1 {
		t.Errorf("%d exits, %d handoffs", tu.exits, tu.handoffs)
	}
	n, err := readNext(tu.handoffDir)
	if err != nil || n.Version != "v9.1.0" || n.Starts != 0 || filepath.Dir(n.Root) != tu.handoffDir {
		t.Fatalf("marker %+v, %v", n, err)
	}
	want, _ := os.ReadFile(tu.sys.exe)
	if got, _ := os.ReadFile(n.Exe); len(want) == 0 || !bytes.Equal(got, want) {
		t.Errorf("the copy of the program has %d bytes, want %d", len(got), len(want))
	}
	if releaseVersion(filepath.Join(n.Root, "usr/lib/os-release")) != "v9.1.0" {
		t.Error("no os-release in the copy")
	}

	// The deck program of the update: no switch to itself, and a switch to a new install.
	t.Setenv(envNextRoot, n.Root)
	t.Setenv(envNextWritten, n.Written.UTC().Format(time.RFC3339Nano))
	tu.refresh(context.Background())
	if err := tu.switchDeck(); err == nil {
		t.Fatal("a switch to the program that runs")
	}
	time.Sleep(time.Millisecond)
	if _, err := tu.stage(nil); err != nil {
		t.Fatal(err)
	}
	if err := tu.switchDeck(); err != nil {
		t.Fatal(err)
	}
	n2, err := readNext(tu.handoffDir)
	if err != nil || n2.Root == n.Root || !n2.Written.After(n.Written) {
		t.Fatalf("marker %+v after %+v, %v", n2, n, err)
	}
	if _, err := os.Stat(n.Root); !os.IsNotExist(err) {
		t.Error("the copy of the first switch stays")
	}
}

// With another interface version, the deck does not switch, and waits for the restart.
func TestSwitchDeckIncompatible(t *testing.T) {
	tu := newTestUpdater(t)
	tu.sys.next = hostapi.Version + 1
	tu.setState(printingState())
	tu.stage(nil)
	if s := tu.Status(); s.CanSwitch || s.SwitchNote == "" {
		t.Errorf("status %+v", s)
	}
	if err := tu.switchDeck(); err == nil {
		t.Fatal("a switch to a deck program of another interface version")
	}
	if tu.exits != 0 || tu.handoffs != 0 {
		t.Errorf("%d exits, %d handoffs", tu.exits, tu.handoffs)
	}
	if _, err := readNext(tu.handoffDir); err == nil {
		t.Error("a marker")
	}
	// A Klipper host with no version (an earlier one): no switch either.
	tu.sys.next, tu.sys.host = hostapi.Version, 0
	if err := tu.switchDeck(); err == nil {
		t.Fatal("a switch with a Klipper host of no version")
	}
}

// After a failed switch, the deck does not offer it again.
func TestSwitchDeckAfterFailure(t *testing.T) {
	tu := newTestUpdater(t)
	tu.setState(printingState())
	tu.stage(nil)
	os.WriteFile(filepath.Join(tu.handoffDir, failedFile), []byte("{}"), 0o600)
	tu.refresh(context.Background())
	if s := tu.Status(); s.CanSwitch || !s.SwitchFailed {
		t.Errorf("status %+v", s)
	}
	if err := tu.switchDeck(); err == nil || tu.exits != 0 {
		t.Errorf("switch after a failure: %v, %d exits", err, tu.exits)
	}
}

// The deck program of the running system runs the one of the update in its place, up to
// maxNextStarts starts with no stable run.
func TestNextToRun(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "printdeck")
	os.WriteFile(exe, []byte("program"), 0o755)
	if _, ok := nextToRun(dir); ok {
		t.Fatal("a program to run with no marker")
	}
	writeNext(dir, nextDeck{Exe: exe, Root: dir, Version: "v9.1.0"})
	for i := 1; i <= maxNextStarts; i++ {
		n, ok := nextToRun(dir)
		if !ok || n.Starts != i {
			t.Fatalf("start %d: %+v, %v", i, n, ok)
		}
	}
	if _, ok := nextToRun(dir); ok {
		t.Fatal("a program to run after maxNextStarts starts")
	}
	if !nextFailed(dir) {
		t.Error("no record of the failure")
	}

	// A stable run sets the count to 0.
	dir = t.TempDir()
	writeNext(dir, nextDeck{Exe: exe, Root: dir, Starts: 2})
	markNextStable(dir)
	if n, ok := nextToRun(dir); !ok || n.Starts != 1 {
		t.Errorf("after a stable run: %+v, %v", n, ok)
	}

	// A missing program, and the deck program of the update itself.
	dir = t.TempDir()
	writeNext(dir, nextDeck{Exe: filepath.Join(dir, "none"), Root: dir})
	if _, ok := nextToRun(dir); ok || !nextFailed(dir) {
		t.Error("a missing program runs")
	}
	dir = t.TempDir()
	writeNext(dir, nextDeck{Exe: exe, Root: dir})
	t.Setenv(envNextRoot, dir)
	if _, ok := nextToRun(dir); ok {
		t.Error("the deck program of the update runs itself again")
	}
}

// In the deck program of an update, a Klipper host of another interface version ends the
// switch: the marker goes, and the deck program ends.
func TestCheckHost(t *testing.T) {
	tu := newTestUpdater(t)
	writeNext(tu.handoffDir, nextDeck{Exe: tu.sys.exe, Root: "/", Version: "v9.1.0"})
	tu.checkHost(idleState())
	if tu.exits != 0 {
		t.Fatal("an exit with the same version")
	}
	tu.mach.HostAPI = 0
	tu.checkHost(idleState())
	if tu.exits != 1 || !nextFailed(tu.handoffDir) {
		t.Errorf("%d exits, failed %v", tu.exits, nextFailed(tu.handoffDir))
	}
}

// testPrinter is a demo printer with Run.
type testPrinter struct{ *ui.DemoPrinter }

func (testPrinter) Run(ctx context.Context, notify func()) {}

// The answer of POST /system/update with progress stays {"error":""} with no print, and has
// "staged" during a print.
func TestSystemUpdateAnswer(t *testing.T) {
	for _, printing := range []bool{false, true} {
		tu := newTestUpdater(t)
		st := idleState()
		if printing {
			st = printingState()
		}
		tu.setState(st)
		pr := testPrinter{&ui.DemoPrinter{State_: st}}
		system.UpdateBundle = filepath.Join(t.TempDir(), "update", "printdeck-update.tar.xz")
		rs := remote.New()
		rs.NoLogin = true
		mux := http.NewServeMux()
		registerSystemPage(mux, rs, pr, tu.updater, nil, "", "")
		var body strings.Builder
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("bundle", "printdeck-update.tar.xz")
		io.WriteString(fw, "bundle")
		mw.Close()
		req := httptest.NewRequest("POST", "/system/update", strings.NewReader(body.String()))
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Printdeck-Progress", "1")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		want := `{"error":""}`
		if printing {
			want = `{"error":"","staged":true}`
		}
		if got := strings.TrimSpace(w.Body.String()); got != want {
			t.Errorf("printing %v: %s, want %s", printing, got, want)
		}
		// Wait for the install in the background.
		for range 200 {
			installing.Lock()
			done := !installing.Running
			installing.Unlock()
			if done {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		installing.Lock()
		s := installing.installState
		installing.Unlock()
		if !s.Finished || s.Staged != printing {
			t.Errorf("printing %v: install state %+v", printing, s)
		}
	}
}
