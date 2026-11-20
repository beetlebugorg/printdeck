package printer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/bambu"
)

// The time lapse of a print: the printer reports a new video file after the end of the print. The
// deck copies that file from /timelapse on the card, and the record of the print gets it. The file
// of the first report, and a video in the memory of the printer, are not copied.
func TestBambuTimelapse(t *testing.T) {
	dir := t.TempDir()
	var asked []string
	defer func(f func(*bambu.Client, string, string) error, d time.Duration) { fetchLapse, lapseRetry = f, d }(fetchLapse, lapseRetry)
	lapseRetry = time.Millisecond
	fetchLapse = func(_ *bambu.Client, name, dst string) error {
		asked = append(asked, name)
		return os.WriteFile(dst, []byte("mp4"), 0o644)
	}
	hist := &History{Path: filepath.Join(dir, "history.json")}
	b := NewBambu("192.168.1.50", "S", "C", "", "X2D", hist)
	b.VideoDir = filepath.Join(dir, "videos")
	t.Cleanup(b.trayWG.Wait)
	t0 := time.Date(2026, 11, 19, 20, 0, 0, 0, time.UTC)
	rep := func(state, lapse string) bambu.Report {
		r := bambu.Report{State: state, Subtask: "Panda", TaskID: "7"}
		r.Device.Cam.TimelapsePath = lapse
		return r
	}
	b.update(rep("IDLE", "/userdata/media/timelapse/video_old.mp4"), t0)
	b.update(rep("RUNNING", "/userdata/media/timelapse/video_old.mp4"), t0.Add(time.Minute))
	b.update(rep("FINISH", "/userdata/media/timelapse/video_old.mp4"), t0.Add(40*time.Minute))
	b.update(rep("FINISH", "/mnt/sdcard/timelapse/video_2026-11-20_01-41-00.mp4"), t0.Add(41*time.Minute))
	b.trayWG.Wait()
	if len(asked) != 1 || asked[0] != "video_2026-11-20_01-41-00.mp4" {
		t.Fatalf("copied %v", asked)
	}
	recs := hist.Records()
	if len(recs) != 1 || recs[0].Video != "video_2026-11-20_01-41-00.mp4" {
		t.Fatalf("records %+v", recs)
	}
	if p, ok := b.VideoPath(recs[0].Video); !ok || filepath.Dir(p) != b.VideoDir {
		t.Errorf("video path %q %v", p, ok)
	}
	if v := b.Videos(); len(v) != 1 {
		t.Errorf("videos %+v", v)
	}
	// A video in the memory of the printer is not on FTPS.
	b.update(rep("FINISH", "/userdata/media/timelapse/video_new.mp4"), t0.Add(42*time.Minute))
	b.trayWG.Wait()
	if len(asked) != 1 {
		t.Errorf("copied a video of the memory: %v", asked)
	}
}
