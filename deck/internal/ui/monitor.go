package ui

import (
	"cmp"
	"fmt"
	"image"
	"strings"
	"time"

	"golang.org/x/image/font"

	"printdeck/internal/gfx"
	"printdeck/internal/hostmetrics"
	"printdeck/kit"
)

// The System monitor page: graphs of the CPU use, the memory use and the writes to the SD card
// of the deck, from the samples in memory (hostmetrics). A key at the bottom sets the time of the
// graphs.

// monitorRange is a time of the graphs: its key, the label at the left of the time axis, its
// length and its number of bars.
type monitorRange struct {
	key, axis string
	span      time.Duration
	bars      int
}

// monitorRanges are the times of the graphs. A bar is 6 seconds (3 samples), 30 seconds or 15
// minutes.
var monitorRanges = []monitorRange{
	{"10 min", "−10 MIN", 10 * time.Minute, 100},
	{"1 h", "−1 H", time.Hour, 120},
	{"24 h", "−24 H", 24 * time.Hour, 96},
}

// monitorLine is the line of the System monitor row of the Machine page.
const monitorLine = "CPU, memory and card writes"

// drawMonitor is the System monitor page.
func (a *App) drawMonitor(c *gfx.Canvas) {
	f := a.navPage(c, "System monitor")
	var content, keys image.Rectangle
	if f.wide {
		content, keys = f.split()
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, 10, 0, 0), 12, kit.Fr(1), kit.Px(66))
		content, keys = r[0], r[1]
	}
	for i, k := range kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		fill, ink := keyOff, text
		if i == a.monitorRange {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, k, monitorRanges[i].key, fill, ink, func() { a.monitorRange = i })
	}

	m := a.Metrics
	switch {
	case m == nil || !m.Available():
		noItems(c, content, "No data", "This computer has no /proc files", "for the CPU, the memory and the disk.")
		return
	case m.Len() == 0:
		noItems(c, content, "Waiting for data", fmt.Sprintf("A sample comes every %d seconds.", int(hostmetrics.FineInterval.Seconds())))
		return
	}

	rg := monitorRanges[min(a.monitorRange, len(monitorRanges)-1)]
	now := a.Now()
	// The bars stay at the same times as now moves: the last bar ends at the next multiple of
	// its length.
	w := rg.span / time.Duration(rg.bars)
	end := now.Truncate(w)
	if end.Before(now) {
		end = end.Add(w)
	}
	buckets := m.Window(end.Add(-rg.span), end, rg.bars)
	total := m.Total(now.Add(-rg.span), now)
	last, _ := m.Last()
	minute := m.Total(now.Add(-time.Minute), now)

	panels := kit.Rows(content, 10, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	axis := !f.wide // a wide screen has no room for the time axis

	// The CPU: the share of the time that the CPU was busy.
	cpu := monitorGraph{name: "CPU", value: "—", axis: rg.axis}
	if last.HasCPU {
		cpu.value = pct(last.CPU)
	}
	peak := 0.0
	for _, b := range buckets {
		cpu.bars = append(cpu.bars, b.CPU)
		cpu.has = append(cpu.has, b.HasCPU)
		if b.HasCPU {
			peak = max(peak, b.CPUMax)
		}
	}
	if total.HasCPU {
		cpu.detail = []string{"Avg " + pct(total.CPU), "Peak " + pct(peak)}
	}
	cpu.top = 1
	a.drawGraph(c, panels[0], cpu, axis)

	// The memory: the memory in use, MemTotal minus MemAvailable.
	mem := monitorGraph{name: "Memory", value: "—", axis: rg.axis, top: 1}
	peak = 0
	for _, b := range buckets {
		v := 0.0
		if b.HasMem && b.MemTotal > 0 {
			v = float64(b.MemUsed) / float64(b.MemTotal)
			peak = max(peak, v)
		}
		mem.bars = append(mem.bars, v)
		mem.has = append(mem.has, b.HasMem)
	}
	if last.HasMem && last.MemTotal > 0 {
		mem.value = pct(float64(last.MemUsed) / float64(last.MemTotal))
		mem.detail = []string{bytesText(float64(last.MemUsed)) + " of " + bytesText(float64(last.MemTotal)), "Peak " + pct(peak)}
	}
	a.drawGraph(c, panels[1], mem, axis)

	// The writes to the disk: the write operations in the last minute, and a bar for the
	// writes a minute of each part of the time.
	name := "Disk writes"
	if strings.HasPrefix(m.Disk(), "mmcblk") {
		name = "Card writes"
	}
	wr := monitorGraph{name: name, region: "Writes", value: "—", axis: rg.axis}
	for _, b := range buckets {
		v := b.WritesPerMinute()
		wr.bars = append(wr.bars, v)
		wr.has = append(wr.has, b.HasDisk)
		wr.top = max(wr.top, v)
	}
	if minute.HasDisk {
		secs := int(minute.Covered.Round(time.Second).Seconds())
		wr.value = fmt.Sprintf("%d %s / %d s", minute.Writes, plural(int(minute.Writes), "write", "writes"), secs)
		wr.short = fmt.Sprintf("%d / %d s", minute.Writes, secs)
		wr.detail = []string{bytesText(float64(minute.WriteBytes)) + fmt.Sprintf(" / %d s", secs)}
	}
	if total.HasDisk {
		wr.detail = append(wr.detail, "Avg "+rate(total.WritesPerMinute())+" / min", bytesText(total.BytesPerMinute())+" / min",
			"Peak "+rate(wr.top)+" / min")
	}
	wr.top = max(wr.top, 1)
	a.drawGraph(c, panels[2], wr, axis)
}

// monitorGraph is a panel of the System monitor page: a name and the value now in the top line,
// details under them, and the bars of the graph.
type monitorGraph struct {
	name, value string
	short       string // the value when value does not fit, or ""
	region      string // the name of the panel for the manual; "" is name
	detail      []string
	bars        []float64
	has         []bool // the bar has data
	top         float64
	axis        string // the label at the left of the time axis
}

// drawGraph draws a panel in a display window. The bars go up from the bottom of the graph: top
// is the full height.
func (a *App) drawGraph(c *gfx.Canvas, r image.Rectangle, g monitorGraph, axis bool) {
	window(c, r)
	a.region(cmp.Or(g.region, g.name), r)
	in := kit.Inset(r, 14, 8, 14, 8)
	headH, lineH, axisH := 24, 16, 14
	if gfx.FinePrint() {
		headH, lineH, axisH = 30, 22, 20
	}
	small := gfx.Face(true, 12)
	lines := packLines(small, g.detail, in.Dx())
	if !axis {
		axisH = 0
	}
	rows := kit.Rows(in, 2, kit.Px(headH), kit.Px(lineH*len(lines)), kit.Fr(1), kit.Px(axisH))
	c.Text(rows[0], strings.ToUpper(g.name), gfx.Face(true, 14), lcdDim, gfx.Left)
	nameW := gfx.Width(gfx.Face(true, 14), strings.ToUpper(g.name)) + 16
	// The large text is a little wider in the frame at the scale of the screen: a right-aligned
	// value keeps a margin for it.
	pad := 0
	if gfx.FinePrint() {
		pad = 8
	}
	value := kit.Inset(rows[0], nameW, 0, pad, 0)
	v, face := strings.ToUpper(g.value), gfx.Face(true, 20)
	if gfx.Width(face, v) > value.Dx() && g.short != "" {
		v = strings.ToUpper(g.short)
	}
	c.Text(value, v, face, lcdInk, gfx.Right)
	for i, l := range lines {
		y := rows[1].Min.Y + i*lineH
		c.Text(image.Rect(in.Min.X, y, in.Max.X, y+lineH), strings.ToUpper(l), small, lcdDim, gfx.Left)
	}
	if axis {
		c.Text(rows[3], g.axis, gfx.Face(true, 11), lcdDim, gfx.Left)
		c.Text(rows[3], "NOW", gfx.Face(true, 11), lcdDim, gfx.Right)
	}

	chart := rows[2]
	if chart.Dy() < 4 {
		return
	}
	// A faint line at the top and in the middle, and the base line.
	grid := mix(lcd, lcdDim, 0.3)
	c.Fill(image.Rect(chart.Min.X, chart.Min.Y, chart.Max.X, chart.Min.Y+1), grid)
	mid := chart.Min.Y + chart.Dy()/2
	c.Fill(image.Rect(chart.Min.X, mid, chart.Max.X, mid+1), grid)
	c.Fill(image.Rect(chart.Min.X, chart.Max.Y-1, chart.Max.X, chart.Max.Y), lcdDim)
	n := len(g.bars)
	if n == 0 || g.top <= 0 {
		return
	}
	gap := 0
	if chart.Dx()/n >= 4 {
		gap = 1
	}
	for i, v := range g.bars {
		if !g.has[i] || v <= 0 {
			continue
		}
		x0 := chart.Min.X + i*chart.Dx()/n
		x1 := chart.Min.X + (i+1)*chart.Dx()/n - gap
		h := max(1, int(min(1, v/g.top)*float64(chart.Dy()-1)+0.5))
		c.Fill(image.Rect(x0, chart.Max.Y-1-h, max(x0+1, x1), chart.Max.Y-1), lcdInk)
	}
}

// packLines puts the parts in lines that fit width, with " · " between the parts of a line.
func packLines(face font.Face, parts []string, width int) []string {
	var lines []string
	for _, p := range parts {
		if n := len(lines); n > 0 && gfx.Width(face, strings.ToUpper(lines[n-1]+" · "+p)) <= width {
			lines[n-1] += " · " + p
			continue
		}
		lines = append(lines, p)
	}
	return lines
}

// pct is a share as a whole percent.
func pct(f float64) string { return fmt.Sprintf("%.0f%%", 100*f) }

// rate is a number of writes a minute: one decimal under 10.
func rate(v float64) string {
	if v < 10 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
	}
	return fmt.Sprintf("%.0f", v)
}

// bytesText is a number of bytes in B, KiB, MiB or GiB.
func bytesText(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 || b >= 10 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}

// plural is one for a count of 1, else many.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
