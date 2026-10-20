package virtual_sdcard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/klipper/extras/aio_executor"
)

// TestReadSplitsCharacters reads a file in small parts. A UTF-8 character across two reads must
// come out whole.
func TestReadSplitsCharacters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.gcode")
	text := "M117 héllo wörld ☃\nG1 X1\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &sdFile{name: path, f: f, executor: aio_executor.Direct()}
	var b strings.Builder
	for {
		data, err := s.read(3)
		if err != nil {
			t.Fatal(err)
		}
		if data == "" {
			break
		}
		b.WriteString(data)
	}
	if b.String() != text {
		t.Fatalf("got %q, want %q", b.String(), text)
	}
}

func TestFileList(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.gcode", "A.g", ".hidden.gcode", "notes.txt", "sub/c.gco"} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("G28\n"), 0o644)
	}
	v := &VirtualSD{sdcardDirname: dir}
	var got []string
	for _, f := range v.FileList(false) {
		got = append(got, f.Name)
	}
	// The list without subdirectories has every file that is not hidden.
	if strings.Join(got, ",") != "A.g,b.gcode,notes.txt" {
		t.Fatalf("FileList(false) = %v", got)
	}
	got = nil
	for _, f := range v.FileList(true) {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != ".hidden.gcode,A.g,b.gcode,sub/c.gco" {
		t.Fatalf("FileList(true) = %v", got)
	}
}
