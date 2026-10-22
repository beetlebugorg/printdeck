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
// The places are the places of the printer, in mm: /sim/path moves the G-code by the G-code
// offsets and the bed tilt, as the host moves the nozzle.
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
			"toolhead": {"homed_axes"}, "simmcu": {"place"}}); err == nil {
			// The place of the nozzle on the simulated board, in mm: it moves before the homing too.
			out["pos"] = q["simmcu"]["place"]
			if out["pos"] == nil {
				out["pos"] = q["motion_report"]["live_position"]
			}
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
		place := gcodeToPlace(k)
		// The moves as one list of numbers, 6 for each move, to 0.01 mm.
		flat := make([]float64, 0, 6*len(segs))
		round := func(v float64) float64 { return math.Round(v*100) / 100 }
		for _, s := range segs {
			for _, p := range [][3]float32{{s.X0, s.Y0, s.Z0}, {s.X1, s.Y1, s.Z1}} {
				x, y, z := place(float64(p[0]), float64(p[1]), float64(p[2]))
				flat = append(flat, round(x), round(y), round(z))
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"key": key, "from": first, "segs": flat})
	}))
}

// gcodeToPlace is the move of the host from a G-code position to the place of the nozzle: the
// G-code offsets (gcode_move: position less gcode_position), then the bed tilt in Z.
func gcodeToPlace(k *printer.Klippy) func(x, y, z float64) (float64, float64, float64) {
	q, err := k.Query(map[string][]string{"gcode_move": {"position", "gcode_position"},
		"bed_tilt": {"x_adjust", "y_adjust", "z_adjust"}})
	if err != nil {
		return func(x, y, z float64) (float64, float64, float64) { return x, y, z }
	}
	num := func(v any, i int) float64 {
		a, _ := v.([]any)
		if i >= len(a) {
			return 0
		}
		f, _ := a[i].(float64)
		return f
	}
	var base [3]float64
	for i := range base {
		base[i] = num(q["gcode_move"]["position"], i) - num(q["gcode_move"]["gcode_position"], i)
	}
	xa, _ := q["bed_tilt"]["x_adjust"].(float64)
	ya, _ := q["bed_tilt"]["y_adjust"].(float64)
	za, _ := q["bed_tilt"]["z_adjust"].(float64)
	return func(x, y, z float64) (float64, float64, float64) {
		x, y, z = x+base[0], y+base[1], z+base[2]
		return x, y, z + x*xa + y*ya + za
	}
}
