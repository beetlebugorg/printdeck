// Package remote is the deck UI in a web browser: each browser has its own session of the UI, and
// draws its frames on a canvas. The password of the Printdeck login protects it.
//
//	remote.go  the server and its routes
//	auth.go    the login and the login sessions
//	screen.go  the UI sessions, their frames and touches, the images and the fonts
package remote

import (
	"bytes"
	"context"
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

// Server is the deck UI in the browser.
type Server struct {
	// Password returns the password for the login. When it returns "", nobody can log in.
	Password func() string
	// NewSession starts a session of the UI for one browser, with a screen size for the shape of
	// its window: 480 wide and a height for a tall window, or wider than high for a wide window. It
	// gives each frame (the JSON of a gfx.Recording) to frame, until ctx ends, and returns the size
	// that it uses and the function that takes the touches.
	NewSession func(ctx context.Context, size image.Point, frame func([]byte)) (image.Point, func(touch.Event))
	// Icon is the PNG icon of the page, for the home screen of a phone.
	Icon []byte
	// NoLogin lets every request through, for the demo (printdeck remote-demo).
	NoLogin bool
	// Source is the address of the source code of this version. The pages link to it, as the
	// AGPL-3.0 asks for a program that people use over a network.
	Source string

	mu       sync.Mutex
	sessions map[string]time.Time // the login sessions
	ui       map[string]*session  // the UI sessions
}

func New() *Server {
	return &Server{sessions: map[string]time.Time{}, ui: map[string]*session{}}
}

// Handler serves the login page, the screen page, and the data of the screen.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.servePage(loginPage))
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.auth(s.servePage(screenPage)))
	mux.HandleFunc("GET /screen", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusMovedPermanently) })
	mux.HandleFunc("GET /ui/events", s.auth(s.serveEvents))
	mux.HandleFunc("POST /ui/touch", s.auth(s.serveTouch))
	mux.HandleFunc("GET /ui/img/{id}", s.auth(s.serveImage))
	mux.HandleFunc("GET /ui/font/{name}", s.serveFont)
	mux.HandleFunc("GET /manifest.webmanifest", s.serveManifest)
	mux.HandleFunc("GET /icon.png", s.serveIcon)
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

// manifest makes the page an app on the home screen of a phone: full screen, with no browser bars.
const manifest = `{"name":"Printdeck","short_name":"Printdeck","start_url":"/","scope":"/","display":"standalone",
"orientation":"portrait","background_color":"#121211","theme_color":"#121211",
"icons":[{"src":"/icon.png","sizes":"512x512","type":"image/png","purpose":"any"}]}`

func (s *Server) serveManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Write([]byte(manifest))
}

func (s *Server) serveIcon(w http.ResponseWriter, r *http.Request) {
	if s.Icon == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(s.Icon)
}

// SecurityHeaders adds the security headers of the pages to the responses of h.
func SecurityHeaders(h http.Handler) http.Handler { return securityHeaders(h) }

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
