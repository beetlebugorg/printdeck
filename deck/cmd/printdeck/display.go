package main

import (
	"log"

	"printdeck/internal/config"
	"printdeck/internal/display"
)

// openDisplay opens the screen of the configuration (config.Config.Display).
func openDisplay(cfg config.Config) display.Display {
	if cfg.Display == "none" {
		log.Print("display: none (the remote screen only)")
		return display.None{}
	}
	p, err := openPanel(cfg)
	if err != nil {
		log.Fatalf("framebuffer: %v", err)
	}
	return p
}
