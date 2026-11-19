//go:build !h264

package h264

import (
	"image"
)

// Decoder is a decoder of a deck with no libavcodec: New returns ErrNoDecoder.
type Decoder struct{}

// New returns ErrNoDecoder: this deck has no H.264 decoder.
func New(int) (*Decoder, error) { return nil, ErrNoDecoder }

func (d *Decoder) Close()                                {}
func (d *Decoder) Decode([]byte) ([]*image.YCbCr, error) { return nil, ErrNoDecoder }
