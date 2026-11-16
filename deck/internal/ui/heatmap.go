package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/kit"
)

// The Heat change page: how the bed changes when it heats. The deck keeps the last full map at
// each bed temperature (printer.MeshStore). The page shows a hot map less the coldest map: the
// plate and the gantry are in both maps, so only the change from the heat stays. The Test key
// has the heat sweep (the map at room temperature and at each step of 5 °C up to 60 °C) and the
// drift test (one place of the bed each minute after the bed gets hot).

// heatSweepTop is the top bed temperature of the sweep of the page, in °C.
const heatSweepTop = 60

// heatSub is the line of the Heat change row.
func heatSub(maps []printer.BedMesh) string {
	if len(maps) < 2 {
		return "The bed map at each temperature · sweep"
	}
	return fmt.Sprintf("%d maps, %.0f° to %.0f°", len(maps), maps[0].BedTemp, maps[len(maps)-1].BedTemp)
}

// driftBed is the bed temperature of the drift test, in °C.
const driftBed = 60

// driftBoosts are the boost times of the drift test keys, in minutes.
var driftBoosts = []int{3, 6}

// askHeatTest shows the tests of the Heat change page.
func (a *App) askHeatTest() {
	pr := a.Printer
	temps := printer.HeatTemps(heatSweepTop)
	sweep := func() {
		a.confirm = &confirmBox{question: fmt.Sprintf("Map the bed at room temperature, then each %d° from %d° to %d°? "+
			"The bed cools to %d° first. About %d minutes.", printer.HeatStep, temps[0], temps[len(temps)-1], printer.HeatCold,
			printer.HeatMinutes(heatSweepTop)),
			yes: "Sweep", onYes: func() { a.do(func() error { return pr.HeatSweep(heatSweepTop) }) }}
	}
	items := []menuItem{{"Heat sweep", fmt.Sprintf("A map at each %d° · about %d minutes", printer.HeatStep,
		printer.HeatMinutes(heatSweepTop)), dim, sweep}}
	if pr.Machine().Has("printdeck_settle") {
		items = append(items,
			menuItem{"Drift test", fmt.Sprintf("The bed cools to %d°, heats to %d°, and the probe measures the middle each minute for %d minutes",
				printer.HeatCold, driftBed, printer.DriftMinutes), dim, func() { a.do(func() error { return pr.DriftTest(driftBed, 0) }) }})
		boost := func() {
			var bs []menuItem
			for _, m := range driftBoosts {
				m := m
				bs = append(bs, menuItem{fmt.Sprintf("Boost %d minutes", m), fmt.Sprintf("%d° for %d minutes after the bed gets to %d°",
					driftBed+printer.DriftBoost, m, driftBed), dim, func() { a.do(func() error { return pr.DriftTest(driftBed, m) }) }})
			}
			a.confirm = &confirmBox{question: "Drift test with a boost of the bed", choices: bs}
		}
		items = append(items,
			menuItem{"Drift test, boost", fmt.Sprintf("The bed heats to %d° first, then goes back to %d°", driftBed+printer.DriftBoost, driftBed), dim, boost},
			menuItem{"Last drift results", "How far the bed moved, and how fast", dim, a.showDrift})
	}
	a.confirm = &confirmBox{question: "Heat tests of the bed", choices: items}
}

// showDrift shows the results of the last drift test.
func (a *App) showDrift() {
	pr := a.Printer
	a.do(func() error {
		rs, err := pr.DriftReadings()
		if err != nil {
			return err
		}
		a.post(func() { a.confirm = infoBox("DRIFT TEST", printer.DriftSummary(rs)) })
		return nil
	})
}

// drawHeatMap is the Heat change page.
func (a *App) drawHeatMap(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	f := a.navPage(c, "Heat change")
	mach := pr.Machine()
	idle := st.Connected && !st.Busy()
	if mach.Has("bed_mesh") {
		a.headKey(c, f.head, "Test", func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.askHeatTest()
		})
	}
	body := f.body()
	maps := pr.HeatMaps()
	if len(maps) < 2 {
		noItems(c, body, "No change yet", "Tap Test, then Heat sweep, to map the bed at each temperature",
			"from the room to 60 °C")
		return
	}
	// The hot map of the page: the last one, or the one of its key.
	if a.heatAt < 1 || a.heatAt >= len(maps) {
		a.heatAt = len(maps) - 1
	}
	hot, cold := maps[a.heatAt], maps[0]
	key := fmt.Sprintf("%d %d", hot.Time.UnixNano(), cold.Time.UnixNano())
	if a.heatKey != key {
		a.heatKey, a.heatMesh = key, nil
		if d, ok := printer.HeatChange(hot, cold); ok {
			a.heatMesh = &d
		}
	}
	d := a.heatMesh
	if d == nil {
		noItems(c, body, "The maps do not match", fmt.Sprintf("The maps at %.0f° and %.0f° have other points.",
			hot.BedTemp, cold.BedTemp), "Tap Test, then Heat sweep, to map the bed again")
		return
	}
	lo, hi := d.Range()
	nx, ny := d.Size()
	tiles := [][2]string{
		{"RANGE MM", fmt.Sprintf("%.3f", hi-lo)},
		{"LOWEST", fmt.Sprintf("%+.3f", lo)},
		{"HIGHEST", fmt.Sprintf("%+.3f", hi)},
	}
	var mapR, tileR, foot, keys image.Rectangle
	footH := 22
	if gfx.FinePrint() {
		footH = 28
	}
	if f.wide {
		cols := kit.Cols(body, 14, kit.Fr(1), kit.Px(132))
		r := kit.Rows(cols[0], 6, kit.Fr(1), kit.Px(footH), kit.Px(keyH))
		mapR, foot, keys = r[0], r[1], r[2]
		tileR = kit.Rows(cols[1], 0, kit.Px(3*80+2*12), kit.Fr(1))[0]
		statTiles(c, tileR, 1, tiles)
	} else {
		r := kit.Rows(kit.Inset(body, 0, 10, 0, 0), 12, kit.Fr(1), kit.Px(footH), kit.Px(keyH), kit.Px(84))
		mapR, foot, keys, tileR = r[0], r[1], r[2], r[3]
		statTiles(c, tileR, 3, tiles)
	}
	a.region("Range", tileR)
	pic := a.meshPicture(mapR, mach, d)
	label(c, foot, fmt.Sprintf("%.0f° less %.0f° · %d × %d points · height × %.0f", hot.BedTemp, cold.BedTemp, nx, ny, pic.exag), gfx.Center)
	window(c, mapR)
	a.region("Map", mapR)
	if pic.img != nil {
		c.Image(pic.at, pic.img)
		c.Text(image.Rect(pic.front.X-60, pic.front.Y+2, pic.front.X+60, pic.front.Y+20), "FRONT", gfx.Face(true, 12), lcdDim, gfx.Center)
	}
	// A key for each hot map: its temperature.
	sizes := make([]kit.Size, len(maps)-1)
	for i := range sizes {
		sizes[i] = kit.Fr(1)
	}
	for i, r := range kit.Cols(keys, 8, sizes...) {
		i := i + 1
		fill, ink := keyOff, text
		if i == a.heatAt {
			fill, ink = keyOn, keyOnInk
		}
		a.button(c, r, fmt.Sprintf("%.0f°", maps[i].BedTemp), fill, ink, func() { a.heatAt = i; a.Redraw() })
	}
}
