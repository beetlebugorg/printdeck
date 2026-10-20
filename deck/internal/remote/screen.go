package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// Publish keeps a copy of the frame when it differs from the last one. It is fast when the picture
// did not change, so the deck calls it after each frame.
func (s *Server) Publish(c *gfx.Canvas) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frame != nil && s.frame.Rect == c.Rect && bytes.Equal(s.frame.Pix, c.Pix) {
		return
	}
	if s.frame == nil || s.frame.Rect != c.Rect {
		s.frame = image.NewRGBA(c.Rect)
	}
	copy(s.frame.Pix, c.Pix)
	s.seq++
	close(s.changed)
	s.changed = make(chan struct{})
}

// serveFrame sends the last frame as a PNG. The PNG is made once for each frame, when the first
// browser asks for it.
func (s *Server) serveFrame(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.frame == nil {
		s.mu.Unlock()
		http.Error(w, "no frame yet", http.StatusServiceUnavailable)
		return
	}
	if s.png == nil || s.pngSeq != s.seq {
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		enc.Encode(&buf, s.frame)
		s.png, s.pngSeq = buf.Bytes(), s.seq
	}
	data := s.png
	s.mu.Unlock()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// serveEvents sends the number of each new frame to the browser (Server-Sent Events). The browser
// then loads /frame.png. When the screen does not change, nothing is sent.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ping := time.NewTicker(30 * time.Second) // keeps the connection open through proxies
	defer ping.Stop()
	var sent uint64
	for {
		s.mu.Lock()
		seq, changed := s.seq, s.changed
		s.mu.Unlock()
		if seq != 0 && seq != sent {
			fmt.Fprintf(w, "data: %d\n\n", seq)
			flusher.Flush()
			sent = seq
		}
		select {
		case <-changed:
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// touchKinds are the names of the touch kinds in the requests of the browser.
var touchKinds = map[string]touch.Kind{"down": touch.Down, "move": touch.Move, "up": touch.Up}

// serveTouch receives one touch: {"kind": "down"|"move"|"up", "x": 0..1, "y": 0..1}, where x and y
// are the position on the screen.
func (s *Server) serveTouch(w http.ResponseWriter, r *http.Request) {
	var t struct {
		Kind string  `json:"kind"`
		X    float64 `json:"x"`
		Y    float64 `json:"y"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&t); err != nil {
		http.Error(w, "bad touch", http.StatusBadRequest)
		return
	}
	kind, ok := touchKinds[t.Kind]
	if !ok || t.X < 0 || t.X > 1 || t.Y < 0 || t.Y > 1 {
		http.Error(w, "bad touch", http.StatusBadRequest)
		return
	}
	if s.Touch != nil {
		s.Touch(touch.Event{Kind: kind, X: t.X, Y: t.Y})
	}
	w.WriteHeader(http.StatusNoContent)
}
