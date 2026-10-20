package octoapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The App Keys flow of OctoPrint (https://docs.octoprint.org/en/master/bundledplugins/appkeys.html):
//
//  1. The app posts {"app": "Cura"} to /plugin/appkeys/request, and gets a token.
//  2. The touch screen asks "Allow Cura to use the Printdeck?".
//  3. The app polls /plugin/appkeys/request/<token>: 202 while the user decides, 200 with the key
//     after "Allow", and 404 after "Deny" or when the request is too old.

// requestTimeout is how long a request waits for the user.
const requestTimeout = 2 * time.Minute

type keyRequest struct {
	app     string
	created time.Time
	decided bool
	key     string // the new key after "Allow"; "" after "Deny"
}

func (s *Server) requestKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App string `json:"app"`
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
	token := randomHex(16)
	kr := &keyRequest{app: app, created: time.Now()}
	s.mu.Lock()
	s.dropOldRequests()
	s.requests[token] = kr
	s.mu.Unlock()

	s.Ask("Allow "+app+" to use the Printdeck?", "Allow", "Deny", func(yes bool) {
		key := ""
		if yes {
			key = s.keys.add(app)
		}
		s.mu.Lock()
		kr.decided, kr.key = true, key
		s.mu.Unlock()
	})
	w.Header().Set("Location", baseURL(r)+"/plugin/appkeys/request/"+token)
	writeJSON(w, http.StatusCreated, map[string]string{"app_token": token})
}

func (s *Server) pollKey(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.dropOldRequests()
	kr := s.requests[r.PathValue("token")]
	var decided bool
	var key string
	if kr != nil {
		decided, key = kr.decided, kr.key
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
		writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
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
