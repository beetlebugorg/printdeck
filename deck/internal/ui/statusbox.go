package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The status panel of the touch screen, in the place of the printer display: a row of three small
// readouts (the nozzle, the bed and the fan) and three lines of text over the progress bar. It is
// lower than the printer display, so the screens under it get more space.

// panelH is the height of the status panel, in the place of the display. The large text makes it
// higher.
func panelH() int {
	if gfx.FinePrint() {
		return 160
	}
	return 132
}

// drawStatusBox draws the status panel in r. A tap on the nozzle or the bed sets its
// temperature. Away from the print, the taps wait for its end.
func (a *App) drawStatusBox(c *gfx.Canvas, st printer.State, r image.Rectangle) {
	c.RoundRect(r.Inset(-8), 8, edge)
	c.RoundRect(r.Inset(-6), 6, lcd)
	rows := kit.Rows(r, 0, kit.Px(46), kit.Px(8), kit.Px(24), kit.Px(20), kit.Px(20), kit.Px(6), kit.Fr(1))
	if gfx.FinePrint() {
		rows = kit.Rows(r, 0, kit.Px(60), kit.Px(6), kit.Px(28), kit.Px(26), kit.Px(26), kit.Px(6), kit.Fr(1))
	}
	cells := kit.Cols(rows[0], 8, kit.Fr(1), kit.Fr(1), kit.Fr(1))
	fan := "off"
	if st.Fan > 0 {
		fan = fmt.Sprintf("%.0f%%", st.Fan)
	}
	if a.resin() {
		resinReadouts(c, cells, st)
	} else if st.Reachable {
		readout(c, cells[0], "NOZZLE", fmt.Sprintf("%.0f°", st.Tool), targetText(st.ToolTarget), st.ToolPower, st.ToolTarget > 0)
		readout(c, cells[1], "BED", fmt.Sprintf("%.0f°", st.Bed), targetText(st.BedTarget), st.BedPower, st.BedTarget > 0)
		readout(c, cells[2], "FAN", fan, "", 0, false)
	} else {
		for i, l := range []string{"NOZZLE", "BED", "FAN"} {
			readout(c, cells[i], l, "--", "", 0, false)
		}
	}
	p := a.panelLines(st)
	line := func(r image.Rectangle) image.Rectangle { return kit.Inset(r, 4, 0, 4, 0) }
	l1 := line(rows[2])
	side, head := gfx.Face(true, 16), gfx.Face(true, 18)
	fits := func() bool { return gfx.Width(head, p.head)+gfx.Width(side, p.side)+12 <= l1.Dx() }
	if p.short != "" && !fits() {
		p.side = p.short
	}
	if p.side == clockText(a.Now(), a.Clock24) && !fits() {
		p.side = "" // the clock gives way to the state
	}
	sw := gfx.Width(side, p.side)
	c.Text(l1, gfx.Fit(head, p.head, l1.Dx()-sw-12), head, lcdInk, gfx.Left)
	c.Text(l1, p.side, side, lcdInk, gfx.Right)
	small := gfx.Face(false, 15)
	c.Text(line(rows[3]), gfx.Fit(small, p.msg, line(rows[3]).Dx()), small, lcdInk, gfx.Left)
	if p.shortDetail != "" && gfx.Width(small, p.detail) > line(rows[4]).Dx() {
		p.detail = p.shortDetail
	}
	if label, ok := a.panelProject(st); ok {
		// The print page: the project of the print in place of the layer, which the plate under
		// the panel shows. A tap picks the project.
		pr := line(rows[4])
		col := lcdInk
		if a.Printer.NextProject() == "" {
			col = lcdDim
		}
		aw := gfx.Width(small, "›")
		c.Text(image.Rect(pr.Min.X, pr.Min.Y, pr.Max.X-aw-8, pr.Max.Y), gfx.Fit(small, label, pr.Dx()-aw-8), small, col, gfx.Left)
		c.Text(pr, "›", small, lcdDim, gfx.Right)
		hit := image.Rect(r.Min.X, rows[4].Min.Y-4, r.Max.X, rows[4].Max.Y+4)
		a.addHit(kit.Hit{R: hit, Tap: a.pickNextProject})
		a.addFocus(label, hit, a.pickNextProject)
	} else {
		c.Text(line(rows[4]), gfx.Fit(small, p.detail, line(rows[4]).Dx()), small, lcdDim, gfx.Left)
	}
	if p.bar {
		bar := line(rows[6])
		c.RoundRect(bar, 3, lcdCell)
		if w := int(float64(bar.Dx()) * max(0, min(1, p.progress))); w > 0 {
			c.RoundRect(image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+w, bar.Max.Y), 3, lcdInk)
		}
	}

	a.region("Status", image.Rect(r.Min.X, rows[2].Min.Y, r.Max.X, r.Max.Y))
	if a.resin() {
		a.region("Plate", cells[0])
		a.region("Time", cells[1])
		a.region("Left", cells[2])
		return
	}
	a.region("Nozzle", cells[0])
	a.region("Bed", cells[1])
	a.region("Fan", cells[2])
	if !st.Reachable {
		return
	}
	pr := a.Printer
	nozzle := func() { a.editTemp("Nozzle temperature", int(st.ToolTarget), 300, toolPresets, pr.SetToolTemp) }
	bed := func() { a.editTemp("Bed temperature", int(st.BedTarget), 120, bedPresets, pr.SetBedTemp) }
	if a.bar {
		nozzle = func() { a.Toast(waitText) }
		bed = nozzle
	}
	a.addHit(kit.Hit{R: cells[0], Tap: nozzle})
	a.addHit(kit.Hit{R: cells[1], Tap: bed})
}

// targetText is the target of a heater in a readout: "→210", or "off".
func targetText(t float64) string {
	if t <= 0 {
		return "off"
	}
	return fmt.Sprintf("→%.0f", t)
}

// readout draws one readout of the status panel in r: the name at the top, the value, and the
// target at the right. A heater that is on has a bar of its power at the bottom.
func readout(c *gfx.Canvas, r image.Rectangle, name, value, target string, power float64, heater bool) {
	c.RoundRect(r, 6, lcdCell)
	in := kit.Inset(r, 8, 4, 8, 6)
	nameH := 14
	if gfx.FinePrint() {
		nameH = 24
	}
	c.Text(image.Rect(in.Min.X, in.Min.Y, in.Max.X, in.Min.Y+nameH), name, gfx.Face(true, 11), lcdDim, gfx.Left)
	vr := image.Rect(in.Min.X, in.Min.Y+nameH, in.Max.X, in.Max.Y)
	tf := gfx.Face(false, 13)
	tw := 0
	if target != "" {
		tw = gfx.Width(tf, target) + 4
		c.Text(vr, target, tf, lcdDim, gfx.Right)
	}
	size := 22
	for size > 14 && gfx.Width(gfx.Face(true, size), value) > vr.Dx()-tw {
		size--
	}
	c.Text(vr, value, gfx.Face(true, size), lcdInk, gfx.Left)
	if heater && power > 0 {
		w := int(float64(r.Dx()-12) * min(1, power))
		c.Fill(image.Rect(r.Min.X+6, r.Max.Y-4, r.Min.X+6+w, r.Max.Y-2), orange)
	}
}

// panelText is the text of the status panel: the state and the time at the right in the first
// line, the message in the second, the details in the third, and the progress bar. short and
// shortDetail are a shorter side and a shorter detail for a narrow panel, or "".
type panelText struct {
	head, side, short, msg, detail, shortDetail string
	bar                                         bool
	progress                                    float64
}

// panelLines returns the text of the status panel for the state st.
func (a *App) panelLines(st printer.State) panelText {
	now := a.Now()
	clock := clockText(now, a.Clock24)
	pct := fmt.Sprintf("%.0f%%", st.Completion)
	hm := func(d time.Duration) string {
		m := int(d.Minutes())
		return fmt.Sprintf("%d:%02d", m/60, m%60)
	}
	or := func(s, alt string) string {
		if s != "" {
			return s
		}
		return alt
	}
	switch {
	case !st.Reachable:
		return panelText{head: "Klipper error", side: clock, msg: st.Message}
	case st.ManualProbe:
		return panelText{head: "Paper test", side: fmt.Sprintf("Z %.3f", st.ManualZ), msg: "Lower the nozzle until the paper drags"}
	}
	if st.Busy() {
		p := panelText{side: clock, bar: true, progress: st.Completion / 100}
		ends := ""
		if st.HasTimeLeft {
			p.side, p.short = hm(st.TimeLeft)+" left", hm(st.TimeLeft)
			ends = "Ends " + whenText(now.Add(st.TimeLeft), now, a.Clock24) + " · "
		}
		p.msg = or(st.Message, ends+hm(st.Elapsed)+" so far")
		p.detail, p.shortDetail = a.layerDetail(st)
		switch {
		case st.Paused:
			p.head = "Paused · " + pct
			p.msg = or(st.Message, "Tap Resume to go on")
		case st.Preparing:
			p.head, p.side, p.short, p.bar = "Starting the print", clock, "", false
			p.msg, p.detail, p.shortDetail = or(st.Activity, "Preparing"), st.Name(), ""
		case st.Finishing && st.HasBedCoolLeft && st.BedCoolTo > 0:
			p.head, p.side, p.short = "Cooling the bed", shortWait(st.BedCoolLeft), ""
			p.msg = fmt.Sprintf("The part comes off at %.0f°", st.BedCoolTo)
			p.detail, p.shortDetail = st.Name(), ""
			p.progress = 0
			if span := st.BedCoolFrom - st.BedCoolTo; span > 0 {
				p.progress = (st.BedCoolFrom - st.Bed) / span
			}
		case st.Finishing:
			p.head, p.detail, p.shortDetail = "Finishing the print", st.Name(), ""
		case st.Layer == 1 && st.Layers > 1:
			p.head = "First layer · " + pct
		default:
			p.head = "Printing · " + pct
		}
		return p
	}
	// An upload to a resin printer comes before the end of the last print: it is newer.
	if p, ok := receivePanel(st.Receive, now); ok {
		return p
	}
	if p, ok := uploadPanel(st.Upload, now); ok {
		return p
	}
	switch st.Finished {
	case "complete":
		return panelText{head: "Print done", side: clock, msg: st.FinishedName()}
	case "cancelled":
		return panelText{head: "Cancelled", side: clock, msg: st.FinishedName()}
	case "error":
		return panelText{head: "Print failed", side: clock, msg: st.FinishedName(), detail: st.FinishedError}
	}
	p := panelText{head: "Ready", side: clock}
	homed := "Not homed"
	if st.Homed == "xyz" {
		homed = "XYZ homed"
	}
	if a.resin() {
		// A resin printer homes its plate at the start of a print: the files on its card say more.
		homed = filesLine(len(a.Printer.Files()))
	}
	p.msg = or(st.Message, homed)
	if st.FirmwareOld {
		p.msg = "Firmware is old"
	}
	if st.HasZ && st.Homed != "" {
		p.detail = fmt.Sprintf("X%.0f Y%.0f Z%.2f", st.X, st.Y, st.Z)
	}
	if a.Material != "" && p.detail != "" {
		p.detail += " · " + a.Material
	} else if a.Material != "" {
		p.detail = a.Material
	}
	return p
}

// layerDetail is the line of the layer of a print, "Layer 12 of 27 · next in 1:32 · Z 3.20", and
// its short form, "12/27 · next 1:32 · Z 3.20". The first layer has the Z offset in place of the
// height.
func (a *App) layerDetail(st printer.State) (string, string) {
	if st.Layers <= 0 || st.Layer <= 0 {
		return fmt.Sprintf("Z %.2f", st.Z), ""
	}
	long, short := fmt.Sprintf("Layer %d of %d", st.Layer, st.Layers), fmt.Sprintf("%d/%d", st.Layer, st.Layers)
	if left, ok := a.layerLeft(st, st.Layer); ok {
		long += " · next in " + mmss(left.Seconds())
		short += " · next " + mmss(left.Seconds())
	}
	z := fmt.Sprintf(" · Z %.2f", st.Z)
	if st.Layer == 1 && st.Layers > 1 {
		z = fmt.Sprintf(" · Z offset %+.3f", st.ZOffset)
	}
	return long + z, short + z
}

// panelProject returns the line of the project of the print for the status panel of the print
// page: "Project · Garden planters", or "No project · tap to pick". ok is false on the other
// pages, and while the print starts or ends.
func (a *App) panelProject(st printer.State) (string, bool) {
	if !a.jobPanel || !st.Reachable || !st.Busy() || st.Preparing || st.Finishing {
		return "", false
	}
	if p := a.Printer.NextProject(); p != "" {
		return "Project · " + p, true
	}
	return "No project · tap to pick", true
}

// pickNextProject opens the choice of the project of the print now. A resin print can also get a
// name in place of the name of its file.
func (a *App) pickNextProject() {
	name := a.Printer.State().Name()
	project := func() { a.askProject(name, a.Printer.NextProject(), a.setNextProject) }
	t, ok := a.Printer.(interface{ SetTitle(string) })
	if !ok || !a.resin() {
		project()
		return
	}
	where := "No project"
	if p := a.Printer.NextProject(); p != "" {
		where = fitText(p, 260)
	}
	a.confirm = &confirmBox{question: fitText(name, 300), choices: []menuItem{
		{"Project", where, dim, project},
		{"Name", "A better name for the pages", dim, func() {
			a.textKB = &keyboard{title: "Name of the print", value: name, max: 80, ok: "Save", cancel: "Back",
				shift: true, back: func() { a.textKB = nil },
				done: func(v string) {
					a.textKB = nil
					t.SetTitle(v)
				}}
		}},
	}}
}

// filesLine is the line of the files on the card of a resin printer in the panel.
func filesLine(n int) string {
	switch n {
	case 0:
		return "No files on the card"
	case 1:
		return "1 file on the card"
	}
	return fmt.Sprintf("%d files on the card", n)
}

// uploadShown is the time that the panel shows the end of an upload.
const uploadShown = 10 * time.Minute

// uploadPanel returns the status panel of an upload to the card of a resin printer: while it runs,
// and for uploadShown after its end. ok is false with no upload to show.
func uploadPanel(u printer.Upload, now time.Time) (panelText, bool) {
	mb := func(n int64) string { return fmt.Sprintf("%.1f", float64(n)/1e6) }
	waiting := ""
	if u.Waiting > 0 {
		waiting = fmt.Sprintf(" · %d waiting", u.Waiting)
	}
	switch {
	case u.Name == "":
		return panelText{}, false
	case u.Result == "" && u.Held:
		return panelText{head: "Waits for the end of the print", msg: u.Name,
			detail: "The deck sends it to the printer after the print" + waiting}, true
	case u.Result == "":
		p := panelText{head: "Sending to the printer", msg: u.Name, bar: true}
		if u.StartAfter {
			p.msg = "Prints after: " + u.Name
		}
		if u.Size > 0 {
			p.progress = float64(u.Done) / float64(u.Size)
			p.head += fmt.Sprintf(" · %.0f%%", p.progress*100)
		}
		p.head += " · tap to stop"
		p.detail = mb(u.Done) + " of " + mb(u.Size) + " MB"
		p.shortDetail = p.detail
		if r := u.Rate(now); r > 0 {
			p.detail += fmt.Sprintf(" · %.2f MB/s", r/1e6)
			left := time.Duration(float64(u.Size-u.Done) / r * float64(time.Second))
			p.side, p.short = shortWait(left)+" left", shortWait(left)
		}
		p.detail += waiting
		return p, true
	case now.Sub(u.Ended) > uploadShown && u.Waiting == 0:
		return panelText{}, false
	case u.Result == "failed":
		return panelText{head: "Upload failed", msg: u.Name, detail: u.Err + waiting}, true
	case u.StartAnswer != "":
		head := "Print started"
		if !strings.HasPrefix(u.StartAnswer, "ok") {
			head = "Print did not start"
		}
		return panelText{head: head, msg: u.Name, detail: u.StartAnswer}, true
	default:
		took := u.Ended.Sub(u.Started)
		return panelText{head: "File on the printer", msg: u.Name,
			detail: mb(u.Size) + " MB in " + shortWait(took) + waiting}, true
	}
}

// receivePanel returns the status panel of an upload from ChiTuBox to the deck, while it comes
// in. ChiTuBox sends no size, so the panel shows the MB. An upload with no packet for 15 s is
// over.
func receivePanel(r printer.Receive, now time.Time) (panelText, bool) {
	if r.Name == "" || r.Done || now.Sub(r.Updated) > 15*time.Second {
		return panelText{}, false
	}
	p := panelText{head: "Receiving from ChiTuBox", msg: r.Name}
	p.detail = fmt.Sprintf("%.1f MB", float64(r.Bytes)/1e6)
	if s := r.Updated.Sub(r.Started).Seconds(); s > 1 && r.Bytes > 0 {
		p.detail += fmt.Sprintf(" · %.2f MB/s", float64(r.Bytes)/1e6/s)
	}
	p.shortDetail = p.detail
	return p, true
}
