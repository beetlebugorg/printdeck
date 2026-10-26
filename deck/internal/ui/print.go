package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	lcdscreen "printdeck/internal/lcd"
	"printdeck/kit"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

func duration(d time.Duration) string {
	m := int(d.Minutes())
	if m >= 60 {
		return fmt.Sprintf("%dh %02dm", m/60, m%60)
	}
	return fmt.Sprintf("%d min", max(m, 1))
}

func (a *App) drawPrint(c *gfx.Canvas) {
	if W > H {
		// Under the content: the zones leave the gap free, and a full-screen keyboard covers it.
		divider(c)
	}
	if a.picker != nil {
		a.drawCuraPicker(c)
		return
	}
	if a.colors != nil {
		a.drawColorPicker(c)
		return
	}
	if a.textKB != nil {
		a.drawKeyboard(c, a.textKB)
		return
	}
	if a.form != nil {
		a.drawRollForm(c)
		return
	}
	if a.swap != nil {
		a.drawSwap(c)
		return
	}
	if a.flash != nil {
		a.drawFlash(c)
		return
	}
	if a.actionScreen(c, a.Printer.State()) {
		return
	}
	st := a.Printer.State()
	// A manual Z probe (the paper test) runs: its screen comes first.
	if st.Reachable && st.ManualProbe {
		a.drawManualProbe(c, st)
		return
	}
	a.followPrint(st)
	switch {
	case a.swapAfterPause && st.Paused:
		a.swapAfterPause = false
		a.openSwap(swapChange)
		a.drawSwap(c)
		return
	case a.swapAfterPause && !st.Busy():
		a.swapAfterPause = false // the print ended before the pause
	}
	// The print bar at the bottom of a tall screen: the screens get the space above it. A wide
	// screen has no print bar: its key panel is on each page, and Main is the print.
	if a.bar = a.showsBar(st) && W <= H; a.bar {
		contentY1 = H - barH
		defer a.drawPrintBar(c, st)
	}
	switch a.topPage() {
	case pageFiles:
		a.drawFiles(c)
		return
	case pageFile:
		a.drawFile(c, st)
		return
	case pageMove:
		a.drawMove(c)
		return
	case pageFilament:
		a.drawFilament(c, st)
		return
	case pageMaintenance:
		a.drawMaintenance(c, st)
		return
	case pageMachine:
		a.drawMachine(c, st)
		return
	case pageMacros:
		a.drawMacros(c, st)
		return
	case pageHistory:
		a.drawHistory(c)
		return
	case pageRecord:
		a.drawRecord(c, st)
		return
	case pageSpools:
		a.drawSpools(c)
		return
	case pageSpool:
		a.drawSpool(c)
		return
	case pageSpoolPrints:
		a.drawSpoolPrints(c)
		return
	case pageStats:
		a.drawStats(c)
		return
	case pageSettings:
		a.drawSettings(c)
		return
	case pagePhones:
		a.drawPhones(c)
		return
	case pageMonitor:
		a.drawMonitor(c)
		return
	case pageUpdate:
		a.drawUpdate(c, st)
		return
	case pageConfigs:
		a.drawConfigs(c)
		return
	case pageSimulator:
		a.drawSimulator(c, st)
		return
	case pageCamera:
		a.drawCamera(c)
		return
	case pageCalibrate:
		a.drawCalibrate(c)
		return
	case pageConsole:
		a.drawConsole(c)
		return
	}
	if a.tune {
		a.drawTune(c)
		return
	}
	if !st.Reachable {
		switch {
		case st.NeedsSetup:
			a.drawPrinterSetup(c, st)
		case st.Starting:
			a.drawStarting(c, st)
		default:
			a.drawKlipperError(c, st)
		}
		return
	}
	if st.Busy() {
		switch {
		case a.away:
			// Menu on the screen of the print: the home screen, while the print goes on.
			a.drawHome(c, st)
		case st.Paused:
			a.drawPaused(c, st)
		case st.Preparing:
			a.drawPreparing(c, st)
		case st.Finishing:
			a.drawFinishing(c, st)
		case st.Layer == 1 && st.Layers > 1:
			a.drawFirstLayer(c, st)
		default:
			a.drawJob(c, st)
		}
		return
	}
	if st.Finished != "" && finishedKey(st) != a.doneSeen {
		a.drawDone(c, st)
		return
	}
	a.drawHome(c, st)
}

// stateAreas are the areas of a state screen under its display. A tall screen has one body under
// the display, so keys and view are both the body. A wide screen has its keys in the left column,
// under the display, and the view in the right column.
func (a *App) stateAreas() (f frame, keys, view image.Rectangle) {
	f = a.stateFrame()
	if f.wide {
		return f, f.side, f.main
	}
	// Under the display, in line with its bezel.
	body := image.Rect(f.lcd.Min.X-8, f.main.Min.Y, f.lcd.Max.X+8, f.main.Max.Y-8)
	return f, body, body
}

// statePage draws the frame of a print screen on a wide screen, as a main page: the display and
// the key panel at the left, with Main lit, and the title at the right. The keys of the screen are
// in rows at the bottom of the right column, keysH high, and the view of the screen is over them.
// Main during a print is the print, so the screen needs no Menu.
func (a *App) statePage(c *gfx.Canvas, st printer.State, title string, keysH int) (view, keys image.Rectangle) {
	f := a.navFrame()
	a.statusPanelAt(c, st, f.lcd)
	a.drawNavKeys(c, f.side)
	headingAt(c, f.head, title)
	if keysH <= 0 {
		return f.main, image.Rectangle{}
	}
	r := kit.Rows(f.main, 12, kit.Fr(1), kit.Px(keysH))
	return r[0], r[1]
}

// jobAreas are the keys and the model view of the job screen. The view has the layer slider at its
// bottom edge.
func (a *App) jobAreas() (keys, box image.Rectangle) {
	f, keys, view := a.stateAreas()
	if f.wide {
		// Two rows of two keys: the column is narrow.
		return kit.Rows(keys, 0, kit.Px(2*keyRowH+10))[0], view
	}
	r := kit.Rows(view, 12, kit.Px(keyRowH), kit.Fr(1))
	return r[0], r[1]
}

// drawJob is the screen of a running print: the status display, the keys, and the model view.
func (a *App) drawJob(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	if W > H {
		// Wide: the print is the Main page. The data and the model, and the keys at the bottom:
		// Cancel, the destructive action, at the left, and Pause at the right.
		view, keys := a.statePage(c, st, gfx.Fit(gfx.Face(true, 24), st.File, a.navFrame().head.Dx()-20), actH)
		k := kit.Cols(keys, 12, kit.Fr(1), kit.Fr(1), kit.Fr(1))
		a.button(c, k[0], "Cancel", raised, red, a.askCancel)
		a.button(c, k[1], "Tune", raised, text, func() { a.tune = true })
		a.button(c, k[2], "Pause", orange, onAcc, pr.Pause)
		r := kit.Rows(view, 12, kit.Px(a.printInfoH(st)), kit.Fr(1))
		a.modelView(c, r[1], st)
		a.drawScrubber(c, r[1], st)
		a.printInfo(c, r[0], st)
		return
	}
	a.statusPanel(c, st)
	keys, box := a.jobAreas()
	if f, _, _ := a.stateAreas(); f.wide {
		// Under the keys in the left column: the file, the time left and the end of the print.
		_, side, _ := a.stateAreas()
		a.jobCard(c, kit.Rows(side, 12, kit.Px(keys.Dy()), kit.Fr(1))[1], st)
	}
	k := kit.Cols(keys, 8, kit.Fr(1), kit.Fr(1), kit.Fr(1), kit.Fr(1))
	if keys.Dx() < 320 {
		k = kit.Grid(keys, 2, 2, 10) // two rows of two in a narrow column
	}
	a.button(c, k[0], "Pause", orange, onAcc, pr.Pause)
	a.button(c, k[1], "Tune", raised, text, func() { a.tune = true })
	a.button(c, k[2], "Cancel", raised, red, a.askCancel)
	a.button(c, k[3], "Menu", raised, text, func() { a.leavePrint(st) })
	// The data of the print in a plate, and the model under it.
	r := kit.Rows(box, 12, kit.Px(a.printInfoH(st)), kit.Fr(1))
	a.modelView(c, r[1], st)
	a.drawScrubber(c, r[1], st)
	a.printInfo(c, r[0], st)
}

// jobCard draws the file of the print, the time left, the time of its end, and the time when the
// bed is cool enough to remove the part, in a plate.
func (a *App) jobCard(c *gfx.Canvas, r image.Rectangle, st printer.State) {
	plate(c, r)
	in := kit.Inset(r, 18, 10, 18, 10)
	s := kit.NewStack(in, 4)
	c.Text(s.Next(30), st.File, gfx.Face(true, 20), text, gfx.Left)
	// The model view shows the layer.
	if st.HasTimeLeft {
		// The time left and the end on two lines: the column is narrow.
		end := clockText(a.Now().Add(st.TimeLeft), a.Clock24)
		c.Text(s.Next(26), duration(st.TimeLeft)+" left", gfx.Face(false, 17), dim, gfx.Left)
		c.Text(s.Next(26), "Done at "+end, gfx.Face(false, 17), dim, gfx.Left)
	}
	if st.HasReleaseLeft {
		end := clockText(a.Now().Add(st.ReleaseLeft), a.Clock24)
		c.Text(s.Next(26), "Cooled at "+end, gfx.Face(false, 17), dim, gfx.Left)
	}
}

// askCancel asks before it cancels the print.
func (a *App) askCancel() {
	a.confirm = &confirmBox{question: "Cancel this print?", yes: "Cancel print", danger: true, onYes: a.Printer.Cancel}
}

// modelView draws the model in a window. A picture that is larger than the window becomes smaller
// to fit. The window has only the model: the data of the print is in the plate above it
// (printInfo).
func (a *App) modelView(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	window(c, box)
	a.region("Model", box)
	pic := box
	if st.Layers > 0 {
		pic.Max.Y -= 40 // the layer slider
	}
	if img := a.Printer.Preview(); img != nil {
		picture(c, pic, img)
	} else {
		c.Text(pic, "LOADING MODEL…", gfx.Face(true, 18), lcdDim, gfx.Center)
	}
}

// nowLineH is the height of the line of the move in printInfo with the large text.
const nowLineH = 24

// printInfoH is the height of the plate of printInfo for st.
func (a *App) printInfoH(st printer.State) int {
	h := 12 + 30 + 12
	if gfx.FinePrint() && st.HasNow && a.Printer.ViewLayer() < 0 {
		h += nowLineH + 8 // the line of the move
	}
	if _, ok := a.rollIn(); ok {
		h += 32
	}
	if len(a.Printer.LayerTimes()) > 0 {
		h += 54
	}
	return h
}

// printInfo draws the data of the print in a plate above the model view: the layer and the move
// now, the roll in the printer, and the times of the layers. While the slider shows another layer,
// Live goes back to the layer that prints.
func (a *App) printInfo(c *gfx.Canvas, r image.Rectangle, st printer.State) {
	plate(c, r)
	a.layers.sample(st, a.Now())
	s := kit.NewStack(kit.Inset(r, 16, 12, 16, 12), 8)
	top := s.Next(30)
	pr := a.Printer
	view := pr.ViewLayer()
	if st.Layers > 0 {
		shown, col := st.Layer, text
		if v, ok := a.dragLayer(st); ok {
			shown, col = v, orange
		} else if view >= 0 {
			shown, col = view, orange
		}
		t := fmt.Sprintf("LAYER %d OF %d", shown, st.Layers)
		c.Text(image.Rect(top.Min.X, top.Min.Y, top.Max.X-120, top.Max.Y), t, gfx.Face(true, 16), col, gfx.Left)
		a.textRegion("Layer", top, gfx.Face(true, 16), t)
	}
	switch {
	case view >= 0:
		live := image.Rect(top.Max.X-90, top.Min.Y-4, top.Max.X, top.Max.Y+2)
		a.button(c, live, "Live", orange, onAcc, func() { pr.SetViewLayer(-1) })
	case st.HasNow:
		// The kind of the move now, and its speed: the live speed of the head, or the speed of the
		// G-code.
		speed := st.NowSpeed * float64(a.speedFactor(st)) / 100
		if st.HasLive {
			speed = st.LiveSpeed
		}
		now := fmt.Sprintf("%s · %.0f MM/S", strings.ToUpper(st.NowKind), speed)
		if gfx.FinePrint() {
			// The large text: the move has a line of its own, under the layer.
			c.Text(s.Next(nowLineH), now, gfx.Face(true, 14), dim, gfx.Left)
		} else {
			c.Text(image.Rect(top.Min.X+180, top.Min.Y, top.Max.X, top.Max.Y), now, gfx.Face(true, 14), dim, gfx.Right)
		}
	}
	if _, ok := a.rollIn(); ok {
		a.rollGauge(c, s.Next(24), st)
	}
	if times := pr.LayerTimes(); len(times) > 0 {
		a.layerChart(c, s.Next(46), st, times)
	}
}

// layerClock measures the time of each layer of a print: from the change to the layer to the
// change to the next one. A layer with a pause, and the layer that printed when the deck started,
// have no time.
type layerClock struct {
	file    string
	layer   int
	start   time.Time
	known   bool // start is the start of the layer
	paused  bool // the layer had a pause
	measure map[int]float64
}

// sample follows the layer of st at the time now.
func (l *layerClock) sample(st printer.State, now time.Time) {
	switch {
	case !st.Busy():
		*l = layerClock{}
		return
	case st.File != l.file:
		*l = layerClock{file: st.File, layer: st.Layer, start: now, measure: map[int]float64{}}
		return
	}
	if st.Paused {
		l.paused = true
	}
	if st.Layer != l.layer {
		if st.Layer == l.layer+1 && l.known && !l.paused {
			l.measure[l.layer] = now.Sub(l.start).Seconds()
		}
		l.layer, l.start, l.known, l.paused = st.Layer, now, true, false
	}
}

// layerChart draws the times of the layers in r: a bar for each layer, or for each group of layers
// on a narrow chart. A layer that is done has its measured time when the deck has it, and the
// others the time of the slicer. The layers that are done are dark, the layer now is orange, and
// the layers to come are light. At the right: the time of the layer now and its plan, and the
// mean time of a layer.
func (a *App) layerChart(c *gfx.Canvas, r image.Rectangle, st printer.State, plan []float64) {
	n := len(plan)
	cur := st.Layer // counting from 1
	at := func(layer int) float64 {
		if t, ok := a.layers.measure[layer]; ok {
			return t
		}
		return plan[layer-1]
	}
	sum, count := 0.0, 0
	for layer, t := range a.layers.measure {
		if layer >= 1 && layer <= n {
			sum, count = sum+t, count+1
		}
	}
	if count == 0 {
		for _, t := range plan {
			sum += t
		}
		count = n
	}
	avg := "AVG " + mmss(sum/float64(count)) + " A LAYER"
	// The numbers at the right: wider for the large text.
	nums := image.Rect(r.Max.X-max(150, gfx.Width(gfx.Face(true, 12), avg)), r.Min.Y, r.Max.X, r.Max.Y)
	now, of := "–", ""
	if cur >= 1 && cur <= n {
		of = " / " + mmss(plan[cur-1])
		if a.layers.known && a.layers.layer == cur {
			now = mmss(a.Now().Sub(a.layers.start).Seconds())
		}
	}
	c.Text(image.Rect(nums.Min.X, r.Min.Y, nums.Max.X, r.Min.Y+24), now+of, gfx.Face(true, 17), text, gfx.Right)
	c.Text(image.Rect(nums.Min.X, r.Min.Y+24, nums.Max.X, r.Max.Y), avg, gfx.Face(true, 12), dim, gfx.Right)

	chart := image.Rect(r.Min.X, r.Min.Y+2, nums.Min.X-16, r.Max.Y-2)
	// One bar for each layer when they fit, else for each group of layers.
	const gap = 1
	per := 1
	for n/per*(2+gap) > chart.Dx() {
		per++
	}
	bars := (n + per - 1) / per
	barW := min(10, max(2, chart.Dx()/max(1, bars)-gap))
	vals := make([]float64, bars)
	top := 1.0
	for b := range vals {
		k := 0
		for layer := b*per + 1; layer <= min(n, (b+1)*per); layer++ {
			vals[b] += at(layer)
			k++
		}
		vals[b] /= float64(max(1, k))
		top = max(top, vals[b])
	}
	c.Fill(image.Rect(chart.Min.X, chart.Max.Y-1, chart.Max.X, chart.Max.Y), edge)
	later := mix(surface, dim, 0.3)
	for b, v := range vals {
		x := chart.Min.X + b*(barW+gap)
		if x+barW > chart.Max.X {
			break
		}
		first, last := b*per+1, min(n, (b+1)*per)
		col := later
		switch {
		case cur >= first && cur <= last:
			col = orange
		case last < cur:
			col = dim
		}
		h := max(1, int(v/top*float64(chart.Dy()-1)))
		c.Fill(image.Rect(x, chart.Max.Y-1-h, x+barW, chart.Max.Y-1), col)
	}
	a.region("Layer times", r)
}

// mmss is a time in seconds as m:ss, or h:mm:ss from an hour.
func mmss(sec float64) string {
	s := int(sec + 0.5)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// DemoLayers measures the layers of the demo print before the layer now, at the plan with some
// spread, and starts the layer now 42 seconds ago. For the pictures of the manual.
func (a *App) DemoLayers() {
	st := a.Printer.State()
	plan := a.Printer.LayerTimes()
	now := a.Now()
	a.layers = layerClock{file: st.File, layer: st.Layer, start: now.Add(-42 * time.Second), known: true, measure: map[int]float64{}}
	for layer := 1; layer < st.Layer && layer <= len(plan); layer++ {
		a.layers.measure[layer] = plan[layer-1] * (1 + 0.08*math.Sin(float64(layer)))
	}
}

// rollGauge draws the roll in the printer in r: a dot in the color of the roll, a bar that shows
// the filament on the roll, its name, and its grams. The filament of the print so far counts
// already, so the bar goes down while the print runs. It reports false when no roll is in the
// printer, and then draws nothing.
func (a *App) rollGauge(c *gfx.Canvas, r image.Rectangle, st printer.State) bool {
	sp, ok := a.rollIn()
	if !ok {
		return false
	}
	left := max(0, sp.Left()-a.grams(st.FilamentUsed, ""))
	full := sp.Capacity()
	// The dot has a light ring, so that a dark color shows on the window. A roll with no color has
	// the color of the display.
	d := r.Dy()
	dot := image.Rect(r.Min.X, r.Min.Y, r.Min.X+d, r.Max.Y)
	c.RoundRect(dot, d/2, lcdDim)
	if col, ok := parseHex(sp.Color); ok {
		c.RoundRect(dot.Inset(3), d/2-3, col)
	} else {
		c.RoundRect(dot.Inset(3), d/2-3, lcd)
	}
	bar := image.Rect(dot.Max.X+10, r.Min.Y+7, dot.Max.X+10+64, r.Max.Y-7)
	segments(c, bar, min(1, left/full))
	face := gfx.Face(true, 13)
	t := image.Rect(bar.Max.X+12, r.Min.Y, r.Max.X, r.Max.Y)
	line := strings.ToUpper(sp.Number() + " " + sp.Label() + " · " + printer.Weight(left))
	c.Text(t, line, face, dim, gfx.Left)
	a.region("Roll", r)
	return true
}

// rollIn is the roll in the printer.
func (a *App) rollIn() (printer.Spool, bool) {
	if rolls := a.Printer.SpoolStore(); rolls != nil {
		return rolls.In()
	}
	return printer.Spool{}, false
}

// picture draws a picture of the model in the middle of a window box. A picture that is larger
// than the window becomes smaller to fit.
func picture(c *gfx.Canvas, box image.Rectangle, img image.Image) {
	b, in := img.Bounds(), box.Inset(2)
	if b.Dx() > in.Dx() || b.Dy() > in.Dy() {
		s := min(float64(in.Dx())/float64(b.Dx()), float64(in.Dy())/float64(b.Dy()))
		c.Cover(kit.Center(in, int(float64(b.Dx())*s), int(float64(b.Dy())*s)), img)
		return
	}
	c.Clipped(in, func(in *gfx.Canvas) {
		in.Image(image.Rect(box.Min.X+(box.Dx()-b.Dx())/2, box.Min.Y+(box.Dy()-b.Dy())/2, box.Max.X, box.Max.Y), img)
	})
}

// speedFactor is the speed in percent of the print (M220).
func (a *App) speedFactor(st printer.State) int {
	if st.SpeedPct > 0 {
		return st.SpeedPct
	}
	return 100
}

// lcdLines are the two lines of 20 characters of the character display.
func (a *App) lcdLines(st printer.State) []string {
	lr := func(l, r string) string { // l at the left, r at the right, 20 characters
		n := 20 - len([]rune(r))
		ls := []rune(l)
		if len(ls) > n-1 {
			ls = ls[:max(0, n-1)]
		}
		return string(ls) + strings.Repeat(" ", n-len(ls)) + r
	}
	right := fmt.Sprintf("%.0f%%", st.Completion)
	if st.HasTimeLeft {
		m := int(st.TimeLeft.Minutes())
		right += fmt.Sprintf(" %d:%02d", m/60, m%60)
	}
	// The second line: the move now and its speed, and every 4 seconds the time of the finish on
	// the clock, as the display of a printer alternates its lines.
	now := "--"
	if st.HasNow {
		now = lr(strings.ToUpper(st.NowKind), fmt.Sprintf("%.0fMM/S", st.NowSpeed*float64(a.speedFactor(st))/100))
	}
	if st.HasTimeLeft && (!st.HasNow || a.Now().Unix()/4%2 == 1) {
		now = lr("DONE AT", lcdscreen.Clock(a.Now().Add(st.TimeLeft), a.Clock24))
	}
	return []string{lr(strings.ToUpper(st.Activity), right), now}
}

// temps draws the nozzle and the bed side by side in r: the temperature, and the target while the
// heater is on. A tap opens the keypad for the target.
func (a *App) temps(c *gfx.Canvas, area image.Rectangle, tool, toolT, bed, bedT float64, ok bool) {
	a.region("Temperatures", area)
	h := area.Dy()
	card := func(r image.Rectangle, icon func(cx, cy, r int, col color.RGBA), name string, v, target float64) {
		plate(c, r)
		ic := dim
		if ok && target > 0 {
			ic = orange // heater on
		}
		// A narrow card has a smaller icon, closer to the edge, and a value that fits.
		ix, tx, ir := 34, 62, 15
		if r.Dx() < 180 {
			ix, tx, ir = 22, 40, 11
		}
		icon(r.Min.X+ix, r.Min.Y+h/2, ir, ic)
		label(c, image.Rect(r.Min.X+tx, r.Min.Y+8, r.Max.X-14, r.Min.Y+28), name, gfx.Left)
		val := "—"
		if ok {
			val = fmt.Sprintf("%.0f°", v)
		}
		valR := image.Rect(r.Min.X+tx, r.Min.Y+26, r.Max.X-10, r.Max.Y-4)
		// The large text: a target that does not fit at the right of the label goes at the right of
		// the value.
		x0 := r.Min.X + tx + gfx.Width(gfx.Face(true, 14), strings.ToUpper(name)) + 8
		setR := image.Rectangle{}
		if set := fmt.Sprintf("%.0f°", target); gfx.FinePrint() && ok && target > 0 && r.Dx() >= 180 &&
			gfx.Width(gfx.Face(true, 13), set) > r.Max.X-10-x0 {
			setR = valR
			setR.Min.X = setR.Max.X - gfx.Width(gfx.Face(true, 13), set)
			valR.Max.X = setR.Min.X - 8
			c.Text(setR, set, gfx.Face(true, 13), orange, gfx.Right)
		}
		size := 30
		for size > 18 && gfx.Width(gfx.Face(true, size), val) > valR.Dx() {
			size -= 2
		}
		c.Text(valR, val, gfx.Face(true, size), text, gfx.Left)
		if ok && target > 0 && setR.Empty() {
			set := fmt.Sprintf("SET %.0f°", target)
			if r.Dx() < 180 { // a narrow card: under the value
				c.Text(image.Rect(r.Min.X+tx, r.Max.Y-24, r.Max.X-10, r.Max.Y-6), set, gfx.Face(true, 13), orange, gfx.Left)
			} else {
				// The target goes at the right of the label. Large text leaves room for the number only.
				if gfx.Width(gfx.Face(true, 13), set) > r.Max.X-10-x0 {
					set = fmt.Sprintf("%.0f°", target)
				}
				c.Text(image.Rect(x0, r.Min.Y+6, r.Max.X-10, r.Min.Y+28), set, gfx.Face(true, 13), orange, gfx.Right)
			}
		}
	}
	cards := kit.Cols(area, 16, kit.Fr(1), kit.Fr(1))
	nozzle, bedR := cards[0], cards[1]
	card(nozzle, func(cx, cy, r int, col color.RGBA) { drawNozzle(c, cx, cy, r, col) }, "Nozzle", tool, toolT)
	card(bedR, func(cx, cy, r int, col color.RGBA) { drawBed(c, cx, cy, r, col) }, "Bed", bed, bedT)
	if ok { // a tap opens the keypad for the target
		pr := a.Printer
		a.addHit(kit.Hit{R: nozzle, Tap: func() { a.editTemp("Nozzle temperature", int(toolT), 300, toolPresets, pr.SetToolTemp) }})
		a.addHit(kit.Hit{R: bedR, Tap: func() { a.editTemp("Bed temperature", int(bedT), 120, bedPresets, pr.SetBedTemp) }})
	}
}

// drawScrubber is the slider at the bottom of the model view: it goes through the layers. While
// a finger drags it, a.scrubLayer is the layer under the finger, for the plate above (printInfo).
func (a *App) drawScrubber(c *gfx.Canvas, box image.Rectangle, st printer.State) {
	a.scrubLayer = -1
	if st.Layers == 0 {
		return
	}
	pr := a.Printer
	view := pr.ViewLayer()
	layerOf := func(f float64) int { return int(f*float64(st.Layers) + 0.5) }

	slider := image.Rect(box.Min.X+18, box.Max.Y-36, box.Max.X-18, box.Max.Y-4)
	a.region("Layer slider", slider)
	f := float64(st.Layer) / float64(st.Layers)
	drag, dragging := a.in.Dragging(slider)
	switch {
	case dragging:
		f = drag
		a.scrubLayer = layerOf(f)
	case view >= 0:
		f = float64(view) / float64(st.Layers)
	}
	set := func(f float64) { pr.SetViewLayer(layerOf(f)) }
	faderWith(c, slider, f, orange, dayPalette.raised) // a light cap on the dark window
	a.addHit(kit.Hit{R: slider, Drag: set, End: set})
}

// dragLayer is the layer under the finger on the layer slider.
func (a *App) dragLayer(printer.State) (int, bool) { return a.scrubLayer, a.scrubLayer >= 0 }
