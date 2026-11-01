package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The video of the time lapse of a print. The deck uploads the frames of the time lapse as JPEG
// files, lapse/<n>.jpg. When the print is done, a job (Kind "lapse") encodes them to one H.264
// video, lapse.mp4, keeps the last frame as lapse.jpg for the poster of the video, and deletes
// the frames. The print record keeps Frames and FPS, and Video is true.
//
//	x/<id>/prints/<print>/lapse.mp4  the video of the time lapse
//	x/<id>/prints/<print>/lapse.jpg  the last frame, the poster of the video

// LapseEncoder writes the MP4 video out of the frames 0.jpg, 1.jpg, ... in the folder dir, at
// fps frames a second.
type LapseEncoder func(ctx context.Context, dir string, fps int, out string) error

// The limits of a video. A video out of these limits is a failure of the encode: the frames stay.
const (
	minLapseVideo = 1 << 10
	maxLapseVideo = 256 << 20
)

// lapseJobTime is the most time of a lapse job. The template gives the function 180 s. A lapse of
// 2000 frames (MaxLapseFrames) at 1024 MB needs up to about 60 s. This is an estimate from a test
// on x86.
const lapseJobTime = 170 * time.Second

// lapseCache is the Cache-Control of the video and its poster: the same as the frames.
const lapseCache = "private, max-age=31536000, immutable"

// FFmpeg returns a LapseEncoder that runs the ffmpeg program at bin. In a test of 400 frames, the
// preset medium made a video 5% larger than the preset slow, with the same SSIM, in 80% of the
// time.
func FFmpeg(bin string) LapseEncoder {
	return func(ctx context.Context, dir string, fps int, out string) error {
		cmd := exec.CommandContext(ctx, bin, "-nostdin", "-loglevel", "error", "-y",
			"-framerate", strconv.Itoa(fps), "-i", filepath.Join(dir, "%d.jpg"),
			// H.264 with 4:2:0 needs an even width and height. The JPEG frames have the full
			// range of levels, and a video for the web has the limited range.
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2:out_range=tv,format=yuv420p",
			"-c:v", "libx264", "-preset", "medium", "-crf", "28", "-pix_fmt", "yuv420p",
			"-movflags", "+faststart", out)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ffmpeg: %v: %s", err, clip(stderr.String(), 300))
		}
		return nil
	}
}

// startLapse starts the job that makes the video of the print, after the request. With no
// s.Later, the job runs in the request. With no s.Lapse, the print keeps its frames.
func (s *Server) startLapse(ctx context.Context, project, print string) {
	if s.Lapse == nil {
		return
	}
	if s.later(ctx, Job{Kind: "lapse", Project: project, Print: print}) {
		return
	}
	if err := s.LapseVideo(ctx, project, print); err != nil {
		log.Printf("lapse %s/%s: %v", project, print, err)
	}
}

// errLapseChanged is the error of a lapse job when the deck sent the print again during the job.
var errLapseChanged = errors.New("the print changed during the job")

// LapseVideo makes the video of the time lapse of the print, and then deletes the frames. It
// deletes the frames only after the video and the record with Video are written. For a print
// with a video, it deletes the frames that are left. pwf lapse-backfill uses it for the old
// prints.
func (s *Server) LapseVideo(ctx context.Context, project, print string) error {
	if s.Lapse == nil {
		return errors.New("no encoder")
	}
	pr, err := s.loadPrint(ctx, project, print)
	if err != nil {
		return err
	}
	if !pr.Done || pr.Frames == 0 {
		return nil
	}
	base := "x/" + project + "/prints/" + print + "/"
	keys, err := s.Store.List(ctx, base)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	var frames []string
	for _, k := range keys {
		rel := strings.TrimPrefix(k, base)
		have[rel] = true
		if strings.HasPrefix(rel, "lapse/") {
			frames = append(frames, k)
		}
	}
	all := true
	for n := range pr.Frames {
		all = all && have["lapse/"+strconv.Itoa(n)+".jpg"]
	}
	switch {
	case pr.Video && len(frames) == 0:
		return nil
	case pr.Video:
	case all:
		if err := s.encodeLapse(ctx, base, pr); err != nil {
			return err
		}
		if err := s.setVideo(ctx, project, print, pr.Added); err != nil {
			return err
		}
	case have["lapse.mp4"] && have["lapse.jpg"]:
		// An earlier job wrote the video, and another write of the record reset Video to false.
		if err := s.setVideo(ctx, project, print, pr.Added); err != nil {
			return err
		}
	default:
		return errors.New("frames are missing")
	}
	// The deck can send the print again during the job: its new frames stay.
	if now, err := s.loadPrint(ctx, project, print); err != nil || !now.Added.Equal(pr.Added) {
		return errLapseChanged
	}
	if err := forAll(len(frames), func(i int) error { return s.Store.Delete(ctx, frames[i]) }); err != nil {
		return err
	}
	// A write of the record during the deletes can reset Video to false: set it again.
	if err := s.setVideo(ctx, project, print, pr.Added); err != nil {
		return err
	}
	p, _, err := s.loadProject(ctx, project)
	if err != nil {
		return err
	}
	return s.publish(ctx, p)
}

// encodeLapse writes lapse.mp4 and lapse.jpg of the print pr in the folder base, from its frames.
func (s *Server) encodeLapse(ctx context.Context, base string, pr *PrintRecord) error {
	dir, err := os.MkdirTemp("", "lapse")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := forAll(pr.Frames, func(n int) error {
		b, _, err := s.Store.Get(ctx, base+"lapse/"+strconv.Itoa(n)+".jpg")
		if err != nil {
			return fmt.Errorf("frame %d: %w", n, err)
		}
		return os.WriteFile(filepath.Join(dir, strconv.Itoa(n)+".jpg"), b, 0o600)
	}); err != nil {
		return err
	}
	out := filepath.Join(dir, "lapse.mp4")
	if err := s.Lapse(ctx, dir, max(1, pr.FPS), out); err != nil {
		return err
	}
	video, err := os.ReadFile(out)
	if err != nil {
		return err
	}
	// An MP4 file starts with the box ftyp.
	if len(video) < minLapseVideo || len(video) > maxLapseVideo || string(video[4:8]) != "ftyp" {
		return fmt.Errorf("a bad video of %d bytes", len(video))
	}
	if _, err := s.Store.Put(ctx, base+"lapse.mp4", video, PutOptions{ContentType: "video/mp4", CacheControl: lapseCache}); err != nil {
		return err
	}
	last := base + "lapse/" + strconv.Itoa(pr.Frames-1) + ".jpg"
	return s.Store.Copy(ctx, last, base+"lapse.jpg", PutOptions{ContentType: "image/jpeg", CacheControl: lapseCache})
}

// setVideo sets Video in the record of the print, when the record is of the upload at added.
// It returns errLapseChanged when the deck sent the print again.
func (s *Server) setVideo(ctx context.Context, project, print string, added time.Time) error {
	key := printKey(project, print)
	for range 5 {
		b, etag, err := s.Store.Get(ctx, key)
		if err != nil {
			return err
		}
		var pr PrintRecord
		if err := json.Unmarshal(b, &pr); err != nil {
			return err
		}
		if !pr.Added.Equal(added) {
			return errLapseChanged
		}
		if pr.Video {
			return nil
		}
		pr.Video = true
		data, err := json.Marshal(pr)
		if err != nil {
			return err
		}
		_, err = s.Store.Put(ctx, key, data, PutOptions{ContentType: "application/json", IfMatch: etag})
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return ErrConflict
}

// LapsePrints returns the IDs of the prints of the project that are done and have frames and no
// video, for pwf lapse-backfill.
func LapsePrints(ctx context.Context, st Store, project string) ([]string, error) {
	files, _, err := st.ListDir(ctx, "x/"+project+"/prints/")
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, k := range files {
		if strings.HasSuffix(k, ".json") {
			keys = append(keys, k)
		}
	}
	var out []string
	for _, b := range (&Server{Store: st}).getAll(ctx, keys) {
		var pr PrintRecord
		if b != nil && json.Unmarshal(b, &pr) == nil && pr.Done && pr.Frames > 0 && !pr.Video {
			out = append(out, pr.ID)
		}
	}
	return out, nil
}
