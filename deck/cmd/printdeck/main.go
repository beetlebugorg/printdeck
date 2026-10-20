// Command printdeck is the Printdeck appliance: the full screen application on the 7" touch
// display, the remote screen, the API for slicers, the Spotify Connect speaker with the equalizer,
// and (as "printdeck klippy", in its own process) the Klipper host.
//
//	printdeck           run the application (started by systemd)
//	printdeck auth      link the Spotify account
//	printdeck demo DIR  write PNG pictures of the screens to DIR
//	printdeck remote-demo [ADDR]  serve the remote screen of a demo deck (password "demo")
//	printdeck preview FILE.png  write the model view of the print that runs now
//	printdeck shot music|print FILE.png  write a picture of a tab, with live data
//	printdeck splash    show the start screen at boot (started by printdeck-init)
//	printdeck password  print a new random password of four words
//	printdeck klipper-probe DEVICE [BAUD]  connect to a Klipper board and print what it reports
//	printdeck klippy [options] CONFIG  run the Klipper host (the Go port of klippy.py)
package main

import (
	"bufio"
	"context"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"printdeck/internal/amp"
	"printdeck/internal/config"
	"printdeck/internal/connect"
	"printdeck/internal/eq"
	"printdeck/internal/fb"
	"printdeck/internal/gfx"
	"printdeck/internal/klipper/cli"
	"printdeck/internal/speaker"
	"printdeck/internal/spotify"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

func main() {
	// The Klipper host runs in its own process, as klippy does, before the deck settings load.
	if len(os.Args) > 1 && os.Args[1] == "klippy" {
		os.Exit(cli.Main(os.Args[2:]))
	}
	cfg := config.Load()
	sp := spotify.New(spotify.Config{ClientID: cfg.SpotifyClientID, DeviceName: cfg.DeviceName, StateDir: cfg.StateDir, LinkBase: cfg.LinkBase, LibrespotAddr: "127.0.0.1:3678"})

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			auth(cfg, sp)
			return
		case "shot":
			shot(cfg, sp, os.Args[2:])
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
		case "password":
			if err := password(); err != nil {
				log.Fatal(err)
			}
			return
		default:
			log.Fatalf("unknown command %q", os.Args[1])
		}
	}
	run(cfg, sp)
}

func run(cfg config.Config, sp *spotify.Client) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	screen, err := fb.Open(cfg.Framebuffer, cfg.Rotate)
	if err != nil {
		log.Fatalf("framebuffer: %v", err)
	}
	defer screen.Close()
	pw, ph := screen.PanelSize()
	log.Printf("framebuffer %s: %dx%d, rotation %d", cfg.Framebuffer, pw, ph, cfg.Rotate)

	pr := newPrinter(cfg)
	am := amp.New(cfg.AlsaCard, cfg.MaxVolume, cfg.FloorDB, cfg.CeilDB)
	conn := connect.New("127.0.0.1:3678")
	am.OnSet = conn.SetVolume
	am.File = filepath.Join(cfg.StateDir, "volume")
	volume := am.Restore()

	app := ui.New()
	app.Music, app.Printer, app.Vol = sp, pr, am
	eqc := eq.New(cfg.StateDir)
	app.Equalizer = eqc
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	// The theme (day, night, auto) is kept in the state folder.
	themeFile := filepath.Join(cfg.StateDir, "theme")
	app.NightFrom, app.NightTo = cfg.NightFrom, cfg.NightTo
	if b, err := os.ReadFile(themeFile); err == nil {
		app.Theme = strings.TrimSpace(string(b))
	}
	app.OnTheme = func(t string) { os.WriteFile(themeFile, []byte(t+"\n"), 0o644) }
	events := make(chan touch.Event, 64)
	// On the Printdeck OS image, the first start opens the setup screen.
	host := newSetupHost(cfg.StateDir)
	if host != nil {
		app.Setup = host
		if host.Needed() {
			app.OpenSetup()
		}
	}

	go sp.Run(ctx, app.Redraw)
	go func() {
		if err := sp.ServeLink(ctx, func() { sp.Poke(); app.Redraw() }); err != nil && ctx.Err() == nil {
			log.Printf("login with a phone is off: %v", err)
		}
	}()
	go pr.Run(ctx, app.Redraw)
	go am.Run(ctx, app.Redraw)
	go eqc.Run(ctx, app.Redraw)
	if cfg.Speaker {
		// The Spotify Connect speaker, with the equalizer on its samples. Its API on 127.0.0.1:3678
		// serves the spotify and connect packages.
		// It starts again after an error, for example when the network is not up yet.
		go func() {
			for ctx.Err() == nil {
				err := speaker.Run(ctx, speaker.Config{DeviceName: cfg.DeviceName, Device: cfg.SpeakerDevice,
					ConfigDir: cfg.SpotifyDir, APIAddr: "127.0.0.1", APIPort: 3678, EQ: eqc, Volume: volume,
					Debug: cfg.SpotifyDebug})
				if err != nil {
					log.Printf("spotify speaker: %v", err)
				}
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	// The volume slider of the Spotify app moves the amp, and the amp tells Spotify its volume.
	go conn.Run(ctx, func(p int) { am.SetRemote(p) }, func() {
		if v := am.Volume(); v >= 0 {
			conn.SetVolume(v)
		}
	})

	go touch.Run(ctx, touch.Options{InvertX: cfg.TouchInvertX, InvertY: cfg.TouchInvertY, Swap: cfg.TouchSwapXY,
		Rotate: cfg.Rotate, Log: cfg.TouchLog,
		OffsetX: float64(cfg.TouchOffsetX) / ui.W, OffsetY: float64(cfg.TouchOffsetY) / ui.H}, events)

	present := screen.Present
	if rs := startWeb(cfg, host, events, pr, app); rs != nil {
		present = func(c *gfx.Canvas) {
			screen.Present(c)
			rs.Publish(c)
		}
	}
	app.Run(ctx, events, present)
}

func auth(cfg config.Config, sp *spotify.Client) {
	if cfg.SpotifyClientID == "" {
		log.Fatal("Set SPOTIFY_CLIENT_ID first. Create an app at https://developer.spotify.com/dashboard\n" +
			"and add this redirect URI to it: " + spotify.RedirectURI)
	}
	link, verifier := sp.AuthURL(spotify.RedirectURI, "")
	fmt.Println("1. Open this address in a browser and allow access:\n\n" + link + "\n")
	fmt.Println("2. The browser then shows an error page for " + spotify.RedirectURI + ".")
	fmt.Println("   Copy the complete address from the address bar and paste it here:")
	fmt.Print("> ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if err := sp.Exchange(strings.TrimSpace(line), verifier, spotify.RedirectURI); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Spotify is linked. Restart the service: sudo systemctl restart printdeck")
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
func shot(cfg config.Config, sp *spotify.Client, args []string) {
	if len(args) < 2 {
		log.Fatal("usage: printdeck shot music|print FILE.png")
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	pr := newPrinter(cfg)
	am := amp.New(cfg.AlsaCard, cfg.MaxVolume, cfg.FloorDB, cfg.CeilDB)
	eqc := eq.New(cfg.StateDir)
	app := ui.New()
	app.Music, app.Printer, app.Vol, app.Equalizer = sp, pr, am, eqc
	app.NightFrom, app.NightTo = cfg.NightFrom, cfg.NightTo
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	if b, err := os.ReadFile(filepath.Join(cfg.StateDir, "theme")); err == nil {
		app.Theme = strings.TrimSpace(string(b))
	}
	go sp.Run(ctx, func() {})
	go pr.Run(ctx, func() {})
	go am.Run(ctx, func() {})
	go eqc.Run(ctx, func() {})
	time.Sleep(8 * time.Second) // the first reads, and the G-code for the model view
	if args[0] == "print" {
		app.SetTab(ui.TabPrint)
	}
	c := gfx.New(ui.W, ui.H)
	app.Draw(c)
	f, err := os.Create(args[1])
	if err != nil {
		log.Fatal(err)
	}
	png.Encode(f, c.RGBA)
	f.Close()
}
