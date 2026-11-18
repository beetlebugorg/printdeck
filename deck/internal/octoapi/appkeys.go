package octoapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	question := "Allow " + app + " to use the Printdeck?"
	var keyID string
	if req.PublicKey != "" && s.Signing != nil {
		pub, err := signing.ParsePublic(req.PublicKey)
		if err != nil {
			apiError(w, http.StatusBadRequest, "The public key does not read")
			return
		}
		keyID = signing.KeyID(pub)
		question = "Allow " + app + " to use the Printdeck, and trust its prints (key " + keyID + ")?"
	}
	token := randomHex(16)
	kr := &keyRequest{app: app, created: time.Now()}
	s.mu.Lock()
	s.dropOldRequests()
	s.requests[token] = kr
	s.mu.Unlock()

	s.Ask(question, "Allow", "Deny", func(yes bool) {
		key := ""
		if yes {
			key = s.keys.add(app)
			if keyID != "" {
				if _, err := s.Signing.Add(app, req.PublicKey, time.Now()); err != nil {
					log.Printf("appkeys: the key of %s: %v", app, err)
				}
			}
		}
		s.mu.Lock()
		kr.decided, kr.key = true, key
		if key != "" {
			kr.trusted = keyID
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
	var decided bool
	var key, trusted string
	if kr != nil {
		decided, key, trusted = kr.decided, kr.key, kr.trusted
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
		out := map[string]string{"api_key": key}
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
	if ks.file != "" {
		b, _ := json.MarshalIndent(ks.apps, "", "  ")
		os.MkdirAll(filepath.Dir(ks.file), 0o700)
		tmp := ks.file + ".new"
		if os.WriteFile(tmp, b, 0o600) == nil {
			os.Rename(tmp, ks.file)
		}
	}
	return key
}
