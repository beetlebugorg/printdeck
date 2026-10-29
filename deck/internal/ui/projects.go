package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The projects of the history: a project is a name on the records of its prints (Project on the
// page of a print). The Projects page lists them with their totals, and a tap opens the prints of
// one project.

// projectRowH is the height of a row of the Projects page, with the gap of 10 under the box.
const projectRowH = 94

// drawProjects lists the projects, the project with the newest print first. A tap opens the
// prints of the project.
func (a *App) drawProjects(c *gfx.Canvas) {
	f := a.navPage(c, "Projects")
	list := printer.Projects(a.Printer.History())
	area := a.listArea(f)
	if len(list) == 0 {
		noItems(c, area, "", "No projects yet. Open a print of the history, then tap Project.")
		return
	}
	l := kit.List{Area: area, N: len(list), RowH: projectRowH, Gap: 10, Scroll: &a.projectsScroll}
	a.list(c, l, func(i int) string { return list[i].Name }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		p := list[i]
		plate(c, box)
		// The two lines are a block in the middle of the row.
		h1, h2 := 30, 26
		in := kit.Inset(box, 18, (box.Dy()-h1-h2)/2, 18, 0)
		s := kit.NewStack(in, 0)
		c.Text(s.Next(h1), gfx.Fit(gfx.Face(true, 19), p.Name, in.Dx()), gfx.Face(true, 19), text, gfx.Left)
		c.Text(s.Next(h2), gfx.Fit(gfx.Face(false, 16), a.projectSummary(p), in.Dx()), gfx.Face(false, 16), dim, gfx.Left)
	}, func(i int) {
		a.project, a.projectScroll = list[i].Name, 0
		a.open(pageProject)
	})
}

// projectSummary is the line of the totals of a project, for example "3 prints · 4:10 · 120 g ·
// Tuesday".
func (a *App) projectSummary(p printer.Project) string {
	m := int(p.Time.Minutes())
	s := fmt.Sprintf("%s · %d:%02d", projectLine(p.Prints), m/60, m%60)
	if p.Filament > 0 {
		s += " · " + a.weight(p.Filament)
	}
	return s + " · " + fileDate(p.Last, a.Now(), a.Clock24)
}

// drawProject lists the prints of the project a.project, newest first. A tap opens the page of
// the print.
func (a *App) drawProject(c *gfx.Canvas) {
	pf := a.navFrame()
	cp, share := a.cloudProjects()
	room := pf.head.Dx() - 200
	if share {
		// The title starts 20 pixels in, and ends 8 pixels before Share.
		room = pf.head.Dx() - 2*(backRect(pf.head).Dx()+12) - 20 - 8
	}
	f := a.navPage(c, fitTitle(a.project, room))
	if share {
		name := a.project
		a.headKey(c, f.head, "Share", func() { a.shareProject(cp, name) })
	}
	recs := printer.OfProject(a.Printer.History(), a.project)
	area := a.listArea(f)
	if len(recs) == 0 {
		noItems(c, area, "", "No prints in this project")
		return
	}
	files := a.fileIndex()
	l := kit.List{Area: area, N: len(recs), RowH: recordRowH(), Gap: 10, Scroll: &a.projectScroll}
	a.list(c, l, func(i int) string { return recs[i].Name() }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		r := recs[i]
		a.recordRow(c, box, r, r.Summary(func(mm float64) float64 { return a.grams(mm, r.Material) }), fileDate(r.End, a.Now(), a.Clock24), files)
	}, func(i int) {
		a.record = recs[i]
		a.open(pageRecord)
	})
}
