package ui

import (
	"fmt"
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The menu pages of the idle screen: Maintenance and Machine, and the pages that Machine opens.

const menuTop, menuRowH = 124, 98

// drawMaintenance lists the calibrations that the printer has (the modules of Klipper).
func (a *App) drawMaintenance(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Maintenance")
	a.backButton(c)
	mach := pr.Machine()
	idle := st.Connected && !st.Busy()

	type row struct {
		title, sub string
		warn       bool
		tap        func()
	}
	var rows []row
	run := func(question, yes string, lines ...string) func() {
		return func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.confirm = &confirmBox{question: question, yes: yes, onYes: func() {
				a.Toast("Running…")
				a.do(func() error {
					if err := pr.Gcode(lines...); err != nil {
						return err
					}
					a.Toast("Done")
					return nil
				})
			}}
		}
	}
	home := func() []string {
		if st.Homed == "xyz" {
			return nil
		}
		return []string{"G28"}
	}
	if mach.Has("bed_mesh") {
		rows = append(rows, row{"Bed mesh", "Probe the bed and map its height", false,
			run("Probe the bed now?", "Probe", append(home(), "BED_MESH_CALIBRATE")...)})
	}
	if mach.Has("bed_tilt") {
		rows = append(rows, row{"Bed tilt", "Probe the bed and correct its tilt", false,
			run("Probe the bed now?", "Probe", append(home(), "BED_TILT_CALIBRATE")...)})
	}
	if mach.Has("screws_tilt_adjust") {
		rows = append(rows, row{"Screw adjust", "How far to turn each bed screw", false,
			run("Probe the bed screws now?", "Probe", append(home(), "SCREWS_TILT_CALCULATE")...)})
	}
	if mach.Has("probe") {
		rows = append(rows, row{"Probe Z offset", "Find the nozzle height with a sheet of paper", false,
			run("Start the paper test?", "Start", append(home(), "PROBE_CALIBRATE")...)})
	}
	rows = append(rows, row{"Nozzle change", "Heat to 250°, and raise the head", false, func() {
		if !idle {
			a.Toast("Wait until the print ends")
			return
		}
		lines := append(home(), "G90", fmt.Sprintf("G1 Z%.0f F600", min(st.Axis[1][2], max(st.Z, 0)+50)), "M104 S250")
		if st.Axis[1][2] == 0 {
			lines = append(home(), "G91", "G1 Z50 F600", "G90", "M104 S250")
		}
		a.confirm = &confirmBox{question: "Heat the nozzle to 250°?", yes: "Heat", onYes: func() {
			a.do(func() error { return pr.Gcode(lines...) })
		}}
	}})
	if st.SaveConfigPending {
		rows = append(rows, row{"Save config", "Calibration results are not saved yet", true, func() {
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			a.confirm = &confirmBox{question: "Save the config? Klipper restarts.", yes: "Save", onYes: func() {
				a.do(func() error { return pr.Gcode("SAVE_CONFIG") })
			}}
		}})
	} else {
		rows = append(rows, row{"Save config", "No unsaved changes", false, nil})
	}
	for i, r := range rows {
		col := dim
		if r.warn {
			col = orange
		}
		a.menuRow(c, image.Rect(24, menuTop+i*menuRowH, W-24, menuTop+i*menuRowH+86), r.title, r.sub, col, r.tap)
	}
}

// drawManualProbe is the paper test of a manual Z probe: move the nozzle until a sheet of paper
// drags a little, then accept. It shows by itself while Klipper runs a manual probe.
func (a *App) drawManualProbe(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	charLCD(c, printLCD, lcd2("Paper test", "", "Nozzle at Z", fmt.Sprintf("%.3f", st.ManualZ)), 20)
	box := image.Rect(24, 170, W-24, 330)
	plate(c, box)
	lines := []string{"Put a sheet of paper under the", "nozzle. Lower the nozzle until the", "paper drags a little when you move", "it, then tap Accept."}
	for i, l := range lines {
		c.Text(image.Rect(box.Min.X+18, box.Min.Y+14+i*34, box.Max.X-18, box.Min.Y+48+i*34), l, gfx.Face(false, 18), text, gfx.Left)
	}
	steps := []float64{-1, -0.1, -0.025, 0.025, 0.1, 1}
	third := (W - 48 - 16) / 3
	for i, s := range steps {
		r := image.Rect(24+(i%3)*(third+8), 346+(i/3)*84, 24+(i%3)*(third+8)+third, 346+(i/3)*84+74)
		v := s
		name := fmt.Sprintf("%+g", s)
		a.button(c, r, name, raised, text, func() { a.do(func() error { return pr.Gcode(fmt.Sprintf("TESTZ Z=%g", v)) }) })
	}
	label(c, image.Rect(24, 516, W-24, 540), "− lowers the nozzle, + raises it (mm)", gfx.Center)
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, 560, 24+half, 650), "Accept", orange, onAcc, func() {
		a.do(func() error {
			if err := pr.Gcode("ACCEPT"); err != nil {
				return err
			}
			a.Toast("Accepted. Save config in Maintenance writes it.")
			return nil
		})
	})
	a.button(c, image.Rect(W-24-half, 560, W-24, 650), "Abort", raised, red, func() { a.do(func() error { return pr.Gcode("ABORT") }) })
}

// drawMachine is the Machine page: the versions and the totals, and the advanced pages.
func (a *App) drawMachine(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Machine")
	a.backButton(c)
	mach := pr.Machine()
	info := image.Rect(24, 122, W-24, 250)
	window(c, info)
	fw := "—"
	switch {
	case st.Firmware != "" && st.FirmwareOld:
		fw = "OLD: FLASH IT"
	case st.Firmware != "":
		fw = "CURRENT"
	}
	ip := "—"
	if a.Setup != nil {
		if s, err := a.Setup.Status(); err == nil && s.IP != "" {
			ip = s.IP
		}
	}
	var prints, hours int
	var filament float64
	for _, r := range pr.History() {
		prints++
		hours += int(r.Duration.Hours())
		filament += r.Filament
	}
	lines := [][2]string{
		{"KLIPPER", strings.TrimSuffix(mach.KlipperVersion, "-printdeck-go")},
		{"FIRMWARE", fw},
		{"NETWORK", ip},
		{"PRINTS", fmt.Sprintf("%d · %d H · %.0f M", prints, hours, filament/1000)},
	}
	for i, l := range lines {
		y := info.Min.Y + 10 + i*28
		c.Text(image.Rect(info.Min.X+16, y, info.Max.X, y+28), l[0], gfx.Face(true, 14), lcdDim, gfx.Left)
		c.Text(image.Rect(info.Min.X+130, y, info.Max.X-16, y+28), gfx.Fit(gfx.Face(true, 17), l[1], info.Dx()-150), gfx.Face(true, 17), lcdInk, gfx.Right)
	}

	macros := "No macros"
	if n := len(mach.Macros); n > 0 {
		macros = strings.Join(mach.Macros[:min(2, n)], ", ")
		if n > 2 {
			macros += fmt.Sprintf(", %d more", n-2)
		}
	}
	rows := []struct {
		t, s string
		tap  func()
	}{
		{"Print history", fmt.Sprintf("%d prints, with the result", prints), func() { a.open(pageHistory) }},
		{"Macros", macros, func() { a.open(pageMacros) }},
	}
	if a.Setup != nil {
		rows = append(rows, struct {
			t, s string
			tap  func()
		}{"Wi-Fi and help", "The network, the password, the addresses", a.OpenHelp})
	}
	for i, r := range rows {
		a.menuRow(c, image.Rect(24, 262+i*94, W-24, 262+i*94+84), r.t, r.s, dim, r.tap)
	}
	half := (W - 48 - 12) / 2
	a.button(c, image.Rect(24, 642, 24+half, 712), "Restart Klipper", raised, text, func() {
		a.confirm = &confirmBox{question: "Restart Klipper?", yes: "Restart", onYes: func() {
			a.Toast("Restarting Klipper…")
			a.do(pr.RestartKlipper)
		}}
	})
	a.button(c, image.Rect(W-24-half, 642, W-24, 712), "Firmware restart", raised, red, func() {
		a.confirm = &confirmBox{question: "Restart the printer firmware?", yes: "Restart", danger: true, onYes: func() {
			a.Toast("Restarting the firmware…")
			a.do(pr.FirmwareRestart)
		}}
	})
}

// drawMacros lists the macros of the printer. A tap runs one, after a question.
func (a *App) drawMacros(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	heading(c, "Macros")
	a.backButton(c)
	macros := pr.Machine().Macros
	if len(macros) == 0 {
		c.Text(image.Rect(30, 300, W-30, 340), "The printer has no macros", gfx.Face(false, 20), dim, gfx.Center)
		return
	}
	const rowH = 70
	bottom := contentY1 - 8
	maxScroll := max(0, len(macros)*rowH-(bottom-menuTop))
	a.macroScroll = max(0, min(a.macroScroll, maxScroll))
	c.Clipped(image.Rect(0, menuTop, W, bottom), func(c *gfx.Canvas) {
		for i, m := range macros {
			y := menuTop + i*rowH - a.macroScroll
			if y+rowH < menuTop || y > bottom {
				continue
			}
			r := image.Rect(24, y, W-24, y+rowH-10)
			plate(c, r)
			c.Text(image.Rect(r.Min.X+18, r.Min.Y, r.Max.X-18, r.Max.Y), m, gfx.Face(true, 19), text, gfx.Left)
		}
	})
	a.addHit(hit{r: image.Rect(0, menuTop, W, bottom),
		scroll: func(dy int) { a.macroScroll = max(0, min(a.macroScroll+dy, maxScroll)) },
		tapAt: func(p image.Point) {
			i := (p.Y - menuTop + a.macroScroll) / rowH
			if i < 0 || i >= len(macros) {
				return
			}
			if st.Busy() {
				a.Toast("Wait until the print ends")
				return
			}
			m := macros[i]
			a.confirm = &confirmBox{question: "Run " + gfx.Fit(gfx.Face(true, 28), m, 300) + "?", yes: "Run", onYes: func() {
				a.do(func() error { return pr.Gcode(m) })
			}}
		}})
}

// drawHistory lists the finished prints, newest first. A tap prints a file again.
func (a *App) drawHistory(c *gfx.Canvas) {
	pr := a.Printer
	heading(c, "Print history")
	a.backButton(c)
	recs := pr.History()
	if len(recs) == 0 {
		c.Text(image.Rect(30, 300, W-30, 340), "No finished prints yet", gfx.Face(false, 20), dim, gfx.Center)
		return
	}
	const rowH = 90
	bottom := contentY1 - 8
	maxScroll := max(0, len(recs)*rowH-(bottom-menuTop))
	a.historyScroll = max(0, min(a.historyScroll, maxScroll))
	c.Clipped(image.Rect(0, menuTop, W, bottom), func(c *gfx.Canvas) {
		for i, r := range recs {
			y := menuTop + i*rowH - a.historyScroll
			if y+rowH < menuTop || y > bottom {
				continue
			}
			box := image.Rect(24, y, W-24, y+rowH-10)
			plate(c, box)
			col := dim
			if r.Result != "complete" {
				col = red
			}
			c.Text(image.Rect(box.Min.X+18, box.Min.Y+8, box.Max.X-18, box.Min.Y+38), gfx.Fit(gfx.Face(true, 19), r.File, box.Dx()-36), gfx.Face(true, 19), text, gfx.Left)
			c.Text(image.Rect(box.Min.X+18, box.Min.Y+38, box.Max.X-18, box.Max.Y-8), r.Summary(), gfx.Face(false, 16), col, gfx.Left)
			c.Text(image.Rect(box.Min.X+18, box.Min.Y+38, box.Max.X-18, box.Max.Y-8), fileDate(r.End, a.Now()), gfx.Face(false, 14), dim, gfx.Right)
		}
	})
	a.addHit(hit{r: image.Rect(0, menuTop, W, bottom),
		scroll: func(dy int) { a.historyScroll = max(0, min(a.historyScroll+dy, maxScroll)) },
		tapAt: func(p image.Point) {
			i := (p.Y - menuTop + a.historyScroll) / rowH
			if i >= 0 && i < len(recs) && !pr.State().Busy() {
				a.confirmPrint(recs[i].Path)
			}
		}})
}
