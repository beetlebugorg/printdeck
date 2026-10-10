// Package config reads the settings from the environment.
// The systemd unit loads them from /etc/default/speakerdeck.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Framebuffer string
	Rotate      int // 90 or 270: rotation of the portrait UI onto the landscape panel

	TouchInvertX, TouchInvertY, TouchSwapXY bool

	SpotifyClientID string
	DeviceName      string
	LinkBase        string // address of this speaker for the login with a phone
	StateDir        string
	NowPlayingPort  int // port of the Inky wHAT service, 0 to disable

	OctoPrintURL string
	OctoPrintKey string
	ModelDir     string // folder with STL files for the preview

	AlsaCard           string
	MaxVolume          int
	FloorDB, CeilDB    float64 // the decibels at 0% and 100% of the volume
	PreheatTool        int
	UnloadMM, LoadMM   float64 // filament lengths of the swap
	JogInvertY         bool
	NightFrom, NightTo int // hours of the night theme in auto mode
	PreheatBed         int
}

func Load() Config {
	return Config{
		Framebuffer:     str("FRAMEBUFFER", "/dev/fb0"),
		Rotate:          num("DISPLAY_ROTATE", 90),
		TouchInvertX:    num("TOUCH_INVERT_X", 0) == 1,
		TouchInvertY:    num("TOUCH_INVERT_Y", 0) == 1,
		TouchSwapXY:     num("TOUCH_SWAP_XY", 0) == 1,
		SpotifyClientID: str("SPOTIFY_CLIENT_ID", ""),
		DeviceName:      str("SPOTIFY_DEVICE_NAME", "Speakerdeck"),
		LinkBase:        linkBase(),
		StateDir:        str("STATE_DIRECTORY", "/var/lib/speakerdeck"),
		NowPlayingPort:  num("NOWPLAYING_PORT", 8765),
		OctoPrintURL:    str("OCTOPRINT_URL", "http://127.0.0.1:5000"),
		OctoPrintKey:    str("OCTOPRINT_API_KEY", ""),
		ModelDir:        str("MODEL_DIR", "/var/lib/speakerdeck/models"),
		AlsaCard:        str("ALSA_CARD", "sndrpihifiberry"),
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
