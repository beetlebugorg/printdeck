package camera

import (
	"context"
	"errors"
	"image"
	"log"
	"math"
	"sort"
	"time"
)

// Focus on the part: a contrast search of the lens position that the deck runs once, so the
// lens stays at one position between two searches and never hunts. The search moves the lens
// over the range of the focus control in a coarse sweep, and then to a few positions near the
// best position of the sweep (the fine search). At each position it waits until the lens
// settles, and measures the sharpness of the part in one or two frames. The lens then goes to
// the peak of a parabola through the best position and its two neighbors.
//
// The sharpness is the variance of the Laplacian of the luma in the box of the part, with a
// small margin. With no calibration, or with no print, the box is the middle of the picture. A
// frame decodes its JPEG at a smaller size when the box keeps focusDetail pixels of width, and
// the deck decodes only the frames that it measures.
//
// The settle time of the lens is the number of frames from the move to a steady sharpness. The
// deck measures it after the coarse sweep of the first search of a camera: the lens goes back
// from the end of the sweep to the best position of the sweep, which has a known sharpness. At a
// position far from the peak the picture is so blurred that the sharpness hardly changes, so a
// move there shows no settle time. The coarse sweep before the measurement waits
// focusSettleFirst frames at each position. With a longer settle time, the sweep starts again.
//
// The positions of a search replace the focus of the settings (Hub.lens), and the settings do
// not change: the settings file is on the SD card. A search of the Focus on the part key saves
// its result in the settings. A search of a print keeps its result in memory until the user
// changes the focus. The searches of a print run at the parks of the time lapse (FocusPrint),
// with the toolhead out of the picture. A search that does not fit in a park goes on at the next
// park.

// The search.
const (
	focusCoarse = 10 // the positions of the coarse sweep
	focusFine   = 4  // the positions of the fine search
	// focusFineFrames is the frames that the deck measures at each position of the fine search.
	// The coarse sweep measures one frame at each position.
	focusFineFrames = 2
	// focusFlat is the least ratio of the best to the worst sharpness of the coarse sweep. A
	// sweep with less has no clear peak: the picture has too little detail.
	focusFlat = 1.15
)

// The settle time of the lens.
const (
	focusSettleFirst = 2    // the frames that the deck waits for the lens before it measures the settle time
	focusSettleMax   = 5    // the most frames that the deck waits for the lens
	focusSteady      = 0.04 // the change of the sharpness between two frames of a lens that settled
	focusApplyMax    = 30   // the most frames that the deck waits for the capture to set a position
)

// The box of the part.
const (
	focusMargin = 0.05 // the margin around the box of the part, as a share of its size at each side
	focusDetail = 320  // the least width of the box in the decoded picture, in pixels
)

// The searches of a print.
const (
	// PartFocusEvery is the height of the print between two searches of a print, in mm.
	PartFocusEvery = 20.0
	// focusResume is the longest time that a search of a print waits for the next park.
	focusResume = 3 * time.Minute
)

// The errors of a search.
var (
	// ErrFocusBusy is the error of a search of a camera that has a search that runs.
	ErrFocusBusy  = errors.New("camera: a focus search runs")
	errNoFocusCtl = errors.New("camera: the camera has no focus control")
	errNoLens     = errors.New("camera: the camera does not set the lens position")
	errFlat       = errors.New("camera: the part has too little detail for a focus search")
	errStopped    = errors.New("camera: the focus search stopped")
)

// sharpness returns the variance of the Laplacian of the Y plane of p in the rectangle r. A
// sharper picture has a larger variance.
func sharpness(p *image.YCbCr, r image.Rectangle) float64 {
	// The Laplacian needs the four neighbors of each pixel.
	r = r.Intersect(p.Rect).Inset(1)
	if r.Empty() {
		return 0
	}
	var sum, sum2 int64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := p.YOffset(r.Min.X, y)
		row, up, down := p.Y[i:i+r.Dx()], p.Y[i-p.YStride:], p.Y[i+p.YStride:]
		left := p.Y[i-1:]
		var s, s2 int64
		for x, c := range row {
			l := 4*int64(c) - int64(left[x]) - int64(left[x+2]) - int64(up[x]) - int64(down[x])
			s += l
			s2 += l * l
		}
		sum += s
		sum2 += s2
	}
	n := float64(r.Dx() * r.Dy())
	mean := float64(sum) / n
	return float64(sum2)/n - mean*mean
}

// sharpness returns the sharpness of the frame in the rectangle r of its pixels. The frame decodes
// its JPEG at 1/2, 1/4 or 1/8 of its size when r keeps focusDetail pixels of width.
func (f *Frame) sharpness(r image.Rectangle) float64 {
	r = r.Intersect(image.Rect(0, 0, f.w, f.h))
	if r.Empty() {
		return 0
	}
	pic := f.pictureFor((focusDetail*f.w + r.Dx() - 1) / r.Dx())
	if pic == nil {
		return 0
	}
	k := float64(pic.Rect.Dx()) / float64(f.w)
	pr := image.Rect(int(float64(r.Min.X)*k), int(float64(r.Min.Y)*k),
		int(math.Ceil(float64(r.Max.X)*k)), int(math.Ceil(float64(r.Max.Y)*k)))
	return sharpness(pic, pr.Add(pic.Rect.Min))
}

// focusTry is a lens position of a search and its sharpness.
type focusTry struct {
	pos   int
	score float64
}

// focusSearch is the plan of a search: the positions to measure, and the sharpness at each
// position that the deck measured. It has no camera, so a search can stop at the end of a park
// and go on at the next park.
type focusSearch struct {
	f     Focus
	tries []focusTry
	queue []int // the positions to measure, the next first
	fine  bool  // the queue is the fine search
	gap   int   // the gap between two positions of the coarse sweep
	all   int   // the positions of the search
}

// newFocusSearch returns the search of the focus control f. The coarse sweep starts at the end
// of the range that is farther from the lens position now (f.Pos).
func newFocusSearch(f Focus) *focusSearch {
	s := &focusSearch{f: f}
	span := f.Max - f.Min
	n := min(focusCoarse, span/max(1, f.Step)+1)
	for i := range n {
		pos := f.Clamp(f.Min + i*span/max(1, n-1))
		if len(s.queue) == 0 || pos != s.queue[len(s.queue)-1] {
			s.queue = append(s.queue, pos)
		}
	}
	s.gap = span / max(1, n-1)
	if f.Pos-f.Min < f.Max-f.Pos {
		for i, j := 0, len(s.queue)-1; i < j; i, j = i+1, j-1 {
			s.queue[i], s.queue[j] = s.queue[j], s.queue[i]
		}
	}
	s.all = len(s.queue) + focusFine
	return s
}

// next returns the next position to measure, and false at the end of the search. After the
// coarse sweep it plans the fine search.
func (s *focusSearch) next() (int, bool) {
	if len(s.queue) == 0 && !s.fine {
		s.fine = true
		s.queue = s.finePositions()
		s.all = len(s.tries) + len(s.queue)
	}
	if len(s.queue) == 0 {
		return 0, false
	}
	return s.queue[0], true
}

// frames returns the number of frames to measure at the next position.
func (s *focusSearch) frames() int {
	if s.fine {
		return focusFineFrames
	}
	return 1
}

// add records the sharpness at the position pos, and removes pos from the queue.
func (s *focusSearch) add(pos int, score float64) {
	s.tries = append(s.tries, focusTry{pos, score})
	if len(s.queue) > 0 && s.queue[0] == pos {
		s.queue = s.queue[1:]
	}
}

// finePositions returns the positions of the fine search: focusFine positions between the
// neighbors of the best position of the coarse sweep, in the order of the range.
func (s *focusSearch) finePositions() []int {
	b, ok := s.bestTry()
	if !ok || s.gap <= 1 {
		return nil
	}
	tried := map[int]bool{}
	for _, t := range s.tries {
		tried[t.pos] = true
	}
	var out []int
	half := focusFine / 2
	for i := -half; i <= half; i++ {
		if i == 0 {
			continue
		}
		pos := s.f.Clamp(b.pos + i*s.gap/(half+1))
		if !tried[pos] {
			tried[pos] = true
			out = append(out, pos)
		}
	}
	sort.Ints(out)
	return out
}

// bestTry returns the position with the best sharpness, and false with no position.
func (s *focusSearch) bestTry() (focusTry, bool) {
	if len(s.tries) == 0 {
		return focusTry{}, false
	}
	b := s.tries[0]
	for _, t := range s.tries[1:] {
		if t.score > b.score {
			b = t
		}
	}
	return b, true
}

// done reports that the search measured each position.
func (s *focusSearch) done() bool {
	_, more := s.next()
	return !more
}

// flat reports that the coarse sweep has no clear peak.
func (s *focusSearch) flat() bool {
	if len(s.tries) == 0 {
		return true
	}
	lo, hi := s.tries[0].score, s.tries[0].score
	for _, t := range s.tries {
		lo, hi = min(lo, t.score), max(hi, t.score)
	}
	return hi <= 0 || hi < focusFlat*lo
}

// best returns the best lens position: the peak of the parabola through the best position and
// its two neighbors, or the best position at an end of the range. It is the lens position now
// (f.Pos) with no position.
func (s *focusSearch) best() int {
	b, ok := s.bestTry()
	if !ok {
		return s.f.Pos
	}
	// The nearest positions under and over the best position.
	var lo, hi *focusTry
	for i := range s.tries {
		t := &s.tries[i]
		if t.pos < b.pos && (lo == nil || t.pos > lo.pos) {
			lo = t
		}
		if t.pos > b.pos && (hi == nil || t.pos < hi.pos) {
			hi = t
		}
	}
	if lo == nil || hi == nil {
		return b.pos
	}
	x0, x1, x2 := float64(lo.pos), float64(b.pos), float64(hi.pos)
	y0, y1, y2 := lo.score, b.score, hi.score
	den := (x1-x0)*(y1-y2) - (x1-x2)*(y1-y0)
	if den == 0 {
		return b.pos
	}
	x := x1 - 0.5*((x1-x0)*(x1-x0)*(y1-y2)-(x1-x2)*(x1-x2)*(y1-y0))/den
	return s.f.Clamp(int(math.Round(max(x0, min(x2, x)))))
}

// focusRun is a search of a camera: a search that runs, or a search of a print that waits for
// the next park.
type focusRun struct {
	s      *focusSearch
	cancel func() // ends the search that runs, or nil while it waits
	print  bool   // a search of a print
	start  time.Time
	height float64 // the height of the print at the start, in mm
	// prev is the lens position of Hub.lens before the search, with hadPrev.
	prev    int
	hadPrev bool
}

// FocusOnPart runs a focus search on the part with the camera id, until ctx ends. It sets the
// best lens position as the fixed focus of the settings, and saves the settings. progress gets
// the positions that the search measured and the positions of the search. It can be nil.
func (h *Hub) FocusOnPart(ctx context.Context, id string, progress func(done, all int)) (int, error) {
	run, ctx, err := h.startRun(ctx, id, false, 0)
	if err != nil {
		return 0, err
	}
	pos, err := h.runSearch(ctx, id, run, progress)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runs[id] != run {
		return 0, errStopped
	}
	delete(h.runs, id)
	if err != nil {
		h.restoreLensLocked(id, run)
		return 0, err
	}
	delete(h.lens, id)
	s := h.settings.clone()
	cs := s.Camera(id)
	cs.FixedFocus, cs.Focus = true, pos
	s.SetCamera(cs)
	h.settings = s
	h.saveSoonLocked()
	log.Printf("camera: %s: focus on the part at %d", id, pos)
	return pos, nil
}

// startRun starts a search of the camera id, or the rest of a search of a print that waits. It
// returns the context of the search, which SetFocus ends.
func (h *Hub) startRun(ctx context.Context, id string, print bool, height float64) (*focusRun, context.Context, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	run := h.runs[id]
	switch {
	case run != nil && run.cancel != nil:
		return nil, nil, ErrFocusBusy
	case run != nil && (!print || !run.print || time.Since(run.start) > focusResume):
		h.restoreLensLocked(id, run)
		run = nil
	}
	if run == nil {
		run = &focusRun{print: print, start: time.Now(), height: height}
		run.prev, run.hadPrev = h.lens[id]
		if h.runs == nil {
			h.runs = map[string]*focusRun{}
		}
		h.runs[id] = run
	}
	ctx, cancel := context.WithCancel(ctx)
	run.cancel = cancel
	return run, ctx, nil
}

// restoreLensLocked sets the lens position of Hub.lens from before the search run.
func (h *Hub) restoreLensLocked(id string, run *focusRun) {
	if run.hadPrev {
		h.lens[id] = run.prev
	} else {
		delete(h.lens, id)
	}
}

// runSearch runs the search run of the camera id until it ends or ctx ends. With a deadline of
// ctx, it stops before a position that does not fit in the time left. Then the lens goes to the
// best position so far, and the search waits for the next call. It returns the best position,
// or errStopped for a search that waits.
func (h *Hub) runSearch(ctx context.Context, id string, run *focusRun, progress func(done, all int)) (int, error) {
	defer func() {
		h.mu.Lock()
		run.cancel()
		run.cancel = nil
		h.mu.Unlock()
	}()
	c := h.Cam(id)
	stop := c.Watch()
	defer stop()
	ctl, after, err := c.focusControl(ctx)
	if err != nil {
		return 0, err
	}
	s := run.s
	if s == nil {
		s = newFocusSearch(ctl)
		run.s = s
	}
	r := image.Rectangle{}
	h.mu.Lock()
	settle := c.settle
	fps := c.status.Format.OutFPS
	h.mu.Unlock()
	frameTime := time.Second / time.Duration(max(1, fps))
	for {
		pos, more := s.next()
		if !more {
			break
		}
		// After the sweep, a camera with no settle time measures it.
		measure := settle == 0 && s.fine
		wait := settle
		if settle == 0 {
			wait = focusSettleFirst
		}
		n := s.frames()
		if dl, ok := ctx.Deadline(); ok {
			// The frames of a position, and then of the move to the best position.
			need := 1 + wait + n
			if measure {
				need = 1 + focusSettleMax + 1
			}
			if time.Until(dl) < time.Duration(need+1+wait)*frameTime {
				break
			}
		}
		if measure {
			b, _ := s.bestTry()
			settle, after, err = c.measureSettle(ctx, run, b.pos, b.score, after, &r)
			if err != nil {
				return 0, h.stopRun(ctx, id, run, err)
			}
			h.mu.Lock()
			c.settle = settle
			h.mu.Unlock()
			log.Printf("camera: %s: the lens settles in %d frames", id, settle)
			if settle > focusSettleFirst {
				// The sweep did not wait long enough: a new sweep.
				ctl.Pos = b.pos
				s = newFocusSearch(ctl)
				run.s = s
			}
			continue
		}
		var score float64
		score, after, err = c.measureAt(ctx, run, pos, after, wait, n, &r)
		if err != nil {
			return 0, h.stopRun(ctx, id, run, err)
		}
		s.add(pos, score)
		if progress != nil {
			progress(len(s.tries), s.all)
		}
	}
	if len(s.tries) == 0 {
		return 0, errStopped
	}
	if s.done() && s.flat() {
		return 0, errFlat
	}
	best := s.best()
	// The lens goes to the best position, and the frames after this call are sharp.
	after, err = c.setLens(ctx, run, best, after)
	if err != nil {
		return 0, h.stopRun(ctx, id, run, err)
	}
	for range max(focusSettleFirst, settle) {
		f, err := c.Next(ctx, after)
		if err != nil {
			break
		}
		after = f.Seq
	}
	if !s.done() {
		return best, errStopped
	}
	return best, nil
}

// stopRun returns the error of a search run that stops with the error err. A search that ctx
// ends at its deadline waits for the next call, with the lens at the best position so far.
func (h *Hub) stopRun(ctx context.Context, id string, run *focusRun, err error) error {
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.runs[id] == run && run.s != nil && len(run.s.tries) > 0 {
		h.lens[id] = run.s.best()
		c := h.cams[id]
		if c != nil {
			defer c.refocus()
		}
	}
	return errStopped
}

// focusControl waits for the focus control of the camera, for at most 5 seconds. It returns the
// control and the number of the last frame.
func (c *Cam) focusControl(ctx context.Context) (Focus, uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var after uint64
	for {
		f, err := c.Next(ctx, after)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return Focus{}, 0, ErrNoFrame
			}
			return Focus{}, 0, err
		}
		after = f.Seq
		c.h.mu.Lock()
		z := c.status.Focus
		c.h.mu.Unlock()
		if z == nil {
			// The capture reports the focus control before its first frame.
			return Focus{}, 0, errNoFocusCtl
		}
		return *z, after, nil
	}
}

// setLens sets the lens position pos for the search run, and waits until the capture sets it. It
// returns the number of the last frame before the move. after is the number of a frame of the
// camera.
func (c *Cam) setLens(ctx context.Context, run *focusRun, pos int, after uint64) (uint64, error) {
	h := c.h
	h.mu.Lock()
	if h.runs[c.id] != run || ctx.Err() != nil {
		h.mu.Unlock()
		return 0, errStopped
	}
	if h.lens == nil {
		h.lens = map[string]int{}
	}
	h.lens[c.id] = pos
	h.mu.Unlock()
	c.refocus()
	for range focusApplyMax {
		h.mu.Lock()
		z, seq := c.status.Focus, c.focusSeq
		h.mu.Unlock()
		if z != nil && !z.Auto && z.Pos == pos {
			return seq, nil
		}
		f, err := c.Next(ctx, after)
		if err != nil {
			return 0, err
		}
		after = f.Seq
	}
	return 0, errNoLens
}

// measureAt moves the lens to pos, skips settle frames, and returns the mean sharpness of the n
// frames after them, and the number of the last frame. r is the box of the part: the first
// frame sets it.
func (c *Cam) measureAt(ctx context.Context, run *focusRun, pos int, after uint64, settle, n int, r *image.Rectangle) (float64, uint64, error) {
	after, err := c.setLens(ctx, run, pos, after)
	if err != nil {
		return 0, 0, err
	}
	for range settle {
		f, err := c.Next(ctx, after)
		if err != nil {
			return 0, 0, err
		}
		after = f.Seq
	}
	sum := 0.0
	for range n {
		f, err := c.Next(ctx, after)
		if err != nil {
			return 0, 0, err
		}
		after = f.Seq
		sum += c.measure(f, r)
	}
	return sum / float64(n), after, nil
}

// measureSettle moves the lens to pos, a position with the sharpness want, and measures the
// sharpness of each frame after the move. The lens settles at the first of two frames in a row
// with the same sharpness, and with half of want or more. It returns the settle time in frames,
// and the number of the last frame.
func (c *Cam) measureSettle(ctx context.Context, run *focusRun, pos int, want float64, after uint64, r *image.Rectangle) (int, uint64, error) {
	after, err := c.setLens(ctx, run, pos, after)
	if err != nil {
		return 0, 0, err
	}
	var prev float64
	for i := range focusSettleMax + 1 {
		f, err := c.Next(ctx, after)
		if err != nil {
			return 0, 0, err
		}
		after = f.Seq
		s := c.measure(f, r)
		if i > 0 && s >= want/2 && math.Abs(s-prev) <= focusSteady*max(s, prev) {
			return max(1, i-1), after, nil
		}
		prev = s
	}
	return focusSettleMax, after, nil
}

// measure returns the sharpness of the frame f in the box of the part r. The first frame sets r.
func (c *Cam) measure(f *Frame, r *image.Rectangle) float64 {
	if r.Empty() {
		*r = c.focusRect(f.Size())
	}
	return f.sharpness(*r)
}

// focusRect returns the box of the part in a frame of size sz, with a margin of focusMargin. It
// is the middle of the frame when the deck does not know the box: with no calibration, or with
// no print.
func (c *Cam) focusRect(sz image.Point) image.Rectangle {
	middle := image.Rect(sz.X/4, sz.Y/4, sz.X*3/4, sz.Y*3/4)
	scene, cal, rotate, ok := c.sceneAndCalibration()
	if !ok {
		return middle
	}
	b, ok := scene.Part()
	if !ok {
		return middle
	}
	// The bed is at the Y of the toolhead.
	bedY := 0.0
	if cal.BedY {
		n, ok := scene.Nozzle()
		if !ok {
			return middle
		}
		bedY = n[1]
	}
	pts, ok := projectAll(projectAt(cal, bedY, sz, rotate), boxCorners(b))
	if !ok {
		return middle
	}
	p := bounds(pts)
	mx, my := focusMargin*(p[2]-p[0]), focusMargin*(p[3]-p[1])
	r := image.Rect(int(p[0]-mx), int(p[1]-my), int(math.Ceil(p[2]+mx)), int(math.Ceil(p[3]+my)))
	r = r.Intersect(image.Rect(0, 0, sz.X, sz.Y))
	if r.Dx() < 16 || r.Dy() < 16 {
		return middle
	}
	return r
}

// refocus wakes the capture, so it sets the focus of the settings before its next frame.
func (c *Cam) refocus() {
	select {
	case c.focusNow <- struct{}{}:
	default:
	}
}

// FocusSetting returns the focus of the camera id now: the focus of the settings, or the lens
// position of a search.
func (h *Hub) FocusSetting(id string) (fixed bool, pos int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	cs := h.camSettingsLocked(id)
	return cs.FixedFocus, cs.Focus
}

// StartFocusPrint starts the searches of a print. The deck calls it again while the print runs:
// a call during the print has no effect.
func (h *Hub) StartFocusPrint() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.printing {
		h.printing = true
		h.focusDone, h.userFocused = nil, nil
	}
}

// EndFocusPrint ends the searches of a print. The lens stays at the position of the last search,
// or at the best position so far of a search that stops.
func (h *Hub) EndFocusPrint() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.printing = false
	h.focusDone, h.userFocused = nil, nil
	for id, run := range h.runs {
		if !run.print {
			continue
		}
		if run.cancel != nil {
			run.cancel()
		}
		if run.s != nil && len(run.s.tries) > 0 && !run.s.done() {
			// The lens goes to the best position of the search that stops.
			h.lens[id] = run.s.best()
		}
		delete(h.runs, id)
	}
}

// focusDueLocked returns the IDs of the cameras that need a search of the print, with done layers
// done and the print at the height (mm): after the first layer, and then each PartFocusEvery mm.
// A camera needs the setting on, a focus control, and no focus change of the user during the
// print. The camera of the time lapse is first.
func (h *Hub) focusDueLocked(done int, height float64) []string {
	if !h.printing || done < 1 {
		return nil
	}
	var ids []string
	for _, in := range h.listLocked() {
		cs := h.settings.Camera(in.ID)
		_, hasFocus := h.focusCtls[in.ID]
		last, searched := h.focusDone[in.ID]
		if !in.Usable() || cs.NoFocusPart || !hasFocus || h.userFocused[in.ID] ||
			(searched && height < last+PartFocusEvery) {
			continue
		}
		if in.HasRole(RoleLapse) {
			ids = append([]string{in.ID}, ids...)
		} else {
			ids = append(ids, in.ID)
		}
	}
	return ids
}

// FocusPrint runs the searches of the print that are due, one camera after the other, with done
// layers done and the print at the height (mm). The lens of each camera stays at its best
// position, and the settings do not change. With a deadline of ctx, a search that does not fit
// in the time goes on at the next call. It returns the number of searches that ended.
func (h *Hub) FocusPrint(ctx context.Context, done int, height float64) int {
	h.StartFocusPrint()
	h.mu.Lock()
	ids := h.focusDueLocked(done, height)
	h.mu.Unlock()
	n := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		run, rctx, err := h.startRun(ctx, id, true, height)
		if err != nil {
			continue
		}
		pos, err := h.runSearch(rctx, id, run, nil)
		h.mu.Lock()
		if h.runs[id] != run {
			// A focus change of the user, or the end of the print.
			h.mu.Unlock()
			continue
		}
		switch {
		case errors.Is(err, errStopped):
			// The search waits for the next call, with the lens at the best position so far.
			h.mu.Unlock()
			continue
		case err != nil:
			h.restoreLensLocked(id, run)
			log.Printf("camera: %s: no focus on the part: %v", id, err)
		default:
			log.Printf("camera: %s: focus on the part at %d, at %.1f mm", id, pos, run.height)
		}
		delete(h.runs, id)
		if h.focusDone == nil {
			h.focusDone = map[string]float64{}
		}
		h.focusDone[id] = run.height
		h.mu.Unlock()
		n++
	}
	return n
}
