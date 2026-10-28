package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/kit"
	"sort"
	"strings"
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
	thumbSmall = 68
)

// fileList keeps the scroll position of the Files page.
type fileList struct{ scroll int }

func (a *App) sortedFiles() []printer.File {
	files := a.Printer.Files()
	sort.SliceStable(files, func(i, j int) bool { return files[i].Date.After(files[j].Date) })
	return files
}

func (a *App) drawFiles(c *gfx.Canvas) {
	f := a.navPage(c, "Files")
	// The count above the list. A printer with an SD card has two keys over it: the files on the
	// deck and the files on the card.
	r := kit.Rows(f.body(), 8, kit.Px(24), kit.Fr(1))
	if _, ok := a.sdPrinter(); ok {
		r = kit.Rows(kit.Inset(f.body(), 0, 10, 0, 0), 8, kit.Px(56), kit.Px(24), kit.Fr(1))
		a.sourceKeys(c, r[0])
		r = r[1:]
		if a.sd.on {
			a.drawSDFiles(c, r[0], r[1])
			return
		}
	}
	files := a.sortedFiles()
	count, area := r[0], r[1]
	countR, countText := kit.Rows(count, 0, kit.Px(24))[0], fmt.Sprintf("Newest first · %d %s", len(files), plural(len(files), "file", "files"))
	label(c, countR, countText, gfx.Left)
	a.textRegion("Count", countR, gfx.Face(true, 14), strings.ToUpper(countText))
	if len(files) == 0 {
		noItems(c, area, "No files yet", "Send a file from the slicer,", "or upload it in the browser.")
		return
	}
	open := func(i int) {
		a.file = files[i]
		a.open(pageFile)
	}
	l := kit.List{Area: area, N: len(files), RowH: fileRowH, Gap: 10, Scroll: &a.fileList.scroll}
	a.list(c, l, func(i int) string { return a.fileName(files[i]) }, func(c *gfx.Canvas, i int, r image.Rectangle) {
		f := files[i]
		plate(c, r)
		m := a.Printer.FileMeta(f.Path)
		name := m.Name(f.Name)
		pic := image.Rect(r.Min.X+10, r.Min.Y+9, r.Min.X+10+thumbSmall, r.Min.Y+9+thumbSmall)
		a.thumb(c, pic, f, m)
		if i == 0 { // the names of the first row
			a.region("Picture", pic)
		}
		// The name, the slicer data, and the date in three lines; a little further apart for the
		// large text.
		y1, y2, y3 := 8, 36, 58
		if gfx.FinePrint() {
			y1, y2, y3 = 4, 34, 60
		}
		c.Text(image.Rect(r.Min.X+92, r.Min.Y+y1, r.Max.X-12, r.Min.Y+y1+30), name, gfx.Face(true, 19), text, gfx.Left)
		c.Text(image.Rect(r.Min.X+92, r.Min.Y+y2, r.Max.X-12, r.Min.Y+y2+24), a.metaLine(m), gfx.Face(false, 16), dim, gfx.Left)
		if i == 0 {
			a.textRegion("File", image.Rect(r.Min.X+92, r.Min.Y+8, r.Max.X-12, r.Min.Y+60), gfx.Face(true, 19), gfx.Fit(gfx.Face(true, 19), name, r.Dx()-104))
		}
		c.Text(image.Rect(r.Min.X+92, r.Min.Y+y3, r.Max.X-12, r.Max.Y-6), fileDate(f.Date, a.Now(), a.Clock24), gfx.Face(false, 14), dim, gfx.Left)
		if i == 0 {
			a.textRegion("Date", image.Rect(r.Min.X+92, r.Min.Y+58, r.Max.X-12, r.Max.Y-6), gfx.Face(false, 14), fileDate(f.Date, a.Now(), a.Clock24))
		}
	}, open)
}

// fileName is the name of a file for the screens: the title in the file, or else the cleaned file
// name.
func (a *App) fileName(f printer.File) string {
	return a.Printer.FileMeta(f.Path).Name(f.Name)
}

// fileGrams is the filament of a file in grams: the weight from the slicer, or else the weight
// of its length, with the material of the file. 0 is not known.
func (a *App) fileGrams(m gcodemeta.Meta) float64 {
	if m.FilamentG > 0 {
		return m.FilamentG
	}
	if m.FilamentMM > 0 {
		return a.grams(m.FilamentMM, m.Material)
	}
	return 0
}

// noItems is the text of an empty list: a title and lines under it, in the upper part of the area.
func noItems(c *gfx.Canvas, area image.Rectangle, title string, lines ...string) {
	s := kit.NewStack(kit.Inset(area, 6, min(156, area.Dy()/3), 6, 0), -10)
	c.Text(s.Next(40), title, gfx.Face(true, 22), dim, gfx.Center)
	for _, l := range lines {
		c.Text(s.Next(40), l, gfx.Face(false, 18), dim, gfx.Center)
	}
}

// metaLine is the short line of a file: time, filament, layer height.
func (a *App) metaLine(m gcodemeta.Meta) string {
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
	if g := a.fileGrams(m); g > 0 {
		add(printer.Weight(g))
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

// thumb draws the thumbnail of a file in a display window. When the slicer wrote none, it draws the
// G-code of the file, and a model shape while the G-code is drawn. A browser gets the image at its
// full size, for its pixel density.
func (a *App) thumb(c *gfx.Canvas, r image.Rectangle, f printer.File, m gcodemeta.Meta) {
	window(c, r)
	img := m.Thumbnail
	if img == nil {
		// The color that the slicer wrote, or the color word of the material, for example "Navy PLA".
		hex := m.Color
		if _, ok := printer.ParseHex(hex); !ok {
			hex = printer.ColorFromName(m.MaterialName)
		}
		img = a.Printer.FilePicture(f, hex)
	}
	if img == nil {
		drawModelShape(c, r, lcdInk, lcdDim)
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

// drawModelShape is the picture of a file with no thumbnail: a part on a bed. The lower part and
// the bed are in ink, the upper part in dim.
func drawModelShape(c *gfx.Canvas, r image.Rectangle, ink, dim color.RGBA) {
	cx, by := r.Min.X+r.Dx()/2, r.Max.Y-r.Dy()/5
	s := float64(r.Dx()) / 200
	p := func(x, y float64) image.Point { return image.Pt(cx+int(x*s), by+int(y*s)) }
	c.Polygon([]image.Point{p(-80, 4), p(80, 4), p(80, 12), p(-80, 12)}, dim)
	c.Polygon([]image.Point{p(-50, 0), p(50, 0), p(40, -40), p(-40, -40)}, ink)
	c.Polygon([]image.Point{p(-22, -40), p(22, -40), p(18, -80), p(-18, -80)}, dim)
}

// fileIndex is the files of the printer by their path.
func (a *App) fileIndex() map[string]printer.File {
	files := a.Printer.Files()
	out := make(map[string]printer.File, len(files))
	for _, f := range files {
		out[f.Path] = f
	}
	return out
}

// recordThumb draws the thumbnail of a finished print in a display window: the thumbnail of its
// file, as on the Files page. When the file is gone, it draws the model shape in the color of the
// roll of the print.
func (a *App) recordThumb(c *gfx.Canvas, r image.Rectangle, rec printer.Record, files map[string]printer.File) {
	if f, ok := files[rec.Path]; ok && rec.Path != "" {
		a.thumb(c, r, f, a.Printer.FileMeta(f.Path))
		return
	}
	window(c, r)
	hex := printer.ColorFromName(rec.Spool)
	if s := a.Printer.SpoolStore(); s != nil {
		if sp, ok := s.Get(rec.SpoolID); ok {
			if _, ok := printer.ParseHex(sp.Color); ok {
				hex = sp.Color
			} else if h := printer.ColorFromName(sp.Label()); h != "" {
				hex = h
			}
		}
	}
	if hex == "" {
		drawModelShape(c, r, lcdInk, lcdDim)
		return
	}
	ink := printer.ModelColor(hex)
	drawModelShape(c, r, ink, mix(ink, lcd, 0.35))
}

// drawFile is the page of one file: the thumbnail, what the slicer wrote, Print and Delete.
func (a *App) drawFile(c *gfx.Canvas, st printer.State) {
	f := a.file
	pf := a.navFrame()
	m := a.Printer.FileMeta(f.Path)
	pg := a.navPage(c, fitTitle(m.Name(f.Name), pf.head.Dx()-200))

	// The first row is the file name, as the browser and the slicer show it.
	rows := [][2]string{{"File", f.Name}}
	if m.PrintTime > 0 {
		rows = append(rows, [2]string{"Print time", duration(m.PrintTime)})
	}
	if g := a.fileGrams(m); g > 0 {
		v := printer.Weight(g)
		if m.FilamentMM > 0 {
			v += fmt.Sprintf(" · %.1f m", m.FilamentMM/1000)
		}
		if m.Material != "" {
			v += " " + m.Material
		}
		rows = append(rows, [2]string{"Filament", v})
		// The roll in the printer: is there enough filament on it, and the cost of the print.
		if s := a.Printer.SpoolStore(); s != nil {
			if sp, ok := s.In(); ok {
				roll := sp.Label()
				if g > sp.Left() {
					roll = "Only " + printer.Weight(sp.Left()) + " left"
				}
				rows = append(rows, [2]string{"Roll", roll})
				if cost := sp.Cost(g); cost > 0 {
					rows = append(rows, [2]string{"Cost", fmt.Sprintf("$%.2f", cost)})
				}
			}
		}
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
	// Tall: the picture, the slicer data, and the keys. Wide: the picture and the slicer data at the
	// right, the keys in the left column.
	tableH := len(rows)*30 + 20
	var pic, box, keys image.Rectangle
	if pg.wide {
		// The picture and the slicer data side by side, as high as each other; the keys in the row
		// of actions.
		content, acts := pg.split()
		side := min(content.Dy(), tableH, content.Dx()/2)
		cols := kit.Cols(content, 16, kit.Px(side), kit.Fr(1))
		pic, box, keys = kit.Rows(cols[0], 0, kit.Px(side))[0], kit.Rows(cols[1], 0, kit.Px(min(tableH, content.Dy())))[0], acts
	} else {
		r := kit.Rows(kit.Inset(pg.body(), 0, 12, 0, 8), 12, kit.Fr(1), kit.Px(tableH), kit.Px(84))
		side := min(r[0].Dx(), r[0].Dy())
		pic, box, keys = kit.Center(r[0], side, side), r[1], r[2]
	}
	a.thumb(c, pic, f, m)
	a.region("Picture", pic)
	plate(c, box)
	a.region("Slicer data", box)
	// A long file name ends in an ellipsis at the right of its label.
	rows[0][1] = gfx.Fit(gfx.Face(true, 17), f.Name, box.Dx()-36-gfx.Width(gfx.Face(true, 14), "FILE")-16)
	detailRows(c, kit.Inset(box, 18, 10, 18, 10), rows, 30)

	pr := a.Printer
	path := f.Path
	// Delete, the destructive action, at the left; Print at the right.
	k := kit.Cols(keys, 12, kit.Px(144), kit.Fr(1))
	a.printerKey(c, k[1], "Print", orange, onAcc, func() { a.confirmPrint(path) })
	if st.Busy() && path == st.FilePath {
		// The file of the print that runs.
		a.lockedKey(c, k[0], "Delete", raised)
		return
	}
	a.button(c, k[0], "Delete", raised, red, func() {
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
