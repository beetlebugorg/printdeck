package printer

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"printdeck/internal/bambu"
	"printdeck/internal/timelapse"
)

// The time lapse of a Bambu Lab printer: the printer makes it, and reports the file of its last
// video (device.cam.timelapse_path). A new file belongs to the print that runs, or to the print
// that ended last. A video on the card of the printer (a USB drive on the X2D) is in /timelapse on
// FTPS: the deck copies it into VideoDir and gives it to the record of the print. A video in the
// memory of the printer (/userdata) is not on FTPS. The player of the deck plays H.264
// (timelapse.Movie).

// The folder of the videos on FTPS, the longest time from the end of a print to its video, the
// count of videos that VideoDir keeps, and the largest video.
const (
	bambuLapseDir    = "/timelapse"
	bambuLapseWindow = 30 * time.Minute
	bambuLapseKeep   = 10
	maxLapseBytes    = 2 << 30
)

// lapseRetry is the time between two tries of a copy: the printer can still write the video.
// A test makes it shorter.
var lapseRetry = 20 * time.Second

// fetchLapse copies the video name of /timelapse on FTPS to the file dst.
var fetchLapse = func(c *bambu.Client, name, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := c.Fetch(ctx, bambuLapseDir+"/"+name, f, maxLapseBytes); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// watchLapse follows the file of the last video of the printer in the report r, at now. The
// first report gives the file from before: it is not new. The caller holds mu.
func (b *Bambu) watchLapse(r bambu.Report, now time.Time) {
	p := r.Device.Cam.TimelapsePath
	if p == "" || p == b.lapsePath {
		return
	}
	first := b.lapsePath == ""
	b.lapsePath = p
	if first || b.VideoDir == "" {
		return
	}
	print, end := b.path, now
	if print == "" {
		if b.endedPath == "" || now.Sub(b.endedAt) > bambuLapseWindow {
			log.Printf("bambu: a time lapse %s of no print", p)
			return
		}
		print, end = b.endedPath, b.endedAt
	}
	if strings.HasPrefix(p, "/userdata/") {
		log.Printf("bambu: the time lapse %s is in the memory of the printer: put a USB drive in the printer for the deck to copy it", p)
		return
	}
	name := path.Base(p)
	b.trayWG.Add(1)
	go func() {
		defer b.trayWG.Done()
		if err := b.copyLapse(print, end, name); err != nil {
			log.Printf("bambu: the time lapse %s of %s: %v", name, print, err)
		}
	}()
}

// copyLapse copies the video name from the card, tries again while the printer writes it, and
// gives it to the record of the print that ended at end.
func (b *Bambu) copyLapse(print string, end time.Time, name string) error {
	if name == "" || name != filepath.Base(name) || !strings.EqualFold(filepath.Ext(name), ".mp4") {
		return fmt.Errorf("not a video: %q", name)
	}
	if err := os.MkdirAll(b.VideoDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(b.VideoDir, name)
	tmp := dst + ".part"
	var err error
	for try := 0; try < 6; try++ {
		if try > 0 {
			time.Sleep(lapseRetry)
		}
		if err = fetchLapse(b.Client, name, tmp); err == nil {
			break
		}
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	log.Printf("bambu: the time lapse %s of %s is in the deck", name, print)
	if b.Hist != nil {
		b.Hist.SetVideo(print, name, end)
	}
	b.pruneLapses()
	return nil
}

// pruneLapses keeps the bambuLapseKeep newest videos of VideoDir.
func (b *Bambu) pruneLapses() {
	v := b.Videos()
	for _, old := range v[min(len(v), bambuLapseKeep):] {
		os.Remove(filepath.Join(b.VideoDir, old.Name))
	}
}

// Videos returns the time lapse videos of the printer in VideoDir, the newest first.
func (b *Bambu) Videos() []timelapse.Video {
	ents, err := os.ReadDir(b.VideoDir)
	if err != nil {
		return nil
	}
	var out []timelapse.Video
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".mp4") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			out = append(out, timelapse.Video{Name: e.Name(), Size: fi.Size(), Time: fi.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// VideoPath returns the path of the video name in VideoDir, and false for a video that is gone.
func (b *Bambu) VideoPath(name string) (string, bool) {
	if b.VideoDir == "" || name != filepath.Base(name) {
		return "", false
	}
	p := filepath.Join(b.VideoDir, name)
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}
