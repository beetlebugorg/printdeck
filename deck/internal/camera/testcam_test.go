package camera

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// With PRINTDECK_TEST_CAMERA, Auto picks the test camera, which sends moving 960 x 540 frames.
func TestTestCamera(t *testing.T) {
	t.Setenv(TestCameraEnv, "1")
	h := NewHub(Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go h.Run(ctx)
	stop := h.Watch()
	defer stop()
	f, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sz := f.Size(); sz.X != 960 || sz.Y != 540 {
		t.Errorf("size %v, want 960x540", sz)
	}
	if st := h.Status(); st.State != "on" || st.Device.ID != TestCamera.ID || st.Format.OutW != 960 {
		t.Errorf("status %+v", st)
	}
	g, err := h.Next(ctx, f.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if g.Seq <= f.Seq || bytes.Equal(f.JPEG(), g.JPEG()) {
		t.Errorf("the second frame is the same as the first (seq %d, %d)", f.Seq, g.Seq)
	}
	if f.Blank() {
		t.Error("the test picture is blank")
	}
}

// Without PRINTDECK_TEST_CAMERA there is no test camera.
func TestNoTestCamera(t *testing.T) {
	t.Setenv(TestCameraEnv, "")
	for _, d := range NewHub(Settings{}).Devices() {
		if d.ID == TestCamera.ID {
			t.Fatal("the test camera is in the list")
		}
	}
}

// With PRINTDECK_TEST_CAMERA=2, the deck finds two test cameras of one model, with two pictures.
func TestTwoTestCameras(t *testing.T) {
	t.Setenv(TestCameraEnv, "2")
	h := NewHub(Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go h.Run(ctx)
	list := h.Cameras()
	if len(list) < 2 || list[0].Name != "Test camera 1" || list[1].Name != "Test camera 2" {
		t.Fatalf("list %+v", list)
	}
	a, _ := h.Source("1")
	b, _ := h.Source("2")
	fa, err := a.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := b.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fa.JPEG(), fb.JPEG()) {
		t.Error("the two test cameras send the same picture")
	}
}

// The test camera turns its picture: a turn of 90° or 270° makes tall frames of 540 x 960.
func TestTestCameraTurn(t *testing.T) {
	t.Setenv(TestCameraEnv, "1")
	for _, rot := range []int{90, 180, 270} {
		h := NewHub(Settings{Cameras: []CamSettings{{ID: TestCamera.ID, Rotate: rot}}})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		go h.Run(ctx)
		stop := h.Watch()
		f, err := h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		w, ht := TurnedSize(960, 540, rot)
		if sz := f.Size(); sz.X != w || sz.Y != ht {
			t.Errorf("turn %d: size %v, want %dx%d", rot, sz, w, ht)
		}
		if st := h.Status(); st.Format.OutW != w || st.Format.OutH != ht {
			t.Errorf("turn %d: format %+v", rot, st.Format)
		}
		if p := f.Picture(); p.Bounds().Dx() != w {
			t.Errorf("turn %d: picture %v", rot, p.Bounds())
		}
		stop()
		cancel()
	}
}
