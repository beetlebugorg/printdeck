// Command speakerdeck is the Speakerdeck appliance: it runs the full screen Spotify and OctoPrint
// application on the 7" touch display and drives the Amp2 volume.
//
//	speakerdeck           run the application (started by systemd)
//	speakerdeck auth      link the Spotify account
//	speakerdeck demo DIR  write PNG pictures of the screens to DIR
//	speakerdeck preview FILE.png  write the model view of the print that runs now
//	speakerdeck shot music|print FILE.png  write a picture of a tab, with live data
package main

import (
	"bufio"
	"context"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"speakerdeck/internal/amp"
	"speakerdeck/internal/config"
	"speakerdeck/internal/connect"
	"speakerdeck/internal/eq"
	"speakerdeck/internal/fb"
	"speakerdeck/internal/gfx"
	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
	"speakerdeck/internal/touch"
	"speakerdeck/internal/ui"
)

func main() {
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

	op := octoprint.New(cfg.OctoPrintURL, cfg.OctoPrintKey)
	op.ModelDir = cfg.ModelDir
	am := amp.New(cfg.AlsaCard, cfg.MaxVolume, cfg.FloorDB, cfg.CeilDB)
	conn := connect.New("127.0.0.1:3678")
	am.OnSet = conn.SetVolume

	app := ui.New()
	app.Music, app.Printer, app.Vol = sp, op, am
	eqc := eq.New("127.0.0.1:1234", cfg.StateDir)
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

	// The Inky wHAT shows the same track as the touch screen.
	if cfg.NowPlayingPort > 0 {
		sp.OnTrack = func(s spotify.State) { go forwardTrack(cfg.NowPlayingPort, s) }
	}

	go sp.Run(ctx, app.Redraw)
	go func() {
		if err := sp.ServeLink(ctx, func() { sp.Poke(); app.Redraw() }); err != nil && ctx.Err() == nil {
			log.Printf("login with a phone is off: %v", err)
		}
	}()
	go op.Run(ctx, app.Redraw)
	go am.Run(ctx, app.Redraw)
	go eqc.Run(ctx, app.Redraw)
	// The volume slider of the Spotify app moves the amp, and the amp tells Spotify its volume.
	go conn.Run(ctx, func(p int) { am.SetRemote(p) }, func() {
		if v := am.Volume(); v >= 0 {
			conn.SetVolume(v)
		}
	})

	events := make(chan touch.Event, 64)
	go touch.Run(ctx, touch.Options{InvertX: cfg.TouchInvertX, InvertY: cfg.TouchInvertY, Swap: cfg.TouchSwapXY, Rotate: cfg.Rotate}, events)

	app.Run(ctx, events, screen.Present)
}

func forwardTrack(port int, s spotify.State) {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.PostForm(fmt.Sprintf("http://127.0.0.1:%d/track", port), url.Values{
		"name": {s.Title}, "artists": {s.Artist}, "album": {s.Album}, "cover": {s.ArtURL},
	})
	if err == nil {
		resp.Body.Close()
	}
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
	fmt.Println("Spotify is linked. Restart the service: sudo systemctl restart speakerdeck")
}

func demo(args []string) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	os.MkdirAll(dir, 0o755)
	m := &ui.DemoMusic{State_: spotify.State{Authorized: true, Active: true, Mine: true, Playing: true, Shuffle: true, Repeat: "context",
		Title: "Everything In Its Right Place", Artist: "Radiohead", URI: "spotify:track:demo", Liked: true, Duration: 251 * time.Second}}
	p := &ui.DemoPrinter{View: -1, State_: octoprint.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "speakerdeck_body.gcode", Activity: "Printing", Layer: 12, Layers: 27, Fan: 100, HasFan: true, Z: 3.6, HasZ: true, HasNow: true, NowKind: "Outer wall", NowSpeed: 30, Completion: 42.4, TimeLeft: 83 * time.Minute, HasTimeLeft: true,
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
	p.State_ = octoprint.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23}
	a.OpenPrintScreen("")
	snap("print-idle")
	for _, n := range []string{"control", "swap", "swap-heat", "keypad"} {
		a.OpenPrintScreen(n)
		snap("print-" + n)
	}
	a.OpenPrintScreen("")
	m.State_ = spotify.State{Authorized: true}
	a.SetTab(ui.TabMusic)
	snap("music-idle")
	m.State_ = spotify.State{}
	snap("link")
}

// preview writes the model view of the running print, to check the G-code and STL handling.
func preview(cfg config.Config, args []string) {
	if len(args) < 1 {
		log.Fatal("usage: speakerdeck preview FILE.png")
	}
	op := octoprint.New(cfg.OctoPrintURL, cfg.OctoPrintKey)
	op.ModelDir = cfg.ModelDir
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go op.Run(ctx, func() {})
	for i := 0; i < 120; i++ {
		time.Sleep(500 * time.Millisecond)
		if img := op.Preview(); img != nil {
			f, _ := os.Create(args[0])
			png.Encode(f, img)
			f.Close()
			fmt.Println("wrote", args[0])
			return
		}
	}
	log.Fatalf("no preview. OctoPrint state: %+v", op.State())
}

// shot runs the application for a few seconds with live data, without the display, and writes a tab as PNG.
func shot(cfg config.Config, sp *spotify.Client, args []string) {
	if len(args) < 2 {
		log.Fatal("usage: speakerdeck shot music|print FILE.png")
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	op := octoprint.New(cfg.OctoPrintURL, cfg.OctoPrintKey)
	op.ModelDir = cfg.ModelDir
	am := amp.New(cfg.AlsaCard, cfg.MaxVolume, cfg.FloorDB, cfg.CeilDB)
	eqc := eq.New("127.0.0.1:1234", cfg.StateDir)
	app := ui.New()
	app.Music, app.Printer, app.Vol, app.Equalizer = sp, op, am, eqc
	app.NightFrom, app.NightTo = cfg.NightFrom, cfg.NightTo
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	if b, err := os.ReadFile(filepath.Join(cfg.StateDir, "theme")); err == nil {
		app.Theme = strings.TrimSpace(string(b))
	}
	go sp.Run(ctx, func() {})
	go op.Run(ctx, func() {})
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
