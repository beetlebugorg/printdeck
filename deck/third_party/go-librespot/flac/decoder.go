package flac

// Printdeck: a stub in place of the libFLAC decoder, so the program builds with no cgo. The
// daemon never plays FLAC in this build: FLAC needs the PlayPlay plugin, and
// playplay.Plugin.IsSupported() is false (daemon/app.go refuses flac_enabled). The API is the API
// of the libFLAC version.

import (
	"errors"

	librespot "github.com/devgianlu/go-librespot"
)

// Decoder implements an FLAC decoder.
type Decoder struct {
	SampleRate int32
	Channels   int32
	BitDepth   int32
}

// ErrNotSupported is the error of New.
var ErrNotSupported = errors.New("flac: FLAC playback is not supported in this build")

func New(log librespot.Logger, r librespot.SizedReadAtSeeker, gain float32) (*Decoder, error) {
	return nil, ErrNotSupported
}

func (d *Decoder) Read(p []float32) (n int, err error) { return 0, ErrNotSupported }

func (d *Decoder) SetPositionMs(pos int64) error { return ErrNotSupported }

func (d *Decoder) PositionMs() int64 { return 0 }

func (d *Decoder) Close() error { return nil }
