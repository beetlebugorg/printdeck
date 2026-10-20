package vorbis

// Printdeck: compares the pure Go decoder with the libvorbis decoder of upstream go-librespot
// (testdata from tools/librespot-reference/make-reference.sh).

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	librespot "github.com/devgianlu/go-librespot"
)

type sizedFile struct {
	*os.File
	size int64
}

func (f sizedFile) Size() int64 { return f.size }

type refInfo struct {
	SampleRate int32  `json:"sample_rate"`
	Channels   int32  `json:"channels"`
	PositionMs int64  `json:"position_ms"`
	Samples    int    `json:"samples"`
	Error      string `json:"error"`
	ReadError  string `json:"read_error"`
}

func decodeFile(t *testing.T, path string) (refInfo, []float32) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	var info refInfo
	d, err := New(&librespot.NullLogger{}, sizedFile{f, st.Size()}, nil, 1.0)
	if err != nil {
		info.Error = err.Error()
		return info, nil
	}
	info.SampleRate, info.Channels = d.SampleRate, d.Channels
	var samples []float32
	buf := make([]float32, 1000)
	for {
		n, err := d.Read(buf)
		samples = append(samples, buf[:n]...)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				info.ReadError = err.Error()
			}
			break
		}
	}
	info.PositionMs = d.PositionMs()
	info.Samples = len(samples)
	return info, samples
}

func TestDecoderMatchesLibvorbis(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.ogg")
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		base := strings.TrimSuffix(path, ".ogg")
		data, err := os.ReadFile(base + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var want refInfo
		json.Unmarshal(data, &want)
		raw, _ := os.ReadFile(base + ".f32")
		ref := make([]float32, len(raw)/4)
		binary.Read(strings.NewReader(string(raw)), binary.LittleEndian, ref)

		start := time.Now()
		got, samples := decodeFile(t, path)
		elapsed := time.Since(start)
		if got != want {
			t.Errorf("%s: got %+v, want %+v", path, got, want)
			continue
		}
		maxDiff, sumSq := 0., 0.
		for i := range ref {
			d := math.Abs(float64(samples[i] - ref[i]))
			maxDiff = math.Max(maxDiff, d)
			sumSq += d * d
		}
		rms := 0.
		if len(ref) > 0 {
			rms = math.Sqrt(sumSq / float64(len(ref)))
		}
		// 1e-4 is -80 dB of full scale: far below what anyone hears.
		if maxDiff > 1e-4 {
			t.Errorf("%s: max difference %g", path, maxDiff)
		}
		t.Logf("%s: %d samples, max difference %.2g, rms %.2g, decode %v", path, len(ref), maxDiff, rms, elapsed)
	}
}
