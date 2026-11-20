package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"printdeck/internal/printer"

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
		// A JSON file for the apps: the zslice web app reads it from its own origin (remote.CORS),
		// so it gets no headers of the page of the app (slicerHeaders).
		w.Header().Set("X-Content-Type-Options", "nosniff")
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
	// Files is the print file that the printer takes: "gcode" (Klipper), "gcode.3mf" (a Bambu
	// Lab printer: a sliced 3MF with Metadata/plate_<n>.gcode), or "ctb" (a ChiTu resin printer,
	// from ChiTuBox only). Model is the model that the printer reports, for example "Bambu Lab
	// X2D", or "".
	Files string `json:"files"`
	Model string `json:"model,omitempty"`
	slicerLive
}

// slicerLive is the state of a printer now, for the printer list of a slicer: State is "ready",
// "printing", "paused", "connecting", "offline" or "error". Progress is 0 to 1, and Left the
// seconds left, during a print (Left is -1 when the printer does not know). Job is the name of
// the print. Camera is the address of the stream of the camera of the printer, from the address of
// the printer, or "".
type slicerLive struct {
	State    string  `json:"state,omitempty"`
	Progress float64 `json:"progress,omitempty"`
	Left     int     `json:"left,omitempty"`
	Job      string  `json:"job,omitempty"`
	Camera   string  `json:"camera,omitempty"`
}

// liveOf returns the state of the printer pr now, and its camera: camera is true when the printer
// has a camera of its own.
func liveOf(st printer.State, camera bool, url string) slicerLive {
	l := slicerLive{State: "ready"}
	switch {
	case st.Starting || !st.Reachable && st.Err == "" && !st.ErrFinal:
		l.State = "connecting"
	case !st.Reachable || !st.Connected:
		l.State = "offline"
	case st.Err != "":
		l.State = "error"
	case st.Busy():
		l.State = "printing"
		if st.Paused {
			l.State = "paused"
		}
		l.Progress, l.Left, l.Job = st.Completion/100, -1, st.Name()
		if st.HasTimeLeft {
			l.Left = int(st.TimeLeft.Seconds())
		}
	}
	if camera {
		l.Camera = url + "/webcam/stream"
	}
	return l
}

// printerLive returns the state and the camera of the printer with the ID ("" for the first
// printer), for /slicer/printer.json. The deck sets it at its start.
var printerLive func(id, url string) slicerLive

// printerFormat returns the file format and the model of the printer with the ID, for
// /slicer/printer.json ("" for the first printer). The deck sets it at its start.
var printerFormat func(id string) (files, model string)

// backendFormat is the file format and the model of a printer backend.
func backendFormat(pr printerBackend) (files, model string) {
	switch b := pr.(type) {
	case *printer.Bambu:
		model, _ = b.Client.Version()
		return "gcode.3mf", model
	case *printer.ChiTu:
		return "ctb", b.Machine().Board
	}
	return "gcode", ""
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
		// The file format and the model of the printer at url (the first printer).
		Files string `json:"files,omitempty"`
		Model string `json:"model,omitempty"`
		slicerLive
	}{Name: system.DeckName(), Kind: "printdeck", HTTPS: httpsURL()}
	if printerFormat != nil {
		out.Files, out.Model = printerFormat("")
	}
	if printerLive != nil {
		out.slicerLive = printerLive("", "")
	}
	if deckPrinterList != nil {
		for i, id := range deckPrinterList() {
			p := slicerPrinter{ID: id.ID, Name: id.Name, Label: id.Label, Kind: "fdm",
				Color: fmt.Sprintf("#%02x%02x%02x", id.Color.R, id.Color.G, id.Color.B)}
			if printerFormat != nil {
				key := id.ID
				if i == 0 {
					key = ""
				}
				p.Files, p.Model = printerFormat(key)
			}
			if id.Resin {
				p.Kind = "resin"
			}
			if i > 0 {
				p.URL = "/printers/" + id.ID
			}
			if printerLive != nil {
				key := id.ID
				if i == 0 {
					key = ""
				}
				p.slicerLive = printerLive(key, p.URL)
			}
			out.Printers = append(out.Printers, p)
		}
	}
	return out
}
