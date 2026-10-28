package timelapse

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// printState is a print for a runner, with a lock.
type printState struct {
	mu sync.Mutex
	p  Print
}

func (s *printState) get() Print  { s.mu.Lock(); defer s.mu.Unlock(); return s.p }
func (s *printState) set(p Print) { s.mu.Lock(); s.p = p; s.mu.Unlock() }

// videoFrames returns the count of frames of the only video in dir.
func videoFrames(t *testing.T, r *Runner) int {
	t.Helper()
	in := r.Info()
	if len(in.Videos) != 1 {
		t.Fatalf("%d videos", len(in.Videos))
	}
	b, err := os.ReadFile(filepath.Join(r.Dir, in.Videos[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	a := map[string][]byte{}
	atoms(t, b, "", a)
	return int(binary.BigEndian.Uint32(a["moov/trak/mdia/minf/stbl/stsz"][8:]))
}

// recordThree runs a print of clip.gcode with 3 frames and a first layer picture, and returns the
// runner, its host and the print state.
func recordThree(t *testing.T) (*Runner, *fakeHost, *printState) {
	host := &fakeHost{st: HostStatus{Present: true, Park: true, Every: 1}}
	ps := &printState{}
	r := NewRunner(Settings{On: true, Park: true, First: FirstPicture}, host, &fakeCamera{t: t, on: true}, ps.get, t.TempDir())
	ctx := context.Background()
	r.step(ctx)
	ps.set(Print{Busy: true, File: "clip.gcode", Known: true})
	r.step(ctx)
	host.frame(true, true, 1)
	r.step(ctx)
	host.frame(false, true, 2)
	r.step(ctx)
	host.frame(false, true, 3)
	r.step(ctx)
	if r.Info().Frames != 3 || r.Info().First == nil {
		t.Fatalf("info %+v", r.Info())
	}
	return r, host, ps
}

// The next deck program continues the video of the print: it has the frames of the old one.
func TestHandoff(t *testing.T) {
	old, host, _ := recordThree(t)
	file := filepath.Join(t.TempDir(), "timelapse.handoff")
	if err := old.SaveHandoff(file); err != nil {
		t.Fatal(err)
	}
	// After the save, the old runner captures no pictures.
	host.frame(false, true, 4)
	old.step(context.Background())
	if len(host.taken) != 3 {
		t.Errorf("the old runner took a picture after the save: %v", host.taken)
	}

	ps := &printState{}
	r := NewRunner(Settings{On: true, Park: true, First: FirstPicture}, host, &fakeCamera{t: t, on: true}, ps.get, t.TempDir())
	if ok, err := r.LoadHandoff(file); !ok || err != nil {
		t.Fatalf("LoadHandoff = %v, %v", ok, err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the handoff file is still there")
	}
	in := r.Info()
	if in.Frames != 3 || in.First == nil || in.FirstFile != "clip.gcode" {
		t.Fatalf("info after the load: %+v", in)
	}
	// The Klipper host does not answer yet: the runner keeps the frames.
	ctx := context.Background()
	r.step(ctx)
	if r.Info().Frames != 3 || len(r.Info().Videos) != 0 {
		t.Fatalf("the runner ended the print with no state: %+v", r.Info())
	}
	// The print continues: frame 4 is the picture that the host waits for.
	ps.set(Print{Busy: true, File: "clip.gcode", Known: true})
	r.step(ctx)
	host.frame(false, true, 5)
	r.step(ctx)
	if r.Info().Frames != 5 {
		t.Fatalf("%d frames, want 5", r.Info().Frames)
	}
	ps.set(Print{Known: true})
	r.step(ctx)
	// 5 frames, the last frame of the print, and the last frame again for one second (5 fps).
	if n := videoFrames(t, r); n != 6+5 {
		t.Errorf("%d frames in the video", n)
	}
}

// A print that ended during the switch: the next deck program writes the video of the frames.
func TestHandoffPrintEnded(t *testing.T) {
	old, host, _ := recordThree(t)
	file := filepath.Join(t.TempDir(), "timelapse.handoff")
	if err := old.SaveHandoff(file); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Settings{On: true, Park: true}, host, &fakeCamera{t: t, on: true}, func() Print { return Print{Known: true} }, t.TempDir())
	if ok, err := r.LoadHandoff(file); !ok || err != nil {
		t.Fatalf("LoadHandoff = %v, %v", ok, err)
	}
	r.step(context.Background())
	if n := videoFrames(t, r); n != 4+5 {
		t.Errorf("%d frames in the video", n)
	}
}

// With no print, there is no file.
func TestHandoffNoPrint(t *testing.T) {
	host := &fakeHost{st: HostStatus{Present: true}}
	r := NewRunner(Settings{On: true}, host, &fakeCamera{t: t, on: true}, func() Print { return Print{Known: true} }, t.TempDir())
	file := filepath.Join(t.TempDir(), "timelapse.handoff")
	os.WriteFile(file, []byte("old"), 0o600)
	if err := r.SaveHandoff(file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("a handoff file with no print")
	}
	if ok, err := r.LoadHandoff(file); ok || err != nil {
		t.Errorf("LoadHandoff of no file = %v, %v", ok, err)
	}
}

// A damaged file is an error, and goes.
func TestHandoffDamaged(t *testing.T) {
	old, _, _ := recordThree(t)
	file := filepath.Join(t.TempDir(), "timelapse.handoff")
	if err := old.SaveHandoff(file); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	os.WriteFile(file, b[:len(b)-10], 0o600)
	r := NewRunner(Settings{On: true}, &fakeHost{}, &fakeCamera{t: t, on: true}, func() Print { return Print{} }, t.TempDir())
	if ok, err := r.LoadHandoff(file); ok || err == nil {
		t.Errorf("LoadHandoff of a short file = %v, %v", ok, err)
	}
	if r.Info().Frames != 0 {
		t.Error("frames from a damaged file")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("the damaged file is still there")
	}
}
