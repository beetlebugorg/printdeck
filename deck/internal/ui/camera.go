package ui

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The camera pages. Settings has two rows for them:
//
//   - Camera opens the Camera page: the live picture, with a tab for each camera that is on and a
//     tab for all of them. Turn, View and Calibrate change the camera of the tab. Camera settings
//     opens the page of that camera.
//   - Cameras opens the list of the cameras, with a small picture of each and its uses. A tap on a
//     camera opens its page.
//
// The page of one camera has its name, On, and with two cameras or more, the uses: Apps, Time
// lapse and Pictures. Each use is on one camera. A check on one camera moves the use from the
// other camera. A camera that goes off gives its uses to another camera that is on. The pictures
// are for the remote viewers too (the webcam of the API). The Camera page shows a few frames a
// second, and the small pictures one frame a second, only while the page is open. The
// calibration (calibrate.go) lets the views of a camera follow the printer, and the View row
// shows each view in the picture.

// Camera is the cameras of the deck (camera.Hub).
type Camera interface {
	// Cameras returns the cameras, in the order of the screen.
	Cameras() []camera.Info
	// Source returns the camera of a selector: "" for the main camera, a role, a number or an ID.
	Source(sel string) (camera.Source, bool)
	Settings() camera.Settings
	SetSettings(camera.Settings)
}

// cameraEvery is the time between two pictures of the Camera page, and thumbEvery the time
// between two small pictures of the Cameras list and of the page of one camera.
const cameraEvery, thumbEvery = 300 * time.Millisecond, time.Second

// camAll is the value of App.camID for the tab with the pictures of all cameras.
const camAll = "*all"

// camTabs returns the cameras of the tabs of the Camera page: each camera that is on, the main
// camera first and then the others in the order of the list.
func camTabs(list []camera.Info) []camera.Info {
	var main, rest []camera.Info
	for _, in := range list {
		switch {
		case !in.Usable():
		case in.HasRole(camera.RoleMain):
			main = append(main, in)
		default:
			rest = append(rest, in)
		}
	}
	return append(main, rest...)
}

// shownCam returns the camera of the tab of the Camera page (a.camID, or the main camera). With
// no camera on, it is the first camera of the list, which is off or not connected.
func (a *App) shownCam() (camera.Source, camera.Info) {
	list := a.Camera.Cameras()
	sel := a.camID
	if sel == camAll {
		sel = ""
	}
	if tabs := camTabs(list); sel != "" && len(tabs) > 0 {
		// A camera that went off has no tab: the main camera.
		if _, ok := camInfo(tabs, sel); !ok {
			sel = ""
		}
	}
	src, ok := a.Camera.Source(sel)
	if !ok {
		a.camID = ""
		src, _ = a.Camera.Source("")
	}
	id := src.ID()
	if id == "" && len(list) > 0 {
		// No main camera: the first camera of the list. It is off or the deck does not find it.
		id = list[0].ID
	}
	if in, ok := camInfo(list, id); ok {
		if s, ok := a.Camera.Source(id); ok {
			src = s
		}
		return src, in
	}
	return src, camera.Info{}
}

// cameraLine is the line of the Cameras row of the Settings page: the cameras that are on.
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

// pictureLine is the line of the Camera row of the Settings page.
func pictureLine(list []camera.Info) string {
	if len(list) == 0 {
		return "No camera found"
	}
	return "See the picture, turn and calibrate"
}

// roleTitles are the names of the roles on the screen. The main camera is "Apps": the camera that
// the apps show first.
var roleTitles = map[string]string{camera.RoleMain: "Apps", camera.RoleLapse: "Time lapse",
	camera.RolePicture: "Pictures"}

// roleLines are the lines under the roles on the page of a camera.
var roleLines = map[string]string{
	camera.RoleMain:    "Cura and the apps show it first",
	camera.RoleLapse:   "The frames of the time lapse video",
	camera.RolePicture: "The first layer and phone pictures",
}

// usesLine is the line of a camera in the Cameras list: its uses, or its state.
func usesLine(in camera.Info) string {
	switch {
	case !in.Found:
		return "Not connected"
	case in.Off:
		return "Off"
	}
	var uses []string
	for _, role := range camera.Roles {
		if in.HasRole(role) {
			uses = append(uses, roleTitles[role])
		}
	}
	if len(uses) == 0 {
		return "On · not in use"
	}
	return strings.Join(uses, ", ")
}

// camInfo returns the camera id of the list.
func camInfo(list []camera.Info, id string) (camera.Info, bool) {
	for _, in := range list {
		if in.ID == id {
			return in, true
		}
	}
	return camera.Info{}, false
}

// openCamEdit opens the page of the camera id. When that page opened the Camera page, it goes
// back to it.
func (a *App) openCamEdit(id string) {
	if n := len(a.pages); n >= 2 && a.pages[n-2] == pageCameraEdit && a.camEdit == id {
		a.back()
		return
	}
	a.camEdit = id
	a.open(pageCameraEdit)
}

// openCamPicture opens the Camera page on the tab of the camera id. When the Camera page opened
// the page of the camera, it goes back to the Camera page.
func (a *App) openCamPicture(id string) {
	a.camID = id
	if n := len(a.pages); n >= 2 && a.pages[n-2] == pageCamera {
		a.back()
		return
	}
	a.open(pageCamera)
}

// changeCamera changes the settings of the camera id.
func (a *App) changeCamera(id string, change func(*camera.CamSettings)) camera.CamSettings {
	s := a.Camera.Settings()
	cs := s.Camera(id)
	change(&cs)
	s.SetCamera(cs)
	a.Camera.SetSettings(s)
	return cs
}

// toggleCamOn turns the camera id on or off. A camera that goes off gives its uses to another
// camera that is on.
func (a *App) toggleCamOn(id string) {
	list := a.Camera.Cameras()
	in, ok := camInfo(list, id)
	if !ok {
		return
	}
	s := a.Camera.Settings()
	moved, to := s.SetOff(list, id, !in.Off)
	a.Camera.SetSettings(s)
	switch {
	case !in.Off && len(moved) > 0:
		other, _ := camInfo(list, to)
		var uses []string
		for _, r := range moved {
			uses = append(uses, roleTitles[r])
		}
		a.Toast(fmt.Sprintf("%s is off. %s moved to %s", in.Name, strings.Join(uses, ", "), other.Name))
	case !in.Off:
		a.Toast(in.Name + " is off")
	default:
		a.Toast(in.Name + " is on")
	}
}

// giveRole gives a role to the camera id. A role stays on one camera: to move it, check it on the
// other camera.
func (a *App) giveRole(id, role string) {
	list := a.Camera.Cameras()
	in, _ := camInfo(list, id)
	if in.HasRole(role) {
		a.Toast("To move " + roleTitles[role] + ", check it on another camera")
		return
	}
	from := ""
	for _, o := range list {
		if o.HasRole(role) {
			from = o.Name
		}
	}
	s := a.Camera.Settings()
	s.GiveRole(list, role, id)
	a.Camera.SetSettings(s)
	if from != "" {
		a.Toast(fmt.Sprintf("%s: from %s to %s", roleTitles[role], from, in.Name))
	}
}

// renameCamera opens the keyboard for the name of the camera id. An empty name is the name of the
// device.
func (a *App) renameCamera(id string) {
	in, ok := camInfo(a.Camera.Cameras(), id)
	if !ok {
		return
	}
	a.camKB = &keyboard{title: "Name of the camera", value: in.Name, max: 24, ok: "OK", cancel: "Back",
		back: func() { a.camKB = nil },
		done: func(v string) {
			a.camKB = nil
			a.changeCamera(id, func(cs *camera.CamSettings) { cs.Name = strings.TrimSpace(v) })
		}}
}

// turnLine is the line of the Turn row.
func turnLine(rotate int) string {
	if rotate == 180 {
		return "Turned 180°, for a camera upside down"
	}
	return "Upright"
}

// toggleTurn turns the picture of the camera of the tab 180°, or back.
func (a *App) toggleTurn() {
	_, in := a.shownCam()
	if in.ID == "" {
		return
	}
	cs := a.changeCamera(in.ID, func(cs *camera.CamSettings) { cs.Rotate = 180 - cs.Rotate })
	a.Toast("Picture: " + turnLine(cs.Rotate))
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
			return name + ": off", "Turn it on in Camera settings."
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

// camFrame returns the newest frame of a camera that is on, or nil. A camera that is on draws the
// page again after every.
func (a *App) camFrame(cam camera.Source, st camera.Status, every time.Duration) *camera.Frame {
	if st.State == "off" || st.State == "none" {
		return nil
	}
	a.redrawSoon(every)
	return cam.Peek()
}

// drawCamTabs draws the tabs of the Camera page: a key for each camera that is on, and a key for
// all of them. The key of the tab that shows is lit.
func (a *App) drawCamTabs(c *gfx.Canvas, r image.Rectangle, tabs []camera.Info, shown string) {
	a.region("Tabs", r)
	// The tabs of the cameras are wider than the tab of all cameras, for the names.
	sizes := make([]kit.Size, len(tabs)+1)
	for i := range tabs {
		sizes[i] = kit.Fr(3)
	}
	sizes[len(tabs)] = kit.Fr(2)
	keys := kit.Cols(r, 8, sizes...)
	for i, in := range tabs {
		fill, ink := keyOff, text
		if a.camID != camAll && in.ID == shown {
			fill, ink = keyOn, keyOnInk
		}
		id := in.ID
		a.button(c, keys[i], gfx.Fit(gfx.Face(true, 13), in.Name, keys[i].Dx()-24), fill, ink, func() { a.camID = id })
	}
	all := "Both"
	if len(tabs) > 2 {
		all = "All"
	}
	fill, ink := keyOff, text
	if a.camID == camAll {
		fill, ink = keyOn, keyOnInk
	}
	a.button(c, keys[len(tabs)], all, fill, ink, func() { a.camID = camAll })
}

// drawCamera is the Camera page.
func (a *App) drawCamera(c *gfx.Canvas) {
	f := a.navPage(c, "Camera")
	list := a.Camera.Cameras()
	tabs := camTabs(list)
	if a.camID == camAll && len(tabs) < 2 {
		a.camID = ""
	}
	cam, in := a.shownCam()
	body, keys := f.body(), image.Rectangle{}
	if f.wide {
		body, keys = f.split()
	} else {
		body = kit.Inset(body, 0, 10, 0, 0)
	}
	if len(tabs) > 1 {
		h := 52
		if f.wide {
			h = 44
		}
		r := kit.Rows(body, 10, kit.Px(h), kit.Fr(1))
		a.drawCamTabs(c, r[0], tabs, in.ID)
		body = r[1]
	}
	if a.camID == camAll {
		if f.wide {
			k := kit.Cols(keys, 8, kit.Fr(3), kit.Fr(2))
			a.button(c, k[1], "Cameras", raised, text, func() { a.open(pageCameras) })
		} else {
			r := kit.Rows(body, 12, kit.Fr(1), kit.Px(72))
			body = r[0]
			a.menuRow(c, r[1], "Cameras", cameraLine(list), dim, func() { a.open(pageCameras) })
		}
		a.drawAllCameras(c, body, tabs)
		return
	}
	st := cam.Status()
	cal, calibrated := cam.Calibration()
	fr := a.camFrame(cam, st, cameraEvery)
	edit := func() {
		if in.ID != "" {
			a.openCamEdit(in.ID)
		}
	}
	var view, info image.Rectangle
	title, line := cameraState(st)
	if f.wide {
		r := kit.Rows(body, 8, kit.Fr(1), kit.Px(52))
		view, info = r[0], r[1]
		sizes := []kit.Size{kit.Fr(2), kit.Fr(2), kit.Fr(2)}
		if calibrated {
			sizes = append(sizes, kit.Fr(2))
		}
		sizes = append(sizes, kit.Fr(4))
		k := kit.Cols(keys, 8, sizes...)
		fill, ink := keyOff, text
		if in.Rotate == 180 {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, k[0], "Turn", fill, ink, a.toggleTurn)
		a.button(c, k[1], "View", raised, text, a.cycleView)
		a.button(c, k[2], "Calibrate", raised, text, a.askCalibrate)
		if calibrated {
			a.button(c, k[3], "Clear", raised, text, a.askClearCalibration)
		}
		a.button(c, k[len(k)-1], "Camera settings", raised, text, edit)
		window(c, info)
		in2 := kit.Inset(info, 14, 4, 14, 4)
		rows := kit.Rows(in2, 0, kit.Fr(1), kit.Fr(1))
		c.Text(rows[0], strings.ToUpper(gfx.Fit(gfx.Face(true, 14), title+" · "+line, in2.Dx())), gfx.Face(true, 14), lcdInk, gfx.Left)
		c.Text(rows[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 13), calibLine(cal, calibrated)+" · "+a.viewLine(cam), in2.Dx())), gfx.Face(true, 13), lcdDim, gfx.Left)
	} else {
		items := []menuItem{
			{"Turn", turnLine(in.Rotate), dim, a.toggleTurn},
			{"View", a.viewLine(cam), dim, a.cycleView},
			{"Calibrate", calibLine(cal, calibrated), dim, a.askCalibrate},
		}
		if calibrated {
			items = append(items, menuItem{"Clear calibration", "The views show the whole picture", dim, a.askClearCalibration})
		}
		if in.ID != "" {
			items = append(items, menuItem{"Camera settings", camSettingsLine(in, len(list)), dim, edit})
		} else {
			// No camera: no rows.
			items = nil
		}
		// The picture has the shape of the camera, at the width of the page, in the space that
		// the rows leave.
		h := body.Dx() * 9 / 16
		if fr != nil && fr.Size().X > 0 {
			h = body.Dx() * fr.Size().Y / fr.Size().X
		}
		const infoH, gap = 44, 12
		rowsH := len(items)*(minRowH+5) + 10
		h = min(h, max(body.Dy()/3, body.Dy()-infoH-2*gap-rowsH))
		r := kit.Rows(body, gap, kit.Px(h), kit.Px(infoH), kit.Fr(1))
		view, info = r[0], r[1]
		a.menuRows(c, r[2], false, 60, 5, items)
		window(c, info)
		in2 := kit.Inset(info, 14, 0, 14, 0)
		c.Text(in2, strings.ToUpper(gfx.Fit(gfx.Face(true, 14), title+" · "+line, in2.Dx())), gfx.Face(true, 14), lcdInk, gfx.Left)
	}
	a.region("State", info)
	a.region("Picture", view)
	a.drawCamPicture(c, view, cam, fr, a.camView, title)
}

// camSettingsLine is the line of the Camera settings row.
func camSettingsLine(in camera.Info, n int) string {
	if n > 1 {
		return "The name of " + in.Name + ", on or off, and its uses"
	}
	return "The name of the camera, and on or off"
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
	// small JPEG, and a small picture has a small decode (Frame.Small).
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

// drawAllCameras draws the pictures of the cameras of the tabs in area: side by side, or one over
// the other, the layout with the larger pictures. A tap on a picture opens its tab.
func (a *App) drawAllCameras(c *gfx.Canvas, area image.Rectangle, tabs []camera.Info) {
	a.region("Pictures", area)
	sizes := make([]kit.Size, len(tabs))
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	n := max(1, len(tabs))
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
	for i, in := range tabs {
		cam, ok := a.Camera.Source(in.ID)
		if !ok {
			continue
		}
		st := cam.Status()
		fr := a.camFrame(cam, st, cameraEvery)
		title, _ := cameraState(st)
		parts := kit.Rows(cells[i], 4, kit.Fr(1), kit.Px(22))
		a.drawCamPicture(c, parts[0], cam, fr, camera.ViewFull, title)
		c.Text(parts[1], strings.ToUpper(gfx.Fit(gfx.Face(true, 14), in.Name, parts[1].Dx())), gfx.Face(true, 14), dim, gfx.Center)
		id := in.ID
		a.addHit(kit.Hit{R: cells[i], Tap: func() { a.camID = id; a.Redraw() }})
	}
}

// defaultNames reports that the deck has two cameras or more, and that a camera has the name of
// its device.
func defaultNames(s camera.Settings, list []camera.Info) bool {
	if len(list) < 2 {
		return false
	}
	for _, in := range list {
		if s.Camera(in.ID).Name == "" {
			return true
		}
	}
	return false
}

// camRowH is the height of a row of the Cameras list, and camThumbW the width of its picture.
const camRowH, camThumbW = 100, 128

// drawCameras is the Cameras page: a row for each camera, with a small picture, the name and the
// uses. A tap opens the page of the camera.
func (a *App) drawCameras(c *gfx.Canvas) {
	f := a.navPage(c, "Cameras")
	list := a.Camera.Cameras()
	area := a.listArea(f)
	if len(list) == 0 {
		noItems(c, area, "No camera", "Plug a USB camera into the deck.")
		return
	}
	const gap, hintH = 10, 52
	if defaultNames(a.Camera.Settings(), list) {
		// The hint under the last row, or at the bottom when the rows fill the page.
		h := min(area.Dy()-hintH, len(list)*(camRowH+gap))
		r := kit.Rows(area, 0, kit.Px(h), kit.Px(hintH))
		area = r[0]
		a.region("Hint", r[1])
		textLines(c, kit.Inset(r[1], 4, 4, 4, 0), "Tap a camera to give it a name, such as Top or Front.", noteFace(), dim, noteLineH)
	}
	l := kit.List{Area: area, N: len(list), RowH: camRowH + gap, Gap: gap, Scroll: a.pageScroll()}
	a.list(c, l, func(i int) string { return list[i].Name }, func(c *gfx.Canvas, i int, r image.Rectangle) {
		a.cameraRow(c, r, list[i])
	}, func(i int) { a.openCamEdit(list[i].ID) })
}

// cameraRow is the row of a camera in the Cameras list: the small picture, the name, and the uses.
func (a *App) cameraRow(c *gfx.Canvas, r image.Rectangle, in camera.Info) {
	plate(c, r)
	a.region(in.Name, r)
	if a.isPressed(r) {
		c.RoundRect(r.Inset(1), 4, raised)
	}
	rowArrow(c, r)
	th := image.Rect(r.Min.X+10, 0, r.Min.X+10+camThumbW, camThumbW*9/16+8)
	th = th.Add(image.Pt(0, r.Min.Y+(r.Dy()-th.Dy())/2))
	a.camThumb(c, th, in)
	x, right := th.Max.X+16, r.Max.X-50
	c.Text(image.Rect(x, r.Min.Y+12, right, r.Min.Y+42), strings.ToUpper(gfx.Fit(gfx.Face(true, 20), in.Name, right-x)), gfx.Face(true, 20), text, gfx.Left)
	col := dim
	if !in.Usable() {
		col = red
	}
	textLines(c, image.Rect(x, r.Min.Y+44, right, r.Max.Y-8), usesLine(in), gfx.Face(false, 16), col, 22)
}

// camThumb draws a small picture of the camera in in r: its newest frame, one frame a second, in
// the small decode of the frame. A camera with no picture has its state in the window.
func (a *App) camThumb(c *gfx.Canvas, r image.Rectangle, in camera.Info) {
	cam, ok := a.Camera.Source(in.ID)
	var fr *camera.Frame
	if ok && in.Usable() {
		fr = a.camFrame(cam, cam.Status(), thumbEvery)
	}
	if fr == nil {
		window(c, r)
		word := "Off"
		switch {
		case !in.Found:
			word = "Not connected"
		case in.Usable():
			word = "Starting"
		}
		c.Text(r.Inset(4), strings.ToUpper(gfx.Fit(gfx.Face(true, 12), word, r.Dx()-8)), gfx.Face(true, 12), lcdDim, gfx.Center)
		return
	}
	a.drawCamPicture(c, r, cam, fr, camera.ViewFull, "")
}

// drawCameraEdit is the page of one camera (a.camEdit): its picture, the name, On, the uses, and
// a row to the Camera page for the turn and the calibration.
func (a *App) drawCameraEdit(c *gfx.Canvas) {
	if a.camKB != nil {
		a.drawKeyboard(c, a.camKB)
		return
	}
	list := a.Camera.Cameras()
	in, ok := camInfo(list, a.camEdit)
	if !ok {
		f := a.navPage(c, "Camera")
		noItems(c, a.listArea(f), "No camera", "The deck does not find the camera.")
		return
	}
	f := a.navPage(c, "Camera: "+in.Name)
	id := in.ID
	onLine := ""
	if !in.Found {
		onLine = "Not connected"
	}
	groups := []menuGroup{
		{items: []menuItem{{"Name", in.Name, dim, func() { a.renameCamera(id) }}}},
		{items: []menuItem{{"On", onLine, dim, func() { a.toggleCamOn(id) }}}, checks: []bool{!in.Off}},
	}
	var uses []menuGroup
	if len(list) > 1 {
		g := menuGroup{heading: "Use for", note: "A use is on one camera. A check moves it here."}
		switch {
		case !in.Found:
			g.note = "Connect the camera to use it."
		case in.Off:
			g.note = "Turn the camera on to use it."
		}
		for _, role := range camera.Roles {
			var tap func()
			if in.Usable() {
				tap = func() { a.giveRole(id, role) }
			}
			g.items = append(g.items, menuItem{roleTitles[role], roleLines[role], dim, tap})
			g.checks = append(g.checks, in.HasRole(role))
		}
		uses = append(uses, g)
	}
	turn := menuItem{"Turn and calibrate", "Turn the camera on first", dim, nil}
	if in.Usable() {
		sub := turnLine(in.Rotate)
		if cam, ok := a.Camera.Source(id); ok {
			sub += " · " + calibLine(cam.Calibration())
			if f.wide {
				// The narrow column: only the calibration.
				sub = "Not calibrated"
				if _, ok := cam.Calibration(); ok {
					sub = "Calibrated"
				}
			}
		}
		turn = menuItem{"Turn and calibrate", sub, dim, func() { a.openCamPicture(id) }}
	}
	groups = append(groups, menuGroup{items: []menuItem{turn}})

	body := f.body()
	var pic image.Rectangle
	if f.wide && len(uses) > 0 {
		// The picture, the name, On and the turn at the left; the uses at the right.
		r := kit.Cols(body, 16, kit.Fr(1), kit.Fr(1))
		left := kit.Rows(r[0], 12, kit.Px(r[0].Dx()*9/16), kit.Fr(1))
		pic = left[0]
		a.menuGroups(c, left[1], true, 60, 5, groups)
		a.menuGroups(c, r[1], true, 80, 5, uses)
	} else {
		if !f.wide {
			body = kit.Inset(body, 0, 10, 0, 0)
		}
		w := body.Dx() * 3 / 5
		if f.wide {
			w = body.Dx() / 3
		}
		r := kit.Rows(body, 12, kit.Px(w*9/16), kit.Fr(1))
		pic = kit.Center(r[0], w, r[0].Dy())
		a.menuGroups(c, r[1], f.wide, 60, 5, slices.Insert(groups, 2, uses...))
	}
	a.region("Picture", pic)
	a.camThumb(c, pic, in)
}

// drawCheckRow draws a check row: a box at the left, checked or empty, and the title and the line
// at its right. A row with no action (enabled false) is dim.
func (a *App) drawCheckRow(c *gfx.Canvas, r image.Rectangle, title, sub string, checked, enabled bool) {
	plate(c, r)
	a.region(title, r)
	if enabled && a.isPressed(r) {
		c.RoundRect(r.Inset(1), 4, raised)
	}
	const size = 30
	cy := r.Min.Y + r.Dy()/2
	box := image.Rect(r.Min.X+16, cy-size/2, r.Min.X+16+size, cy+size/2)
	ink, col := text, dim
	if !enabled {
		ink = dim
	}
	switch {
	case checked && enabled:
		c.RoundRect(box, 5, darken(orange, 0.3))
		c.RoundRect(box.Inset(2), 4, orange)
		col = onAcc
	case checked:
		c.RoundRect(box, 5, edge)
		c.RoundRect(box.Inset(2), 4, raised)
	default:
		c.RoundRect(box, 5, edge)
		c.RoundRect(box.Inset(2), 4, bg)
	}
	if checked {
		x, y := box.Min.X+size/2, cy
		c.Polygon([]image.Point{{x - 10, y - 1}, {x - 6, y - 5}, {x - 2, y + 1}, {x + 7, y - 9}, {x + 11, y - 5}, {x - 2, y + 9}}, col)
	}
	// The title, and under it the line in one line, or in smaller lines when it is too long. The
	// text is in the middle of the row.
	x0, x1 := box.Max.X+16, r.Max.X-14
	const titleH = 30
	if sub == "" {
		c.Text(image.Rect(x0, r.Min.Y, x1, r.Max.Y), strings.ToUpper(title), gfx.Face(true, 20), ink, gfx.Left)
		return
	}
	face, lineH := gfx.Face(false, 16), 22
	lines := wrapText(face, sub, x1-x0)
	if len(lines) > 1 {
		face, lineH = gfx.Face(false, 14), 18
		lines = wrapText(face, sub, x1-x0)
	}
	n := max(1, min(len(lines), (r.Dy()-titleH-8)/lineH))
	y := r.Min.Y + (r.Dy()-titleH-n*lineH)/2
	c.Text(image.Rect(x0, y, x1, y+titleH), strings.ToUpper(title), gfx.Face(true, 20), ink, gfx.Left)
	textLines(c, image.Rect(x0, y+titleH, x1, y+titleH+n*lineH), sub, face, dim, lineH)
}
