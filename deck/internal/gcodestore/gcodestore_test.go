package gcodestore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveListOpenDelete(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Save("old.gcode", strings.NewReader("G28\n")); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(s.Dir, "old.gcode"), past, past)
	if p, err := s.Save("parts/new.gcode", strings.NewReader("G1 X1\n")); err != nil || p != "parts/new.gcode" {
		t.Fatalf("save: %q, %v", p, err)
	}
	s.Save("cap.stl", strings.NewReader("solid"))

	files := s.Files()
	if len(files) != 2 || files[0].Path != "parts/new.gcode" || files[1].Path != "old.gcode" {
		t.Fatalf("files %+v, want the new file first and no STL", files)
	}
	if m := s.Models(); len(m) != 1 || m[0].Name != "cap.stl" {
		t.Errorf("models %+v", m)
	}
	r, err := s.Open("parts/new.gcode")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "G1 X1\n" {
		t.Errorf("content %q", b)
	}
	if err := s.Delete("old.gcode"); err != nil || len(s.Files()) != 1 {
		t.Errorf("delete: %v, files %d", err, len(s.Files()))
	}
}

func TestNamesStayInTheStore(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	for _, bad := range []string{"../escape.gcode", "a/../b.gcode", "/etc/passwd", "notes.txt", ".hidden.gcode",
		"a/.b/c.gcode", "a//b.gcode", ""} {
		if _, err := s.Save(bad, strings.NewReader("x")); !errors.Is(err, ErrBadName) {
			t.Errorf("save %q: %v, want ErrBadName", bad, err)
		}
	}
	// A leading slash is allowed: OctoPrint paths can start with one.
	if p, err := s.Save("/b.gcode", strings.NewReader("x")); err != nil || p != "b.gcode" {
		t.Errorf("save /b.gcode: %q, %v", p, err)
	}
}

func TestBrokenUploadKeepsTheOldFile(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	s.Save("part.gcode", strings.NewReader("good"))
	if _, err := s.Save("part.gcode", io.MultiReader(strings.NewReader("half"), errReader{})); err == nil {
		t.Fatal("a broken upload gave no error")
	}
	b, _ := os.ReadFile(filepath.Join(s.Dir, "part.gcode"))
	if string(b) != "good" {
		t.Errorf("the old file is %q after a broken upload", b)
	}
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 1 {
		t.Errorf("a temporary file stayed: %d entries", len(entries))
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection lost") }

func TestSymlinkCanNotLeaveTheStore(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.gcode"), []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(s.Dir, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := s.Save("link/evil.gcode", strings.NewReader("x")); err == nil {
		t.Error("save through a symlink out of the store gave no error")
	}
	if _, err := os.Stat(filepath.Join(outside, "evil.gcode")); err == nil {
		t.Error("save wrote a file outside the store")
	}
	if r, err := s.Open("link/secret.gcode"); err == nil {
		r.Close()
		t.Error("open read a file outside the store")
	}
	if err := s.Delete("link/secret.gcode"); err == nil {
		t.Error("delete removed a file outside the store")
	}
}
