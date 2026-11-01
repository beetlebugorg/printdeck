package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// fakeEncoder is a LapseEncoder for the tests: it reads the frames in order, and writes a file
// that starts as an MP4 file does.
type fakeEncoder struct {
	frames []string
	fps    int
	err    error
	size   int
}

func (f *fakeEncoder) encode(_ context.Context, dir string, fps int, out string) error {
	f.frames, f.fps = nil, fps
	for n := 0; ; n++ {
		b, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(n)+".jpg"))
		if err != nil {
			break
		}
		f.frames = append(f.frames, string(b))
	}
	if f.err != nil {
		return f.err
	}
	size := f.size
	if size == 0 {
		size = 4 << 10
	}
	video := make([]byte, size)
	copy(video, "\x00\x00\x00\x20ftypisom")
	return os.WriteFile(out, video, 0o600)
}

// lapsePrint sends a print with n frames to the project, and returns its ID.
func lapsePrint(t *testing.T, c *cloud.Client, id *cloud.Identity, project, key string, n int) string {
	t.Helper()
	up := cloud.PrintUpload{Model: []byte("model"), Frames: n, FPS: 12,
		Frame:   func(i int) ([]byte, error) { return []byte(fmt.Sprintf("frame %d", i)), nil },
		Request: cloud.PrintRequest{Name: "Hook", Result: "complete", Ended: time.Now().UTC(), Seconds: 600, Key: key}}
	pid, err := c.SendPrint(context.Background(), id, project, up)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// The job of a done print makes the video and the poster, and deletes the frames. The pages get
// the video, and the public page has copies of the video and the poster.
func TestLapseVideo(t *testing.T) {
	d, base := devService(t)
	ctx := context.Background()
	d.Store.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
	enc := &fakeEncoder{}
	d.API.Lapse = enc.encode
	l := connectedLink(t, d, base)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Shop wall")
	if err != nil {
		t.Fatal(err)
	}
	pid := lapsePrint(t, &c, id, sum.ID, "hook@1", 3)
	dir := "x/" + sum.ID + "/prints/" + pid + "/"
	if strings.Join(enc.frames, ",") != "frame 0,frame 1,frame 2" || enc.fps != 12 {
		t.Fatalf("the encoder got %q at %d fps", enc.frames, enc.fps)
	}
	if keys, _ := d.Store.List(ctx, dir+"lapse/"); len(keys) != 0 {
		t.Errorf("the frames after the video: %v", keys)
	}
	if ct, cc := d.Store.ContentType(dir+"lapse.mp4"), d.Store.CacheControl(dir+"lapse.mp4"); ct != "video/mp4" || cc != lapseCache {
		t.Errorf("the video: %q %q", ct, cc)
	}
	if b, _, err := d.Store.Get(ctx, dir+"lapse.jpg"); err != nil || string(b) != "frame 2" {
		t.Errorf("the poster: %q %v", b, err)
	}
	pr, err := d.API.loadPrint(ctx, sum.ID, pid)
	if err != nil || !pr.Video || pr.Frames != 3 || pr.FPS != 12 {
		t.Fatalf("the record %+v %v", pr, err)
	}

	// The view and the frames endpoint give the video and the poster.
	var v cloud.ProjectView
	c.Do(ctx, id, "GET", "/api/v1/projects/"+sum.ID, nil, &v)
	if len(v.Prints) != 1 || v.Prints[0].Video == "" || v.Prints[0].Poster == "" || v.Prints[0].Frames != 3 {
		t.Fatalf("prints %+v", v.Prints)
	}
	if code, b := get(t, v.Prints[0].Video); code != 200 || len(b) != 4<<10 {
		t.Errorf("the video of the view: %d %d bytes", code, len(b))
	}
	var fr cloud.FramesReply
	if err := c.Do(ctx, id, "GET", "/api/v1/projects/"+sum.ID+"/prints/"+pid+"/frames", nil, &fr); err != nil {
		t.Fatal(err)
	}
	if fr.Video == "" || fr.Poster == "" || len(fr.URLs) != 0 || fr.FPS != 12 {
		t.Errorf("frames %+v", fr)
	}
	if code, b := get(t, fr.Poster); code != 200 || string(b) != "frame 2" {
		t.Errorf("the poster of the frames endpoint: %d %q", code, b)
	}

	// A second done of the print keeps the count of the frames.
	if err := c.Do(ctx, id, "POST", "/api/v1/projects/"+sum.ID+"/prints/"+pid+"/done", cloud.PrintDone{Frames: 3, FPS: 12}, nil); err != nil {
		t.Fatal(err)
	}
	if pr, _ := d.API.loadPrint(ctx, sum.ID, pid); !pr.Video || pr.Frames != 3 {
		t.Errorf("the record after a second done %+v", pr)
	}

	// The public page copies the video and the poster, and no frames.
	if _, err := d.API.setPublic(ctx, sum.ID, cloud.PublicRequest{On: true}); err != nil {
		t.Fatal(err)
	}
	p, _, _ := d.API.loadProject(ctx, sum.ID)
	pub := base + "/p/" + p.Public + "/"
	_, pj := get(t, pub+"page.json")
	var pv cloud.ProjectView
	json.Unmarshal(pj, &pv)
	if len(pv.Prints) != 1 || pv.Prints[0].Video != "prints/"+pid+"/lapse.mp4" || pv.Prints[0].Poster != "prints/"+pid+"/lapse.jpg" {
		t.Fatalf("public page.json %s", pj)
	}
	if ct := d.Store.ContentType("p/" + p.Public + "/" + pv.Prints[0].Video); ct != "video/mp4" {
		t.Errorf("the public video: %q", ct)
	}
	if keys, _ := d.Store.List(ctx, "p/"+p.Public+"/prints/"+pid+"/lapse/"); len(keys) != 0 {
		t.Errorf("public frames: %v", keys)
	}
	// A browser asks for a range of a video.
	req, _ := http.NewRequest("GET", pub+pv.Prints[0].Video, nil)
	req.Header.Set("Range", "bytes=0-99")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resp.ContentLength != 100 {
		t.Errorf("a range of the public video: %d %d", resp.StatusCode, resp.ContentLength)
	}

	// The delete of the project deletes the video and the poster.
	d.API.deleteProject(ctx, p)
	if keys, _ := d.Store.List(ctx, "x/"+sum.ID+"/"); len(keys) != 0 {
		t.Errorf("files after the delete: %v", keys)
	}
}

// A failed encode and a bad video keep the frames, and the pages play the frames.
func TestLapseVideoFails(t *testing.T) {
	for _, enc := range []*fakeEncoder{{err: errors.New("no encode")}, {size: 100}} {
		d, base := devService(t)
		ctx := context.Background()
		d.API.Lapse = enc.encode
		l := connectedLink(t, d, base)
		id := l.Identity()
		var c cloud.Client
		sum, err := c.NewProject(ctx, id, "Shop wall")
		if err != nil {
			t.Fatal(err)
		}
		pid := lapsePrint(t, &c, id, sum.ID, "hook@1", 2)
		dir := "x/" + sum.ID + "/prints/" + pid + "/"
		if keys, _ := d.Store.List(ctx, dir+"lapse/"); len(keys) != 2 {
			t.Errorf("the frames after a failed video: %v", keys)
		}
		if _, _, err := d.Store.Get(ctx, dir+"lapse.mp4"); !errors.Is(err, ErrNotFound) {
			t.Errorf("a bad video is up: %v", err)
		}
		var fr cloud.FramesReply
		c.Do(ctx, id, "GET", "/api/v1/projects/"+sum.ID+"/prints/"+pid+"/frames", nil, &fr)
		if fr.Video != "" || len(fr.URLs) != 2 {
			t.Errorf("frames %+v", fr)
		}
		if prints, err := LapsePrints(ctx, d.Store, sum.ID); err != nil || len(prints) != 1 || prints[0] != pid {
			t.Errorf("the prints for the backfill: %v %v", prints, err)
		}
	}
}

// With Later, the job runs after the request: the frames stay until RunJob. With no encoder, the
// print keeps its frames.
func TestLapseJob(t *testing.T) {
	d, base := devService(t)
	ctx := context.Background()
	var jobs []Job
	d.API.Later = func(_ context.Context, job Job) error {
		jobs = append(jobs, job)
		return nil
	}
	l := connectedLink(t, d, base)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Shop wall")
	if err != nil {
		t.Fatal(err)
	}
	old := lapsePrint(t, &c, id, sum.ID, "old@1", 2)
	if len(jobs) != 0 {
		t.Fatalf("a job with no encoder: %+v", jobs)
	}
	enc := &fakeEncoder{}
	d.API.Lapse = enc.encode
	pid := lapsePrint(t, &c, id, sum.ID, "hook@1", 2)
	if len(jobs) != 1 || jobs[0] != (Job{Kind: "lapse", Project: sum.ID, Print: pid}) {
		t.Fatalf("jobs %+v", jobs)
	}
	dir := "x/" + sum.ID + "/prints/" + pid + "/"
	if keys, _ := d.Store.List(ctx, dir+"lapse/"); len(keys) != 2 {
		t.Errorf("the frames before the job: %v", keys)
	}
	d.API.RunJob(ctx, jobs[0])
	if keys, _ := d.Store.List(ctx, dir+"lapse/"); len(keys) != 0 {
		t.Errorf("the frames after the job: %v", keys)
	}
	if pr, _ := d.API.loadPrint(ctx, sum.ID, pid); !pr.Video {
		t.Errorf("the record after the job %+v", pr)
	}
	// The backfill finds the print from before the encoder.
	if prints, err := LapsePrints(ctx, d.Store, sum.ID); err != nil || len(prints) != 1 || prints[0] != old {
		t.Errorf("the prints for the backfill: %v %v", prints, err)
	}
	if err := d.API.LapseVideo(ctx, sum.ID, old); err != nil {
		t.Fatal(err)
	}
	if prints, _ := LapsePrints(ctx, d.Store, sum.ID); len(prints) != 0 {
		t.Errorf("the prints after the backfill: %v", prints)
	}
}

// FFmpeg makes an MP4 file of JPEG frames with an odd width and height. The test needs ffmpeg on
// the PATH.
func TestFFmpeg(t *testing.T) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	for n := range 5 {
		img := image.NewRGBA(image.Rect(0, 0, 65, 37))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{uint8(n * 50), 90, 30, 255}}, image.Point{}, draw.Src)
		var buf bytes.Buffer
		jpeg.Encode(&buf, img, nil)
		os.WriteFile(filepath.Join(dir, strconv.Itoa(n)+".jpg"), buf.Bytes(), 0o600)
	}
	out := filepath.Join(dir, "lapse.mp4")
	if err := FFmpeg(bin)(context.Background(), dir, 12, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || len(b) < 8 || string(b[4:8]) != "ftyp" {
		t.Fatalf("the video: %d bytes, %v", len(b), err)
	}
}
