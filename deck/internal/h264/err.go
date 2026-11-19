package h264

import "errors"

// ErrNoDecoder is a deck that is built with no H.264 decoder (no tag h264).
var ErrNoDecoder = errors.New("this deck has no H.264 decoder")
