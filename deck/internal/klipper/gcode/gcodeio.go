package gcode

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

type inputBlock struct {
	eventtime float64
	data      string
}

// GCodeIO reads G-code from the file of the batch mode (start argument "gcode_fd", with
// "debuginput"). Klipper also reads a pseudo-terminal here. The Go host does not: the deck sends
// G-code to the dispatcher directly.
type GCodeIO struct {
	printer          *host.Printer
	gcode            *Dispatch
	gcodeMutex       *reactor.Mutex
	fd               *os.File
	reactor          *reactor.Reactor
	isPrinterReady   bool
	isProcessingData bool
	isFileinput      bool
	pipeIsActive     bool
	fdHandle         *reactor.FileHandler
	partialInput     string
	pendingCommands  []string
	bytesRead        int
	inputLog         []inputBlock
}

func newGCodeIO(printer *host.Printer) *GCodeIO {
	gio := &GCodeIO{printer: printer, gcode: Of(printer), reactor: printer.Reactor(), pipeIsActive: true}
	gio.gcodeMutex = gio.gcode.Mutex()
	gio.fd, _ = printer.StartArgs()["gcode_fd"].(*os.File)
	_, gio.isFileinput = printer.StartArgs().Get("debuginput")
	// Register event handlers
	printer.RegisterEventHandler("klippy:ready", func(...any) { gio.handleReady() })
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { gio.handleShutdown() })
	printer.RegisterEventHandler("klippy:analyze_shutdown", func(...any) { gio.handleAnalyzeShutdown() })
	return gio
}

func init() {
	host.EarlyObjects = append(host.EarlyObjects, func(p *host.Printer) { p.AddObject("gcode_io", newGCodeIO(p)) })
}

func (gio *GCodeIO) handleReady() {
	gio.isPrinterReady = true
	if gio.isFileinput && gio.fdHandle == nil && gio.fd != nil {
		gio.fdHandle = gio.reactor.RegisterFd(gio.fd, gio.processData)
	}
}

func (gio *GCodeIO) handleAnalyzeShutdown() {
	out := []string{fmt.Sprintf("Dumping gcode input %d blocks", len(gio.inputLog))}
	for _, b := range gio.inputLog {
		out = append(out, fmt.Sprintf("Read %f: %q", b.eventtime, b.data))
	}
	log.Print(strings.Join(out, "\n"))
}

func (gio *GCodeIO) handleShutdown() {
	if !gio.isPrinterReady {
		return
	}
	gio.isPrinterReady = false
	if gio.isFileinput {
		gio.printer.RequestExit("error_exit")
	}
}

func (gio *GCodeIO) doDebuginputExit() {
	for {
		all := true
		for _, r := range gio.printer.SendEventResults("gcode:debuginput_exit") {
			if b, ok := r.(bool); !ok || !b {
				all = false
			}
		}
		if all {
			break
		}
		gio.reactor.Pause(gio.reactor.Monotonic() + 0.010)
	}
	gio.gcode.RequestRestart("exit")
}

var m112RE = regexp.MustCompile(`^(?:[nN][0-9]+)?\s*[mM]112(?:\s|$)`)

func (gio *GCodeIO) processData(eventtime float64) {
	// Read input, separate by newline, and add to pending_commands
	buf := make([]byte, 4096)
	n, err := gio.fd.Read(buf)
	if err != nil && n == 0 && !errors.Is(err, io.EOF) {
		log.Printf("Read g-code: %v", err)
		return
	}
	if !utf8.Valid(buf[:n]) {
		log.Print("Read g-code: UnicodeDecodeError")
		return
	}
	data := string(buf[:n])
	gio.inputLog = append(gio.inputLog, inputBlock{eventtime, data})
	if len(gio.inputLog) > 50 {
		gio.inputLog = gio.inputLog[1:]
	}
	gio.bytesRead += len(data)
	lines := strings.Split(data, "\n")
	lines[0] = gio.partialInput + lines[0]
	gio.partialInput = lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	pendingCommands := append(gio.pendingCommands, lines...)
	gio.pendingCommands = pendingCommands
	gio.pipeIsActive = true
	// Special handling for debug file input EOF
	if data == "" && gio.isFileinput {
		gio.reactor.UnregisterFd(gio.fdHandle)
		gio.fdHandle = nil
		if !gio.isProcessingData {
			gio.doDebuginputExit()
		}
		return
	}
	// Handle case where multiple commands pending
	if len(pendingCommands) < 20 {
		// Check for M112 out-of-order
		for _, line := range lines {
			if m112RE.MatchString(line) {
				gio.gcode.CmdM112()
			}
		}
	}
	if gio.isProcessingData {
		if len(pendingCommands) >= 20 {
			// Stop reading input
			gio.reactor.UnregisterFd(gio.fdHandle)
			gio.fdHandle = nil
		}
		return
	}
	// Process commands
	gio.isProcessingData = true
	for len(pendingCommands) > 0 {
		gio.pendingCommands = nil
		gio.gcode.ProcessCommands(pendingCommands)
		pendingCommands = gio.pendingCommands
	}
	gio.isProcessingData = false
	if gio.fdHandle == nil {
		gio.fdHandle = gio.reactor.RegisterFd(gio.fd, gio.processData)
	}
}

// Stats is the number of G-code bytes read.
func (gio *GCodeIO) Stats(eventtime float64) (bool, string) {
	return false, fmt.Sprintf("gcodein=%d", gio.bytesRead)
}
