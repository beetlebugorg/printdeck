package vorbis

// The Ogg framing of the Vorbis stream, in pure Go.
//
// This is a port of the functions of libogg's src/framing.c that the decoder uses: the sync state
// (ogg_sync_buffer, ogg_sync_wrote, ogg_sync_pageseek, ogg_sync_pageout) and the stream state
// (ogg_stream_pagein, ogg_stream_packetout). The return values are the same as in libogg, so the
// decoder keeps the structure of its libogg version.
//
//	libogg: Copyright (c) 2002-2018 Xiph.Org Foundation (BSD-3-Clause)
//	Go port: Copyright (C) 2026 the Printdeck authors

import "encoding/binary"

var oggCRCTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return
}()

func oggCRC(crc uint32, data []byte) uint32 {
	for _, b := range data {
		crc = crc<<8 ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

// oggPage is one page: its header and its body.
type oggPage struct {
	header, body []byte
}

func (p *oggPage) serialno() uint32  { return binary.LittleEndian.Uint32(p.header[14:]) }
func (p *oggPage) granulepos() int64 { return int64(binary.LittleEndian.Uint64(p.header[6:])) }
func (p *oggPage) eos() bool         { return p.header[5]&0x04 != 0 }

// oggPacket is one packet of the stream.
type oggPacket struct {
	packet     []byte
	bos, eos   bool
	granulepos int64
	packetno   int64
}

// oggSyncState finds the pages in the bytes of the stream (ogg_sync_state).
type oggSyncState struct {
	data        []byte
	fill        int
	returned    int
	unsynced    bool
	headerbytes int
	bodybytes   int
}

// buffer returns a buffer for size more bytes (ogg_sync_buffer).
func (oy *oggSyncState) buffer(size int) []byte {
	// first, clear out any space that has been previously returned
	if oy.returned != 0 {
		oy.fill -= oy.returned
		if oy.fill > 0 {
			copy(oy.data, oy.data[oy.returned:oy.returned+oy.fill])
		}
		oy.returned = 0
	}
	if size > len(oy.data)-oy.fill {
		// We need to extend the internal buffer
		newsize := oy.fill + size + 4096 // an extra page to be nice
		data := make([]byte, newsize)
		copy(data, oy.data[:oy.fill])
		oy.data = data
	}
	return oy.data[oy.fill:]
}

// wrote notes the bytes that were written to the buffer (ogg_sync_wrote).
func (oy *oggSyncState) wrote(bytes int) { oy.fill += bytes }

// pageseek syncs to the next page (ogg_sync_pageseek). It returns the page length for a page,
// 0 when it needs more data, or minus the number of bytes that it skipped.
func (oy *oggSyncState) pageseek(og *oggPage) int {
	page := oy.data[oy.returned:oy.fill]
	bytes := len(page)

	syncFail := func() int {
		oy.headerbytes, oy.bodybytes = 0, 0
		// search for possible capture
		next := oy.fill
		for i := 1; i < bytes; i++ {
			if page[i] == 'O' {
				next = oy.returned + i
				break
			}
		}
		skipped := next - oy.returned
		oy.returned = next
		return -skipped
	}

	if oy.headerbytes == 0 {
		if bytes < 27 {
			return 0 // not enough for a header
		}
		// verify capture pattern
		if string(page[:4]) != "OggS" {
			return syncFail()
		}
		headerbytes := int(page[26]) + 27
		if bytes < headerbytes {
			return 0 // not enough for header + seg table
		}
		// count up body length in the segment table
		for i := 0; i < int(page[26]); i++ {
			oy.bodybytes += int(page[27+i])
		}
		oy.headerbytes = headerbytes
	}

	if oy.bodybytes+oy.headerbytes > bytes {
		return 0
	}

	// The whole test page is buffered. Verify the checksum
	saved := binary.LittleEndian.Uint32(page[22:])
	binary.LittleEndian.PutUint32(page[22:], 0)
	crc := oggCRC(0, page[:oy.headerbytes])
	crc = oggCRC(crc, page[oy.headerbytes:oy.headerbytes+oy.bodybytes])
	binary.LittleEndian.PutUint32(page[22:], saved)
	if crc != saved {
		// D'oh. Mismatch! Corrupt page (or miscapture and not a page at all)
		return syncFail()
	}

	// yes, have a whole page all ready to go
	n := oy.headerbytes + oy.bodybytes
	if og != nil {
		og.header = page[:oy.headerbytes]
		og.body = page[oy.headerbytes:n]
	}
	oy.unsynced = false
	oy.returned += n
	oy.headerbytes, oy.bodybytes = 0, 0
	return n
}

// pageout returns 1 for a page, 0 when it needs more data, and -1 once when it lost the sync
// (ogg_sync_pageout). The page is valid until the next call to buffer.
func (oy *oggSyncState) pageout(og *oggPage) int {
	for {
		ret := oy.pageseek(og)
		if ret > 0 {
			return 1 // have a page
		}
		if ret == 0 {
			return 0 // need more data
		}
		// head did not start a synced page... skipped some bytes
		if !oy.unsynced {
			oy.unsynced = true
			return -1
		}
		// loop. keep looking
	}
}

// The flags of the lacing values.
const (
	lacingBOS  = 0x100
	lacingEOS  = 0x200
	lacingHole = 0x400
)

// oggStreamState joins the pages of one logical stream into packets (ogg_stream_state).
type oggStreamState struct {
	bodyData       []byte
	bodyReturned   int
	lacingVals     []int
	granuleVals    []int64
	lacingPacket   int
	lacingReturned int
	eos            bool
	serialno       uint32
	pageno         int64
	packetno       int64
}

func newOggStreamState(serialno uint32) *oggStreamState {
	return &oggStreamState{serialno: serialno}
}

// pagein adds a page to the stream (ogg_stream_pagein). It returns -1 for a page of another
// stream.
func (os *oggStreamState) pagein(og *oggPage) int {
	header := og.header
	body := og.body
	version := header[4]
	continued := header[5]&0x01 != 0
	bos := header[5]&0x02 != 0
	eos := header[5]&0x04 != 0
	granulepos := og.granulepos()
	serialno := og.serialno()
	pageno := int64(binary.LittleEndian.Uint32(header[18:]))
	segments := int(header[26])
	segptr := 0

	// clean up 'returned data'
	if br := os.bodyReturned; br != 0 {
		os.bodyData = append(os.bodyData[:0], os.bodyData[br:]...)
		os.bodyReturned = 0
	}
	if lr := os.lacingReturned; lr != 0 {
		os.lacingVals = append(os.lacingVals[:0], os.lacingVals[lr:]...)
		os.granuleVals = append(os.granuleVals[:0], os.granuleVals[lr:]...)
		os.lacingPacket -= lr
		os.lacingReturned = 0
	}

	// check the serial number
	if serialno != os.serialno {
		return -1
	}
	if version > 0 {
		return -1
	}

	// are we in sequence?
	if pageno != os.pageno {
		// unroll previous partial packet (if any)
		for i := os.lacingPacket; i < len(os.lacingVals); i++ {
			os.bodyData = os.bodyData[:len(os.bodyData)-os.lacingVals[i]&0xff]
		}
		os.lacingVals = os.lacingVals[:os.lacingPacket]
		os.granuleVals = os.granuleVals[:os.lacingPacket]

		// make a note of dropped data in segment table
		if os.pageno != -1 {
			os.lacingVals = append(os.lacingVals, lacingHole)
			os.granuleVals = append(os.granuleVals, 0)
			os.lacingPacket++
		}
	}

	// are we a 'continued packet' page? If so, we may need to skip some segments
	if continued {
		n := len(os.lacingVals)
		if n < 1 || os.lacingVals[n-1]&0xff < 255 || os.lacingVals[n-1] == lacingHole {
			bos = false
			for ; segptr < segments; segptr++ {
				val := int(header[27+segptr])
				body = body[val:]
				if val < 255 {
					segptr++
					break
				}
			}
		}
	}

	if len(body) > 0 {
		os.bodyData = append(os.bodyData, body...)
	}

	saved := -1
	for segptr < segments {
		val := int(header[27+segptr])
		lv := val
		if bos {
			lv |= lacingBOS
			bos = false
		}
		os.lacingVals = append(os.lacingVals, lv)
		os.granuleVals = append(os.granuleVals, -1)
		if val < 255 {
			saved = len(os.lacingVals) - 1
		}
		segptr++
		if val < 255 {
			os.lacingPacket = len(os.lacingVals)
		}
	}
	// set the granulepos on the last granuleval of the last full packet
	if saved != -1 {
		os.granuleVals[saved] = granulepos
	}

	if eos {
		os.eos = true
		if n := len(os.lacingVals); n > 0 {
			os.lacingVals[n-1] |= lacingEOS
		}
	}
	os.pageno = pageno + 1
	return 0
}

// packetout returns 1 for a packet, 0 when it needs another page, and -1 once for a hole in the
// data (ogg_stream_packetout). The packet is valid until the next call to pagein.
func (os *oggStreamState) packetout(op *oggPacket) int {
	ptr := os.lacingReturned
	if os.lacingPacket <= ptr {
		return 0
	}
	if os.lacingVals[ptr]&lacingHole != 0 {
		// we need to tell the codec there's a gap; it might need to handle previous packet
		// dependencies.
		os.lacingReturned++
		os.packetno++
		return -1
	}
	size := os.lacingVals[ptr] & 0xff
	bytes := size
	eos := os.lacingVals[ptr]&lacingEOS != 0
	bos := os.lacingVals[ptr]&lacingBOS != 0
	for size == 255 {
		ptr++
		val := os.lacingVals[ptr]
		size = val & 0xff
		if val&lacingEOS != 0 {
			eos = true
		}
		bytes += size
	}
	if op != nil {
		op.eos = eos
		op.bos = bos
		op.packet = os.bodyData[os.bodyReturned : os.bodyReturned+bytes]
		op.packetno = os.packetno
		op.granulepos = os.granuleVals[ptr]
	}
	os.bodyReturned += bytes
	os.lacingReturned = ptr + 1
	os.packetno++
	return 1
}
