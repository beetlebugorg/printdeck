package ui

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"time"

	"printdeck/internal/gfx"
)

// The moves between two frames on the touch screen: a page that opens comes in from the right,
// Back slides it out to the right, a page at the same depth (a key of the page keys) fades in, and
// another printer slides in from the side of its place in the list. A move is short, and the
// input of the move waits in its channel.

type transition int

const (
	transitionNone transition = iota
	transitionForward
	transitionBack
	transitionFade
)

// The times of the moves.
const (
	slideTime = 220 * time.Millisecond
	fadeTime  = 150 * time.Millisecond
	frameTime = 16 * time.Millisecond
)

// pageKey is what decides a move: the depth of the page stack, the page at its top, and the
// sheets that cover the page.
type pageKey struct {
	depth int
	top   string
}

func (a *App) pageKey() pageKey {
	k := pageKey{depth: len(a.pages)}
	if n := len(a.pages); n > 0 {
		k.top = fmt.Sprint(a.pages[n-1])
	}
	if a.away {
		k.top += " away"
	}
	if a.fleetList {
		k.depth, k.top = k.depth+1, "printers"
	}
	if a.setup != nil {
		k.top += " setup"
	}
	return k
}

// transitionFor returns the move from the page last to the page now. same is false when the
// frame shows another App than the last one: dir is then the side of the new printer.
func transitionFor(same bool, last, now pageKey, dir int) transition {
	switch {
	case !same || dir != 0:
		if dir < 0 {
			return transitionBack
		}
		return transitionForward
	case now == last:
		return transitionNone
	case now.depth > last.depth:
		return transitionForward
	case now.depth < last.depth:
		return transitionBack
	}
	return transitionFade
}

// animate shows the move from the frame from to the frame to, and ends on to.
func animate(present func(*gfx.Canvas), from, to *gfx.Canvas, t transition) {
	d := slideTime
	if t == transitionFade {
		d = fadeTime
	}
	out := gfx.New(to.Bounds().Dx(), to.Bounds().Dy())
	start := time.Now()
	for {
		f := float64(time.Since(start)) / float64(d)
		if f >= 1 {
			break
		}
		e := 1 - math.Pow(1-f, 3) // ease out: fast at the start, slow at the end
		switch t {
		case transitionFade:
			blend(out, from, to, e)
		default:
			slide(out, from, to, e, t == transitionForward)
		}
		present(out)
		if wait := frameTime - time.Since(start)%frameTime; wait > 0 {
			time.Sleep(wait)
		}
	}
	present(to)
}

// slide draws the frames from and to side by side into out, with to e of the way in: from the
// right when forward, and from the left when not.
func slide(out, from, to *gfx.Canvas, e float64, forward bool) {
	b := out.Bounds()
	w := b.Dx()
	off := int(float64(w) * e)
	if forward {
		draw.Draw(out.RGBA, image.Rect(0, 0, w-off, b.Dy()), from.RGBA, image.Pt(off, 0), draw.Src)
		draw.Draw(out.RGBA, image.Rect(w-off, 0, w, b.Dy()), to.RGBA, image.Pt(0, 0), draw.Src)
		return
	}
	draw.Draw(out.RGBA, image.Rect(off, 0, w, b.Dy()), from.RGBA, image.Pt(0, 0), draw.Src)
	draw.Draw(out.RGBA, image.Rect(0, 0, off, b.Dy()), to.RGBA, image.Pt(w-off, 0), draw.Src)
}

// blend draws the mix of from and to into out, with e of to.
func blend(out, from, to *gfx.Canvas, e float64) {
	k := uint32(e * 256)
	for i := range out.Pix {
		out.Pix[i] = uint8((uint32(from.Pix[i])*(256-k) + uint32(to.Pix[i])*k) >> 8)
	}
}
