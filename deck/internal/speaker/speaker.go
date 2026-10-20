// Package speaker is the Spotify Connect speaker of the deck: go-librespot's daemon
// (deck/third_party/go-librespot) in this program, with the pure Go ALSA output and the equalizer.
// It replaces the go-librespot and CamillaDSP programs.
//
// The setup follows cmd/daemon/main.go of go-librespot (GPL-3.0, Copyright (c) devgianlu): the
// daemon config has the settings that Printdeck gave the go-librespot program before, on the
// defaults of cmd/daemon/cli_config.go. The API of the daemon serves the deck's spotify package
// on 127.0.0.1.
package speaker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devgianlu/go-librespot/daemon"
	"github.com/devgianlu/go-librespot/output"

	"printdeck/internal/eq"
)

// Config is the setup of the speaker.
type Config struct {
	DeviceName string // the name in the Spotify app
	Device     string // the ALSA device of the amp, like "hw:sndrpihifiberry"
	ConfigDir  string // the state and the credentials of go-librespot
	APIAddr    string // the address of the API (go-librespot's server.address)
	APIPort    int
	EQ         *eq.EQ
	Volume     int  // the volume at the start in percent, or -1 for the default
	Debug      bool // log go-librespot's debug messages
}

// Run runs the speaker until ctx ends.
func Run(ctx context.Context, cfg Config) error {
	if err := os.MkdirAll(cfg.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("speaker: %w", err)
	}
	log := newLogger(cfg.Debug)
	output.RegisterBackend(backendName, func(opts *output.NewOutputOptions) (output.Output, error) {
		return newAlsaOutput(opts, cfg.EQ), nil
	})

	dc := &daemon.Config{
		DeviceName:   cfg.DeviceName,
		DeviceType:   "speaker",
		AudioBackend: backendName,
		AudioDevice:  cfg.Device,
		// go-librespot must not scale the samples: the speaker volume is set on the amp. The deck
		// follows the volume events of the API.
		ExternalVolume: true,

		// The defaults of cmd/daemon/cli_config.go
		Bitrate:          160,
		VolumeSteps:      100,
		InitialVolume:    100,
		SkipDebounce:     600 * time.Millisecond,
		MixerControlName: "Master",
		ZeroconfEnabled:  false,
		ZeroconfBackend:  "builtin",
		ImageSize:        "default",
	}
	// The deck keeps the volume between starts, and Spotify starts with the same volume. The
	// LastVolume of state.json is not used.
	if cfg.Volume >= 0 {
		dc.InitialVolume = uint32(cfg.Volume)
		dc.IgnoreLastVolume = true
	}
	dc.Cache.Dir = filepath.Join(cfg.ConfigDir, "cache")
	dc.Metadata.Enabled = true
	dc.Metadata.MaxTracks = 800
	dc.Credentials.Type = "zeroconf"
	dc.Credentials.Zeroconf.PersistCredentials = true

	store := newFileStateStore(filepath.Join(cfg.ConfigDir, "state.json"),
		filepath.Join(cfg.ConfigDir, "credentials.json"), log)
	api, err := daemon.NewApiServer(log, cfg.APIAddr, cfg.APIPort, "", "", "")
	if err != nil {
		return fmt.Errorf("speaker: api server: %w", err)
	}
	app, err := daemon.New(&daemon.Options{Logger: log, Config: dc, StateStore: store, APIServer: api})
	if err != nil {
		return fmt.Errorf("speaker: %w", err)
	}
	defer app.Close()
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("speaker: %w", err)
	}
	return nil
}
