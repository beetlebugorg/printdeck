//go:build !vulkan

// Package gpujpeg makes the pixels of a JPEG on the GPU. With no tag vulkan, the deck has no GPU
// code: Open gives an error.
package gpujpeg

import (
	"errors"
	"image"

	"printdeck/internal/jpegdec"
)

// Available reports that the deck has the GPU code.
const Available = false

// GPU is a compute pipeline on the GPU.
type GPU struct{ Name string }

// Open gives an error: the deck has no GPU code.
func Open() (*GPU, error) { return nil, errors.New("gpujpeg: the deck has no GPU code") }

// Close has no effect: there is no GPU code.
func (g *GPU) Close() {}

// Render gives an error.
func (g *GPU) Render(in *jpegdec.GPUInput) (image.Image, error) {
	return nil, errors.New("gpujpeg: the deck has no GPU code")
}
