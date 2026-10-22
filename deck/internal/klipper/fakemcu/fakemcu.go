//go:build linux

// Package fakemcu is a fake Klipper board for tests. It speaks the Klipper protocol on a
// pseudo-terminal with a real data dictionary: it answers identify, get_clock, get_uptime and
// get_config, acknowledges each message block as the firmware does, and logs the commands that it
// receives.
package fakemcu

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"printdeck/internal/klipper/msgproto"
)

// Board is a fake board.
type Board struct {
	parser   *msgproto.Parser
	identify []byte // the compressed data dictionary
	freq     float64
	start    time.Time

	mu       sync.Mutex
	handlers map[string]func(params map[string]msgproto.Value) []string
	// configCRC is the CRC of finalize_config, or "" before it.
	configCRC string
	received  []string
	nextSeq   byte
}

// New makes a board with a data dictionary (the JSON of klipper.dict).
func New(dictionary []byte) (*Board, error) {
	p := msgproto.NewParser()
	if err := p.ProcessIdentify(dictionary, false); err != nil {
		return nil, err
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(dictionary)
	w.Close()
	freq, err := p.ConstantFloat("CLOCK_FREQ")
	if err != nil {
		return nil, err
	}
	b := &Board{parser: p, identify: z.Bytes(), freq: freq, start: time.Now(),
		handlers: map[string]func(map[string]msgproto.Value) []string{}}
	b.Handle("get_clock", func(map[string]msgproto.Value) []string {
		return []string{fmt.Sprintf("clock clock=%d", uint32(b.Clock()))}
	})
	b.Handle("get_uptime", func(map[string]msgproto.Value) []string {
		c := b.Clock()
		return []string{fmt.Sprintf("uptime high=%d clock=%d", c>>32, uint32(c))}
	})
	b.Handle("get_config", func(map[string]msgproto.Value) []string {
		if b.configCRC == "" {
			return []string{"config is_config=0 crc=0 is_shutdown=0 move_count=0"}
		}
		return []string{"config is_config=1 crc=" + b.configCRC + " is_shutdown=0 move_count=500"}
	})
	return b, nil
}

// Clock is the clock of the board: CLOCK_FREQ ticks per second since New.
func (b *Board) Clock() uint64 { return uint64(time.Since(b.start).Seconds() * b.freq) }

// Handle sets the answer to a command: response texts, for example "clock clock=5".
func (b *Board) Handle(name string, f func(params map[string]msgproto.Value) []string) {
	b.mu.Lock()
	b.handlers[name] = f
	b.mu.Unlock()
}

// Received returns the text of each command that the board received.
func (b *Board) Received() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.received...)
}

// Serve answers the commands on rw until it fails. On a pseudo-terminal, a read fails with EIO
// while the host has the port closed: the board waits and goes on, as a real board does between
// connections.
func (b *Board) Serve(rw io.ReadWriter) error {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := rw.Read(tmp)
		if errors.Is(err, syscall.EIO) {
			buf = buf[:0]
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
		buf = append(buf, tmp[:n]...)
		for {
			l := b.parser.CheckPacket(buf)
			if l == 0 {
				break
			}
			if l < 0 {
				buf = buf[1:] // skip a sync byte or bad data
				continue
			}
			out, err := b.handleBlock(buf[:l])
			if err != nil {
				return err
			}
			buf = buf[l:]
			if _, err := rw.Write(out); err != nil {
				return err
			}
		}
	}
}

// handleBlock answers one message block as the firmware does (command_find_and_dispatch in
// src/command.c): each response in its own block, and then an empty block, which is the ack. All
// the blocks have the next sequence number.
func (b *Board) handleBlock(block []byte) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSeq = (block[msgproto.MessagePosSeq] + 1) & msgproto.MessageSeqMask
	msgs, err := b.parser.Dump(block)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, text := range msgs[1:] {
		b.received = append(b.received, text)
		// After finalize_config, the board reports its config, as the firmware does.
		if crc, ok := strings.CutPrefix(text, "finalize_config crc="); ok {
			b.configCRC = crc
		}
		var resps [][]byte
		if name := firstWord(text); name == "identify" {
			resp, err := b.identifyResponse(block)
			if err != nil {
				return nil, err
			}
			resps = append(resps, resp)
		} else if h := b.handlers[name]; h != nil {
			for _, r := range h(b.params(block, name)) {
				cmd, err := b.parser.CreateCommand(r)
				if err != nil {
					return nil, err
				}
				resps = append(resps, cmd)
			}
		}
		for _, r := range resps {
			out = append(out, msgproto.EncodeMsgblock(int(b.nextSeq), r)...)
		}
	}
	return append(out, msgproto.EncodeMsgblock(int(b.nextSeq), nil)...), nil
}

func firstWord(s string) string {
	for i, c := range s {
		if c == ' ' {
			return s[:i]
		}
	}
	return s
}

// params decodes the parameters of the command name in a block with one command.
func (b *Board) params(block []byte, name string) map[string]msgproto.Value {
	p, err := b.parser.Parse(block)
	if err != nil || p["#name"] != name {
		return nil
	}
	return p
}

func (b *Board) identifyResponse(block []byte) ([]byte, error) {
	p, err := b.parser.Parse(block)
	if err != nil {
		return nil, err
	}
	offset, count := p["offset"].(int64), p["count"].(int64)
	end := min(int(offset+count), len(b.identify))
	data := []byte{}
	if int(offset) < end {
		data = b.identify[offset:end]
	}
	return b.parser.LookupResponse("identify_response").EncodeByName(
		map[string]msgproto.Value{"offset": offset, "data": data})
}

// OpenPty opens a pseudo-terminal. The board serves the master side, and the host opens path.
func OpenPty() (master *os.File, path string, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, "", err
	}
	var unlock int32
	var n uint32
	rc, err := m.SyscallConn()
	if err != nil {
		m.Close()
		return nil, "", err
	}
	var ioErr error
	rc.Control(func(fd uintptr) {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
			ioErr = e
			return
		}
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
			ioErr = e
		}
	})
	if ioErr != nil {
		m.Close()
		return nil, "", ioErr
	}
	return m, fmt.Sprintf("/dev/pts/%d", n), nil
}
