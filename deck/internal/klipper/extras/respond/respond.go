// Package respond adds the RESPOND and M118 commands, which send messages to the host.
//
// Ported to Go from klippy/extras/respond.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Alec Plumb <alec@etherwalker.com>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package respond

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

var respondTypes = map[string]any{
	"echo":    "echo:",
	"command": "//",
	"error":   "!!",
}

var respondTypesNoSpace = map[string]string{
	"echo_no_space": "echo:",
}

// HostResponder is the respond object.
type HostResponder struct {
	defaultPrefix string
}

func load(config *configfile.Wrapper) any {
	r := &HostResponder{}
	r.defaultPrefix = config.GetChoice("default_type", respondTypes, configfile.Default("echo")).(string)
	r.defaultPrefix = config.Get("default_prefix", configfile.Default(r.defaultPrefix))
	g := gcode.Of(host.Of(config))
	g.RegisterCommand("M118", r.cmdM118, true, "")
	g.RegisterCommand("RESPOND", r.cmdRespond, true, "Echo the message prepended with a prefix")
	return r
}

func init() { host.RegisterModule("respond", load, nil) }

func (r *HostResponder) cmdM118(gcmd *gcode.Command) {
	msg := gcmd.RawParams()
	gcmd.RespondRaw(fmt.Sprintf("%s %s", r.defaultPrefix, msg))
}

func (r *HostResponder) cmdRespond(gcmd *gcode.Command) {
	noSpace := false
	respondType, hasType := gcmd.Get("TYPE", configfile.Default(nil))
	prefix := r.defaultPrefix
	if hasType {
		respondType = strings.ToLower(respondType)
		if p, ok := respondTypes[respondType]; ok {
			prefix = p.(string)
		} else if p, ok := respondTypesNoSpace[respondType]; ok {
			prefix = p
			noSpace = true
		} else {
			panic(gcmd.Error("RESPOND TYPE '%s' is invalid. Must be one of 'echo', 'command', or 'error'", respondType))
		}
	}
	prefix = gcmd.GetString("PREFIX", configfile.Default(prefix))
	msg := gcmd.GetString("MSG", configfile.Default(""))
	if noSpace {
		gcmd.RespondRaw(prefix + msg)
	} else {
		gcmd.RespondRaw(fmt.Sprintf("%s %s", prefix, msg))
	}
}
