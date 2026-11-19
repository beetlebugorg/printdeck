//go:build !turbojpeg

// Package turbojpeg decodes and encodes JPEG with libjpeg-turbo. With no tag turbojpeg, the deck
// has no libjpeg-turbo: the functions give an error.
package turbojpeg

import (
	"errors"
	"image"
)

// Available reports that the deck has libjpeg-turbo.
const Available = false

var errNone = errors.New("turbojpeg: the deck has no libjpeg-turbo")

// Decode gives an error.
func Decode(b []byte, shift int) (*image.YCbCr, error) { return nil, errNone }

// Encode gives an error.
func Encode(p *image.YCbCr, quality int) ([]byte, error) { return nil, errNone }
