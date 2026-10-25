package hostmetrics

import (
	"bytes"
	"strconv"
)

// The parsers of the kernel files. They work on the bytes of a file with a buffer of fields from
// the caller, so a sample makes no allocations.

// nextLine returns the first line of b and the rest after it.
func nextLine(b []byte) (line, rest []byte) {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i], b[i+1:]
	}
	return b, nil
}

// splitFields puts the fields of line, between spaces and tabs, in out[:0], and returns it.
func splitFields(line []byte, out [][]byte) [][]byte {
	out = out[:0]
	start := -1
	for i, c := range line {
		if c == ' ' || c == '\t' {
			if start >= 0 {
				out = append(out, line[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, line[start:])
	}
	return out
}

// parseUint is a decimal number with no sign.
func parseUint(b []byte) (uint64, bool) {
	if len(b) == 0 || len(b) > 20 {
		return 0, false
	}
	var n uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

// parseStat returns the total and the idle time of all CPUs from /proc/stat, in clock ticks. The
// idle time includes the time that waits for I/O. The guest times are part of the user times, so
// the total leaves them out.
func parseStat(b []byte, fields [][]byte) (total, idle uint64, ok bool) {
	for len(b) > 0 {
		var line []byte
		line, b = nextLine(b)
		f := splitFields(line, fields)
		if len(f) < 5 || string(f[0]) != "cpu" {
			continue
		}
		for i, v := range f[1:min(len(f), 9)] {
			n, ok := parseUint(v)
			if !ok {
				return 0, 0, false
			}
			total += n
			if i == 3 || i == 4 { // idle, iowait
				idle += n
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

// parseMeminfo returns MemTotal and MemAvailable from /proc/meminfo, in KiB.
func parseMeminfo(b []byte, fields [][]byte) (total, avail uint64, ok bool) {
	var haveT, haveA bool
	for len(b) > 0 && !(haveT && haveA) {
		var line []byte
		line, b = nextLine(b)
		f := splitFields(line, fields)
		if len(f) < 2 {
			continue
		}
		switch string(f[0]) {
		case "MemTotal:":
			total, haveT = parseUint(f[1])
		case "MemAvailable:":
			avail, haveA = parseUint(f[1])
		}
	}
	return total, avail, haveT && haveA && total > 0
}

// diskWrites returns the write counters of the device disk in /proc/diskstats: field 8 is the
// write operations that completed, and field 10 the sectors written.
func diskWrites(b, disk []byte, fields [][]byte) (writes, sectors uint64, ok bool) {
	if len(disk) == 0 {
		return 0, 0, false
	}
	for len(b) > 0 {
		var line []byte
		line, b = nextLine(b)
		f := splitFields(line, fields)
		if len(f) < 10 || !bytes.Equal(f[2], disk) {
			continue
		}
		w, ok1 := parseUint(f[7])
		s, ok2 := parseUint(f[9])
		return w, s, ok1 && ok2
	}
	return 0, 0, false
}

// virtualDisks are the prefixes of the devices that are not a disk.
var virtualDisks = []string{"loop", "ram", "zram", "dm-", "md", "sr", "nbd", "fd"}

// pickDisk returns the disk to count from /proc/diskstats: the SD card mmcblk0 when it is there,
// else the first whole disk. A partition is a device whose name is the name of another device
// and a number, with or without a "p" (mmcblk0p2, sda1, nvme0n1p1).
func pickDisk(b []byte, fields [][]byte) string {
	var names []string
	for len(b) > 0 {
		var line []byte
		line, b = nextLine(b)
		if f := splitFields(line, fields); len(f) >= 10 {
			names = append(names, string(f[2]))
		}
	}
	isPart := func(name string) bool {
		for _, o := range names {
			if len(name) <= len(o) || name[:len(o)] != o {
				continue
			}
			rest := name[len(o):]
			if rest[0] == 'p' {
				rest = rest[1:]
			}
			if _, err := strconv.Atoi(rest); err == nil && rest != "" {
				return true
			}
		}
		return false
	}
	first := ""
	for _, n := range names {
		if n == "mmcblk0" {
			return n
		}
		virtual := false
		for _, v := range virtualDisks {
			if len(n) >= len(v) && n[:len(v)] == v {
				virtual = true
			}
		}
		if first == "" && !virtual && !isPart(n) {
			first = n
		}
	}
	return first
}
