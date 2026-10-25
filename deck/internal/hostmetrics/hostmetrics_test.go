package hostmetrics

import (
	"errors"
	"math"
	"testing"
	"time"
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
	total, idle, ok := parseStat(statA)
	if !ok || total != 10000 || idle != 8500 {
		t.Fatalf("parseStat = %d, %d, %v; want 10000, 8500, true", total, idle, ok)
	}
	if _, _, ok := parseStat("intr 1 2\n"); ok {
		t.Fatal("parseStat of a file with no cpu line is ok")
	}
	// The guest times are part of user: the total leaves them out.
	total, _, _ = parseStat("cpu  100 0 0 100 0 0 0 0 50 50\n")
	if total != 200 {
		t.Fatalf("total with guest = %d; want 200", total)
	}
}

func TestParseMeminfo(t *testing.T) {
	total, avail, ok := parseMeminfo(meminfo)
	if !ok || total != 4045000 || avail != 3445000 {
		t.Fatalf("parseMeminfo = %d, %d, %v", total, avail, ok)
	}
	if _, _, ok := parseMeminfo("MemTotal: 100 kB\n"); ok {
		t.Fatal("parseMeminfo with no MemAvailable is ok")
	}
}

func TestParseDiskstats(t *testing.T) {
	m := parseDiskstats(diskA)
	if d := m["mmcblk0"]; d.writes != 900 || d.sectors != 7200 {
		t.Fatalf("mmcblk0 = %+v; want 900 writes, 7200 sectors", d)
	}
	if len(m) != 5 {
		t.Fatalf("%d devices; want 5", len(m))
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
		if got := pickDisk(c.stats); got != c.want {
			t.Errorf("pickDisk = %q; want %q", got, c.want)
		}
	}
}

func TestSample(t *testing.T) {
	p := fakeProc{"/proc/stat": statA, "/proc/meminfo": meminfo, "/proc/diskstats": diskA}
	r := New(10)
	r.ReadFile = p.read
	t0 := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	r.Sample(t0)
	if r.Len() != 0 {
		t.Fatal("the first read added a sample")
	}
	p["/proc/stat"], p["/proc/diskstats"] = statB, diskB
	r.Sample(t0.Add(Interval))
	b, ok := r.Last()
	if !ok {
		t.Fatal("no sample after two reads")
	}
	if math.Abs(b.CPU-0.4) > 1e-6 || !b.HasCPU {
		t.Errorf("CPU = %v; want 0.4", b.CPU)
	}
	if b.MemTotal != 4045000*1024 || b.MemUsed != 600000*1024 {
		t.Errorf("memory = %d of %d", b.MemUsed, b.MemTotal)
	}
	if b.Writes != 5 || b.WriteBytes != 80*512 || !b.HasDisk {
		t.Errorf("writes = %d, %d bytes", b.Writes, b.WriteBytes)
	}
	// 5 writes in 10 s are 30 writes a minute.
	if w := b.WritesPerMinute(); math.Abs(w-30) > 1e-9 {
		t.Errorf("WritesPerMinute = %v; want 30", w)
	}
	if r.Disk() != "mmcblk0" {
		t.Errorf("Disk = %q", r.Disk())
	}
}

func TestSampleCounterReset(t *testing.T) {
	p := fakeProc{"/proc/stat": statB, "/proc/meminfo": meminfo, "/proc/diskstats": diskB}
	r := New(10)
	r.ReadFile = p.read
	t0 := time.Unix(1_800_000_000, 0)
	r.Sample(t0)
	p["/proc/stat"], p["/proc/diskstats"] = statA, diskA // the counters went down
	r.Sample(t0.Add(Interval))
	b, _ := r.Last()
	if b.HasCPU || b.HasDisk || !b.HasMem {
		t.Errorf("after a reset: HasCPU %v, HasDisk %v, HasMem %v; want false, false, true", b.HasCPU, b.HasDisk, b.HasMem)
	}
}

func TestNoProc(t *testing.T) {
	r := New(10)
	r.ReadFile = fakeProc{}.read
	if !r.Available() {
		t.Fatal("not available before a read")
	}
	now := time.Unix(1_800_000_000, 0)
	r.Sample(now)
	r.Sample(now.Add(Interval))
	if r.Available() {
		t.Fatal("available with no /proc files")
	}
	if _, ok := r.Last(); !ok {
		t.Fatal("no entry: the entry of a read with no data is in the buffer")
	}
	b := r.Total(now, now.Add(Interval))
	if b.HasCPU || b.HasMem || b.HasDisk {
		t.Errorf("a bucket has data with no /proc files: %+v", b)
	}
	if b.WritesPerMinute() != 0 {
		t.Error("a rate with no disk data")
	}
}

// fill adds n entries, one each Interval up to end, with i writes in entry i.
func fill(r *Recorder, end time.Time, n int) {
	for i := 0; i < n; i++ {
		t := end.Add(-time.Duration(n-1-i) * Interval)
		r.mu.Lock()
		r.add(entry{t: t.UnixMilli(), span: uint32(Interval.Milliseconds()), writes: uint32(i), sectors: 8,
			cpu: 0.5, memUsed: 100, memTotal: 400, flags: hasCPU | hasMem | hasDisk})
		r.mu.Unlock()
	}
}

func TestRingLimit(t *testing.T) {
	r := New(5)
	end := time.Unix(1_800_000_000, 0)
	fill(r, end, 12)
	if r.Len() != 5 {
		t.Fatalf("Len = %d; want 5", r.Len())
	}
	// The buffer has the newest 5: writes 7 to 11.
	b := r.Total(end.Add(-time.Hour), end)
	if b.Writes != 7+8+9+10+11 {
		t.Errorf("writes = %d; want %d", b.Writes, 7+8+9+10+11)
	}
	if b.Covered != 5*Interval {
		t.Errorf("covered = %v", b.Covered)
	}
	if Keep != 8640 {
		t.Errorf("Keep = %d; want a day of samples", Keep)
	}
}

func TestWindow(t *testing.T) {
	r := New(Keep)
	end := time.Unix(1_800_000_000, 0)
	fill(r, end, 12) // 2 minutes
	ws := r.Window(end.Add(-2*time.Minute), end, 2)
	if len(ws) != 2 {
		t.Fatalf("%d buckets", len(ws))
	}
	// A sample at the end of a bucket is in that bucket: 6 samples in each.
	for i, b := range ws {
		if b.Covered != time.Minute {
			t.Errorf("bucket %d covers %v; want 1m", i, b.Covered)
		}
	}
	if ws[0].Writes != 0+1+2+3+4+5 || ws[1].Writes != 6+7+8+9+10+11 {
		t.Errorf("writes = %d, %d", ws[0].Writes, ws[1].Writes)
	}
	if got := ws[1].WritesPerMinute(); got != 51 {
		t.Errorf("WritesPerMinute = %v; want 51", got)
	}
	if got := ws[1].BytesPerMinute(); got != 6*8*512 {
		t.Errorf("BytesPerMinute = %v; want %d", got, 6*8*512)
	}
	if ws[0].CPU != 0.5 || ws[0].MemUsed != 100*1024 || ws[0].MemTotal != 400*1024 {
		t.Errorf("bucket 0 = %+v", ws[0])
	}
	// A bucket with half its time: the rate of the time that it has data for.
	half := r.Window(end.Add(-30*time.Second), end.Add(30*time.Second), 1)[0]
	if half.Covered != 30*time.Second || half.WritesPerMinute() != 2*(9+10+11) {
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
	if r.Len() != Keep {
		t.Fatalf("Len = %d", r.Len())
	}
	day := r.Total(now.Add(-24*time.Hour), now)
	if !day.HasCPU || !day.HasMem || !day.HasDisk || day.Writes == 0 {
		t.Errorf("day = %+v", day)
	}
}
