// Package config reads the settings from the environment.
// The systemd unit loads them from /etc/default/printdeck.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Display is the screen of the UI: "panel" (the framebuffer and the touch panel of the deck) or
	// "none" (no local screen; the remote screen only).
	Display     string
	Framebuffer string
	// Rotate turns the UI onto the landscape panel: 90 or 270 for a display that is mounted tall,
	// 0 or 180 for a display that is mounted wide.
	Rotate int

	TouchInvertX, TouchInvertY, TouchSwapXY bool
	// TouchLog logs each touch sample, for the measurement of the calibration.
	TouchLog bool
	// TouchOffsetX and TouchOffsetY move each touch of the panel, in pixels of the UI. Positive
	// is to the right and down.
	TouchOffsetX, TouchOffsetY int

	StateDir string

	OctoPrintKey string // the key of the OctoPrint-compatible API for slicers
	ModelDir     string // folder with STL files for the preview

	PreheatTool        int
	UnloadMM, LoadMM   float64 // filament lengths of the swap
	JogInvertY         bool
	NightFrom, NightTo int // hours of the night theme in auto mode
	PreheatBed         int
	RemoteAddr         string // address of the remote screen, for example ":80"; empty turns it off
	// TLSAddr is the HTTPS address of the same server, for example ":443", with the certificate
	// of the name that the cloud service gives the deck (tlscert). Empty turns it off.
	TLSAddr string
	// The web pages that can call the API for slicers from the browser (CORS), separated by
	// spaces: the zslice web app. Each call still needs the API key. "none" turns it off.
	WebOrigins    string
	KlippySocket  string // the API socket of the Klipper host
	GcodeDir      string // the folder of Klipper's [virtual_sdcard]
	KlipperConfig string // printer.cfg of the Klipper host
	KlippyLog     string // the log of the Klipper host, for the system page
}

func Load() Config {
	c := Config{
		Display:       str("DISPLAY_KIND", "panel"),
		Framebuffer:   str("FRAMEBUFFER", "/dev/fb0"),
		Rotate:        num("DISPLAY_ROTATE", -1), // -1: from the shape of the panel (fb.Open)
		TouchInvertX:  num("TOUCH_INVERT_X", 0) == 1,
		TouchInvertY:  num("TOUCH_INVERT_Y", 0) == 1,
		TouchSwapXY:   num("TOUCH_SWAP_XY", 0) == 1,
		TouchLog:      num("TOUCH_LOG", 0) == 1,
		TouchOffsetX:  num("TOUCH_OFFSET_X", 0),
		TouchOffsetY:  num("TOUCH_OFFSET_Y", 0),
		StateDir:      str("STATE_DIRECTORY", "/var/lib/printdeck"),
		RemoteAddr:    str("REMOTE_ADDR", ""),
		TLSAddr:       str("TLS_ADDR", ""),
		WebOrigins:    str("WEB_ORIGINS", "https://zslice.beetlebug.org"),
		KlippySocket:  str("KLIPPY_SOCKET", "/run/klipper/klippy.sock"),
		GcodeDir:      str("GCODE_DIR", "/var/lib/klipper/gcodes"),
		KlipperConfig: str("KLIPPER_CONFIG", "/var/lib/klipper/config/printer.cfg"),
		// The log is in RAM, because the disk is an SD card.
		KlippyLog:    str("KLIPPY_LOG", "/run/klipper/klippy.log"),
		OctoPrintKey: str("OCTOPRINT_API_KEY", ""),
		ModelDir:     str("MODEL_DIR", "/var/lib/printdeck/models"),
		PreheatTool:  num("PREHEAT_TOOL", 200),
		UnloadMM:     float64(num("FILAMENT_UNLOAD_MM", 80)),
		LoadMM:       float64(num("FILAMENT_LOAD_MM", 70)),
		JogInvertY:   num("JOG_INVERT_Y", 0) == 1,
		NightFrom:    num("NIGHT_FROM", 20),
		NightTo:      num("NIGHT_TO", 7),
		PreheatBed:   num("PREHEAT_BED", 60),
	}
	return c
}

func str(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func num(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}
