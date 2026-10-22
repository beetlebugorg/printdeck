package main

import (
	"log"

	"printdeck/internal/config"
	"printdeck/internal/display"
)

func openPanel(cfg config.Config) (display.Display, error) {
	p, err := display.OpenPanel(cfg.Framebuffer, cfg.Rotate)
	if err != nil {
		return nil, err
	}
	pw, ph := p.PanelSize()
	log.Printf("framebuffer %s: %dx%d, rotation %d", cfg.Framebuffer, pw, ph, cfg.Rotate)
	return p, nil
}
