package main

import (
	"context"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/ui"
	"printdeck/internal/wifi"
)

// demoNow is the fixed time of the pictures, so that two runs make the same pictures.
func demoNow() time.Time { return time.Date(2026, 10, 20, 14, 24, 0, 0, time.Local) }

// demoApp is a deck with fixed data: a running print, a short history, and the files.
func demoApp() (*ui.App, *ui.DemoPrinter) {
	p := &ui.DemoPrinter{View: -1, State_: printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "printdeck_body.gcode", Activity: "Printing", Layer: 12, Layers: 27, Fan: 100, HasFan: true, Z: 3.6, HasZ: true, HasNow: true, NowKind: "Outer wall", NowSpeed: 30, Completion: 42.4, TimeLeft: 83 * time.Minute, HasTimeLeft: true,
		Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60}}
	if f, err := os.Open(os.Getenv("DEMO_PREVIEW")); err == nil {
		p.Img, _ = png.Decode(f)
		f.Close()
	}
	a := ui.New()
	a.Theme = "day"
	a.Printer = p
	a.PreheatTool, a.PreheatBed = 200, 60
	a.Material = "PLA"
	// The setup host and the updater of the deck, as on a Pi.
	a.Setup = &ui.DemoSetup{
		Nets: []wifi.Network{{SSID: "Extremely", Signal: -48, Secure: true}, {SSID: "Workshop", Signal: -63, Secure: true},
			{SSID: "Guest", Signal: -71}, {SSID: "NETGEAR-5G", Signal: -82, Secure: true}},
		Status_: wifi.Status{State: "COMPLETED", SSID: "Extremely", IP: "192.168.76.119"},
	}
	a.Updater = &ui.DemoUpdater{S: ui.UpdateStatus{Current: "v0.3.0", Latest: "v0.4.0", Newer: true, Checked: true,
		Notes: "The printer display shows the menus of the deck, with the knob. New status screen with icons. Signed updates."}}
	// What the test printer has, a short history, and the slicer data of the files.
	p.Machine_ = printer.Machine{KlipperVersion: "v0.13.0-777-g7bc4d0946-printdeck-go",
		Modules: []string{"probe", "bed_tilt", "extruder", "heater_bed"},
		Macros:  []string{"NOZZLE_WIPE", "PRINT_START", "PRINT_END", "G29", "G12"}, FilamentDiameter: 2.85}
	end := time.Date(2026, 10, 20, 11, 42, 0, 0, time.Local)
	// The timings of a TAZ Workhorse: the bed heats at about 6 s for each degree, the nozzle at
	// about 0.7 s, and the probe takes about a minute and a half.
	phases := func(bed, nozzle, probe float64) []printer.Phase {
		return []printer.Phase{{Kind: "heat-bed", Seconds: bed, From: 23, To: 60},
			{Kind: "heat-nozzle", Seconds: nozzle, From: 24, To: 210}, {Kind: "probe", Seconds: probe}}
	}
	p.History_ = []printer.Record{
		{File: "benchy.gcode", Path: "benchy.gcode", Result: "complete", Duration: 102 * time.Minute, Filament: 4100, End: end,
			Phases: phases(222, 128, 86), CoolBed: 640, Planned: 5560},
		{File: "cable_clip_x8.gcode", Path: "cable_clip_x8.gcode", Result: "cancelled", Duration: 12 * time.Minute, Filament: 600, End: end.AddDate(0, 0, -1)},
		{File: "rail_clip.gcode", Path: "rail_clip.gcode", Result: "complete", Duration: 65 * time.Minute, Filament: 6300, End: end.AddDate(0, 0, -3),
			Phases: phases(230, 131, 90), CoolBed: 610, Planned: 3420},
	}
	// The rolls of filament: the black roll is in the printer.
	p.Spools_ = &printer.Spools{}
	p.Spools_.Add("Galaxy Black", "PLA", 1180, 180, 22, end.AddDate(0, 0, -30))
	p.Spools_.Add("Signal Orange", "PLA", 690, 180, 22, end.AddDate(0, 0, -12))
	black := p.Spools_.List()[1]
	p.Spools_.Put(black.ID)
	p.Spools_.Use(func(string) float64 { return 358 })
	p.History_[0].Spool, p.History_[0].Cost, p.History_[0].Material = "Galaxy Black", 0.71, "PLA"
	p.Meta_ = gcodemeta.Meta{Slicer: "Cura 5.13.0", PrintTime: 102 * time.Minute, FilamentMM: 4100, LayerHeight: 0.2,
		NozzleTemp: 210, BedTemp: 60}
	return a, p
}

// demo writes PNG pictures of the screens to DIR.
func demo(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	os.MkdirAll(dir, 0o755)
	demoShots(dir, false)
	demoShots(dir, true)
	c := gfx.New(ui.BaseW, ui.BaseH)
	ui.DrawSplash(c, "v0.4.0", true)
	f, _ := os.Create(filepath.Join(dir, "splash.png"))
	png.Encode(f, c.RGBA)
	f.Close()
}

// demoShots writes the pictures of the screens, tall or wide. The names of the wide pictures end
// in "-wide".
func demoShots(dir string, wide bool) {
	a, p := demoApp()
	a.Now = demoNow
	a.Wide = wide
	suffix := ""
	if wide {
		suffix = "-wide"
	}
	snap := func(name string) {
		c := gfx.New(a.Screen().W, a.Screen().H)
		a.Draw(c)
		f, _ := os.Create(filepath.Join(dir, name+suffix+".png"))
		png.Encode(f, c.RGBA)
		f.Close()
	}
	snap("print-busy")
	a.Theme = "night"
	snap("night-print-busy")
	a.OpenPrintScreen("control")
	snap("night-control")
	a.OpenPrintScreen("keypad")
	snap("night-keypad")
	a.Theme = "day"
	a.OpenPrintScreen("")
	p.View = 9
	snap("print-scrub")
	p.View = -1
	p.State_.Activity, p.State_.Layer, p.State_.Completion, p.State_.Tool, p.State_.HasTimeLeft = "Heating nozzle  172/220°", 0, 0, 172, false
	snap("print-heating")
	p.State_.Activity, p.State_.Tool, p.State_.ToolTarget = "Wiping nozzle", 160, 160
	snap("print-wipe")
	a.OpenPrintScreen("tune")
	snap("print-tune")
	// The states of a print: preparing, the first layer, paused, done.
	p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
		Activity: "Heating bed  52/60°", Preparing: true, HasWipe: true, Tool: 24, Bed: 52, BedTarget: 60}
	a.OpenPrintScreen("")
	snap("print-preparing")
	// The end macro: the bed cools to 45° before the print moves to the front.
	p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
		Activity: "Cooling the bed  52°", Finishing: true, Layer: 27, Layers: 27, Completion: 100, Tool: 120, Bed: 52, BedTarget: 45, PrepN: 1}
	p.State_.Prep[0] = printer.PrepStep{Kind: "cool-bed", Name: "Cool the bed", Now: true}
	snap("print-finishing")
	// A pause of the file with a message and a height check: the part goes in its pocket.
	p.State_ = printer.State{Reachable: true, Text: "Paused", Connected: true, Printing: true, Paused: true, File: "pod_panel_logo.gcode",
		Layer: 61, Layers: 64, Completion: 82, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60,
		PausePrompt: "Put the panel in the pocket on the bed, top up.", PauseCheck: true}
	snap("print-pause-prompt")
	p.State_.PauseChecked, p.State_.PauseDelta = true, 0.12
	snap("print-pause-checked")
	p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
		Activity: "Printing", Layer: 1, Layers: 27, Completion: 2, HasTimeLeft: true, TimeLeft: 100 * time.Minute,
		ZOffset: 0.04, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}
	snap("print-first-layer")
	p.State_.Layer, p.State_.Paused, p.State_.Completion, p.State_.Message = 12, true, 42, "Paused by user"
	snap("print-paused")
	p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 80, Bed: 48,
		Finished: "complete", FinishedFile: "benchy.gcode", FinishedPath: "benchy.gcode", FinishedTime: 102 * time.Minute,
		FinishedFilament: 4100}
	snap("print-done")
	p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, ManualProbe: true, ManualZ: 0.425}
	snap("print-manual-probe")
	p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23, Homed: "xyz",
		X: 110, Y: 110, Z: 5, Axis: [2][3]float64{{0, 0, 0}, {280, 280, 270}}}
	a.OpenPrintScreen("")
	snap("print-idle")
	for _, n := range []string{"move", "files", "filament", "maintenance", "machine", "macros", "history", "record", "rolls", "roll", "new-roll", "stats", "swap", "swap-heat", "keypad"} {
		a.OpenPrintScreen(n)
		snap("print-" + n)
	}
	a.OpenPrintScreen("file")
	snap("print-file")
	p.State_.FirmwareOld = true
	a.OpenPrintScreen("")
	snap("print-firmware-old")
	// The printer setup, with the board of the test printer, and an error of Klipper.
	p.State_ = printer.State{Err: "Klipper is not ready", NeedsSetup: true}
	p.Boards_ = []board.Info{{Name: "UltiMachine ultimachine.com RAMBo", Klipper: true, MCU: "atmega2560",
		Version: "v0.13.0-777-g7bc4d0946-prind-20260930_185514-10f1cdde21e6"}}
	snap("print-setup")
	p.Boards_ = []board.Info{{Name: "1a86 USB Serial"}}
	snap("print-setup-stock")
	p.State_ = printer.State{Err: "Error in printer.cfg", ErrDetail: "Option 'rotation_distance' in section 'stepper_x' must be specified\n\nOnce the underlying issue is corrected, use the\n\"RESTART\" command to reload the config and\nrestart the host software.\nPrinter is halted"}
	snap("print-error")
	p.State_ = printer.State{Err: "Printer is not ready", Starting: true, ErrDetail: "Printer is not ready\nThe klippy host software is attempting to connect."}
	snap("print-starting")
	for _, n := range []string{"wifi", "password", "done", "help"} {
		a.OpenSetupScreen(n)
		snap("setup-" + n)
	}
	// The pages of the Machine page, the printer configs, and last the screens that take over: the
	// firmware flash and an action.
	a.OpenSetupScreen("")
	p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23, Homed: "xyz"}
	p.Console_ = []printer.ConsoleLine{{Kind: "in", Text: "G28"}, {Kind: "in", Text: "M105"},
		{Kind: "out", Text: "ok T:24.1 /0.0 B:23.4 /0.0"}, {Kind: "in", Text: "G1 X400"},
		{Kind: "error", Text: "Move out of range: 400.000 150.000 10.000 [0.000]"}}
	for _, n := range []string{"update", "console", "configs", "action", "flash"} {
		a.OpenPrintScreen(n)
		snap("print-" + n)
	}
}

// remoteDemo serves the UI of a demo deck in the browser on ADDR (default :8080), to try it with no
// Pi. It needs no login.
func remoteDemo(args []string) {
	addr := ":8080"
	if len(args) > 0 {
		addr = args[0]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, _ := demoApp()
	rs := remote.New()
	rs.Password = func() string { return "demo" }
	rs.NewSession = uiSession(a)
	rs.Icon = ui.Icon()
	// The demo needs no login, to look at it in a browser at once.
	rs.NoLogin = true
	srv := &http.Server{Addr: addr, Handler: rs.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("demo on %s", addr)
		log.Fatal(srv.ListenAndServe())
	}()
	<-ctx.Done()
}
