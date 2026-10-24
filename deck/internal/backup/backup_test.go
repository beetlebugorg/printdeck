package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// deck makes the folders of a deck in a temporary folder: the state, with its password, and the
// Klipper config.
func deck(t *testing.T) (state, config string, sources []Source) {
	t.Helper()
	root := t.TempDir()
	state, config = filepath.Join(root, "state"), filepath.Join(root, "config")
	write(t, state, "spools.json", `[{"id":1}]`)
	write(t, state, "history.json", `[]`)
	write(t, state, "api.env", "OCTOPRINT_API_KEY=abc\n")
	write(t, state, "push.json", `{"phones":[]}`)
	write(t, state, "models/benchy.stl", noise(200000)) // more than one chunk after gzip
	write(t, state, "password", "old words\n")
	write(t, state, "logs/deck.log", "log\n")
	write(t, config, "printer.cfg", "[printer]\n")
	write(t, config, "macros/pause.cfg", "[gcode_macro P]\n")
	return state, config, Sources(state, config, "")
}

// Sources is the list of the deck for the tests: the state without the password and the logs,
// and the config.
func Sources(state, config, gcodes string) []Source {
	s := []Source{
		{Prefix: "state", Dir: state, Skip: func(rel string) bool {
			return rel == "password" || rel == "logs" || strings.HasPrefix(rel, "logs/")
		}},
		{Prefix: "config", Dir: config},
	}
	if gcodes != "" {
		s = append(s, Source{Prefix: "gcodes", Dir: gcodes})
	}
	return s
}

// noise is text that gzip cannot make much smaller.
func noise(n int) string {
	b := make([]byte, n)
	x := uint32(1)
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = 'a' + byte(x>>24)%26
	}
	return string(b)
}

func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRoundTrip(t *testing.T) {
	state, config, sources := deck(t)
	var buf bytes.Buffer
	if err := Write(&buf, "correct horse", sources, nil); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("OCTOPRINT_API_KEY")) {
		t.Fatal("the backup has the API key in clear text")
	}
	want := map[string]string{}
	for _, d := range []struct{ dir, rel string }{{state, "spools.json"}, {state, "history.json"}, {state, "api.env"},
		{state, "push.json"}, {state, "models/benchy.stl"}, {config, "printer.cfg"}, {config, "macros/pause.cfg"}} {
		want[d.dir+"|"+d.rel] = read(t, d.dir, d.rel)
	}
	// A new deck: other data, its own password, one file that the backup does not have.
	os.RemoveAll(state)
	os.RemoveAll(config)
	write(t, state, "spools.json", `[]`)
	write(t, state, "password", "new words\n")
	write(t, state, "theme", "night\n")
	write(t, config, "printer.cfg", "[other]\n")

	if err := Restore(&buf, "correct horse", sources, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	for k, v := range want {
		dir, rel, _ := strings.Cut(k, "|")
		if got := read(t, dir, rel); got != v {
			t.Errorf("%s: got %d bytes, want %d", rel, len(got), len(v))
		}
	}
	if got := read(t, state, "password"); got != "new words\n" {
		t.Errorf("the password changed to %q", got)
	}
	if got := read(t, state, "theme"); got != "night\n" {
		t.Errorf("a file that is not in the backup changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(state, "logs")); err == nil {
		t.Error("the logs came back from the backup")
	}
	if fi, _ := os.Stat(filepath.Join(state, "api.env")); fi.Mode().Perm() != 0o600 {
		t.Errorf("api.env has mode %v", fi.Mode().Perm())
	}
}

// restoreFails restores a backup that must fail, and checks that the deck did not change.
func restoreFails(t *testing.T, b []byte, password string, sources []Source, state string) error {
	t.Helper()
	write(t, state, "spools.json", "deck\n")
	err := Restore(bytes.NewReader(b), password, sources, t.TempDir(), nil)
	if err == nil {
		t.Fatal("the restore did not fail")
	}
	if got := read(t, state, "spools.json"); got != "deck\n" {
		t.Errorf("a failed restore changed spools.json to %q", got)
	}
	return err
}

func TestWrongPassword(t *testing.T) {
	state, _, sources := deck(t)
	var buf bytes.Buffer
	if err := Write(&buf, "correct horse", sources, nil); err != nil {
		t.Fatal(err)
	}
	if err := restoreFails(t, buf.Bytes(), "wrong horse", sources, state); !errors.Is(err, ErrPassword) {
		t.Errorf("got %v, want ErrPassword", err)
	}
}

func TestDamagedFile(t *testing.T) {
	state, _, sources := deck(t)
	var buf bytes.Buffer
	if err := Write(&buf, "correct horse", sources, nil); err != nil {
		t.Fatal(err)
	}
	good := buf.Bytes()
	if len(good) < headerLen+chunkSize+100 {
		t.Fatalf("the backup has one chunk only (%d bytes)", len(good))
	}
	// A changed byte in the last chunk: the files of the first chunk decrypt, then the restore
	// fails before it changes a file.
	b := bytes.Clone(good)
	b[len(b)-20] ^= 1
	restoreFails(t, b, "correct horse", sources, state)
	// A changed header.
	b = bytes.Clone(good)
	b[len(magic)+5] ^= 1
	restoreFails(t, b, "correct horse", sources, state)
	// A cut file: the final chunk is missing.
	restoreFails(t, good[:headerLen+chunkSize+tagSize], "correct horse", sources, state)
	// Not a backup.
	restoreFails(t, []byte("hello"), "correct horse", sources, state)
}

// encrypted makes a backup with the entries of a hand-made archive.
func encrypted(t *testing.T, password string, entries []tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	ew, err := newWriter(&buf, password)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(ew)
	tw := tar.NewWriter(gz)
	m := []byte(`{"version":1}`)
	tw.WriteHeader(&tar.Header{Name: manifestName, Size: int64(len(m)), Mode: 0o600, Typeflag: tar.TypeReg, ModTime: time.Now()})
	tw.Write(m)
	for _, h := range entries {
		h.ModTime = time.Now()
		if h.Typeflag == tar.TypeReg {
			h.Size = 3
		}
		tw.WriteHeader(&h)
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte("bad"))
		}
	}
	tw.Close()
	gz.Close()
	ew.Close()
	return buf.Bytes()
}

func TestBadPaths(t *testing.T) {
	state, _, sources := deck(t)
	for _, h := range []tar.Header{
		{Name: "state/../../escape", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "/etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "other/file", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "state/./spools.json", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "state/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		b := encrypted(t, "pw", []tar.Header{{Name: "state/spools.json", Typeflag: tar.TypeReg, Mode: 0o644}, h})
		restoreFails(t, b, "pw", sources, state)
		if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(state)), "escape")); err == nil {
			t.Fatalf("%s: a file was written outside the deck", h.Name)
		}
	}
}

// A backup with the password file does not replace the password of the deck.
func TestSkippedFileStays(t *testing.T) {
	state, _, sources := deck(t)
	b := encrypted(t, "pw", []tar.Header{{Name: "state/password", Typeflag: tar.TypeReg, Mode: 0o600},
		{Name: "state/theme", Typeflag: tar.TypeReg, Mode: 0o600}})
	if err := Restore(bytes.NewReader(b), "pw", sources, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, state, "password"); got != "old words\n" {
		t.Errorf("the password changed to %q", got)
	}
	if got := read(t, state, "theme"); got != "bad" {
		t.Errorf("theme is %q", got)
	}
}

// An archive that ends at a chunk boundary still has its final chunk.
func TestChunkBoundary(t *testing.T) {
	for _, n := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 2 * chunkSize} {
		var buf bytes.Buffer
		ew, err := newWriter(&buf, "pw")
		if err != nil {
			t.Fatal(err)
		}
		data := bytes.Repeat([]byte{7}, n)
		ew.Write(data)
		ew.Close()
		er, err := newReader(&buf, "pw")
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if _, err := out.ReadFrom(er); err != nil {
			t.Fatalf("%d bytes: %v", n, err)
		}
		if !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("%d bytes: got %d back", n, out.Len())
		}
	}
}
