package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"printdeck/internal/printer"
)

// The state of the simulated printer for the 3D window of the desktop app (desktop/Sim3D.qml):
//
//	GET /sim/state  the live position of the tool head and the bed, the temperatures, the fan,
//	                and the place of the print in its file
//	GET /sim/path?key=K&from=N  the extruding moves of the print from move N, up to the place of
//	                the print: the part that is printed. A new print has a new key.
//
// ok checks the token of the window.
func registerSim(mux *http.ServeMux, k *printer.Klippy, ok func(*http.Request) bool) {
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !ok(r) {
				http.Error(w, "no access", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			h(w, r)
		}
	}
	mux.HandleFunc("GET /sim/state", auth(func(w http.ResponseWriter, r *http.Request) {
		st := k.State()
		out := map[string]any{"state": st.Text, "busy": st.Busy(), "file": st.File, "filepos": st.FilePos,
			"tool": st.Tool, "toolT": st.ToolTarget, "bed": st.Bed, "bedT": st.BedTarget, "fan": st.Fan / 100}
		if q, err := k.Query(map[string][]string{"motion_report": {"live_position", "live_extruder_velocity"},
			"toolhead": {"homed_axes"}}); err == nil {
			out["pos"] = q["motion_report"]["live_position"]
			out["ev"] = q["motion_report"]["live_extruder_velocity"]
			out["homed"] = q["toolhead"]["homed_axes"]
		}
		json.NewEncoder(w).Encode(out)
	}))
	mux.HandleFunc("GET /sim/path", auth(func(w http.ResponseWriter, r *http.Request) {
		st := k.State()
		upto := int64(math.MaxInt64) // a print that ended: all of it
		if st.Busy() {
			upto = st.FilePos
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		key, first, segs := k.View.Toolpath(r.URL.Query().Get("key"), from, upto, 20000)
		// The moves as one list of numbers, 6 for each move, to 0.01 mm.
		flat := make([]float64, 0, 6*len(segs))
		round := func(v float32) float64 { return math.Round(float64(v)*100) / 100 }
		for _, s := range segs {
			flat = append(flat, round(s.X0), round(s.Y0), round(s.Z0), round(s.X1), round(s.Y1), round(s.Z1))
		}
		json.NewEncoder(w).Encode(map[string]any{"key": key, "from": first, "segs": flat})
	}))
}
