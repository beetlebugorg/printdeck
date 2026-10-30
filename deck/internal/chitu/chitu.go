// Package chitu talks to the main board of a ChiTu resin printer (for example the Peopoly Phenom)
// over the network: one line of text in one UDP packet to port 3000, and one line in the answer
// (M20 answers with one packet for each line). The package has the queries that only read: M99999
// (the board), M27 (the progress), M4000 (the state) and M20 (the files). The answers of a Phenom
// of 2026-10-30 are the test data.
package chitu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Port is the UDP port of the board.
const Port = 3000

// Info is the answer to M99999: the board.
type Info struct {
	MAC, IP, Version, ID, Name string
}

// Status is the answer to M4000. The meaning of some fields is not known: see the comments.
type Status struct {
	X, Y, Z float64 // the axes in mm; Z is the height of the plate
	// Read and Size are the bytes of the file that the board read, and the size of the file (D).
	// Flag is the third number of D: 1 during a pause (not checked).
	Read, Size int64
	Flag       int
	// Seconds is T: the time of the print in seconds (not checked).
	Seconds int
	// B, E1, E2 and F are the raw fields: "104/0" and so on (meaning not known).
	B, E1, E2, F string
}

// Printing reports that the board has a file in work: a size, and bytes read that are less than
// the size.
func (s Status) Printing() bool { return s.Size > 0 && s.Read < s.Size }

// Progress is the share of the file that the board read, from 0 to 1.
func (s Status) Progress() float64 {
	if s.Size <= 0 {
		return 0
	}
	return float64(s.Read) / float64(s.Size)
}

// fields splits "ok A:1 B:2/3" into the values by key. The first word "ok" is optional.
func fields(line string) map[string]string {
	out := map[string]string{}
	for _, w := range strings.Fields(strings.TrimSpace(line)) {
		if k, v, ok := strings.Cut(w, ":"); ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// ParseInfo reads the answer to M99999, for example
// "ok MAC:00:e0:4c:18:00:29 IP:192.168.76.58 VER:V1.4.1 ID:29,00,18,00,0b,47,39,33 NAME:CBD".
func ParseInfo(line string) (Info, error) {
	if !strings.HasPrefix(strings.TrimSpace(line), "ok") {
		return Info{}, fmt.Errorf("chitu: not an answer to M99999: %q", line)
	}
	// The MAC has colons: it is the text from "MAC:" to the next space.
	f := fields(line)
	in := Info{IP: f["IP"], Version: f["VER"], ID: f["ID"], Name: f["NAME"]}
	if i := strings.Index(line, "MAC:"); i >= 0 {
		in.MAC, _, _ = strings.Cut(line[i+4:], " ")
	}
	if in.IP == "" && in.MAC == "" {
		return Info{}, fmt.Errorf("chitu: no board in the answer: %q", line)
	}
	return in, nil
}

// ParseStatus reads the answer to M4000, for example
// "ok B:104/0 E1:-8/0 E2:72/0 X:0.000 Y:0.000 Z:65.147 F:256/256 D:78174460/79439932/0 T:63309".
func ParseStatus(line string) (Status, error) {
	f := fields(line)
	d, ok := f["D"]
	if !ok {
		return Status{}, fmt.Errorf("chitu: not an answer to M4000: %q", line)
	}
	var s Status
	num := func(k string) float64 {
		v, _ := strconv.ParseFloat(f[k], 64)
		return v
	}
	s.X, s.Y, s.Z = num("X"), num("Y"), num("Z")
	parts := strings.Split(d, "/")
	if len(parts) >= 2 {
		s.Read, _ = strconv.ParseInt(parts[0], 10, 64)
		s.Size, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) >= 3 {
		s.Flag, _ = strconv.Atoi(parts[2])
	}
	s.Seconds, _ = strconv.Atoi(f["T"])
	s.B, s.E1, s.E2, s.F = f["B"], f["E1"], f["E2"], f["F"]
	return s, nil
}

// ParseProgress reads the answer to M27, "SD printing byte 78174460/79439932". ok is false for an
// answer with no numbers, for example a board with no print.
func ParseProgress(line string) (read, size int64, ok bool) {
	i := strings.LastIndexByte(line, ' ')
	r, s, found := strings.Cut(strings.TrimSpace(line[i+1:]), "/")
	if !found {
		return 0, 0, false
	}
	read, e1 := strconv.ParseInt(r, 10, 64)
	size, e2 := strconv.ParseInt(s, 10, 64)
	return read, size, e1 == nil && e2 == nil
}

// ErrTimeout is a query with no answer in the time of the client.
var ErrTimeout = errors.New("chitu: no answer from the board")

// Client sends queries to the board at Addr ("192.168.76.58:3000"), one at a time.
type Client struct {
	Addr    string
	Timeout time.Duration // the wait for an answer; 0 is 2 s

	mu sync.Mutex
}

// Query sends the line and returns the answer, with no line end.
func (c *Client) Query(ctx context.Context, line string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.Addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	wait := c.Timeout
	if wait <= 0 {
		wait = 2 * time.Second
	}
	deadline := time.Now().Add(wait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	if _, err := conn.Write([]byte(line)); err != nil {
		return "", err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "", ErrTimeout
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(buf[:n]), "\r\n"), nil
}

// File is a file on the card of the board.
type File struct {
	Name string
	Size int64
}

// Printable reports that the file is a sliced file to print: no folder (size 0), and no file of
// macOS ("._" first) or other hidden file.
func (f File) Printable() bool {
	n := strings.ToLower(f.Name)
	return f.Size > 0 && !strings.HasPrefix(f.Name, ".") && (strings.HasSuffix(n, ".cbddlp") || strings.HasSuffix(n, ".ctb"))
}

// ParseFileLine reads a line of the answer to M20: "shoulders-flex.cbddlp 39815689". The name can
// have spaces: the size is after the last space.
func ParseFileLine(line string) (File, bool) {
	line = strings.TrimSpace(line)
	i := strings.LastIndexByte(line, ' ')
	if i <= 0 {
		return File{}, false
	}
	size, err := strconv.ParseInt(line[i+1:], 10, 64)
	if err != nil {
		return File{}, false
	}
	return File{Name: line[:i], Size: size}, true
}

// Files asks the board for the files on its card (M20). The board sends "Begin file list", one
// packet for each file, "End file list", and "ok L:<count>".
func (c *Client) Files(ctx context.Context) ([]File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.Addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	wait := c.Timeout
	if wait <= 0 {
		wait = 2 * time.Second
	}
	if _, err := conn.Write([]byte("M20")); err != nil {
		return nil, err
	}
	var out []File
	begun := false
	buf := make([]byte, 4096)
	for {
		conn.SetReadDeadline(time.Now().Add(wait))
		n, err := conn.Read(buf)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			if begun {
				return out, nil // the end of the list got lost
			}
			return nil, ErrTimeout
		}
		if err != nil {
			return nil, err
		}
		line := strings.TrimSpace(string(buf[:n]))
		switch {
		case line == "Begin file list":
			begun = true
		case line == "End file list", strings.HasPrefix(line, "ok"):
			return out, nil
		case begun:
			if f, ok := ParseFileLine(line); ok {
				out = append(out, f)
			}
		}
	}
}

// Info asks the board for its info (M99999).
func (c *Client) Info(ctx context.Context) (Info, error) {
	a, err := c.Query(ctx, "M99999")
	if err != nil {
		return Info{}, err
	}
	return ParseInfo(a)
}

// Status asks the board for its state (M4000).
func (c *Client) Status(ctx context.Context) (Status, error) {
	a, err := c.Query(ctx, "M4000")
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(a)
}
