package system

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"printdeck/internal/klippy"
)

// KlippySocket is the API socket of the Klipper host. Before a restart into an update, the helper
// asks it for the state of the print.
var KlippySocket = "/run/klipper/klippy.sock"

// errPrinting is the error of a restart during a print.
var errPrinting = errors.New("A print runs. The Printdeck restarts into the update after the print.")

// printing reports that the Klipper host prints or pauses a print. With no socket, or no process
// on it, no Klipper host runs. A host that does not answer in time counts as a print: it can
// still move the printer.
func printing() bool {
	if _, err := os.Stat(KlippySocket); errors.Is(err, os.ErrNotExist) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := klippy.Dial(ctx, KlippySocket)
	if err != nil {
		return !errors.Is(err, syscall.ECONNREFUSED)
	}
	defer c.Close()
	st, err := c.Query(ctx, map[string][]string{"print_stats": {"state"}})
	if err != nil {
		// An error answer: the host runs with no print objects, for example after a config error.
		var ke *klippy.Error
		return !errors.As(err, &ke)
	}
	s, _ := st["print_stats"]["state"].(string)
	return s == "printing" || s == "paused"
}
