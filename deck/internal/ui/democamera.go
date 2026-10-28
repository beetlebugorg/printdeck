package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"

	"printdeck/internal/camera"
)

// DemoCamera is the cameras of a deck for the demo, the manual and the tests: two cameras of one
// model, with drawn pictures of a print bed. The first camera looks at the bed from the front,
// and the second looks down on it.
type DemoCamera struct {
	Set  camera.Settings
	Cams []*DemoSource
}

// DemoSource is one camera of a DemoCamera.
type DemoSource struct {
	Dev   camera.Device
	State camera.Status
	Frame *camera.Frame
	Peeks int // the calls of Peek
	// Cal is the calibration, or nil. With a calibration, each view is the middle half of the
	// picture.
	Cal *camera.Calibration

	set *DemoCamera
}

// NewDemoCamera returns two demo cameras that are on, with pictures of 1280 x 720.
func NewDemoCamera() *DemoCamera {
	format := camera.Format{In: "MJPEG", InW: 1280, InH: 720, OutW: 1280, OutH: 720, OutFPS: 15}
	c := &DemoCamera{}
	for i, dev := range []camera.Device{
		{Path: "/dev/video0", ID: "/dev/v4l/by-path/platform-xhci-hcd.0-usb-0:1:1.0-video-index0", Name: "B0561"},
		{Path: "/dev/video2", ID: "/dev/v4l/by-path/platform-xhci-hcd.1-usb-0:1:1.0-video-index0", Name: "B0561"},
	} {
		c.Cams = append(c.Cams, &DemoSource{Dev: dev, Frame: demoPicture(i == 1), set: c,
			State: camera.Status{State: "on", Device: dev, Format: format, Focus: &camera.Focus{Max: 1023, Step: 1, Auto: true, Pos: 410}}})
	}
	// The first camera has a digital PTZ, with the ranges of the test camera of the hub.
	ptz := DemoPTZ
	ptz.Pos = ptz.Wide()
	c.Cams[0].State.PTZ = &ptz
	return c
}

// DemoPTZ is the PTZ control of the first demo camera, and DemoPTZMap its measured map: a zoom
// factor of the zoom value / 100.
var (
	DemoPTZ = camera.PTZ{Zoom: camera.PTZRange{Min: 100, Max: 400, Step: 1, Default: 100},
		Pan:  camera.PTZRange{Min: -36000, Max: 36000, Step: 100},
		Tilt: camera.PTZRange{Min: -36000, Max: 36000, Step: 100}}
	DemoPTZMap = camera.PTZMap{Zoom: [][2]float64{{100, 1}, {400, 4}}, Pan: [2]float64{1.0 / 96000, 0},
		Tilt: [2]float64{0, -1.0 / 72000}}
)

// MeasurePTZ sets DemoPTZMap as the map of the camera id, as camera.Hub.MeasurePTZ does after
// a measurement.
func (c *DemoCamera) MeasurePTZ(ctx context.Context, id string, progress func(done, all int)) (*camera.PTZMap, error) {
	m := DemoPTZMap
	m.Zoom = append([][2]float64(nil), m.Zoom...)
	cs := c.Set.Camera(id)
	m.Rotate = cs.Rotate
	cs.PTZMap = &m
	c.Set.SetCamera(cs)
	if progress != nil {
		progress(1, 1)
	}
	return &m, nil
}

// demoPicture draws a print bed with a print on it, from the front, or from the top with top.
func demoPicture(top bool) *camera.Frame {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := range 720 {
		for x := range 1280 {
			c := color.RGBA{60, 64, 70, 255} // the frame of the printer
			bed := x > 240 && x < 1040 && y > 160
			if top {
				bed = x > 280 && x < 1000
			}
			if bed {
				c = color.RGBA{40, 40, 44, 255}
				if x%80 == 0 || y%80 == 0 {
					c = color.RGBA{200, 200, 200, 255}
				}
			}
			cy, r := 420, 90
			if top {
				cy, r = 360, 120
			}
			if (x-640)*(x-640)+(y-cy)*(y-cy) < r*r {
				c = color.RGBA{240, 120, 30, 255} // a print
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	f, _ := camera.FrameOf(b.Bytes())
	return f
}

func (c *DemoCamera) Settings() camera.Settings { return c.Set }

// SetSettings sets the settings, with the model name of each camera, as camera.Hub.SetSettings.
func (c *DemoCamera) SetSettings(s camera.Settings) {
	for i, cs := range s.Cameras {
		for _, d := range c.Cams {
			if d.Dev.ID == cs.ID {
				s.Cameras[i].Model = d.Dev.Name
			}
		}
	}
	c.Set = s
}

// Devices returns the devices of the cameras that are connected.
func (c *DemoCamera) Devices() []camera.Device {
	var out []camera.Device
	for _, s := range c.Cams {
		if s.State.State != "none" {
			out = append(out, s.Dev)
		}
	}
	return out
}

func (c *DemoCamera) Cameras() []camera.Info { return camera.List(c.Set, c.Devices()) }

// Source returns the camera of a selector, as camera.Hub.Source.
func (c *DemoCamera) Source(sel string) (camera.Source, bool) {
	id := ""
	switch sel {
	case "", camera.RoleMain, camera.RoleLapse, camera.RolePicture:
		role := sel
		if role == "" {
			role = camera.RoleMain
		}
		for _, in := range c.Cameras() {
			if in.HasRole(role) {
				id = in.ID
			}
		}
	default:
		for _, in := range c.Cameras() {
			if in.ID == sel || in.Device.Is(sel) || sel == strconv.Itoa(in.Number) {
				id = in.ID
			}
		}
		if id == "" {
			return nil, false
		}
	}
	for _, s := range c.Cams {
		if s.Dev.ID == id {
			return s, true
		}
	}
	return &DemoSource{set: c, State: camera.Status{State: "none"}}, true
}

func (s *DemoSource) ID() string { return s.Dev.ID }

func (s *DemoSource) On() bool {
	st := s.Status().State
	return st != "off" && st != "none"
}

// Status returns the state of the camera. The PTZ position is the position of the settings, as
// the capture of camera.Hub applies it.
func (s *DemoSource) Status() camera.Status {
	st := s.State
	for _, in := range s.set.Cameras() {
		if in.ID == s.Dev.ID {
			st.Name = in.Name
			if in.Off {
				return camera.Status{State: "off", Name: in.Name}
			}
		}
	}
	if st.PTZ != nil {
		p := *st.PTZ
		cs := s.set.Set.Camera(s.Dev.ID)
		p.Pos, p.Mapped = p.Target(cs.PTZ), cs.PTZMap.ValidAt(cs.Rotate)
		st.PTZ = &p
	}
	return st
}

func (s *DemoSource) Peek() *camera.Frame { s.Peeks++; return s.Frame }
func (s *DemoSource) Watch() func()       { return func() {} }

func (s *DemoSource) Next(ctx context.Context, after uint64) (*camera.Frame, error) {
	return s.Frame, nil
}

func (s *DemoSource) Snapshot(ctx context.Context) (*camera.Frame, error) {
	if !s.On() {
		return nil, camera.ErrNoCamera
	}
	return s.Frame, nil
}

func (s *DemoSource) Calibration() (*camera.Calibration, bool) { return s.Cal, s.Cal != nil }
func (s *DemoSource) SetCalibration(cal *camera.Calibration)   { s.Cal = cal }

func (s *DemoSource) Views() []string {
	if s.Cal == nil {
		return []string{camera.ViewFull}
	}
	return camera.ViewNames
}

func (s *DemoSource) View(f *camera.Frame, view string, w int) (*camera.Frame, string) {
	if s.Cal == nil || view == camera.ViewFull || view == "" || f == nil {
		if w > 0 && f != nil {
			return f.Small(w), camera.ViewFull
		}
		return f, camera.ViewFull
	}
	sz := f.Size()
	v, _ := f.Crop(float64(sz.X)/2, float64(sz.Y)/2, float64(sz.X)/2, w)
	return v, view
}
