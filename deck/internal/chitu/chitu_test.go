package chitu

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// The answers of the Phenom of 2026-10-30.
const (
	phenomInfo     = "ok MAC:00:e0:4c:18:00:29 IP:192.168.76.58 VER:V1.4.1 ID:29,00,18,00,0b,47,39,33 NAME:CBD\r\n"
	phenomProgress = "SD printing byte 78174460/79439932\r\n"
	phenomStatus   = "ok B:104/0 E1:-8/0 E2:72/0 X:0.000 Y:0.000 Z:65.147 F:256/256 D:78174460/79439932/0 T:63309\r\n"
)

func TestParse(t *testing.T) {
	in, err := ParseInfo(phenomInfo)
	if err != nil || in.MAC != "00:e0:4c:18:00:29" || in.IP != "192.168.76.58" || in.Version != "V1.4.1" || in.Name != "CBD" ||
		in.ID != "29,00,18,00,0b,47,39,33" {
		t.Errorf("info %+v %v", in, err)
	}
	if _, err := ParseInfo("SD printing byte 1/2"); err == nil {
		t.Error("an answer to M27 read as the info")
	}

	st, err := ParseStatus(phenomStatus)
	if err != nil || st.Z != 65.147 || st.Read != 78174460 || st.Size != 79439932 || st.Flag != 0 || st.Seconds != 63309 ||
		st.B != "104/0" || st.F != "256/256" || !st.Printing() {
		t.Errorf("status %+v %v", st, err)
	}
	if p := st.Progress(); p < 0.984 || p > 0.985 {
		t.Errorf("progress %f", p)
	}
	// A field that a newer firmware adds does not change the others.
	if st, err := ParseStatus("ok Z:1.500 D:10/20/1 T:5 W:9"); err != nil || st.Z != 1.5 || st.Flag != 1 || st.Seconds != 5 {
		t.Errorf("status with a new field %+v %v", st, err)
	}
	if _, err := ParseStatus("ok N:3"); err == nil {
		t.Error("an answer with no D read as a status")
	}

	if r, s, ok := ParseProgress(phenomProgress); !ok || r != 78174460 || s != 79439932 {
		t.Errorf("progress %d %d %v", r, s, ok)
	}
	if _, _, ok := ParseProgress("Not SD printing"); ok {
		t.Error("progress of a board with no print")
	}
}

// The client and the fake board: the answers, a board that does not answer, and one query at a
// time.
func TestClientFakeBoard(t *testing.T) {
	b, err := NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Set(func(b *FakeBoard) { b.Read, b.Size, b.Z, b.Seconds = 500, 1000, 12.5, 600 })
	c := &Client{Addr: b.Addr(), Timeout: 300 * time.Millisecond}
	ctx := context.Background()

	in, err := c.Info(ctx)
	if err != nil || in.Name != "CBD" || in.IP != "127.0.0.1" {
		t.Fatalf("info %+v %v", in, err)
	}
	st, err := c.Status(ctx)
	if err != nil || st.Z != 12.5 || st.Progress() != 0.5 || st.Seconds != 600 {
		t.Fatalf("status %+v %v", st, err)
	}

	b.Drop(1)
	if _, err := c.Status(ctx); !errors.Is(err, ErrTimeout) {
		t.Errorf("a board that does not answer: %v", err)
	}
	if _, err := c.Status(ctx); err != nil {
		t.Errorf("the next query: %v", err)
	}

	// Queries from many goroutines go one at a time: each gets its own answer.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Status(ctx); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a query at the same time: %v", err)
	}

	// The board got only queries that read.
	for _, q := range b.Log() {
		if q != "M99999" && q != "M4000" {
			t.Errorf("a query that is not a read: %q", q)
		}
	}
}

// Discover finds the boards that answer M99999, once each, with the address of the answer.
func TestDiscover(t *testing.T) {
	var addrs []string
	for range 2 {
		b, err := NewFakeBoard("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		addrs = append(addrs, b.Addr())
	}
	// The second board twice: one answer for each MAC address.
	got, err := Discover(context.Background(), 300*time.Millisecond, addrs[0], addrs[1], addrs[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "CBD" {
		t.Fatalf("boards %+v", got) // the fake boards have the same MAC address
	}
	if got[0].IP != addrs[0] && got[0].IP != addrs[1] {
		t.Errorf("the address of the answer %q", got[0].IP)
	}
}

// The file list of M20: the lines of the Phenom, with the folders and the files of macOS left out
// by Printable.
func TestFiles(t *testing.T) {
	b, err := NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Files = []string{"CHITUBOX 0", "._shoulders-flex.cbddlp 4096", "shoulders-flex.cbddlp 39815689",
		"bittlex pod cover.cbddlp 159456774", ".Trashes 0"}
	c := &Client{Addr: b.Addr(), Timeout: 300 * time.Millisecond}
	files, err := c.Files(context.Background())
	if err != nil || len(files) != 5 {
		t.Fatalf("files %+v %v", files, err)
	}
	var ok []string
	for _, f := range files {
		if f.Printable() {
			ok = append(ok, f.Name)
		}
	}
	if len(ok) != 2 || ok[0] != "shoulders-flex.cbddlp" || ok[1] != "bittlex pod cover.cbddlp" || files[2].Size != 39815689 {
		t.Errorf("printable %v of %+v", ok, files)
	}
}
