package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The About page of the printer: how long each heater takes to heat up to each target, and how
// long the bed and the nozzles take to cool after a print (printer.HeatLog). A chart shows the
// time of each heat-up against its target. The deck records the times from the reports of the
// printer only.

// heatTimer is a printer with a heat log.
type heatTimer interface {
	HeatTimes() printer.HeatReport
}

// heaterLabel is the name of a heater of the heat log on the screens.
func heaterLabel(heater string) string {
	switch heater {
	case printer.HeatBed:
		return "Bed"
	case printer.HeatNozzle:
		return "Nozzle"
	case printer.HeatAux:
		return "Aux nozzle"
	case printer.HeatChamber:
		return "Chamber"
	}
	return heater
}

// aboutLine is the line of the About key of the Machine page.
func aboutLine(r printer.HeatReport) string {
	if len(r.Heaters) == 0 && len(r.Cool) == 0 {
		return "No heat-ups yet"
	}
	return "Heat and cool times"
}

// secondsText is a time for the About page, for example "45 s", "2 min 10 s" or "1 h 5 min",
// with no-break spaces: a line does not break in a time.
func secondsText(s float64) string {
	n := int(math.Round(s))
	switch {
	case n < 60:
		return together(fmt.Sprintf("%d s", n))
	case n < 3600:
		if n%60 == 0 {
			return together(fmt.Sprintf("%d min", n/60))
		}
		return together(fmt.Sprintf("%d min %d s", n/60, n%60))
	}
	n = int(math.Round(s / 60))
	if n%60 == 0 {
		return together(fmt.Sprintf("%d h", n/60))
	}
	return together(fmt.Sprintf("%d h %d min", n/60, n%60))
}

// degText is a temperature, for example "60 °C", with a no-break space.
func degText(t float64) string { return together(fmt.Sprintf("%.0f °C", t)) }

// spreadText is the spread of the times of a target: the middle half of the samples.
func spreadText(low, high float64, n int, one, many string) string {
	if n < 2 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("Middle half %s to %s · %d %s", secondsText(low), secondsText(high), n, many)
}

// heatTimesLine is a text of the About page.
type heatTimesLine struct {
	text string
	kind int // 0 a heading, 1 a main line, 2 a detail line
}

// heatTimesLines are the texts of the About page: a heading for each heater, then a line and a
// detail line for each target, and the same for the cool-downs after a print.
func heatTimesLines(r printer.HeatReport) []heatTimesLine {
	var out []heatTimesLine
	for _, ht := range r.Heaters {
		name := heaterLabel(ht.Heater)
		out = append(out, heatTimesLine{strings.ToUpper(name) + " HEAT-UPS", 0})
		for _, tt := range ht.Targets {
			out = append(out, heatTimesLine{fmt.Sprintf("%s to %s: %s typical, from %s", name, degText(tt.Target),
				secondsText(tt.Median), degText(tt.From)), 1})
			detail := spreadText(tt.Low, tt.High, tt.Samples, "heat-up", "heat-ups")
			if tt.Room != nil {
				detail += " · room " + degText(*tt.Room)
			}
			out = append(out, heatTimesLine{detail, 2})
		}
		if f := ht.Fit; f != nil {
			fit := fmt.Sprintf("Estimate: %.1f s for each °C", f.PerDegree)
			if f.Intercept > 0 {
				fit = fmt.Sprintf("Estimate: %s, then %.1f s for each °C", secondsText(f.Intercept), f.PerDegree)
			}
			out = append(out, heatTimesLine{fmt.Sprintf("%s · from %d %s", fit, f.Samples, plural(f.Samples, "heat-up", "heat-ups")), 2})
		}
	}
	if len(r.Cool) > 0 {
		out = append(out, heatTimesLine{"AFTER A PRINT", 0})
		for _, ct := range r.Cool {
			out = append(out, heatTimesLine{fmt.Sprintf("%s to %s: %s typical, from %s", heaterLabel(ct.Heater), degText(ct.To),
				secondsText(ct.Median), degText(ct.From)), 1})
			out = append(out, heatTimesLine{spreadText(ct.Low, ct.High, ct.Samples, "cool-down", "cool-downs"), 2})
		}
	}
	return out
}

// drawAbout is the About page.
func (a *App) drawAbout(c *gfx.Canvas) {
	f := a.navPage(c, "About")
	ht, ok := a.Printer.(heatTimer)
	body := f.body()
	if !ok {
		noItems(c, body, "No heat log", "This printer has no heat log.")
		return
	}
	r := ht.HeatTimes()
	if len(r.Heaters) == 0 && len(r.Cool) == 0 {
		noItems(c, body, "No heat-ups yet", "The deck records each heat-up", "and each cool-down after a print.")
		return
	}
	var chart, list image.Rectangle
	switch {
	case len(r.Heaters) == 0:
		list = body
	case f.wide:
		cols := kit.Cols(body, 12, kit.Fr(1), kit.Fr(1))
		chart, list = cols[0], cols[1]
	default:
		rows := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Px(min(260, body.Dy()/3)), kit.Fr(1))
		chart, list = rows[0], rows[1]
	}
	if !chart.Empty() {
		drawHeatChart(c, chart, r)
		a.region("Heat-up chart", chart)
	}
	a.drawHeatTimesList(c, list, heatTimesLines(r))
}

// The fonts of the lines of the About page, and their line heights.
func heatTimesFace(kind int) (font.Face, color.RGBA, int) {
	fine := gfx.FinePrint()
	switch kind {
	case 0:
		if fine {
			return gfx.Face(true, 18), dim, 30
		}
		return gfx.Face(true, 14), dim, 22
	case 1:
		if fine {
			return gfx.Face(true, 20), text, 30
		}
		return gfx.Face(true, 17), text, 24
	}
	if fine {
		return gfx.Face(false, 17), dim, 26
	}
	return gfx.Face(false, 14), dim, 20
}

// drawHeatTimesList draws the lines in area, each one wrapped to the width of area. The list
// scrolls when the lines do not fit.
func (a *App) drawHeatTimesList(c *gfx.Canvas, area image.Rectangle, lines []heatTimesLine) {
	type part struct {
		y, h  int
		text  string
		face  font.Face
		col   color.RGBA
		lineH int
	}
	width := area.Dx() - 8
	var parts []part
	y := 0
	for i, l := range lines {
		face, col, lineH := heatTimesFace(l.kind)
		if i > 0 && l.kind == 0 {
			y += 12
		}
		if l.kind == 1 && i > 0 && lines[i-1].kind != 0 {
			y += 6
		}
		for _, w := range wrapText(face, l.text, width) {
			parts = append(parts, part{y, lineH, w, face, col, lineH})
			y += lineH
		}
	}
	scroll := a.pageScroll()
	maxScroll := max(0, y-area.Dy())
	*scroll = max(0, min(*scroll, maxScroll))
	c.Clipped(area, func(c *gfx.Canvas) {
		for _, p := range parts {
			top := area.Min.Y + p.y - *scroll
			r := image.Rect(area.Min.X+4, top, area.Max.X-4, top+p.h)
			if r.Overlaps(area) {
				c.Text(r, p.text, p.face, p.col, gfx.Left)
			}
		}
	})
	if maxScroll > 0 {
		a.addHit(kit.Hit{R: area, Scroll: func(dy int) { *scroll = max(0, min(*scroll+dy, maxScroll)) }})
	}
}

// drawHeatChart draws the time of each heat-up against its target in a display window: a mark
// for each sample, a different mark for each heater, and a dotted line for the estimate of each
// heater from its usual start temperature.
func drawHeatChart(c *gfx.Canvas, r image.Rectangle, rep printer.HeatReport) {
	window(c, r)
	in := kit.Inset(r, 14, 8, 14, 8)
	headH, axisH := 24, 16
	small := gfx.Face(true, 11)
	if gfx.FinePrint() {
		headH, axisH = 30, 22
		small = gfx.Face(true, 15)
	}
	// The legend: a mark and the name of each heater, in as many rows as it needs.
	type entry struct {
		heater, name string
		w            int
	}
	var legend [][]entry
	row, rowW := []entry(nil), 0
	for _, ht := range rep.Heaters {
		name := strings.ToUpper(heaterLabel(ht.Heater))
		w := 16 + gfx.Width(small, name) + 20
		if rowW+w > in.Dx() && len(row) > 0 {
			legend, row, rowW = append(legend, row), nil, 0
		}
		row, rowW = append(row, entry{ht.Heater, name, w}), rowW+w
	}
	if len(row) > 0 {
		legend = append(legend, row)
	}
	rows := kit.Rows(in, 2, kit.Px(headH), kit.Px(axisH*len(legend)), kit.Fr(1), kit.Px(axisH))
	c.Text(rows[0], "HEAT-UP TIME BY TARGET", gfx.Face(true, 14), lcdDim, gfx.Left)
	for i, l := range legend {
		x := rows[1].Min.X
		y := rows[1].Min.Y + i*axisH
		for _, e := range l {
			heatMark(c, e.heater, image.Pt(x+4, y+axisH/2))
			c.Text(image.Rect(x+16, y, x+e.w, y+axisH), e.name, small, lcdDim, gfx.Left)
			x += e.w
		}
	}

	// The ranges of the axes: the targets, and the seconds from 0.
	loT, hiT, hiS := math.Inf(1), math.Inf(-1), 0.0
	for _, ht := range rep.Heaters {
		for _, p := range ht.Points {
			loT, hiT, hiS = min(loT, p[0]), max(hiT, p[0]), max(hiS, p[1])
		}
	}
	if math.IsInf(loT, 0) {
		return
	}
	loT, hiT = math.Floor(loT/10)*10-10, math.Ceil(hiT/10)*10+10
	loT = max(0, loT)
	hiS = math.Ceil(hiS/60) * 60
	if hiS <= 0 {
		hiS = 60
	}
	// The label of the time at the top of the axis, at the left, and the targets under the axis.
	topLabel := strings.ToUpper(secondsText(hiS))
	labelW := gfx.Width(small, topLabel) + 8
	plot := kit.Inset(rows[2], labelW, 6, 6, 4)
	if plot.Dx() < 20 || plot.Dy() < 2*axisH {
		return
	}
	c.Text(image.Rect(rows[2].Min.X, plot.Min.Y, plot.Min.X-4, plot.Min.Y+axisH), topLabel, small, lcdDim, gfx.Left)
	c.Text(image.Rect(rows[2].Min.X, plot.Max.Y-axisH, plot.Min.X-4, plot.Max.Y), "0", small, lcdDim, gfx.Left)
	c.Text(image.Rect(plot.Min.X, rows[3].Min.Y, plot.Max.X, rows[3].Max.Y), degText(loT), small, lcdDim, gfx.Left)
	c.Text(image.Rect(plot.Min.X, rows[3].Min.Y, plot.Max.X, rows[3].Max.Y), "TARGET", small, lcdDim, gfx.Center)
	c.Text(image.Rect(plot.Min.X, rows[3].Min.Y, plot.Max.X, rows[3].Max.Y), degText(hiT), small, lcdDim, gfx.Right)

	grid := mix(lcd, lcdDim, 0.3)
	c.Fill(image.Rect(plot.Min.X, plot.Min.Y, plot.Max.X, plot.Min.Y+1), grid)
	mid := plot.Min.Y + plot.Dy()/2
	c.Fill(image.Rect(plot.Min.X, mid, plot.Max.X, mid+1), grid)
	c.Fill(image.Rect(plot.Min.X, plot.Max.Y-1, plot.Max.X, plot.Max.Y), lcdDim)
	c.Fill(image.Rect(plot.Min.X, plot.Min.Y, plot.Min.X+1, plot.Max.Y), lcdDim)
	at := func(target, s float64) image.Point {
		x := plot.Min.X + int((target-loT)/(hiT-loT)*float64(plot.Dx()-1)+0.5)
		y := plot.Max.Y - 1 - int(min(1, s/hiS)*float64(plot.Dy()-1)+0.5)
		return image.Pt(x, y)
	}
	for _, ht := range rep.Heaters {
		// The estimate from the usual start temperature, over the targets of the heater: a
		// dot each 4 pixels.
		if ht.Fit != nil && len(ht.Targets) > 0 {
			from := ht.Targets[0].From
			t0, t1 := ht.Targets[0].Target, ht.Targets[len(ht.Targets)-1].Target
			p0, p1 := at(t0, 0), at(t1, 0)
			for x := p0.X; x <= p1.X; x += 4 {
				t := loT + float64(x-plot.Min.X)/float64(plot.Dx()-1)*(hiT-loT)
				p := at(t, ht.Fit.Seconds(from, t))
				c.Fill(image.Rect(p.X, p.Y, p.X+2, p.Y+1), lcdDim)
			}
		}
		for _, p := range ht.Points {
			heatMark(c, ht.Heater, at(p[0], p[1]))
		}
	}
}

// heatMark draws the mark of a heater at p: a full square for the bed, an open square for the
// nozzle, a cross for the aux nozzle, and a small dot for the chamber.
func heatMark(c *gfx.Canvas, heater string, p image.Point) {
	switch heater {
	case printer.HeatBed:
		c.Fill(image.Rect(p.X-3, p.Y-3, p.X+3, p.Y+3), lcdInk)
	case printer.HeatNozzle:
		c.Fill(image.Rect(p.X-4, p.Y-4, p.X+4, p.Y-2), lcdInk)
		c.Fill(image.Rect(p.X-4, p.Y+2, p.X+4, p.Y+4), lcdInk)
		c.Fill(image.Rect(p.X-4, p.Y-4, p.X-2, p.Y+4), lcdInk)
		c.Fill(image.Rect(p.X+2, p.Y-4, p.X+4, p.Y+4), lcdInk)
	case printer.HeatAux:
		c.Fill(image.Rect(p.X-4, p.Y-1, p.X+4, p.Y+1), lcdDim)
		c.Fill(image.Rect(p.X-1, p.Y-4, p.X+1, p.Y+4), lcdDim)
	default:
		c.Fill(image.Rect(p.X-2, p.Y-2, p.X+2, p.Y+2), lcdDim)
	}
}
