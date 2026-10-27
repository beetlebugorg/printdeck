package ui

import (
	"fmt"
	"image"
	"path"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// SDPrinter is a printer that reads the SD card in the printer: the Klipper host with
// [printdeck_sdcard] (printer.Klippy).
type SDPrinter interface {
	HasSDCard() bool
	SDFiles(dir string) ([]printer.SDFile, error)
	// CopySDFile starts the copy of a file of the card to the deck. It returns the name on the
	// deck, and the ID of the copy (printer.SDCopy), or 0 when the deck has the file.
	CopySDFile(path string, size int64) (name string, id int, err error)
	CancelSDCopy()
}

// sdView is the SD card of the Files page.
type sdView struct {
	on      bool   // the Files page shows the SD card
	dir     string // the folder of the card that shows
	files   []printer.SDFile
	loaded  bool // files is the list of dir
	loading bool
	err     string
	scroll  int
	copyID  int    // the copy that runs for a print, or 0
	copyTo  string // the name of that file on the deck
}

// sdRowH is the height of a row of the SD card, with the gap under it.
const sdRowH = 74

// sdPrinter is the printer when it reads its SD card.
func (a *App) sdPrinter() (SDPrinter, bool) {
	sp, ok := a.Printer.(SDPrinter)
	return sp, ok && sp.HasSDCard()
}

// loadSD reads the folder dir of the card, in the background.
func (a *App) loadSD(dir string) {
	sp, ok := a.sdPrinter()
	if !ok || a.sd.loading {
		return
	}
	a.sd.loading, a.sd.err = true, ""
	go func() {
		files, err := sp.SDFiles(dir)
		a.post(func() {
			a.sd.loading, a.sd.loaded, a.sd.dir, a.sd.files, a.sd.scroll = false, true, dir, files, 0
			a.sd.err = ""
			if err != nil {
				a.sd.files, a.sd.err = nil, err.Error()
			}
		})
	}()
}

// sourceKeys draws the two keys over the list of the Files page: the files on the deck and the
// files on the SD card of the printer.
func (a *App) sourceKeys(c *gfx.Canvas, r image.Rectangle) {
	k := kit.Cols(r, 12, kit.Fr(1), kit.Fr(1))
	deck, card := keyOn, keyOff
	if a.sd.on {
		deck, card = keyOff, keyOn
	}
	a.button(c, k[0], "On the deck", deck, text, func() { a.sd.on = false })
	a.button(c, k[1], "SD card", card, text, func() {
		if !a.sd.on || !a.sd.loading {
			a.sd.on = true
			a.loadSD(a.sd.dir)
		}
	})
}

// drawSDFiles draws the SD card of the printer: the folder in count, and its folders and G-code
// files in area. A tap on a folder opens it. A tap on a file copies it to the deck, and then asks
// to print it.
func (a *App) drawSDFiles(c *gfx.Canvas, count, area image.Rectangle) {
	st := a.Printer.State()
	where := "SD card of the printer"
	if a.sd.dir != "" {
		where = "SD card · " + a.sd.dir
	}
	if a.sd.loaded && a.sd.err == "" {
		where += fmt.Sprintf(" · %d files", countFiles(a.sd.files))
	}
	countR := kit.Rows(count, 0, kit.Px(24))[0]
	label(c, countR, where, gfx.Left)
	a.textRegion("Count", countR, gfx.Face(true, 14), strings.ToUpper(where))

	// The copy that runs: its file, its progress, and Stop.
	if cp := st.SDCopy; cp.Path != "" {
		r := kit.Rows(area, 10, kit.Px(84), kit.Fr(1))
		a.drawSDCopy(c, r[0], cp)
		area = r[1]
	}
	switch {
	case a.sd.loading && !a.sd.loaded:
		noItems(c, area, "Reading the SD card", "Wait a few seconds.")
		return
	case a.sd.err != "":
		noItems(c, area, "No files", a.sd.err)
		again := kit.Rows(kit.Inset(area, 40, area.Dy()-80, 40, 10), 0, kit.Px(64))[0]
		a.button(c, again, "Read the card again", raised, text, func() { a.loadSD(a.sd.dir) })
		return
	}
	rows := a.sd.files
	up := a.sd.dir != ""
	n := len(rows)
	if up {
		n++
	}
	if n == 0 {
		noItems(c, area, "No G-code files", "This folder of the card has none.")
		return
	}
	entry := func(i int) (printer.SDFile, bool) {
		if up {
			if i == 0 {
				return printer.SDFile{}, true
			}
			i--
		}
		return rows[i], false
	}
	l := kit.List{Area: area, N: n, RowH: sdRowH, Gap: 10, Scroll: &a.sd.scroll}
	a.list(c, l, func(i int) string {
		f, isUp := entry(i)
		if isUp {
			return "Up"
		}
		return f.Name
	}, func(c *gfx.Canvas, i int, r image.Rectangle) {
		f, isUp := entry(i)
		plate(c, r)
		icon := image.Rect(r.Min.X+14, r.Min.Y+12, r.Min.X+14+40, r.Max.Y-12)
		x := r.Min.X + 70
		switch {
		case isUp:
			drawFolder(c, icon)
			c.Text(image.Rect(x, r.Min.Y, r.Max.X-12, r.Max.Y), "‹ Up to "+parentName(a.sd.dir), gfx.Face(true, 19), text, gfx.Left)
			return
		case f.Dir:
			drawFolder(c, icon)
		default:
			drawModelShape(c, icon, orange, dim)
		}
		c.Text(image.Rect(x, r.Min.Y+8, r.Max.X-12, r.Min.Y+38), f.Name, gfx.Face(true, 19), text, gfx.Left)
		sub := "Folder"
		if !f.Dir {
			sub = sizeText(f.Size)
			if !f.Modified.IsZero() {
				sub += " · " + fileDate(f.Modified, a.Now(), a.Clock24)
			}
		}
		c.Text(image.Rect(x, r.Min.Y+38, r.Max.X-12, r.Max.Y-8), sub, gfx.Face(false, 16), dim, gfx.Left)
	}, func(i int) {
		f, isUp := entry(i)
		switch {
		case isUp:
			a.loadSD(parentDir(a.sd.dir))
		case f.Dir:
			a.loadSD(path.Join(a.sd.dir, f.Name))
		default:
			a.copySDFile(path.Join(a.sd.dir, f.Name), f.Size)
		}
	})
}

// drawSDCopy draws the copy that runs in r: the file, a bar, the bytes, and Stop.
func (a *App) drawSDCopy(c *gfx.Canvas, r image.Rectangle, cp printer.SDCopy) {
	plate(c, r)
	a.region("Copy", r)
	in := kit.Inset(r, 14, 10, 14, 10)
	cols := kit.Cols(in, 14, kit.Fr(1), kit.Px(110))
	label(c, image.Rect(cols[0].Min.X, cols[0].Min.Y, cols[0].Max.X, cols[0].Min.Y+18), "Copy to the deck", gfx.Left)
	c.Text(image.Rect(cols[0].Min.X, cols[0].Min.Y+18, cols[0].Max.X, cols[0].Min.Y+42), path.Base(cp.Path), gfx.Face(true, 18), text, gfx.Left)
	frac := 0.
	if cp.Size > 0 {
		frac = float64(cp.Copied) / float64(cp.Size)
	}
	bar := image.Rect(cols[0].Min.X, cols[0].Max.Y-12, cols[0].Max.X, cols[0].Max.Y-2)
	c.RoundRect(bar, 3, edge)
	if w := int(float64(bar.Dx()-2) * frac); w > 0 {
		c.RoundRect(image.Rect(bar.Min.X+1, bar.Min.Y+1, bar.Min.X+1+w, bar.Max.Y-1), 2, orange)
	}
	pct := fmt.Sprintf("%d%% · %s of %s", int(frac*100), sizeText(cp.Copied), sizeText(cp.Size))
	c.Text(image.Rect(cols[0].Min.X, cols[0].Min.Y+40, cols[0].Max.X, cols[0].Max.Y-12), pct, gfx.Face(false, 14), dim, gfx.Left)
	sp, ok := a.sdPrinter()
	a.button(c, cols[1], "Stop", raised, text, func() {
		if ok {
			sp.CancelSDCopy()
		}
	})
}

// copySDFile copies a file of the card to the deck. At the end of the copy, the deck asks to print
// it (checkSDCopy).
func (a *App) copySDFile(p string, size int64) {
	sp, ok := a.sdPrinter()
	if !ok {
		return
	}
	if a.printing() {
		a.Toast(waitText)
		return
	}
	if a.Printer.State().SDCopy.Path != "" || a.sd.copyID != 0 {
		a.Toast("Wait until the copy ends")
		return
	}
	go func() {
		name, id, err := sp.CopySDFile(p, size)
		a.post(func() {
			switch {
			case err != nil:
				a.toast, a.toastAt = err.Error(), a.Now()
			case id == 0: // the deck has this file
				a.Printer.LoadFiles()
				a.confirmPrint(name)
			default:
				a.sd.copyID, a.sd.copyTo = id, name
			}
		})
	}()
}

// checkSDCopy asks to print the file at the end of its copy, or shows the error of the copy.
func (a *App) checkSDCopy(st printer.State) {
	cp := st.SDCopy
	if a.sd.copyID == 0 || cp.ID != a.sd.copyID || cp.Path != "" {
		return
	}
	name := a.sd.copyTo
	a.sd.copyID, a.sd.copyTo = 0, ""
	if cp.Err != "" || cp.Done != name {
		msg := cp.Err
		if msg == "" {
			msg = "The copy did not end"
		}
		a.toast, a.toastAt = msg, a.Now()
		return
	}
	go a.Printer.LoadFiles()
	a.confirmPrint(name)
}

// countFiles is the count of the files in a list of the card, with no folders.
func countFiles(list []printer.SDFile) int {
	n := 0
	for _, f := range list {
		if !f.Dir {
			n++
		}
	}
	return n
}

func parentDir(dir string) string {
	if p := path.Dir(dir); p != "." && p != "/" {
		return p
	}
	return ""
}

func parentName(dir string) string {
	if p := parentDir(dir); p != "" {
		return path.Base(p)
	}
	return "the card"
}

// sizeText is a size in bytes as "640 KB" or "3.4 MB".
func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// drawFolder draws a folder in r.
func drawFolder(c *gfx.Canvas, r image.Rectangle) {
	h := min(r.Dy(), r.Dx()*4/5)
	box := image.Rect(r.Min.X, r.Min.Y+(r.Dy()-h)/2, r.Max.X, r.Min.Y+(r.Dy()+h)/2)
	tab := image.Rect(box.Min.X, box.Min.Y, box.Min.X+box.Dx()*2/5, box.Min.Y+8)
	c.RoundRect(tab, 2, dim)
	c.RoundRect(image.Rect(box.Min.X, box.Min.Y+5, box.Max.X, box.Max.Y), 3, dim)
	c.RoundRect(image.Rect(box.Min.X+2, box.Min.Y+9, box.Max.X-2, box.Max.Y-2), 2, raised)
}
