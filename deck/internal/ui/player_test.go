package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// The player plays at the rate of the video, stops at the last frame, and starts again at the
// first frame. A seek goes on from the new frame.
func TestPlayerState(t *testing.T) {
	t0 := time.Unix(1000, 0)
	p := &lapsePlayer{n: 20, fps: 10}
	p.play(t0)
	p.update(t0.Add(550 * time.Millisecond))
	if p.pos != 5 || !p.playing {
		t.Fatalf("frame %d, playing %v", p.pos, p.playing)
	}
	if w := p.wait(t0.Add(550 * time.Millisecond)); w != 50*time.Millisecond {
		t.Errorf("wait %v, want 50ms", w)
	}
	p.pause(t0.Add(720 * time.Millisecond))
	if p.pos != 7 || p.playing {
		t.Fatalf("pause: frame %d, playing %v", p.pos, p.playing)
	}
	p.update(t0.Add(5 * time.Second))
	if p.pos != 7 {
		t.Errorf("the paused video moved to frame %d", p.pos)
	}
	p.play(t0.Add(5 * time.Second))
	p.seek(0.5, t0.Add(6*time.Second))
	p.update(t0.Add(6*time.Second + 200*time.Millisecond))
	if p.pos != 12 {
		t.Errorf("seek: frame %d, want 12", p.pos)
	}
	p.update(t0.Add(time.Minute))
	if p.pos != 19 || p.playing {
		t.Errorf("end: frame %d, playing %v", p.pos, p.playing)
	}
	p.play(t0.Add(time.Minute))
	if p.pos != 0 || !p.playing {
		t.Errorf("play after the end: frame %d", p.pos)
	}
	p.seek(2, t0)
	if p.pos != 19 || p.frac() != 1 {
		t.Errorf("seek past the end: frame %d", p.pos)
	}
}

// writeVideo writes a video of n frames of w x h to dir, and returns its name.
func writeVideo(t *testing.T, dir string, n, w, h int) string {
	t.Helper()
	r := timelapse.NewRecorder()
	for i := range n {
		img := image.NewGray(image.Rect(0, 0, w, h))
		for k := range img.Pix {
			img.Pix[k] = uint8(i * 9)
		}
		var b bytes.Buffer
		jpeg.Encode(&b, img, nil)
		r.Add(b.Bytes(), image.Pt(w, h), i+1)
	}
	var b bytes.Buffer
	if err := r.WriteVideo(&b); err != nil {
		t.Fatal(err)
	}
	name := "clip-20261027-1432.mov"
	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

// The record of a print with a video has the still and Play. Play opens the player, which plays
// the video. Back closes the file. A removed video shows as Removed.
func TestRecordVideo(t *testing.T) {
	for _, wide := range []bool{false, true} {
		dir := t.TempDir()
		name := writeVideo(t, dir, 24, 128, 72)
		a, pr, cv := newPrintApp(idle)
		a.Wide = wide
		if wide {
			cv = gfx.New(a.Screen().W, a.Screen().H)
		}
		now := time.Unix(5000, 0)
		a.Now = func() time.Time { return now }
		tl := &fakeTimeLapse{dir: dir, in: timelapse.Info{Videos: []timelapse.Video{{Name: name}}}}
		a.TimeLapse = tl
		pr.History_ = []printer.Record{{File: "clip.gcode", Path: "clip.gcode", Result: "complete", Video: name}}
		a.record = pr.History_[0]
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
		f := simFrame(t, a, "record-video")
		line := "0:06 · 29 frames"
		if wide {
			line = `"0:06"` // the narrow table has the length only
		}
		if !strings.Contains(f, "TIME LAPSE") || !strings.Contains(f, line) {
			t.Fatalf("wide %v: no time lapse on the record page", wide)
		}
		tapLabel(t, a, cv, "Play")
		if a.topPage() != pagePlayer || a.player == nil {
			t.Fatalf("page %v, want the player", a.topPage())
		}
		simFrame(t, a, "player")
		if a.player.mov == nil || !a.player.playing || a.player.n != 29 {
			t.Fatalf("player %+v", a.player)
		}
		now = now.Add(time.Second)
		if f := simFrame(t, a, "player-1s"); !strings.Contains(f, "Frame 6 of 29") {
			t.Errorf("wide %v: after a second the player is not at frame 6", wide)
		}
		tapLabel(t, a, cv, "Pause")
		if a.player.playing {
			t.Error("Pause does not stop the video")
		}
		tapLabel(t, a, cv, "Back")
		a.Draw(cv)
		if a.topPage() != pageRecord || a.player.mov != nil {
			t.Errorf("page %v; the file is open after Back", a.topPage())
		}

		// The deck removed the video.
		tl.in.Videos = nil
		if f := simFrame(t, a, "record-removed"); !strings.Contains(f, "Removed") {
			t.Error("no Removed for a removed video")
		}
	}
}

// The done screen shows Saving while the deck writes the video, and then Time lapse.
func TestDoneVideo(t *testing.T) {
	dir := t.TempDir()
	name := writeVideo(t, dir, 8, 64, 36)
	st := printer.State{Reachable: true, Connected: true, Text: "Ready", Finished: "complete",
		FinishedFile: "clip.gcode", FinishedPath: "clip.gcode", FinishedTime: time.Hour}
	a, pr, cv := newPrintApp(st)
	tl := &fakeTimeLapse{dir: dir, in: timelapse.Info{Saving: true, Frames: 8}}
	a.TimeLapse = tl
	pr.History_ = []printer.Record{{File: "clip.gcode", Path: "clip.gcode", Result: "complete", Duration: time.Hour}}
	if f := simFrame(t, a, "done-saving"); !strings.Contains(f, "SAVING") {
		t.Fatal("no Saving key on the done screen")
	}
	tl.in = timelapse.Info{Videos: []timelapse.Video{{Name: name}}}
	pr.History_[0].Video = name
	tapLabel(t, a, cv, "Time lapse")
	if a.topPage() != pagePlayer {
		t.Fatalf("page %v, want the player", a.topPage())
	}
	tapLabel(t, a, cv, "Back")
	a.Draw(cv)
	if len(a.pages) != 0 || a.player.mov != nil {
		t.Error("Back does not go to the done screen")
	}
	// No time lapse: Reprint and Done only.
	pr.History_[0].Video = ""
	tl.in = timelapse.Info{}
	if f := simFrame(t, a, "done"); strings.Contains(f, "TIME LAPSE") || strings.Contains(f, "SAVING") {
		t.Error("a Time lapse key with no video")
	}
}
