package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"printdeck/internal/camera"
)

// testCamera is a camera with two devices, and a picture of a print bed with a grid.
type testCamera struct {
	settings camera.Settings
	status   camera.Status
	frame    *camera.Frame
	peeks    int
}

func (c *testCamera) Peek() *camera.Frame           { c.peeks++; return c.frame }
func (c *testCamera) Status() camera.Status         { return c.status }
func (c *testCamera) Settings() camera.Settings     { return c.settings }
func (c *testCamera) SetSettings(s camera.Settings) { c.settings = s }
func (c *testCamera) Devices() []camera.Device {
	return []camera.Device{{Path: "/dev/video0", ID: "/dev/v4l/by-id/usb-Logitech_C270-video-index0", Name: "C270 HD WEBCAM"},
		{Path: "/dev/video2", ID: "/dev/v4l/by-id/usb-Elgato_Cam_Link_4K-video-index0", Name: "Cam Link 4K"}}
}

func newTestCamera(t *testing.T) *testCamera {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := range 720 {
		for x := range 1280 {
			c := color.RGBA{60, 64, 70, 255} // the frame of the printer
			if x > 240 && x < 1040 && y > 160 {
				c = color.RGBA{40, 40, 44, 255} // the bed
				if x%80 == 0 || y%80 == 0 {
					c = color.RGBA{200, 200, 200, 255}
				}
			}
			if (x-640)*(x-640)+(y-420)*(y-420) < 90*90 {
				c = color.RGBA{240, 120, 30, 255} // a print
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	f, err := camera.FrameOf(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return &testCamera{frame: f, status: camera.Status{State: "on", Device: camera.Device{Name: "C270 HD WEBCAM"},
		Format: camera.Format{In: "MJPEG", InW: 1280, InH: 720, OutW: 1280, OutH: 720, OutFPS: 15}}}
}

// Settings opens the Camera page, which shows the picture and changes the settings.
func TestCameraPage(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cam := newTestCamera(t)
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Camera")
	if a.topPage() != pageCamera {
		t.Fatalf("page %v, want the Camera page", a.topPage())
	}
	f := simFrame(t, a, "camera")
	if cam.peeks == 0 || !strings.Contains(f, `["c",`) {
		t.Error("no picture of the camera")
	}
	if !strings.Contains(f, "MJPEG 1280×720, 15 FPS") {
		t.Error("no format of the camera")
	}
	// Camera goes Auto, each camera, Off, Auto.
	want := []string{"/dev/v4l/by-id/usb-Logitech_C270-video-index0", "/dev/v4l/by-id/usb-Elgato_Cam_Link_4K-video-index0", camera.Off, ""}
	for _, w := range want {
		tapLabel(t, a, cv, "Camera")
		if cam.settings.Device != w {
			t.Fatalf("camera %q, want %q", cam.settings.Device, w)
		}
	}
	tapLabel(t, a, cv, "Picture")
	if cam.settings.Rotate != 180 {
		t.Errorf("turn %d, want 180", cam.settings.Rotate)
	}
	tapLabel(t, a, cv, "Picture")
	if cam.settings.Rotate != 0 {
		t.Errorf("turn %d, want 0", cam.settings.Rotate)
	}

	// With no camera, the page tells it and asks for no picture.
	cam.status = camera.Status{State: "none"}
	cam.peeks = 0
	if f := simFrame(t, a, "camera-none"); !strings.Contains(f, "PLUG A USB CAMERA") || cam.peeks != 0 {
		t.Errorf("no camera: peeks %d", cam.peeks)
	}
	cam.status = camera.Status{State: "error", Device: camera.Device{Name: "Cam Link 4K"}, Err: "the camera sends no pictures"}
	if f := simFrame(t, a, "camera-error"); !strings.Contains(f, "THE CAMERA SENDS NO PICTURES") {
		t.Error("no error")
	}
}

// The wide Camera page has the settings in its row of actions.
func TestCameraPageWide(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	a.Wide = true
	cam := newTestCamera(t)
	cam.settings.Rotate = 180
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	f := simFrame(t, a, "camera-wide")
	if !strings.Contains(f, `["c",`) || !strings.Contains(f, "TURN 180°") {
		t.Error("no picture or no Turn key")
	}
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-camera-wide"); !strings.Contains(f, "Auto: C270 HD WEBCAM") {
		t.Error("no Camera row in Settings")
	}
}

func TestCameraLine(t *testing.T) {
	devs := (&testCamera{}).Devices()
	for _, tc := range []struct {
		s    camera.Settings
		devs []camera.Device
		want string
	}{
		{camera.Settings{}, nil, "Auto: no camera found"},
		{camera.Settings{}, devs, "Auto: C270 HD WEBCAM"},
		{camera.Settings{Device: camera.Off}, devs, "Off"},
		{camera.Settings{Device: devs[1].ID}, devs, "Cam Link 4K"},
		{camera.Settings{Device: devs[1].ID}, nil, "Elgato Cam Link 4K (not connected)"},
	} {
		if got := cameraLine(tc.s, tc.devs); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.s, got, tc.want)
		}
	}
}
