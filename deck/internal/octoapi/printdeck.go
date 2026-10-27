package octoapi

import (
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strconv"

	"printdeck/internal/printer"
)

// The API of the Printdeck plugin for Cura, at the path of an OctoPrint plugin. It tells which
// filament is in the printer, so that Cura can choose the material and warn when the roll has too
// little filament for a model.
//
//	GET /api/plugin/printdeck
//
//	{"material": "PLA", "diameter": 2.85,
//	 "spool": {"id": "3", "name": "Galaxy Black", "material": "PLA", "left": 642, "full": 1000, "price": 22},
//	 "spools": [{"id": "3", "name": "Galaxy Black", ...}, {"id": "4", "name": "Signal Orange", ...}],
//	 "job": {"file": "benchy.gcode", "state": "printing", "layer": 12, "layers": 27, "progress": 0.42, "left": 4980},
//	 "camera": {"name": "C270 HD WEBCAM", "state": "idle", "width": 1280, "height": 720, "fps": 15},
//	 "cameras": [{"id": "/dev/v4l/by-id/usb-...", "number": 1, "roles": ["main", "lapse", "picture"],
//	              "name": "C270 HD WEBCAM", "state": "idle", ...}]}
//
// material is the loaded material, or "". spool is the roll in the printer, or null. left and full
// are grams of filament, and price is the price of the roll, or 0. spools is the list of all rolls,
// with the same fields: the plugin makes a material of Cura for each roll. job is the print that runs, or
// null: its state ("printing" or "paused"), its layer, its progress from 0 to 1, and the seconds
// left (-1 when the time left is not known). camera is the camera of the deck, or null when the
// deck has no camera on: its name, its state ("idle" with no viewer, "starting", "on" or "error"),
// and the size and the rate of its frames when it has run. camera is the main camera. cameras is the
// list of all cameras, with the same fields, the ID and the number of each for the camera parameter
// of the webcam (webcam.go), and the roles of each: main, lapse (the time lapse) and picture (the
// first layer picture and the pictures of the notifications). A camera that is off has the state
// "off", and a camera of the settings that the deck does not find has the state "none".
//
// The webcam of the API (webcam.go) is the camera of the deck, or the screen of the deck for a
// printer with no camera.

// spoolJSON is a roll of filament in the API.
type spoolJSON struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Material string  `json:"material"`
	Left     float64 `json:"left"`
	Full     float64 `json:"full"`
	Price    float64 `json:"price"`
	// CuraMaterial is the base file of the material of Cura of the roll, or "".
	CuraMaterial string `json:"cura_material,omitempty"`
	Color        string `json:"color,omitempty"`
}

// rollJSON is a roll in the API.
func rollJSON(sp printer.Spool) spoolJSON {
	return spoolJSON{ID: sp.ID, Name: sp.Label(), Material: sp.Material, Left: sp.Left(), Full: sp.Capacity(), Price: sp.Price,
		CuraMaterial: sp.CuraMaterial, Color: sp.Color}
}

// jobJSON is the print that runs, in the API.
type jobJSON struct {
	File     string  `json:"file"`
	State    string  `json:"state"`
	Layer    int     `json:"layer"`
	Layers   int     `json:"layers"`
	Progress float64 `json:"progress"`
	Left     float64 `json:"left"`
	// Release is the seconds until the bed after the print is cool enough to remove the part;
	// -1 is not known.
	Release float64 `json:"release"`
	// Pos is the bytes of the file that the printer has read. The Cura plugin finds the move that
	// prints from it.
	Pos int64 `json:"pos"`
}

type printdeckJSON struct {
	Material string      `json:"material"`
	Diameter float64     `json:"diameter"`
	Spool    *spoolJSON  `json:"spool"`
	Spools   []spoolJSON `json:"spools"`
	Job      *jobJSON    `json:"job"`
	// Camera is the main camera of the deck, or null when the webcam is the screen of the deck.
	Camera *cameraJSON `json:"camera"`
	// Cameras are all cameras of the deck.
	Cameras []cameraJSON `json:"cameras"`
	// LCD is the status screen of the printer display: the state, the message, the heaters, the
	// fan (0 to 1), and the progress, for an app that draws the display.
	LCD any `json:"lcd,omitempty"`
}

func (s *Server) servePrintdeck(w http.ResponseWriter, r *http.Request) {
	out := printdeckJSON{Camera: s.cameraInfo(), Cameras: s.camerasInfo()}
	if s.Status != nil {
		out.LCD = s.Status()
	}
	if s.Material != nil {
		out.Material = s.Material()
	}
	if s.Diameter != nil {
		out.Diameter = s.Diameter()
	}
	if s.Spools != nil {
		if sp, ok := s.Spools.In(); ok {
			j := rollJSON(sp)
			out.Spool = &j
			out.Material = sp.Material
		}
		out.Spools = []spoolJSON{}
		for _, sp := range s.Spools.List() {
			out.Spools = append(out.Spools, rollJSON(sp))
		}
	}
	if st := s.Printer.State(); st.Busy() {
		j := &jobJSON{File: st.FilePath, State: "printing", Layer: st.Layer, Layers: st.Layers,
			Progress: st.Completion / 100, Left: -1, Release: -1, Pos: st.FilePos}
		if st.Paused {
			j.State = "paused"
		}
		if st.HasTimeLeft {
			j.Left = st.TimeLeft.Seconds()
		}
		if st.HasReleaseLeft {
			j.Release = st.ReleaseLeft.Seconds()
		}
		out.Job = j
	}
	writeJSON(w, http.StatusOK, out)
}

// serveSlicerMaterials takes the materials of Cura for the rolls: a JSON list of
// printer.SlicerMaterial. The new-roll form of the deck offers them.
func (s *Server) serveSlicerMaterials(w http.ResponseWriter, r *http.Request) {
	if s.Spools == nil {
		http.NotFound(w, r)
		return
	}
	var list []printer.SlicerMaterial
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&list); err != nil {
		apiError(w, http.StatusBadRequest, "bad materials")
		return
	}
	var keep []printer.SlicerMaterial
	for _, p := range list {
		if p.ID != "" && p.Name != "" && len(keep) < 200 {
			if p.Density < 0.5 || p.Density > 3 {
				p.Density = 0 // not a density of a filament: the deck uses its own
			}
			keep = append(keep, p)
		}
	}
	s.Spools.SetSlicerMaterials(keep)
	w.WriteHeader(http.StatusNoContent)
}

// serveRollMaterial links a roll to a material of Cura: {"id": the roll, "cura_material": the
// base file, "color": "#rrggbb"}. The Cura plugin makes a material for a roll that has none.
func (s *Server) serveRollMaterial(w http.ResponseWriter, r *http.Request) {
	if s.Spools == nil {
		http.NotFound(w, r)
		return
	}
	var in struct {
		ID           string `json:"id"`
		CuraMaterial string `json:"cura_material"`
		Color        string `json:"color"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.ID == "" {
		apiError(w, http.StatusBadRequest, "bad roll")
		return
	}
	if _, ok := s.Spools.Get(in.ID); !ok {
		apiError(w, http.StatusNotFound, "no such roll")
		return
	}
	s.Spools.SetCuraMaterial(in.ID, printer.SlicerMaterial{ID: in.CuraMaterial, Color: in.Color})
	w.WriteHeader(http.StatusNoContent)
}

// The fields of the Server for the Printdeck API.
type printdeckFields struct {
	// Spools are the rolls of filament, or nil.
	Spools *printer.Spools
	// Material gives the loaded material when no roll is in the printer, or nil.
	Material func() string
	// Diameter gives the filament diameter of the printer in mm, or nil.
	Diameter func() float64
	// Frame draws the screen of the deck for the webcam, or nil for no webcam.
	Frame func() image.Image
	// Camera is the camera of the deck, or nil (webcam.go).
	Camera Camera
	// Allow lets a request to the webcam through with no API key, or is nil: the login of the
	// remote screen, or the token of the desktop app.
	Allow func(r *http.Request) bool
	// StartPrint starts a print of a file in place of Printer.PrintFile, or nil. The deck first
	// checks that the material of the file is in the printer, and asks on the touch screen.
	StartPrint func(path string)
	// Status gives the status screen of the printer display (lcd.Status), or nil.
	Status func() any
	// Model draws the model of the print for an app (printer.View.ModelPicture), or nil.
	Model func(st printer.State, w, h int, mono bool) image.Image
}

// startPrint starts a print of a file, with StartPrint when the Server has it.
func (s *Server) startPrint(path string) {
	if s.StartPrint != nil {
		s.StartPrint(path)
		return
	}
	s.Printer.PrintFile(path)
}

// serveModel is the picture of the model of the print, as a PNG of w x h pixels (16 to 512, 128
// by default). mono=1 gives three grays for a display of dots. It is 404 when no print runs, or
// when its G-code is not loaded yet.
func (s *Server) serveModel(w http.ResponseWriter, r *http.Request) {
	st := s.Printer.State()
	if s.Model == nil || !st.Busy() {
		http.NotFound(w, r)
		return
	}
	size := func(name string) int {
		n, err := strconv.Atoi(r.URL.Query().Get(name))
		if err != nil {
			return 128
		}
		return max(16, min(512, n))
	}
	img := s.Model(st, size("w"), size("h"), r.URL.Query().Get("mono") == "1")
	if img == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	png.Encode(w, img)
}
