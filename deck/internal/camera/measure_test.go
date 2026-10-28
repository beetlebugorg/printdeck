package camera

import (
	"context"
	"math"
	"testing"
	"time"
)

// testHub returns a hub with the first test camera, and runs it until the end of the test.
func testHub(t *testing.T, s Settings) *Hub {
	t.Helper()
	t.Setenv(TestCameraEnv, "1")
	h := NewHub(s)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	return h
}

// The measurement finds the map of the test camera: the zoom factor of each zoom value, and the
// move of the crop for each unit of pan and tilt.
func TestMeasurePTZ(t *testing.T) {
	ptzSettle = 0
	t.Cleanup(func() { ptzSettle = 700 * time.Millisecond })
	for _, rot := range []int{0, 90} {
		h := testHub(t, Settings{Cameras: []CamSettings{{ID: TestCamera.ID, Rotate: rot}}})
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		m, err := h.MeasurePTZ(ctx, TestCamera.ID, nil)
		cancel()
		if err != nil {
			t.Fatalf("turn %d: %v", rot, err)
		}
		t.Logf("turn %d: %.1f s: %+v", rot, time.Since(start).Seconds(), *m)
		if m.Rotate != rot {
			t.Errorf("turn %d: map turn %d", rot, m.Rotate)
		}
		for z := 100; z <= 400; z += 50 {
			if got, want := m.Factor(z), float64(z)/100; math.Abs(got-want)/want > 0.01 {
				t.Errorf("turn %d: zoom %d: factor %.4f, want %.4f", rot, z, got, want)
			}
		}
		// The truth in the frames of the turn: the moves of the scene, turned.
		pan, tilt := [2]float64{testPanShift, 0}, [2]float64{0, testTiltShift}
		if rot == 90 {
			// (u, v) of the scene is (1 - v, u) in the frame.
			pan, tilt = [2]float64{0, testPanShift}, [2]float64{-testTiltShift, 0}
		}
		for i, c := range []struct {
			got, want [2]float64
		}{{m.Pan, pan}, {m.Tilt, tilt}} {
			n := math.Hypot(c.want[0], c.want[1])
			if d := math.Hypot(c.got[0]-c.want[0], c.got[1]-c.want[1]); d > 0.02*n {
				t.Errorf("turn %d: axis %d: %v, want %v (error %.1f %%)", rot, i, c.got, c.want, 100*d/n)
			}
		}
		if got := h.Settings().Camera(TestCamera.ID).PTZMap; got == nil || got.Rotate != rot {
			t.Errorf("turn %d: the settings have the map %+v", rot, got)
		}
		// The camera is back at its position.
		if p, _ := h.PTZ(TestCamera.ID); p.Pos != p.Wide() {
			t.Errorf("turn %d: position %+v after the measurement", rot, p.Pos)
		}
	}
}
