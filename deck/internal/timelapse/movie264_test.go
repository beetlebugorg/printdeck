package timelapse

import (
	"errors"
	"testing"

	"printdeck/internal/h264"
)

// An MP4 with an H.264 track (as the time lapse of a Bambu Lab printer, here 30 frames of 320 x
// 240 at 10 frames a second, a sync sample each 5 frames) opens, and each frame decodes: in order,
// back to an earlier frame, and on to a frame after a sync sample.
func TestMovieH264(t *testing.T) {
	m, err := OpenMovie("testdata/h264.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.W != 320 || m.H != 240 || m.FPS != 10 || m.Len() != 30 || m.avc == nil || len(m.avc.sync) != 6 {
		t.Fatalf("%dx%d %d fps, %d frames, avc %+v", m.W, m.H, m.FPS, m.Len(), m.avc)
	}
	if _, err := h264.New(1); errors.Is(err, h264.ErrNoDecoder) {
		t.Skip("no H.264 decoder (the tag h264)")
	}
	for _, i := range []int{0, 1, 2, 3, 12, 11, 29, 7} {
		p, err := m.Frame(i, 320)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if b := p.Picture().Bounds(); b.Dx() != 320 || b.Dy() != 240 {
			t.Errorf("frame %d: %v", i, b)
		}
		if j := p.JPEG(); len(j) < 100 || j[0] != 0xff || j[1] != 0xd8 {
			t.Errorf("frame %d: no JPEG", i)
		}
	}
	// A small screen gets a smaller picture.
	p, _ := m.Frame(4, 80)
	if b := p.Picture().Bounds(); b.Dx() >= 320 {
		t.Errorf("a small screen: %v", b)
	}
}
