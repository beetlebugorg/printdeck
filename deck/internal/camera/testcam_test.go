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
