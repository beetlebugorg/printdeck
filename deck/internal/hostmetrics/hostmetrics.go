// Package hostmetrics records the CPU use, the memory use and the writes to the disk of the
// host, for the System monitor page. The samples stay in memory, in two ring buffers of a fixed
// size: a sample every 2 seconds for the last hour, and a sample a minute for the last day. The
// package writes no files and no log lines.
package hostmetrics

import (
	"context"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

// FineInterval is the time between two samples, and FineKeep the number of samples in the fine
// buffer: 1 hour. CoarseInterval is the time of a sample of the coarse buffer, and CoarseKeep
// the number of these samples: 24 hours.
const (
	FineInterval   = 2 * time.Second
	FineKeep       = int(time.Hour / FineInterval)
	CoarseInterval = time.Minute
	CoarseKeep     = int(24 * time.Hour / CoarseInterval)
)

// sectorSize is the size of a sector in /proc/diskstats. The kernel always counts 512 bytes.
const sectorSize = 512

// entry is one sample in a buffer: the values over the interval that ends at t. The fields are
// small: an entry is 40 bytes, and the two buffers are about 127 KiB.
type entry struct {
	t        int64   // the end of the interval, Unix milliseconds
	span     uint32  // the time of the samples in the interval, milliseconds
	cpu      float32 // the busy share of the CPU, 0 to 1
	cpuMax   float32 // the largest busy share of a fine sample in the interval
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

// ring is a buffer of entries of a fixed size. When it is full, a new entry replaces the oldest.
type ring struct {
	e    []entry
	next int // the index of the next entry
	n    int // the number of entries
}

func newRing(size int) ring { return ring{e: make([]entry, max(1, size))} }

func (g *ring) add(e entry) {
	g.e[g.next] = e
	g.next = (g.next + 1) % len(g.e)
	g.n = min(g.n+1, len(g.e))
}

// at is entry k, from the oldest.
func (g *ring) at(k int) entry { return g.e[(g.next-g.n+k+len(g.e))%len(g.e)] }

// minuteSum is the sums of the fine samples of one minute, for an entry of the coarse buffer.
type minuteSum struct {
	end             int64 // the end of the minute, Unix milliseconds
	span            int64 // ms
	cpu, cpuSpan    float64
	cpuMax          float32
	mem             float64
	memN            int
	memTotal        uint32
	writes, sectors uint64
	flags           uint8
}

func (s *minuteSum) add(e entry) {
	s.span += int64(e.span)
	s.flags |= e.flags
	if e.flags&hasCPU != 0 {
		s.cpu += float64(e.cpu) * float64(e.span)
		s.cpuSpan += float64(e.span)
		s.cpuMax = max(s.cpuMax, e.cpuMax)
	}
	if e.flags&hasMem != 0 {
		s.mem += float64(e.memUsed)
		s.memN++
		s.memTotal = e.memTotal
	}
	if e.flags&hasDisk != 0 {
		s.writes += uint64(e.writes)
		s.sectors += uint64(e.sectors)
	}
}

// entry is the coarse entry of the sums: the averages of the CPU and the memory, the largest
// CPU, and the sums of the writes.
func (s *minuteSum) entry() entry {
	e := entry{t: s.end, span: uint32(min(s.span, math.MaxUint32)), cpuMax: s.cpuMax, memTotal: s.memTotal,
		writes: uint32(min(s.writes, math.MaxUint32)), sectors: uint32(min(s.sectors, math.MaxUint32)), flags: s.flags}
	if s.cpuSpan > 0 {
		e.cpu = float32(s.cpu / s.cpuSpan)
	}
	if s.memN > 0 {
		e.memUsed = uint32(s.mem/float64(s.memN) + 0.5)
	}
	return e
}

// counters are the totals of the kernel at one read.
type counters struct {
	at                time.Time
	cpuTotal, cpuIdle uint64
	cpuOK             bool
	writes, sectors   uint64
	diskOK            bool
}

// procFile is a file of the kernel that stays open. A read of it from offset 0 gets the values
// of now.
type procFile struct {
	name string
	f    *os.File
}

// Recorder samples the host at FineInterval. It keeps the samples of the last hour, and a sample
// a minute for the last day.
type Recorder struct {
	// ReadFile reads a file of the kernel. Tests replace it. Nil reads the files of the system.
	ReadFile func(name string) ([]byte, error)

	mu     sync.Mutex
	fine   ring
	coarse ring
	minute minuteSum // the sums of the minute now, for the next coarse entry
	prev   counters
	read   bool   // a read found /proc/stat or /proc/meminfo
	tried  bool   // a read happened
	disk   string // the disk in /proc/diskstats, for example mmcblk0
	diskB  []byte // disk as bytes, for the search in /proc/diskstats
	files  [3]procFile
	buf    []byte   // the content of a file
	fields [][]byte // the fields of a line
}

// New returns a recorder with buffers for 1 hour of fine samples and 24 hours of coarse
// samples.
func New() *Recorder { return newSized(FineKeep, CoarseKeep) }

func newSized(fine, coarse int) *Recorder {
	return &Recorder{fine: newRing(fine), coarse: newRing(coarse), buf: make([]byte, 8192), fields: make([][]byte, 0, 24),
		files: [3]procFile{{name: "/proc/stat"}, {name: "/proc/meminfo"}, {name: "/proc/diskstats"}}}
}

// Run samples the host at FineInterval until ctx ends. The first read is the base of the first
// sample.
func (r *Recorder) Run(ctx context.Context) {
	r.Sample(time.Now())
	t := time.NewTicker(FineInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			for i := range r.files {
				if r.files[i].f != nil {
					r.files[i].f.Close()
					r.files[i].f = nil
				}
			}
			r.mu.Unlock()
			return
		case now := <-t.C:
			r.Sample(now)
		}
	}
}

// readFile reads file i of r.files, and returns its content or nil. The content is valid until
// the next read. The caller holds mu.
func (r *Recorder) readFile(i int) []byte {
	p := &r.files[i]
	if r.ReadFile != nil {
		b, err := r.ReadFile(p.name)
		if err != nil {
			return nil
		}
		return b
	}
	if p.f == nil {
		f, err := os.Open(p.name)
		if err != nil {
			return nil
		}
		p.f = f
	}
	for {
		n, err := p.f.ReadAt(r.buf, 0)
		if err != nil && err != io.EOF {
			p.f.Close()
			p.f = nil
			return nil
		}
		if n < len(r.buf) {
			return r.buf[:n]
		}
		// The file fills the buffer: read it again into a larger buffer.
		r.buf = make([]byte, 2*len(r.buf))
	}
}

// Sample reads the kernel files at the time now. It adds an entry with the change since the
// previous read. The first read only sets the base.
func (r *Recorder) Sample(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tried = true
	c := counters{at: now}
	c.cpuTotal, c.cpuIdle, c.cpuOK = parseStat(r.readFile(0), r.fields)
	memTotal, memAvail, memOK := parseMeminfo(r.readFile(1), r.fields)
	stats := r.readFile(2)
	w, s, ok := diskWrites(stats, r.diskB, r.fields)
	if !ok {
		r.disk = pickDisk(stats, r.fields)
		r.diskB = []byte(r.disk)
		w, s, ok = diskWrites(stats, r.diskB, r.fields)
	}
	c.writes, c.sectors, c.diskOK = w, s, ok
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
		e.cpuMax = e.cpu
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

// add puts a fine entry in the fine buffer, and adds it to the sums of its minute. At the first
// entry of a new minute, the sums of the minute before go in the coarse buffer. The caller holds
// mu.
func (r *Recorder) add(e entry) {
	r.fine.add(e)
	const m = int64(CoarseInterval / time.Millisecond)
	end := (e.t + m - 1) / m * m
	if end != r.minute.end {
		if r.minute.span > 0 {
			r.coarse.add(r.minute.entry())
		}
		r.minute = minuteSum{end: end}
	}
	r.minute.add(e)
}

// Len is the number of fine samples.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fine.n
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

// Bucket is the samples of a part of the time: the averages of the CPU and the memory, the
// largest CPU, and the sums of the writes.
type Bucket struct {
	Start, End time.Time
	// Covered is the time of the samples in the bucket. The rates use it, so a bucket with a gap
	// has the rate of the time that it has data for.
	Covered            time.Duration
	CPU, CPUMax        float64 // the busy share, 0 to 1
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
// sample goes in the bucket that has the end of its interval. A time up to the length of the
// fine buffer uses the fine samples. A longer time uses the coarse samples and the sums of the
// minute now.
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
	put := func(e entry) {
		if e.t <= f || e.t > t {
			return
		}
		// The end of a bucket belongs to that bucket.
		i := min(n-1, int((time.Duration(e.t-f)*time.Millisecond-1)/w))
		b, s := &out[i], &acc[i]
		span := time.Duration(e.span) * time.Millisecond
		b.Covered += span
		if e.flags&hasCPU != 0 {
			s.cpu += float64(e.cpu) * span.Seconds()
			s.cpuT += span.Seconds()
			b.CPUMax = max(b.CPUMax, float64(e.cpuMax))
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
	r.mu.Lock()
	if to.Sub(from) <= time.Duration(len(r.fine.e))*FineInterval {
		for k := 0; k < r.fine.n; k++ {
			put(r.fine.at(k))
		}
	} else {
		for k := 0; k < r.coarse.n; k++ {
			put(r.coarse.at(k))
		}
		if r.minute.span > 0 {
			// The minute now: its sums so far, at the time of its last sample.
			e := r.minute.entry()
			e.t = min(e.t, r.fine.at(r.fine.n-1).t)
			put(e)
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

// Last is the newest sample, and false when there is no sample.
func (r *Recorder) Last() (Bucket, bool) {
	r.mu.Lock()
	if r.fine.n == 0 {
		r.mu.Unlock()
		return Bucket{}, false
	}
	e := r.fine.at(r.fine.n - 1)
	r.mu.Unlock()
	end := time.UnixMilli(e.t)
	return r.Total(end.Add(-time.Duration(e.span)*time.Millisecond), end), true
}
