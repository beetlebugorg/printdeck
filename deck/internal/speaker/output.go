package speaker

// The ALSA output of the speaker: a port of output/driver-alsa.go of go-librespot (GPL-3.0,
// Copyright (c) devgianlu) to the pure Go ALSA package of the deck, with the equalizer before
// the conversion to the format of the device. The mixer of the upstream driver is not ported: the
// volume is external (the deck sets it on the amp).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/output"

	"printdeck/internal/alsa"
	"printdeck/internal/eq"
)

const backendName = "printdeck-alsa"

const (
	defaultBufferTimeMicro = 500_000
	defaultNumPeriods      = 4
)

type alsaOutput struct {
	log librespot.Logger
	eq  *eq.EQ

	channels   int
	sampleRate int
	device     string
	reader     librespot.Float32Reader

	lock sync.Mutex

	pcm         *alsa.PCM // nil when the device is closed
	bufferTime  int
	periodCount int

	externalVolume bool
	volume         float32
	closed         bool

	volumeUpdate chan float32
	err          chan error
}

func newAlsaOutput(opts *output.NewOutputOptions, e *eq.EQ) *alsaOutput {
	out := &alsaOutput{
		log:            opts.Log,
		eq:             e,
		reader:         opts.Reader,
		channels:       opts.ChannelCount,
		sampleRate:     opts.SampleRate,
		device:         opts.Device,
		volume:         opts.InitialVolume,
		err:            make(chan error, 2),
		externalVolume: opts.ExternalVolume,
		volumeUpdate:   opts.VolumeUpdate,
		bufferTime:     opts.BufferTimeMicro,
		periodCount:    opts.PeriodCount,
	}
	if out.bufferTime == 0 {
		out.bufferTime = defaultBufferTimeMicro
	}
	if out.periodCount == 0 {
		out.periodCount = defaultNumPeriods
	}
	if opts.Mixer != "" {
		out.log.Warnf("the alsa mixer is not supported: the volume is external")
	}
	return out
}

func (out *alsaOutput) setupPcm() error {
	pcm, err := alsa.OpenPlayback(out.device, alsa.Config{
		Channels:        out.channels,
		Rate:            out.sampleRate,
		Formats:         []alsa.Format{alsa.FormatFloatLE, alsa.FormatS32LE, alsa.FormatS16LE},
		BufferTimeMicro: out.bufferTime,
		PeriodCount:     out.periodCount,
	})
	if err != nil {
		return err
	}
	out.pcm = pcm
	out.log.Debugf("alsa driver configured, rate = %d, period size = %d frames, buffer size = %d frames, PCM format = %s",
		out.sampleRate, pcm.PeriodSize, pcm.BufferSize, pcm.Format)

	// Move samples from the reader out to the ALSA device.
	// This loop continues until the device is closed and set to nil
	// (Pause() or Close()) or there is an error.
	go out.outputLoop(pcm)
	return nil
}

// encode converts the samples to the format of the device, clipped to [-1.0, 1.0].
func encode(dst []byte, floats []float32, format alsa.Format) []byte {
	dst = dst[:0]
	for _, f := range floats {
		v := math.Max(-1, math.Min(1, float64(f)))
		switch format {
		case alsa.FormatFloatLE:
			dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(float32(v)))
		case alsa.FormatS32LE:
			dst = binary.LittleEndian.AppendUint32(dst, uint32(int32(v*2147483647)))
		default:
			dst = binary.LittleEndian.AppendUint16(dst, uint16(int16(v*32767)))
		}
	}
	return dst
}

func (out *alsaOutput) outputLoop(pcm *alsa.PCM) {
	floats := make([]float32, out.channels*pcm.PeriodSize)
	var data []byte

	for {
		// Calculate how long we should wait until there's enough space in the
		// ALSA buffer for writing. We wait until periodSize*1.125 frames can be
		// written, as upstream.
		out.lock.Lock()
		if pcm != out.pcm {
			// Either out.pcm is nil, or it is a new device entirely (with a very
			// fast pause+resume). In both cases, the loop should be stopped.
			out.lock.Unlock()
			return
		}
		availableFrames := pcm.Avail()
		waitForFrames := (pcm.PeriodSize + pcm.PeriodSize/8) - availableFrames
		waitTime := time.Duration(waitForFrames) * time.Second / time.Duration(out.sampleRate)
		out.lock.Unlock()

		// Wait until enough frames are available, with the output unlocked so
		// that things like pause can still happen in the meantime.
		time.Sleep(waitTime)

		out.lock.Lock()
		if pcm != out.pcm {
			out.lock.Unlock()
			return
		}

		// Read audio data. This can take a few milliseconds because it needs to
		// decode the audio data.
		n, err := out.reader.Read(floats)

		// Apply volume.
		if !out.externalVolume {
			volume := out.volume * out.volume
			for i := 0; i < n; i++ {
				floats[i] *= volume
			}
		}

		if n > 0 {
			if out.eq != nil {
				out.eq.Process(floats[:n])
			}
			data = encode(data, floats[:n], pcm.Format)
			if _, werr := pcm.Write(data); werr != nil {
				// Got an error, so must recover (even for an underrun).
				if rerr := pcm.Recover(werr); rerr != nil {
					// Failed to recover from this error. Close the output and
					// report the error.
					out.err <- fmt.Errorf("ALSA error at recover: %w", rerr)
					out.closed = true
					out.lock.Unlock()
					return
				}
			}
		}

		if errors.Is(err, io.EOF) {
			// Reached EOF, move to a "paused" state.
			out.pcm = nil
			if cerr := pcm.Close(); cerr != nil {
				out.err <- cerr
			}
			out.lock.Unlock()
			return
		} else if err != nil {
			// Got some other error. Close the output and report the error.
			out.err <- err
			out.closed = true
			out.lock.Unlock()
			return
		}

		out.lock.Unlock()
	}
}

func (out *alsaOutput) Pause() error {
	out.lock.Lock()
	defer out.lock.Unlock()

	if out.closed || out.pcm == nil {
		return nil
	}

	err := out.pcm.Close()
	out.pcm = nil
	return err
}

func (out *alsaOutput) Resume() error {
	out.lock.Lock()
	defer out.lock.Unlock()

	if out.closed || out.pcm != nil {
		return nil
	}

	return out.setupPcm()
}

func (out *alsaOutput) Drop() error {
	out.lock.Lock()
	defer out.lock.Unlock()

	if out.closed || out.pcm == nil {
		return nil
	}

	if err := out.pcm.Drop(); err != nil {
		return err
	}

	// Since we are not actually stopping the stream, prepare it again.
	return out.pcm.Prepare()
}

func (out *alsaOutput) DelayMs() (int64, error) {
	out.lock.Lock()
	defer out.lock.Unlock()

	if out.closed || out.pcm == nil {
		return 0, nil
	}

	frames, err := out.pcm.Delay()
	if err != nil {
		return 0, err
	}
	return int64(frames) * 1000 / int64(out.sampleRate), nil
}

func (out *alsaOutput) SetVolume(vol float32) {
	if vol < 0 || vol > 1 {
		panic(fmt.Sprintf("invalid volume value: %0.2f", vol))
	} else if vol == out.volume {
		// No need to update the volume if it didn't change.
		return
	}

	out.volume = vol
	sendVolumeUpdate(out.volumeUpdate, vol)
}

// sendVolumeUpdate is a non-blocking send on a buffered channel, that replaces an old value
// (output.sendVolumeUpdate of go-librespot).
func sendVolumeUpdate(ch chan float32, val float32) {
	if cap(ch) == 0 {
		panic("channel must be buffered") // sanity check
	}
	select {
	case <-ch:
	default:
	}
	ch <- val
}

func (out *alsaOutput) Error() <-chan error {
	return out.err
}

func (out *alsaOutput) Close() error {
	out.lock.Lock()
	defer out.lock.Unlock()

	if out.closed {
		return nil
	}

	if out.pcm != nil {
		if err := out.pcm.Close(); err != nil {
			return err
		}
		out.pcm = nil
	}

	out.closed = true
	return nil
}
