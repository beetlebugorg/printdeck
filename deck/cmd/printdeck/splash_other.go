//go:build !linux

package main

import (
	"log"

	"printdeck/internal/config"
)

// splash is the start screen of the deck. The desktop app has none.
func splash(cfg config.Config) { log.Fatal("the start screen runs only on the deck") }
