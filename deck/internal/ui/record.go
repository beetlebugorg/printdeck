package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/kit"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// drawRecord is the page of one finished print of the history: the model, the result, the time,
// the filament, and Reprint. The model of a print that did not complete shows the part that
// printed in colour.
func (a *App) drawRecord(c *gfx.Canvas, st printer.State) {
	r := a.record
	pf := a.navFrame()
	pg := a.navPage(c, fitTitle(r.File, pf.head.Dx()-200))
	rows := a.recordRows(r)

	// Tall: the picture, the table, and Reprint. Wide: the picture and the table side by side, as
	// high as each other, and Reprint at the right of the row of actions.
	tableH := len(rows)*30 + 20
	var pic, box, keys image.Rectangle
	if pg.wide {
		content, acts := pg.split()
		side := min(content.Dy(), tableH, content.Dx()/2)
		cols := kit.Cols(content, 16, kit.Px(side), kit.Fr(1))
		pic, box = kit.Rows(cols[0], 0, kit.Px(side))[0], kit.Rows(cols[1], 0, kit.Px(min(tableH, content.Dy())))[0]
		keys = kit.Cols(acts, 12, kit.Fr(1), kit.Fr(1))[1]
	} else {
		rr := kit.Rows(kit.Inset(pg.body(), 0, 12, 0, 8), 12, kit.Fr(1), kit.Px(tableH), kit.Px(84))
		pic, box, keys = rr[0], rr[1], rr[2]
	}
	gone := a.recordModel(c, pic, r)
	plate(c, box)
	a.region("Details", box)
	detailRows(c, kit.Inset(box, 18, 10, 18, 10), rows, 30)
	a.reprintKey(c, keys, r, gone, orange, onAcc)
}

// recordRows are the rows of the table of a finished print: the result, the end, the time, the
// filament, the material, the roll and the cost.
func (a *App) recordRows(r printer.Record) [][2]string {
	res := map[string]string{"complete": "Done", "cancelled": "Cancelled", "error": "Failed"}[r.Result]
	rows := [][2]string{
		{"Result", res},
		{"Ended", fileDate(r.End, a.Now(), a.Clock24)},
		{"Print time", duration(r.Duration)},
	}
	if r.Filament > 0 {
		v := fmt.Sprintf("%s · %.1f m", printer.Weight(a.grams(r.Filament, r.Material)), r.Filament/1000)
		rows = append(rows, [2]string{"Filament", v})
	}
	if r.Material != "" {
		rows = append(rows, [2]string{"Material", r.Material})
	}
	if r.Spool != "" {
		rows = append(rows, [2]string{"Roll", r.Spool})
	}
	if r.Cost > 0 {
		rows = append(rows, [2]string{"Cost", fmt.Sprintf("$%.2f", r.Cost)})
	}
	return rows
}

// recordModel draws the model of a finished print in a window pic. It reports that the file is
// gone, and then the window says so.
func (a *App) recordModel(c *gfx.Canvas, pic image.Rectangle, r printer.Record) (gone bool) {
	window(c, pic)
	a.region("Model", pic)
	img, loading := a.Printer.RecordPicture(r)
	switch {
	case img != nil:
		picture(c, pic, img)
	case loading:
		c.Text(pic, "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
	default:
		c.Text(pic, "THE FILE IS GONE", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
	return img == nil && !loading
}

// reprintKey draws Reprint for a finished print. It is off when the file is gone.
func (a *App) reprintKey(c *gfx.Canvas, r image.Rectangle, rec printer.Record, gone bool, fill, fg color.RGBA) {
	if gone {
		key(c, r, "Reprint", surface, dim, false)
		a.region("Reprint", r)
		return
	}
	path := rec.Path
	a.printerKey(c, r, "Reprint", fill, fg, func() { a.confirmPrint(path) })
}
