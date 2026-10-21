package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"printdeck/internal/config"
	"printdeck/internal/fb"
	"printdeck/internal/gfx"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

// diagMarker tells printdeck-deck.service not to start, so the boot messages stay on the screen.
// It is in /dev, because systemd mounts a new /run when it starts.
const diagMarker = "/dev/.printdeck-diagnostics"

const (
	splashWatch = 10 * time.Second // how long after the start a touch can open the boot messages
	splashHold  = time.Second      // how long the finger must stay on the screen
)

// splash shows the start screen. If a finger stays on the screen at the start, it shows the boot messages
// instead. printdeck-init starts it before systemd.
func splash(cfg config.Config) {
	screen, name := openSplash(cfg)
	if screen == nil {
		log.Fatal("no framebuffer")
	}

	ctx, stop := context.WithTimeout(context.Background(), splashWatch)
	defer stop()
	events := make(chan touch.Event, 16)
	go touch.Run(ctx, touch.Options{}, events)
	// If the HDMI driver (vc4) replaces the framebuffer later, draw the start screen again on the
	// new one.
	check := time.NewTicker(200 * time.Millisecond)
	defer check.Stop()
	var hold <-chan time.Time
	for {
		select {
		case e := <-events:
			switch e.Kind {
			case touch.Down:
				hold = time.After(splashHold)
			case touch.Up:
				hold = nil
			}
		case <-hold:
			diagnostics(screen)
			return
		case <-check.C:
			if fbName(cfg) != name {
				if s, n := openSplash(cfg); s != nil {
					screen, name = s, n
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// openSplash draws the start screen on the framebuffer, and returns the screen and the name of its
// driver. It waits up to 5 seconds for the framebuffer of the HDMI driver. The framebuffer of the
// firmware ("simple") is 640x480 and the display is 800x480, so the start screen does not use it.
// The screen stays open: tty1 stays in graphics mode, so the console does not draw over the start
// screen before the deck starts.
func openSplash(cfg config.Config) (*fb.FB, string) {
	for i := 0; i < 50; i++ {
		name := fbName(cfg)
		if name == "simple" {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if screen, err := fb.Open(cfg.Framebuffer, cfg.Rotate); err == nil {
			// The start screen on the empty display of the kernel logo (ui.SplashLogo), with
			// the hint.
			c := gfx.New(screen.LogicalW, screen.LogicalH)
			ui.DrawSplash(c, osVersion(), true)
			screen.Present(c)
			return screen, name
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, ""
}

// fbName is the name of the framebuffer driver, for example "simple" from the firmware or "vc4drmfb".
func fbName(cfg config.Config) string {
	b, _ := os.ReadFile("/sys/class/graphics/" + filepath.Base(cfg.Framebuffer) + "/name")
	return strings.TrimSpace(string(b))
}

// diagnostics replaces the start screen with the kernel log, and shows the systemd status lines from now on.
func diagnostics(screen *fb.FB) {
	os.WriteFile(diagMarker, nil, 0o644)
	screen.Close() // tty1 goes back to text mode
	os.WriteFile("/proc/sys/kernel/printk", []byte("7\n"), 0)
	if tty, err := os.OpenFile("/dev/tty1", os.O_WRONLY, 0); err == nil {
		tty.WriteString("\033[2J\033[H")
		cmd := exec.Command("dmesg")
		cmd.Stdout = tty
		cmd.Run()
		tty.Close()
	}
	// SIGRTMIN+20 turns on the status lines of systemd. SIGRTMIN is 34 in glibc, which systemd uses.
	syscall.Kill(1, syscall.Signal(34+20))
}

// osVersion is PRINTDECK_VERSION from os-release, or empty.
func osVersion() string {
	f, err := os.Open("/usr/lib/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "PRINTDECK_VERSION="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// fadeFromStart changes the screen from the start screen to the first screen of the app in half
// a second. The touches during the change are dropped.
func fadeFromStart(screen *fb.FB, app *ui.App, events chan touch.Event) {
	w, h := screen.LogicalW, screen.LogicalH
	c, from := gfx.New(w, h), gfx.New(w, h)
	ui.DrawSplash(from, osVersion(), false)
	to := gfx.New(app.Screen().W, app.Screen().H)
	app.Draw(to)
	tick := time.NewTicker(time.Second / 30)
	defer tick.Stop()
	const steps = 15 // half a second
	for i := 1; i <= steps; i++ {
		ui.Crossfade(c, from, to, float64(i)/steps)
		screen.Present(c)
		<-tick.C
	}
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

// uptime is the time since the boot, from /proc/uptime.
func uptime() time.Duration {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	s, _ := strconv.ParseFloat(f[0], 64)
	return time.Duration(s * float64(time.Second))
}
