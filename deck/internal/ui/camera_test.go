package ui

import (
	"strings"
	"testing"

	"printdeck/internal/camera"
)

// Settings opens the Camera page, which shows the picture and changes the settings.
func TestCameraPage(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cam := NewDemoCamera()
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Camera")
	if a.topPage() != pageCamera {
		t.Fatalf("page %v, want the Camera page", a.topPage())
	}
	f := simFrame(t, a, "camera")
	if cam.Peeks == 0 || !strings.Contains(f, `["c",`) {
		t.Error("no picture of the camera")
	}
	if !strings.Contains(f, "MJPEG 1280×720, 15 FPS") {
		t.Error("no format of the camera")
	}
	// Camera goes Auto, each camera, Off, Auto.
	want := []string{"/dev/v4l/by-id/usb-Logitech_C270-video-index0", "/dev/v4l/by-id/usb-Elgato_Cam_Link_4K-video-index0", camera.Off, ""}
	for _, w := range want {
		tapLabel(t, a, cv, "Camera")
		if cam.Set.Device != w {
			t.Fatalf("camera %q, want %q", cam.Set.Device, w)
		}
	}
	tapLabel(t, a, cv, "Turn")
	if cam.Set.Rotate != 180 {
		t.Errorf("turn %d, want 180", cam.Set.Rotate)
	}
	tapLabel(t, a, cv, "Turn")
	if cam.Set.Rotate != 0 {
		t.Errorf("turn %d, want 0", cam.Set.Rotate)
	}

	// With no camera, the page tells it and asks for no picture.
	cam.State = camera.Status{State: "none"}
	cam.Peeks = 0
	if f := simFrame(t, a, "camera-none"); !strings.Contains(f, "PLUG A USB CAMERA") || cam.Peeks != 0 {
		t.Errorf("no camera: peeks %d", cam.Peeks)
	}
	cam.State = camera.Status{State: "nosignal", Device: camera.Device{Name: "Cam Link 4K"}}
	if f := simFrame(t, a, "camera-nosignal"); !strings.Contains(f, "NO SIGNAL") {
		t.Error("no signal: not on the page")
	}
	cam.State = camera.Status{State: "error", Device: camera.Device{Name: "Cam Link 4K"}, Err: "the camera sends no pictures"}
	if f := simFrame(t, a, "camera-error"); !strings.Contains(f, "THE CAMERA SENDS NO PICTURES") {
		t.Error("no error")
	}
}

// The wide Camera page has the settings in its row of actions.
func TestCameraPageWide(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	a.Wide = true
	cam := NewDemoCamera()
	cam.Set.Rotate = 180
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	f := simFrame(t, a, "camera-wide")
	if !strings.Contains(f, `["c",`) || !strings.Contains(f, `"TURN"`) {
		t.Error("no picture or no Turn key")
	}
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-camera-wide"); !strings.Contains(f, "Auto: C270 HD WEBCAM") {
		t.Error("no Camera row in Settings")
	}
}

func TestCameraLine(t *testing.T) {
	devs := NewDemoCamera().Devices()
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
