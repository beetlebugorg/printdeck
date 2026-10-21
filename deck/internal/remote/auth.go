package remote

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	cookieName = "printdeck"
	sessionAge = 30 * 24 * time.Hour
)

// wrongDelay is the wait after a wrong password. It makes guessing slow.
var wrongDelay = time.Second

// Auth lets a request through only with a valid session of the login, for the other pages of the
// web server.
func (s *Server) Auth(h http.HandlerFunc) http.HandlerFunc { return s.auth(h) }

// auth lets a request through only with a valid session. Without one, the screen page goes to the
// login page, and the other requests fail.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieName); err == nil && s.validSession(c.Value) {
			h(w, r)
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/screen" || r.URL.Path == "/system") {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "log in first", http.StatusUnauthorized)
	}
}

func (s *Server) validSession(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.sessions[token]
	if ok && time.Now().After(until) {
		delete(s.sessions, token)
		ok = false
	}
	return ok
}

// login checks the password and starts a session.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	want := ""
	if s.Password != nil {
		want = s.Password()
	}
	got := strings.TrimSpace(r.FormValue("password"))
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		time.Sleep(wrongDelay)
		http.Redirect(w, r, "/login?wrong", http.StatusSeeOther)
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(raw)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionAge)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", MaxAge: int(sessionAge.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout ends the session.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
