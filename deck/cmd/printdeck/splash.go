package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"printdeck/internal/display"
	"printdeck/internal/gfx"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

// osVersion is the version of this deck program: PRINTDECK_VERSION of the os-release file of its
// system, or "". The deck program of an installed update (runNextDeck) reads the file of its slot.
func osVersion() string { return releaseVersion(filepath.Join(deckRoot(), "usr/lib/os-release")) }

// systemVersion is the version of the running system, or "".
func systemVersion() string { return releaseVersion("/usr/lib/os-release") }

// releaseVersion returns PRINTDECK_VERSION of an os-release file, or "".
func releaseVersion(path string) string {
	f, err := os.Open(path)
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
func fadeFromStart(screen display.Display, app *ui.App, events chan touch.Event) {
	w, h := screen.Size().X, screen.Size().Y
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
