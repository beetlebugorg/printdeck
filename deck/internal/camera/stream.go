package camera

import (
	"context"
	"crypto/tls"
	"errors"
	"image"
	"time"

	"printdeck/internal/h264"
	"printdeck/internal/rtsp"
)

// A network camera: the H.264 stream of a camera over RTSP, as the camera of a Bambu Lab printer
// sends it. The hub decodes the stream (package h264) only while a viewer watches, as for a USB
// camera, and makes frames of the pictures.

// streamFPS is the most frames a second that the hub makes of a stream. The decoder decodes each
// picture of the stream, because the next pictures need it.
const streamFPS = 15

// UseStream makes the camera at the RTSP address url the only camera of the hub, as the device d.
// d.Path is the address for the screens, with no login; url has the login. check checks the TLS
// certificate of an RTSPS camera (rtsp.Dial).
func (h *Hub) UseStream(d Device, url string, check func(*tls.ConnectionState) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scan = func() []Device { return []Device{d} }
	h.scanned = time.Time{}
	other := h.capture
	h.capture = func(ctx context.Context, dev Device, l link) error {
		if dev.ID != d.ID {
			return other(ctx, dev, l)
		}
		return streamCapture(ctx, url, check, l)
	}
}

// streamCapture reads the stream at url, decodes it, and sends its frames to l until ctx ends or
// the stream fails.
func streamCapture(ctx context.Context, url string, check func(*tls.ConnectionState) error, l link) error {
	dec, err := h264.New(2)
	if err != nil {
		return err
	}
	defer dec.Close()
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, err := rtsp.Dial(dctx, url, check)
	cancel()
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	if err := c.Start(); err != nil {
		if errors.Is(err, rtsp.ErrAuth) {
			return errors.New("the camera refused the access code. Turn on LAN Only Liveview on the printer")
		}
		return err
	}
	s := l.settings()
	var last time.Time
	formatSet := false
	for {
		au, err := c.Read()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		pics, err := dec.Decode(au)
		if err != nil {
			return err
		}
		for _, pic := range pics {
			if !formatSet {
				w, hh := pic.Rect.Dx(), pic.Rect.Dy()
				ow, oh := TurnedSize(w, hh, s.Rotate)
				l.format(Format{In: "H264", InW: w, InH: hh, OutW: ow, OutH: oh, OutFPS: streamFPS})
				formatSet = true
			}
			now := time.Now()
			if now.Sub(last) < time.Second/streamFPS-5*time.Millisecond {
				continue
			}
			last = now
			var p *image.YCbCr = pic
			if s.Rotate != 0 {
				ow, oh := TurnedSize(pic.Rect.Dx(), pic.Rect.Dy(), s.Rotate)
				p = scaleYCbCr(pic, ow, oh, s.Rotate)
			}
			l.emit(picFrame(p))
		}
	}
}
