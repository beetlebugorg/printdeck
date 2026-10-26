// Package octoapi serves the part of the OctoPrint REST API that slicers use, so Cura, OrcaSlicer,
// PrusaSlicer and others can send files to the Printdeck and start prints. The Printdeck shows its
// own name, and says "OctoPrint" only in the version text that the slicers check.
//
// The API is defined by OctoPrint (https://docs.octoprint.org/en/master/api/, AGPL-3.0). This is our
// own code: it follows the documentation and the requests of the slicers, and has none of
// OctoPrint's code. Moonraker's octoprint_compat.py (GPL-3.0) showed which endpoints the slicers need.
//
//	octoapi.go  the server, the routes and the API keys
//	printer.go  version, printer, job, connection and commands
//	files.go    the file list, upload, select and print, delete, download
//	appkeys.go  the App Keys flow: an app asks for a key, and the touch screen asks the user
//	printdeck.go the loaded filament, for the Printdeck plugin for Cura
//	estimate.go the time of a print of a file, for the Printdeck plugin for Cura
//	webcam.go   the webcam: the camera of the deck, or its screen
package octoapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// The version that the slicers see. OrcaSlicer checks that the text starts with "OctoPrint".
const (
	apiVersion    = "0.1"
	serverVersion = "1.11.0"
)

// Printer is what the API needs of the printer: the deck's printer backend.
type Printer interface {
	State() printer.State
	PrintFile(path string)
	Pause()
	Resume()
	Cancel()
	Connect()
	Gcode(lines ...string) error
	Home(axes ...string) error
	Jog(x, y, z float64, speed int) error
	SetToolTemp(t int) error
	SetBedTemp(t int) error
}

// Server is the OctoPrint-compatible API.
type Server struct {
	Printer Printer
	Store   *gcodestore.Store
	// Version is the Printdeck version, for the version text.
	Version string
	// Ask shows a question on the touch screen, for the App Keys flow. answer gets true for yes.
	// When it is nil, apps cannot get a key.
	Ask func(question, yes, no string, answer func(bool))
	// The loaded filament, for the Printdeck plugin for Cura (printdeck.go).
	printdeckFields
	// The time of a print of a file, for the Printdeck plugin for Cura (estimate.go).
	estimateFields
	// The phones of the Printdeck app for push messages (push.go).
	pushFields

	keys *keyStore

	mu       sync.Mutex
	requests map[string]*keyRequest // App Keys requests by token
}

// New makes the API. mainKey is the key of the Printdeck user (from printdeck-credentials), and
// keyFile keeps the keys that apps got. Both can be empty.
func New(p Printer, store *gcodestore.Store, mainKey, keyFile string) *Server {
	return &Server{Printer: p, Store: store, keys: newKeyStore(mainKey, keyFile), requests: map[string]*keyRequest{}}
}

// Register adds the routes of the API to mux.
func (s *Server) Register(mux *http.ServeMux) {
	// With no key, OctoPrint answers the version and the App Keys probe, so a slicer can find it.
	mux.HandleFunc("GET /api/version", s.serveVersion)
	mux.HandleFunc("GET /plugin/appkeys/probe", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("POST /plugin/appkeys/request", s.requestKey)
	mux.HandleFunc("GET /plugin/appkeys/request/{token}", s.pollKey)

	mux.HandleFunc("GET /api/server", s.key(s.serveServer))
	mux.HandleFunc("GET /api/settings", s.key(s.serveSettings))
	mux.HandleFunc("GET /api/currentuser", s.key(s.serveUser))
	mux.HandleFunc("POST /api/login", s.key(s.serveUser))
	mux.HandleFunc("GET /api/printerprofiles", s.key(s.serveProfiles))
	mux.HandleFunc("GET /api/printer", s.key(s.servePrinter))
	mux.HandleFunc("POST /api/printer/command", s.key(s.serveCommand))
	mux.HandleFunc("POST /api/printer/printhead", s.key(s.servePrinthead))
	mux.HandleFunc("POST /api/printer/tool", s.key(s.serveTool))
	mux.HandleFunc("POST /api/printer/bed", s.key(s.serveBed))
	mux.HandleFunc("GET /api/job", s.key(s.serveJob))
	mux.HandleFunc("POST /api/job", s.key(s.serveJobCommand))
	mux.HandleFunc("GET /api/connection", s.key(s.serveConnection))
	mux.HandleFunc("POST /api/connection", s.key(s.serveConnect))
	mux.HandleFunc("GET /api/files", s.key(s.serveFiles))
	mux.HandleFunc("GET /api/files/local", s.key(s.serveFiles))
	mux.HandleFunc("POST /api/files/local", s.key(s.serveUpload))
	mux.HandleFunc("GET /api/files/local/{path...}", s.key(s.serveFile))
	mux.HandleFunc("POST /api/files/local/{path...}", s.key(s.serveFileCommand))
	mux.HandleFunc("DELETE /api/files/local/{path...}", s.key(s.serveDelete))
	mux.HandleFunc("GET /downloads/files/local/{path...}", s.key(s.serveDownload))
	mux.HandleFunc("GET /api/plugin/printdeck", s.key(s.servePrintdeck))
	mux.HandleFunc("POST /api/plugin/printdeck/materials", s.key(s.serveSlicerMaterials))
	mux.HandleFunc("POST /api/plugin/printdeck/roll", s.key(s.serveRollMaterial))
	mux.HandleFunc("GET /api/plugin/printdeck/model.png", s.key(s.serveModel))
	mux.HandleFunc("POST /api/plugin/printdeck/estimate", s.key(s.serveEstimate))
	mux.HandleFunc("POST /api/plugin/printdeck/push/pair", s.key(s.servePair))
	mux.HandleFunc("GET /api/plugin/printdeck/push", s.key(s.servePhones))
	mux.HandleFunc("DELETE /api/plugin/printdeck/push/{id}", s.key(s.serveUnpair))
	mux.HandleFunc("GET /webcam/stream", s.webcam(s.serveStream))
	mux.HandleFunc("GET /webcam/snapshot", s.webcam(s.serveSnapshot))
}

// key lets a request through with a valid API key: in the X-Api-Key header, as a Bearer token, or
// as the apikey parameter.
func (s *Server) key(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		k := requestKey(r)
		if k == "" || !s.keys.valid(k) {
			apiError(w, http.StatusForbidden, "Invalid API key")
			return
		}
		h(w, r)
	}
}

// ValidKey tells if a request has a valid API key. The remote screen uses it, so that a slicer
// plugin can show the deck UI with its API key.
func (s *Server) ValidKey(r *http.Request) bool {
	k := requestKey(r)
	return k != "" && s.keys.valid(k)
}

// requestKey is the API key of a request, or "".
func requestKey(r *http.Request) string {
	if k := r.Header.Get("X-Api-Key"); k != "" {
		return k
	}
	if k := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); k != "" {
		return k
	}
	return r.URL.Query().Get("apikey")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// apiError answers like OctoPrint: {"error": "..."}.
func apiError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// equalKeys compares two keys in constant time.
func equalKeys(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
