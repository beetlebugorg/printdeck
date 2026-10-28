package timelapse

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"printdeck/internal/camera"
)

func TestSettings(t *testing.T) {
	if s := ParseSettings(nil); s != DefaultSettings || !s.Park {
		t.Errorf("no file: %+v", s)
	}
	for _, s := range []Settings{{}, {On: true, Park: true, First: FirstPause}, {On: true, First: FirstPicture}} {
		if got := ParseSettings(s.Bytes()); got != s {
			t.Errorf("%+v: read %+v", s, got)
		}
	}
}

// At the limit of the frames, the recorder keeps every second frame and the layers between two
// frames double.
func TestRecorderThin(t *testing.T) {
	r := NewRecorder()
	sz := image.Pt(64, 48)
	for layer := 1; layer <= MaxFrames+1; layer++ {
		r.Add([]byte{byte(layer)}, sz, layer)
	}
	if r.Every() != 2 || r.Len() != MaxFrames/2+1 {
		t.Fatalf("every %d, %d frames", r.Every(), r.Len())
	}
	if r.Add([]byte{1}, sz, MaxFrames+2) != true || r.Add([]byte{1}, sz, MaxFrames+3) != false {
		t.Error("the frame of an odd layer is added")
	}
	if r.Add([]byte{1}, image.Pt(32, 24), MaxFrames+4) {
		t.Error("a frame of another size is added")
	}
}

func testJPEG(t *testing.T, w, h int, gray uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = gray
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// atoms returns the atoms of a QuickTime file by their path, for example "moov/trak/tkhd".
func atoms(t *testing.T, b []byte, prefix string, out map[string][]byte) {
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			t.Fatalf("%s: a bad atom size %d of %d", prefix, size, len(b))
		}
		kind := prefix + string(b[4:8])
		out[kind] = b[8:size]
		switch string(b[4:8]) {
		case "moov", "trak", "mdia", "minf", "stbl", "dinf":
			atoms(t, b[8:size], kind+"/", out)
		}
		b = b[size:]
	}
}

func TestWriteMOV(t *testing.T) {
	frames := [][]byte{testJPEG(t, 64, 48, 10), testJPEG(t, 64, 48, 200), testJPEG(t, 64, 48, 90)}
	var b bytes.Buffer
	if err := WriteMOV(&b, frames, 64, 48, 5); err != nil {
		t.Fatal(err)
	}
	a := map[string][]byte{}
	atoms(t, b.Bytes(), "", a)
	for _, k := range []string{"ftyp", "mdat", "moov/mvhd", "moov/trak/tkhd", "moov/trak/mdia/mdhd",
		"moov/trak/mdia/minf/stbl/stsd", "moov/trak/mdia/minf/stbl/stsz", "moov/trak/mdia/minf/stbl/stco"} {
		if _, ok := a[k]; !ok {
			t.Fatalf("no atom %s", k)
		}
	}
	if !bytes.Contains(a["moov/trak/mdia/minf/stbl/stsd"], []byte("jpeg")) {
		t.Error("the codec is not jpeg")
	}
	// Each sample is a frame at the offset of the chunk plus the sizes before it.
	stsz, stco := a["moov/trak/mdia/minf/stbl/stsz"], a["moov/trak/mdia/minf/stbl/stco"]
	if n := binary.BigEndian.Uint32(stsz[8:]); n != 3 {
		t.Fatalf("%d samples", n)
	}
	off := int(binary.BigEndian.Uint32(stco[8:]))
	for i, f := range frames {
		size := int(binary.BigEndian.Uint32(stsz[12+4*i:]))
		if !bytes.Equal(b.Bytes()[off:off+size], f) {
			t.Errorf("sample %d is not frame %d", i, i)
		}
		off += size
	}
	// The width and the height of the track.
	tkhd := a["moov/trak/tkhd"]
	if w, h := binary.BigEndian.Uint32(tkhd[76:]), binary.BigEndian.Uint32(tkhd[80:]); w != 64<<16 || h != 48<<16 {
		t.Errorf("track %x x %x", w, h)
	}
	if os.Getenv("TIMELAPSE_MOV") != "" {
		os.WriteFile(os.Getenv("TIMELAPSE_MOV"), b.Bytes(), 0o644)
	}
}

func TestVideoName(t *testing.T) {
	at := time.Date(2026, 10, 27, 14, 32, 0, 0, time.UTC)
	for file, want := range map[string]string{"clip.gcode": "clip-20261027-1432.mov", "parts/a b.gcode": "a_b-20261027-1432.mov",
		"": "print-20261027-1432.mov"} {
		if got := VideoName(file, at); got != want {
			t.Errorf("VideoName(%q) = %q, want %q", file, got, want)
		}
	}
}

// fakeHost is the printdeck_timelapse module of the Klipper host.
type fakeHost struct {
	mu    sync.Mutex
	st    HostStatus
	sets  int
	taken []int
}

func (h *fakeHost) TimelapseStatus() (HostStatus, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st, true
}

func (h *fakeHost) TimelapseTaken(n int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taken = append(h.taken, n)
	h.st.Taken, h.st.Waiting = n, false
	return nil
}

func (h *fakeHost) SetTimelapse(enable, park bool, first, every int, parkY float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sets++
	h.st.Enable, h.st.Park, h.st.First, h.st.Every = enable, park, first, every
	h.st.ParkY, h.st.HasParkY = parkY, !math.IsNaN(parkY)
	return nil
}

// frame asks for a picture, as a park of the module.
func (h *fakeHost) frame(first, lapse bool, layer int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.st.Frame++
	h.st.Waiting, h.st.IsFirst, h.st.IsLapse, h.st.Layer = true, first, lapse, layer
}

// fakeCamera sends a new frame at each call of Next.
type fakeCamera struct {
	t        *testing.T
	mu       sync.Mutex
	seq      uint64
	watchers int
	on       bool
}

func (c *fakeCamera) On() bool { return c.on }
func (c *fakeCamera) Watch() func() {
	c.mu.Lock()
	c.watchers++
	c.mu.Unlock()
	return func() { c.mu.Lock(); c.watchers--; c.mu.Unlock() }
}
func (c *fakeCamera) Peek() *camera.Frame { return nil }
func (c *fakeCamera) Next(ctx context.Context, after uint64) (*camera.Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq = max(c.seq, after) + 1
	f, err := camera.FrameOf(testJPEG(c.t, 64, 48, uint8(c.seq)))
	if err != nil {
		return nil, err
	}
	f.Seq = c.seq
	return f, nil
}

// A print with the time lapse and the first layer picture: the runner sends the settings,
// captures the pictures of the parks, and writes one video at the end.
func TestRunner(t *testing.T) {
	dir := t.TempDir()
	host := &fakeHost{st: HostStatus{Present: true, Park: true, Every: 1}}
	cam := &fakeCamera{t: t, on: true}
	var mu sync.Mutex
	p := Print{}
	r := NewRunner(Settings{On: true, Park: true, First: FirstPicture}, host, cam, func() Print {
		mu.Lock()
		defer mu.Unlock()
		return p
	}, dir)
	var firsts int
	r.OnFirstLayer = func(f *camera.Frame, file string) { firsts++ }
	var video, videoFile string
	r.OnVideo = func(name, file string) {
		video, videoFile = name, file
		if !r.Info().Saving {
			t.Error("the save is done before OnVideo")
		}
	}
	ctx := context.Background()

	r.step(ctx)
	if host.st.Enable != true || host.st.First != FirstPicture || host.sets != 1 {
		t.Fatalf("settings of the host: %+v", host.st)
	}
	mu.Lock()
	p = Print{Busy: true, File: "clip.gcode"}
	mu.Unlock()
	r.step(ctx)
	if cam.watchers != 1 {
		t.Errorf("%d watchers of the camera during the print", cam.watchers)
	}
	host.frame(true, true, 1)
	r.step(ctx)
	for layer := 2; layer <= 5; layer++ {
		host.frame(false, true, layer)
		r.step(ctx)
	}
	if len(host.taken) != 5 || firsts != 1 || r.Info().Frames != 5 || r.Info().First == nil {
		t.Fatalf("taken %v, %d first layer pictures, info %+v", host.taken, firsts, r.Info())
	}
	r.step(ctx) // no new frame: no picture
	if len(host.taken) != 5 {
		t.Errorf("taken %v", host.taken)
	}
	mu.Lock()
	p = Print{}
	mu.Unlock()
	r.step(ctx)
	if cam.watchers != 0 {
		t.Errorf("%d watchers of the camera after the print", cam.watchers)
	}
	in := r.Info()
	if len(in.Videos) != 1 || in.Recording || in.Saving {
		t.Fatalf("videos %+v", in.Videos)
	}
	if video != in.Videos[0].Name || videoFile != "clip.gcode" {
		t.Errorf("OnVideo got %q of %q", video, videoFile)
	}
	b, err := os.ReadFile(filepath.Join(dir, in.Videos[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	a := map[string][]byte{}
	atoms(t, b, "", a)
	// 5 frames, the last frame of the print, and the last frame again for one second (5 fps).
	if n := binary.BigEndian.Uint32(a["moov/trak/mdia/minf/stbl/stsz"][8:]); n != 6+5 {
		t.Errorf("%d frames in the video", n)
	}
	if _, ok := r.VideoPath(in.Videos[0].Name); !ok {
		t.Error("no path of the video")
	}
	if _, ok := r.VideoPath("../timelapse"); ok {
		t.Error("a path of a name that is not a video")
	}
}

// The first layer picture comes from the Picture camera, and the frames from the camera of the
// time lapse. Each camera is on during the print.
func TestRunnerPictureCamera(t *testing.T) {
	host := &fakeHost{st: HostStatus{Present: true, Park: true, Every: 1}}
	lapse := &fakeCamera{t: t, on: true}
	pic := &fakeCamera{t: t, on: true, seq: 1000}
	busy := false
	r := NewRunner(Settings{On: true, Park: true, First: FirstPicture}, host, lapse, func() Print {
		return Print{Busy: busy, File: "clip.gcode"}
	}, t.TempDir())
	r.Picture = pic
	ctx := context.Background()
	r.step(ctx)
	busy = true
	r.step(ctx)
	if lapse.watchers != 1 || pic.watchers != 1 {
		t.Errorf("watchers %d %d", lapse.watchers, pic.watchers)
	}
	host.frame(true, true, 1)
	r.step(ctx)
	host.frame(false, true, 2)
	r.step(ctx)
	in := r.Info()
	if in.First == nil || in.First.Seq < 1000 || in.Frames != 2 {
		t.Errorf("first layer picture %+v, %d frames", in.First, in.Frames)
	}
	if lapse.seq >= 1000 {
		t.Errorf("the frames came from the picture camera: %d", lapse.seq)
	}
	// A Picture camera that is off: no first layer picture.
	pic.on = false
	if s, _, _ := r.want(); s.First != FirstOff || !s.On {
		t.Errorf("want %+v", s)
	}
	busy = false
	r.step(ctx)
	if lapse.watchers != 0 || pic.watchers != 0 {
		t.Errorf("watchers after the print %d %d", lapse.watchers, pic.watchers)
	}
}

// With no camera, the host gets no frames to take.
func TestRunnerNoCamera(t *testing.T) {
	host := &fakeHost{st: HostStatus{Present: true, Enable: true, First: FirstPause, Park: true, Every: 1}}
	r := NewRunner(Settings{On: true, Park: true, First: FirstPause}, host, &fakeCamera{t: t}, func() Print { return Print{} }, t.TempDir())
	r.step(context.Background())
	if host.st.Enable || host.st.First != FirstOff {
		t.Errorf("settings of the host with no camera: %+v", host.st)
	}
}

// The video keeps KeepVideos files: a new video removes the oldest.
func TestKeepVideos(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(DefaultSettings, &fakeHost{}, &fakeCamera{t: t}, func() Print { return Print{} }, dir)
	for i := range KeepVideos + 2 {
		rec := NewRecorder()
		rec.Add(testJPEG(t, 16, 16, 1), image.Pt(16, 16), 1)
		rec.Add(testJPEG(t, 16, 16, 2), image.Pt(16, 16), 2)
		at := time.Date(2026, 1, 1, 10, i, 0, 0, time.UTC)
		if err := r.save(rec, "part.gcode", at); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(filepath.Join(dir, VideoName("part.gcode", at)), at, at)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != KeepVideos {
		t.Errorf("%d files", len(entries))
	}
}

// A print with a crop: the host gets the park Y of the print, and each frame of the video is the
// crop of the part at one size. The first layer picture stays whole. With the crop off, or with
// no park, the frames are whole.
func TestRunnerCrop(t *testing.T) {
	for _, tc := range []struct{ crop, park bool }{{true, true}, {false, true}, {true, false}} {
		crop := tc.crop
		dir := t.TempDir()
		host := &fakeHost{st: HostStatus{Present: true, Park: true, Every: 1}}
		cam := &fakeCamera{t: t, on: true}
		p := Print{}
		r := NewRunner(Settings{On: true, Park: tc.park, First: FirstPicture, Crop: crop}, host, cam, func() Print { return p }, dir)
		asks := 0
		r.ParkY = func(file string) (float64, bool) {
			asks++
			return 142.5, asks > 1 // the G-code of the print loads after the start
		}
		r.PartCrop = func(file string, sz image.Point) (image.Rectangle, bool) {
			if sz != image.Pt(64, 48) {
				t.Errorf("crop for a frame of %v", sz)
			}
			return image.Rect(0, 16, 48, 48), true
		}
		ctx := context.Background()
		p = Print{Busy: true, File: "clip.gcode"}
		r.step(ctx)
		r.step(ctx)
		if !host.st.HasParkY || host.st.ParkY != 142.5 {
			t.Fatalf("park Y of the host %+v", host.st)
		}
		host.frame(true, true, 1)
		r.step(ctx)
		for layer := 2; layer <= 4; layer++ {
			host.frame(false, true, layer)
			r.step(ctx)
		}
		if sz := r.Info().First.Size(); sz != image.Pt(64, 48) {
			t.Errorf("first layer picture %v, want the whole picture", sz)
		}
		p = Print{}
		r.step(ctx)
		if host.st.HasParkY {
			t.Error("the park Y stays after the print")
		}
		in := r.Info()
		if len(in.Videos) != 1 {
			t.Fatalf("videos %+v", in.Videos)
		}
		m, err := OpenMovie(filepath.Join(dir, in.Videos[0].Name))
		if err != nil {
			t.Fatal(err)
		}
		want := image.Pt(64, 42) // 48 x 32, 4/3 times: as wide as the frames
		if !crop || !tc.park {
			want = image.Pt(64, 48)
		}
		if m.W != want.X || m.H != want.Y {
			t.Errorf("crop %v, park %v: video of %dx%d, want %v", crop, tc.park, m.W, m.H, want)
		}
		for i := range m.Len() {
			b, _ := m.JPEG(i)
			c, err := jpeg.DecodeConfig(bytes.NewReader(b))
			if err != nil || c.Width != want.X || c.Height != want.Y {
				t.Errorf("frame %d: %dx%d, %v", i, c.Width, c.Height, err)
			}
		}
		m.Close()
	}
}

// A small crop is at most 2 times larger in the video.
func TestCropSize(t *testing.T) {
	for _, c := range []struct {
		r    image.Rectangle
		w    int
		want image.Point
	}{
		{image.Rect(0, 0, 640, 368), 1280, image.Pt(1280, 736)},
		{image.Rect(0, 0, 320, 176), 1280, image.Pt(640, 352)},
		{image.Rect(0, 0, 960, 544), 1280, image.Pt(1280, 724)},
	} {
		if got := cropSize(c.r, c.w); got != c.want {
			t.Errorf("cropSize(%v, %d) = %v, want %v", c.r, c.w, got, c.want)
		}
	}
}
