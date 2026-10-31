package main

import (
	"errors"

	"printdeck/internal/printer"
)

// resinAPIPrinter is the resin printer for the app API: the state, pause, resume and cancel of
// the board. The API refuses each start and each resume on a resin deck (octoapi.Server.Resin).
// A resin printer has no G-code, no axes to jog and no heaters.
type resinAPIPrinter struct {
	*printer.ChiTu
}

var errResinAPI = errors.New("a resin printer has no such command")

// PrintFile and Resume do nothing: only a press on the touch screen turns on the UV light. The
// API refuses these requests first (octoapi.Server.Resin).
func (resinAPIPrinter) PrintFile(string) {}
func (resinAPIPrinter) Resume()          {}

func (resinAPIPrinter) Gcode(...string) error            { return errResinAPI }
func (resinAPIPrinter) Home(...string) error             { return errResinAPI }
func (resinAPIPrinter) Jog(x, y, z float64, s int) error { return errResinAPI }
func (resinAPIPrinter) SetToolTemp(int) error            { return errResinAPI }
func (resinAPIPrinter) SetBedTemp(int) error             { return errResinAPI }
