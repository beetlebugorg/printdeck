package ui

import (
	"fmt"
	"image"
	"sort"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"printdeck/internal/gcodemeta"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Files page: the G-code files, newest first, with what the slicer wrote about each. A tap
// opens the file page, with Print and Delete.

const (
	fileRowH   = 96
	filesTop   = 98
	thumbSmall = 68
)

// fileList keeps the scroll position of the Files page.
type fileList struct{ scroll int }

func (a *App) filesBottom() int { return contentY1 - 8 }

func (a *App) sortedFiles() []printer.File {
	files := a.Printer.Files()
	sort.SliceStable(files, func(i, j int) bool { return files[i].Date.After(files[j].Date) })
	return files
}

func (a *App) drawFiles(c *gfx.Canvas) {
	heading(c, "Files")
	a.backButton(c)
	files := a.sortedFiles()
	label(c, image.Rect(24, 66, W-24, 90), fmt.Sprintf("Newest first · %d files", len(files)), gfx.Left)
	if len(files) == 0 {
		c.Text(image.Rect(30, 254, W-30, 294), "No files yet", gfx.Face(true, 22), dim, gfx.Center)
		c.Text(image.Rect(30, 294, W-30, 334), "Send a file from the slicer,", gfx.Face(false, 18), dim, gfx.Center)
		c.Text(image.Rect(30, 324, W-30, 364), "or upload it in the browser.", gfx.Face(false, 18), dim, gfx.Center)
		return
	}
	bottom := a.filesBottom()
	maxScroll := max(0, len(files)*fileRowH-(bottom-filesTop))
	a.fileList.scroll = max(0, min(a.fileList.scroll, maxScroll))
	c.Clipped(image.Rect(0, filesTop, W, bottom), func(c *gfx.Canvas) {
		for i, f := range files {
			y := filesTop + i*fileRowH - a.fileList.scroll
			if y+fileRowH < filesTop || y > bottom {
				continue
			}
			r := image.Rect(24, y, W-24, y+fileRowH-10)
			plate(c, r)
			m := a.Printer.FileMeta(f.Path)
			a.thumb(c, image.Rect(r.Min.X+10, r.Min.Y+9, r.Min.X+10+thumbSmall, r.Min.Y+9+thumbSmall), f.Path, m)
			c.Text(image.Rect(r.Min.X+92, r.Min.Y+8, r.Max.X-12, r.Min.Y+38), gfx.Fit(gfx.Face(true, 19), f.Name, r.Dx()-104), gfx.Face(true, 19), text, gfx.Left)
			c.Text(image.Rect(r.Min.X+92, r.Min.Y+36, r.Max.X-12, r.Min.Y+60), metaLine(m), gfx.Face(false, 16), dim, gfx.Left)
			c.Text(image.Rect(r.Min.X+92, r.Min.Y+58, r.Max.X-12, r.Max.Y-6), fileDate(f.Date, a.Now(), a.Clock24), gfx.Face(false, 14), dim, gfx.Left)
		}
	})
	open := func(i int) {
		if i >= 0 && i < len(files) {
			a.file = files[i]
			a.open(pageFile)
		}
	}
	a.addHit(hit{r: image.Rect(0, filesTop, W, bottom),
		scroll: func(dy int) { a.fileList.scroll = max(0, min(a.fileList.scroll+dy, maxScroll)) },
		tapAt:  func(p image.Point) { open((p.Y - filesTop + a.fileList.scroll) / fileRowH) }})
	a.listFocus(len(files), filesTop, bottom, fileRowH, &a.fileList.scroll, func(i int) string { return files[i].Name }, open)
}

// metaLine is the short line of a file: time, filament, layer height.
func metaLine(m gcodemeta.Meta) string {
	s := ""
	add := func(p string) {
		if s != "" {
			s += " · "
		}
		s += p
	}
	if m.PrintTime > 0 {
		add(hm(int(m.PrintTime.Minutes())))
	}
	switch {
	case m.FilamentG > 0:
		add(fmt.Sprintf("%.1f g", m.FilamentG))
	case m.FilamentMM > 0:
		add(fmt.Sprintf("%.1f m", m.FilamentMM/1000))
	}
	if m.LayerHeight > 0 {
		add(fmt.Sprintf("%.2f mm", m.LayerHeight))
	}
	if s == "" {
		s = "No slicer data"
	}
	return s
}

// fileDate is the upload time: today, yesterday, or the date.
func fileDate(t, now time.Time, clock24 bool) string {
	if t.IsZero() {
		return ""
	}
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Today " + clockText(t, clock24)
	case now.Sub(t) < 48*time.Hour && now.AddDate(0, 0, -1).Day() == d1:
		return "Yesterday " + clockText(t, clock24)
	case y1 == y2:
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}

// thumbs keeps the small thumbnails of the files, scaled once.
var thumbs struct {
	sync.Mutex
	small map[image.Image]*image.RGBA
}

// thumb draws the thumbnail of a file in a display window, or a model shape when the slicer wrote
// none. A browser gets the image at its full size, for its pixel density.
func (a *App) thumb(c *gfx.Canvas, r image.Rectangle, path string, m gcodemeta.Meta) {
	window(c, r)
	img := m.Thumbnail
	if img == nil {
		drawModelShape(c, r)
		return
	}
	in := r.Inset(3)
	if c.Recording() != nil || in.Dx() > thumbSmall {
		c.Cover(in, img)
		return
	}
	thumbs.Lock()
	if thumbs.small == nil {
		thumbs.small = map[image.Image]*image.RGBA{}
	}
	s := thumbs.small[img]
	if s == nil {
		s = image.NewRGBA(image.Rect(0, 0, in.Dx(), in.Dy()))
		xdraw.CatmullRom.Scale(s, s.Rect, img, img.Bounds(), xdraw.Src, nil)
		thumbs.small[img] = s
	}
	thumbs.Unlock()
	c.Image(in, s)
}

// drawModelShape is the picture of a file with no thumbnail: a part on a bed, in the colors of the
// display.
func drawModelShape(c *gfx.Canvas, r image.Rectangle) {
	cx, by := r.Min.X+r.Dx()/2, r.Max.Y-r.Dy()/5
	s := float64(r.Dx()) / 200
	p := func(x, y float64) image.Point { return image.Pt(cx+int(x*s), by+int(y*s)) }
	c.Polygon([]image.Point{p(-80, 4), p(80, 4), p(80, 12), p(-80, 12)}, lcdDim)
	c.Polygon([]image.Point{p(-50, 0), p(50, 0), p(40, -40), p(-40, -40)}, lcdInk)
	c.Polygon([]image.Point{p(-22, -40), p(22, -40), p(18, -80), p(-18, -80)}, lcdDim)
}

// drawFile is the page of one file: the thumbnail, what the slicer wrote, Print and Delete.
func (a *App) drawFile(c *gfx.Canvas, st printer.State) {
	f := a.file
	heading(c, gfx.Fit(gfx.Face(true, 24), f.Name, W-200))
	a.backButton(c)
	m := a.Printer.FileMeta(f.Path)
	a.thumb(c, image.Rect(110, 78, W-110, 338), f.Path, m)

	var rows [][2]string
	if m.PrintTime > 0 {
		rows = append(rows, [2]string{"Print time", duration(m.PrintTime)})
	}
	if m.FilamentMM > 0 || m.FilamentG > 0 {
		v := ""
		if m.FilamentG > 0 {
			v = fmt.Sprintf("%.1f g", m.FilamentG)
		}
		if m.FilamentMM > 0 {
			if v != "" {
				v += " · "
			}
			v += fmt.Sprintf("%.1f m", m.FilamentMM/1000)
		}
		if m.Material != "" {
			v += " " + m.Material
		}
		rows = append(rows, [2]string{"Filament", v})
	}
	if m.LayerHeight > 0 {
		rows = append(rows, [2]string{"Layer height", fmt.Sprintf("%.2f mm", m.LayerHeight)})
	}
	if m.NozzleTemp > 0 || m.BedTemp > 0 {
		rows = append(rows, [2]string{"Nozzle / bed", fmt.Sprintf("%d° / %d°", m.NozzleTemp, m.BedTemp)})
	}
	if m.Slicer != "" {
		rows = append(rows, [2]string{"Slicer", m.Slicer})
	}
	rows = append(rows, [2]string{"Uploaded", fileDate(f.Date, a.Now(), a.Clock24)})
	box := image.Rect(24, 350, W-24, 350+len(rows)*30+20)
	plate(c, box)
	for i, kv := range rows {
		y := box.Min.Y + 10 + i*30
		label(c, image.Rect(box.Min.X+18, y, box.Max.X, y+30), kv[0], gfx.Left)
		c.Text(image.Rect(box.Min.X+170, y, box.Max.X-18, y+30), gfx.Fit(gfx.Face(true, 17), kv[1], box.Dx()-190), gfx.Face(true, 17), text, gfx.Right)
	}

	pr := a.Printer
	path := f.Path
	if st.Busy() {
		key(c, image.Rect(24, 586, W-180, 670), "A print runs", surface, dim, false)
	} else {
		a.button(c, image.Rect(24, 586, W-180, 670), "Print", orange, onAcc, func() { a.confirmPrint(path) })
	}
	a.button(c, image.Rect(W-168, 586, W-24, 670), "Delete", raised, red, func() {
		a.confirm = &confirmBox{question: "Delete " + gfx.Fit(gfx.Face(true, 28), f.Name, 300) + "?", yes: "Delete", danger: true, onYes: func() {
			a.do(func() error {
				if err := pr.DeleteFile(path); err != nil {
					return err
				}
				a.post(a.back)
				return nil
			})
		}}
	})
}
