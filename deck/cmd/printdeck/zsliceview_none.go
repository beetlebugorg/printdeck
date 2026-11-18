//go:build !zslice

package main

import (
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// startZsliceView returns nil: a deck built with no tag zslice draws its own picture of a print.
func startZsliceView(dir string) printer.LiveRenderer { return nil }

// gpuCheck reports that the deck has no zslice.
func gpuCheck(dir string) map[string]any { return map[string]any{"zslice": false} }

// startZsliceViewer returns nil: no 3D view that the finger turns.
func startZsliceViewer(dir string) ui.ModelViewer { return nil }
