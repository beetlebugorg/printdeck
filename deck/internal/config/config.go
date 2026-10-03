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
	StateDir        string
	NowPlayingPort  int // port of the Inky wHAT service, 0 to disable

	OctoPrintURL string
	OctoPrintKey string

	AlsaCard    string
	MaxVolume   int
	PreheatTool int
	PreheatBed  int
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
		StateDir:        str("STATE_DIRECTORY", "/var/lib/speakerdeck"),
		NowPlayingPort:  num("NOWPLAYING_PORT", 8765),
		OctoPrintURL:    str("OCTOPRINT_URL", "http://127.0.0.1:5000"),
		OctoPrintKey:    str("OCTOPRINT_API_KEY", ""),
		AlsaCard:        str("ALSA_CARD", "sndrpihifiberry"),
		MaxVolume:       num("MAX_VOLUME", 100),
		PreheatTool:     num("PREHEAT_TOOL", 200),
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
