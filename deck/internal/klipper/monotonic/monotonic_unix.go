//go:build linux || darwin

// Package monotonic is the clock of the Klipper host: get_monotonic of chelper/pyhelper.c.
// Klipper reads CLOCK_MONOTONIC_RAW, which NTP does not slew. The monotonic clock of Go
// (CLOCK_MONOTONIC) changes its rate while timesyncd corrects the time, and then the clock
// synchronization with the MCU sees a wrong MCU frequency.
package monotonic

import "golang.org/x/sys/unix"

// Now returns the time in seconds of CLOCK_MONOTONIC_RAW.
func Now() float64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC_RAW, &ts); err != nil {
		panic(err) // pyhelper.c reports the error and returns 0; the clock exists on Linux
	}
	return float64(ts.Sec) + float64(ts.Nsec)*1e-9
}
