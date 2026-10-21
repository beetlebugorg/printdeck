// Package printdeck_display lets the deck draw the display of the printer and read its knob. It
// is a module of Printdeck, not a port of a Klipper module. The config section is
// [printdeck_display] (printdeck.cfg has it). It works with the [display] of printer.cfg.
//
// The deck sends a screen with the endpoint printdeck/screen, and the module draws it at the
// pixels of the display: rows of text, a selection, a progress bar, or a full screen animation.
// The knob and the buttons of the display go to the deck through printdeck/subscribe_input. While
// the deck sends no screen, the display shows the screen and the menu of Klipper.
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_display

import (
	"encoding/json"
	"log"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/display"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/webhooks"
)

// screenTTL is how long a screen stays without a new one. The deck sends its screen again
// before this time, so the display goes back to Klipper's screen when the deck stops.
const screenTTL = 15.

// frameTime is the time between the frames of an animation.
const frameTime = 0.25

// PrintdeckDisplay is the [printdeck_display] section.
type PrintdeckDisplay struct {
	printer *host.Printer
	reactor *reactor.Reactor
	lcd     *display.PrinterLCD

	mu        sync.Mutex
	screen    *Screen
	screenEnd float64 // the time when the screen ends: screenTTL after the last one
	clients   map[webhooks.ClientConn]map[string]any
	animTimer *reactor.Timer
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	p := &PrintdeckDisplay{printer: printer, reactor: printer.Reactor(), clients: map[webhooks.ClientConn]map[string]any{}}
	wh := webhooks.Of(printer)
	wh.RegisterEndpoint("printdeck/screen", p.handleScreen)
	wh.RegisterEndpoint("printdeck/subscribe_input", p.handleSubscribeInput)
	printer.RegisterEventHandler("klippy:connect", func(...any) { p.handleConnect() })
	return p
}

func init() { host.RegisterModule("printdeck_display", load, nil) }

func (p *PrintdeckDisplay) handleConnect() {
	lcd, ok := p.printer.TryLookupObject("display").(*display.PrinterLCD)
	if !ok {
		return // the printer has no display
	}
	p.lcd = lcd
	lcd.SetHooks(p.drawScreen, p.handleKey)
	p.animTimer = p.reactor.RegisterTimer(p.animEvent, reactor.Never)
}

// handleScreen takes a screen of the deck: {"screen": {...}}, or {"screen": null} to give the
// display back to Klipper.
func (p *PrintdeckDisplay) handleScreen(req *webhooks.WebRequest) {
	var s *Screen
	if raw := req.Get("screen", nil); raw != nil {
		b, _ := json.Marshal(raw)
		s = &Screen{}
		if err := json.Unmarshal(b, s); err != nil {
			log.Printf("printdeck_display: bad screen: %v", err)
			return
		}
	}
	now := p.reactor.Monotonic()
	p.mu.Lock()
	p.screen, p.screenEnd = s, now+screenTTL
	p.mu.Unlock()
	if p.lcd != nil {
		p.lcd.RequestRedraw()
		if s != nil && s.Anim != "" {
			p.reactor.UpdateTimer(p.animTimer, now+frameTime)
		}
	}
}

// handleSubscribeInput sends each key of the knob and the buttons to the client, as
// {"key": "up"} in the response_template of the request.
func (p *PrintdeckDisplay) handleSubscribeInput(req *webhooks.WebRequest) {
	conn := req.ClientConnection()
	template := req.GetDict("response_template", map[string]any{})
	p.mu.Lock()
	p.clients[conn] = template
	p.mu.Unlock()
}

// current returns the screen of the deck, or nil when it has none or it is too old.
func (p *PrintdeckDisplay) current(eventtime float64) *Screen {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.screen == nil || eventtime > p.screenEnd {
		return nil
	}
	return p.screen
}

// drawScreen is the screen hook of the display.
func (p *PrintdeckDisplay) drawScreen(eventtime float64) bool {
	s := p.current(eventtime)
	if s == nil {
		return false
	}
	var c canvas
	s.draw(&c, int(eventtime/frameTime))
	lcd := p.lcd.LCD()
	data := make([]byte, 16)
	for row := 0; row < 4; row++ {
		for col := 0; col < 16; col++ {
			for i := range data {
				data[i] = c[row*16+i][col]
			}
			lcd.WriteGraphics(col, row, data)
		}
	}
	return true
}

// animEvent draws the next frame of an animation.
func (p *PrintdeckDisplay) animEvent(eventtime float64) float64 {
	s := p.current(eventtime)
	if s == nil || s.Anim == "" {
		return reactor.Never
	}
	p.lcd.RequestRedraw()
	return eventtime + frameTime
}

// handleKey is the key hook of the display. It sends the key to the deck, and keeps it from the
// menu, while the deck has a screen and a subscription.
func (p *PrintdeckDisplay) handleKey(key string, eventtime float64) bool {
	if p.current(eventtime) == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	sent := false
	for conn, template := range p.clients {
		if conn.IsClosed() {
			delete(p.clients, conn)
			continue
		}
		msg := map[string]any{}
		for k, v := range template {
			msg[k] = v
		}
		msg["params"] = map[string]any{"key": key}
		conn.Send(msg)
		sent = true
	}
	return sent
}
