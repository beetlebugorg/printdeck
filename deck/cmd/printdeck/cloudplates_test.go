package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/chitu"
	"printdeck/internal/cloud"
	"printdeck/internal/printer"
)

// A resin plate goes into the copies of the deck and then to the board, under its name of the
// plate. The board gets no M6030: a queued plate prints only after a press on the deck. A file
// with another SHA-256 does not go.
func TestResinPlate(t *testing.T) {
	b, err := chitu.NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := printer.NewChiTu(b.Addr(), "Phenom", &printer.History{})
	c.UploadDir = t.TempDir()
	data := bytes.Repeat([]byte("layer"), 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer srv.Close()
	sum := sha256.Sum256(data)
	pl := cloud.SentPlate{Build: "b1", Plate: 0, File: "plate-1.cbddlp", URL: srv.URL, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}

	bad := pl
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := resinPlate(c)(context.Background(), bad); err == nil {
		t.Fatal("a file with another SHA-256 went")
	}
	name, err := resinPlate(c)(context.Background(), pl)
	if err != nil || name != "b1-plate-1.cbddlp" {
		t.Fatalf("name %q, %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(c.UploadDir, name)); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		got := false
		b.Set(func(b *chitu.FakeBoard) { got = bytes.Equal(b.Uploaded[name], data) })
		if got {
			break
		}
		if i == 300 {
			t.Fatal("the board did not get the file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, q := range b.Log() {
		if strings.HasPrefix(q, "M6030") {
			t.Fatalf("the board got %q", q)
		}
	}
}
