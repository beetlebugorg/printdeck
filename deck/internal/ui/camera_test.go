package ui

import (
	"slices"
	"strings"
	"testing"

	"printdeck/internal/camera"
)

// Settings opens the Camera page: a tab for each camera, the main camera first, and Both. Turn
// changes the camera of the tab.
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
	if !strings.Contains(f, "MJPEG 1280×720, 15 FPS") || !strings.Contains(f, `"BOTH"`) {
		t.Error("no format of the camera, or no Both tab")
	}
	// The second camera is the main camera: its tab is the first.
	s := cam.Settings()
	s.Main = cam.Cams[1].Dev.ID
	cam.SetSettings(s)
	if tabs := camTabs(cam.Cameras()); len(tabs) != 2 || tabs[0].ID != cam.Cams[1].Dev.ID {
		t.Errorf("tabs %+v", tabs)
	}
	tapLabel(t, a, cv, "B0561 1")
	if a.camID != cam.Cams[0].Dev.ID {
		t.Fatalf("tab %q", a.camID)
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

	// Both: both pictures, and a tap on a picture opens its tab.
	tapLabel(t, a, cv, "Both")
	cam.Cams[0].Peeks, cam.Cams[1].Peeks = 0, 0
	f = simFrame(t, a, "camera-all")
	if a.camID != camAll || cam.Cams[0].Peeks == 0 || cam.Cams[1].Peeks == 0 || strings.Count(f, `["c",`) < 2 {
		t.Error("not two pictures")
	}
	for _, n := range a.names {
		if n.label == lcdText("Pictures") {
			tapAt(a, cv, n.r.Min.X+20, n.r.Min.Y+20)
		}
	}
	if a.camID == camAll {
		t.Error("a tap on a picture does not open its tab")
	}

	// Camera settings opens the page of the camera of the tab, and Turn and calibrate there goes
	// back to the Camera page.
	a.camID = cam.Cams[0].Dev.ID
	tapLabel(t, a, cv, "Camera settings")
	if a.topPage() != pageCameraEdit || a.camEdit != cam.Cams[0].Dev.ID {
		t.Fatalf("page %v, camera %q", a.topPage(), a.camEdit)
	}
	tapLabel(t, a, cv, "Turn and calibrate")
	if a.topPage() != pageCamera || len(a.pages) != 3 {
		t.Fatalf("pages %v", a.pages)
	}

	// A camera that goes off has no tab: the page shows the other camera, with no tabs.
	cam.Set.SetCamera(camera.CamSettings{ID: cam.Cams[0].Dev.ID, Off: true})
	if f := simFrame(t, a, "camera-one-on"); strings.Contains(f, `"BOTH"`) || !strings.Contains(f, "B0561 2") {
		t.Error("tabs with one camera on")
	}
	if _, in := a.shownCam(); in.ID != cam.Cams[1].Dev.ID {
		t.Errorf("shown %q", in.ID)
	}
	cam.Set = camera.Settings{}

	// With no camera, the page shows the state and gets no picture.
	a.camID = ""
	for _, c := range cam.Cams {
		c.State = camera.Status{State: "none"}
		c.Peeks = 0
	}
	if f := simFrame(t, a, "camera-none"); !strings.Contains(f, "NO CAMERA") || strings.Contains(f, "TURN") || cam.Cams[0].Peeks != 0 {
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

// Settings opens the Cameras list: a row for each camera, with its picture and its uses. A tap
// opens the page of the camera.
func TestCamerasList(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cam := NewDemoCamera()
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Cameras")
	if a.topPage() != pageCameras {
		t.Fatalf("page %v, want the Cameras page", a.topPage())
	}
	f := simFrame(t, a, "cameras")
	if strings.Count(f, `["c",`) != 2 || !strings.Contains(f, "Apps, Time lapse,") || !strings.Contains(f, "On · not in use") {
		t.Error("no pictures or no uses")
	}
	if !strings.Contains(f, "such as Top") {
		t.Error("no hint for the names")
	}
	tapLabel(t, a, cv, "B0561 2")
	if a.topPage() != pageCameraEdit || a.camEdit != cam.Cams[1].Dev.ID {
		t.Fatalf("page %v, camera %q", a.topPage(), a.camEdit)
	}
	tapLabel(t, a, cv, "Name")
	if a.camKB == nil {
		t.Fatal("no keyboard")
	}
	simFrame(t, a, "camera-name")
	for range len("B0561 2") {
		a.key("", "Backspace")
	}
	for _, r := range "Top" {
		a.key(string(r), "")
	}
	a.key("", "Enter")
	if n := cam.Set.Camera(cam.Cams[1].Dev.ID).Name; n != "Top" {
		t.Errorf("name %q", n)
	}
	a.back()
	if f := simFrame(t, a, "cameras-named"); !strings.Contains(f, `"TOP"`) {
		t.Error("no new name")
	}
	cam.Set.SetCamera(camera.CamSettings{ID: cam.Cams[0].Dev.ID, Name: "Front"})
	if f := simFrame(t, a, "cameras-named"); strings.Contains(f, "such as Top") {
		t.Error("a hint with names")
	}

	// One camera: one row, no hint.
	cam.Cams[1].State = camera.Status{State: "none"}
	cam.Set = camera.Settings{}
	if f := simFrame(t, a, "cameras-one"); strings.Count(f, `["c",`) != 1 || strings.Contains(f, "such as Top") {
		t.Error("not one row")
	}
}

// The page of a camera: a check on a use moves it from the other camera, and a camera that goes
// off gives its uses to the other camera.
func TestCameraEdit(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cam := NewDemoCamera()
	a.Camera = cam
	one, two := cam.Cams[0].Dev.ID, cam.Cams[1].Dev.ID
	a.camEdit = two
	a.pages = []page{pageMachine, pageSettings, pageCameras, pageCameraEdit}
	roles := func() map[string][]string {
		out := map[string][]string{}
		for _, in := range cam.Cameras() {
			out[in.ID] = in.Roles
		}
		return out
	}
	f := simFrame(t, a, "camera-settings")
	if !strings.Contains(f, "USE FOR") || !strings.Contains(f, "Cura and the apps show it first") {
		t.Error("no uses, or no line for Apps")
	}
	if strings.Contains(strings.ToLower(f), "auto") {
		t.Error("Auto on the page")
	}
	tapLabel(t, a, cv, "Time lapse")
	if got := roles(); !slices.Equal(got[two], []string{camera.RoleLapse}) || !slices.Equal(got[one], []string{camera.RoleMain, camera.RolePicture}) {
		t.Errorf("time lapse to camera 2: %v", got)
	}
	// The main camera moves, and the pictures stay on camera 1.
	tapLabel(t, a, cv, "Apps")
	if got := roles(); !slices.Equal(got[two], []string{camera.RoleMain, camera.RoleLapse}) || !slices.Equal(got[one], []string{camera.RolePicture}) {
		t.Errorf("apps to camera 2: %v", got)
	}
	// A check that is on stays on.
	tapLabel(t, a, cv, "Apps")
	if got := roles(); !slices.Equal(got[two], []string{camera.RoleMain, camera.RoleLapse}) {
		t.Errorf("apps off: %v", got)
	}
	// Camera 2 goes off: its uses go to camera 1, and stay there when it goes on again.
	tapLabel(t, a, cv, "On")
	if !cam.Set.Camera(two).Off || cam.Cams[1].Status().State != "off" {
		t.Error("camera 2 is not off")
	}
	if got := roles(); !slices.Equal(got[one], camera.Roles) {
		t.Errorf("camera 2 off: %v", got)
	}
	if f := simFrame(t, a, "camera-settings-off"); !strings.Contains(f, "Turn the camera on to use it") {
		t.Error("no note for a camera that is off")
	}
	tapRegion(t, a, cv, "Time lapse")
	if got := roles(); len(got[two]) != 0 {
		t.Errorf("a use on a camera that is off: %v", got)
	}
	tapLabel(t, a, cv, "On")
	if got := roles(); cam.Set.Camera(two).Off || len(got[two]) != 0 {
		t.Errorf("camera 2 on: %v", got)
	}
	if strings.Contains(strings.ToLower(string(cam.Set.Bytes())), "auto") {
		t.Error("auto in the settings")
	}

	// One camera: no uses.
	cam.Cams[1].State = camera.Status{State: "none"}
	cam.Set = camera.Settings{}
	a.camEdit = one
	if f := simFrame(t, a, "camera-settings-one"); strings.Contains(f, "USE FOR") {
		t.Error("uses with one camera")
	}
}

// The wide Camera page has the tabs at the top and the actions in its row of keys.
func TestCameraPageWide(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	a.Wide = true
	cam := NewDemoCamera()
	cam.Set.SetCamera(camera.CamSettings{ID: cam.Cams[0].Dev.ID, Rotate: 180})
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	f := simFrame(t, a, "camera-wide")
	if !strings.Contains(f, `["c",`) || !strings.Contains(f, `"TURN"`) || !strings.Contains(f, `"CAMERA SETTINGS"`) || !strings.Contains(f, `"BOTH"`) {
		t.Error("no picture, no Turn key, no Camera settings key or no tabs")
	}
	a.camID = camAll
	if f := simFrame(t, a, "camera-all-wide"); strings.Count(f, `["c",`) < 2 {
		t.Error("not two pictures")
	}
	a.pages = []page{pageMachine, pageSettings, pageCameras}
	if f := simFrame(t, a, "cameras-wide"); strings.Count(f, `["c",`) != 2 {
		t.Error("not two pictures in the list")
	}
	a.camEdit = cam.Cams[1].Dev.ID
	a.pages = append(a.pages, pageCameraEdit)
	if f := simFrame(t, a, "camera-settings-wide"); !strings.Contains(f, "USE FOR") {
		t.Error("no uses")
	}
	a.pages = []page{pageMachine, pageSettings}
	if f := simFrame(t, a, "settings-camera-wide"); !strings.Contains(f, "2 cameras: B0561 1, B0561 2") {
		t.Error("no Cameras row in Settings")
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

// Focus shows the focus keys. A step turns off the autofocus and moves the lens from the position
// of the autofocus. Auto turns the autofocus on again.
func TestCameraFocus(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	cam := NewDemoCamera()
	a.Camera = cam
	a.pages = []page{pageMachine, pageSettings, pageCamera}
	id := cam.Cams[0].Dev.ID
	tapLabel(t, a, cv, "Focus")
	if !a.camFocus {
		t.Fatal("no focus keys")
	}
	tapLabel(t, a, cv, "+")
	tapLabel(t, a, cv, "+")
	if cs := cam.Set.Camera(id); !cs.FixedFocus || cs.Focus != 410+2*20 {
		t.Fatalf("after two steps: %+v", cs)
	}
	tapLabel(t, a, cv, "Auto")
	if cs := cam.Set.Camera(id); cs.FixedFocus {
		t.Fatalf("after Auto: %+v", cs)
	}
	tapLabel(t, a, cv, "Done")
	if a.camFocus {
		t.Error("focus keys after Done")
	}
	// A camera with no focus control has no Focus key.
	cam.Cams[0].State.Focus = nil
	if f := simFrame(t, a, "camera-nofocus"); strings.Contains(f, `"FOCUS"`) {
		t.Error("Focus key with no focus control")
	}
}
