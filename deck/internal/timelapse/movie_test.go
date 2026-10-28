package timelapse

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The reader finds each frame of a video of WriteMOV by its offset, with the size and the rate.
func TestReadMovie(t *testing.T) {
	r := NewRecorder()
	var frames [][]byte
	for i := range 12 {
		f := testJPEG(t, 64, 48, uint8(20*i))
		frames = append(frames, f)
		r.Add(f, image.Pt(64, 48), i+1)
	}
	var b bytes.Buffer
	if err := r.WriteVideo(&b); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMovie(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	fps := Rate(12)
	if m.W != 64 || m.H != 48 || m.FPS != fps || m.Len() != 12+fps {
		t.Fatalf("%dx%d, %d fps, %d frames", m.W, m.H, m.FPS, m.Len())
	}
	if m.Length() != time.Duration(12+fps)*time.Second/time.Duration(fps) {
		t.Errorf("length %v", m.Length())
	}
	for i := range m.Len() {
		got, err := m.JPEG(i)
		if err != nil {
			t.Fatal(err)
		}
		want := frames[min(i, 11)]
		if !bytes.Equal(got, want) {
			t.Fatalf("frame %d: %d bytes, want %d", i, len(got), len(want))
		}
	}
	if _, err := m.JPEG(m.Len()); err == nil {
		t.Error("a frame after the last")
	}

	// A file: the picture has the JPEG of the file, and a decode at the size of the screen.
	path := filepath.Join(t.TempDir(), "clip.mov")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := OpenMovie(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fm.Close()
	p, err := fm.Frame(3, 16)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.JPEG(), frames[3]) || p.Bounds().Dx() != 64 {
		t.Error("the picture does not have the JPEG of the file")
	}
	if w := p.Picture().Bounds().Dx(); w != 16 {
		t.Errorf("decode width %d, want 16 (1/4)", w)
	}

	// Bad files.
	for name, bad := range map[string][]byte{"empty": nil, "no index": b.Bytes()[:b.Len()/2], "text": []byte("not a video at all")} {
		if _, err := ReadMovie(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// decodeShift picks the smallest decode with the pixels of the screen.
func TestDecodeShift(t *testing.T) {
	for _, c := range []struct{ w, screen, want int }{
		{1280, 1280, 0}, {1280, 810, 0}, {1280, 750, 1}, {1280, 640, 1}, {1280, 320, 2}, {1280, 100, 3}, {1280, 200, 3}, {1280, 201, 2},
	} {
		if got := decodeShift(c.w, c.screen); got != c.want {
			t.Errorf("decodeShift(%d, %d) = %d, want %d", c.w, c.screen, got, c.want)
		}
	}
}
