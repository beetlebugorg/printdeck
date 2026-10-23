package octoapi

import (
	"encoding/json"
	"net/http"
	"time"

	"printdeck/internal/notify"
)

// The pairing of the Printdeck app for push messages (notify.Phones). The app sends its device
// token, the send token of the relay, and its X25519 public key, and gets the public key of the
// deck. The list of the phones has no key and no token.

type pushFields struct {
	// Phones are the paired phones, or nil for a deck with no push messages.
	Phones *notify.Phones
	// OnPaired is called after a pairing, for a test message to the phone.
	OnPaired func(ph notify.Phone)
}

type phoneJSON struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Env   string    `json:"env"`
	Added time.Time `json:"added"`
}

func (s *Server) servePair(w http.ResponseWriter, r *http.Request) {
	if s.Phones == nil {
		http.NotFound(w, r)
		return
	}
	var req notify.PairRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		apiError(w, http.StatusBadRequest, "bad JSON")
		return
	}
	ph, pub, err := s.Phones.Pair(req, time.Now())
	if err != nil {
		apiError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.OnPaired != nil {
		s.OnPaired(ph)
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": ph.ID, "public_key": pub})
}

func (s *Server) servePhones(w http.ResponseWriter, r *http.Request) {
	if s.Phones == nil {
		http.NotFound(w, r)
		return
	}
	out := []phoneJSON{}
	for _, ph := range s.Phones.List() {
		out = append(out, phoneJSON{ID: ph.ID, Name: ph.Name, Env: ph.Env, Added: ph.Added})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) serveUnpair(w http.ResponseWriter, r *http.Request) {
	if s.Phones == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Phones.Remove(r.PathValue("id")); err != nil {
		apiError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
