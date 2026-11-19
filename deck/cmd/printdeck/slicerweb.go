package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"printdeck/internal/remote"
	"printdeck/internal/system"
	"printdeck/internal/ui"
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
func registerSlicer(mux *http.ServeMux, rs *remote.Server, dir string, deckKey func(*http.Request) bool) {
	// The app adds the deck as its printer. The empty url is the server of the page, and https
	// the HTTPS address of the deck, or "" (https.go). A deck with many printers also lists each
	// printer in printers: its url is "" for the first printer and /printers/<id> for another. The
	// file needs the login, or a deck key (an App Keys key of the whole deck): a slicer app reads
	// it after the Allow on the deck.
	printerFile := func(w http.ResponseWriter, r *http.Request) {
		slicerHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(slicerPrinterFile())
	}
	authed := rs.Auth(printerFile)
	mux.HandleFunc("GET /slicer/printer.json", func(w http.ResponseWriter, r *http.Request) {
		if deckKey != nil && deckKey(r) {
			printerFile(w, r)
			return
		}
		authed(w, r)
	})
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

// slicerPrinter is a printer of the deck in /slicer/printer.json.
type slicerPrinter struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Color string `json:"color"` // "#rrggbb"
	Kind  string `json:"kind"`  // "fdm" or "resin"
	URL   string `json:"url"`
}

// deckPrinterList returns the printers of a deck with many printers, or is nil.
var deckPrinterList func() []ui.Identity

// slicerPrinterFile is the content of /slicer/printer.json.
func slicerPrinterFile() any {
	out := struct {
		Name     string          `json:"name"`
		Kind     string          `json:"kind"`
		URL      string          `json:"url"`
		HTTPS    string          `json:"https"`
		Printers []slicerPrinter `json:"printers,omitempty"`
	}{Name: system.DeckName(), Kind: "printdeck", HTTPS: httpsURL()}
	if deckPrinterList != nil {
		for i, id := range deckPrinterList() {
			p := slicerPrinter{ID: id.ID, Name: id.Name, Label: id.Label, Kind: "fdm",
				Color: fmt.Sprintf("#%02x%02x%02x", id.Color.R, id.Color.G, id.Color.B)}
			if id.Resin {
				p.Kind = "resin"
			}
			if i > 0 {
				p.URL = "/printers/" + id.ID
			}
			out.Printers = append(out.Printers, p)
		}
	}
	return out
}
