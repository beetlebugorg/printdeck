// Package query_endstops reports the state of the endstops (QUERY_ENDSTOPS, M119).
//
// Ported to Go from klippy/extras/query_endstops.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package query_endstops

import (
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/webhooks"
)

// Endstop is an endstop that reports its state at a print time.
type Endstop interface {
	QueryEndstop(printTime float64) int
}

type namedEndstop struct {
	endstop Endstop
	name    string
}

type state struct {
	name  string
	value int
}

type toolhead interface{ LastMoveTime() float64 }

// QueryEndstops holds the endstops of the rails.
type QueryEndstops struct {
	printer   *host.Printer
	endstops  []namedEndstop
	lastState []state
}

func load(config *configfile.Wrapper) any {
	q := &QueryEndstops{printer: host.Of(config)}
	// Register webhook if server is available
	webhooks.Of(q.printer).RegisterEndpoint("query_endstops/status", q.handleWebRequest)
	g := gcode.Of(q.printer)
	g.RegisterCommand("QUERY_ENDSTOPS", q.cmdQueryEndstops, false, "Report on the status of each endstop")
	g.RegisterCommand("M119", q.cmdQueryEndstops, false, "")
	return q
}

func init() { host.RegisterModule("query_endstops", load, nil) }

// RegisterEndstop adds an endstop.
func (q *QueryEndstops) RegisterEndstop(endstop any, name string) {
	q.endstops = append(q.endstops, namedEndstop{endstop.(Endstop), name})
}

// Status returns the last query.
func (q *QueryEndstops) Status(eventtime float64) map[string]any {
	last := map[string]any{}
	for _, s := range q.lastState {
		last[s.name] = s.value
	}
	return map[string]any{"last_query": last}
}

func (q *QueryEndstops) query() {
	printTime := q.printer.LookupObject("toolhead").(toolhead).LastMoveTime()
	q.lastState = q.lastState[:0]
	for _, e := range q.endstops {
		q.lastState = append(q.lastState, state{e.name, e.endstop.QueryEndstop(printTime)})
	}
}

func stateName(v int) string {
	if v != 0 {
		return "TRIGGERED"
	}
	return "open"
}

func (q *QueryEndstops) handleWebRequest(req *webhooks.WebRequest) {
	mutex := gcode.Of(q.printer).Mutex()
	mutex.Lock()
	q.query()
	mutex.Unlock()
	res := map[string]any{}
	for _, s := range q.lastState {
		res[s.name] = stateName(s.value)
	}
	req.Send(res)
}

func (q *QueryEndstops) cmdQueryEndstops(gcmd *gcode.Command) {
	// Query the endstops
	q.query()
	// Report results
	parts := make([]string, len(q.lastState))
	for i, s := range q.lastState {
		parts[i] = s.name + ":" + stateName(s.value)
	}
	gcmd.RespondRaw(strings.Join(parts, " "))
}
