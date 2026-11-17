package main

import (
	"encoding/json"
	"net/http"
	"os"

	"printdeck/internal/remote"
	"printdeck/internal/system"
)

// slicerDir is the folder of the zslice web app in the image (os/package/printdeck-zslice).
const slicerDir = "/usr/share/printdeck/zslice"

// slicerCSP lets the zslice app run its WebAssembly and its workers. The app loads no file from
// other sites, and connects only to the deck.
const slicerCSP = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' blob: data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// registerSlicer serves the zslice web app at /slicer/, for the browsers with the login of the
// deck. The app slices in the browser, and sends the G-code to the deck (POST /api/files/local,
// with the login). A print starts only after a press on the touch screen. An image with no zslice
// app has no /slicer/ page.
func registerSlicer(mux *http.ServeMux, rs *remote.Server, dir string) {
	if !dirExists(dir) {
		return
	}
	files := http.StripPrefix("/slicer/", http.FileServer(http.Dir(dir)))
	mux.HandleFunc("GET /slicer", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/slicer/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /slicer/", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		slicerHeaders(w)
		files.ServeHTTP(w, r)
	}))
	// The app adds the deck as its printer. The empty url is the server of the page. The file
	// has no API key: the upload uses the login.
	mux.HandleFunc("GET /slicer/printer.json", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		slicerHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]string{"name": system.DeckName(), "kind": "printdeck", "url": ""})
	}))
}

// slicerHeaders isolate the page, so that the app can slice on more than one thread
// (SharedArrayBuffer). They replace the policy of remote.SecurityHeaders for these files.
func slicerHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Embedder-Policy", "require-corp")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Security-Policy", slicerCSP)
	h.Set("X-Content-Type-Options", "nosniff")
}

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
