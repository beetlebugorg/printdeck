// Package query_adc reports the last value of an analog pin (QUERY_ADC).
//
// Ported to Go from klippy/extras/query_adc.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package query_adc

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
)

// QueryADC holds the analog pins of the sensors.
type QueryADC struct {
	printer *host.Printer
	adc     map[string]*mcu.ADC
}

func load(config *configfile.Wrapper) any {
	q := &QueryADC{printer: host.Of(config), adc: map[string]*mcu.ADC{}}
	gcode.Of(q.printer).RegisterCommand("QUERY_ADC", q.cmdQueryADC, false, "Report the last value of an analog pin")
	return q
}

func init() { host.RegisterModule("query_adc", load, nil) }

// RegisterADC adds an analog pin.
func (q *QueryADC) RegisterADC(name string, adc *mcu.ADC) { q.adc[name] = adc }

func (q *QueryADC) cmdQueryADC(gcmd *gcode.Command) {
	name, _ := gcmd.Get("NAME", gcode.Default(nil))
	adc, ok := q.adc[name]
	if !ok {
		names := make([]string, 0, len(q.adc))
		for n := range q.adc {
			names = append(names, n)
		}
		sort.Strings(names)
		objs := make([]string, len(names))
		for i, n := range names {
			objs[i] = `"` + n + `"`
		}
		gcmd.RespondInfo("Available ADC objects: "+strings.Join(objs, ", "), true)
		return
	}
	last := adc.LastValue()
	msg := fmt.Sprintf("ADC object \"%s\" has value %.6f (timestamp %.3f)", name, last.Value, last.Time)
	if pullup, ok := gcmd.GetFloatOptional("PULLUP", gcode.Above(0.)); ok {
		v := math.Max(.00001, math.Min(.99999, last.Value))
		r := pullup * v / (1.0 - v)
		msg += fmt.Sprintf("\n resistance %.3f (with %.0f pullup)", r, pullup)
	}
	gcmd.RespondInfo(msg, true)
}
