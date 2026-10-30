package chitu

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

// FakeBoard answers the queries of a ChiTu board on UDP, for the tests and for a deck with no
// printer. The answers have the form of the answers of a Phenom of 2026-10-30. The answers of a
// board with no print (D:0/0/0, "Not SD printing") are a guess: no idle board answered yet.
type FakeBoard struct {
	conn net.PacketConn

	mu sync.Mutex
	// The print: the bytes read, the size of the file, the pause flag, the height of the plate
	// and the time of the print in seconds. A Size of 0 is a board with no print.
	Read, Size int64
	Flag       int
	Z          float64
	Seconds    int
	drop       int      // the queries to drop: a board that does not answer
	log        []string // the queries, in order
}

// NewFakeBoard starts a fake board at addr ("127.0.0.1:0" for any port).
func NewFakeBoard(addr string) (*FakeBoard, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	b := &FakeBoard{conn: conn}
	go b.serve()
	return b, nil
}

// Addr is the address of the board.
func (b *FakeBoard) Addr() string { return b.conn.LocalAddr().String() }

// Close stops the board.
func (b *FakeBoard) Close() error { return b.conn.Close() }

// Set changes the print of the board.
func (b *FakeBoard) Set(change func(b *FakeBoard)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	change(b)
}

// Drop makes the board give no answer to the next n queries.
func (b *FakeBoard) Drop(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drop = n
}

// Log returns the queries that the board got.
func (b *FakeBoard) Log() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.log...)
}

func (b *FakeBoard) serve() {
	buf := make([]byte, 4096)
	for {
		n, from, err := b.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if a, ok := b.answer(strings.TrimSpace(string(buf[:n]))); ok {
			b.conn.WriteTo([]byte(a+"\r\n"), from)
		}
	}
}

// answer returns the answer to the query q, or ok false for no answer.
func (b *FakeBoard) answer(q string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.log = append(b.log, q)
	if b.drop > 0 {
		b.drop--
		return "", false
	}
	switch q {
	case "M99999":
		host, _, _ := net.SplitHostPort(b.Addr())
		return "ok MAC:00:e0:4c:18:00:29 IP:" + host + " VER:V1.4.1 ID:29,00,18,00,0b,47,39,33 NAME:CBD", true
	case "M27":
		if b.Size == 0 {
			return "Not SD printing", true
		}
		return fmt.Sprintf("SD printing byte %d/%d", b.Read, b.Size), true
	case "M4000":
		return fmt.Sprintf("ok B:104/0 E1:-8/0 E2:72/0 X:0.000 Y:0.000 Z:%.3f F:256/256 D:%d/%d/%d T:%d",
			b.Z, b.Read, b.Size, b.Flag, b.Seconds), true
	}
	return "", false
}
