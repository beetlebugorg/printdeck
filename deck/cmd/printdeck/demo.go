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
	// What the test printer has, a short history, and the slicer data of the files.
	p.Machine_ = printer.Machine{KlipperVersion: "v0.13.0-777-g7bc4d0946-printdeck-go",
		Modules: []string{"probe", "bed_tilt", "extruder", "heater_bed"},
		Macros:  []string{"NOZZLE_WIPE", "PRINT_START", "PRINT_END", "G29", "G12"}}
	end := time.Date(2026, 10, 20, 11, 42, 0, 0, time.Local)
	p.History_ = []printer.Record{
		{File: "benchy.gcode", Path: "benchy.gcode", Result: "complete", Duration: 102 * time.Minute, Filament: 4100, End: end},
		{File: "cable_clip_x8.gcode", Path: "cable_clip_x8.gcode", Result: "cancelled", Duration: 12 * time.Minute, Filament: 600, End: end.AddDate(0, 0, -1)},
		{File: "amp_bracket.gcode", Path: "amp_bracket.gcode", Result: "complete", Duration: 65 * time.Minute, Filament: 6300, End: end.AddDate(0, 0, -3)},
	}
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
	a, p := demoApp()
	snap := func(name string) {
		c := gfx.New(ui.W, ui.BaseH)
		a.Draw(c)
		f, _ := os.Create(filepath.Join(dir, name+".png"))
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
	for _, n := range []string{"move", "files", "filament", "maintenance", "machine", "macros", "history", "stats", "swap", "swap-heat", "keypad"} {
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
	a.Setup = &ui.DemoSetup{
		Nets: []wifi.Network{{SSID: "Extremely", Signal: -48, Secure: true}, {SSID: "Workshop", Signal: -63, Secure: true},
			{SSID: "Guest", Signal: -71}, {SSID: "NETGEAR-5G", Signal: -82, Secure: true}},
		Status_: wifi.Status{State: "COMPLETED", SSID: "Extremely", IP: "192.168.76.119"},
	}
	for _, n := range []string{"wifi", "password", "done", "help"} {
		a.OpenSetupScreen(n)
		snap("setup-" + n)
	}
	for _, theme := range []string{"day", "night"} {
		c := gfx.New(ui.W, ui.BaseH)
		ui.DrawSplash(c, "v2.1.0", theme == "night")
		f, _ := os.Create(filepath.Join(dir, "splash-"+theme+".png"))
		png.Encode(f, c.RGBA)
		f.Close()
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
