package ui

import (
	"bytes"
	"fmt"
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

// The record page adds a note to a print, and the note shows in the table.
func TestRecordNote(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(idle)
		a.Wide = wide
		if wide {
			cv = gfx.New(a.Screen().W, a.Screen().H)
		}
		end := time.Unix(5000, 0)
		pr.History_ = []printer.Record{{File: "pod.gcode", Path: "pod.gcode", Result: "cancelled", End: end}}
		a.record = pr.History_[0]
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
		simFrame(t, a, "record-no-note")
		tapLabel(t, a, cv, "Note")
		if a.textKB == nil {
			t.Fatalf("wide %v: no keyboard", wide)
		}
		a.textKB.done(" The pegs are loose ")
		if pr.History_[0].Note != "The pegs are loose" {
			t.Fatalf("wide %v: note %q", wide, pr.History_[0].Note)
		}
		f := simFrame(t, a, "record-note")
		if !strings.Contains(f, `"NOTE",1,14`) || !strings.Contains(f, "The pegs are loose") {
			t.Errorf("wide %v: the page shows no note", wide)
		}
	}
}

// Project on the page of a print: a new project by name, an older project from the choice, and
// No project. The history then has the Projects page with the prints of each project.
func TestRecordProject(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(idle)
		a.Wide = wide
		if wide {
			cv = gfx.New(a.Screen().W, a.Screen().H)
		}
		pr.History_ = []printer.Record{
			{File: "lid.gcode", Path: "lid.gcode", Result: "complete", End: time.Unix(6000, 0), Duration: time.Hour, Filament: 3000},
			{File: "pot.gcode", Path: "pot.gcode", Result: "complete", End: time.Unix(5000, 0), Duration: 2 * time.Hour, Filament: 5000},
		}
		a.record = pr.History_[1]
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
		tapLabel(t, a, cv, "Project")
		tapLabel(t, a, cv, "New project…")
		if a.textKB == nil {
			t.Fatalf("wide %v: no keyboard", wide)
		}
		a.textKB.done(" Garden planters ")
		if pr.History_[1].Project != "Garden planters" || a.record.Project != "Garden planters" {
			t.Fatalf("wide %v: project %q", wide, pr.History_[1].Project)
		}
		if f := simFrame(t, a, "record-project"); !strings.Contains(f, "Garden planters") {
			t.Errorf("wide %v: the page shows no project", wide)
		}

		// The other print goes in the same project from the choice.
		a.record = pr.History_[0]
		tapLabel(t, a, cv, "Project")
		tapLabel(t, a, cv, "Garden planters")
		if pr.History_[0].Project != "Garden planters" {
			t.Fatalf("wide %v: project of the lid %q", wide, pr.History_[0].Project)
		}

		// The Projects page, and the prints of the project.
		a.pages = []page{pageMachine, pageStats, pageHistory}
		tapLabel(t, a, cv, "Projects")
		f := simFrame(t, a, fmt.Sprint("projects-", wide))
		if !strings.Contains(f, "Garden planters") || !strings.Contains(f, "2 prints · 3:00") {
			t.Errorf("wide %v: the Projects page: %s", wide, f)
		}
		tapLabel(t, a, cv, "Garden planters")
		if a.pages[len(a.pages)-1] != pageProject || a.project != "Garden planters" {
			t.Fatalf("wide %v: pages %v, project %q", wide, a.pages, a.project)
		}
		simFrame(t, a, fmt.Sprint("project-", wide))

		// No project.
		a.record = pr.History_[0]
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
		tapLabel(t, a, cv, "Project")
		tapLabel(t, a, cv, "No project")
		if pr.History_[0].Project != "" {
			t.Errorf("wide %v: project %q after No project", wide, pr.History_[0].Project)
		}
	}
}

// TestPrintProject puts the print now in a project from the print page. The record gets it at the
// end of the print.
func TestPrintProject(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, cv := newPrintApp(printing)
		a.Wide = wide
		if wide {
			cv = gfx.New(a.Screen().W, a.Screen().H)
		}
		pr.History_ = []printer.Record{{File: "lid.gcode", Path: "lid.gcode", Result: "complete", End: time.Unix(6000, 0),
			Duration: time.Hour, Project: "Garden planters"}}
		tapLabel(t, a, cv, "No project · tap to pick")
		tapLabel(t, a, cv, "Garden planters")
		if pr.Next_ != "Garden planters" {
			t.Fatalf("wide %v: project %q", wide, pr.Next_)
		}
		a.Draw(cv)
		if !hasKey(a, "Project · Garden planters") {
			t.Errorf("wide %v: the print page shows no project", wide)
		}
		simFrame(t, a, fmt.Sprint("print-project-", wide))
		tapLabel(t, a, cv, "Project · Garden planters")
		tapLabel(t, a, cv, "No project")
		a.Draw(cv)
		if pr.Next_ != "" || !hasKey(a, "No project · tap to pick") {
			t.Errorf("wide %v: No project left %q", wide, pr.Next_)
		}
	}
}
