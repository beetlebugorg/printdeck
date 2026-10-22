package statistics

import (
	"syscall"
	"time"
)

// processTime is the CPU time of the process (time.process_time).
func processTime() float64 {
	var c, e, k, u syscall.Filetime
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	if err := syscall.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return 0
	}
	// A Filetime counts in units of 100 ns.
	ticks := func(t syscall.Filetime) int64 { return int64(t.HighDateTime)<<32 | int64(t.LowDateTime) }
	return (time.Duration(ticks(k)+ticks(u)) * 100).Seconds()
}
