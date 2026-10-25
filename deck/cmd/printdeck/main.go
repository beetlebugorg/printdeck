// Command printdeck is the Printdeck appliance: the full screen application on the 7" touch
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
//	printdeck snapshot URL PASSWORD OUT.png [h=H] [tap=X,Y ...]  a frame of the UI of a Printdeck, as a PNG
//	printdeck helper    the system helper: the actions of the deck that need root (started by systemd)
//	printdeck klipper-probe DEVICE [BAUD]  connect to a Klipper board and print what it reports
//	printdeck klippy [options] CONFIG  run the Klipper host (the Go port of klippy.py)
//	printdeck verify FILE  check the signature and the checksums of an update file
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

	"printdeck/internal/config"
	"printdeck/internal/display"
	"printdeck/internal/gfx"
	"printdeck/internal/hostmetrics"
	"printdeck/internal/klipper/cli"
	"printdeck/internal/printer"
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
		os.Exit(cli.Main(os.Args[2:]))
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
			if err := system.Helper(); err != nil {
				log.Fatal(err)
			}
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

	pr := newPrinter(cfg)
	app := ui.New()
	app.Printer = pr
	// The UI is wide when the turned panel is wider than high (DISPLAY_ROTATE 0 or 180).
	app.Wide = screen.Size().X > screen.Size().Y
	// A tall screen with more pixels than the UI (the Touch Display 2, 720 x 1280): the UI keeps
	// its width of 480 and gets the height of the shape of the screen, and each frame is drawn
	// at the size of the screen.
	if sz := screen.Size(); !app.Wide && sz.X > ui.BaseW {
		app.Height = (sz.Y*ui.BaseW + sz.X - 1) / sz.X
		app.Scale = [2]int{sz.X, ui.BaseW}
	}
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	// The theme (day, night, auto) is kept in the state folder.
	themeFile := filepath.Join(cfg.StateDir, "theme")
	app.NightFrom, app.NightTo = cfg.NightFrom, cfg.NightTo
	if b, err := os.ReadFile(themeFile); err == nil {
		app.Theme = strings.TrimSpace(string(b))
	}
	app.OnTheme = func(t string) { os.WriteFile(themeFile, []byte(t+"\n"), 0o644) }
	// The clock (12 or 24 hours) too.
	clockFile := filepath.Join(cfg.StateDir, "clock")
	if b, err := os.ReadFile(clockFile); err == nil {
		app.Clock24 = strings.TrimSpace(string(b)) == "24"
	}
	app.OnClock = func(h24 bool) {
		v := "12"
		if h24 {
			v = "24"
		}
		os.WriteFile(clockFile, []byte(v+"\n"), 0o644)
	}
	// The text size too. The file is written only when the Settings page changes it.
	textSizeFile := filepath.Join(cfg.StateDir, "text-size")
	if b, err := os.ReadFile(textSizeFile); err == nil {
		app.TextSize = strings.TrimSpace(string(b))
	}
	app.OnTextSize = func(s string) { os.WriteFile(textSizeFile, []byte(s+"\n"), 0o644) }
	// The homing in a filament change too. It is on when the file is not there.
	swapHomeFile := filepath.Join(cfg.StateDir, "swap-home")
	if b, err := os.ReadFile(swapHomeFile); err == nil {
		app.SwapHome = strings.TrimSpace(string(b)) != "off"
	}
	app.OnSwapHome = func(on bool) {
		v := "off"
		if on {
			v = "on"
		}
		os.WriteFile(swapHomeFile, []byte(v+"\n"), 0o644)
	}
	// The loaded material is kept in the state folder too.
	materialFile := filepath.Join(cfg.StateDir, "material")
	if b, err := os.ReadFile(materialFile); err == nil {
		app.Material = strings.TrimSpace(string(b))
	}
	app.OnMaterial = func(m string) { os.WriteFile(materialFile, []byte(m+"\n"), 0o644) }
	events := make(chan touch.Event, 64)
	// On the Printdeck OS image, the first start opens the setup screen, and the deck checks
	// for updates.
	host := newSetupHost(cfg.StateDir)
	if host != nil {
		// The Screen row of the Settings page: the deck saves the turn and ends; systemd starts it
		// again in the new turn. Only the Printdeck OS restarts the deck.
		app.Rotate = cfg.Rotate
		app.OnRotate = func(r int) {
			if err := os.WriteFile(rotateFile, []byte(fmt.Sprintf("%d\n", r)), 0o644); err != nil {
				app.Toast("The turn was not saved: " + err.Error())
				return
			}
			stop()
		}
		up := newUpdater(cfg.StateDir, func() bool { return pr.State().Busy() }, app.Redraw)
		app.Updater = up
		go up.run(ctx)
		app.Setup = host
		if host.Needed() {
			app.OpenSetup()
		}
	}

	// The samples of the CPU, the memory and the disk writes, in memory only.
	app.Metrics = hostmetrics.New()
	go app.Metrics.Run(ctx)

	go pr.Run(ctx, app.Redraw)
	go lcdLoop(ctx, pr, app)
	startNotify(ctx, cfg.StateDir, pr, app)

	if p, ok := screen.(interface{ SetTouch(touch.Options) }); ok {
		p.SetTouch(touch.Options{InvertX: cfg.TouchInvertX, InvertY: cfg.TouchInvertY, Swap: cfg.TouchSwapXY,
			Rotate: cfg.Rotate, Log: cfg.TouchLog,
			OffsetX: float64(cfg.TouchOffsetX) / float64(app.Screen().W),
			OffsetY: float64(cfg.TouchOffsetY) / float64(app.Screen().H)})
	}
	go screen.Run(ctx, events)

	startWeb(cfg, host, pr, app)
	// After a boot, a soft change from the start screen to the first screen.
	if _, ok := screen.(display.None); !ok && uptime() < 90*time.Second {
		fadeFromStart(screen, app, events)
	}
	app.Run(ctx, events, screen.Present)
}

// preview writes the model view of the running print, to check the G-code and STL handling.
func preview(cfg config.Config, args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck preview FILE.png")
	}
	pr := newPrinter(cfg)
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
	pr := newPrinter(cfg)
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
