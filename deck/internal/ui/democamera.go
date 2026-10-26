package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"

	"printdeck/internal/camera"
)

// DemoCamera is a camera with two devices and a drawn picture of a print bed, for the demo, the
// manual and the tests.
type DemoCamera struct {
	Set   camera.Settings
	State camera.Status
	Frame *camera.Frame
	Peeks int // the calls of Peek
	// Cal is the calibration, or nil. With a calibration, each view is the middle half of the
	// picture.
	Cal *camera.Calibration
}

// NewDemoCamera returns a demo camera that is on, with a picture of 1280 x 720.
func NewDemoCamera() *DemoCamera {
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
	f, _ := camera.FrameOf(b.Bytes())
	return &DemoCamera{Frame: f, State: camera.Status{State: "on", Device: camera.Device{Name: "C270 HD WEBCAM"},
		Format: camera.Format{In: "MJPEG", InW: 1280, InH: 720, OutW: 1280, OutH: 720, OutFPS: 15}}}
}

func (c *DemoCamera) Peek() *camera.Frame           { c.Peeks++; return c.Frame }
func (c *DemoCamera) Status() camera.Status         { return c.State }
func (c *DemoCamera) Settings() camera.Settings     { return c.Set }
func (c *DemoCamera) SetSettings(s camera.Settings) { c.Set = s }
func (c *DemoCamera) Devices() []camera.Device {
	return []camera.Device{{Path: "/dev/video0", ID: "/dev/v4l/by-id/usb-Logitech_C270-video-index0", Name: "C270 HD WEBCAM"},
		{Path: "/dev/video2", ID: "/dev/v4l/by-id/usb-Elgato_Cam_Link_4K-video-index0", Name: "Cam Link 4K"}}
}

func (c *DemoCamera) Calibration() (*camera.Calibration, bool) { return c.Cal, c.Cal != nil }
func (c *DemoCamera) SetCalibration(cal *camera.Calibration)   { c.Cal = cal }

func (c *DemoCamera) Views() []string {
	if c.Cal == nil {
		return []string{camera.ViewFull}
	}
	return camera.ViewNames
}

func (c *DemoCamera) View(f *camera.Frame, view string, w int) (*camera.Frame, string) {
	if c.Cal == nil || view == camera.ViewFull || view == "" || f == nil {
		if w > 0 {
			return f.Small(w), camera.ViewFull
		}
		return f, camera.ViewFull
	}
	sz := f.Size()
	v, _ := f.Crop(float64(sz.X)/2, float64(sz.Y)/2, float64(sz.X)/2, w)
	return v, view
}
