package hostmetrics

import (
	"errors"
	"math"
	"os"
	"testing"
	"time"
	"unsafe"
)

// The files of a Raspberry Pi 5, cut down.
const (
	statA = "cpu  1000 0 500 8000 500 0 0 0 0 0\ncpu0 250 0 125 2000 125 0 0 0 0 0\nintr 1 2 3\n"
	// 400 more busy ticks and 600 more idle ticks: 40 % busy.
	statB = "cpu  1300 0 600 8500 600 0 0 0 0 0\ncpu0 325 0 150 2125 150 0 0 0 0 0\nintr 1 2 3\n"

	meminfo = "MemTotal:        4045000 kB\nMemFree:         3000000 kB\nMemAvailable:    3445000 kB\nBuffers:           10000 kB\n"

	diskA = `   1       0 ram0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
   7       0 loop0 10 0 20 0 0 0 0 0 0 0 0 0 0 0 0 0 0
 179       0 mmcblk0 5000 100 200000 3000 900 50 7200 4000 0 5000 7000 0 0 0 0 0 0
 179       1 mmcblk0p1 100 0 2000 50 0 0 0 0 0 50 50 0 0 0 0 0 0
 179       2 mmcblk0p2 4900 100 198000 2950 900 50 7200 4000 0 4950 6950 0 0 0 0 0 0
`
	// 5 more writes and 80 more sectors (40 KiB) on mmcblk0.
	diskB = `   1       0 ram0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0
   7       0 loop0 10 0 20 0 0 0 0 0 0 0 0 0 0 0 0 0 0
 179       0 mmcblk0 5000 100 200000 3000 905 50 7280 4010 0 5010 7010 0 0 0 0 0 0
 179       1 mmcblk0p1 100 0 2000 50 0 0 0 0 0 50 50 0 0 0 0 0 0
 179       2 mmcblk0p2 4900 100 198000 2950 905 50 7280 4010 0 4960 6960 0 0 0 0 0 0
`
)

// fakeProc is a reader of the kernel files from a map.
type fakeProc map[string]string

func (p fakeProc) read(name string) ([]byte, error) {
	s, ok := p[name]
	if !ok {
		return nil, errors.New("no file")
	}
	return []byte(s), nil
}

func TestParseStat(t *testing.T) {
	f := make([][]byte, 0, 24)
	total, idle, ok := parseStat([]byte(statA), f)
	if !ok || total != 10000 || idle != 8500 {
		t.Fatalf("parseStat = %d, %d, %v; want 10000, 8500, true", total, idle, ok)
	}
	if _, _, ok := parseStat([]byte("intr 1 2\n"), f); ok {
		t.Fatal("parseStat of a file with no cpu line is ok")
	}
	// The guest times are part of user: the total leaves them out.
	total, _, _ = parseStat([]byte("cpu  100 0 0 100 0 0 0 0 50 50\n"), f)
	if total != 200 {
		t.Fatalf("total with guest = %d; want 200", total)
	}
}

func TestParseMeminfo(t *testing.T) {
	f := make([][]byte, 0, 24)
	total, avail, ok := parseMeminfo([]byte(meminfo), f)
	if !ok || total != 4045000 || avail != 3445000 {
		t.Fatalf("parseMeminfo = %d, %d, %v", total, avail, ok)
	}
	if _, _, ok := parseMeminfo([]byte("MemTotal: 100 kB\n"), f); ok {
		t.Fatal("parseMeminfo with no MemAvailable is ok")
	}
}

func TestDiskWrites(t *testing.T) {
	f := make([][]byte, 0, 24)
	w, s, ok := diskWrites([]byte(diskA), []byte("mmcblk0"), f)
	if !ok || w != 900 || s != 7200 {
		t.Fatalf("mmcblk0 = %d writes, %d sectors, %v; want 900, 7200", w, s, ok)
	}
	if _, _, ok := diskWrites([]byte(diskA), []byte("sda"), f); ok {
		t.Fatal("a disk that is not in the file is ok")
	}
	if _, _, ok := diskWrites([]byte(diskA), nil, f); ok {
		t.Fatal("no disk is ok")
	}
}

func TestPickDisk(t *testing.T) {
	for _, c := range []struct{ stats, want string }{
		{diskA, "mmcblk0"},
		{"   7 0 loop0 1 0 0 0 0 0 0 0 0 0 0\n 259 0 nvme0n1 1 0 0 0 5 0 9 0 0 0 0\n 259 1 nvme0n1p1 1 0 0 0 5 0 9 0 0 0 0\n", "nvme0n1"},
		{" 8 1 sda1 1 0 0 0 0 0 0 0 0 0 0\n 8 0 sda 1 0 0 0 0 0 0 0 0 0 0\n", "sda"},
		{" 7 0 loop0 1 0 0 0 0 0 0 0 0 0 0\n", ""},
		{"", ""},
	} {
		if got := pickDisk([]byte(c.stats), nil); got != c.want {
			t.Errorf("pickDisk = %q; want %q", got, c.want)
		}
	}
}

func TestSample(t *testing.T) {
	p := fakeProc{"/proc/stat": statA, "/proc/meminfo": meminfo, "/proc/diskstats": diskA}
	r := newSized(10, 10)
	r.ReadFile = p.read
	t0 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	r.Sample(t0)
	if r.Len() != 0 {
		t.Fatal("the first read added a sample")
	}
	p["/proc/stat"], p["/proc/diskstats"] = statB, diskB
	r.Sample(t0.Add(FineInterval))
	b, ok := r.Last()
	if !ok {
		t.Fatal("no sample after two reads")
	}
	if math.Abs(b.CPU-0.4) > 1e-6 || math.Abs(b.CPUMax-0.4) > 1e-6 || !b.HasCPU {
		t.Errorf("CPU = %v, max %v; want 0.4", b.CPU, b.CPUMax)
	}
	if b.MemTotal != 4045000*1024 || b.MemUsed != 600000*1024 {
		t.Errorf("memory = %d of %d", b.MemUsed, b.MemTotal)
	}
	if b.Writes != 5 || b.WriteBytes != 80*512 || !b.HasDisk {
		t.Errorf("writes = %d, %d bytes", b.Writes, b.WriteBytes)
	}
	// 5 writes in 2 s are 150 writes a minute.
	if w := b.WritesPerMinute(); math.Abs(w-150) > 1e-9 {
		t.Errorf("WritesPerMinute = %v; want 150", w)
	}
	if r.Disk() != "mmcblk0" {
		t.Errorf("Disk = %q", r.Disk())
	}
}

func TestSampleCounterReset(t *testing.T) {
	p := fakeProc{"/proc/stat": statB, "/proc/meminfo": meminfo, "/proc/diskstats": diskB}
	r := newSized(10, 10)
	r.ReadFile = p.read
	t0 := time.Unix(1_800_000_000, 0)
	r.Sample(t0)
	p["/proc/stat"], p["/proc/diskstats"] = statA, diskA // the counters went down
	r.Sample(t0.Add(FineInterval))
	b, _ := r.Last()
	if b.HasCPU || b.HasDisk || !b.HasMem {
		t.Errorf("after a reset: HasCPU %v, HasDisk %v, HasMem %v; want false, false, true", b.HasCPU, b.HasDisk, b.HasMem)
	}
}

func TestNoProc(t *testing.T) {
	r := New()
	r.ReadFile = fakeProc{}.read
	if !r.Available() {
		t.Fatal("not available before a read")
	}
	now := time.Unix(1_800_000_000, 0)
	r.Sample(now)
	r.Sample(now.Add(FineInterval))
	if r.Available() {
		t.Fatal("available with no /proc files")
	}
	if _, ok := r.Last(); !ok {
		t.Fatal("no entry: the entry of a read with no data is in the buffer")
	}
	b := r.Total(now, now.Add(FineInterval))
	if b.HasCPU || b.HasMem || b.HasDisk {
		t.Errorf("a bucket has data with no /proc files: %+v", b)
	}
	if b.WritesPerMinute() != 0 {
		t.Error("a rate with no disk data")
	}
}

// The real files of this computer: a sample makes no allocations after the first reads.
func TestSampleAllocs(t *testing.T) {
	if _, err := os.Stat("/proc/stat"); err != nil {
		t.Skip("no /proc")
	}
	r := New()
	now := time.Now()
	r.Sample(now)
	r.Sample(now.Add(FineInterval))
	k := 2
	allocs := testing.AllocsPerRun(100, func() {
		r.Sample(now.Add(time.Duration(k) * FineInterval))
		k++
	})
	if allocs > 0 {
		t.Errorf("%v allocations a sample", allocs)
	}
	if !r.Available() || r.Len() == 0 {
		t.Error("no samples from /proc")
	}
}

// fill adds n fine entries, one each FineInterval up to end, with i writes in entry i.
func fill(r *Recorder, end time.Time, n int) {
	for i := 0; i < n; i++ {
		t := end.Add(-time.Duration(n-1-i) * FineInterval)
		r.mu.Lock()
		r.add(entry{t: t.UnixMilli(), span: uint32(FineInterval.Milliseconds()), writes: uint32(i), sectors: 8,
			cpu: 0.5, cpuMax: 0.5, memUsed: 100, memTotal: 400, flags: hasCPU | hasMem | hasDisk})
		r.mu.Unlock()
	}
}

func TestRingLimit(t *testing.T) {
	r := newSized(5, 3)
	end := time.Unix(1_800_000_000, 0)
	fill(r, end, 12)
	if r.Len() != 5 {
		t.Fatalf("Len = %d; want 5", r.Len())
	}
	// The buffer has the newest 5: writes 7 to 11.
	b := r.Total(end.Add(-10*time.Second), end)
	if b.Writes != 7+8+9+10+11 {
		t.Errorf("writes = %d; want %d", b.Writes, 7+8+9+10+11)
	}
	if b.Covered != 5*FineInterval {
		t.Errorf("covered = %v", b.Covered)
	}
	if FineKeep != 1800 || CoarseKeep != 1440 {
		t.Errorf("FineKeep %d, CoarseKeep %d; want an hour of 2 s and a day of minutes", FineKeep, CoarseKeep)
	}
}

// The fine samples of each minute go in one coarse entry: the sums of the writes and the
// averages.
func TestCoarse(t *testing.T) {
	r := New()
	end := time.Unix(1_800_000_000-1_800_000_000%60, 0) // the end of a minute
	fill(r, end, 90)                                    // 3 minutes
	// The first two minutes are in the coarse buffer. The third is the minute now.
	if r.coarse.n != 2 {
		t.Fatalf("%d coarse entries; want 2", r.coarse.n)
	}
	sum := func(from, to int) (s uint32) {
		for i := from; i <= to; i++ {
			s += uint32(i)
		}
		return s
	}
	for k, want := range []uint32{sum(0, 29), sum(30, 59)} {
		e := r.coarse.at(k)
		if e.writes != want || e.sectors != 30*8 || e.span != 60000 || e.cpu != 0.5 || e.memUsed != 100 {
			t.Errorf("coarse entry %d = %+v; want %d writes", k, e, want)
		}
		if e.t != end.Add(time.Duration(k-2)*time.Minute).UnixMilli() {
			t.Errorf("coarse entry %d ends at %v", k, time.UnixMilli(e.t))
		}
	}
	// A day uses the coarse buffer and the minute now: all the writes.
	day := r.Window(end.Add(-24*time.Hour), end, 96)
	var writes uint64
	for _, b := range day {
		writes += b.Writes
	}
	if writes != uint64(sum(0, 89)) || day[95].Covered != 3*time.Minute {
		t.Errorf("day: %d writes, last bucket covers %v; want %d, 3m", writes, day[95].Covered, sum(0, 89))
	}
}

// A coarse entry keeps the largest CPU of its fine samples.
func TestCoarseMax(t *testing.T) {
	r := New()
	end := time.Unix(1_800_000_000-1_800_000_000%60, 0)
	for i := 0; i < 60; i++ {
		c := float32(0.1)
		if i == 10 {
			c = 0.9
		}
		r.mu.Lock()
		r.add(entry{t: end.Add(time.Duration(i-59) * FineInterval).UnixMilli(), span: 2000, cpu: c, cpuMax: c, flags: hasCPU})
		r.mu.Unlock()
	}
	b := r.Total(end.Add(-2*time.Hour), end.Add(time.Minute))
	if math.Abs(b.CPUMax-0.9) > 1e-6 || math.Abs(b.CPU-(0.1*59+0.9)/60) > 1e-6 {
		t.Errorf("CPU %v, max %v", b.CPU, b.CPUMax)
	}
}

func TestWindow(t *testing.T) {
	r := New()
	end := time.Unix(1_800_000_000, 0)
	fill(r, end, 60) // 2 minutes
	ws := r.Window(end.Add(-2*time.Minute), end, 2)
	if len(ws) != 2 {
		t.Fatalf("%d buckets", len(ws))
	}
	// A sample at the end of a bucket is in that bucket: 30 samples in each.
	for i, b := range ws {
		if b.Covered != time.Minute {
			t.Errorf("bucket %d covers %v; want 1m", i, b.Covered)
		}
	}
	var w0, w1 uint64
	for i := 0; i < 30; i++ {
		w0, w1 = w0+uint64(i), w1+uint64(i+30)
	}
	if ws[0].Writes != w0 || ws[1].Writes != w1 {
		t.Errorf("writes = %d, %d; want %d, %d", ws[0].Writes, ws[1].Writes, w0, w1)
	}
	if got := ws[1].WritesPerMinute(); got != float64(w1) {
		t.Errorf("WritesPerMinute = %v; want %d", got, w1)
	}
	if got := ws[1].BytesPerMinute(); got != 30*8*512 {
		t.Errorf("BytesPerMinute = %v; want %d", got, 30*8*512)
	}
	if ws[0].CPU != 0.5 || ws[0].MemUsed != 100*1024 || ws[0].MemTotal != 400*1024 {
		t.Errorf("bucket 0 = %+v", ws[0])
	}
	// A bucket with half its time: the rate of the time that it has data for.
	half := r.Window(end.Add(-6*time.Second), end.Add(6*time.Second), 1)[0]
	if half.Covered != 6*time.Second || half.WritesPerMinute() != 10*(57+58+59) {
		t.Errorf("half bucket: covered %v, %v writes a minute", half.Covered, half.WritesPerMinute())
	}
	// No samples: an empty bucket.
	empty := r.Total(end.Add(time.Hour), end.Add(2*time.Hour))
	if empty.HasDisk || empty.Covered != 0 || empty.WritesPerMinute() != 0 {
		t.Errorf("empty bucket = %+v", empty)
	}
}

func TestDemo(t *testing.T) {
	now := time.Date(2026, 10, 20, 14, 24, 0, 0, time.UTC)
	r := Demo(now)
	if r.Len() != FineKeep || r.coarse.n < CoarseKeep-1 {
		t.Fatalf("Len = %d, %d coarse", r.Len(), r.coarse.n)
	}
	day := r.Total(now.Add(-24*time.Hour), now)
	if !day.HasCPU || !day.HasMem || !day.HasDisk || day.Writes == 0 || day.Covered < 24*time.Hour-time.Minute {
		t.Errorf("day = %+v", day)
	}
}

// The two buffers stay small: 40 bytes an entry.
func TestSize(t *testing.T) {
	r := New()
	size := uintptr(len(r.fine.e)+len(r.coarse.e)) * unsafe.Sizeof(entry{})
	t.Logf("entry %d bytes, buffers %d bytes", unsafe.Sizeof(entry{}), size)
	if unsafe.Sizeof(entry{}) != 40 || size > 130_000 {
		t.Errorf("entry %d bytes, buffers %d bytes", unsafe.Sizeof(entry{}), size)
	}
}
