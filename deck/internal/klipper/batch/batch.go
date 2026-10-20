// Package batch runs the Klipper host in klippy's batch mode: it reads a config file and a G-code
// file, and writes the commands for the board to a file, with no board. The tests compare this
// file with the file of the real Klipper host (tools/klipper-reference).
package batch

import (
	"fmt"
	"os"
	"strings"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/reactor"

	_ "printdeck/internal/klipper/extras/all"
)

// Run runs the printer with a config file, the data dictionary of the board, and a G-code file.
// It writes the commands to output, and returns the run result and the state message of the
// printer.
func Run(configFile, dictionary, gcodeFile, output string) (string, string) {
	r := reactor.New()
	p := host.New(r, host.StartArgs{"config_file": configFile, "start_reason": "startup",
		"debuginput": gcodeFile, "debugoutput": output, "dictionary": dictionary})
	// The G-code input of the batch mode is not ported yet: the run ends when the printer is
	// ready.
	p.RegisterEventHandler("klippy:ready", func(...any) {
		r.RegisterCallback(func(float64) any { p.RequestExit("exit"); return nil }, reactor.Now)
	})
	result := p.Run()
	msg, _ := p.StateMessage()
	return result, msg
}

// Decode returns the text form of a command file, one command on each line, as klippy's
// parsedump.py writes it.
func Decode(dictionary, output string) (string, error) {
	dict, err := os.ReadFile(dictionary)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return "", err
	}
	mp := msgproto.NewParser()
	if err := mp.ProcessIdentify(dict, false); err != nil {
		return "", err
	}
	var out strings.Builder
	for len(data) > 0 {
		l := mp.CheckPacket(data)
		if l <= 0 {
			return out.String(), fmt.Errorf("bad block at %d bytes from the end", len(data))
		}
		msgs, err := mp.Dump(data[:l])
		if err != nil {
			return out.String(), err
		}
		for _, m := range msgs[1:] {
			out.WriteString(m + "\n")
		}
		data = data[l:]
	}
	return out.String(), nil
}
