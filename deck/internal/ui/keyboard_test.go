package ui

import (
	"image"
	"printdeck/kit"
	"sort"
	"testing"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

func drawTestKeyboard(t *testing.T) (*App, *keyboard, []kit.Hit) {
	a := New()
	cv := gfx.New(W, BaseH)
	k := &keyboard{title: "Test", max: 64, ok: "OK", cancel: "Back", done: func(string) {}, back: func() {}}
	a.in.Reset()
	a.drawKeyboard(cv, k)
	var keys []kit.Hit
	for _, h := range a.in.Hits() {
		if h.R.Min.Y >= kbY0-kbRowGap {
			keys = append(keys, h)
		}
	}
	if len(keys) != 10+10+9+7+2+2 {
		t.Fatalf("%d keys", len(keys))
	}
	return a, k, keys
}

// The touch areas of the keys do not overlap, and the keys of a row and the rows meet each other,
// so a tap in a gap goes to a key.
func TestKeyAreasMeet(t *testing.T) {
	_, _, keys := drawTestKeyboard(t)
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[i].R.Overlaps(keys[j].R) {
				t.Errorf("keys %v and %v overlap", keys[i].R, keys[j].R)
			}
		}
	}
	rows := map[int][]image.Rectangle{}
	var tops []int
	for _, h := range keys {
		if _, ok := rows[h.R.Min.Y]; !ok {
			tops = append(tops, h.R.Min.Y)
		}
		rows[h.R.Min.Y] = append(rows[h.R.Min.Y], h.R)
	}
	sort.Ints(tops)
	for i, top := range tops {
		row := rows[top]
		sort.Slice(row, func(a, b int) bool { return row[a].Min.X < row[b].Min.X })
		for j := 1; j < len(row); j++ {
			if gap := row[j].Min.X - row[j-1].Max.X; gap != 0 {
				t.Errorf("row %d: %d pixels between %v and %v", i, gap, row[j-1], row[j])
			}
		}
		if i > 0 {
			if prev := rows[tops[i-1]][0]; prev.Max.Y != top {
				t.Errorf("rows %d and %d: %d pixels apart", i-1, i, top-prev.Max.Y)
			}
		}
	}
}

// A tap in the gap between q and w types one of the two letters, and a tap in the gap above the
// row types the letter below it.
func TestTapInGap(t *testing.T) {
	a, k, _ := drawTestKeyboard(t)
	tap := func(x, y int) {
		a.handle(touch.Event{Kind: touch.Down, X: float64(x) / BaseW, Y: float64(y) / BaseH})
		a.handle(touch.Event{Kind: touch.Up, X: float64(x) / BaseW, Y: float64(y) / BaseH})
	}
	kw := (kbX1 - kbX0 - 9*kbGap) / 10
	qRight := kbX0 + (kbX1-kbX0-(10*kw+9*kbGap))/2 + kw // the right edge of q
	rowTop := kbY0 + kbKeyH + kbRowGap                  // the top of the q row
	tap(qRight+1, rowTop+20)
	tap(qRight+20, rowTop-3)
	if k.value != "qw" && k.value != "ww" {
		t.Errorf("typed %q", k.value)
	}
}
