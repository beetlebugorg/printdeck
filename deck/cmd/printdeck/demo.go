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
	"printdeck/internal/eq"
	"printdeck/internal/gcodestore"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
	"printdeck/internal/webui"
	"printdeck/internal/wifi"
)

// demoApp is a deck with fixed data: a playing song, a running print, and an equalizer.
func demoApp() (*ui.App, *ui.DemoMusic, *ui.DemoPrinter) {
	m := &ui.DemoMusic{State_: spotify.State{Authorized: true, Active: true, Mine: true, Playing: true, Shuffle: true, Repeat: "context",
		Title: "Everything In Its Right Place", Artist: "Radiohead", URI: "spotify:track:demo", Liked: true, Duration: 251 * time.Second}}
	p := &ui.DemoPrinter{View: -1, State_: printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "printdeck_body.gcode", Activity: "Printing", Layer: 12, Layers: 27, Fan: 100, HasFan: true, Z: 3.6, HasZ: true, HasNow: true, NowKind: "Outer wall", NowSpeed: 30, Completion: 42.4, TimeLeft: 83 * time.Minute, HasTimeLeft: true,
		Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60}}
	if f, err := os.Open(os.Getenv("DEMO_PREVIEW")); err == nil {
		p.Img, _ = png.Decode(f)
		f.Close()
	}
	a := ui.New()
	a.Theme = "day"
	a.Music, a.Printer, a.Vol = m, p, ui.DemoVolume(35)
	a.Equalizer = &ui.DemoEQ{G: eq.Gains{4, 1.5, 0, -2, 3}}
	a.PreheatTool, a.PreheatBed = 200, 60
	return a, m, p
}

// demo writes PNG pictures of the screens to DIR.
func demo(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	os.MkdirAll(dir, 0o755)
	a, m, p := demoApp()
	snap := func(name string) {
		c := gfx.New(ui.W, ui.H)
		a.Draw(c)
		f, _ := os.Create(filepath.Join(dir, name+".png"))
		png.Encode(f, c.RGBA)
		f.Close()
	}
	snap("music")
	a.Theme = "night"
	snap("night-music")
	a.OpenEQ()
	snap("night-eq")
	a.Theme = "day"
	a.SetTab(ui.TabMusic)
	a.OpenEQ()
	snap("eq")
	a.SetTab(ui.TabMusic)
	a.OpenPicker(false)
	snap("picker-add")
	a.OpenTracks(200)
	snap("tracks")
	a.OpenPicker(true)
	snap("picker")
	a.SetTab(ui.TabMusic)
	a.SetTab(ui.TabPrint)
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
	p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23}
	a.OpenPrintScreen("")
	snap("print-idle")
	for _, n := range []string{"control", "swap", "swap-heat", "keypad"} {
		a.OpenPrintScreen(n)
		snap("print-" + n)
	}
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
	a.OpenPrintScreen("")
	m.State_ = spotify.State{Authorized: true}
	a.SetTab(ui.TabMusic)
	snap("music-idle")
	m.State_ = spotify.State{}
	snap("link")
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
		c := gfx.New(ui.W, ui.H)
		ui.DrawSplash(c, "v2.1.0", theme == "night")
		f, _ := os.Create(filepath.Join(dir, "splash-"+theme+".png"))
		png.Encode(f, c.RGBA)
		f.Close()
	}
}

// remoteDemo serves the remote screen (/screen) and the page for phones (/) of a demo deck on ADDR
// (default :8080), to try them in a browser with no Pi. The password is "demo".
func remoteDemo(args []string) {
	addr := ":8080"
	if len(args) > 0 {
		addr = args[0]
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, m, p := demoApp()
	events := make(chan touch.Event, 64)
	rs := remote.New()
	rs.Password = func() string { return "demo" }
	rs.Touch = func(e touch.Event) { events <- e }
	mux := http.NewServeMux()
	mux.Handle("/", rs.Handler())
	// The page for phones. In the demo it needs no login, to look at it in a browser at once.
	page := &webui.Server{Printer: p, Music: m, Volume: a.Vol, PreheatTool: 200, PreheatBed: 60,
		Store: &gcodestore.Store{Dir: os.TempDir()}, Auth: func(h http.HandlerFunc) http.HandlerFunc { return h }}
	page.Register(mux)
	go func() {
		log.Printf("demo on %s", addr)
		log.Fatal((&http.Server{Addr: addr, Handler: remote.SecurityHeaders(mux), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
	}()
	a.Run(ctx, events, rs.Publish)
}
