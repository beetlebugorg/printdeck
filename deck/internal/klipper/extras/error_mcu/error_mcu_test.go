package error_mcu

import (
	"strings"
	"testing"

	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// A board shutdown message gets the board name, the clarify text, the hint, and the restart text.
func TestShutdownMessage(t *testing.T) {
	p := host.New(reactor.New(), host.StartArgs{})
	p.InvokeShutdown("MCU shutdown", map[string]any{})
	e := &PrinterMCUError{printer: p, clarifyCallbacks: map[string][]ClarifyFunc{}}
	e.AddClarify("Timer too close", func(string, map[string]any) string { return "More text" })
	e.handleAnalyzeShutdown("MCU shutdown", map[string]any{"mcu": "mcu", "reason": "Timer too close",
		"event_type": "shutdown"})
	msg, kind := p.StateMessage()
	want := "MCU 'mcu' shutdown: Timer too close\n\nMore text\n" + errorHint("Timer too close") + messageShutdown
	if kind != "shutdown" || msg != want {
		t.Errorf("message %q (%s)\nwant %q", msg, kind, want)
	}
	if !strings.HasPrefix(errorHint("Stepper too far in past x"), "\nThis generally occurs when the micro") {
		t.Error("no hint for the second prefix")
	}
}
