package ui

import (
	"fmt"
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
	if cam.Cams[0].Peeks == 0 || !strings.Contains(f, `["c",`) {
		t.Error("no picture of the camera")
	}
	if !strings.Contains(f, "MJPEG 1280×720, 15 FPS") {
		t.Error("no format of the camera")
	}
	// Camera shows each camera, then all cameras, then the first camera again.
	ids := []string{cam.Cams[1].Dev.ID, camAll, cam.Cams[0].Dev.ID}
	for i, w := range ids {
		tapLabel(t, a, cv, "Camera")
		if a.camID != w {
			t.Fatalf("camera %q, want %q", a.camID, w)
		}
		simFrame(t, a, fmt.Sprintf("camera-%d", i))
	}
	tapLabel(t, a, cv, "Turn")
	if r := cam.Set.Camera(cam.Cams[0].Dev.ID).Rotate; r != 180 {
		t.Errorf("turn %d, want 180", r)
	}
	if r := cam.Set.Camera(cam.Cams[1].Dev.ID).Rotate; r != 0 {
		t.Errorf("turn of the other camera %d, want 0", r)
	}
	tapLabel(t, a, cv, "Turn")
	if r := cam.Set.Camera(cam.Cams[0].Dev.ID).Rotate; r != 0 {
		t.Errorf("turn %d, want 0", r)
	}

	// All cameras: both pictures, and a tap on a picture shows its camera.
	a.camID = camAll
	cam.Cams[0].Peeks, cam.Cams[1].Peeks = 0, 0
	f = simFrame(t, a, "camera-all")
	if cam.Cams[0].Peeks == 0 || cam.Cams[1].Peeks == 0 || strings.Count(f, `["c",`) < 2 {
		t.Error("not two pictures")
	}

	// Setup: on or off, the name, and the roles.
	a.camID = cam.Cams[1].Dev.ID
	tapLabel(t, a, cv, "Setup")
	if a.topPage() != pageCameraSetup {
		t.Fatalf("page %v, want the Camera setup page", a.topPage())
	}
	if f := simFrame(t, a, "camera-setup"); !strings.Contains(f, "B0561 2") || !strings.Contains(f, "PICTURE CAMERA") {
		t.Error("no camera or no roles on the Setup page")
	}
	tapLabel(t, a, cv, "Main camera")
	if cam.Set.Main != cam.Cams[0].Dev.ID {
		t.Errorf("main %q", cam.Set.Main)
	}
	tapLabel(t, a, cv, "Main camera")
	if cam.Set.Main != cam.Cams[1].Dev.ID {
		t.Errorf("main %q", cam.Set.Main)
	}
	tapLabel(t, a, cv, "Main camera")
	if cam.Set.Main != "" {
		t.Errorf("main %q, want auto", cam.Set.Main)
	}
	tapLabel(t, a, cv, "Time lapse camera")
	tapLabel(t, a, cv, "Time lapse camera")
	if cam.Set.Lapse != cam.Cams[1].Dev.ID {
		t.Errorf("lapse %q", cam.Set.Lapse)
	}
	tapLabel(t, a, cv, "On")
	if !cam.Set.Camera(cam.Cams[1].Dev.ID).Off {
		t.Error("camera 2 is not off")
	}
	if s := cam.Cams[1].Status(); s.State != "off" {
		t.Errorf("state %+v", s)
	}
	tapLabel(t, a, cv, "On")
	tapLabel(t, a, cv, "Name")
	if a.camKB == nil {
		t.Fatal("no keyboard")
	}
	simFrame(t, a, "camera-name")
	a.key("", "Backspace")
	a.key("x", "")
	a.key("", "Enter")
	if n := cam.Set.Camera(cam.Cams[1].Dev.ID).Name; n != "B0561 x" {
		t.Errorf("name %q", n)
	}
	a.back()

	// With no camera, the page shows the state and gets no picture.
	a.camID = ""
	for _, c := range cam.Cams {
		c.State = camera.Status{State: "none"}
		c.Peeks = 0
	}
	if f := simFrame(t, a, "camera-none"); !strings.Contains(f, "NOT CONNECTED") || cam.Cams[0].Peeks != 0 {
		t.Errorf("no camera: peeks %d", cam.Cams[0].Peeks)
	}
	cam.Cams[0].State = camera.Status{State: "nosignal", Device: cam.Cams[0].Dev}
	if f := simFrame(t, a, "camera-nosignal"); !strings.Contains(f, "NO SIGNAL") {
		t.Error("no signal: not on the page")
	}
	cam.Cams[0].State = camera.Status{State: "error", Device: cam.Cams[0].Dev, Err: "the camera sends no pictures"}
	if f := simFrame(t, a, "camera-error"); !strings.Contains(f, "THE CAMERA SENDS NO PICTURES") {
		t.Error("no error")
	}
}

// The wide Camera page has the settings in its row of actions.
func TestCameraPageWide(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	a.Wide = true
	cam := NewDemoCamera()
	cam.Set.SetCamera(camera.CamSettings{ID: cam.Cams[0].Dev.ID, Rotate: 180})
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	f := simFrame(t, a, "camera-wide")
	if !strings.Contains(f, `["c",`) || !strings.Contains(f, `"TURN"`) || !strings.Contains(f, `"SETUP"`) {
		t.Error("no picture, no Turn key or no Setup key")
	}
	a.camID = camAll
	if f := simFrame(t, a, "camera-all-wide"); strings.Count(f, `["c",`) < 2 {
		t.Error("not two pictures")
	}
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-camera-wide"); !strings.Contains(f, "2 cameras: B0561 1, B0561 2") {
		t.Error("no Camera row in Settings")
	}
}

func TestCameraLine(t *testing.T) {
	cam := NewDemoCamera()
	devs := cam.Devices()
	off := camera.Settings{}
	off.SetCamera(camera.CamSettings{ID: devs[1].ID, Off: true})
	for _, tc := range []struct {
		s    camera.Settings
		devs []camera.Device
		want string
	}{
		{camera.Settings{}, nil, "No camera found"},
		{camera.Settings{}, devs, "2 cameras: B0561 1, B0561 2"},
		{camera.Settings{}, devs[:1], "B0561"},
		{camera.Settings{AllOff: true}, devs, "Off"},
		{off, devs, "B0561 1"},
		{camera.Settings{Cameras: []camera.CamSettings{{ID: "/dev/v4l/by-id/usb-Elgato_Cam_Link_4K-video-index0"}}}, nil,
			"Elgato Cam Link 4K (not connected)"},
	} {
		if got := cameraLine(camera.List(tc.s, tc.devs)); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.s, got, tc.want)
		}
	}
}
