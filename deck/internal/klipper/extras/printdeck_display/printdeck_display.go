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
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/display"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/webhooks"
	"printdeck/internal/lcd"
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
	screen    *lcd.Screen
	screenEnd float64 // the time when the screen ends: screenTTL after the last one
	clock24   bool    // the clock of the last screen of the deck
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
	p.printer.RegisterEventHandler("klippy:ready", func(...any) {
		p.reactor.UpdateTimer(p.animTimer, p.reactor.Monotonic()+frameTime)
	})
}

// Status is the status of the module: the command stack of the G-code (gcode.CommandStack), for
// the steps of a start macro on the deck.
func (p *PrintdeckDisplay) Status(eventtime float64) map[string]any {
	return map[string]any{"commands": gcode.Of(p.printer).CommandStack()}
}

// handleScreen takes a screen of the deck: {"screen": {...}}, or {"screen": null} to give the
// display back to Klipper.
func (p *PrintdeckDisplay) handleScreen(req *webhooks.WebRequest) {
	var s *lcd.Screen
	if raw := req.Get("screen", nil); raw != nil {
		b, _ := json.Marshal(raw)
		s = &lcd.Screen{}
		if err := json.Unmarshal(b, s); err != nil {
			log.Printf("printdeck_display: bad screen: %v", err)
			return
		}
	}
	now := p.reactor.Monotonic()
	p.mu.Lock()
	p.screen, p.screenEnd = s, now+screenTTL
	if s != nil {
		p.clock24 = s.Clock24
	}
	p.mu.Unlock()
	if p.lcd != nil {
		p.lcd.RequestRedraw()
		if s != nil && s.Moves() {
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
func (p *PrintdeckDisplay) current(eventtime float64) *lcd.Screen {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.screen == nil || eventtime > p.screenEnd {
		return nil
	}
	return p.screen
}

// drawScreen is the screen hook of the display. With no screen of the deck, it draws the status
// screen from the state of Klipper, so that the display has the look of the deck from the start.
// The menu of Klipper then works with the knob.
func (p *PrintdeckDisplay) drawScreen(eventtime float64) bool {
	s := p.current(eventtime)
	if s == nil {
		if p.lcd.MenuRunning() {
			return false
		}
		s = &lcd.Screen{Status: p.ownStatus(eventtime)}
	}
	c := s.Bits(int(eventtime / frameTime))
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

// ownStatus is the status screen from the state of Klipper.
func (p *PrintdeckDisplay) ownStatus(eventtime float64) *lcd.Status {
	status := func(name string) map[string]any {
		if o, ok := p.printer.TryLookupObject(name).(webhooks.Statuser); ok {
			return o.Status(eventtime)
		}
		return nil
	}
	num := func(m map[string]any, k string) float64 {
		switch v := m[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}
	ex, bed, fan := status("extruder"), status("heater_bed"), status("fan")
	s := &lcd.Status{State: "Ready", Clock: lcd.Clock(time.Now(), p.clock24),
		Tool: num(ex, "temperature"), ToolT: num(ex, "target"),
		Bed: num(bed, "temperature"), BedT: num(bed, "target"), Fan: num(fan, "speed")}
	if ps := status("print_stats"); ps != nil {
		switch st, _ := ps["state"].(string); st {
		case "printing", "paused":
			s.State, s.Printing = strings.ToUpper(st[:1])+st[1:], true
			if ds := status("display_status"); ds != nil {
				s.Progress = num(ds, "progress")
			}
			e := int(num(ps, "print_duration") / 60)
			s.Elapsed = fmt.Sprintf("%d:%02d", e/60, e%60)
		}
	}
	if th := status("toolhead"); th != nil {
		homed, _ := th["homed_axes"].(string)
		if pos, ok := th["position"].(gcode.Coord); ok && homed != "" {
			s.Position = fmt.Sprintf("X%.0f Y%.0f Z%.2f", pos[0], pos[1], pos[2])
		} else if homed == "" {
			s.Message = "Not homed"
		}
	}
	if p.printer.IsShutdown() {
		s.State = "Shutdown"
	}
	return s
}

// animEvent draws the next frame of an animation.
func (p *PrintdeckDisplay) animEvent(eventtime float64) float64 {
	s := p.current(eventtime)
	if s == nil {
		if p.lcd.MenuRunning() {
			return eventtime + 1 // the menu of Klipper: look again later
		}
		s = &lcd.Screen{Status: p.ownStatus(eventtime)}
		if !s.Moves() {
			return eventtime + 1
		}
	}
	if !s.Moves() {
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
