package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"printdeck/internal/gfx"
	"printdeck/internal/touch"
)

// Each browser has its own UI: a session of the deck UI that draws for this browser only. The
// frames are the drawing commands of the UI (gfx.Recording), and the browser draws them on a
// canvas at the pixel density of its screen. The touches of the browser go to its session.

// maxSessions is the most browsers at a time.
const maxSessions = 8

type session struct {
	input Input
}

func (s *Server) addSession(id string, ss *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ui) >= maxSessions {
		return false
	}
	s.ui[id] = ss
	return true
}

func (s *Server) removeSession(id string) {
	s.mu.Lock()
	delete(s.ui, id)
	s.mu.Unlock()
}

// frameBox keeps the newest frame that is not sent yet. A new frame replaces a frame that waits, so
// a slow browser gets the latest picture and no queue.
type frameBox struct {
	mu    sync.Mutex
	frame []byte
	ready chan struct{}
}

func (b *frameBox) put(f []byte) {
	b.mu.Lock()
	b.frame = f
	b.mu.Unlock()
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

func (b *frameBox) take() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := b.frame
	b.frame = nil
	return f
}

// serveEvents starts a session and sends its frames (Server-Sent Events). ?w= and ?h= are the size that
// the browser asks for, and ?deck=1 asks for the screens of the touch screen. The first event is the number of the session, for the touches, and the
// height of its screen. The session ends when the browser closes the stream.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok || s.NewSession == nil {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	raw := make([]byte, 16)
	rand.Read(raw)
	id := hex.EncodeToString(raw)
	box := &frameBox{ready: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ss := &session{}
	if !s.addSession(id, ss) {
		http.Error(w, "too many browsers", http.StatusServiceUnavailable)
		return
	}
	defer s.removeSession(id)
	var size image.Point
	size.X, _ = strconv.Atoi(r.URL.Query().Get("w"))
	size.Y, _ = strconv.Atoi(r.URL.Query().Get("h"))
	size, in := s.NewSession(ctx, size, r.URL.Query().Get("deck") == "1", box.put)
	s.mu.Lock()
	ss.input = in
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "event: session\ndata: %s %d %d\n\n", id, size.X, size.Y)
	flusher.Flush()
	ping := time.NewTicker(30 * time.Second) // keeps the connection open through proxies
	defer ping.Stop()
	for {
		select {
		case <-box.ready:
			if f := box.take(); f != nil {
				w.Write([]byte("data: "))
				w.Write(f)
				w.Write([]byte("\n\n"))
				flusher.Flush()
			}
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// touchKinds are the names of the touch kinds in the requests of the browser.
var touchKinds = map[string]touch.Kind{"down": touch.Down, "move": touch.Move, "up": touch.Up}

// serveTouch receives one touch for a session: {"kind": "down"|"move"|"up", "x": 0..1, "y": 0..1},
// where x and y are the position on the screen, and ?s= is the number of the session.
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
	s.mu.Lock()
	var touchFn func(touch.Event)
	if ss := s.ui[r.URL.Query().Get("s")]; ss != nil {
		touchFn = ss.input.Touch
	}
	s.mu.Unlock()
	if touchFn == nil {
		http.Error(w, "no session", http.StatusGone)
		return
	}
	touchFn(touch.Event{Kind: kind, X: t.X, Y: t.Y})
	w.WriteHeader(http.StatusNoContent)
}

// keyNames are the keys with no text that a session reads.
var keyNames = map[string]bool{"Enter": true, "Escape": true, "Backspace": true}

// serveKey receives one key of a real keyboard for a session: {"text": "a"} for a key that types
// text, or {"name": "Enter"} for Enter, Escape or Backspace. ?s= is the number of the session.
func (s *Server) serveKey(w http.ResponseWriter, r *http.Request) {
	var k struct {
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&k); err != nil ||
		utf8.RuneCountInString(k.Text) > 8 || (k.Text == "") == (k.Name == "") || (k.Name != "" && !keyNames[k.Name]) {
		http.Error(w, "bad key", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	var keyFn func(text, name string)
	if ss := s.ui[r.URL.Query().Get("s")]; ss != nil {
		keyFn = ss.input.Key
	}
	s.mu.Unlock()
	if keyFn == nil {
		http.Error(w, "no session", http.StatusGone)
		return
	}
	keyFn(k.Text, k.Name)
	w.WriteHeader(http.StatusNoContent)
}

// serveImage sends an image of the frames as a PNG, or the JPEG of a picture of a camera. An
// image number is for one image only, so the browser keeps it. The number can have ".png" at the
// end.
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("id"), ".png"))
	img := gfx.ImageByID(id)
	if err != nil || img == nil {
		http.NotFound(w, r)
		return
	}
	if p, ok := img.(gfx.Photo); ok {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
		w.Write(p.JPEG())
		return
	}
	var b bytes.Buffer
	(&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.Write(b.Bytes())
}

// serveFont sends a font of the UI, so the browser draws the same letters.
func (s *Server) serveFont(w http.ResponseWriter, r *http.Request) {
	var b []byte
	switch r.PathValue("name") {
	case "regular.ttf":
		b = gfx.FontRegular()
	case "bold.ttf":
		b = gfx.FontBold()
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/ttf")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Write(b)
}
