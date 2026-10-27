package camera

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// testPicture is a picture of 64 x 32 in four quarters: the left half is dark and the right half
// is light (Y), and the top half is blue and the bottom half is red (Cb and Cr).
func testPicture(x, y int) (Y, Cb, Cr byte) {
	Y = 40
	if x >= 32 {
		Y = 200
	}
	Cb, Cr = 200, 60
	if y >= 16 {
		Cb, Cr = 60, 200
	}
	return
}

// rawFrame makes the test picture in a raw format, with bpl bytes a row of the Y plane.
func rawFrame(format string, w, h, bpl int) []byte {
	switch format {
	case "YUYV":
		b := make([]byte, bpl*h)
		for y := range h {
			for x := 0; x < w; x += 2 {
				y0, cb, cr := testPicture(x, y)
				y1, _, _ := testPicture(x+1, y)
				copy(b[y*bpl+x*2:], []byte{y0, cb, y1, cr})
			}
		}
		return b
	case "NV12", "YU12":
		b := make([]byte, bpl*h*3/2)
		for y := range h {
			for x := range w {
				b[y*bpl+x], _, _ = testPicture(x, y)
			}
		}
		for y := range h / 2 {
			for x := range w / 2 {
				_, cb, cr := testPicture(x*2, y*2)
				if format == "NV12" {
					b[bpl*h+y*bpl+2*x], b[bpl*h+y*bpl+2*x+1] = cb, cr
				} else {
					b[bpl*h+y*(bpl/2)+x] = cb
					b[bpl*h+(bpl/2)*(h/2)+y*(bpl/2)+x] = cr
				}
			}
		}
		return b
	}
	return nil
}

func TestRawFormats(t *testing.T) {
	const w, h = 64, 32
	for _, format := range []string{"YUYV", "NV12", "YU12"} {
		bpl := w + 16 // rows with padding
		if format == "YUYV" {
			bpl = 2*w + 16
		}
		y, cb, cr, err := rawPlanes(format, rawFrame(format, w, h, bpl), w, h, bpl)
		if err != nil {
			t.Fatal(format, err)
		}
		for _, flip := range []bool{false, true} {
			for _, size := range []image.Point{{64, 32}, {32, 16}, {16, 8}, {48, 24}} {
				out := toYCbCr(size.X, size.Y, y, cb, cr, flip)
				// The four quarters at the size of the output.
				for _, q := range []image.Point{{0, 0}, {3, 0}, {0, 3}, {3, 3}} {
					px, py := q.X*size.X/4+size.X/8, q.Y*size.Y/4+size.Y/8
					sx, sy := px*w/size.X, py*h/size.Y
					if flip {
						sx, sy = w-1-sx, h-1-sy
					}
					wy, wcb, wcr := testPicture(sx, sy)
					c := out.YCbCrAt(px, py)
					if c.Y != wy || c.Cb != wcb || c.Cr != wcr {
						t.Errorf("%s flip %v %v: at %d,%d: %v, want %d %d %d", format, flip, size, px, py, c, wy, wcb, wcr)
					}
				}
			}
		}
	}
	if _, _, _, err := rawPlanes("NV12", make([]byte, 100), 64, 32, 64); err == nil {
		t.Error("a short frame: no error")
	}
}

// A picture of one color is blank, as an HDMI capture sends with no signal.
func TestBlank(t *testing.T) {
	y, _, _, _ := rawPlanes("YU12", rawFrame("YU12", 64, 32, 64), 64, 32, 64)
	if blank(y) {
		t.Error("the test picture is blank")
	}
	flat := make([]byte, 64*32*3/2)
	for i := range flat {
		flat[i] = 7 + byte(i%3)
	}
	y, _, _, _ = rawPlanes("YU12", flat, 64, 32, 64)
	if !blank(y) {
		t.Error("a picture of one color is not blank")
	}
}

// TestHalfMean checks that a picture at half the size has the mean of each 2 x 2 block.
func TestHalfMean(t *testing.T) {
	src := plane{pix: []byte{0, 100, 10, 10, 200, 100, 30, 50}, stride: 4, step: 1, w: 4, h: 2}
	dst := make([]byte, 2)
	scalePlane(dst, 2, 2, 1, src, false)
	if dst[0] != 100 || dst[1] != 25 {
		t.Errorf("got %v, want [100 25]", dst)
	}
}

// stripDHT removes the DHT segments of a JPEG, as some cameras send it.
func stripDHT(b []byte) []byte {
	out := []byte{0xFF, 0xD8}
	i := 2
	for i+4 <= len(b) {
		m := b[i+1]
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if m == 0xDA {
			return append(out, b[i:]...)
		}
		if m != 0xC4 {
			out = append(out, b[i:i+2+n]...)
		}
		i += 2 + n
	}
	return out
}

func testJPEG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 40, 24))
	for y := range 24 {
		for x := range 40 {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 10), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFixJPEG(t *testing.T) {
	full := testJPEG(t)
	bare := stripDHT(full)
	if bytes.Contains(bare, []byte{0xFF, 0xC4}) {
		t.Fatal("the test JPEG has a DHT")
	}
	if _, err := jpeg.Decode(bytes.NewReader(bare)); err == nil {
		t.Fatal("a JPEG with no DHT decodes: the test shows nothing")
	}
	// A frame of a camera: no DHT, and padding after the end.
	frame := append(append([]byte(nil), bare...), 0, 0, 0, 0)
	fixed, w, h, err := fixJPEG(frame)
	if err != nil {
		t.Fatal(err)
	}
	if w != 40 || h != 24 {
		t.Errorf("size %dx%d, want 40x24", w, h)
	}
	if !bytes.HasSuffix(fixed, []byte{0xFF, 0xD9}) {
		t.Error("the padding is still there")
	}
	got, err := jpeg.Decode(bytes.NewReader(fixed))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := jpeg.Decode(bytes.NewReader(full))
	// Go encodes with the standard tables, so the pictures are the same.
	if !bytes.Equal(got.(*image.YCbCr).Y, want.(*image.YCbCr).Y) {
		t.Error("the fixed JPEG has another picture")
	}
	// A JPEG with its tables stays the same.
	same, _, _, err := fixJPEG(full)
	if err != nil || !bytes.Equal(same, full) {
		t.Errorf("a full JPEG changed (%v)", err)
	}
	for _, bad := range [][]byte{nil, {0xFF, 0xD8}, []byte("not a jpeg at all"), full[:20]} {
		if _, _, _, err := fixJPEG(bad); err == nil {
			t.Errorf("no error for %q", bad)
		}
	}
}

func TestSmall(t *testing.T) {
	b, w, h, err := fixJPEG(testJPEG(t))
	if err != nil {
		t.Fatal(err)
	}
	big := newFrame(b, w, h, nil)
	if big.Small(1000) != big || big.Small(40) != big {
		t.Error("a width that is not smaller gives another frame")
	}
	big.w, big.h = 400, 240 // a larger frame, to have room for smaller widths
	big.pic = scaleYCbCr(big.picture(), 400, 240, false)
	s := big.Small(100)
	if s.Size() != image.Pt(96, 58) {
		t.Errorf("size %v, want 96x58", s.Size())
	}
	if big.Small(100) != s || big.Small(96) != s {
		t.Error("the viewers of one size get two frames")
	}
	if _, err := jpeg.Decode(bytes.NewReader(s.JPEG())); err != nil {
		t.Error(err)
	}
	for _, w := range []int{64, 128, 160, 192, 224} {
		big.Small(w)
	}
	if len(big.smalls) != smallKeep {
		t.Errorf("%d sizes kept, want %d", len(big.smalls), smallKeep)
	}
}

func TestSettings(t *testing.T) {
	for _, s := range []Settings{{}, {AllOff: true},
		{Cameras: []CamSettings{{ID: "/dev/v4l/by-id/usb-x-video-index0", Rotate: 180, Name: "Top"}, {ID: "b", Off: true}},
			Main: "b", Lapse: "/dev/v4l/by-id/usb-x-video-index0", Picture: "b"}} {
		if got := ParseSettings(s.Bytes()); !reflect.DeepEqual(got, s) {
			t.Errorf("%+v: read back as %+v", s, got)
		}
	}
	if s := ParseSettings([]byte(`{"cameras":[{"id":"a","rotate":90}]}`)); s.Cameras[0].Rotate != 0 {
		t.Errorf("a turn of 90: %+v", s)
	}
}

// The camera file of an older deck: one line for the device, and one for the turn.
func TestSettingsMigration(t *testing.T) {
	for _, c := range []struct {
		file string
		want Settings
	}{
		{"auto\n0\n", Settings{Cameras: []CamSettings{{}}}},
		{"auto\n90\n", Settings{Cameras: []CamSettings{{}}}},
		{"auto\n180\n", Settings{Cameras: []CamSettings{{Rotate: 180}}}},
		{"off\n0\n", Settings{AllOff: true}},
		{"/dev/v4l/by-id/usb-x-video-index0\n180\n", Settings{Cameras: []CamSettings{{ID: "/dev/v4l/by-id/usb-x-video-index0", Rotate: 180}},
			Main: "/dev/v4l/by-id/usb-x-video-index0"}},
		{"", Settings{Cameras: []CamSettings{{}}}},
	} {
		if got := ParseSettings([]byte(c.file)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v, want %+v", c.file, got, c.want)
		}
	}
}

// fakeHub is a hub with one fake camera that makes a frame each 10 ms.
func fakeHub(t *testing.T) (*Hub, *atomic.Int32) {
	linger = 300 * time.Millisecond
	t.Cleanup(func() { linger = 8 * time.Second })
	var running atomic.Int32
	h := NewHub(Settings{})
	h.scan = func() []Device { return []Device{{Path: "/dev/video9", ID: "cam", Name: "Test camera"}} }
	jb := testJPEG(t)
	h.capture = func(ctx context.Context, d Device, s CamSettings, format func(Format), emit func(*Frame)) error {
		running.Add(1)
		defer running.Add(-1)
		format(Format{In: "MJPEG", InW: 40, InH: 24, OutW: 40, OutH: 24, OutFPS: 100})
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-tick.C:
				emit(newFrame(jb, 40, 24, nil))
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	return h, &running
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("no %s in 3 s", what)
}

func TestHubOnDemand(t *testing.T) {
	h, running := fakeHub(t)
	time.Sleep(50 * time.Millisecond)
	if running.Load() != 0 {
		t.Fatal("the camera runs with no viewer")
	}
	if s := h.Status(); s.State != "idle" || s.Device.Name != "Test camera" {
		t.Errorf("status %+v", s)
	}
	f, err := h.Snapshot(context.Background())
	if err != nil || f == nil {
		t.Fatal(err)
	}
	if s := h.Status(); s.State != "on" || s.Format.In != "MJPEG" {
		t.Errorf("status %+v", s)
	}
	// The camera stops after linger with no viewer.
	waitFor(t, "stop", func() bool { return running.Load() == 0 })

	// A stream keeps it on; a slow viewer skips frames.
	stop := h.Watch()
	ctx := context.Background()
	a, _ := h.Next(ctx, 0)
	time.Sleep(100 * time.Millisecond)
	b, _ := h.Next(ctx, a.Seq)
	if b.Seq <= a.Seq+1 {
		t.Errorf("a slow viewer got frame %d after %d: it skipped no frame", b.Seq, a.Seq)
	}
	time.Sleep(2 * linger)
	if running.Load() != 1 {
		t.Error("the camera stopped while a viewer watches")
	}
	stop()
	waitFor(t, "stop", func() bool { return running.Load() == 0 })

	// Off stops the camera, and On is false.
	h.SetSettings(Settings{AllOff: true})
	if h.On() || h.Status().State != "off" {
		t.Error("the camera is on with the setting Off")
	}
	if _, err := h.Snapshot(context.Background()); err == nil {
		t.Error("a snapshot with the setting Off")
	}
	// A main camera that is not there: the main camera is the first camera that is on.
	h.SetSettings(Settings{Main: "other"})
	if !h.On() || h.Status().Device.ID != "cam" {
		t.Error("no main camera with a main camera that is not there")
	}
	// No camera.
	h.mu.Lock()
	h.scan, h.scanned = func() []Device { return nil }, time.Time{}
	h.mu.Unlock()
	if h.On() || h.Status().State != "none" {
		t.Errorf("no camera: %+v", h.Status())
	}
}
