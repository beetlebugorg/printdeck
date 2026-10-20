// Package verify_heater shuts the printer down when a heater does not heat as expected: the
// thermal runaway protection of each heater.
//
// Ported to Go from klippy/extras/verify_heater.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package verify_heater

import (
	"fmt"
	"log"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

const hintThermal = `
See the 'verify_heater' section in docs/Config_Reference.md
for the parameters that control this check.
`

// HeaterCheck checks one heater once a second.
type HeaterCheck struct {
	printer           *host.Printer
	heaterName        string
	heater            *heaters.Heater
	hysteresis        float64
	maxError          float64
	heatingGain       float64
	checkGainTime     float64
	approachingTarget bool
	startingApproach  bool
	lastTarget        float64
	goalTemp          float64
	err               float64
	goalSystime       float64
	checkTimer        *reactor.Timer
}

func loadPrefix(config *configfile.Wrapper) any {
	printer := host.Of(config)
	hc := &HeaterCheck{printer: printer, heaterName: strings.Fields(config.Name())[1], goalSystime: reactor.Never}
	printer.RegisterEventHandler("klippy:connect", func(...any) { hc.handleConnect() })
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { hc.handleShutdown() })
	hc.hysteresis = config.GetFloat("hysteresis", configfile.Default(5.), configfile.Min(0.))
	hc.maxError = config.GetFloat("max_error", configfile.Default(120.), configfile.Min(0.))
	hc.heatingGain = config.GetFloat("heating_gain", configfile.Default(2.), configfile.Above(0.))
	defaultGainTime := 20.
	if hc.heaterName == "heater_bed" {
		defaultGainTime = 60.
	}
	hc.checkGainTime = config.GetFloat("check_gain_time", configfile.Default(defaultGainTime), configfile.Min(1.))
	return hc
}

func init() { host.RegisterModule("verify_heater", nil, loadPrefix) }

func (hc *HeaterCheck) handleConnect() {
	if _, ok := hc.printer.StartArgs().Get("debugoutput"); ok {
		// Disable verify_heater if outputting to a debug file
		return
	}
	hc.heater = heaters.Of(hc.printer).LookupHeater(hc.heaterName)
	log.Printf("Starting heater checks for %s", hc.heaterName)
	hc.checkTimer = hc.printer.Reactor().RegisterTimer(hc.checkEvent, reactor.Now)
}

func (hc *HeaterCheck) handleShutdown() {
	if hc.checkTimer != nil {
		hc.printer.Reactor().UpdateTimer(hc.checkTimer, reactor.Never)
	}
}

func (hc *HeaterCheck) checkEvent(eventtime float64) float64 {
	temp, target := hc.heater.GetTemp(eventtime)
	if temp >= target-hc.hysteresis || target <= 0. {
		// Temperature near target - reset checks
		if hc.approachingTarget && target != 0 {
			log.Printf("Heater %s within range of %.3f", hc.heaterName, target)
		}
		hc.approachingTarget, hc.startingApproach = false, false
		if temp <= target+hc.hysteresis {
			hc.err = 0.
		}
		hc.lastTarget = target
		return eventtime + 1.
	}
	hc.err += (target - hc.hysteresis) - temp
	if !hc.approachingTarget {
		if target != hc.lastTarget {
			// Target changed - reset checks
			log.Printf("Heater %s approaching new target of %.3f", hc.heaterName, target)
			hc.approachingTarget, hc.startingApproach = true, true
			hc.goalTemp = temp + hc.heatingGain
			hc.goalSystime = eventtime + hc.checkGainTime
		} else if hc.err >= hc.maxError {
			// Failure due to inability to maintain target temperature
			return hc.heaterFault()
		}
	} else if temp >= hc.goalTemp {
		// Temperature approaching target - reset checks
		hc.startingApproach = false
		hc.err = 0.
		hc.goalTemp = temp + hc.heatingGain
		hc.goalSystime = eventtime + hc.checkGainTime
	} else if eventtime >= hc.goalSystime {
		// Temperature is no longer approaching target
		hc.approachingTarget = false
		log.Printf("Heater %s no longer approaching target %.3f", hc.heaterName, target)
	} else if hc.startingApproach {
		hc.goalTemp = math.Min(hc.goalTemp, temp+hc.heatingGain)
	}
	hc.lastTarget = target
	return eventtime + 1.
}

func (hc *HeaterCheck) heaterFault() float64 {
	msg := fmt.Sprintf("Heater %s not heating at expected rate", hc.heaterName)
	log.Print(msg)
	hc.printer.InvokeShutdown(msg+hintThermal, nil)
	return reactor.Never
}
