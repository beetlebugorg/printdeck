package octoapi

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strconv"

	"printdeck/internal/klipper/toolhead"
	"printdeck/internal/printer"
	"printdeck/internal/queue"
	"printdeck/internal/signing"
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
	File string `json:"file"`
	// Title is the name of the print for the screens: the title in the file, or else the cleaned
	// file name.
	Title    string  `json:"title"`
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
	// Process is the kind of printer: "fdm", or "resin". An app shows no Print and no Resume for a
	// resin printer.
	Process string `json:"process"`
	// PrinterID is the ID of the printer at the service, or "": the live tiles of the projects
	// name it (printer_id).
	PrinterID string      `json:"printer_id"`
	Material  string      `json:"material"`
	Diameter  float64     `json:"diameter"`
	Spool     *spoolJSON  `json:"spool"`
	Spools    []spoolJSON `json:"spools"`
	Job       *jobJSON    `json:"job"`
	// Camera is the main camera of the deck, or null when the webcam is the screen of the deck.
	Camera *cameraJSON `json:"camera"`
	// Cameras are all cameras of the deck.
	Cameras []cameraJSON `json:"cameras"`
	// LCD is the status screen of the printer display: the state, the message, the heaters, the
	// fan (0 to 1), and the progress, for an app that draws the display.
	LCD any `json:"lcd,omitempty"`
	// Projects are the names of the projects of the history, the newest first, for the project
	// pick of the plugin. Null when the deck has no history.
	Projects []string `json:"projects"`
	// Limits are the motion limits of the printer, for a slicer that plans the time of the moves
	// as the printer does. Null before the deck has read them.
	Limits *limitsJSON `json:"limits"`
	// SignedOnly is true when the deck accepts only G-code files with a good signature of a trusted
	// slicer.
	SignedOnly bool `json:"signed_only"`
	// Controls are the controls of the printer for the apps (controls.go): "jog", "home", "fan",
	// "speed", "flow", "zoffset", "extrude", "filament", "ams_dry", "chamber" and "tool1". The
	// state of the controls follows, at the top of the reply (controlState).
	Controls []string `json:"controls"`
	controlState
	// Filaments are the spools that the printer has loaded (filamentsOf), or null when the printer
	// does not tell them.
	Filaments []filamentJSON `json:"filaments,omitempty"`
	// Nozzles are the nozzles of a Bambu Lab printer, and Plate the code of the build plate that
	// it found on the heatbed (for example "P0101"), or "" (nozzlesOf).
	Nozzles []nozzleJSON `json:"nozzles,omitempty"`
	Plate   string       `json:"plate,omitempty"`
}

// nozzleJSON is a nozzle of a Bambu Lab printer: its id in the reports, its role ("main" with the
// AMS, "aux" with the external spool), its diameter in mm, and its type code as the printer
// reports it (for example "HS01").
type nozzleJSON struct {
	ID       int     `json:"id"`
	Role     string  `json:"role"`
	Diameter float64 `json:"diameter"`
	Type     string  `json:"type"`
}

// nozzlesOf returns the nozzles and the plate of a printer that reports them (printer.Bambu).
func nozzlesOf(p any) ([]nozzleJSON, string) {
	n, ok := p.(interface {
		Nozzles() []printer.NozzleInfo
		PlateID() string
	})
	if !ok {
		return nil, ""
	}
	var out []nozzleJSON
	for _, z := range n.Nozzles() {
		out = append(out, nozzleJSON{ID: z.ID, Role: z.Role, Diameter: z.Diameter, Type: z.Type})
	}
	return out, n.PlateID()
}

// filamentJSON is a spool that the printer has loaded. Slot is its name on the printer: "A1" for
// tray 1 of the first AMS, "Ext" for the external spool, "" for the roll of a printer with no
// AMS. Index is its place in the list, in the order of the trays. Tray is the number of the tray
// in the reports of a Bambu Lab printer (Unit*4+Index, 254 or 255 for an external spool), or -1.
// Nozzle is "main" or "aux" on a printer with two nozzles, else "". Remain is the part of a full
// spool that is left, 0 to 1, or null when it is not known.
type filamentJSON struct {
	Slot     string   `json:"slot"`
	Index    int      `json:"index"`
	Tray     int      `json:"tray"`
	Nozzle   string   `json:"nozzle,omitempty"`
	Material string   `json:"material"`
	Color    string   `json:"color,omitempty"`
	Name     string   `json:"name,omitempty"`
	Remain   *float64 `json:"remain"`
	// Active is true for the spool that feeds the nozzle now.
	Active bool `json:"active,omitempty"`
	// Idx is the filament code of Bambu Studio for a spool of a Bambu Lab printer (tray_info_idx),
	// for example "GFA00", or "" when it is not known. A slicer writes it in the 3MF.
	Idx string `json:"idx"`
}

// filamentsOf returns the loaded spools of the printer p: each tray of an AMS with a spool and
// the external spools last (a Bambu Lab printer), or the roll of a printer that tells it (a
// Klipper printer). Empty trays are out of the list.
func filamentsOf(p any) []filamentJSON {
	remain := func(f float64) *float64 {
		if f < 0 {
			return nil
		}
		return &f
	}
	if a, ok := p.(interface {
		AMS() printer.AMS
		Loaded() []printer.Loaded
	}); ok {
		ams := a.AMS()
		loaded := a.Loaded() // the same trays in the same order, with the part left of the rolls
		out := []filamentJSON{}
		for i, t := range ams.Trays {
			if !t.Loaded {
				continue
			}
			f := filamentJSON{Slot: fmt.Sprintf("%c%d", 'A'+t.Unit, t.Index+1), Tray: t.Slot, Material: t.Material,
				Color: t.Color, Name: t.Brand, Remain: remain(-1), Active: t.Active, Idx: t.InfoIdx}
			if t.External {
				f.Slot = "Ext"
			}
			switch t.Feeds {
			case 0:
				f.Nozzle = "main"
			case 1:
				f.Nozzle = "aux"
			}
			if i < len(loaded) {
				f.Remain = remain(loaded[i].Fraction)
				if loaded[i].Color != "" {
					f.Color = loaded[i].Color
				}
			}
			f.Index = len(out)
			out = append(out, f)
		}
		return out
	}
	if l, ok := p.(interface{ Loaded() []printer.Loaded }); ok {
		var out []filamentJSON
		for _, it := range l.Loaded() {
			if it.Empty || it.Material == "" && it.Color == "" {
				continue
			}
			out = append(out, filamentJSON{Index: len(out), Tray: -1, Material: it.Material, Color: it.Color,
				Remain: remain(it.Fraction), Active: it.Active})
		}
		return out
	}
	return nil
}

// limitsJSON is the [printer] section of printer.cfg: velocities in mm/s, accelerations in
// mm/s². A Z value of 0 is the value of the XY moves.
type limitsJSON struct {
	MaxVelocity          float64 `json:"max_velocity"`
	MaxAccel             float64 `json:"max_accel"`
	SquareCornerVelocity float64 `json:"square_corner_velocity"`
	MinimumCruiseRatio   float64 `json:"minimum_cruise_ratio"`
	MaxZVelocity         float64 `json:"max_z_velocity"`
	MaxZAccel            float64 `json:"max_z_accel"`
}

func (s *Server) servePrintdeck(w http.ResponseWriter, r *http.Request) {
	out := printdeckJSON{Camera: s.cameraInfo(), Cameras: s.camerasInfo(), Process: "fdm"}
	if s.Resin {
		out.Process = "resin"
	}
	if s.PrinterID != nil {
		out.PrinterID = s.PrinterID()
	}
	if s.Status != nil {
		out.LCD = s.Status()
	}
	if s.Limits != nil {
		if l, ok := s.Limits(); ok {
			out.Limits = &limitsJSON{MaxVelocity: l.MaxVelocity, MaxAccel: l.MaxAccel, SquareCornerVelocity: l.SquareCornerVelocity,
				MinimumCruiseRatio: l.MinCruiseRatio, MaxZVelocity: l.MaxZVelocity, MaxZAccel: l.MaxZAccel}
		}
	}
	if s.Material != nil {
		out.Material = s.Material()
	}
	if s.Signing != nil {
		out.SignedOnly = s.Signing.SignedOnly()
	}
	if s.Printer != nil {
		out.Controls, out.controlState = s.controls(), s.state()
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
	if s.History != nil {
		out.Projects = []string{}
		for _, p := range printer.Projects(s.History()) {
			out.Projects = append(out.Projects, p.Name)
		}
	}
	// The projects of the other printers, from the service.
	if s.Projects != nil {
		have := map[string]bool{}
		for _, n := range out.Projects {
			have[n] = true
		}
		for _, n := range s.Projects() {
			if !have[n] {
				out.Projects = append(out.Projects, n)
				have[n] = true
			}
		}
	}
	out.Filaments = filamentsOf(s.Printer)
	out.Nozzles, out.Plate = nozzlesOf(s.Printer)
	if st := s.Printer.State(); st.Busy() {
		j := &jobJSON{File: st.FilePath, Title: st.Name(), State: "printing", Layer: st.Layer, Layers: st.Layers,
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
	// Login tells if a request comes from a browser with the login of the deck, or is nil. It lets
	// an upload from a page of the deck through with no API key (upload in files.go).
	Login func(r *http.Request) bool
	// StartPrint starts a print of a file in place of Printer.PrintFile, or nil. The deck first
	// checks that the material of the file is in the printer, and asks on the touch screen.
	StartPrint func(path string)
	// Enqueue adds a file to the print queue of the deck, or is nil. A file that a slicer sends to
	// print while the printer prints goes to the queue.
	Enqueue func(path string) error
	// Resin is true on the deck of a resin printer. The API then refuses uploads, each start and
	// each resume: the UV light goes on only after a press on the touch screen of the deck. Pause
	// and cancel work.
	Resin bool
	// PrinterID returns the ID of the printer of the deck at the service, or "" before the deck
	// registers it. It can be nil.
	PrinterID func() string
	// Queue is the print queue of the deck, or nil. The API lists, moves and removes its files.
	Queue *queue.Queue
	// Status gives the status screen of the printer display (lcd.Status), or nil.
	Status func() any
	// Limits gives the motion limits of the printer ([printer] of printer.cfg), and false before
	// the deck has read them. It can be nil.
	Limits func() (toolhead.Limits, bool)
	// Model draws the model of the print for an app (printer.View.ModelPicture), or nil.
	Model func(st printer.State, w, h int, mono bool) image.Image
	// Signing keeps "Signed prints only" and the trusted keys of the slicers, or is nil. With the
	// setting on, the API queues and starts only G-code files with a good signature of a trusted
	// key, and refuses G-code commands (signed.go).
	Signing *signing.Store
	// Rejected shows the reason for a rejected file on the touch screen, or is nil.
	Rejected func(name, reason string)
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
