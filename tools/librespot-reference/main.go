// Decodes the test files with the C decoders of upstream go-librespot, and writes the samples
// (float32, little endian, interleaved) and the stream info. The tests of the pure Go decoders
// compare their output with these files.
//
//	go run . vorbis IN.ogg OUT
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/vorbis"
)

type sizedFile struct {
	*os.File
	size int64
}

func (f sizedFile) Size() int64 { return f.size }

func main() {
	kind, in, out := os.Args[1], os.Args[2], os.Args[3]
	f, err := os.Open(in)
	check(err)
	st, _ := f.Stat()
	r := sizedFile{f, st.Size()}
	var samples []float32
	var decodeNs int64
	info := map[string]any{}
	switch kind {
	case "vorbis":
		start := time.Now()
		defer func() { _ = start }()
		d, err := vorbis.New(&librespot.NullLogger{}, r, nil, 1.0)
		if err != nil {
			info["error"] = err.Error()
			break
		}
		info["sample_rate"], info["channels"] = d.SampleRate, d.Channels
		buf := make([]float32, 1000)
		for {
			n, err := d.Read(buf)
			samples = append(samples, buf[:n]...)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					info["read_error"] = err.Error()
				}
				break
			}
		}
		info["position_ms"] = d.PositionMs()
		decodeNs = time.Since(start).Nanoseconds()
	default:
		check(fmt.Errorf("unknown kind %s", kind))
	}
	info["samples"] = len(samples)
	// The decode time is not compared: it shows the cost of the C decoder on the same machine.
	fmt.Printf("%s: %d samples, C decode %v\n", in, len(samples), time.Duration(decodeNs))
	o, err := os.Create(out + ".f32")
	check(err)
	check(binary.Write(o, binary.LittleEndian, samples))
	check(o.Close())
	j, _ := json.MarshalIndent(info, "", " ")
	check(os.WriteFile(out+".json", append(j, '\n'), 0o644))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
