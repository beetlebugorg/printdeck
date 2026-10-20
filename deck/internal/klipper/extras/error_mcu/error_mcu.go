// Package error_mcu writes the longer messages of board errors: the board shutdowns, the protocol
// errors, and the connect errors.
//
// Ported to Go from klippy/extras/error_mcu.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package error_mcu

import (
	"fmt"
	"log"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
)

const messageShutdown = `
Once the underlying issue is corrected, use the
"FIRMWARE_RESTART" command to reset the firmware, reload the
config, and restart the host software.
Printer is shutdown
`

const messageProtocolError1 = `
This is frequently caused by running an older version of the
firmware on the MCU(s). Fix by recompiling and flashing the
firmware.
`

const messageProtocolError2 = `
Once the underlying issue is corrected, use the "RESTART"
command to reload the config and restart the host software.
`

const messageMCUConnectError = `
Once the underlying issue is corrected, use the
"FIRMWARE_RESTART" command to reset the firmware, reload the
config, and restart the host software.
Error configuring printer
`

// commonMCUErrors are the hints for the shutdown messages that start with a prefix, in the order
// of Klipper.
var commonMCUErrors = []struct {
	prefixes []string
	help     string
}{
	{[]string{"Timer too close"}, `
This often indicates the host computer is overloaded. Check
for other processes consuming excessive CPU time, high swap
usage, disk errors, overheating, unstable voltage, or
similar system problems on the host computer.`},
	{[]string{"Missed scheduling of next "}, `
This is generally indicative of an intermittent
communication failure between micro-controller and host.`},
	{[]string{"ADC out of range"}, `
This generally occurs when a heater temperature exceeds
its configured min_temp or max_temp.`},
	{[]string{"Rescheduled timer in the past", "Stepper too far in past"}, `
This generally occurs when the micro-controller has been
requested to step at a rate higher than it is capable of
obtaining.`},
	{[]string{"Command request"}, `
This generally occurs in response to an M112 G-Code command
or in response to an internal error in the host software.`},
}

func errorHint(msg string) string {
	for _, e := range commonMCUErrors {
		for _, prefix := range e.prefixes {
			if strings.HasPrefix(msg, prefix) {
				return e.help
			}
		}
	}
	return ""
}

// ClarifyFunc returns more text for a shutdown message, or "".
type ClarifyFunc func(msg string, details map[string]any) string

// statuser is the part of a board that the protocol error message needs.
type statuser interface {
	Status(eventtime float64) map[string]any
}

// PrinterMCUError writes the messages of board errors.
type PrinterMCUError struct {
	printer          *host.Printer
	clarifyCallbacks map[string][]ClarifyFunc
}

func load(config *configfile.Wrapper) any {
	e := &PrinterMCUError{printer: host.Of(config), clarifyCallbacks: map[string][]ClarifyFunc{}}
	e.printer.RegisterEventHandler("klippy:analyze_shutdown", func(args ...any) {
		e.handleAnalyzeShutdown(args[0].(string), details(args[1]))
	})
	e.printer.RegisterEventHandler("klippy:notify_mcu_error", func(args ...any) {
		e.handleNotifyMCUError(args[0].(string), details(args[1]))
	})
	return e
}

func init() { host.RegisterModule("error_mcu", load, nil) }

func details(v any) map[string]any {
	d, _ := v.(map[string]any)
	if d == nil {
		d = map[string]any{}
	}
	return d
}

// AddClarify adds a function that gives more text for a board shutdown message.
func (e *PrinterMCUError) AddClarify(msg string, callback ClarifyFunc) {
	e.clarifyCallbacks[msg] = append(e.clarifyCallbacks[msg], callback)
}

func (e *PrinterMCUError) checkMCUShutdown(msg string, details map[string]any) {
	mcuName := fmt.Sprint(details["mcu"])
	mcuMsg := fmt.Sprint(details["reason"])
	prefix := fmt.Sprintf("MCU '%s' shutdown: ", mcuName)
	if details["event_type"] == "is_shutdown" {
		prefix = fmt.Sprintf("Previous MCU '%s' shutdown: ", mcuName)
	}
	// Lookup generic hint
	hint := errorHint(mcuMsg)
	// Add per instance help
	var clarify []string
	for _, cb := range e.clarifyCallbacks[mcuMsg] {
		if cm := cb(msg, details); cm != "" {
			clarify = append(clarify, cm)
		}
	}
	clarifyMsg := ""
	if len(clarify) > 0 {
		clarifyMsg = strings.Join(append(append([]string{"", ""}, clarify...), ""), "\n")
	}
	// Update error message
	e.printer.UpdateErrorMsg(msg, prefix+mcuMsg+clarifyMsg+hint+messageShutdown)
}

func (e *PrinterMCUError) handleAnalyzeShutdown(msg string, details map[string]any) {
	if msg == "MCU shutdown" {
		e.checkMCUShutdown(msg, details)
	} else {
		e.printer.UpdateErrorMsg(msg, msg+messageShutdown)
	}
}

func (e *PrinterMCUError) checkProtocolError(msg string, details map[string]any) {
	hostVersion, _ := e.printer.StartArgs().Get("software_version")
	var msgUpdate, msgUpdated []string
	for _, o := range e.printer.LookupObjects("mcu") {
		s, ok := o.Obj.(statuser)
		if !ok {
			log.Print("Unable to retrieve mcu_version from mcu")
			continue
		}
		mcuVersion := fmt.Sprint(s.Status(0)["mcu_version"])
		f := strings.Fields(o.Name)
		line := fmt.Sprintf("%s: Current version %s", f[len(f)-1], mcuVersion)
		if mcuVersion != hostVersion {
			msgUpdate = append(msgUpdate, line)
		} else {
			msgUpdated = append(msgUpdated, line)
		}
	}
	if len(msgUpdate) == 0 {
		msgUpdate = []string{"<none>"}
	}
	if len(msgUpdated) == 0 {
		msgUpdated = []string{"<none>"}
	}
	newmsg := []string{"MCU Protocol error", messageProtocolError1,
		"Your Klipper version is: " + hostVersion, "MCU(s) which should be updated:"}
	newmsg = append(newmsg, msgUpdate...)
	newmsg = append(newmsg, "Up-to-date MCU(s):")
	newmsg = append(newmsg, msgUpdated...)
	newmsg = append(newmsg, messageProtocolError2, fmt.Sprint(details["error"]))
	e.printer.UpdateErrorMsg(msg, strings.Join(newmsg, "\n"))
}

func (e *PrinterMCUError) handleNotifyMCUError(msg string, details map[string]any) {
	switch msg {
	case "Protocol error":
		e.checkProtocolError(msg, details)
	case "MCU error during connect":
		e.printer.UpdateErrorMsg(msg, fmt.Sprint(details["error"])+messageMCUConnectError)
	}
}
