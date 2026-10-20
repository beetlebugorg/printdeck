// Package remote shows the deck screen in a web browser, and sends the clicks and taps of the browser
// to the deck as touches. The password of the Printdeck login protects it.
//
//	remote.go  the server and its routes
//	auth.go    the login and the sessions
//	screen.go  the frames, the frame events and the touches
package remote

import (
	"bytes"
	_ "embed"
	"html/template"
	"image"
	"log"
	"net/http"
	"sync"
	"time"

	"printdeck/internal/touch"
)

//go:embed page.html
var screenPage []byte

//go:embed login.html
var loginPage []byte

// Server is the remote screen. The deck gives it each frame with Publish.
type Server struct {
	// Password returns the password for the login. When it returns "", nobody can log in.
	Password func() string
	// Touch receives the touches from the browser.
	Touch func(touch.Event)
	// Source is the address of the source code of this version. The pages link to it, as the
	// AGPL-3.0 asks for a program that people use over a network.
	Source string

	mu       sync.Mutex
	frame    *image.RGBA   // the last frame; nil before the first frame
	seq      uint64        // the number of the last frame; it grows when the picture changes
	png      []byte        // the PNG of frame pngSeq, made when a browser asks for it
	pngSeq   uint64        //
	changed  chan struct{} // closed and replaced when a new frame comes
	sessions map[string]time.Time
}

func New() *Server {
	return &Server{changed: make(chan struct{}), sessions: map[string]time.Time{}}
}

// Handler serves the login page, the screen page, and the data of the screen.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.servePage(loginPage))
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.auth(s.servePage(screenPage)))
	mux.HandleFunc("GET /frame.png", s.auth(s.serveFrame))
	mux.HandleFunc("GET /events", s.auth(s.serveEvents))
	mux.HandleFunc("POST /touch", s.auth(s.serveTouch))
	return securityHeaders(mux)
}

// ListenAndServe serves the remote screen on addr until it fails.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("remote screen on %s", addr)
	return srv.ListenAndServe()
}

// servePage sends a page, with the source link in the place of {{SOURCE}}.
func (s *Server) servePage(page []byte) http.HandlerFunc {
	source := s.Source
	if source == "" {
		source = DefaultSource
	}
	html := bytes.ReplaceAll(page, []byte("{{SOURCE}}"), []byte(template.HTMLEscapeString(source)))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(html)
	}
}

// DefaultSource is the source code of Printdeck.
const DefaultSource = "https://github.com/beetlebugorg/printdeck"

// securityHeaders stops other sites from framing the pages, and the pages from loading anything
// from other sites.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src 'self' blob:")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}
