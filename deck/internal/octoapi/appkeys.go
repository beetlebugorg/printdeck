package octoapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"printdeck/internal/signing"
)

// The App Keys flow of OctoPrint (https://docs.octoprint.org/en/master/bundledplugins/appkeys.html):
//
//  1. The app posts {"app": "Cura"} to /plugin/appkeys/request, and gets a token and an empty
//     auth_dialog.
//  2. The touch screen asks "Allow Cura to use the Printdeck?".
//  3. The app polls /plugin/appkeys/request/<token>: 202 while the user decides, 200 with the key
//     after "Allow", and 404 after "Deny" or when the request is too old.
//
// At the root of a deck (Server.DeckWide), the question is "Allow Cura to use this Printdeck and
// its printers?", and the key is a deck key: it works for each printer, at /printers/<id>/ too.
// The answer has "scope": "deck". A request under /printers/<id>/ gives a key of that printer
// only ("scope": "printer").
//
// The request can also have "public_key": the public key of the app for signed prints
// ("ed25519 <base64>", package signing). The question then names its key id, and "Allow" also
// adds the key to the trusted keys of the deck.

// requestTimeout is how long a request waits for the user.
const requestTimeout = 2 * time.Minute

type keyRequest struct {
	app     string
	created time.Time
	decided bool
	key     string // the new key after "Allow"; "" after "Deny"
	trusted string // the key id of the public key that "Allow" added, or ""
	deck    bool   // the key is a deck key (DeckKeys)
}

func (s *Server) requestKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App       string `json:"app"`
		PublicKey string `json:"public_key"`
	}
	if !decode(w, r, &req) {
		return
	}
	app := strings.TrimSpace(req.App)
	if app == "" || len(app) > 64 {
		apiError(w, http.StatusBadRequest, "No app name")
		return
	}
	if s.Ask == nil {
		apiError(w, http.StatusNotFound, "App keys are off")
		return
	}
	// The address of the app is in the question, so the user can tell a request from another
	// computer.
	who := app
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		who = app + " (" + host + ")"
	}
	question := "Allow " + who + " to use the Printdeck?"
	deck := s.DeckWide && s.DeckKeys != nil
	if deck {
		question = "Allow " + who + " to use this Printdeck and its printers?"
	}
	var trustID string
	if req.PublicKey != "" && s.Signing != nil {
		pub, err := signing.ParsePublic(req.PublicKey)
		if err != nil {
			apiError(w, http.StatusBadRequest, "The public key does not read")
			return
		}
		trustID = signing.KeyID(pub)
		question = strings.TrimSuffix(question, "?") + ", and trust its prints (key " + trustID + ")?"
	}
	token := randomHex(16)
	kr := &keyRequest{app: app, created: time.Now(), deck: deck}
	s.mu.Lock()
	s.dropOldRequests()
	s.requests[token] = kr
	s.mu.Unlock()

	s.Ask(question, "Allow", "Deny", func(yes bool) {
		key := ""
		if yes {
			if deck {
				key = s.DeckKeys.ks.add(app)
			} else {
				key = s.keys.add(app)
			}
			if trustID != "" {
				if _, err := s.Signing.Add(app, req.PublicKey, time.Now()); err != nil {
					log.Printf("appkeys: the key of %s: %v", app, err)
				}
			}
		}
		s.mu.Lock()
		kr.decided, kr.key = true, key
		if key != "" {
			kr.trusted = trustID
		}
		s.mu.Unlock()
	})
	w.Header().Set("Location", baseURL(r)+"/plugin/appkeys/request/"+token)
	// The empty auth_dialog tells the app that no web page asks the user: the OctoPrint plugin of
	// Cura opens a browser to auth_dialog, or to the Printdeck when the answer has none.
	writeJSON(w, http.StatusCreated, map[string]string{"app_token": token, "auth_dialog": ""})
}

func (s *Server) pollKey(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.dropOldRequests()
	kr := s.requests[r.PathValue("token")]
	var decided, deck bool
	var key, trusted string
	if kr != nil {
		decided, key, trusted, deck = kr.decided, kr.key, kr.trusted, kr.deck
		if decided {
			delete(s.requests, r.PathValue("token")) // the key is given once
		}
	}
	s.mu.Unlock()
	switch {
	case kr == nil, decided && key == "":
		apiError(w, http.StatusNotFound, "No such request, or it was denied")
	case !decided:
		w.WriteHeader(http.StatusAccepted)
	default:
		// trusted_key is the key id of the public key of the app that the deck now trusts.
		// scope is "deck" for a key of each printer of the deck, or "printer" for a key of one
		// printer.
		out := map[string]string{"api_key": key, "scope": "printer"}
		if deck {
			out["scope"] = "deck"
		}
		if trusted != "" {
			out["trusted_key"] = trusted
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// dropOldRequests forgets the requests that waited too long. s.mu must be locked.
func (s *Server) dropOldRequests() {
	for t, kr := range s.requests {
		if time.Since(kr.created) > requestTimeout {
			delete(s.requests, t)
		}
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// keyStore keeps the API keys: the main key of the Printdeck user, and the keys of the apps in a file.
type keyStore struct {
	main string
	file string

	mu   sync.Mutex
	apps map[string]string // key to app name
}

func newKeyStore(main, file string) *keyStore {
	ks := &keyStore{main: main, file: file, apps: map[string]string{}}
	if file != "" {
		if b, err := os.ReadFile(file); err == nil {
			json.Unmarshal(b, &ks.apps)
		}
	}
	return ks
}

func (ks *keyStore) valid(key string) bool {
	if key == "" {
		return false
	}
	if equalKeys(key, ks.main) {
		return true
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	for k := range ks.apps {
		if equalKeys(key, k) {
			return true
		}
	}
	return false
}

// add makes a key for an app and saves it.
func (ks *keyStore) add(app string) string {
	key := randomHex(16)
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.apps[key] = app
	ks.saveLocked()
	return key
}

// saveLocked writes the keys to the file. The caller holds mu.
func (ks *keyStore) saveLocked() {
	if ks.file == "" {
		return
	}
	b, _ := json.MarshalIndent(ks.apps, "", "  ")
	os.MkdirAll(filepath.Dir(ks.file), 0o700)
	tmp := ks.file + ".new"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, ks.file)
	}
}

// KeyEntry is an app key for the list of the deck. ID is a short hash of the key: the list never
// shows the key itself.
type KeyEntry struct {
	ID  string
	App string
}

// keyID is the ID of a key in the list: the first 8 hex digits of its SHA-256.
func keyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:4])
}

// list returns the app keys, by the name of the app.
func (ks *keyStore) list() []KeyEntry {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	var out []KeyEntry
	for k, app := range ks.apps {
		out = append(out, KeyEntry{ID: keyID(k), App: app})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// remove removes the app key with the ID, and reports that it had it.
func (ks *keyStore) remove(id string) bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	for k := range ks.apps {
		if keyID(k) == id {
			delete(ks.apps, k)
			ks.saveLocked()
			return true
		}
	}
	return false
}

// DeckKeys are the app keys of the whole deck: a key that works for each printer of the deck and
// for the list of the printers (/slicer/printer.json). The App Keys request at the root of the deck
// gives one (Server.DeckWide). The file is in the state folder of the deck.
type DeckKeys struct{ ks *keyStore }

// NewDeckKeys reads the deck keys of file.
func NewDeckKeys(file string) *DeckKeys { return &DeckKeys{ks: newKeyStore("", file)} }

// ValidRequest reports that the request has a deck key (X-Api-Key, or Authorization: Bearer).
func (d *DeckKeys) ValidRequest(r *http.Request) bool {
	return d != nil && d.ks.valid(requestKey(r))
}

// List returns the deck keys.
func (d *DeckKeys) List() []KeyEntry { return d.ks.list() }

// Remove removes the deck key with the ID.
func (d *DeckKeys) Remove(id string) bool { return d.ks.remove(id) }

// Keys returns the app keys of this printer.
func (s *Server) Keys() []KeyEntry { return s.keys.list() }

// RemoveKey removes the app key of this printer with the ID.
func (s *Server) RemoveKey(id string) bool { return s.keys.remove(id) }
