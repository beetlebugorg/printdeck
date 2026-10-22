package ui

import (
	"fmt"
	"image"
	"printdeck/kit"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// drawRecord is the page of one finished print of the history: the model, the result, the time,
// the filament, and Reprint. The model of a print that did not complete shows the part that
// printed in colour.
func (a *App) drawRecord(c *gfx.Canvas, st printer.State) {
	r := a.record
	pf := a.pageFrame()
	pg := a.page(c, gfx.Fit(gfx.Face(true, 24), r.File, pf.head.Dx()-200))
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

	// Tall: the picture, the table, and Reprint. Wide: the picture and the table at the right,
	// Reprint in the left column.
	tableH := len(rows)*30 + 20
	var pic, box, keys image.Rectangle
	if pg.wide {
		rr := kit.Rows(pg.body(), 12, kit.Fr(1), kit.Px(tableH))
		pic, box = rr[0], rr[1]
		keys = kit.Rows(pg.aside(), 0, kit.Fr(1), kit.Px(84))[1]
	} else {
		rr := kit.Rows(kit.Inset(pg.body(), 0, 12, 0, 8), 12, kit.Fr(1), kit.Px(tableH), kit.Px(84))
		pic, box, keys = rr[0], rr[1], rr[2]
	}
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
	plate(c, box)
	a.region("Details", box)
	for i, kv := range rows {
		y := box.Min.Y + 10 + i*30
		label(c, image.Rect(box.Min.X+18, y, box.Max.X, y+30), kv[0], gfx.Left)
		c.Text(image.Rect(box.Min.X+170, y, box.Max.X-18, y+30), gfx.Fit(gfx.Face(true, 17), kv[1], box.Dx()-190), gfx.Face(true, 17), text, gfx.Right)
	}

	// The picture is nil when the file is gone.
	switch {
	case img == nil && !loading:
		key(c, keys, "The file is gone", surface, dim, false)
	case st.Busy():
		key(c, keys, "A print runs", surface, dim, false)
	default:
		path := r.Path
		a.button(c, keys, "Reprint", orange, onAcc, func() { a.confirmPrint(path) })
	}
}
