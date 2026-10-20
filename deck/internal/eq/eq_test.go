package eq

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func readF32(t *testing.T, path string) []float32 {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, len(raw)/4)
	binary.Read(bytes.NewReader(raw), binary.LittleEndian, out)
	return out
}

// The output is the output of the real CamillaDSP for the same filters and gains
// (testdata from tools/camilladsp-reference/make-reference.sh).
func TestProcessMatchesCamillaDSP(t *testing.T) {
	input := readF32(t, "testdata/input.f32")
	f, err := os.Open("testdata/cases.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		var g Gains
		for i := range g {
			g[i], _ = strconv.ParseFloat(fields[1+i], 64)
		}
		want := readF32(t, "testdata/"+fields[0]+".f32")
		e := New(t.TempDir())
		e.changed(g)
		got := append([]float32(nil), input...)
		// CamillaDSP processes chunks of 1024 frames
		for i := 0; i < len(got); i += 2048 {
			e.Process(got[i:min(i+2048, len(got))])
		}
		maxDiff, exact := 0.0, 0
		for i := range want {
			d := math.Abs(float64(got[i] - want[i]))
			maxDiff = math.Max(maxDiff, d)
			if got[i] == want[i] {
				exact++
			}
		}
		if maxDiff > 1e-6 {
			t.Errorf("%s: max difference %g", fields[0], maxDiff)
		}
		t.Logf("%s %v: %d of %d samples exact, max difference %.2g", fields[0], g, exact, len(want), maxDiff)
	}
}

func TestSetGainSteps(t *testing.T) {
	e := New(t.TempDir())
	e.SetGain(0, 3.3)
	e.SetGain(1, 40)
	if g := e.Gains(); g[0] != 3.5 || g[1] != MaxGain {
		t.Errorf("gains %v", g)
	}
	e.SetPreset("Bass")
	if e.Preset() != "Bass" {
		t.Errorf("preset %q", e.Preset())
	}
	if Preamp(Gains{6, -3, 0, 2, 0}) != -6 || Preamp(Gains{-4, 0, 0, 0, -2}) != 0 {
		t.Error("preamp")
	}
}

func BenchmarkProcess(b *testing.B) {
	e := New(b.TempDir())
	e.changed(Gains{5, 1, 0, 1, 4})
	buf := make([]float32, 2*4096)
	b.SetBytes(int64(len(buf) * 4))
	for i := 0; i < b.N; i++ {
		e.Process(buf)
	}
}
