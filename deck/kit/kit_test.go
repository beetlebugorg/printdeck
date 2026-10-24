package kit

import (
	"image"
	"testing"
	"time"
)

func TestRowsAndCols(t *testing.T) {
	r := image.Rect(10, 20, 110, 220)
	got := Rows(r, 10, Px(50), Fr(1), Fr(1))
	want := []Rect{image.Rect(10, 20, 110, 70), image.Rect(10, 80, 110, 145), image.Rect(10, 155, 110, 220)}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: %v, want %v", i, got[i], want[i])
		}
	}
	c := Cols(r, 0, Fr(1), Fr(2))
	if c[0] != image.Rect(10, 20, 43, 220) || c[1] != image.Rect(43, 20, 110, 220) {
		t.Errorf("cols %v", c)
	}
}

// The Fr parts fill the length, so no pixel stays at the end.
func TestFrFillsTheLength(t *testing.T) {
	c := Cols(image.Rect(0, 0, 100, 10), 7, Fr(1), Fr(1), Fr(1))
	if c[2].Max.X != 100 {
		t.Errorf("last column ends at %d", c[2].Max.X)
	}
}

func TestGridAndStack(t *testing.T) {
	g := Grid(image.Rect(0, 0, 210, 100), 2, 2, 10)
	if len(g) != 4 || g[1] != image.Rect(110, 0, 210, 45) || g[2] != image.Rect(0, 55, 100, 100) {
		t.Errorf("grid %v", g)
	}
	s := NewStack(image.Rect(0, 100, 50, 300), 8)
	a, b := s.Next(40), s.Next(20)
	if a != image.Rect(0, 100, 50, 140) || b != image.Rect(0, 148, 50, 168) || s.Rest() != image.Rect(0, 176, 50, 300) {
		t.Errorf("stack %v %v %v", a, b, s.Rest())
	}
}

func TestSplit(t *testing.T) {
	side, main := Screen{480, 800}.Split(232)
	if side != image.Rect(0, 0, 480, 232) || main != image.Rect(0, 232, 480, 800) {
		t.Errorf("tall: %v %v", side, main)
	}
	side, main = Screen{800, 480}.Split(400)
	if side != image.Rect(0, 0, 400, 480) || main != image.Rect(400, 0, 800, 480) {
		t.Errorf("wide: %v %v", side, main)
	}
}

func TestTapSliderScrollSwipe(t *testing.T) {
	var in Input
	var taps, scrolled, swiped int
	var at, end float64
	var tapAt image.Point
	key := image.Rect(0, 0, 100, 50)
	slider := image.Rect(0, 100, 200, 120)
	list := image.Rect(0, 200, 200, 400)
	swipe := image.Rect(0, 500, 400, 600)
	in.Add(Hit{R: key, Tap: func() { taps++ }})
	in.Add(Hit{R: slider, Drag: func(f float64) { at = f }, End: func(f float64) { end = f }})
	in.Add(Hit{R: list, Scroll: func(dy int) { scrolled += dy }, TapAt: func(p image.Point) { tapAt = p }})
	in.Add(Hit{R: swipe, Swipe: func(dir int) { swiped = dir }})
	ev := func(k Kind, x, y int) { in.Handle(Event{Kind: k, P: image.Pt(x, y)}) }

	ev(Down, 50, 25)
	if !in.Pressed(key) {
		t.Error("the key is not pressed")
	}
	ev(Up, 60, 60) // near the key: it still takes the tap
	if taps != 1 || in.Pressed(key) {
		t.Errorf("taps %d", taps)
	}
	ev(Down, 50, 25)
	ev(Move, 50, 200) // far away: no tap
	ev(Up, 50, 200)
	if taps != 1 {
		t.Errorf("taps %d after a move away", taps)
	}

	ev(Down, 50, 110)
	ev(Move, 150, 110)
	if f, ok := in.Dragging(slider); !ok || f != 0.75 || at != 0.75 {
		t.Errorf("drag %v %v %v", f, ok, at)
	}
	ev(Up, 100, 110)
	if end != 0.5 {
		t.Errorf("end %v", end)
	}

	ev(Down, 50, 300)
	ev(Up, 52, 302) // no movement: a tap in the list
	if tapAt != image.Pt(52, 302) || scrolled != 0 {
		t.Errorf("tapAt %v, scrolled %d", tapAt, scrolled)
	}
	ev(Down, 50, 300)
	ev(Move, 50, 280)
	ev(Move, 50, 250)
	ev(Up, 50, 250)
	if scrolled != 50 || tapAt != image.Pt(52, 302) {
		t.Errorf("scrolled %d", scrolled)
	}

	// The moves of a swipe were lost (a slow frame): the lift far away scrolls, and it is no tap.
	tapAt, scrolled = image.Point{}, 0
	ev(Down, 50, 300)
	ev(Up, 50, 240)
	if scrolled != 60 || tapAt != (image.Point{}) {
		t.Errorf("a swipe with no moves: scrolled %d, tapAt %v", scrolled, tapAt)
	}

	ev(Down, 300, 550)
	ev(Move, 200, 555)
	if in.SwipeOffset() != -80 {
		t.Errorf("swipe offset %d", in.SwipeOffset())
	}
	ev(Up, 200, 555)
	if swiped != 1 {
		t.Errorf("swipe %d", swiped)
	}
}

// A pad gets the end of the touch also when its area is gone.
func TestPadGetsUpAfterReset(t *testing.T) {
	var in Input
	var phases []Phase
	in.Add(Hit{R: image.Rect(0, 0, 100, 100), Pad: func(p image.Point, ph Phase) { phases = append(phases, ph) }})
	in.Handle(Event{Kind: Down, P: image.Pt(10, 10)})
	in.Handle(Event{Kind: Move, P: image.Pt(20, 10)})
	in.Reset()
	in.Handle(Event{Kind: Up, P: image.Pt(20, 10)})
	if len(phases) != 3 || phases[2] != PadUp {
		t.Errorf("phases %v", phases)
	}
}

func TestList(t *testing.T) {
	scroll := 0
	l := List{Area: image.Rect(0, 100, 200, 400), N: 10, RowH: 100, Gap: 10, Scroll: &scroll}
	if l.MaxScroll() != 700 || l.Row(1) != image.Rect(0, 200, 200, 290) || l.Shows(3) {
		t.Errorf("max %d, row 1 %v, shows 3 %v", l.MaxScroll(), l.Row(1), l.Shows(3))
	}
	l.Reveal(5)
	if scroll != 300 || l.At(image.Pt(10, 350)) != 5 || l.At(image.Pt(10, 50)) != -1 {
		t.Errorf("scroll %d, at %d", scroll, l.At(image.Pt(10, 350)))
	}
	var tapped int
	h := l.Hit(func(i int) { tapped = i })
	h.Scroll(1000)
	h.TapAt(image.Pt(10, 120))
	if scroll != 700 || tapped != 7 {
		t.Errorf("scroll %d, tapped %d", scroll, tapped)
	}
}

// A flick of a list goes on after the lift, slower and slower, and a new touch stops it.
func TestFling(t *testing.T) {
	var in Input
	scrolled := 0
	in.Add(Hit{R: image.Rect(0, 0, 100, 400), Scroll: func(dy int) { scrolled += dy }})
	t0 := time.Unix(100, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	in.Handle(Event{Kind: Down, P: image.Pt(50, 300), At: at(0)})
	for i := 1; i <= 5; i++ { // 20 pixels each 20 ms: 1000 pixels a second
		in.Handle(Event{Kind: Move, P: image.Pt(50, 300-20*i), At: at(20 * i)})
	}
	in.Handle(Event{Kind: Up, P: image.Pt(50, 200), At: at(180)}) // the panel reports the lift 80 ms late
	byFinger := scrolled
	if !in.Fling(at(213)) || scrolled <= byFinger {
		t.Fatalf("no fling: scrolled %d, by the finger %d", scrolled, byFinger)
	}
	moving := true
	for ms := 246; moving && ms < 5000; ms += 33 {
		moving = in.Fling(at(ms))
	}
	if moving {
		t.Fatal("the fling does not stop")
	}
	// A slow drag has no fling.
	in.Handle(Event{Kind: Down, P: image.Pt(50, 300), At: at(6000)})
	in.Handle(Event{Kind: Move, P: image.Pt(50, 280), At: at(6200)})
	in.Handle(Event{Kind: Up, P: image.Pt(50, 280), At: at(6300)})
	if in.Fling(at(6333)) {
		t.Error("a slow drag flings")
	}
}
