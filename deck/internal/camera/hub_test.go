package camera

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The two cameras of the tests: two cameras of the same model in two USB ports. udev makes one
// by-id link for the two.
var (
	camA = Device{Path: "/dev/video0", ID: "/dev/v4l/by-path/usb-0:1-video-index0", Alias: "/dev/v4l/by-id/usb-Arducam_B0561-video-index0", Name: "B0561"}
	camB = Device{Path: "/dev/video2", ID: "/dev/v4l/by-path/usb-0:2-video-index0", Alias: "/dev/v4l/by-id/usb-Arducam_B0561-video-index0", Name: "B0561"}
)

// twoHub is a hub with two fake cameras that make a frame each 10 ms. running counts the
// captures of each device by ID.
func twoHub(t *testing.T, s Settings) (*Hub, func(id string) int) {
	linger = 300 * time.Millisecond
	t.Cleanup(func() { linger = 8 * time.Second })
	var mu sync.Mutex
	running := map[string]int{}
	h := NewHub(s)
	h.scan = func() []Device { return []Device{camB, camA} }
	jb := testJPEG(t)
	h.capture = func(ctx context.Context, d Device, s CamSettings, format func(Format), emit func(*Frame)) error {
		mu.Lock()
		running[d.ID]++
		mu.Unlock()
		defer func() {
			mu.Lock()
			running[d.ID]--
			mu.Unlock()
		}()
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
	return h, func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return running[id]
	}
}

// Each camera has a capture of its own, which runs only while a viewer watches that camera.
func TestTwoCameras(t *testing.T) {
	h, running := twoHub(t, Settings{})
	list := h.Cameras()
	if len(list) != 2 || list[0].ID != camA.ID || list[1].ID != camB.ID {
		t.Fatalf("list %+v", list)
	}
	if list[0].Name != "B0561 1" || list[1].Name != "B0561 2" || list[0].Number != 1 || list[1].Number != 2 {
		t.Errorf("names %q %q", list[0].Name, list[1].Name)
	}
	if !reflect.DeepEqual(list[0].Roles, Roles) || len(list[1].Roles) != 0 {
		t.Errorf("roles %v %v", list[0].Roles, list[1].Roles)
	}

	b, ok := h.Source("2")
	if !ok || b.ID() != camB.ID {
		t.Fatalf("camera 2: %v", b)
	}
	stop := b.Watch()
	ctx := context.Background()
	fb, err := b.Next(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fb.cam != camB.ID || running(camB.ID) != 1 || running(camA.ID) != 0 {
		t.Errorf("camera 2 only: frame of %q, running %d %d", fb.cam, running(camA.ID), running(camB.ID))
	}
	// The main camera runs at the same time.
	fa, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fa.cam != camA.ID || running(camA.ID) != 1 || running(camB.ID) != 1 {
		t.Errorf("both cameras: frame of %q, running %d %d", fa.cam, running(camA.ID), running(camB.ID))
	}
	if st := b.Status(); st.State != "on" || st.Name != "B0561 2" || st.Device.ID != camB.ID {
		t.Errorf("status of camera 2: %+v", st)
	}
	stop()
	waitFor(t, "stop", func() bool { return running(camA.ID) == 0 && running(camB.ID) == 0 })

	// A camera that goes off stops, and its frames stop.
	stop = b.Watch()
	defer stop()
	waitFor(t, "start", func() bool { return running(camB.ID) == 1 })
	s := h.Settings()
	s.SetCamera(CamSettings{ID: camB.ID, Off: true})
	h.SetSettings(s)
	waitFor(t, "stop of the camera that is off", func() bool { return running(camB.ID) == 0 })
	if b.On() || b.Status().State != "off" {
		t.Errorf("camera 2 is on: %+v", b.Status())
	}
	if _, err := b.Snapshot(ctx); err != ErrNoCamera {
		t.Errorf("snapshot of a camera that is off: %v", err)
	}
}

// The roles go to the first camera that is on, or to the camera of the settings. A role camera
// that is off gives its role to the main camera.
func TestRoles(t *testing.T) {
	h, _ := twoHub(t, Settings{})
	role := func(r string) string { return h.Role(r).ID() }
	if role(RoleMain) != camA.ID || role(RoleLapse) != camA.ID || role(RolePicture) != camA.ID {
		t.Errorf("default roles: %s %s %s", role(RoleMain), role(RoleLapse), role(RolePicture))
	}
	h.SetSettings(Settings{Lapse: camB.ID})
	if role(RoleLapse) != camB.ID || role(RolePicture) != camA.ID {
		t.Errorf("lapse on camera 2: %s %s", role(RoleLapse), role(RolePicture))
	}
	h.SetSettings(Settings{Main: camB.ID})
	if role(RoleMain) != camB.ID || role(RolePicture) != camB.ID || h.Status().Device.ID != camB.ID {
		t.Errorf("main camera 2: %s %s", role(RoleMain), role(RolePicture))
	}
	s := Settings{Lapse: camB.ID}
	s.SetCamera(CamSettings{ID: camB.ID, Off: true})
	h.SetSettings(s)
	if role(RoleLapse) != camA.ID {
		t.Errorf("lapse on a camera that is off: %s", role(RoleLapse))
	}
	s.SetCamera(CamSettings{ID: camA.ID, Off: true})
	h.SetSettings(s)
	if h.On() || h.Status().State != "off" || role(RoleLapse) != "" {
		t.Errorf("all cameras off: %+v", h.Status())
	}
}

// A selector of the API is a role, a number, an ID, a path or an alias.
func TestSource(t *testing.T) {
	h, _ := twoHub(t, Settings{})
	for sel, want := range map[string]string{"": camA.ID, "main": camA.ID, "lapse": camA.ID, "picture": camA.ID,
		"1": camA.ID, "2": camB.ID, camB.ID: camB.ID, camB.Path: camB.ID, camA.Alias: camA.ID} {
		s, ok := h.Source(sel)
		if !ok || s.ID() != want {
			t.Errorf("%q: %v %v, want %s", sel, s, ok, want)
		}
	}
	for _, sel := range []string{"0", "3", "nope"} {
		if _, ok := h.Source(sel); ok {
			t.Errorf("%q is a camera", sel)
		}
	}
}

// A name of the settings replaces the name of the device. A camera of the settings that the deck
// does not find is at the end of the list.
func TestCameraList(t *testing.T) {
	s := Settings{Cameras: []CamSettings{{ID: "/dev/v4l/by-id/usb-Logitech_C270-video-index0"}, {ID: camB.ID, Name: "Top"}}}
	list := List(s, []Device{camA, camB})
	if len(list) != 3 {
		t.Fatalf("list %+v", list)
	}
	if list[0].ID != camA.ID || list[0].Name != "B0561 1" {
		t.Errorf("first %+v", list[0])
	}
	if list[1].ID != camB.ID || list[1].Name != "Top" || !list[1].Found {
		t.Errorf("second %+v", list[1])
	}
	if list[2].Name != "Logitech C270" || list[2].Found {
		t.Errorf("third %+v", list[2])
	}
	if !list[0].HasRole(RoleMain) || list[1].HasRole(RoleMain) {
		t.Errorf("roles %+v", list)
	}
}

// Two cameras of one model with one by-id link get their by-path links as IDs.
func TestAssignIDs(t *testing.T) {
	devs := []Device{{Path: "/dev/video0", Name: "B0561"}, {Path: "/dev/video2", Name: "B0561"},
		{Path: "/dev/video4", Name: "C270"}, {Path: "/dev/video6", Name: "Cam Link 4K"}}
	byID := map[string]string{"/dev/video2": "/dev/v4l/by-id/usb-B0561-video-index0", "/dev/video4": "/dev/v4l/by-id/usb-C270-video-index0"}
	byPath := map[string]string{"/dev/video0": "/dev/v4l/by-path/usb-0:1-video-index0", "/dev/video2": "/dev/v4l/by-path/usb-0:2-video-index0",
		"/dev/video4": "/dev/v4l/by-path/usb-0:3-video-index0"}
	got := assignIDs(devs, byID, byPath)
	want := []Device{
		{Path: "/dev/video0", ID: "/dev/v4l/by-path/usb-0:1-video-index0", Alias: "/dev/v4l/by-id/usb-B0561-video-index0", Name: "B0561"},
		{Path: "/dev/video2", ID: "/dev/v4l/by-path/usb-0:2-video-index0", Alias: "/dev/v4l/by-id/usb-B0561-video-index0", Name: "B0561"},
		{Path: "/dev/video4", ID: "/dev/v4l/by-id/usb-C270-video-index0", Name: "C270"},
		{Path: "/dev/video6", ID: "/dev/video6", Name: "Cam Link 4K"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// Two cameras with two by-id links keep them.
	byID["/dev/video0"] = "/dev/v4l/by-id/usb-B0561_SN1-video-index0"
	got = assignIDs(devs[:2], byID, byPath)
	if got[0].ID != byID["/dev/video0"] || got[1].ID != byID["/dev/video2"] || got[0].Alias != "" {
		t.Errorf("two links: %+v", got)
	}
}

// The settings and the calibration of an older deck go to the camera that they name, with no
// loss: the turn, the main camera, and the calibration.
func TestHubMigration(t *testing.T) {
	saved := make(chan Settings, 4)
	savedCal := make(chan []*Calibration, 4)
	start := func(file string, cal *Calibration) *Hub {
		h := NewHub(ParseSettings([]byte(file)))
		h.scan = func() []Device { return []Device{camA, camB} }
		h.OnSettings = func(s Settings) { saved <- s }
		h.OnCalibration = func(c []*Calibration) { savedCal <- c }
		if cal != nil {
			h.LoadCalibration(cal)
		}
		return h
	}
	// Auto with a turn and a calibration of the second camera, by its path.
	cal := &Calibration{Device: camB.Path, InW: 40, InH: 24, Width: 40, Height: 24, P: [12]float64{1}}
	h := start("auto\n180\n", cal)
	list := h.Cameras()
	want := Settings{Cameras: []CamSettings{{ID: camB.ID, Rotate: 180}}, Main: camB.ID}
	if s := h.Settings(); !reflect.DeepEqual(s, want) {
		t.Errorf("settings %+v, want %+v", s, want)
	}
	if list[1].ID != camB.ID || list[1].Rotate != 180 || !list[1].HasRole(RoleMain) {
		t.Errorf("list %+v", list)
	}
	if c, _ := h.Cam(camB.ID).Calibration(); c == nil || c.Device != camB.ID {
		t.Errorf("calibration %+v", c)
	}
	if c, _ := h.Cam(camA.ID).Calibration(); c != nil {
		t.Errorf("a calibration of the other camera: %+v", c)
	}
	select {
	case s := <-saved:
		if !reflect.DeepEqual(s, want) {
			t.Errorf("saved %+v", s)
		}
	case <-time.After(time.Second):
		t.Error("the settings were not saved")
	}
	if c := <-savedCal; len(c) != 1 || c[0].Device != camB.ID {
		t.Errorf("saved calibrations %+v", c)
	}

	// One camera by its by-id link: the alias of the cameras.
	h = start(camA.Alias+"\n180\n", nil)
	h.Devices()
	if s := h.Settings(); s.Main != camA.ID || len(s.Cameras) != 1 || s.Cameras[0].ID != camA.ID || s.Cameras[0].Rotate != 180 {
		t.Errorf("settings %+v", s)
	}
	<-saved

	// Auto with no calibration: the first camera in the order of the paths.
	h = start("auto\n0\n", nil)
	h.scan = func() []Device { return []Device{{Path: "/dev/video4", ID: "a"}, {Path: "/dev/video2", ID: "b"}} }
	if st := h.Status(); st.Device.ID != "b" || h.Settings().Main != "b" {
		t.Errorf("auto: %+v %+v", st, h.Settings())
	}
	<-saved

	// Off: all cameras are off.
	h = start("off\n0\n", nil)
	if h.On() || len(h.Cameras()) != 2 || h.Cameras()[1].Off != true {
		t.Errorf("off: %+v", h.Cameras())
	}
}

// Each camera has its calibration, in one file.
func TestCalibrations(t *testing.T) {
	h, _ := twoHub(t, Settings{})
	var saved []*Calibration
	h.OnCalibration = func(c []*Calibration) { saved = c }
	ca := &Calibration{InW: 40, InH: 24, Width: 40, Height: 24, P: [12]float64{1}}
	cb := &Calibration{InW: 40, InH: 24, Width: 40, Height: 24, P: [12]float64{2}}
	h.Cam(camA.ID).SetCalibration(ca)
	h.Cam(camB.ID).SetCalibration(cb)
	if len(saved) != 2 || saved[0].Device != camA.ID || saved[1].Device != camB.ID {
		t.Fatalf("saved %+v", saved)
	}
	got, err := ParseCalibrations(CalibrationsBytes(saved))
	if err != nil || len(got) != 2 || got[1].P[0] != 2 {
		t.Errorf("read back: %+v %v", got, err)
	}
	// The file of an older deck has one calibration.
	got, err = ParseCalibrations(ca.Bytes())
	if err != nil || len(got) != 1 || got[0].Device != camA.ID {
		t.Errorf("one calibration: %+v %v", got, err)
	}
	h.Cam(camA.ID).SetCalibration(nil)
	if len(saved) != 1 || saved[0].Device != camB.ID {
		t.Errorf("after clear: %+v", saved)
	}
	if _, ok := h.Calibration(); ok {
		t.Error("the main camera is calibrated")
	}
}
