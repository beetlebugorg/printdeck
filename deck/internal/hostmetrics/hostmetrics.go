// Package hostmetrics records the CPU use, the memory use and the writes to the disk of the
// host, for the System monitor page. The samples stay in memory, in a ring buffer of a fixed
// size. The package writes no files and no log lines.
package hostmetrics

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Interval is the time between two samples. Keep is the number of samples in the buffer: 24
// hours at Interval.
const (
	Interval = 10 * time.Second
	Keep     = int(24 * time.Hour / Interval)
)

// sectorSize is the size of a sector in /proc/diskstats. The kernel always counts 512 bytes.
const sectorSize = 512

// entry is one sample in the buffer: the values over the interval that ends at t. The fields are
// small, so the buffer of a day uses about 340 KiB.
type entry struct {
	t        int64   // the end of the interval, Unix milliseconds
	span     uint32  // the length of the interval, milliseconds
	cpu      float32 // the busy share of the CPU, 0 to 1
	memUsed  uint32  // KiB
	memTotal uint32  // KiB
	writes   uint32  // the write operations that completed in the interval
	sectors  uint32  // the sectors written in the interval
	flags    uint8   // hasCPU, hasMem, hasDisk
}

const (
	hasCPU uint8 = 1 << iota
	hasMem
	hasDisk
)

// counters are the totals of the kernel at one read.
type counters struct {
	at                time.Time
	cpuTotal, cpuIdle uint64
	cpuOK             bool
	writes, sectors   uint64
	diskOK            bool
}

// Recorder samples the host at Interval, and keeps the samples of the last day.
type Recorder struct {
	// ReadFile reads a file of the kernel. Tests replace it. Nil is os.ReadFile.
	ReadFile func(name string) ([]byte, error)

	mu    sync.Mutex
	ring  []entry
	next  int // the index of the next entry
	n     int // the number of entries
	prev  counters
	read  bool   // a read found /proc/stat or /proc/meminfo
	tried bool   // a read happened
	disk  string // the disk in /proc/diskstats, for example mmcblk0
}

// New returns a recorder with room for keep samples.
func New(keep int) *Recorder {
	return &Recorder{ring: make([]entry, max(1, keep))}
}

// Run samples the host at Interval until ctx ends. The first read is the base of the first
// sample.
func (r *Recorder) Run(ctx context.Context) {
	r.Sample(time.Now())
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			r.Sample(now)
		}
	}
}

func (r *Recorder) readFile(name string) string {
	read := r.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	b, err := read(name)
	if err != nil {
		return ""
	}
	return string(b)
}

// Sample reads the kernel files at the time now. It adds an entry with the change since the
// previous read. The first read only sets the base.
func (r *Recorder) Sample(now time.Time) {
	stat := r.readFile("/proc/stat")
	meminfo := r.readFile("/proc/meminfo")
	diskstats := r.readFile("/proc/diskstats")

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tried = true
	c := counters{at: now}
	c.cpuTotal, c.cpuIdle, c.cpuOK = parseStat(stat)
	memTotal, memAvail, memOK := parseMeminfo(meminfo)
	disks := parseDiskstats(diskstats)
	if _, ok := disks[r.disk]; !ok {
		r.disk = pickDisk(diskstats)
	}
	if d, ok := disks[r.disk]; ok {
		c.writes, c.sectors, c.diskOK = d.writes, d.sectors, true
	}
	if c.cpuOK || memOK {
		r.read = true
	}
	p := r.prev
	r.prev = c
	if p.at.IsZero() {
		return
	}
	span := now.Sub(p.at)
	if span <= 0 {
		return
	}
	e := entry{t: now.UnixMilli(), span: uint32(min(span.Milliseconds(), math.MaxUint32))}
	if c.cpuOK && p.cpuOK && c.cpuTotal > p.cpuTotal && c.cpuIdle >= p.cpuIdle {
		total, idle := c.cpuTotal-p.cpuTotal, c.cpuIdle-p.cpuIdle
		e.cpu = float32(1 - float64(min(idle, total))/float64(total))
		e.flags |= hasCPU
	}
	if memOK {
		e.memTotal = uint32(min(memTotal, math.MaxUint32))
		e.memUsed = uint32(min(memTotal-min(memAvail, memTotal), math.MaxUint32))
		e.flags |= hasMem
	}
	// A counter that went down is a new disk or a reset: the interval has no disk data.
	if c.diskOK && p.diskOK && c.writes >= p.writes && c.sectors >= p.sectors {
		e.writes = uint32(min(c.writes-p.writes, math.MaxUint32))
		e.sectors = uint32(min(c.sectors-p.sectors, math.MaxUint32))
		e.flags |= hasDisk
	}
	r.add(e)
}

// add puts e in the buffer. When the buffer is full, e replaces the oldest entry. The caller
// holds mu.
func (r *Recorder) add(e entry) {
	r.ring[r.next] = e
	r.next = (r.next + 1) % len(r.ring)
	r.n = min(r.n+1, len(r.ring))
}

// Len is the number of samples in the buffer.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// Disk is the name of the disk that the recorder counts, for example "mmcblk0", or "".
func (r *Recorder) Disk() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disk
}

// Available is false when a read found no /proc/stat and no /proc/meminfo, as on a computer
// that is not Linux.
func (r *Recorder) Available() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.tried || r.read
}

// Bucket is the samples of a part of the time: the averages of the CPU and the memory, and the
// sums of the writes.
type Bucket struct {
	Start, End time.Time
	// Covered is the time of the samples in the bucket. The rates use it, so a bucket with a gap
	// has the rate of the time that it has data for.
	Covered            time.Duration
	CPU                float64 // the busy share, 0 to 1
	MemUsed, MemTotal  uint64  // bytes
	Writes, WriteBytes uint64  // the write operations and the bytes written
	HasCPU, HasMem     bool
	HasDisk            bool
	diskCovered        time.Duration
}

// WritesPerMinute is the rate of the write operations in the bucket.
func (b Bucket) WritesPerMinute() float64 { return perMinute(float64(b.Writes), b.diskCovered) }

// BytesPerMinute is the rate of the bytes written in the bucket.
func (b Bucket) BytesPerMinute() float64 { return perMinute(float64(b.WriteBytes), b.diskCovered) }

func perMinute(v float64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return v * float64(time.Minute) / float64(d)
}

// Window divides the time from from to to into n buckets of the same length, oldest first. A
// sample goes in the bucket that has the end of its interval.
func (r *Recorder) Window(from, to time.Time, n int) []Bucket {
	n = max(1, n)
	out := make([]Bucket, n)
	w := to.Sub(from) / time.Duration(n)
	for i := range out {
		out[i].Start = from.Add(time.Duration(i) * w)
		out[i].End = out[i].Start.Add(w)
	}
	if w <= 0 {
		return out
	}
	type sums struct{ cpu, cpuT, mem, memT float64 }
	acc := make([]sums, n)
	f, t := from.UnixMilli(), to.UnixMilli()
	r.mu.Lock()
	for k := 0; k < r.n; k++ {
		e := r.ring[(r.next-r.n+k+len(r.ring))%len(r.ring)]
		if e.t <= f || e.t > t {
			continue
		}
		// The end of a bucket belongs to that bucket.
		i := min(n-1, int((time.Duration(e.t-f)*time.Millisecond-1)/w))
		b, s := &out[i], &acc[i]
		span := time.Duration(e.span) * time.Millisecond
		b.Covered += span
		if e.flags&hasCPU != 0 {
			s.cpu += float64(e.cpu) * span.Seconds()
			s.cpuT += span.Seconds()
			b.HasCPU = true
		}
		if e.flags&hasMem != 0 {
			s.mem += float64(e.memUsed)
			s.memT++
			b.MemTotal = uint64(e.memTotal) * 1024
			b.HasMem = true
		}
		if e.flags&hasDisk != 0 {
			b.Writes += uint64(e.writes)
			b.WriteBytes += uint64(e.sectors) * sectorSize
			b.diskCovered += span
			b.HasDisk = true
		}
	}
	r.mu.Unlock()
	for i := range out {
		if s := acc[i]; s.cpuT > 0 {
			out[i].CPU = s.cpu / s.cpuT
		}
		if s := acc[i]; s.memT > 0 {
			out[i].MemUsed = uint64(s.mem/s.memT+0.5) * 1024
		}
	}
	return out
}

// Total is the time from from to to in one bucket.
func (r *Recorder) Total(from, to time.Time) Bucket { return r.Window(from, to, 1)[0] }

// Last is the newest sample, and false when the buffer is empty.
func (r *Recorder) Last() (Bucket, bool) {
	r.mu.Lock()
	if r.n == 0 {
		r.mu.Unlock()
		return Bucket{}, false
	}
	e := r.ring[(r.next-1+len(r.ring))%len(r.ring)]
	r.mu.Unlock()
	end := time.UnixMilli(e.t)
	return r.Total(end.Add(-time.Duration(e.span)*time.Millisecond), end), true
}

// parseStat returns the total and the idle time of all CPUs from /proc/stat, in clock ticks. The
// idle time includes the time that waits for I/O. The guest times are part of the user times, so
// the total leaves them out.
func parseStat(s string) (total, idle uint64, ok bool) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		for i, v := range f[1:min(len(f), 9)] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
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
func parseMeminfo(s string) (total, avail uint64, ok bool) {
	var haveT, haveA bool
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, haveT = n, true
		case "MemAvailable:":
			avail, haveA = n, true
		}
	}
	return total, avail, haveT && haveA && total > 0
}

// diskCounts are the write counters of one device in /proc/diskstats.
type diskCounts struct{ writes, sectors uint64 }

// parseDiskstats returns the write counters of each device in /proc/diskstats. Field 8 is the
// write operations that completed, and field 10 the sectors written.
func parseDiskstats(s string) map[string]diskCounts {
	m := map[string]diskCounts{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		w, err1 := strconv.ParseUint(f[7], 10, 64)
		sec, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m[f[2]] = diskCounts{w, sec}
	}
	return m
}

// virtualDisks are the prefixes of the devices that are not a disk.
var virtualDisks = []string{"loop", "ram", "zram", "dm-", "md", "sr", "nbd", "fd"}

// pickDisk returns the disk to count from /proc/diskstats: the SD card mmcblk0 when it is there,
// else the first whole disk. A partition is a device whose name is the name of another device
// and a number, with or without a "p" (mmcblk0p2, sda1, nvme0n1p1).
func pickDisk(s string) string {
	var names []string
	for _, line := range strings.Split(s, "\n") {
		if f := strings.Fields(line); len(f) >= 10 {
			names = append(names, f[2])
		}
	}
	isPart := func(name string) bool {
		for _, o := range names {
			if o == name || !strings.HasPrefix(name, o) {
				continue
			}
			rest := strings.TrimPrefix(name[len(o):], "p")
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
			if strings.HasPrefix(n, v) {
				virtual = true
			}
		}
		if first == "" && !virtual && !isPart(n) {
			first = n
		}
	}
	return first
}
