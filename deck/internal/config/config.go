// Package config reads the settings from the environment.
// The systemd unit loads them from /etc/default/printdeck.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Framebuffer string
	Rotate      int // 90 or 270: rotation of the portrait UI onto the landscape panel

	TouchInvertX, TouchInvertY, TouchSwapXY bool
	// TouchLog logs each touch sample, for the measurement of the calibration.
	TouchLog bool
	// TouchOffsetX and TouchOffsetY move each touch of the panel, in pixels of the UI. Positive
	// is to the right and down.
	TouchOffsetX, TouchOffsetY int

	SpotifyClientID string
	DeviceName      string
	LinkBase        string // address of this speaker for the login with a phone
	StateDir        string

	OctoPrintURL string
	OctoPrintKey string
	ModelDir     string // folder with STL files for the preview

	AlsaCard string
	// Speaker runs the Spotify Connect speaker (go-librespot) in this program, on SpeakerDevice,
	// with its state in SpotifyDir.
	Speaker            bool
	SpeakerDevice      string
	SpotifyDir         string
	SpotifyDebug       bool // log the debug messages of go-librespot
	MaxVolume          int
	FloorDB, CeilDB    float64 // the decibels at 0% and 100% of the volume
	PreheatTool        int
	UnloadMM, LoadMM   float64 // filament lengths of the swap
	JogInvertY         bool
	NightFrom, NightTo int // hours of the night theme in auto mode
	PreheatBed         int
	RemoteAddr         string // address of the remote screen, for example ":80"; empty turns it off
	KlippySocket       string // the API socket of Klipper; when set, the deck uses Klipper directly, not OctoPrint
	GcodeDir           string // the folder of Klipper's [virtual_sdcard]
}

func Load() Config {
	c := Config{
		Framebuffer:     str("FRAMEBUFFER", "/dev/fb0"),
		Rotate:          num("DISPLAY_ROTATE", 90),
		TouchInvertX:    num("TOUCH_INVERT_X", 0) == 1,
		TouchInvertY:    num("TOUCH_INVERT_Y", 0) == 1,
		TouchSwapXY:     num("TOUCH_SWAP_XY", 0) == 1,
		TouchLog:        num("TOUCH_LOG", 0) == 1,
		TouchOffsetX:    num("TOUCH_OFFSET_X", 0),
		TouchOffsetY:    num("TOUCH_OFFSET_Y", 0),
		SpotifyClientID: str("SPOTIFY_CLIENT_ID", ""),
		DeviceName:      str("SPOTIFY_DEVICE_NAME", "Printdeck"),
		LinkBase:        linkBase(),
		StateDir:        str("STATE_DIRECTORY", "/var/lib/printdeck"),
		RemoteAddr:      str("REMOTE_ADDR", ""),
		KlippySocket:    str("KLIPPY_SOCKET", ""),
		GcodeDir:        str("GCODE_DIR", "/var/lib/klipper/gcodes"),
		OctoPrintURL:    str("OCTOPRINT_URL", "http://127.0.0.1:5000"),
		OctoPrintKey:    str("OCTOPRINT_API_KEY", ""),
		ModelDir:        str("MODEL_DIR", "/var/lib/printdeck/models"),
		AlsaCard:        str("ALSA_CARD", "sndrpihifiberry"),
		Speaker:         str("SPOTIFY_SPEAKER", "1") == "1",
		SpotifyDebug:    str("SPOTIFY_DEBUG", "0") == "1",
		MaxVolume:       num("MAX_VOLUME", 100),
		FloorDB:         float64(num("VOLUME_FLOOR_DB", -40)),
		CeilDB:          float64(num("VOLUME_CEIL_DB", -10)),
		PreheatTool:     num("PREHEAT_TOOL", 200),
		UnloadMM:        float64(num("FILAMENT_UNLOAD_MM", 80)),
		LoadMM:          float64(num("FILAMENT_LOAD_MM", 70)),
		JogInvertY:      num("JOG_INVERT_Y", 0) == 1,
		NightFrom:       num("NIGHT_FROM", 20),
		NightTo:         num("NIGHT_TO", 7),
		PreheatBed:      num("PREHEAT_BED", 60),
	}
	c.SpeakerDevice = str("SPEAKER_DEVICE", "hw:"+c.AlsaCard)
	c.SpotifyDir = str("SPOTIFY_DIR", c.StateDir+"/spotify")
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

// linkBase is the address that the phone uses to reach the speaker for the Spotify login.
// SPOTIFY_LINK_BASE changes it, for example to https://192.168.1.20:8888 when the phone cannot find .local names.
func linkBase() string {
	if v := os.Getenv("SPOTIFY_LINK_BASE"); v != "" {
		return v
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return ""
	}
	return "https://" + host + ".local:8888"
}
