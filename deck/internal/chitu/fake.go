package chitu

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// FakeBoard answers the queries of a ChiTu board on UDP, for the tests and for a deck with no
// printer. The answers have the form of the answers of a Phenom of 2026-10-30, during a print and
// with no print.
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
	// Files are the lines of the answer to M20, for example "part.cbddlp 1234".
	Files []string
	// The upload now (M28), and the files that the board saved (M29), by name.
	upName   string
	upData   []byte
	Uploaded map[string][]byte
	// SaveNoOK answers M29 with "Done saving file!" and no "ok", as the Phenom did once.
	SaveNoOK bool
	// WriteErrors is the count of the next data packets that get "Error:write dat".
	WriteErrors int
	// SlowStart answers M6030 after 3 s, as the Phenom did with a large file.
	SlowStart bool
	// LoseData is the count of the next data packets that the board loses, with no answer.
	LoseData int
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
		if a, ok := b.data(buf[:n]); ok {
			if a != "" {
				b.conn.WriteTo([]byte(a+"\r\n"), from)
			}
			continue
		}
		q := strings.TrimSpace(string(buf[:n]))
		if q == "M20" {
			b.mu.Lock()
			b.log = append(b.log, q)
			lines := append([]string{"Begin file list"}, b.Files...)
			b.mu.Unlock()
			lines = append(lines, "End file list", fmt.Sprintf("ok L:%d", len(lines)-1))
			for _, l := range lines {
				b.conn.WriteTo([]byte(l+"\r\n"), from)
			}
			continue
		}
		b.mu.Lock()
		slow := b.SlowStart && strings.HasPrefix(q, "M6030")
		b.mu.Unlock()
		if a, ok := b.answer(q); ok && slow {
			go func(a string, from net.Addr) {
				time.Sleep(3 * time.Second)
				b.conn.WriteTo([]byte(a+"\r\n"), from)
			}(a, from)
			continue
		} else if ok {
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
	case "M6030 ':" + strings.TrimSuffix(strings.TrimPrefix(q, "M6030 ':"), "'") + "'":
		// The start of a print of a file of the list: the board reads the file from its start.
		name := strings.TrimSuffix(strings.TrimPrefix(q, "M6030 ':"), "'")
		for _, l := range b.Files {
			if i := strings.LastIndexByte(l, ' '); i > 0 && l[:i] == name {
				fmt.Sscan(l[i+1:], &b.Size)
				b.Read, b.Seconds, b.Z, b.Flag = 1, 0, 0, 0
				return "ok", true
			}
		}
		return "Error:Cann't open file:" + name, true
	case "G28 Z0", "G90", "G91", "M25", "M24", "M33":
		return "ok", true
	case "M99999":
		host, _, _ := net.SplitHostPort(b.Addr())
		return "ok MAC:00:e0:4c:18:00:29 IP:" + host + " VER:V1.4.1 ID:29,00,18,00,0b,47,39,33 NAME:CBD", true
	case "M27":
		if b.Size == 0 {
			return "Error:It's not printing now!", true
		}
		return fmt.Sprintf("SD printing byte %d/%d", b.Read, b.Size), true
	case "M4000":
		if b.Size == 0 {
			return "ok B:103/0 E1:-7/0 E2:73/0 X:0.000 Y:0.000 Z:0.000 F:0/0 D:0/0/1 T:0", true
		}
		return fmt.Sprintf("ok B:104/0 E1:-8/0 E2:72/0 X:0.000 Y:0.000 Z:%.3f F:256/256 D:%d/%d/%d T:%d",
			b.Z, b.Read, b.Size, b.Flag, b.Seconds), true
	}
	if name, ok := strings.CutPrefix(q, "M30 "); ok {
		for i, l := range b.Files {
			if j := strings.LastIndexByte(l, ' '); j > 0 && l[:j] == name {
				b.Files = append(b.Files[:i], b.Files[i+1:]...)
				return "ok", true
			}
		}
		return "Error:Cann't open file:" + name, true
	}
	if name, ok := strings.CutPrefix(q, "M28 "); ok {
		b.upName, b.upData = name, []byte{}
		return "ok N:0", true
	}
	if q == "M29" && b.upName != "" {
		if b.Uploaded == nil {
			b.Uploaded = map[string][]byte{}
		}
		b.Uploaded[b.upName] = b.upData
		b.Files = append(b.Files, fmt.Sprintf("%s %d", b.upName, len(b.upData)))
		b.upName, b.upData = "", nil
		if b.SaveNoOK {
			return "Done saving file!\r\n// " + strings.TrimSpace(q[3:]), true
		}
		return "ok\r\nDone saving file!", true
	}
	if strings.HasPrefix(q, "G1 Z") {
		return "ok", true
	}
	return "", false
}

// data takes a data packet of an upload. ok is false for another packet. The answer is "" for a
// lost packet.
func (b *FakeBoard) data(p []byte) (answer string, ok bool) {
	d, off, ok := DataPacket(p)
	if !ok {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.upName == "" {
		return "Error:No file open", true
	}
	if b.LoseData > 0 {
		b.LoseData--
		return "", true
	}
	if b.WriteErrors > 0 {
		b.WriteErrors--
		return "Error:write dat", true
	}
	if off != len(b.upData) {
		return fmt.Sprintf("resend %d,offset error:%d", len(b.upData), off), true
	}
	b.upData = append(b.upData, d...)
	return "ok", true
}
