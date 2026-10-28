package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/timelapse"
)

// fakeTimeLapse is the time lapse of the deck.
type fakeTimeLapse struct {
	s   timelapse.Settings
	in  timelapse.Info
	dir string // the folder of the videos of in
}

func (f *fakeTimeLapse) Settings() timelapse.Settings     { return f.s }
func (f *fakeTimeLapse) SetSettings(s timelapse.Settings) { f.s = s }
func (f *fakeTimeLapse) Info() timelapse.Info             { return f.in }
func (f *fakeTimeLapse) VideoPath(name string) (string, bool) {
	for _, v := range f.in.Videos {
		if v.Name == name {
			return filepath.Join(f.dir, name), true
		}
	}
	return "", false
}

func testFrame(t *testing.T) *camera.Frame {
	t.Helper()
	img := image.NewYCbCr(image.Rect(0, 0, 320, 180), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = 120
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	f, err := camera.FrameOf(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Settings opens the Time lapse page. Its rows change the settings.
func TestTimeLapsePage(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	a.Camera = NewDemoCamera()
	tl := &fakeTimeLapse{s: timelapse.DefaultSettings}
	a.TimeLapse = tl
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-timelapse"); !strings.Contains(f, "TIME LAPSE") {
		t.Fatal("no Time lapse row in Settings")
	}
	tapLabel(t, a, cv, "Time lapse")
	if a.topPage() != pageTimeLapse {
		t.Fatalf("page %v, want the Time lapse page", a.topPage())
	}
	simFrame(t, a, "timelapse")
	tapLabel(t, a, cv, "Time lapse")
	if !tl.s.On {
		t.Error("the time lapse is not on")
	}
	tapLabel(t, a, cv, "Park for the frames")
	if tl.s.Park {
		t.Error("the park is on")
	}
	if f := simFrame(t, a, "timelapse-crop"); !strings.Contains(f, "Needs a calibration of the camera") {
		t.Error("no line for the crop with no calibration")
	}
	tapLabel(t, a, cv, "Crop to the part")
	if tl.s.Crop {
		t.Error("the crop is on")
	}
	for _, want := range []int{timelapse.FirstPicture, timelapse.FirstPause, timelapse.FirstOff} {
		tapLabel(t, a, cv, "First layer")
		if tl.s.First != want {
			t.Errorf("first layer %d, want %d", tl.s.First, want)
		}
	}

	// A picture of the first layer opens its page.
	tl.in = timelapse.Info{First: testFrame(t), FirstAt: time.Now(), FirstFile: "clip.gcode",
		Videos: []timelapse.Video{{Name: "clip-20261027-1432.mov"}}}
	if f := simFrame(t, a, "timelapse-first"); !strings.Contains(f, "1 video") {
		t.Error("no line of the videos")
	}
	a.ShowFirstLayer()
	for len(a.posted) > 0 {
		(<-a.posted)()
	}
	if a.topPage() != pageFirstLayer {
		t.Fatalf("page %v, want the First layer page", a.topPage())
	}
	if f := simFrame(t, a, "first-layer"); !strings.Contains(f, `["c",`) || !strings.Contains(f, "clip") {
		t.Error("no picture or no file on the First layer page")
	}
}

func TestTimeLapseLine(t *testing.T) {
	for _, tc := range []struct {
		s    timelapse.Settings
		want string
	}{
		{timelapse.Settings{}, "Off"},
		{timelapse.Settings{On: true}, "On · a frame at each layer"},
		{timelapse.Settings{First: timelapse.FirstPause}, "Off · a picture of the first layer"},
		{timelapse.Settings{On: true, First: timelapse.FirstPicture}, "On · and a picture of the first layer"},
	} {
		if got := timeLapseLine(tc.s); got != tc.want {
			t.Errorf("timeLapseLine(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}
