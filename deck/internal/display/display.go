// Package display connects the deck UI to a screen. On the deck, the screen is the framebuffer
// and the touch panel (Panel). The desktop app has no local screen (None): its window shows the
// remote screen.
package display

import (
	"context"
	"image"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// A Display shows the frames of the UI and sends the touches.
type Display interface {
	// Size is the size of the UI on the screen, after the rotation.
	Size() image.Point
	// Present shows a frame.
	Present(c *gfx.Canvas)
	// Run sends the touches to out until ctx ends.
	Run(ctx context.Context, out chan<- touch.Event)
	Close()
}

// None is a display with no screen. The UI still runs: the printer display follows it.
type None struct{}

// Size is the size of the tall touch screen of the deck.
func (None) Size() image.Point                             { return image.Pt(480, 800) }
func (None) Present(*gfx.Canvas)                           {}
func (None) Run(ctx context.Context, _ chan<- touch.Event) { <-ctx.Done() }
func (None) Close()                                        {}
