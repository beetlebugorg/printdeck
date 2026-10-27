package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/timelapse"
	"printdeck/kit"
)

// The Time lapse page (Settings > Time lapse): a frame of the camera at each layer change of a
// print, and a picture of the first layer. For each picture, the toolhead parks out of the picture
// (printdeck_timelapse of the Klipper host). The video of a print is on the System page of the web
// server. The First layer page shows the last picture of a first layer. The deck opens it when the
// picture is taken.

// TimeLapse is the time lapse of the deck (timelapse.Runner).
type TimeLapse interface {
	Settings() timelapse.Settings
	SetSettings(timelapse.Settings)
	Info() timelapse.Info
}

// timeLapseLine is the line of the Time lapse row of the Settings page.
func timeLapseLine(s timelapse.Settings) string {
	switch {
	case s.On && s.First != timelapse.FirstOff:
		return "On · and a picture of the first layer"
	case s.On:
		return "On · a frame at each layer"
	case s.First != timelapse.FirstOff:
		return "Off · a picture of the first layer"
	}
	return "Off"
}

// lapseLine is the line of the Time lapse row of the page.
func lapseLine(in timelapse.Info, s timelapse.Settings) string {
	switch {
	case in.Recording:
		return fmt.Sprintf("On · %d frames of this print", in.Frames)
	case s.On:
		return "On · a frame at each layer change"
	}
	return "Off"
}

// parkLine is the line of the Park row.
func parkLine(park bool) string {
	if park {
		return "On · the toolhead moves out of the picture"
	}
	return "Off · the toolhead stays in the picture"
}

// firstLayerLine is the line of the First layer row.
func firstLayerLine(first int) string {
	switch first {
	case timelapse.FirstPicture:
		return "A picture after the first layer"
	case timelapse.FirstPause:
		return "A picture, then a pause for a check"
	}
	return "Off"
}

// videosLine is the line of the Videos row.
func videosLine(n int) string {
	switch n {
	case 0:
		return "None · the video of a print is on the System page"
	case 1:
		return "1 video · download it on the System page"
	}
	return fmt.Sprintf("%d videos · download them on the System page", n)
}

func (a *App) toggleLapse() {
	s := a.TimeLapse.Settings()
	s.On = !s.On
	a.TimeLapse.SetSettings(s)
	a.Toast("Time lapse: " + map[bool]string{true: "on", false: "off"}[s.On])
}

func (a *App) toggleLapsePark() {
	s := a.TimeLapse.Settings()
	s.Park = !s.Park
	a.TimeLapse.SetSettings(s)
	a.Toast("Park: " + parkLine(s.Park))
}

func (a *App) cycleFirstLayer() {
	s := a.TimeLapse.Settings()
	s.First = (s.First + 1) % 3
	a.TimeLapse.SetSettings(s)
	a.Toast("First layer: " + firstLayerLine(s.First))
}

// drawTimeLapse is the Time lapse page.
func (a *App) drawTimeLapse(c *gfx.Canvas) {
	f := a.navPage(c, "Time lapse")
	s := a.TimeLapse.Settings()
	in := a.TimeLapse.Info()
	items := []menuItem{
		{"Time lapse", lapseLine(in, s), dim, a.toggleLapse},
		{"Park for the frames", parkLine(s.Park), dim, a.toggleLapsePark},
		{"First layer", firstLayerLine(s.First), dim, a.cycleFirstLayer},
	}
	if in.First != nil {
		items = append(items, menuItem{"Picture of the first layer", firstSub(in, a), dim, func() { a.open(pageFirstLayer) }})
	}
	items = append(items, menuItem{"Videos", videosLine(len(in.Videos)), dim, nil})
	if st := a.Camera.Status().State; st == "off" || st == "none" {
		items = append(items, menuItem{"Camera", "No camera: no frames and no pictures", red, func() { a.open(pageCamera) }})
	}
	a.menuRows(c, f.body(), f.wide, 60, 5, items)
}

// firstSub is the file and the time of the picture of the first layer.
func firstSub(in timelapse.Info, a *App) string {
	name := strings.TrimSuffix(filepath.Base(in.FirstFile), filepath.Ext(in.FirstFile))
	if name == "" || name == "." {
		return fileDate(in.FirstAt, a.Now(), a.Clock24)
	}
	return name + " · " + fileDate(in.FirstAt, a.Now(), a.Clock24)
}

// ShowFirstLayer opens the First layer page. It is safe to call from any goroutine.
func (a *App) ShowFirstLayer() {
	a.post(func() {
		if a.topPage() != pageFirstLayer {
			a.pages = append(a.pages, pageFirstLayer)
		}
	})
}

// drawFirstLayerPicture is the First layer page: the last picture of a first layer, as large as it fits.
func (a *App) drawFirstLayerPicture(c *gfx.Canvas) {
	f := a.navPage(c, "First layer")
	in := a.TimeLapse.Info()
	body := f.body()
	rows := kit.Rows(body, 12, kit.Fr(1), kit.Px(40))
	view := rows[0]
	window(c, view)
	a.region("Picture", view)
	pic := view.Inset(4)
	if in.First == nil {
		c.Text(pic, "NO PICTURE", gfx.Face(true, 18), lcdDim, gfx.Center)
		return
	}
	c.Text(rows[1], gfx.Fit(gfx.Face(false, 16), firstSub(in, a), rows[1].Dx()), gfx.Face(false, 16), dim, gfx.Left)
	sz := in.First.Size()
	k := min(float64(pic.Dx())/float64(sz.X), float64(pic.Dy())/float64(sz.Y))
	r := kit.Center(pic, int(float64(sz.X)*k), int(float64(sz.Y)*k))
	w := r.Dx()
	if a.Scale[1] > 0 {
		w = w * a.Scale[0] / a.Scale[1]
	}
	c.Cover(r, in.First.Small(w))
}
