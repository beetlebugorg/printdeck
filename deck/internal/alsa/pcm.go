package alsa

import (
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"unsafe"
)

// Format is a sample format (SNDRV_PCM_FORMAT_*).
type Format int

const (
	FormatS16LE   Format = 2
	FormatS32LE   Format = 10
	FormatFloatLE Format = 14
)

func (f Format) String() string {
	switch f {
	case FormatS16LE:
		return "S16_LE"
	case FormatS32LE:
		return "S32_LE"
	case FormatFloatLE:
		return "FLOAT_LE"
	}
	return fmt.Sprintf("format %d", int(f))
}

// Bytes is the size of one sample.
func (f Format) Bytes() int {
	if f == FormatS16LE {
		return 2
	}
	return 4
}

// The hardware parameters (struct snd_pcm_hw_params).
const (
	paramAccess    = 0
	paramFormat    = 1
	paramSubformat = 2

	paramChannels      = 10
	paramRate          = 11
	paramPeriodSize    = 13
	paramBufferSize    = 17
	paramFirstInterval = 8
	paramLastInterval  = 19

	accessRWInterleaved = 3
	pcmVersion          = 0x20012
)

type mask [8]uint32

type interval struct {
	min, max uint32
	flags    uint32 // openmin:1, openmax:1, integer:1, empty:1
}

const (
	intervalOpenMin = 1 << 0
	intervalOpenMax = 1 << 1
	intervalInteger = 1 << 2
)

type hwParams struct {
	flags     uint32
	masks     [3]mask
	mres      [5]mask
	intervals [12]interval
	ires      [9]interval
	rmask     uint32
	cmask     uint32
	info      uint32
	msbits    uint32
	rateNum   uint32
	rateDen   uint32
	fifoSize  uint64
	reserved  [64]byte
}

// any sets all values possible (_snd_pcm_hw_params_any).
func (p *hwParams) any() {
	*p = hwParams{}
	for i := range p.masks {
		for j := range p.masks[i] {
			p.masks[i][j] = ^uint32(0)
		}
	}
	for i := range p.intervals {
		p.intervals[i] = interval{0, ^uint32(0), 0}
	}
	p.rmask = ^uint32(0)
	p.info = ^uint32(0)
}

func (p *hwParams) setMask(param int, value int) {
	m := &p.masks[param]
	*m = mask{}
	m[value/32] = 1 << (value % 32)
	p.rmask |= 1 << param
}

func (p *hwParams) setInterval(param int, lo, hi uint32) {
	p.intervals[param-paramFirstInterval] = interval{lo, hi, intervalInteger}
	p.rmask |= 1 << param
}

func (p *hwParams) interval(param int) interval { return p.intervals[param-paramFirstInterval] }

type swParams struct {
	tstampMode       int32
	periodStep       uint32
	sleepMin         uint32
	_                uint32
	availMin         uint64
	xferAlign        uint64
	startThreshold   uint64
	stopThreshold    uint64
	silenceThreshold uint64
	silenceSize      uint64
	boundary         uint64
	proto            uint32
	tstampType       uint32
	reserved         [56]byte
}

type xferi struct {
	result int64
	buf    unsafe.Pointer
	frames uint64
}

// PCM is an open playback device.
type PCM struct {
	f          *os.File
	Format     Format
	Channels   int
	Rate       int
	PeriodSize int // frames
	BufferSize int // frames
}

// Config is the wanted setup of a playback device.
type Config struct {
	Channels int
	Rate     int
	// Formats in order of preference. The first that the device takes is used.
	Formats []Format
	// BufferTimeMicro and PeriodCount set the buffer size and the period size, as near as the
	// device allows (as snd_pcm_hw_params_set_buffer_time_near and set_period_size_near).
	BufferTimeMicro int
	PeriodCount     int
}

// OpenPlayback opens a playback device ("hw:CARD,DEV") and sets it up.
func OpenPlayback(device string, cfg Config) (*PCM, error) {
	card, dev, err := ParseDevice(device)
	if err != nil {
		return nil, err
	}
	f, err := openDevice(fmt.Sprintf("/dev/snd/pcmC%dD%dp", card, dev))
	if err != nil {
		return nil, err
	}
	pcm := &PCM{f: f, Channels: cfg.Channels, Rate: cfg.Rate}
	if err := pcm.setup(cfg); err != nil {
		f.Close()
		return nil, err
	}
	return pcm, nil
}

func (pcm *PCM) fd() uintptr { return pcm.f.Fd() }

// refine narrows all the parameters to what the device takes, from the values in p.
func (pcm *PCM) refine(p *hwParams) error {
	p.rmask = ^uint32(0)
	return ioctl(pcm.fd(), ioctlHWRefine, unsafe.Pointer(p))
}

// near narrows an interval parameter to the value nearest to target that the device takes.
func (pcm *PCM) near(p *hwParams, param int, target uint32) error {
	iv := p.interval(param)
	target = max(iv.min, min(iv.max, target))
	try := func(lo, hi uint32) bool {
		q := *p
		q.setInterval(param, lo, hi)
		if pcm.refine(&q) != nil {
			return false
		}
		*p = q
		return true
	}
	if try(target, target) {
		return nil
	}
	// The nearest values above and below the target
	above, below := uint32(0), uint32(0)
	hasAbove, hasBelow := false, false
	q := *p
	q.setInterval(param, target, iv.max)
	if pcm.refine(&q) == nil {
		above, hasAbove = q.interval(param).min, true
	}
	q = *p
	q.setInterval(param, iv.min, target)
	if pcm.refine(&q) == nil {
		below, hasBelow = q.interval(param).max, true
	}
	switch {
	case hasAbove && (!hasBelow || above-target <= target-below):
		if try(above, above) {
			return nil
		}
	case hasBelow:
		if try(below, below) {
			return nil
		}
	}
	return fmt.Errorf("alsa: no value near %d for parameter %d", target, param)
}

func (pcm *PCM) setup(cfg Config) error {
	var version int32
	if err := ioctl(pcm.fd(), ioctlPVersion, unsafe.Pointer(&version)); err != nil {
		return fmt.Errorf("alsa: PVERSION: %w", err)
	}

	var p hwParams
	p.any()
	if err := pcm.refine(&p); err != nil {
		return fmt.Errorf("alsa: HW_REFINE: %w", err)
	}
	p.setMask(paramAccess, accessRWInterleaved)
	if err := pcm.refine(&p); err != nil {
		return fmt.Errorf("alsa: interleaved access is not supported: %w", err)
	}
	found := false
	for _, f := range cfg.Formats {
		q := p
		q.setMask(paramFormat, int(f))
		if pcm.refine(&q) == nil {
			p, pcm.Format, found = q, f, true
			break
		}
	}
	if !found {
		return errors.New("alsa: could not find a supported PCM format")
	}
	p.setInterval(paramChannels, uint32(cfg.Channels), uint32(cfg.Channels))
	if err := pcm.refine(&p); err != nil {
		return fmt.Errorf("alsa: %d channels are not supported: %w", cfg.Channels, err)
	}
	p.setInterval(paramRate, uint32(cfg.Rate), uint32(cfg.Rate))
	if err := pcm.refine(&p); err != nil {
		return fmt.Errorf("alsa: the rate %d is not supported: %w", cfg.Rate, err)
	}
	bufferFrames := uint32(int64(cfg.Rate) * int64(cfg.BufferTimeMicro) / 1_000_000)
	if err := pcm.near(&p, paramBufferSize, bufferFrames); err != nil {
		return err
	}
	periodCount := max(cfg.PeriodCount, 1)
	if err := pcm.near(&p, paramPeriodSize, p.interval(paramBufferSize).min/uint32(periodCount)); err != nil {
		return err
	}
	if err := ioctl(pcm.fd(), ioctlHWParams, unsafe.Pointer(&p)); err != nil {
		return fmt.Errorf("alsa: HW_PARAMS: %w", err)
	}
	pcm.PeriodSize = int(p.interval(paramPeriodSize).min)
	pcm.BufferSize = int(p.interval(paramBufferSize).min)

	// The boundary, as the kernel computes it: the buffer size times the largest power of two
	// that fits in a long.
	boundary := uint64(pcm.BufferSize)
	for boundary*2 <= math.MaxInt64-uint64(pcm.BufferSize) {
		boundary *= 2
	}
	sw := swParams{
		periodStep:     1,
		availMin:       uint64(pcm.PeriodSize),
		xferAlign:      1,
		startThreshold: uint64(pcm.BufferSize - pcm.PeriodSize),
		stopThreshold:  uint64(pcm.BufferSize),
		boundary:       boundary,
		proto:          pcmVersion,
	}
	if err := ioctl(pcm.fd(), ioctlSWParams, unsafe.Pointer(&sw)); err != nil {
		return fmt.Errorf("alsa: SW_PARAMS: %w", err)
	}
	return pcm.Prepare()
}

// Prepare makes the device ready to play after a stop or an underrun.
func (pcm *PCM) Prepare() error {
	if err := ioctl(pcm.fd(), ioctlPrepare, nil); err != nil {
		return fmt.Errorf("alsa: PREPARE: %w", err)
	}
	return nil
}

// Drop stops the playback and throws away the samples in the buffer.
func (pcm *PCM) Drop() error {
	if err := ioctl(pcm.fd(), ioctlDrop, nil); err != nil {
		return fmt.Errorf("alsa: DROP: %w", err)
	}
	return nil
}

// Delay returns the frames that are in the buffer and the hardware: the time until a new frame
// sounds.
func (pcm *PCM) Delay() (int, error) {
	var frames int64
	if err := ioctl(pcm.fd(), ioctlDelay, unsafe.Pointer(&frames)); err != nil {
		if err == syscall.EPIPE {
			return 0, ErrUnderrun
		}
		return 0, fmt.Errorf("alsa: DELAY: %w", err)
	}
	return int(frames), nil
}

// Avail returns the frames that a write can take now.
func (pcm *PCM) Avail() int {
	d, err := pcm.Delay()
	if err != nil {
		return pcm.BufferSize
	}
	return max(0, pcm.BufferSize-d)
}

// Write writes interleaved frames in the format of the device. It waits for room in the
// buffer. It returns ErrUnderrun when the buffer ran empty; Recover then makes the device ready.
func (pcm *PCM) Write(data []byte) (int, error) {
	frameBytes := pcm.Format.Bytes() * pcm.Channels
	frames := len(data) / frameBytes
	if frames == 0 {
		return 0, nil
	}
	x := xferi{buf: unsafe.Pointer(&data[0]), frames: uint64(frames)}
	if err := ioctl(pcm.fd(), ioctlWriteIFrame, unsafe.Pointer(&x)); err != nil {
		if err == syscall.EPIPE {
			return 0, ErrUnderrun
		}
		return 0, fmt.Errorf("alsa: WRITEI_FRAMES: %w", err)
	}
	return int(x.result), nil
}

// Recover makes the device ready after an underrun, as snd_pcm_recover.
func (pcm *PCM) Recover(err error) error {
	if errors.Is(err, ErrUnderrun) {
		return pcm.Prepare()
	}
	return err
}

// Close closes the device. The kernel stops the playback.
func (pcm *PCM) Close() error { return pcm.f.Close() }
