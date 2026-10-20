package vorbis

// Printdeck: this decoder is pure Go. The upstream version uses libogg and libvorbis through cgo
// (github.com/xlab/vorbis-go). The Ogg framing is in ogg.go, the Vorbis packets decode with
// github.com/jfreymuth/vorbis, and blockin ports the granule position handling of
// vorbis_synthesis_blockin (libvorbis lib/block.c, Copyright (c) 2002-2015 Xiph.Org Foundation,
// BSD-3-Clause). The structure and the error paths are those of the libvorbis version.

import (
	"errors"
	"fmt"
	"io"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/jfreymuth/vorbis"
)

const (
	// DataChunkSize represents the amount of data read from physical bitstream on each iteration.
	DataChunkSize = 4096 // could be also 8192
)

// Decoder implements an OggVorbis decoder.
type Decoder struct {
	sync.Mutex

	log librespot.Logger

	SampleRate int32
	Channels   int32

	// meta is the associated metadata (for seeking)
	meta *MetadataPage

	// gain is the default track gain.
	gain float32

	// syncState tracks the synchronization of the current page.
	syncState oggSyncState

	// streamState tracks the current decode state of the current logical bitstream.
	streamState *oggStreamState

	// page is the current Ogg page.
	page oggPage

	// packet is the current packet.
	packet oggPacket

	// dec decodes the Vorbis packets.
	dec vorbis.Decoder

	// The state of vorbis_synthesis_blockin for the granule positions.
	granulepos  int64
	sampleCount int64

	input    librespot.SizedReadAtSeeker
	buf      []float32
	stopChan chan struct{}
	closed   bool

	lastGranulepos int64
}

// Info represents basic information about the audio in a Vorbis bitstream.
type Info struct {
	Channels   int32
	SampleRate int32
	Comments   []string
	Vendor     string
}

// New creates and initialises a new OggVorbis decoder for the provided bytestream.
func New(log librespot.Logger, r librespot.SizedReadAtSeeker, meta *MetadataPage, gain float32) (*Decoder, error) {
	d := &Decoder{
		log:         log,
		input:       r,
		meta:        meta,
		gain:        gain,
		stopChan:    make(chan struct{}),
		granulepos:  -1,
		sampleCount: -1,
	}

	if err := d.readStreamHeaders(); err != nil {
		return nil, err
	}

	return d, nil
}

// Close stops and finalizes the decoding process.
// Puts the decoder into an unrecoverable state.
func (d *Decoder) Close() {
	if !d.stopRequested() {
		close(d.stopChan)
	}
	d.Lock()
	defer d.Unlock()
	d.closed = true
}

func (d *Decoder) stopRequested() bool {
	select {
	case <-d.stopChan:
		return true
	default:
		return false
	}
}

func (d *Decoder) readChunk() (n int, err error) {
	buf := d.syncState.buffer(DataChunkSize)
	n, err = io.ReadFull(d.input, buf[:DataChunkSize])
	d.syncState.wrote(n)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, io.EOF
	}
	return n, err
}

func (d *Decoder) readStreamHeaders() error {
	if _, err := d.readChunk(); err != nil {
		return fmt.Errorf("vorbis: failed reading headers chunk: %w", err)
	}

	// Read the first page
	if ret := d.syncState.pageout(&d.page); ret != 1 {
		return errors.New("vorbis: not a valid Ogg bitstream")
	}

	// Init the logical bitstream with serial number stored in the page
	d.streamState = newOggStreamState(d.page.serialno())

	// Add a complete page to the bitstream
	if ret := d.streamState.pagein(&d.page); ret < 0 {
		return errors.New("vorbis: the supplied page does not belong this Vorbis stream")
	}
	// Get the first packet
	if ret := d.streamState.packetout(&d.packet); ret != 1 {
		return errors.New("vorbis: unable to fetch initial Vorbis packet from the first page")
	}
	// Finally decode the header packet, which must be the identification header
	if !vorbis.IsHeader(d.packet.packet) || d.packet.packet[0] != 1 {
		return errors.New("vorbis: unable to decode the initial Vorbis header: not an identification header")
	}
	if err := d.dec.ReadHeader(d.packet.packet); err != nil {
		return fmt.Errorf("vorbis: unable to decode the initial Vorbis header: %w", err)
	}

	var headersRead int
forPage:
	for headersRead < 2 {
		if res := d.syncState.pageout(&d.page); res < 0 {
			// bytes have been skipped, try to sync again
			continue forPage
		} else if res == 0 {
			// go get more data
			if _, err := d.readChunk(); err != nil {
				return errors.New("vorbis: got EOF while reading Vorbis headers")
			}
			continue forPage
		}
		// page is synced at this point
		d.streamState.pagein(&d.page)
		for headersRead < 2 {
			if ret := d.streamState.packetout(&d.packet); ret < 0 {
				return errors.New("vorbis: data is missing near the secondary Vorbis header")
			} else if ret == 0 {
				// no packets left on the page, go get a new one
				continue forPage
			}
			if err := d.dec.ReadHeader(d.packet.packet); err != nil {
				return errors.New("vorbis: unable to read the secondary Vorbis header")
			}
			headersRead++
		}
	}

	d.Channels = int32(d.dec.Channels())
	d.SampleRate = int32(d.dec.SampleRate())

	return nil
}

func (d *Decoder) Read(p []float32) (n int, err error) {
	d.Lock()
	defer d.Unlock()
	if d.closed {
		return 0, errors.New("decoder: decoder has already been closed")
	}

	n = 0
	for n < len(p) {
		// read from page buffer
		if len(d.buf) > 0 {
			copied := copy(p[n:], d.buf)
			d.buf = d.buf[copied:]
			n += copied
			continue
		}

		// decode another page
		err = d.readNextPage()
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// blockin decodes a packet and returns its samples (interleaved), as vorbis_synthesis,
// vorbis_synthesis_blockin and vorbis_synthesis_pcmout do together. The samples past the
// granule position of the last packet are cut, and so are the samples before the start of a
// stream whose first audio page is short.
func (d *Decoder) blockin(op *oggPacket) []float32 {
	pcm, err := d.dec.Decode(op.packet)
	if err != nil {
		// vorbis_synthesis failed: libvorbis does not take the block
		return nil
	}
	channels := int64(d.dec.Channels())
	samples := int64(len(pcm)) / channels

	// track the frame number... This is for convenience, but also making sure our last packet
	// doesn't end with added padding.
	if d.sampleCount == -1 {
		d.sampleCount = 0
	} else {
		d.sampleCount += samples
	}

	if d.granulepos == -1 {
		if op.granulepos != -1 { // only set if we have a position to set to
			d.granulepos = op.granulepos

			// is this a short page?
			if d.sampleCount > d.granulepos {
				// corner case; if this is both the first and last audio page, then spec says
				// the end is cut, not beginning
				extra := max(d.sampleCount-op.granulepos, 0)
				extra = min(extra, samples)
				if op.eos {
					// trim the end
					pcm = pcm[:(samples-extra)*channels]
				} else {
					// trim the beginning
					pcm = pcm[extra*channels:]
				}
			}
		}
	} else {
		d.granulepos += samples
		if op.granulepos != -1 && d.granulepos != op.granulepos {
			if d.granulepos > op.granulepos {
				extra := d.granulepos - op.granulepos
				if extra != 0 && op.eos {
					// partial last frame. Strip the extra samples off
					extra = min(extra, samples)
					pcm = pcm[:(samples-extra)*channels]
				}
			}
			d.granulepos = op.granulepos
		}
	}
	return pcm
}

func (d *Decoder) readNextPage() (err error) {
	for {
		if ret := d.syncState.pageout(&d.page); ret < 0 {
			d.log.Debugf("vorbis: corrupt or missing data in bitstream")
			continue
		} else if ret == 0 {
			// need more data
			_, err = d.readChunk()
			if err != nil {
				return err
			}
		} else {
			// we have read the page
			break
		}
	}

	// page is synced at this point
	d.streamState.pagein(&d.page)

	for {
		if ret := d.streamState.packetout(&d.packet); ret < 0 {
			// skip this packet
			continue
		} else if ret == 0 {
			// no packets left on the page
			break
		}

		for _, s := range d.blockin(&d.packet) {
			d.buf = append(d.buf, s*d.gain)
		}

		// save last observed position
		d.lastGranulepos = d.page.granulepos()
	}

	if d.page.eos() {
		return io.EOF
	}

	return nil
}

func (d *Decoder) SetPositionMs(pos int64) (err error) {
	d.Lock()
	defer d.Unlock()

	// get the seek position in bytes from the milliseconds
	posSamples := pos * int64(d.SampleRate) / 1000
	posBytes := d.meta.GetSeekPosition(posSamples)
	if posBytes > d.input.Size() {
		posBytes = d.input.Size()
	}

	// seek there
	if _, err = d.input.Seek(posBytes, io.SeekStart); err != nil {
		return fmt.Errorf("failed seeking input: %w", err)
	}

	// empty the read buffer here, if we found the correct offset
	// the buffer will contain the correct page data
	d.buf = nil

	// we trust that the bytes offset we were given is accurate and process the data at this point
	if _, err = d.readChunk(); err != nil {
		if errors.Is(err, io.EOF) {
			// The seek table only estimates a byte offset, and its
			// resolution is coarsest right at the tail of the track, so
			// landing on or past the last page here is an expected outcome
			// of seeking close to the end, not a failure. Leave the decoder
			// positioned at EOF; the next Read will correctly report the
			// track as finished.
			return nil
		}
		return fmt.Errorf("failed reading chunk: %w", err)
	}

	// get in sync with the next page, this avoids obvious sync errors
	d.syncState.pageout(&d.page)

	// read the page now that we are aligned
	if err = d.readNextPage(); err != nil {
		if errors.Is(err, io.EOF) {
			// see comment above: landing on the EOS page while seeking is
			// a valid outcome, not a failure
			return nil
		}
		return fmt.Errorf("failed reading page: %w", err)
	}

	d.log.Tracef("seek to %dms (diff: %dms, samples: %d, bytes: %d)", pos, pos-d.PositionMs(), posSamples, posBytes)
	return nil
}

// PositionMs is the time of the last page, as vorbis_granule_time.
func (d *Decoder) PositionMs() int64 {
	if d.lastGranulepos < 0 || d.SampleRate == 0 {
		return -1 * 1000
	}
	return int64(float64(d.lastGranulepos) / float64(d.SampleRate) * 1000)
}
