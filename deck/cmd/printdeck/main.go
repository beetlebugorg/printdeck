// Command printdeck is the Printdeck appliance: the full screen application on the touch
// display, the remote screen, the API for slicers, and (as "printdeck klippy", in its own process)
// the Klipper host.
//
//	printdeck           run the application (started by systemd)
//	printdeck app       run the desktop app: the deck for a printer on USB, with no screen
//	printdeck demo DIR  write PNG pictures of the screens to DIR
//	printdeck remote-demo [ADDR]  serve the remote screen of a demo deck (password "demo")
//	printdeck preview FILE.png  write the model view of the print that runs now
//	printdeck shot FILE.png  write a picture of the screen, with live data
//	printdeck splash    show the start screen at boot (started by printdeck-init)
//	printdeck password  print a new random password of four words
//	printdeck board     find the printer board, and print its MCU and firmware version
//	printdeck manual DIR  write the manual (DIR/index.html and its pictures)
//	printdeck snapshot URL PASSWORD OUT.png [h=H] [printer=ID] [tap=X,Y ...]  a frame of the UI of a Printdeck, as a PNG
//	printdeck helper    the system helper: the actions of the deck that need root (started by systemd)
//	printdeck klipper-probe DEVICE [BAUD]  connect to a Klipper board and print what it reports
//	printdeck klippy [options] CONFIG  run the Klipper host (the Go port of klippy.py)
//	printdeck verify FILE  check the signature and the checksums of an update file
//	printdeck host-api  print the interface version for the Klipper host and the helper
//	printdeck bench SOCKET OUT.png  run the deck UI with no screen on a Klipper host (a test bench)
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/chitu"
	"printdeck/internal/config"
	"printdeck/internal/display"
	"printdeck/internal/gfx"
	"printdeck/internal/hostapi"
	"printdeck/internal/hostmetrics"
	"printdeck/internal/klipper/cli"
	"printdeck/internal/printer"
	"printdeck/internal/signing"
	"printdeck/internal/system"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

func main() {
	if systemMain(programName(), os.Args[1:]) {
		return
	}
	// The Klipper host runs in its own process, as klippy does, before the deck settings load.
	if len(os.Args) > 1 && os.Args[1] == "klippy" {
		if dir := os.Getenv(simCardEnv); dir != "" {
			simCard(dir)
		}
		// A TAZ Workhorse config of an earlier version gets the SD card section before the start.
		if path := cli.ConfigFile(os.Args[2:]); path != "" {
			if changed, err := printer.AddSDCard(path); err != nil {
				fmt.Fprintf(os.Stderr, "printdeck klippy: SD card section: %v\n", err)
			} else if changed {
				fmt.Fprintf(os.Stderr, "printdeck klippy: added [printdeck_sdcard] to %s\n", path)
			}
		}
		os.Exit(cli.Main(os.Args[2:]))
	}
	// After "Restart the screen now", the deck program of an installed update runs in place of
	// this one (handoff.go).
	if len(os.Args) == 1 {
		runNextDeck()
	}
	cfg := config.Load()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "app":
			desktopApp(os.Args[2:])
			return
		case "shot":
			shot(cfg, os.Args[2:])
			return
		case "preview":
			preview(cfg, os.Args[2:])
			return
		case "demo":
			demo(os.Args[2:])
			return
		case "remote-demo":
			remoteDemo(os.Args[2:])
			return
		case "splash":
			splash(cfg)
			return
		case "klipper-probe":
			klipperProbe(os.Args[2:])
			return
		case "board":
			boardList()
			return
		case "verify":
			if len(os.Args) != 3 {
				log.Fatal("usage: printdeck verify printdeck-update.tar.xz")
			}
			if err := system.VerifyBundle(os.Args[2]); err != nil {
				log.Fatal(err)
			}
			fmt.Println("valid: a Printdeck release with a valid signature")
			return
		case "bench":
			bench(os.Args[2:])
			return
		case "flash":
			boardFlash(os.Args[2:])
			return
		case "manual":
			manual(os.Args[2:])
			return
		case "snapshot":
			snapshot(os.Args[2:])
			return
		case "helper":
			system.SetDataDirs(cfg.StateDir, filepath.Dir(cfg.KlipperConfig), cfg.GcodeDir)
			system.KlippySocket = cfg.KlippySocket
			if err := system.Helper(); err != nil {
				log.Fatal(err)
			}
			return
		case "host-api":
			// The interface version for the deck program of another slot (package hostapi).
			fmt.Println(hostapi.Version)
			return
		case "password":
			p, err := system.NewPassword()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(p)
			return
		default:
			log.Fatalf("unknown command %q", os.Args[1])
		}
	}
	run(cfg)
}

// settingsRotate sets the turn of the screen from the Settings page in cfg: it wins over
// display_rotate of printdeck.txt. The start screen and the deck both use it, so the start screen
// has the shape of the deck. It returns the file of the setting.
func settingsRotate(cfg *config.Config) string {
	file := filepath.Join(cfg.StateDir, "rotate")
	if b, err := os.ReadFile(file); err == nil {
		if r, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && r%90 == 0 && r >= 0 && r < 360 {
			cfg.Rotate = r
		}
	}
	return file
}

func run(cfg config.Config) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rotateFile := settingsRotate(&cfg)

	screen := openDisplay(cfg)
	defer screen.Close()
	// The default turn depends on the shape of the panel. The Settings page and the touch use the
	// turn of the panel.
	if p, ok := screen.(interface{ Rotation() int }); ok {
		cfg.Rotate = p.Rotation()
	}

	// The printers of the deck (printers.go): each one has its backend and its App. The first
	// printer has the deck-wide parts: the setup, the updater, the web server.
	deckStateDir = cfg.StateDir
	printers := loadDeckPrinters(cfg.StateDir)
	var apps []*ui.App
	var backends []printerBackend
	var cfgs []config.Config
	for _, dp := range printers {
		pcfg := dp.config(cfg)
		if err := os.MkdirAll(pcfg.StateDir, 0o700); err != nil {
			log.Printf("printer %s: %v", dp.Name, err)
		}
		pr := newPrinter(pcfg, dp.ID)
		a := ui.New()
		a.Printer = pr
		a.Animate = true
		switch pr.(type) {
		case *printer.Klippy, *printer.Bambu:
			// The last print of the home screen as a 3D view that the finger turns (zslice). A
			// Bambu Lab printer has its sliced 3MF files in the same folder.
			if v := startZsliceViewer(pcfg.GcodeDir); v != nil {
				a.Viewer = v
			}
		}
		// The UI is wide when the turned panel is wider than high (DISPLAY_ROTATE 0 or 180).
		a.Wide = screen.Size().X > screen.Size().Y
		// A tall screen with more pixels than the UI (the Touch Display 2, 720 x 1280): the UI
		// keeps its width of 480 and gets the height of the shape of the screen, and each frame
		// is drawn at the size of the screen.
		if sz := screen.Size(); !a.Wide && sz.X > ui.BaseW {
			a.Height = (sz.Y*ui.BaseW + sz.X - 1) / sz.X
			a.Scale = [2]int{sz.X, ui.BaseW}
		}
		a.PreheatTool, a.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
		a.UnloadMM, a.LoadMM, a.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
		a.NightFrom, a.NightTo = cfg.NightFrom, cfg.NightTo
		// The loaded material is kept in the state folder of the printer.
		materialFile := filepath.Join(pcfg.StateDir, "material")
		if b, err := os.ReadFile(materialFile); err == nil {
			a.Material = strings.TrimSpace(string(b))
		}
		a.OnMaterial = func(m string) { os.WriteFile(materialFile, []byte(m+"\n"), 0o644) }
		apps, backends, cfgs = append(apps, a), append(backends, pr), append(cfgs, pcfg)
	}
	app, pr := apps[0], backends[0]
	printerFormat = func(id string) (string, string) {
		for i, p := range printers {
			if (id == "" && i == 0) || (id != "" && p.ID == id) {
				return backendFormat(backends[i])
			}
		}
		return "", ""
	}
	printerLive = func(id, url string) slicerLive {
		for i, p := range printers {
			if (id == "" && i == 0) || (id != "" && p.ID == id) {
				return liveOf(backends[i].State(), apps[i].Camera != nil, url)
			}
		}
		return slicerLive{}
	}
	deckPrefs(cfg.StateDir, apps)
	events := make(chan touch.Event, 64)
	// On the Printdeck OS image, the first start opens the setup screen, and the deck checks
	// for updates.
	host := newSetupHost(cfg.StateDir)
	var up *updater
	if host != nil {
		// The Screen row of the Settings page: the deck saves the turn and ends; systemd starts it
		// again in the new turn. Only the Printdeck OS restarts the deck.
		for _, a := range apps {
			a := a
			a.Rotate = cfg.Rotate
			a.OnRotate = func(r int) {
				if err := os.WriteFile(rotateFile, []byte(fmt.Sprintf("%d\n", r)), 0o644); err != nil {
					a.Toast("The turn was not saved: " + err.Error())
					return
				}
				stop()
			}
		}
		// The updater restarts into an update only when no printer of the deck prints.
		up = newUpdater(cfg.StateDir, helperSystem{socket: cfg.KlippySocket}, deckState{pr, backends}, app.Redraw)
		up.lastInput, up.exit, up.handoff = app.LastInput, stop, deckHandoff(app)
		up.saving = anyTimelapseSaving
		go up.run(ctx)
		go up.watch(ctx)
		for _, a := range apps {
			a.Updater = up
			a.Setup = host
		}
		host.restart = stop // systemd starts the deck again
		// Settings, Printers: a change starts to work after a restart of the deck.
		admin := &printerAdmin{base: cfg, running: printers, restart: stop, helper: system.AskHelper}
		for _, a := range apps {
			a.Printers = admin
		}
		if host.Needed() {
			app.OpenSetup()
		}
	}

	// The samples of the CPU, the memory and the disk writes, in memory only: one for the deck.
	metrics := hostmetrics.New()
	go metrics.Run(ctx)
	// The cloud link of the deck, for each printer.
	link := newCloudLink(cfg.StateDir, func() {
		for _, a := range apps {
			a.Redraw()
		}
	})
	// "Signed prints only" and the trusted slicers (Settings > Signed prints), for the API.
	sign := &signing.Store{Path: filepath.Join(cfg.StateDir, "signing.json")}
	camMap := cameraPrinters(cfg.StateDir)
	for i, a := range apps {
		pr, pcfg := backends[i], cfgs[i]
		a.Metrics = metrics
		a.Signing = sign
		startQueue(ctx, pcfg.StateDir, pr, a)
		go pr.Run(ctx, a.Redraw)
		go lcdLoop(ctx, pr, a)
		label := ""
		if len(printers) > 1 {
			label = printers[i].Label
		}
		startNotify(ctx, cfg.StateDir, pr, a, label)
		if i == 0 {
			// The ChiTu relay has one port: the first printer has it.
			startChiTuRelay(ctx, pcfg.StateDir, pr, chitu.Port)
		}
		// Each printer has a hub with its cameras (camera-printers.json), and its time lapse.
		var keep func(camera.Device) bool
		if len(printers) > 1 {
			keep = keepCameras(camMap, printers[i].ID)
			id := printers[i].ID
			a.OnCameraPrinter = func(cam, to string) error {
				if to == "first" {
					to = ""
				}
				if to == id {
					return nil
				}
				return setCameraPrinter(cfg.StateDir, cam, to)
			}
		}
		startCamera(ctx, pcfg.StateDir, a, pr, keep)
		startTimelapse(ctx, pcfg.StateDir, pcfg.KlipperConfig, a, pr)
		startBedCool(ctx, pcfg.StateDir, a, pr)
		name := ""
		if len(printers) > 1 {
			name = printers[i].Name
		}
		startShare(link, pcfg.StateDir, pcfg.KlipperConfig, pcfg.GcodeDir, a, pr, name, printers[i].ID)
	}
	startTLSCert(ctx, cfg.StateDir, cfg.TLSAddr, link)
	if len(apps) > 1 {
		ids := deckIdentities(printers, backends)
		deckPrinterList = func() []ui.Identity { return ids }
		deckApps = func(id string) *ui.App {
			for i, x := range ids {
				if x.ID == id {
					return apps[i]
				}
			}
			return nil
		}
	}

	if p, ok := screen.(interface{ SetTouch(touch.Options) }); ok {
		p.SetTouch(touch.Options{InvertX: cfg.TouchInvertX, InvertY: cfg.TouchInvertY, Swap: cfg.TouchSwapXY,
			Rotate: cfg.Rotate, Log: cfg.TouchLog,
			OffsetX: float64(cfg.TouchOffsetX) / float64(app.Screen().W),
			OffsetY: float64(cfg.TouchOffsetY) / float64(app.Screen().H)})
	}
	go screen.Run(ctx, events)

	var others []webPrinter
	for i := 1; i < len(apps); i++ {
		others = append(others, webPrinter{id: printers[i].ID, cfg: cfgs[i], pr: backends[i], app: apps[i]})
	}
	startWeb(cfg, host, pr, app, up, others)
	// After a boot, a soft change from the start screen to the first screen.
	if _, ok := screen.(display.None); !ok && uptime() < 90*time.Second {
		fadeFromStart(screen, app, events)
	}
	if len(apps) == 1 {
		app.Run(ctx, events, screen.Present)
		return
	}
	// A deck with many printers: the touch screen shows one printer at a time, and after a restart
	// the printer that it showed last.
	sw := ui.NewSwitcher(apps, deckIdentities(printers, backends))
	activeFile := filepath.Join(cfg.StateDir, "active-printer")
	if b, err := os.ReadFile(activeFile); err == nil {
		sw.Restore(strings.TrimSpace(string(b)))
	}
	ask := sw.Ask
	deckAsk.Store(&ask)
	sw.OnSwitch = func(id string) {
		if err := os.WriteFile(activeFile, []byte(id+"\n"), 0o644); err != nil {
			log.Printf("printers: the printer on the screen was not saved: %v", err)
		}
	}
	sw.Run(ctx, events, screen.Present)
}

// deckPrefs reads the settings of the screen of the deck (the theme, the clock, the text size,
// the homing in a filament change) from its state folder, and gives them to each App. A change on
// one App changes all of them.
func deckPrefs(stateDir string, apps []*ui.App) {
	read := func(name string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(stateDir, name))
		return strings.TrimSpace(string(b)), err == nil
	}
	write := func(name, v string) { os.WriteFile(filepath.Join(stateDir, name), []byte(v+"\n"), 0o644) }
	theme, _ := read("theme")
	clock, _ := read("clock")
	textSize, _ := read("text-size") // written only when the Settings page changes it
	swapHome, hasSwap := read("swap-home")
	for _, a := range apps {
		if theme != "" {
			a.Theme = theme
		}
		a.Clock24 = clock == "24"
		a.TextSize = textSize
		if hasSwap {
			a.SwapHome = swapHome != "off" // on when the file is not there
		}
		a.OnTheme = func(t string) {
			write("theme", t)
			for _, b := range apps {
				b.Theme = t
			}
		}
		a.OnClock = func(h24 bool) {
			v := "12"
			if h24 {
				v = "24"
			}
			write("clock", v)
			for _, b := range apps {
				b.Clock24 = h24
			}
		}
		a.OnTextSize = func(s string) {
			write("text-size", s)
			for _, b := range apps {
				b.TextSize = s
			}
		}
		a.OnSwapHome = func(on bool) {
			v := "off"
			if on {
				v = "on"
			}
			write("swap-home", v)
			for _, b := range apps {
				b.SwapHome = on
			}
		}
	}
}

// preview writes the model view of the running print, to check the G-code and STL handling.
func preview(cfg config.Config, args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck preview FILE.png")
	}
	pr := newPrinter(cfg, "")
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go pr.Run(ctx, func() {})
	for i := 0; i < 120; i++ {
		time.Sleep(500 * time.Millisecond)
		if img := pr.Preview(); img != nil {
			f, _ := os.Create(args[0])
			png.Encode(f, img)
			f.Close()
			fmt.Println("wrote", args[0])
			return
		}
	}
	log.Fatalf("no preview. Printer state: %+v", pr.State())
}

// shot runs the application for a few seconds with live data, without the display, and writes a tab as PNG.
func shot(cfg config.Config, args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck shot FILE.png")
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	pr := newPrinter(cfg, "")
	app := ui.New()
	app.Printer = pr
	app.NightFrom, app.NightTo = cfg.NightFrom, cfg.NightTo
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	if b, err := os.ReadFile(filepath.Join(cfg.StateDir, "theme")); err == nil {
		app.Theme = strings.TrimSpace(string(b))
	}
	go pr.Run(ctx, func() {})
	time.Sleep(8 * time.Second) // the first reads, and the G-code for the model view
	c := gfx.New(app.Screen().W, app.Screen().H)
	app.Draw(c)
	f, err := os.Create(args[0])
	if err != nil {
		log.Fatal(err)
	}
	png.Encode(f, c.RGBA)
	f.Close()
}

// lcdLoop shows the screen of the deck on the display of the printer: at each change, and every
// 5 seconds, because a screen of printdeck_display lasts 15 seconds.
func lcdLoop(ctx context.Context, pr printerBackend, app *ui.App) {
	k, ok := pr.(*printer.Klippy)
	if !ok {
		return
	}
	k.SetOnKey(app.LCDKey)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var last []byte
	var sent time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if !pr.State().Reachable {
			last = nil
			continue
		}
		s := app.LCD()
		s.Clock24 = app.Clock24
		b, _ := json.Marshal(s)
		if bytes.Equal(b, last) && time.Since(sent) < 5*time.Second {
			continue
		}
		if err := k.ShowLCD(&s); err == nil {
			last, sent = b, time.Now()
		}
	}
}
