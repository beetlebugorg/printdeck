package mp3

// Printdeck: compares the pure Go decoder with the libmpg123 decoder of upstream go-librespot
// (testdata from tools/librespot-reference/make-reference.sh).

import (
	"bytes"
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

type sizedReader struct {
	*bytes.Reader
}

func (r sizedReader) Size() int64 { return r.Reader.Size() }

type refInfo struct {
	SampleRate     int32  `json:"sample_rate"`
	Channels       int32  `json:"channels"`
	PositionMs     int64  `json:"position_ms"`
	SeekPositionMs int64  `json:"seek_position_ms"`
	Samples        int    `json:"samples"`
	Error          string `json:"error"`
	ReadError      string `json:"read_error"`
	SeekError      string `json:"seek_error"`
}

func decode(t *testing.T, path string, seek bool) (refInfo, []float32) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var info refInfo
	d, err := New(&librespot.NullLogger{}, sizedReader{bytes.NewReader(data)}, 1.0)
	if err != nil {
		info.Error = err.Error()
		return info, nil
	}
	info.SampleRate, info.Channels = d.SampleRate, d.Channels
	if seek {
		if err := d.SetPositionMs(1000); err != nil {
			info.SeekError = err.Error()
		}
		info.SeekPositionMs = d.PositionMs()
	}
	var samples []float32
	buf := make([]float32, 1000)
	for !seek || len(samples) < 8000 {
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

func TestDecoderMatchesLibmpg123(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.mp3")
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		for _, seek := range []bool{false, true} {
			base := strings.TrimSuffix(path, ".mp3")
			if seek {
				base += ".seek"
			}
			data, err := os.ReadFile(base + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var want refInfo
			json.Unmarshal(data, &want)
			raw, _ := os.ReadFile(base + ".f32")
			ref := make([]float32, len(raw)/4)
			binary.Read(bytes.NewReader(raw), binary.LittleEndian, ref)

			start := time.Now()
			got, samples := decode(t, path, seek)
			elapsed := time.Since(start)
			if got != want {
				t.Errorf("%s: got %+v, want %+v", base, got, want)
				continue
			}
			maxDiff := 0.
			for i := range ref {
				maxDiff = math.Max(maxDiff, math.Abs(float64(samples[i]-ref[i])))
			}
			// go-mp3 gives 16-bit samples, libmpg123 gives float: 1e-4 is -80 dB of full scale.
			if maxDiff > 1e-4 {
				t.Errorf("%s: max difference %g", base, maxDiff)
			}
			t.Logf("%s: %d samples, max difference %.2g, decode %v", base, len(ref), maxDiff, elapsed)
		}
	}
}
