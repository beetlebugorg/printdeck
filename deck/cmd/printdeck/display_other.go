//go:build !linux

package main

import (
	"errors"

	"printdeck/internal/config"
	"printdeck/internal/display"
)

// openPanel fails: only the deck has the panel. Set DISPLAY_KIND=none.
func openPanel(cfg config.Config) (display.Display, error) {
	return nil, errors.New("the panel is only on the deck; set DISPLAY_KIND=none")
}
