package display

import (
	"context"
	"image"

	"printdeck/internal/fb"
	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// Panel is the screen of the deck: the Linux framebuffer and the touch panel.
type Panel struct {
	*fb.FB
	touch touch.Options
}

// SetTouch sets the settings of the touch panel. Call it before Run.
func (p *Panel) SetTouch(o touch.Options) { p.touch = o }

// OpenPanel maps the framebuffer dev. rot is the rotation of the UI on the panel (see fb.Open).
func OpenPanel(dev string, rot int) (*Panel, error) {
	f, err := fb.Open(dev, rot)
	if err != nil {
		return nil, err
	}
	return &Panel{FB: f}, nil
}

func (p *Panel) Size() image.Point { return image.Pt(p.LogicalW, p.LogicalH) }

func (p *Panel) Present(c *gfx.Canvas) { p.FB.Present(c) }

func (p *Panel) Run(ctx context.Context, out chan<- touch.Event) { touch.Run(ctx, p.touch, out) }
