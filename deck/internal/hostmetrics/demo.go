package hostmetrics

import (
	"math"
	"time"
)

// Demo returns a recorder with a day of made-up samples up to now, for the pictures of the
// manual. The values are the same at each call: a quiet deck with a print in the last hours.
func Demo(now time.Time) *Recorder {
	r := New()
	r.read, r.tried, r.disk = true, true, "mmcblk0"
	const memTotal = 4045 * 1024 // KiB: a Raspberry Pi 5 with 4 GB
	n := int(24 * time.Hour / FineInterval)
	end := now.Truncate(FineInterval)
	for k := n - 1; k >= 0; k-- {
		t := end.Add(-time.Duration(k) * FineInterval)
		ago := now.Sub(t)
		x := float64(k) / 5 // the curves of 10 seconds
		// A print runs from 3 hours ago until 1 hour ago: more CPU, more memory.
		printing := ago > time.Hour && ago < 3*time.Hour
		cpu := 0.06 + 0.03*math.Sin(x/7) + 0.02*math.Sin(x/2.3) + 0.02*math.Sin(float64(k)*1.7)
		mem := 610.0 + 25*math.Sin(x/300)
		if printing {
			cpu += 0.18 + 0.04*math.Sin(x/3.1)
			mem += 140
		}
		// The journal writes about once a minute, and the history of the print at its end.
		var writes, sectors uint32
		if k%30 == 0 {
			writes, sectors = 2, 16
		}
		if (k+57)%185 == 0 {
			writes, sectors = writes+3, sectors+24
		}
		if ago >= time.Hour && ago < time.Hour+FineInterval {
			writes, sectors = writes+14, sectors+180
		}
		c := float32(max(0, cpu))
		r.add(entry{t: t.UnixMilli(), span: uint32(FineInterval.Milliseconds()), cpu: c, cpuMax: c,
			memUsed: uint32(mem * 1024), memTotal: memTotal, writes: writes, sectors: sectors,
			flags: hasCPU | hasMem | hasDisk})
	}
	return r
}
