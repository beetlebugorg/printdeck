package monotonic

import "time"

// start is the zero of the clock. The clock starts at 1000 s, so no time is near 0: the code of
// Klipper uses 0 for "now".
var start = time.Now().Add(-1000 * time.Second)

// Now returns the time in seconds of the monotonic clock of Go. On Windows, Go reads the
// interrupt time, which the time service does not slew.
func Now() float64 { return time.Since(start).Seconds() }
