package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/kit"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// drawRecord is the page of one finished print of the history: the model, the result, the time,
// the filament, the note, the project, the time lapse, Note, Project and Reprint. The model of a print that did not complete shows the
// part that printed in colour.
func (a *App) drawRecord(c *gfx.Canvas, st printer.State) {
	r := a.record
	pf := a.navFrame()
	pg := a.navPage(c, fitTitle(r.Name(), pf.head.Dx()-200))
	rows := a.recordRows(r)
	video := r.Video != "" && a.TimeLapse != nil
	if video {
		rows = append(rows, [2]string{"Time lapse", ""}) // the line comes with the still
	}

	// Tall: the picture, the table, the time lapse, and Reprint. Wide: the picture and the table
	// side by side, as high as each other, and the time lapse and Reprint in the row of actions.
	tableH := len(rows)*30 + 20
	var pic, box, keys, lapse image.Rectangle
	if pg.wide {
		content, acts := pg.split()
		side := min(content.Dy(), tableH, content.Dx()/2)
		cols := kit.Cols(content, 16, kit.Px(side), kit.Fr(1))
		pic, box = kit.Rows(cols[0], 0, kit.Px(side))[0], kit.Rows(cols[1], 0, kit.Px(min(tableH, content.Dy())))[0]
		// The still of the time lapse is 16:9, and the keys get the rest of the row.
		k := kit.Cols(acts, 12, kit.Px(min(acts.Dx()/3, acts.Dy()*16/9)), kit.Fr(1))
		lapse, keys = k[0], k[1]
		if !video {
			keys = acts
		}
	} else {
		sizes := []kit.Size{kit.Fr(1), kit.Px(tableH), kit.Px(84)}
		if video {
			sizes = []kit.Size{kit.Fr(1), kit.Px(tableH), kit.Px(lapseRowH), kit.Px(84)}
		}
		rr := kit.Rows(kit.Inset(pg.body(), 0, 12, 0, 8), 12, sizes...)
		pic, box, keys = rr[0], rr[1], rr[len(rr)-1]
		if video {
			lapse = rr[2]
		}
	}
	if video && pg.wide {
		// The row of actions has no room for a Play key beside the still: the still is the key.
		s := a.videoStill(r.Video, a.stillWidth(lapse))
		if !s.gone && s.still != nil {
			rows[len(rows)-1][1] = clock(s.length) // the narrow table has the length only
		} else {
			rows[len(rows)-1][1] = stillLine(s)
		}
		a.drawStill(c, lapse, s)
		if !s.gone && s.still != nil {
			name, title := r.Video, r.Name()
			a.playSign(c, lapse)
			play := func() { a.openPlayer(name, title) }
			a.addHit(kit.Hit{R: lapse, Tap: play})
			a.addFocus("Play", lapse, play)
			a.region("Play", lapse)
		}
	} else if video {
		// The still is as high as the row, 16:9.
		k := kit.Cols(lapse, 12, kit.Px(lapse.Dy()*16/9), kit.Fr(1))
		s := a.videoStill(r.Video, a.stillWidth(k[0]))
		rows[len(rows)-1][1] = stillLine(s)
		a.drawStill(c, k[0], s)
		if s.gone || s.still == nil {
			key(c, k[1], "Play", surface, dim, false)
			a.region("Play", k[1])
		} else {
			name, title := r.Video, r.Name()
			a.button(c, k[1], "Play", raised, text, func() { a.openPlayer(name, title) })
		}
	}
	gone := a.recordModel(c, pic, r)
	plate(c, box)
	a.region("Details", box)
	inner := kit.Inset(box, 18, 10, 18, 10)
	for i, kv := range rows {
		if kv[0] == "Note" || kv[0] == "Project" {
			// The note is one line: the start of a long note, and the whole note on the keyboard.
			w := inner.Dx() - gfx.Width(gfx.Face(true, 14), strings.ToUpper(kv[0])) - 16
			rows[i][1] = gfx.Fit(gfx.Face(true, 17), kv[1], w)
		}
	}
	detailRows(c, inner, rows, 30)
	k := kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	a.button(c, k[0], "Note", raised, text, func() { a.editNote(r) })
	a.region("Note", k[0])
	a.button(c, k[1], "Project", raised, text, func() { a.pickProject(r) })
	a.reprintKey(c, k[2], r, gone, orange, onAcc)
}

// pickChoices is the most projects in the choice of the project of a print. New project types
// the name of an older project too.
const pickChoices = 4

// pickProject asks for the project of the finished print r: New project, the newest projects of
// the history, and No project for a print in a project.
func (a *App) pickProject(r printer.Record) {
	choices := []menuItem{{"New project…", "Type a name", dim, func() { a.newProject(r) }}}
	n := 0
	for _, p := range printer.Projects(a.Printer.History()) {
		if p.Name == r.Project || n == pickChoices {
			continue
		}
		n++
		name := p.Name
		choices = append(choices, menuItem{fitText(name, 300), projectLine(p.Prints), dim, func() { a.setProject(r, name) }})
	}
	if r.Project != "" {
		choices = append(choices, menuItem{"No project", "Take the print out of " + fitText(r.Project, 220), dim, func() { a.setProject(r, "") }})
	}
	a.confirm = &confirmBox{question: "Project of " + fitText(r.Name(), 240) + "?", choices: choices}
}

// projectLine is the line of a project in the choice: its number of prints.
func projectLine(n int) string {
	if n == 1 {
		return "1 print"
	}
	return fmt.Sprintf("%d prints", n)
}

// fitText is s cut to w pixels of the face of the rows of a choice.
func fitText(s string, w int) string { return gfx.Fit(gfx.Face(true, 19), s, w) }

// newProject opens the keyboard for the name of a new project of the finished print r.
func (a *App) newProject(r printer.Record) {
	a.textKB = &keyboard{title: "Project of " + r.Name(), value: "", max: printer.MaxProject, ok: "Save", cancel: "Back",
		shift: true, back: func() { a.textKB = nil },
		done: func(v string) {
			a.textKB = nil
			if v = strings.TrimSpace(v); v != "" {
				a.setProject(r, v)
			}
		}}
}

// setProject sets the project of the finished print r to name, or takes it out of its project
// for "".
func (a *App) setProject(r printer.Record, name string) {
	if !a.Printer.SetProject(r, name) {
		a.Toast("The print is gone from the history")
		return
	}
	if a.record.Path == r.Path && a.record.End.Equal(r.End) {
		a.record.Project = name
	}
	if name == "" {
		a.Toast("The print is in no project")
	} else {
		a.Toast("The print is in " + fitText(name, 300))
	}
}

// editNote opens the keyboard for the note of the finished print r, for example why it was
// cancelled. An empty note removes it.
func (a *App) editNote(r printer.Record) {
	a.textKB = &keyboard{title: "A note for " + r.Name(), value: r.Note, max: printer.MaxNote, ok: "Save", cancel: "Back",
		shift: true, back: func() { a.textKB = nil },
		done: func(v string) {
			a.textKB = nil
			v = strings.TrimSpace(v)
			if !a.Printer.SetNote(r, v) {
				a.Toast("The print is gone from the history")
				return
			}
			if a.record.Path == r.Path && a.record.End.Equal(r.End) {
				a.record.Note = v
			}
			if v == "" {
				a.Toast("The note is removed")
			} else {
				a.Toast("The note is saved")
			}
		}}
}

// lapseRowH is the height of the row of the time lapse on a tall record page.
const lapseRowH = 84

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
	if r.Project != "" {
		rows = append(rows, [2]string{"Project", r.Project})
	}
	if r.Note != "" {
		rows = append(rows, [2]string{"Note", r.Note})
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
		c.Text(kit.Inset(pic, 10, 0, 10, 0), "THE FILE IS GONE", gfx.Face(true, 18), lcdDim, gfx.Center)
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
