package ui

import (
	"image"
	"path/filepath"
	"strings"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The Camera page (Settings > Camera): the setup of the camera of the deck. It shows the picture
// of the camera, to aim the camera at the bed, the camera that the deck uses, the turn of the
// picture, and the state of the camera. The live picture is for the remote viewers (the webcam of
// the API); this page shows a few frames a second while it is open. The calibration
// (calibrate.go) lets the views of the camera follow the printer, and the View row shows each
// view in the picture of the page, to check them.

// Camera is the camera of the deck (camera.Hub).
type Camera interface {
	// Peek returns the newest frame, or nil, and keeps the camera on for a few seconds.
	Peek() *camera.Frame
	Status() camera.Status
	Settings() camera.Settings
	SetSettings(camera.Settings)
	Devices() []camera.Device
	// View returns the view of a frame at the width w, and the name of the view that it shows.
	View(f *camera.Frame, view string, w int) (*camera.Frame, string)
	// Views returns the views that show a part of the picture now, with full first.
	Views() []string
	// Calibration returns the calibration, or nil, and reports that it is valid for the camera.
	Calibration() (*camera.Calibration, bool)
	// SetCalibration saves a calibration, or removes it with nil.
	SetCalibration(*camera.Calibration)
}

// cameraEvery is the time between two pictures of the Camera page.
const cameraEvery = 300 * time.Millisecond

// cameraName is the name of the camera of a setting: the name of the device when the deck finds
// it, or the name of its link.
func cameraName(id string, devs []camera.Device) string {
	for _, d := range devs {
		if d.ID == id || d.Path == id {
			return d.Name
		}
	}
	name := strings.TrimPrefix(filepath.Base(id), "usb-")
	if i := strings.LastIndex(name, "-video-index"); i > 0 {
		name = name[:i]
	}
	return strings.ReplaceAll(name, "_", " ")
}

// cameraLine is the line of the Camera row of the Settings page, and of the camera choice.
func cameraLine(s camera.Settings, devs []camera.Device) string {
	switch {
	case s.Device == camera.Off:
		return "Off"
	case s.Device == "" && len(devs) == 0:
		return "Auto: no camera found"
	case s.Device == "":
		return "Auto: " + devs[0].Name
	}
	name := cameraName(s.Device, devs)
	for _, d := range devs {
		if d.ID == s.Device || d.Path == s.Device {
			return name
		}
	}
	return name + " (not connected)"
}

// cycleCamera chooses the next camera: Auto, each camera that the deck finds, Off, and Auto again.
func (a *App) cycleCamera() {
	s := a.Camera.Settings()
	devs := a.Camera.Devices()
	choices := []string{""}
	for _, d := range devs {
		choices = append(choices, d.ID)
	}
	choices = append(choices, camera.Off)
	next := ""
	for i, c := range choices {
		if c == s.Device {
			next = choices[(i+1)%len(choices)]
			break
		}
	}
	s.Device = next
	a.Camera.SetSettings(s)
	a.Toast("Camera: " + cameraLine(s, devs))
}

// turnLine is the line of the Turn row.
func turnLine(rotate int) string {
	if rotate == 180 {
		return "Turned 180°, for a camera upside down"
	}
	return "Upright"
}

// toggleTurn turns the picture 180°, or back.
func (a *App) toggleTurn() {
	s := a.Camera.Settings()
	s.Rotate = 180 - s.Rotate
	a.Camera.SetSettings(s)
	a.Toast("Picture: " + turnLine(s.Rotate))
}

// viewTitles are the names of the views on the screen.
var viewTitles = map[string]string{camera.ViewFull: "Whole picture", camera.ViewToolhead: "Toolhead",
	camera.ViewFirstLayer: "First layer", camera.ViewPart: "Part"}

// viewLine is the line of the View row: the view of the picture of the page.
func (a *App) viewLine() string {
	v := a.camView
	if v == "" {
		v = camera.ViewFull
	}
	line := viewTitles[v]
	if v == camera.ViewFull {
		return line
	}
	for _, n := range a.Camera.Views() {
		if n == v {
			return line
		}
	}
	return line + ": no place now, whole picture"
}

// cycleView shows the next view in the picture of the page. With no calibration, the picture is
// the whole picture.
func (a *App) cycleView() {
	if _, ok := a.Camera.Calibration(); !ok {
		a.camView = camera.ViewFull
		a.Toast("Calibrate the camera for the other views")
		return
	}
	next := camera.ViewNames[0]
	for i, n := range camera.ViewNames {
		if n == a.camView || (a.camView == "" && n == camera.ViewFull) {
			next = camera.ViewNames[(i+1)%len(camera.ViewNames)]
		}
	}
	a.camView = next
	a.Toast("View: " + viewTitles[next])
}

// cameraState is the state of the camera in words: a title, and a line under it.
func cameraState(st camera.Status) (title, line string) {
	switch st.State {
	case "off":
		return "Camera off", "The webcam of the apps is the screen."
	case "none":
		return "No camera", "Plug a USB camera into the deck."
	case "error":
		return "Camera error", st.Err
	case "on":
		return st.Device.Name, st.Format.String()
	case "nosignal":
		return "No signal", "The picture is blank. Check the camera and its cable."
	}
	return st.Device.Name, "Starting the camera…"
}

// drawCamera is the Camera page.
func (a *App) drawCamera(c *gfx.Canvas) {
	f := a.navPage(c, "Camera")
	cam := a.Camera
	st := cam.Status()
	s := cam.Settings()
	devs := cam.Devices()
	cal, calibrated := cam.Calibration()
	var fr *camera.Frame
	if st.State != "off" && st.State != "none" {
		fr = cam.Peek()
		a.redrawSoon(cameraEvery)
	}
	var view, info image.Rectangle
	if f.wide {
		content, keys := f.split()
		r := kit.Rows(content, 8, kit.Fr(1), kit.Px(52))
		view, info = r[0], r[1]
		sizes := []kit.Size{kit.Fr(3), kit.Fr(2), kit.Fr(2), kit.Fr(2)}
		if calibrated {
			sizes = append(sizes, kit.Fr(2))
		}
		k := kit.Cols(keys, 8, sizes...)
		a.button(c, k[0], gfx.Fit(gfx.Face(true, 18), cameraLine(s, devs), k[0].Dx()-40), raised, text, a.cycleCamera)
		fill, ink := keyOff, text
		if s.Rotate == 180 {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, k[1], "Turn", fill, ink, a.toggleTurn)
		a.button(c, k[2], "View", raised, text, a.cycleView)
		a.button(c, k[3], "Calibrate", raised, text, a.askCalibrate)
		if calibrated {
			a.button(c, k[4], "Clear", raised, text, a.askClearCalibration)
		}
		// The names of the rows of the tall page, for the manual.
		a.region("Camera", k[0])
	} else {
		body := f.body()
		// The picture has the shape of the camera, at the width of the page.
		h := body.Dx() * 9 / 16
		if fr != nil && fr.Size().X > 0 {
			h = body.Dx() * fr.Size().Y / fr.Size().X
		}
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Px(min(h, body.Dy()/2)), kit.Px(64), kit.Fr(1))
		view, info = r[0], r[1]
		items := []menuItem{
			{"Camera", cameraLine(s, devs), dim, a.cycleCamera},
			{"Turn", turnLine(s.Rotate), dim, a.toggleTurn},
			{"View", a.viewLine(), dim, a.cycleView},
			{"Calibrate", calibLine(cal, calibrated), dim, a.askCalibrate},
		}
		if calibrated {
			items = append(items, menuItem{"Clear calibration", "The views show the whole picture", dim, a.askClearCalibration})
		}
		a.menuRows(c, r[2], false, 60, 5, items)
	}

	// The state of the camera, in a window under the picture.
	window(c, info)
	a.region("State", info)
	title, line := cameraState(st)
	in := kit.Inset(info, 14, 4, 14, 4)
	if f.wide {
		rows := kit.Rows(in, 0, kit.Fr(1), kit.Fr(1))
		c.Text(rows[0], strings.ToUpper(gfx.Fit(gfx.Face(true, 14), title+" · "+line, in.Dx())), gfx.Face(true, 14), lcdInk, gfx.Left)
		c.Text(rows[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 13), calibLine(cal, calibrated)+" · "+a.viewLine(), in.Dx())), gfx.Face(true, 13), lcdDim, gfx.Left)
	} else {
		rows := kit.Rows(in, 0, kit.Fr(1), kit.Fr(1))
		c.Text(rows[0], strings.ToUpper(gfx.Fit(gfx.Face(true, 16), title, in.Dx())), gfx.Face(true, 16), lcdInk, gfx.Left)
		c.Text(rows[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 13), line, in.Dx())), gfx.Face(true, 13), lcdDim, gfx.Left)
	}

	// The picture.
	window(c, view)
	a.region("Picture", view)
	pic := view.Inset(4)
	if fr == nil {
		c.Text(pic, strings.ToUpper(title), gfx.Face(true, 18), lcdDim, gfx.Center)
		return
	}
	// The whole picture, as large as it fits. The frame is as wide as the picture in pixels of the
	// screen of the deck (1.5 times the UI on the Touch Display 2), so that a remote viewer loads a
	// small JPEG.
	sz := fr.Size()
	k := min(float64(pic.Dx())/float64(sz.X), float64(pic.Dy())/float64(sz.Y))
	r := kit.Center(pic, int(float64(sz.X)*k), int(float64(sz.Y)*k))
	w := r.Dx()
	if a.Scale[1] > 0 {
		w = w * a.Scale[0] / a.Scale[1]
	}
	v, _ := cam.View(fr, a.camView, w)
	c.Cover(r, v)
}
