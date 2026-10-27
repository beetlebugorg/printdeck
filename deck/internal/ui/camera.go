package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The Camera page (Settings > Camera): the setup of the cameras of the deck. It shows the picture
// of one camera, to aim the camera at the bed, or the pictures of all cameras side by side.
// Camera shows the next camera. Turn, View and Calibrate are for the camera that the page shows.
// Setup opens the Camera setup page: the camera on or off, its name, and with two cameras or more,
// the roles. The roles choose the main camera (the webcam of the apps and of Cura), the camera of
// the time lapse, and the camera of the pictures (the first layer and the notifications). The live
// picture is for the remote viewers (the webcam of the API); this page shows a few frames a second
// while it is open. The calibration (calibrate.go) lets the views of a camera follow the printer,
// and the View row shows each view in the picture of the page, to check them.

// Camera is the cameras of the deck (camera.Hub).
type Camera interface {
	// Cameras returns the cameras, in the order of the screen.
	Cameras() []camera.Info
	// Source returns the camera of a selector: "" for the main camera, a role, a number or an ID.
	Source(sel string) (camera.Source, bool)
	Settings() camera.Settings
	SetSettings(camera.Settings)
}

// cameraEvery is the time between two pictures of the Camera page.
const cameraEvery = 300 * time.Millisecond

// camAll is the value of App.camID for the pictures of all cameras side by side.
const camAll = "*all"

// foundCameras returns the cameras that the deck finds.
func foundCameras(list []camera.Info) []camera.Info {
	var out []camera.Info
	for _, in := range list {
		if in.Found {
			out = append(out, in)
		}
	}
	return out
}

// shownCam returns the camera that the page shows (a.camID, or the main camera), with its place
// in the list. The camera is a fixed camera, also when the page shows the main camera.
func (a *App) shownCam() (camera.Source, camera.Info) {
	sel := a.camID
	if sel == camAll {
		sel = ""
	}
	src, ok := a.Camera.Source(sel)
	if !ok {
		a.camID = ""
		src, _ = a.Camera.Source("")
	}
	id := src.ID()
	if id == "" {
		// No main camera: the first camera of the list. It is off or the deck does not find it.
		if list := a.Camera.Cameras(); len(list) > 0 {
			id = list[0].ID
		}
	}
	for _, in := range a.Camera.Cameras() {
		if in.ID == id {
			if s, ok := a.Camera.Source(id); ok {
				src = s
			}
			return src, in
		}
	}
	return src, camera.Info{}
}

// cameraLine is the line of the Camera row of the Settings page.
func cameraLine(list []camera.Info) string {
	var on []string
	for _, in := range list {
		if in.Usable() {
			on = append(on, in.Name)
		}
	}
	switch {
	case len(list) == 0:
		return "No camera found"
	case len(list) == 1 && !list[0].Found:
		return list[0].Name + " (not connected)"
	case len(on) == 0:
		return "Off"
	case len(on) == 1:
		return on[0]
	}
	return fmt.Sprintf("%d cameras: %s", len(on), strings.Join(on, ", "))
}

// camLine is the line of the Camera row of the page: the camera that the page shows.
func (a *App) camLine(in camera.Info, n int) string {
	switch {
	case a.camID == camAll:
		return "All cameras, side by side"
	case in.ID == "":
		return "No camera found"
	case n < 2:
		return camName(in)
	}
	return fmt.Sprintf("%s · %d of %d", camName(in), in.Number, n)
}

// camKey is the label of the Camera key of the wide page: the name of the camera.
func camKey(in camera.Info) string {
	if in.ID == "" {
		return "No camera"
	}
	return in.Name
}

// camName is the name of a camera, with its state when the deck cannot use it.
func camName(in camera.Info) string {
	switch {
	case !in.Found:
		return in.Name + " (not connected)"
	case in.Off:
		return in.Name + " (off)"
	}
	return in.Name
}

// cycleCamera shows the next camera: each camera, all cameras side by side, and the first camera
// again.
func (a *App) cycleCamera() {
	list := a.Camera.Cameras()
	if len(list) == 0 {
		a.Toast("No camera found")
		return
	}
	choices := []string{}
	for _, in := range list {
		choices = append(choices, in.ID)
	}
	if len(foundCameras(list)) > 1 {
		choices = append(choices, camAll)
	}
	now := a.camID
	if now != camAll {
		_, in := a.shownCam()
		now = in.ID
	}
	next := choices[0]
	for i, c := range choices {
		if c == now {
			next = choices[(i+1)%len(choices)]
		}
	}
	a.camID = next
	_, in := a.shownCam()
	a.Toast("Camera: " + a.camLine(in, len(list)))
}

// setCamera changes the settings of the camera that the page shows.
func (a *App) setCamera(change func(*camera.CamSettings)) (camera.CamSettings, bool) {
	_, in := a.shownCam()
	if in.ID == "" {
		return camera.CamSettings{}, false
	}
	s := a.Camera.Settings()
	cs := s.Camera(in.ID)
	change(&cs)
	s.SetCamera(cs)
	a.Camera.SetSettings(s)
	return cs, true
}

// onLine is the line of the On row.
func onLine(in camera.Info) string {
	if in.Off {
		return "Off · the apps and the time lapse do not use it"
	}
	return "On"
}

// toggleCamOn turns the camera that the page shows on or off.
func (a *App) toggleCamOn() {
	if cs, ok := a.setCamera(func(cs *camera.CamSettings) { cs.Off = !cs.Off }); ok {
		a.Toast("Camera: " + map[bool]string{true: "off", false: "on"}[cs.Off])
	}
}

// renameCamera opens the keyboard for the name of the camera that the page shows. An empty name
// is the name of the device.
func (a *App) renameCamera() {
	_, in := a.shownCam()
	if in.ID == "" {
		return
	}
	a.camKB = &keyboard{title: "Name of the camera", value: in.Name, max: 24, ok: "OK", cancel: "Back",
		back: func() { a.camKB = nil },
		done: func(v string) {
			a.camKB = nil
			a.setCamera(func(cs *camera.CamSettings) { cs.Name = strings.TrimSpace(v) })
		}}
}

// turnLine is the line of the Turn row.
func turnLine(rotate int) string {
	if rotate == 180 {
		return "Turned 180°, for a camera upside down"
	}
	return "Upright"
}

// toggleTurn turns the picture of the camera that the page shows 180°, or back.
func (a *App) toggleTurn() {
	if cs, ok := a.setCamera(func(cs *camera.CamSettings) { cs.Rotate = 180 - cs.Rotate }); ok {
		a.Toast("Picture: " + turnLine(cs.Rotate))
	}
}

// roleTitles are the names of the roles on the screen.
var roleTitles = map[string]string{camera.RoleMain: "Main camera", camera.RoleLapse: "Time lapse camera",
	camera.RolePicture: "Picture camera"}

// roleLine is the line of a role row: the camera of the role now.
func roleLine(list []camera.Info, s camera.Settings, role string) string {
	for _, in := range list {
		if in.HasRole(role) {
			switch {
			case s.Role(role) != "" && s.Role(role) == in.ID:
				return in.Name
			case role == camera.RoleMain:
				return "Auto: " + in.Name + ", the first camera that is on"
			}
			return "Auto: " + in.Name + ", the main camera"
		}
	}
	return "No camera"
}

// cycleRole gives a role to the next camera that is on: Auto, each camera, and Auto again.
func (a *App) cycleRole(role string) {
	list := a.Camera.Cameras()
	s := a.Camera.Settings()
	choices := []string{""}
	for _, in := range list {
		if in.Usable() {
			choices = append(choices, in.ID)
		}
	}
	next := ""
	for i, c := range choices {
		if c == s.Role(role) {
			next = choices[(i+1)%len(choices)]
		}
	}
	s.SetRole(role, next)
	a.Camera.SetSettings(s)
	a.Toast(roleTitles[role] + ": " + roleLine(a.Camera.Cameras(), s, role))
}

// roleItems are the rows of the roles, for a deck with two cameras or more.
func (a *App) roleItems(roles ...string) []menuItem {
	list := a.Camera.Cameras()
	if len(list) < 2 {
		return nil
	}
	s := a.Camera.Settings()
	var items []menuItem
	for _, role := range roles {
		items = append(items, menuItem{roleTitles[role], roleLine(list, s, role), dim, func() { a.cycleRole(role) }})
	}
	return items
}

// viewTitles are the names of the views on the screen.
var viewTitles = map[string]string{camera.ViewFull: "Whole picture", camera.ViewToolhead: "Toolhead",
	camera.ViewFirstLayer: "First layer", camera.ViewPart: "Part"}

// viewLine is the line of the View row: the view of the picture of the page.
func (a *App) viewLine(cam camera.Source) string {
	v := a.camView
	if v == "" {
		v = camera.ViewFull
	}
	line := viewTitles[v]
	if v == camera.ViewFull {
		return line
	}
	for _, n := range cam.Views() {
		if n == v {
			return line
		}
	}
	return line + ": no place now, whole picture"
}

// cycleView shows the next view in the picture of the page. With no calibration, the picture is
// the whole picture.
func (a *App) cycleView() {
	cam, _ := a.shownCam()
	if _, ok := cam.Calibration(); !ok {
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
	name := st.Name
	if name == "" {
		name = st.Device.Name
	}
	switch st.State {
	case "off":
		if name != "" {
			return name + ": off", "Turn it on with the On row."
		}
		return "Camera off", "The webcam of the apps is the screen."
	case "none":
		if name != "" {
			return name + ": not connected", "Plug the camera into the deck."
		}
		return "No camera", "Plug a USB camera into the deck."
	case "error":
		return "Camera error", st.Err
	case "on":
		return name, st.Format.String()
	case "nosignal":
		return "No signal", "The picture is blank. Check the camera and its cable."
	}
	return name, "Starting the camera…"
}

// drawCamera is the Camera page.
func (a *App) drawCamera(c *gfx.Canvas) {
	if a.camKB != nil {
		a.drawKeyboard(c, a.camKB)
		return
	}
	f := a.navPage(c, "Camera")
	list := a.Camera.Cameras()
	if a.camID == camAll && len(foundCameras(list)) < 2 {
		a.camID = ""
	}
	if a.camID == camAll {
		a.drawAllCameras(c, f, list)
		return
	}
	cam, in := a.shownCam()
	st := cam.Status()
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
		sizes = append(sizes, kit.Fr(2))
		k := kit.Cols(keys, 8, sizes...)
		a.button(c, k[0], gfx.Fit(gfx.Face(true, 18), camKey(in), k[0].Dx()-40), raised, text, a.cycleCamera)
		fill, ink := keyOff, text
		if in.Rotate == 180 {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, k[1], "Turn", fill, ink, a.toggleTurn)
		a.button(c, k[2], "View", raised, text, a.cycleView)
		a.button(c, k[3], "Calibrate", raised, text, a.askCalibrate)
		if calibrated {
			a.button(c, k[4], "Clear", raised, text, a.askClearCalibration)
		}
		a.button(c, k[len(k)-1], "Setup", raised, text, func() { a.open(pageCameraSetup) })
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
			{"Camera", a.camLine(in, len(list)), dim, a.cycleCamera},
			{"Turn", turnLine(in.Rotate), dim, a.toggleTurn},
			{"View", a.viewLine(cam), dim, a.cycleView},
			{"Calibrate", calibLine(cal, calibrated), dim, a.askCalibrate},
		}
		if calibrated {
			items = append(items, menuItem{"Clear calibration", "The views show the whole picture", dim, a.askClearCalibration})
		}
		items = append(items, menuItem{"Setup", setupLine(list), dim, func() { a.open(pageCameraSetup) }})
		a.menuRows(c, r[2], false, 60, 5, items)
	}

	// The state of the camera, in a window under the picture.
	window(c, info)
	a.region("State", info)
	title, line := cameraState(st)
	in2 := kit.Inset(info, 14, 4, 14, 4)
	if f.wide {
		rows := kit.Rows(in2, 0, kit.Fr(1), kit.Fr(1))
		c.Text(rows[0], strings.ToUpper(gfx.Fit(gfx.Face(true, 14), title+" · "+line, in2.Dx())), gfx.Face(true, 14), lcdInk, gfx.Left)
		c.Text(rows[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 13), calibLine(cal, calibrated)+" · "+a.viewLine(cam), in2.Dx())), gfx.Face(true, 13), lcdDim, gfx.Left)
	} else {
		rows := kit.Rows(in2, 0, kit.Fr(1), kit.Fr(1))
		c.Text(rows[0], strings.ToUpper(gfx.Fit(gfx.Face(true, 16), title, in2.Dx())), gfx.Face(true, 16), lcdInk, gfx.Left)
		c.Text(rows[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 13), line, in2.Dx())), gfx.Face(true, 13), lcdDim, gfx.Left)
	}

	a.region("Picture", view)
	a.drawCamPicture(c, view, cam, fr, a.camView, title)
}

// drawCamPicture draws the frame fr of the camera cam in a window, as large as it fits, or the
// text with no frame.
func (a *App) drawCamPicture(c *gfx.Canvas, view image.Rectangle, cam camera.Source, fr *camera.Frame, viewName, title string) {
	window(c, view)
	pic := view.Inset(4)
	if fr == nil {
		c.Text(pic, strings.ToUpper(gfx.Fit(gfx.Face(true, 18), title, pic.Dx())), gfx.Face(true, 18), lcdDim, gfx.Center)
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
	v, _ := cam.View(fr, viewName, w)
	c.Cover(r, v)
}

// drawAllCameras is the Camera page with the pictures of all cameras that the deck finds, side by
// side on a wide screen and one over the other on a tall screen. A tap on a picture shows that
// camera alone.
func (a *App) drawAllCameras(c *gfx.Canvas, f frame, list []camera.Info) {
	found := foundCameras(list)
	var area image.Rectangle
	if f.wide {
		content, keys := f.split()
		area = content
		k := kit.Cols(keys, 8, kit.Fr(3), kit.Fr(2))
		a.button(c, k[0], "All cameras", raised, text, a.cycleCamera)
		a.button(c, k[1], "Setup", raised, text, func() { a.open(pageCameraSetup) })
		a.region("Camera", k[0])
	} else {
		body := kit.Inset(f.body(), 0, 10, 0, 0)
		// Each picture has the shape of 16:9 at the width of the page, in at most 2/3 of the page.
		h := min(len(found)*(body.Dx()*9/16+40), body.Dy()*2/3)
		r := kit.Rows(body, 12, kit.Px(h), kit.Fr(1))
		area = r[0]
		a.menuRows(c, r[1], false, 60, 5, []menuItem{{"Camera", a.camLine(camera.Info{}, len(list)), dim, a.cycleCamera},
			{"Setup", setupLine(list), dim, func() { a.open(pageCameraSetup) }}})
	}
	a.region("Pictures", area)
	sizes := make([]kit.Size, len(found))
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	// The pictures in a row or in a column: the layout with the larger pictures of 16:9.
	n := max(1, len(found))
	cw := (area.Dx() - 8*(n-1)) / n
	colH := min(area.Dy()-26, cw*9/16)
	rowH := (area.Dy()-8*(n-1))/n - 26
	colW := min(area.Dx(), rowH*16/9)
	var cells []image.Rectangle
	if cw*colH > colW*rowH {
		cells = kit.Cols(area, 8, sizes...)
	} else {
		cells = kit.Rows(area, 8, sizes...)
	}
	for i, in := range found {
		cam, ok := a.Camera.Source(in.ID)
		if !ok {
			continue
		}
		st := cam.Status()
		var fr *camera.Frame
		if st.State != "off" && st.State != "none" {
			fr = cam.Peek()
			a.redrawSoon(cameraEvery)
		}
		title, _ := cameraState(st)
		parts := kit.Rows(cells[i], 4, kit.Fr(1), kit.Px(22))
		a.drawCamPicture(c, parts[0], cam, fr, camera.ViewFull, title)
		label := in.Name
		if in.HasRole(camera.RoleMain) {
			label += " · main"
		}
		c.Text(parts[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 14), label, parts[1].Dx())), gfx.Face(true, 14), dim, gfx.Center)
		id := in.ID
		a.addHit(kit.Hit{R: cells[i], Tap: func() { a.camID = id; a.Redraw() }})
	}
}

// setupLine is the line of the Setup row.
func setupLine(list []camera.Info) string {
	if len(list) > 1 {
		return "On or off, the name, and the roles of the cameras"
	}
	return "On or off, and the name"
}

// drawCameraSetup is the Camera setup page: the camera on or off, its name, and the roles of the
// cameras.
func (a *App) drawCameraSetup(c *gfx.Canvas) {
	if a.camKB != nil {
		a.drawKeyboard(c, a.camKB)
		return
	}
	f := a.navPage(c, "Camera setup")
	var items []menuItem
	if a.camID == camAll {
		a.camID = ""
	}
	if _, in := a.shownCam(); in.ID != "" {
		items = append(items,
			menuItem{"Camera", a.camLine(in, len(a.Camera.Cameras())), dim, a.cycleCamera},
			menuItem{"On", onLine(in), dim, a.toggleCamOn},
			menuItem{"Name", in.Name, dim, a.renameCamera})
	}
	items = append(items, a.roleItems(camera.Roles...)...)
	if len(items) == 0 {
		items = append(items, menuItem{"Camera", "No camera found", dim, nil})
	}
	a.menuRows(c, f.body(), f.wide, 60, 5, items)
}
