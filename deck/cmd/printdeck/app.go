package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"printdeck/internal/awake"
	"printdeck/internal/config"
	"printdeck/internal/display"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

// appAddr is the address of the web server of the desktop app. The port stays the same, so Cura
// finds the app again after a restart.
const appAddr = "127.0.0.1:7351"

// The first line of the app on its output: the address of the web server and the token of the
// window. The window of the app starts printdeck app, and reads this line from the pipe.
type appHello struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// desktopApp runs the deck on a computer, for a printer on USB: the Klipper host, the deck UI
// with no screen, and the web server on this computer only. The window of the app (desktop/)
// shows the UI with the remote screen. It sends the token of the first line in the X-Api-Key
// header.
//
//	printdeck app [-dir DIR] [-addr ADDR] [-parent] [-sim]
func desktopApp(args []string) {
	fs := flag.NewFlagSet("app", flag.ExitOnError)
	dir := fs.String("dir", "", "the folder of the app (default: the configuration folder of the user)")
	addr := fs.String("addr", appAddr, "the address of the web server")
	parent := fs.Bool("parent", false, "end when the input closes (the window of the app started the program)")
	sim := fs.Bool("sim", false, "a simulated printer: the TAZ Workhorse on the simulated board, in its own folder")
	fs.Parse(args)
	if *dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		name := "Printdeck"
		if *sim {
			name = "Printdeck Sim"
		}
		*dir = filepath.Join(base, name)
	}
	cfg, err := appConfig(*dir, *sim)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *parent {
		// The window keeps the input open. It closes it at the end, and the system closes it when
		// the window stops. Both end the app.
		go func() {
			io.Copy(io.Discard, os.Stdin)
			log.Print("app: the window closed the input")
			stop()
		}()
	}

	kl := &klippyProcess{cfg: cfg}
	go kl.run(ctx)

	pr := newPrinter(cfg)
	if k, ok := pr.(*printer.Klippy); ok {
		k.RestartService = kl.restart
		k.StopService = kl.stop
	}
	app := ui.New()
	app.Printer = pr
	app.PreheatTool, app.PreheatBed = cfg.PreheatTool, cfg.PreheatBed
	app.UnloadMM, app.LoadMM, app.JogInvertY = cfg.UnloadMM, cfg.LoadMM, cfg.JogInvertY
	go pr.Run(ctx, app.Redraw)
	go lcdLoop(ctx, pr, app)
	go keepAwake(ctx, pr)

	token := randomToken()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("web server: %v", err)
	}
	mux := http.NewServeMux()
	api := newAPI(cfg, pr, app, mux)
	// No password opens the login: the window of the app has the token.
	tokenOK := func(r *http.Request) bool {
		return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Api-Key")), []byte(token)) == 1
	}
	rs := newRemote(app, api, func() string { return "" }, tokenOK)
	if k, ok := pr.(*printer.Klippy); ok && *sim {
		registerSim(mux, k, tokenOK) // for the 3D window of the simulated printer
	}
	// The pages of the rolls (the label sheet, and the page of a roll that its QR code opens): with
	// the simulator, open to a browser on this computer, so that you can try them.
	if *sim {
		open := remote.New()
		open.NoLogin = true
		registerRollPages(mux, open, pr, app)
	}
	mux.Handle("/", rs.Handler())
	srv := &http.Server{Handler: remote.SecurityHeaders(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != http.ErrServerClosed {
			log.Printf("web server: %v", err)
		}
	}()
	log.Printf("app: the folder is %s, and the web server is on %s", *dir, ln.Addr())
	json.NewEncoder(os.Stdout).Encode(appHello{URL: "http://" + ln.Addr().String(), Token: token})

	// The UI runs with no screen: the display of the printer follows it.
	screen := display.None{}
	events := make(chan touch.Event)
	app.Run(ctx, events, screen.Present)
	srv.Close()
	kl.stop()
}

// appConfig makes the folders of the app in dir, and returns the configuration of the deck for
// them. A new folder gets a printer.cfg with only the include of the Printdeck sections: the
// user adds the printer. With sim, a new folder gets the simulated printer (simprinter.cfg).
func appConfig(dir string, sim bool) (config.Config, error) {
	cfg := config.Load()
	cfg.Display = "none"
	cfg.StateDir = dir
	cfg.GcodeDir = filepath.Join(dir, "gcodes")
	cfg.ModelDir = filepath.Join(dir, "models")
	cfg.KlipperConfig = filepath.Join(dir, "klipper", "printer.cfg")
	cfg.KlippySocket = socketPath(dir)
	cfg.RemoteAddr = ""
	for _, d := range []string{cfg.GcodeDir, cfg.ModelDir, filepath.Dir(cfg.KlipperConfig), filepath.Join(dir, "logs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return cfg, err
		}
	}
	include := filepath.Join(filepath.Dir(cfg.KlipperConfig), "printdeck.cfg")
	if err := os.WriteFile(include, []byte(appKlipperCfg(cfg.GcodeDir)), 0o644); err != nil {
		return cfg, err
	}
	if _, err := os.Stat(cfg.KlipperConfig); os.IsNotExist(err) {
		body := appPrinterCfg
		if sim {
			body = simPrinterCfg + "\n[include printdeck.cfg]\n"
		}
		if err := os.WriteFile(cfg.KlipperConfig, []byte(body), 0o644); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// socketPath is the path of the API socket of the Klipper host: in dir, or in the temporary folder
// when that path is too long. The path of a Unix socket has at most 104 bytes on macOS.
func socketPath(dir string) string {
	p := filepath.Join(dir, "klippy.sock")
	if len(p) <= 100 {
		return p
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(os.TempDir(), "printdeck-"+hex.EncodeToString(sum[:4])+".sock")
}

// simPrinterCfg is the printer of "printdeck app -sim": the TAZ Workhorse on the simulated board.
//
//go:embed simprinter.cfg
var simPrinterCfg string

// appPrinterCfg is the printer.cfg of a new app folder.
const appPrinterCfg = `# Put the configuration of the printer above this line: the [mcu] section with the serial port
# of the printer, the [printer] section, the steppers, the extruder and the heater bed.
[include printdeck.cfg]
`

// appKlipperCfg is the include file of the Printdeck sections, as
// /usr/share/printdeck/klipper/printdeck.cfg on the deck, with the G-code folder of the app.
func appKlipperCfg(gcodeDir string) string {
	return fmt.Sprintf(`# The Klipper sections that Printdeck needs. The app writes this file at each start.
[virtual_sdcard]
path: %s

[pause_resume]

[display_status]

[printdeck_display]

[printdeck_pause]
`, filepath.ToSlash(gcodeDir))
}

// randomToken is a new random token of 32 bytes, in hex.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// keepAwake keeps the computer awake while the printer is busy.
func keepAwake(ctx context.Context, pr printerBackend) {
	var lock awake.Lock
	defer lock.Set(false)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		lock.Set(pr.State().Busy())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// klippyProcess runs the Klipper host (printdeck klippy) in its own process, as klipper.service
// does on the deck. It starts the host again when it ends.
type klippyProcess struct {
	cfg config.Config

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
	wake    chan struct{}
}

func (k *klippyProcess) run(ctx context.Context) {
	k.mu.Lock()
	k.wake = make(chan struct{}, 1)
	k.mu.Unlock()
	self, err := os.Executable()
	if err != nil {
		log.Printf("klippy: %v", err)
		return
	}
	logFile := filepath.Join(k.cfg.StateDir, "logs", "klippy.log")
	for ctx.Err() == nil {
		k.mu.Lock()
		stopped := k.stopped
		var cmd *exec.Cmd
		if !stopped {
			os.Remove(k.cfg.KlippySocket) // a socket of a host that ended stops the next one
			cmd = exec.CommandContext(ctx, self, "klippy", k.cfg.KlipperConfig, "-l", logFile, "-a", k.cfg.KlippySocket)
			cmd.SysProcAttr = childAttr()
			cmd.Stderr = os.Stderr // a panic of the host is not in its log file
			if err := cmd.Start(); err != nil {
				log.Printf("klippy: %v", err)
				cmd = nil
			}
			k.cmd = cmd
		}
		k.mu.Unlock()
		if cmd != nil {
			err := cmd.Wait()
			log.Printf("klippy: the host ended: %v", err)
		}
		select {
		case <-ctx.Done():
		case <-k.wake:
		case <-time.After(2 * time.Second):
		}
	}
}

// restart ends the host, and starts it again (the restart-klipper action of the deck).
func (k *klippyProcess) restart() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopped = false
	k.kill()
	return nil
}

// stop ends the host until the next restart (the stop-klipper action of the deck).
func (k *klippyProcess) stop() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopped = true
	k.kill()
	return nil
}

// kill ends the process of the host, and wakes run. Call it with mu held.
func (k *klippyProcess) kill() {
	if k.cmd != nil && k.cmd.Process != nil {
		k.cmd.Process.Kill()
	}
	select {
	case k.wake <- struct{}{}:
	default:
	}
}
