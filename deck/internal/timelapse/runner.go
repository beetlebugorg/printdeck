package timelapse

import (
	"context"
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"printdeck/internal/camera"
)

// HostStatus is the status of the printdeck_timelapse module of the Klipper host.
type HostStatus struct {
	Enable, Park bool
	First, Every int
	Frame, Taken int // the number of the last frame, and of the last frame with a picture
	Waiting      bool
	IsFirst      bool // the frame is the picture of the first layer
	IsLapse      bool // the frame is a frame of the time lapse
	Layer        int  // the layers that are done at the frame
	Stopped      bool
	Present      bool    // the host has the module
	ParkY        float64 // the Y of the park of the print, with HasParkY
	HasParkY     bool
}

// Host is the Klipper host of the print.
type Host interface {
	// TimelapseStatus returns the status of the module, and false with no connection.
	TimelapseStatus() (HostStatus, bool)
	// TimelapseTaken sends the number n to the module: the picture of frame n is ready.
	TimelapseTaken(n int) error
	// SetTimelapse sends the settings to the module (PRINTDECK_TIMELAPSE). parkY is the Y of the
	// park of the print, or NaN for none.
	SetTimelapse(enable, park bool, first, every int, parkY float64) error
}

// Camera is a camera of the deck (camera.Source).
type Camera interface {
	On() bool
	Watch() (stop func())
	Peek() *camera.Frame
	Next(ctx context.Context, after uint64) (*camera.Frame, error)
}

// Print is the state of the print for the runner.
type Print struct {
	Busy bool   // printing or paused
	File string // the name of the file
}

// Video is a time lapse video in the folder of the videos.
type Video struct {
	Name string
	Size int64
	Time time.Time
}

// Info is the state of the time lapse for the screen.
type Info struct {
	Recording bool // a print with the time lapse runs
	Saving    bool // the deck writes the video of the print that ended
	Frames    int  // the frames of the print now, or of the video that the deck writes
	Videos    []Video
	// First is the picture of the last first layer, or nil, with its time and its file.
	First     *camera.Frame
	FirstAt   time.Time
	FirstFile string
}

// KeepVideos is the count of videos that the folder keeps. A new video removes the oldest.
const KeepVideos = 3

// The times of the runner.
var (
	tick       = 100 * time.Millisecond
	frameWait  = 4 * time.Second // the longest wait for a picture of the camera
	resendWait = 2 * time.Second // the time between two sends of the same settings
)

// Runner follows the prints and the park of the Klipper host, and captures the pictures.
type Runner struct {
	// Dir is the folder of the videos.
	Dir string
	// OnSettings gets the new settings, to save them, or is nil.
	OnSettings func(Settings)
	// OnFirstLayer gets the picture of the first layer, or is nil.
	OnFirstLayer func(f *camera.Frame, file string)
	// OnVideo gets the name of a new video and the file of its print, or is nil. The runner calls
	// it before Info shows that the save is done.
	OnVideo func(name, file string)
	// Picture is the camera of the first layer picture, or nil for the camera of the frames.
	Picture Camera
	// ParkY returns the Y of the park for the print of the file: the middle of the part in Y. It
	// returns false while the deck does not know it, and for a file with no moves. It can be nil.
	ParkY func(file string) (float64, bool)
	// PartCrop returns the crop of the part of the print of the file in a frame of size sz, with
	// the bed at the park Y. It returns false with no calibration of the camera. It can be nil.
	PartCrop func(file string, sz image.Point) (image.Rectangle, bool)

	host  Host
	cam   Camera
	print func() Print

	mu        sync.Mutex
	settings  Settings
	rec       *Recorder // the frames of the print now, or nil
	saving    *Recorder // the frames of the video that the deck writes now, or nil
	file      string
	stopCam   func()
	stopPic   func() // the watch of the Picture camera
	handled   int    // the last frame of the host with a picture
	sent      string
	sentAt    time.Time
	first     *camera.Frame
	firstAt   time.Time
	firstFile string
	videos    []Video
	parkY     float64 // the park Y of the print, or NaN
	crop      lapseCrop
}

// lapseCrop is the crop of the frames of a print.
type lapseCrop struct {
	set  bool            // the crop of the print is known
	src  image.Point     // the size of the frames of the camera
	r    image.Rectangle // the crop, or empty for the whole picture
	size image.Point     // the size of the frames of the video
}

// NewRunner returns a runner with the settings s. Call Run to start it.
func NewRunner(s Settings, host Host, cam Camera, print func() Print, dir string) *Runner {
	r := &Runner{settings: s, host: host, cam: cam, print: print, Dir: dir, parkY: math.NaN()}
	r.videos = listVideos(dir)
	return r
}

// Settings returns the settings.
func (r *Runner) Settings() Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settings
}

// SetSettings changes the settings. The runner sends them to the host at its next step.
func (r *Runner) SetSettings(s Settings) {
	r.mu.Lock()
	r.settings = s
	r.sent = ""
	save := r.OnSettings
	r.mu.Unlock()
	if save != nil {
		save(s)
	}
}

// Info returns the state of the time lapse.
func (r *Runner) Info() Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	in := Info{Recording: r.rec != nil && r.settings.On, Saving: r.saving != nil, Videos: append([]Video(nil), r.videos...),
		First: r.first, FirstAt: r.firstAt, FirstFile: r.firstFile}
	switch {
	case r.rec != nil:
		in.Frames = r.rec.Len()
	case r.saving != nil:
		in.Frames = r.saving.Len()
	}
	return in
}

// VideoPath returns the path of the video name, and false for a name that is not a video of the
// folder.
func (r *Runner) VideoPath(name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.videos {
		if v.Name == name {
			return filepath.Join(r.Dir, name), true
		}
	}
	return "", false
}

// Run follows the prints until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.endPrint(ctx, false)
			return
		case <-t.C:
		}
		r.step(ctx)
	}
}

// step does one turn of the runner: the start and the end of a print, the settings of the host,
// and a picture when the host waits for one.
func (r *Runner) step(ctx context.Context) {
	p := r.print()
	r.mu.Lock()
	active := r.rec != nil
	file := r.file
	r.mu.Unlock()
	switch {
	case p.Busy && !active:
		r.startPrint(p.File)
	case p.Busy && p.File != file:
		r.endPrint(ctx, true)
		r.startPrint(p.File)
	case !p.Busy && active:
		r.endPrint(ctx, true)
	}
	r.watch()
	r.findParkY()

	hs, ok := r.host.TimelapseStatus()
	if !ok || !hs.Present {
		return
	}
	r.mu.Lock()
	handled := r.handled
	r.mu.Unlock()
	if hs.Waiting && hs.Frame > handled {
		// The picture first: a G-code script waits while the host waits for the picture.
		r.takePicture(ctx, hs)
		return
	}
	r.sync(hs)
}

// want returns the settings for the host: no frames with no camera.
func (r *Runner) want() (s Settings, every int, parkY float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, every, parkY = r.settings, 1, r.parkY
	if r.rec != nil {
		every = r.rec.Every()
	}
	if !r.cam.On() {
		s.On = false
	}
	if !r.picture().On() {
		s.First = FirstOff
	}
	return s, every, parkY
}

// sync sends the settings to the host when the host has other settings.
func (r *Runner) sync(hs HostStatus) {
	s, every, parkY := r.want()
	sameY := hs.HasParkY == !math.IsNaN(parkY) && (!hs.HasParkY || math.Abs(hs.ParkY-parkY) < 0.01)
	if hs.Enable == s.On && hs.Park == s.Park && hs.First == s.First && hs.Every == every && sameY {
		return
	}
	key := fmt.Sprint(s, every, parkY)
	r.mu.Lock()
	if key == r.sent && time.Since(r.sentAt) < resendWait {
		r.mu.Unlock()
		return
	}
	r.sent, r.sentAt = key, time.Now()
	r.mu.Unlock()
	if err := r.host.SetTimelapse(s.On, s.Park, s.First, every, parkY); err != nil {
		log.Printf("timelapse: the settings were not sent: %v", err)
	}
}

// startPrint starts the frames of a print.
func (r *Runner) startPrint(file string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rec, r.file = NewRecorder(), file
	r.parkY, r.crop = math.NaN(), lapseCrop{}
}

// findParkY asks for the park Y of the print until the deck knows it.
func (r *Runner) findParkY() {
	r.mu.Lock()
	ask := r.rec != nil && math.IsNaN(r.parkY) && r.ParkY != nil
	file := r.file
	r.mu.Unlock()
	if !ask {
		return
	}
	if y, ok := r.ParkY(file); ok {
		r.mu.Lock()
		if r.file == file {
			r.parkY = y
		}
		r.mu.Unlock()
	}
}

// lapseFrame returns the frame f of the camera as a frame of the video: the crop of the part,
// or the whole picture at MaxWidth or smaller. The first frame of a print sets the crop and the
// size of the video.
func (r *Runner) lapseFrame(f *camera.Frame) *camera.Frame {
	r.mu.Lock()
	// The crop needs the park: with no park, a bed that moves in Y is at another place in each
	// frame.
	c, file, on := r.crop, r.file, r.settings.Crop && r.settings.Park
	r.mu.Unlock()
	if !c.set {
		c = lapseCrop{set: true, src: f.Size()}
		if on && r.PartCrop != nil {
			if cr, ok := r.PartCrop(file, f.Size()); ok && cr != image.Rect(0, 0, f.Size().X, f.Size().Y) {
				c.r, c.size = cr, cropSize(cr, min(f.Size().X, MaxWidth))
			}
		}
		r.mu.Lock()
		r.crop = c
		r.mu.Unlock()
	}
	if c.r.Empty() || f.Size() != c.src {
		return small(f)
	}
	return f.CropTo(c.r, c.size.X, c.size.Y)
}

// cropSize is the size of the frames of the video of the crop c, for frames of width w: as wide
// as the frames, and at most 2 times the crop. The sides are even.
func cropSize(c image.Rectangle, w int) image.Point {
	k := min(2, float64(w)/float64(c.Dx()))
	return image.Pt(int(float64(c.Dx())*k)&^1, int(float64(c.Dy())*k)&^1)
}

// picture returns the camera of the first layer picture.
func (r *Runner) picture() Camera {
	if r.Picture != nil {
		return r.Picture
	}
	return r.cam
}

// watch keeps the cameras on during a print with the time lapse or the first layer picture, so
// that a picture has no wait for the start of a camera.
func (r *Runner) watch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	watch := func(stop *func(), cam Camera, need bool) {
		need = need && r.rec != nil && cam.On()
		switch {
		case need && *stop == nil:
			*stop = cam.Watch()
		case !need && *stop != nil:
			(*stop)()
			*stop = nil
		}
	}
	watch(&r.stopCam, r.cam, r.settings.On || (r.Picture == nil && r.settings.First != FirstOff))
	if r.Picture != nil {
		watch(&r.stopPic, r.Picture, r.settings.First != FirstOff)
	}
}

// endPrint ends the frames of the print. With save, it adds a last frame and writes the video.
func (r *Runner) endPrint(ctx context.Context, save bool) {
	r.mu.Lock()
	rec, file, stop, stopPic := r.rec, r.file, r.stopCam, r.stopPic
	r.rec, r.file, r.stopCam, r.stopPic, r.parkY = nil, "", nil, nil, math.NaN()
	if rec != nil && save && rec.Len() > 0 {
		r.saving = rec
	}
	r.mu.Unlock()
	if rec == nil {
		return
	}
	defer func() {
		r.mu.Lock()
		r.saving = nil
		r.mu.Unlock()
	}()
	if save && rec.Len() > 0 {
		// The last frame: the end G-code of the print moves the toolhead away.
		if f := r.grab(ctx, r.cam); f != nil {
			f = r.lapseFrame(f)
			rec.AddLast(f.JPEG(), f.Size())
		}
	}
	if stop != nil {
		stop()
	}
	if stopPic != nil {
		stopPic()
	}
	if !save || rec.Len() < 2 {
		return
	}
	if err := r.save(rec, file, time.Now()); err != nil {
		log.Printf("timelapse: the video was not saved: %v", err)
	}
}

// takePicture captures the picture of a frame of the host, and sends the frame number to the host.
// A frame of the time lapse comes from the camera of the frames, and the first layer picture from
// the Picture camera. When they are two cameras, the deck gets the two pictures at the same time.
func (r *Runner) takePicture(ctx context.Context, hs HostStatus) {
	var f, pf *camera.Frame
	pic := r.picture()
	switch {
	case hs.IsFirst && hs.IsLapse && pic != r.cam:
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			pf = r.grab(ctx, pic)
		}()
		f = r.grab(ctx, r.cam)
		wg.Wait()
	case hs.IsFirst && !hs.IsLapse:
		pf = r.grab(ctx, pic)
	default:
		f = r.grab(ctx, r.cam)
		pf = f
	}
	r.mu.Lock()
	r.handled = hs.Frame
	rec := r.rec
	file := r.file
	r.mu.Unlock()
	if f != nil && hs.IsLapse && rec != nil {
		lf := r.lapseFrame(f)
		b := lf.JPEG()
		r.mu.Lock()
		rec.Add(b, lf.Size(), hs.Layer)
		r.mu.Unlock()
	}
	if pf != nil {
		f := small(pf)
		if hs.IsFirst {
			r.mu.Lock()
			r.first, r.firstAt, r.firstFile = f, time.Now(), file
			cb := r.OnFirstLayer
			r.mu.Unlock()
			if cb != nil {
				cb(f, file)
			}
		}
	}
	if err := r.host.TimelapseTaken(hs.Frame); err != nil {
		log.Printf("timelapse: frame %d: %v", hs.Frame, err)
	}
}

// grab returns a frame that the camera cam captures after this call, or nil. The camera sends a
// few frames late, so grab skips one frame: the picture shows the toolhead at the park position.
func (r *Runner) grab(ctx context.Context, cam Camera) *camera.Frame {
	if !cam.On() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, frameWait)
	defer cancel()
	var after uint64
	if f := cam.Peek(); f != nil {
		after = f.Seq
	}
	var f *camera.Frame
	for range 2 {
		var err error
		if f, err = cam.Next(ctx, after); err != nil {
			return nil
		}
		after = f.Seq
	}
	return f
}

// small returns the frame at MaxWidth or smaller.
func small(f *camera.Frame) *camera.Frame {
	if f.Size().X > MaxWidth {
		return f.Small(MaxWidth)
	}
	return f
}

// save writes the video of a print to the folder, and removes the oldest videos.
func (r *Runner) save(rec *Recorder, file string, now time.Time) error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	name := VideoName(file, now)
	tmp, err := os.CreateTemp(r.Dir, ".video-*")
	if err != nil {
		return err
	}
	if err := rec.WriteVideo(tmp); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(r.Dir, name)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	videos := listVideos(r.Dir)
	for len(videos) > KeepVideos {
		os.Remove(filepath.Join(r.Dir, videos[len(videos)-1].Name))
		videos = videos[:len(videos)-1]
	}
	r.mu.Lock()
	r.videos = videos
	cb := r.OnVideo
	r.mu.Unlock()
	log.Printf("timelapse: %s, %d frames", name, rec.Len())
	if cb != nil {
		cb(name, file)
	}
	return nil
}

// VideoName is the name of the video of the file of a print at the time t, for example
// "clip-20261027-1432.mov".
func VideoName(file string, t time.Time) string {
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	base = strings.Map(func(c rune) rune {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			return c
		}
		return '_'
	}, base)
	if base == "" || base == "_" || base == "." {
		base = "print"
	}
	return base + "-" + t.Format("20060102-1504") + ".mov"
}

// listVideos returns the videos of the folder, the newest first.
func listVideos(dir string) []Video {
	entries, _ := os.ReadDir(dir)
	var out []Video
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".mov") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Video{Name: e.Name(), Size: fi.Size(), Time: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Name > out[j].Name
	})
	return out
}
