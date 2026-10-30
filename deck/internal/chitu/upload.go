package chitu

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The upload of a file to the card of a board, in the form of a capture of ChituManager and a
// Phenom of 2026-10-30:
//
//  1. M28 <name>. The board answers "ok N:0".
//  2. The data packets of 1280 bytes of the file (MakeDataPacket). The board answers "ok" to each
//     packet, or "resend <offset>,offset error:<got>" to a packet at the wrong offset.
//  3. M29. The board answers "ok", and "Done saving file!\r\n// <name>" some seconds later.
//
// ChituManager sends the packets with no wait for the answers and does not resend, so its
// uploads to the Phenom broke. Upload sends one packet at a time.

// UploadChunk is the count of the bytes of the file in a data packet.
const UploadChunk = 1280

// MakeDataPacket returns the data packet of the bytes data at offset off of the file: the data,
// the offset (4 bytes, little-endian), the XOR of these bytes, and the byte 0x83.
func MakeDataPacket(data []byte, off int64) []byte {
	p := make([]byte, 0, len(data)+6)
	p = append(p, data...)
	p = binary.LittleEndian.AppendUint32(p, uint32(off))
	var x byte
	for _, b := range p {
		x ^= b
	}
	return append(p, x, 0x83)
}

// resendRE finds the offset in an answer "resend 2560,offset error:3840". The Phenom sometimes
// joins two answers into one ("ok\rend 2560,offset error:3840"), so only the end is sure.
var resendRE = regexp.MustCompile(`(\d+),offset error`)

// The waits of an upload.
const (
	uploadAnswerWait = time.Second      // the wait for the answer to a packet, before it goes again
	uploadTries      = 10               // the sends of one packet with no answer before the upload fails
	uploadSaveWait   = 30 * time.Second // the wait for the answer to M29
)

// Upload sends the size bytes of r to the card of the board as the file name. progress, when not
// nil, gets the bytes that the board took. It uses one socket for the whole upload, so the board
// sends each answer to it.
func (c *Client) Upload(ctx context.Context, name string, r io.ReaderAt, size int64, progress func(done int64)) error {
	if name == "" || strings.ContainsAny(name, "\r\n/") {
		return fmt.Errorf("chitu: a bad file name %q", name)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	buf := make([]byte, 4096)
	// ask sends p and returns the next answer, with uploadTries sends at most.
	ask := func(p []byte, wait time.Duration) (string, error) {
		for try := 0; try < uploadTries; try++ {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if _, err := conn.Write(p); err != nil {
				return "", err
			}
			conn.SetReadDeadline(time.Now().Add(wait))
			n, err := conn.Read(buf)
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			if err != nil {
				return "", err
			}
			return string(buf[:n]), nil
		}
		return "", ErrTimeout
	}
	// drain reads the answers that came late, so the answer to the next packet is its own.
	drain := func() {
		for {
			conn.SetReadDeadline(time.Now().Add(time.Millisecond))
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}

	a, err := ask([]byte("M28 "+name), 3*time.Second)
	if err != nil {
		return fmt.Errorf("chitu: M28: %w", err)
	}
	if !strings.HasPrefix(a, "ok") {
		return fmt.Errorf("chitu: M28: %s", strings.TrimSpace(a))
	}
	chunk := make([]byte, UploadChunk)
	var off int64
	for off < size {
		n, err := r.ReadAt(chunk[:min(UploadChunk, size-off)], off)
		if err != nil && err != io.EOF {
			return err
		}
		a, err := ask(MakeDataPacket(chunk[:n], off), uploadAnswerWait)
		if err != nil {
			return fmt.Errorf("chitu: the packet at %d: %w", off, err)
		}
		if m := resendRE.FindStringSubmatch(a); m != nil {
			to, _ := strconv.ParseInt(m[1], 10, 64)
			if to > size || to%UploadChunk != 0 {
				return fmt.Errorf("chitu: the board asked for offset %d", to)
			}
			drain()
			off = to
			continue
		}
		if !strings.Contains(a, "ok") {
			return fmt.Errorf("chitu: the packet at %d: %s", off, strings.TrimSpace(a))
		}
		off += int64(n)
		if progress != nil {
			progress(off)
		}
	}
	drain()
	a, err = ask([]byte("M29"), uploadSaveWait)
	if err != nil {
		return fmt.Errorf("chitu: M29: %w", err)
	}
	// The first answer is "ok", or "Done saving file!" when the "ok" came before the drain.
	if m := resendRE.FindStringSubmatch(a); m != nil || !strings.Contains(a, "ok") && !strings.Contains(a, "Done saving") {
		return fmt.Errorf("chitu: M29: %s", strings.TrimSpace(a))
	}
	// The board saves the file after "ok", and then sends "Done saving file!" (3.4 s on the Phenom).
	// A query before it can find the file short.
	conn.SetReadDeadline(time.Now().Add(uploadSaveWait))
	for !strings.Contains(a, "Done saving") {
		n, err := conn.Read(buf)
		if err != nil {
			return nil // no message: the file is maybe saved, and the list of the card shows it
		}
		a = string(buf[:n])
	}
	return nil
}
