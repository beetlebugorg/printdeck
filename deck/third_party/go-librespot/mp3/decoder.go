package mp3

// Printdeck: this decoder is pure Go. The upstream version uses libmpg123 through cgo. The frames
// decode with github.com/hajimehoshi/go-mp3 (Apache-2.0), which gives 16-bit samples. The gapless
// handling follows libmpg123 (src/libmpg123/frame.c and parse.c, LGPL-2.1, the mpg123 project):
// with the LAME tag, the encoder delay plus the decoder delay of 529 samples are cut at the start,
// and the padding at the end. The output is stereo, as upstream asks libmpg123 for
// (MPG123_STEREO).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/hajimehoshi/go-mp3"
)

// gaplessDelay is the delay of the MP3 synthesis filter bank (GAPLESS_DELAY of libmpg123).
const gaplessDelay = 529

// Decoder implements an MP3 decoder.
type Decoder struct {
	log librespot.Logger

	SampleRate int32
	Channels   int32

	// gain is the default track gain.
	gain float32

	input librespot.SizedReadAtSeeker

	dec *mp3.Decoder
	buf []byte

	// The gapless window, in frames of the decoded stream of go-mp3 (which includes the Xing
	// frame): the first frame to return, and the end (-1 for no end).
	begin, end int64
	// pos is the frame of the decoded stream at the next read.
	pos int64
	// eof is a seek past the end of a stream of unknown length.
	eof bool

	// spf is the samples per frame. frameStarts are the byte offsets of the frames that a seek
	// indexed, from the first frame.
	spf         int64
	frameStarts []int64
	scanned     int64 // the byte offset after the last indexed frame
}

// readerOnly hides the Seek method of the input, so that go-mp3 does not scan the whole stream
// (and download it) when it opens.
type readerOnly struct{ io.Reader }

func New(log librespot.Logger, r librespot.SizedReadAtSeeker, gain float32) (_ *Decoder, err error) {
	d := &Decoder{log: log, input: r, gain: gain, end: -1}

	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("could not open mp3 stream: %w", err)
	}
	head := make([]byte, 16*1024)
	n, _ := io.ReadFull(r, head)
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("could not open mp3 stream: %w", err)
	}
	if g, ok := parseGapless(head[:n]); ok {
		d.begin = g.xingFrames + g.delay + gaplessDelay
		if g.frames > 0 {
			d.end = g.xingFrames + g.frames - g.padding + gaplessDelay
		}
	}

	d.dec, err = mp3.NewDecoder(readerOnly{r})
	if err != nil {
		return nil, fmt.Errorf("could not open mp3 stream: %w", err)
	}
	d.SampleRate = int32(d.dec.SampleRate())
	d.Channels = 2
	d.spf = 1152
	if d.SampleRate < 32000 {
		d.spf = 576
	}

	log.Debugf("mp3 stream info: sample rate = %d, channels = %d", d.SampleRate, d.Channels)
	return d, nil
}

type gapless struct {
	xingFrames     int64 // the samples of the Xing frame, which go-mp3 decodes as silence
	frames         int64 // the samples of the audio frames, or 0 when unknown
	delay, padding int64
}

// parseGapless reads the Xing/Info tag and its LAME tag from the first frame.
func parseGapless(b []byte) (gapless, bool) {
	// skip an ID3v2 tag
	if len(b) >= 10 && string(b[:3]) == "ID3" {
		size := int(b[6]&0x7f)<<21 | int(b[7]&0x7f)<<14 | int(b[8]&0x7f)<<7 | int(b[9]&0x7f)
		if b[5]&0x10 != 0 {
			size += 10 // footer
		}
		if 10+size > len(b) {
			return gapless{}, false
		}
		b = b[10+size:]
	}
	// find the first frame
	for len(b) >= 4 && !(b[0] == 0xff && b[1]&0xe0 == 0xe0) {
		b = b[1:]
	}
	if len(b) < 4 {
		return gapless{}, false
	}
	version := (b[1] >> 3) & 3 // 3: MPEG-1, 2: MPEG-2, 0: MPEG-2.5
	layer := (b[1] >> 1) & 3   // 1: layer III
	crc := b[1]&1 == 0
	mono := b[3]>>6 == 3
	if layer != 1 || version == 1 {
		return gapless{}, false
	}
	spf := int64(1152)
	sideInfo := 32
	if mono {
		sideInfo = 17
	}
	if version != 3 {
		spf = 576
		sideInfo = 17
		if mono {
			sideInfo = 9
		}
	}
	off := 4 + sideInfo
	if crc {
		off += 2
	}
	if len(b) < off+8 || (string(b[off:off+4]) != "Xing" && string(b[off:off+4]) != "Info") {
		return gapless{}, false
	}
	flags := binary.BigEndian.Uint32(b[off+4:])
	p := off + 8
	g := gapless{xingFrames: spf}
	if flags&1 != 0 {
		if len(b) < p+4 {
			return gapless{}, false
		}
		g.frames = int64(binary.BigEndian.Uint32(b[p:])) * spf
		p += 4
	}
	if flags&2 != 0 {
		p += 4
	}
	if flags&4 != 0 {
		p += 100
	}
	if flags&8 != 0 {
		p += 4
	}
	// The LAME tag: 9 bytes of encoder name, then the delay and the padding in 3 bytes at 21.
	if len(b) < p+24 {
		return gapless{}, false
	}
	dp := b[p+21:]
	g.delay = int64(dp[0])<<4 | int64(dp[1])>>4
	g.padding = int64(dp[1]&0x0f)<<8 | int64(dp[2])
	return g, true
}

// fill reads decoded frames (4 bytes each: 16-bit stereo) from go-mp3.
func (d *Decoder) fill() error {
	tmp := make([]byte, 4608)
	n, err := d.dec.Read(tmp)
	d.buf = append(d.buf, tmp[:n]...)
	return err
}

func (d *Decoder) Read(p []float32) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	for n+1 < len(p) && !d.eof {
		if d.end >= 0 && d.pos >= d.end {
			break
		}
		if len(d.buf) < 4 {
			if err := d.fill(); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return n, fmt.Errorf("error while decoding mp3 frame: %w", err)
			}
			continue
		}
		frame := d.buf[:4]
		d.buf = d.buf[4:]
		d.pos++
		if d.pos <= d.begin {
			continue
		}
		p[n] = float32(int16(binary.LittleEndian.Uint16(frame))) / 32768 * d.gain
		p[n+1] = float32(int16(binary.LittleEndian.Uint16(frame[2:]))) / 32768 * d.gain
		n += 2
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// The frame header tables of MPEG audio layer III.
var (
	bitratesMPEG1 = [15]int64{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	bitratesMPEG2 = [15]int64{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	sampleRates   = map[byte][3]int64{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

// frameSize returns the size of a layer III frame from its header, or 0 for no valid header.
func frameSize(h []byte) int64 {
	if h[0] != 0xff || h[1]&0xe0 != 0xe0 {
		return 0
	}
	version, layer := (h[1]>>3)&3, (h[1]>>1)&3
	bi, si, pad := h[2]>>4, (h[2]>>2)&3, int64((h[2]>>1)&1)
	if version == 1 || layer != 1 || bi == 0 || bi == 15 || si == 3 {
		return 0
	}
	rate := sampleRates[version][si]
	if version == 3 {
		return 144000*bitratesMPEG1[bi]/rate + pad
	}
	return 72000*bitratesMPEG2[bi]/rate + pad
}

// frameStart returns the byte offset of a frame, or false after the last frame. It indexes the
// frame headers from the start of the stream as far as needed, as libmpg123 does for an exact
// seek.
func (d *Decoder) frameStart(frame int64) (int64, bool) {
	if d.frameStarts == nil {
		head := make([]byte, 10)
		if _, err := d.input.ReadAt(head, 0); err == nil && string(head[:3]) == "ID3" {
			size := int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 | int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f)
			d.scanned = 10 + size
			if head[5]&0x10 != 0 {
				d.scanned += 10
			}
		}
		d.frameStarts = []int64{}
	}
	h := make([]byte, 4)
	for int64(len(d.frameStarts)) <= frame {
		if _, err := d.input.ReadAt(h, d.scanned); err != nil {
			return 0, false
		}
		size := frameSize(h)
		if size == 0 {
			// not a frame here: look for the next sync
			d.scanned++
			continue
		}
		d.frameStarts = append(d.frameStarts, d.scanned)
		d.scanned += size
	}
	return d.frameStarts[frame], true
}

// seekPreroll is the number of frames that decode before the target frame, so that the bit
// reservoir and the filter states are those of a decode from the start.
const seekPreroll = 3

func (d *Decoder) SetPositionMs(pos int64) error {
	// mpg123 seeks and tells in samples, not bytes.
	off := pos * int64(d.SampleRate) / 1000
	target := d.begin + off
	if d.end >= 0 && target > d.end {
		target = d.end
	}
	frame := target / d.spf
	first := max(0, frame-seekPreroll)
	d.buf, d.pos, d.eof = nil, target, false
	if _, ok := d.frameStart(frame); !ok {
		// past the end of the stream
		d.eof = true
		return nil
	}
	from, _ := d.frameStart(first)
	if first == 0 {
		from = 0 // go-mp3 skips the ID3 tag itself
	}
	dec, err := mp3.NewDecoder(readerOnly{io.NewSectionReader(d.input, from, d.input.Size()-from)})
	if err != nil {
		return fmt.Errorf("could not seek to position %dms: %w", pos, err)
	}
	d.dec = dec
	// decode the preroll frames and the start of the target frame, and drop their samples
	skip := (target - first*d.spf) * 4
	for int64(len(d.buf)) < skip {
		if err := d.fill(); err != nil {
			if errors.Is(err, io.EOF) {
				d.eof = true
				return nil
			}
			return fmt.Errorf("could not seek to position %dms: %w", pos, err)
		}
	}
	d.buf = d.buf[skip:]
	return nil
}

func (d *Decoder) PositionMs() int64 {
	return max(0, d.pos-d.begin) * 1000 / int64(d.SampleRate)
}

func (d *Decoder) Close() error {
	return nil
}
