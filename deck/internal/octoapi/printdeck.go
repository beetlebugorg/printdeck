package octoapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"time"

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
//	 "job": {"file": "benchy.gcode", "state": "printing", "layer": 12, "layers": 27, "progress": 0.42, "left": 4980}}
//
// material is the loaded material, or "". spool is the roll in the printer, or null. left and full
// are grams of filament, and price is the price of the roll, or 0. spools is the list of all rolls,
// with the same fields: the plugin makes a material of Cura for each roll. job is the print that runs, or
// null: its state ("printing" or "paused"), its layer, its progress from 0 to 1, and the seconds
// left (-1 when the time left is not known).
//
// The webcam of the API is the screen of the deck, for a printer with no camera: /webcam/stream is
// an MJPEG stream with one frame a second, and /webcam/snapshot is one JPEG.

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
	return spoolJSON{ID: sp.ID, Name: sp.Label(), Material: sp.Material, Left: sp.Left(), Full: sp.Full, Price: sp.Price,
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
}

type printdeckJSON struct {
	Material string      `json:"material"`
	Diameter float64     `json:"diameter"`
	Spool    *spoolJSON  `json:"spool"`
	Spools   []spoolJSON `json:"spools"`
	Job      *jobJSON    `json:"job"`
}

func (s *Server) servePrintdeck(w http.ResponseWriter, r *http.Request) {
	out := printdeckJSON{}
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
			Progress: st.Completion / 100, Left: -1}
		if st.Paused {
			j.State = "paused"
		}
		if st.HasTimeLeft {
			j.Left = st.TimeLeft.Seconds()
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

// frameJPEG is a frame of the screen as a JPEG.
func (s *Server) frameJPEG() ([]byte, error) {
	var b bytes.Buffer
	err := jpeg.Encode(&b, s.Frame(), &jpeg.Options{Quality: 80})
	return b.Bytes(), err
}

func (s *Server) serveSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.Frame == nil {
		http.NotFound(w, r)
		return
	}
	b, err := s.frameJPEG()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

// serveStream sends a frame of the screen each second, until the client goes.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request) {
	if s.Frame == nil {
		http.NotFound(w, r)
		return
	}
	const boundary = "printdeckframe"
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-store")
	flush := http.NewResponseController(w)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		b, err := s.frameJPEG()
		if err != nil {
			return
		}
		fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(b))
		w.Write(b)
		w.Write([]byte("\r\n"))
		if flush.Flush() != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
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
	// StartPrint starts a print of a file in place of Printer.PrintFile, or nil. The deck first
	// checks that the material of the file is in the printer, and asks on the touch screen.
	StartPrint func(path string)
}

// startPrint starts a print of a file, with StartPrint when the Server has it.
func (s *Server) startPrint(path string) {
	if s.StartPrint != nil {
		s.StartPrint(path)
		return
	}
	s.Printer.PrintFile(path)
}
