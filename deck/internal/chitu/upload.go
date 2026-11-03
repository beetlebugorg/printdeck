package chitu

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
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

// The waits of an upload: the wait for the answer to a packet before it goes again, the time with
// no answer before the upload fails, and the wait for the answer to M29. UploadStall is a variable
// for the tests.
const (
	uploadAnswerWait = time.Second
	uploadSaveWait   = 30 * time.Second
)

var UploadStall = time.Minute

// uploadWriteTries is the most answers "Error:write dat" in one upload, and uploadWritePause the
// wait after each of them.
const (
	uploadWriteTries = 5
	uploadWritePause = 3 * time.Second
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
	// ask sends p and returns the next answer. It sends p again after each wait with no answer,
	// for uploadStall at most: the Phenom stopped answering for more than 10 s once, at 92 % of a
	// file of 40 MB.
	ask := func(p []byte, wait time.Duration) (string, error) {
		for end := time.Now().Add(max(wait, UploadStall)); time.Now().Before(end); {
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
	writeErrs := 0
	for off < size {
		n, err := r.ReadAt(chunk[:min(UploadChunk, size-off)], off)
		if err != nil && err != io.EOF {
			return err
		}
		a, err := ask(MakeDataPacket(chunk[:n], off), uploadAnswerWait)
		if err != nil {
			return fmt.Errorf("chitu: the packet at %d: %w", off, err)
		}
		if strings.Contains(a, "write dat") && writeErrs < uploadWriteTries {
			// The board could not write the packet to its card. It stalled before on the same
			// card, so the deck waits and sends the packet again.
			writeErrs++
			time.Sleep(uploadWritePause)
			drain()
			continue
		}
		if m := resendRE.FindStringSubmatch(a); m != nil {
			to, _ := strconv.ParseInt(m[1], 10, 64)
			if to < 0 || to > size || to > math.MaxUint32 || to%UploadChunk != 0 {
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
