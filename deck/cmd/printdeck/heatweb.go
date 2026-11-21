package main

import (
	"encoding/json"
	"net/http"

	"printdeck/internal/printer"
	"printdeck/internal/remote"
)

// heatTimer is a printer backend with a heat log (printer.HeatLog).
type heatTimer interface {
	HeatTimes() printer.HeatReport
}

// registerHeat serves GET /slicer/heat.json on mux: the heat-up and the cool-down times of the
// printer pr (printer.HeatReport). The file needs the login of the deck, or a key that apiKey
// accepts. A printer with no heat log has no file (404).
//
// The root of the deck serves the file of the first printer, with its key or a deck key. Each
// other printer has its file at /printers/<id>/slicer/heat.json, with its key or a deck key.
func registerHeat(mux *http.ServeMux, rs *remote.Server, pr any, apiKey func(*http.Request) bool) {
	file := func(w http.ResponseWriter, r *http.Request) {
		ht, ok := pr.(heatTimer)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(ht.HeatTimes())
	}
	authed := rs.Auth(file)
	mux.HandleFunc("GET /slicer/heat.json", func(w http.ResponseWriter, r *http.Request) {
		if apiKey != nil && apiKey(r) {
			file(w, r)
			return
		}
		authed(w, r)
	})
}
