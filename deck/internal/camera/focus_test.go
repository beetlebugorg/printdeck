package camera

import (
	"context"
	"errors"
	"image"
	"math"
	"sync"
	"testing"
	"time"
)

// gridPicture is a picture of w x h with a grid of lines each 16 pixels.
func gridPicture(w, h int) *image.YCbCr {
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for y := range h {
		for x := range w {
			v := uint8(60)
			if x%16 < 2 || y%16 < 2 {
				v = 200
			}
			p.Y[y*p.YStride+x] = v
		}
	}
	return p
}

// The sharpness goes down with the blur, is 0 for a flat picture, and measures only the box.
func TestSharpness(t *testing.T) {
	p := gridPicture(320, 240)
	all := p.Rect
	sharp := sharpness(p, all)
	prev := sharp
	for r := 1; r <= 3; r++ {
		q := gridPicture(320, 240)
		copy(q.Y, boxBlur(q, r))
		s := sharpness(q, all)
		if s >= prev {
			t.Errorf("radius %d: sharpness %.1f, not less than %.1f", r, s, prev)
		}
		prev = s
	}
	flat := image.NewYCbCr(all, image.YCbCrSubsampleRatio420)
	if s := sharpness(flat, all); s != 0 {
		t.Errorf("flat picture: %.1f", s)
	}
	// The grid only at the left: a box at the right has no detail.
	half := gridPicture(320, 240)
	for y := range 240 {
		for x := 160; x < 320; x++ {
			half.Y[y*half.YStride+x] = 60
		}
	}
	if s := sharpness(half, image.Rect(170, 0, 320, 240)); s != 0 {
		t.Errorf("box with no detail: %.1f", s)
	}
	if s := sharpness(half, image.Rect(0, 0, 150, 240)); math.Abs(s-sharp) > sharp/10 {
		t.Errorf("box with the grid: %.1f, want about %.1f", s, sharp)
	}
	// A box out of the picture.
	if s := sharpness(p, image.Rect(400, 0, 500, 10)); s != 0 {
		t.Errorf("box out of the picture: %.1f", s)
	}
}

// A JPEG frame decodes at a smaller size when the box keeps focusDetail pixels of width.
func TestFrameSharpness(t *testing.T) {
	for _, tc := range []struct {
		r     image.Rectangle
		shift int // the decode at 1/2^shift of the size
	}{
		{image.Rect(0, 0, 1280, 720), 2},
		{image.Rect(320, 180, 960, 540), 1},
		{image.Rect(500, 250, 800, 500), 0},
	} {
		f, err := FrameOf(photoJPEG(t, 1280, 720))
		if err != nil {
			t.Fatal(err)
		}
		if s := f.sharpness(tc.r); s <= 0 {
			t.Fatalf("%v: sharpness %.1f", tc.r, s)
		}
		for i, p := range f.scaled {
			if (p != nil) != (i+1 == tc.shift) {
				t.Errorf("%v: decode at 1/%d: %v", tc.r, 2<<i, p != nil)
			}
		}
		if (f.pic != nil) != (tc.shift == 0) {
			t.Errorf("%v: whole decode %v", tc.r, f.pic != nil)
		}
	}
}

// curve is a sharpness curve of a lens with its peak at peak: a Gaussian of width w over a floor.
func curve(peak, w float64) func(int) float64 {
	return func(pos int) float64 {
		d := (float64(pos) - peak) / w
		return 10 + 1000*math.Exp(-d*d/2)
	}
}

// runCurve runs a search of the focus control f on the curve, and returns the best position and
// the number of positions.
func runCurve(f Focus, c func(int) float64) (int, int) {
	s := newFocusSearch(f)
	for {
		pos, ok := s.next()
		if !ok {
			break
		}
		s.add(pos, c(pos))
	}
	return s.best(), len(s.tries)
}

// The search finds the peak of a sharpness curve: in a range of 0 to 255, of 0 to 1023, with a
// step of 5, and at the ends of the range. It measures at most 14 positions.
func TestFocusSearchCurve(t *testing.T) {
	for _, tc := range []struct {
		f          Focus
		peak, w    float64
		tolerance  int
		wantFlat   bool
		wantClamps bool
	}{
		{f: Focus{Min: 0, Max: 255, Step: 1}, peak: 97, w: 30, tolerance: 3},
		{f: Focus{Min: 0, Max: 255, Step: 1}, peak: 200, w: 15, tolerance: 4},
		{f: Focus{Min: 0, Max: 1023, Step: 1}, peak: 300, w: 120, tolerance: 8},
		{f: Focus{Min: 0, Max: 1023, Step: 1}, peak: 712, w: 60, tolerance: 12},
		{f: Focus{Min: 0, Max: 250, Step: 5}, peak: 133, w: 25, tolerance: 5},
		{f: Focus{Min: 0, Max: 255, Step: 1}, peak: 0, w: 30, tolerance: 0},
		{f: Focus{Min: 0, Max: 255, Step: 1}, peak: 255, w: 30, tolerance: 0},
		{f: Focus{Min: 10, Max: 40, Step: 1}, peak: 22, w: 5, tolerance: 2},
	} {
		best, n := runCurve(tc.f, curve(tc.peak, tc.w))
		if d := math.Abs(float64(best) - tc.peak); d > float64(tc.tolerance) {
			t.Errorf("%+v, peak %.0f: best %d", tc.f, tc.peak, best)
		}
		if n > focusCoarse+focusFine {
			t.Errorf("%+v: %d positions", tc.f, n)
		}
		if best != tc.f.Clamp(best) {
			t.Errorf("%+v: %d is not on a step", tc.f, best)
		}
	}
}

// A curve with no peak is flat, and a curve with a peak is not.
func TestFocusSearchFlat(t *testing.T) {
	f := Focus{Min: 0, Max: 255, Step: 1}
	s := newFocusSearch(f)
	for {
		pos, ok := s.next()
		if !ok {
			break
		}
		s.add(pos, 100+float64(pos%3))
	}
	if !s.flat() {
		t.Error("a flat curve has a peak")
	}
	s = newFocusSearch(f)
	for {
		pos, ok := s.next()
		if !ok {
			break
		}
		s.add(pos, curve(120, 30)(pos))
	}
	if s.flat() {
		t.Error("a curve with a peak is flat")
	}
}

// The sweep starts at the end of the range that is farther from the lens, and goes in order.
func TestFocusSearchOrder(t *testing.T) {
	for _, tc := range []struct {
		pos       int
		wantFirst int
	}{{10, 255}, {200, 0}} {
		s := newFocusSearch(Focus{Min: 0, Max: 255, Step: 1, Pos: tc.pos})
		first, _ := s.next()
		if first != tc.wantFirst {
			t.Errorf("lens at %d: first %d, want %d", tc.pos, first, tc.wantFirst)
		}
		if len(s.queue) != focusCoarse {
			t.Errorf("lens at %d: %d positions in the sweep", tc.pos, len(s.queue))
		}
	}
}

// focusHub is a hub with the first test camera at 30 frames a second, which runs until the end of the
// test. saves counts the saves of the settings.
func focusHub(t *testing.T, s Settings) (h *Hub, saves func() int) {
	return lensHub(t, s, testLensSpeed)
}

// lensHub is focusHub with a lens that moves speed steps in a frame at most.
func lensHub(t *testing.T, s Settings, speed int) (h *Hub, saves func() int) {
	oldSave := focusSaveAfter
	focusSaveAfter = 50 * time.Millisecond
	t.Cleanup(func() { focusSaveAfter = oldSave })
	h = NewHub(s)
	fast := testFormat
	fast.InFPS, fast.OutFPS = 30, 30
	h.scan = func() []Device { return []Device{TestCamera} }
	h.capture = func(ctx context.Context, d Device, l link) error { return testCapture(ctx, l, false, fast, speed) }
	var mu sync.Mutex
	n := 0
	h.OnSettings = func(Settings) {
		mu.Lock()
		n++
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	// The capture reports the focus control.
	stop := h.Watch()
	t.Cleanup(stop)
	if _, err := h.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	return h, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

// The Focus on the part key finds the sharp position of the test camera, and saves it in the
// settings as a fixed focus.
func TestFocusOnPart(t *testing.T) {
	h, saves := focusHub(t, Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var last, all int
	start := time.Now()
	pos, err := h.FocusOnPart(ctx, TestCamera.ID, func(done, n int) { last, all = done, n })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("best %d in %v, %d of %d positions, settle %d frames", pos, time.Since(start).Round(time.Millisecond),
		last, all, h.Cam(TestCamera.ID).settle)
	if d := pos - testFocus.Pos; d < -20 || d > 20 {
		t.Errorf("best %d, want about %d", pos, testFocus.Pos)
	}
	if last != all || all < focusCoarse {
		t.Errorf("progress %d of %d", last, all)
	}
	cs := h.Settings().Camera(TestCamera.ID)
	if !cs.FixedFocus || cs.Focus != pos {
		t.Errorf("settings %+v", cs)
	}
	if h.lens[TestCamera.ID] != 0 || len(h.lens) != 0 {
		t.Errorf("lens positions after the search: %v", h.lens)
	}
	deadline := time.Now().Add(2 * time.Second)
	for saves() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if saves() != 1 {
		t.Errorf("%d saves, want 1", saves())
	}
	if st := h.Cam(TestCamera.ID).Status(); st.Focus == nil || st.Focus.Auto || st.Focus.Pos != pos || st.Searching {
		t.Errorf("status %+v", st.Focus)
	}
}

// The deck measures the settle time of a slow lens, and finds the sharp position with it.
func TestFocusSlowLens(t *testing.T) {
	h, _ := lensHub(t, Settings{}, 60)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pos, err := h.FocusOnPart(ctx, TestCamera.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := pos - testFocus.Pos; d < -20 || d > 20 {
		t.Errorf("best %d, want about %d", pos, testFocus.Pos)
	}
	// The move to the first fine position is about 220 steps: 4 frames.
	if s := h.Cam(TestCamera.ID).settle; s < 3 {
		t.Errorf("settle %d frames, want 3 or more", s)
	}
}

// The searches of a print: after the first layer and each 20 mm, with the result in memory and no
// save. A focus change of the user stops them until the end of the print, and the setting turns
// them off.
func TestFocusPrint(t *testing.T) {
	h, saves := focusHub(t, Settings{})
	ctx := context.Background()
	id := TestCamera.ID
	if n := h.FocusPrint(ctx, 0, 0.2); n != 0 {
		t.Fatalf("%d searches in the first layer", n)
	}
	if n := h.FocusPrint(ctx, 1, 0.4); n != 1 {
		t.Fatalf("%d searches after the first layer", n)
	}
	pos, ok := h.lens[id]
	if !ok || pos < testFocus.Pos-20 || pos > testFocus.Pos+20 {
		t.Fatalf("lens %d %v, want about %d", pos, ok, testFocus.Pos)
	}
	if fixed, p := h.FocusSetting(id); !fixed || p != pos {
		t.Errorf("focus setting %v %d", fixed, p)
	}
	if cs := h.Settings().Camera(id); cs.FixedFocus {
		t.Errorf("the search changed the settings: %+v", cs)
	}
	if n := h.FocusPrint(ctx, 30, 12); n != 0 {
		t.Errorf("%d searches at 12 mm", n)
	}
	if n := h.FocusPrint(ctx, 60, 20.5); n != 1 {
		t.Errorf("%d searches at 20.5 mm", n)
	}
	time.Sleep(3 * focusSaveAfter)
	if saves() != 0 {
		t.Errorf("%d saves of the settings", saves())
	}
	// A focus change of the user.
	h.SetFocus(id, true, 500)
	if _, ok := h.lens[id]; ok {
		t.Error("lens position after a focus change of the user")
	}
	if n := h.FocusPrint(ctx, 200, 80); n != 0 {
		t.Errorf("%d searches after a focus change of the user", n)
	}
	// The next print.
	h.EndFocusPrint()
	if n := h.FocusPrint(ctx, 1, 0.3); n != 1 {
		t.Errorf("%d searches in the next print", n)
	}
	// The setting off.
	h.EndFocusPrint()
	s := h.Settings()
	cs := s.Camera(id)
	cs.NoFocusPart = true
	s.SetCamera(cs)
	h.SetSettings(s)
	if n := h.FocusPrint(ctx, 1, 0.3); n != 0 {
		t.Errorf("%d searches with the setting off", n)
	}
}

// A search that does not fit in a park goes on at the next park, with the lens at the best
// position so far between the two.
func TestFocusPrintResume(t *testing.T) {
	h, _ := focusHub(t, Settings{})
	id := TestCamera.ID
	h.StartFocusPrint()
	park, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	n := h.FocusPrint(park, 1, 0.3)
	cancel()
	if n != 0 {
		t.Fatalf("%d searches end in a short park", n)
	}
	run := h.runs[id]
	if run == nil || run.cancel != nil || len(run.s.tries) == 0 || run.s.done() {
		t.Fatalf("no search waits: %+v", run)
	}
	tried := len(run.s.tries)
	if _, ok := h.lens[id]; !ok {
		t.Error("no lens position between the parks")
	}
	if n := h.FocusPrint(context.Background(), 2, 0.6); n != 1 {
		t.Fatalf("%d searches at the next park", n)
	}
	if pos := h.lens[id]; pos < testFocus.Pos-20 || pos > testFocus.Pos+20 {
		t.Errorf("lens %d, want about %d", pos, testFocus.Pos)
	}
	if h.runs[id] != nil {
		t.Error("the search did not end")
	}
	if h.focusDone[id] != 0.3 {
		t.Errorf("height of the search %.1f, want 0.3: the start of the search", h.focusDone[id])
	}
	t.Logf("%d positions in the first park", tried)
}

// A search ends at a focus change of the user, and a second search of a camera waits for none.
func TestFocusStop(t *testing.T) {
	h, _ := focusHub(t, Settings{})
	id := TestCamera.ID
	errc := make(chan error, 1)
	go func() {
		_, err := h.FocusOnPart(context.Background(), id, nil)
		errc <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !h.Cam(id).Status().Searching && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := h.FocusOnPart(context.Background(), id, nil); !errors.Is(err, ErrFocusBusy) {
		t.Errorf("second search: %v", err)
	}
	h.SetFocus(id, true, 700)
	if err := <-errc; err == nil {
		t.Error("the search did not stop")
	}
	if cs := h.Settings().Camera(id); !cs.FixedFocus || cs.Focus != 700 {
		t.Errorf("settings %+v", cs)
	}
	if fixed, pos := h.FocusSetting(id); !fixed || pos != 700 {
		t.Errorf("focus %v %d", fixed, pos)
	}
}
