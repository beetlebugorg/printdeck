//go:build !linux

package camera

import (
	"context"
	"errors"
)

// The capture uses V4L2, which only Linux has. On other systems the deck finds no camera.

func listDevices() []Device { return nil }

func capture(ctx context.Context, d Device, s Settings, format func(Format), emit func(*Frame)) error {
	return errors.New("no camera support on this system")
}
