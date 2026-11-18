//go:build !zslice

package main

import "printdeck/internal/printer"

// startZsliceView returns nil: a deck built with no tag zslice draws its own picture of a print.
func startZsliceView(dir string) printer.LiveRenderer { return nil }

// gpuCheck reports that the deck has no zslice.
func gpuCheck(dir string) map[string]any { return map[string]any{"zslice": false} }
