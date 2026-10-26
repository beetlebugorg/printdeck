package octoapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) serveVersion(w http.ResponseWriter, r *http.Request) {
	text := "OctoPrint " + serverVersion + " (Printdeck"
	if s.Version != "" {
		text += " " + s.Version
	}
	writeJSON(w, http.StatusOK, map[string]string{"api": apiVersion, "server": serverVersion, "text": text + ")"})
}

func (s *Server) serveServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": serverVersion, "safemode": nil})
}

// serveSettings answers the few settings that the slicers read. The webcam is the camera or the
// screen of the deck (webcam.go), with the key of the request in its address, because the slicers
// do not send the key with the stream. The deck turns the pictures itself, so flipH, flipV and
// rotate90 are false.
func (s *Server) serveSettings(w http.ResponseWriter, r *http.Request) {
	webcam := map[string]any{"webcamEnabled": false, "streamUrl": "", "snapshotUrl": ""}
	if s.webcamOn() {
		q := "?apikey=" + url.QueryEscape(requestKey(r))
		webcam = map[string]any{"webcamEnabled": true, "streamUrl": "/webcam/stream" + q,
			"snapshotUrl": "/webcam/snapshot" + q, "flipH": false, "flipV": false, "rotate90": false}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"api":     map[string]any{"allowCrossOrigin": false},
		"feature": map[string]any{"sdSupport": false},
		"webcam":  webcam,
		"plugins": map[string]any{},
	})
}

// serveUser answers the current user and the passive login of Cura. All keys belong to the
// printdeck user.
func (s *Server) serveUser(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "printdeck", "active": true, "admin": true, "user": true,
		"groups": []string{"admins", "users"}, "permissions": []string{},
	})
}

// serveProfiles answers one printer profile. The slicers have their own machine settings, so only
// the name and the heated bed matter.
func (s *Server) serveProfiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"profiles": map[string]any{"_default": map[string]any{
		"id": "_default", "name": "Printdeck", "model": "Printdeck", "default": true, "current": true,
		"heatedBed": true, "heatedChamber": false,
		"extruder": map[string]any{"count": 1, "offsets": [][]float64{{0, 0}}},
	}}})
}

// stateFlags are the state of the printer as OctoPrint names it.
func (s *Server) stateFlags() (string, map[string]bool) {
	st := s.Printer.State()
	text := st.Text
	if !st.Connected {
		text = "Offline"
	}
	flags := map[string]bool{
		"operational":   st.Connected,
		"printing":      st.Printing && !st.Paused,
		"paused":        st.Paused,
		"pausing":       false,
		"cancelling":    false,
		"resuming":      false,
		"finishing":     false,
		"ready":         st.Connected && !st.Busy(),
		"error":         st.Err != "" && st.Reachable,
		"closedOrError": !st.Connected,
		"sdReady":       false,
	}
	return text, flags
}

func (s *Server) servePrinter(w http.ResponseWriter, r *http.Request) {
	st := s.Printer.State()
	if !st.Connected {
		apiError(w, http.StatusConflict, "Printer is not operational")
		return
	}
	text, flags := s.stateFlags()
	temp := func(actual, target float64) map[string]float64 {
		return map[string]float64{"actual": actual, "target": target, "offset": 0}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":       map[string]any{"text": text, "flags": flags, "error": st.Err},
		"temperature": map[string]any{"tool0": temp(st.Tool, st.ToolTarget), "bed": temp(st.Bed, st.BedTarget)},
		"sd":          map[string]bool{"ready": false},
	})
}

// operational refuses a command when the printer is not connected, as OctoPrint does.
func (s *Server) operational(w http.ResponseWriter) bool {
	if !s.Printer.State().Connected {
		apiError(w, http.StatusConflict, "Printer is not operational")
		return false
	}
	return true
}

// commandResult answers a command: 204, or 409 with the refusal of the printer.
func commandResult(w http.ResponseWriter, err error) {
	if err != nil {
		apiError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		apiError(w, http.StatusBadRequest, "Malformed JSON body")
		return false
	}
	return true
}

func (s *Server) serveCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command  string   `json:"command"`
		Commands []string `json:"commands"`
	}
	if !decode(w, r, &req) || !s.operational(w) {
		return
	}
	lines := req.Commands
	if req.Command != "" {
		lines = append([]string{req.Command}, lines...)
	}
	commandResult(w, s.Printer.Gcode(lines...))
}

func (s *Server) servePrinthead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string   `json:"command"`
		Axes    []string `json:"axes"`
		X, Y, Z float64
		Speed   int `json:"speed"`
	}
	if !decode(w, r, &req) || !s.operational(w) {
		return
	}
	switch req.Command {
	case "home":
		commandResult(w, s.Printer.Home(req.Axes...))
	case "jog":
		speed := req.Speed
		if speed <= 0 {
			speed = 6000
		}
		commandResult(w, s.Printer.Jog(req.X, req.Y, req.Z, speed))
	default:
		apiError(w, http.StatusBadRequest, "Unknown command")
	}
}

func (s *Server) serveTool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string         `json:"command"`
		Targets map[string]int `json:"targets"`
	}
	if !decode(w, r, &req) || !s.operational(w) {
		return
	}
	t, ok := req.Targets["tool0"]
	if req.Command != "target" || !ok {
		apiError(w, http.StatusBadRequest, "Only the target of tool0 is supported")
		return
	}
	commandResult(w, s.Printer.SetToolTemp(t))
}

func (s *Server) serveBed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		Target  int    `json:"target"`
	}
	if !decode(w, r, &req) || !s.operational(w) {
		return
	}
	if req.Command != "target" {
		apiError(w, http.StatusBadRequest, "Only the target of the bed is supported")
		return
	}
	commandResult(w, s.Printer.SetBedTemp(req.Target))
}

func (s *Server) serveJob(w http.ResponseWriter, r *http.Request) {
	st := s.Printer.State()
	text, _ := s.stateFlags()
	var file map[string]any
	if st.Busy() {
		file = map[string]any{"name": st.File, "display": st.File, "path": st.FilePath, "origin": "local",
			"size": st.FileSize, "date": st.FileDate}
	} else {
		file = map[string]any{"name": nil, "path": nil, "origin": nil, "size": nil, "date": nil}
	}
	progress := map[string]any{"completion": nil, "filepos": nil, "printTime": nil, "printTimeLeft": nil,
		"printTimeLeftOrigin": nil}
	if st.Busy() {
		progress["completion"], progress["filepos"] = st.Completion, st.FilePos
		if st.HasTimeLeft {
			progress["printTimeLeft"], progress["printTimeLeftOrigin"] = int(st.TimeLeft.Seconds()), "estimate"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":      map[string]any{"file": file, "estimatedPrintTime": nil, "filament": nil},
		"progress": progress,
		"state":    text,
		"error":    st.Err,
	})
}

func (s *Server) serveJobCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
		Action  string `json:"action"`
	}
	if !decode(w, r, &req) || !s.operational(w) {
		return
	}
	st := s.Printer.State()
	switch req.Command {
	case "cancel":
		if !st.Busy() {
			apiError(w, http.StatusConflict, "Printer is not printing")
			return
		}
		s.Printer.Cancel()
	case "pause":
		if !st.Busy() {
			apiError(w, http.StatusConflict, "Printer is not printing")
			return
		}
		switch strings.ToLower(req.Action) {
		case "pause":
			s.Printer.Pause()
		case "resume":
			s.Printer.Resume()
		default: // "toggle" and no action
			if st.Paused {
				s.Printer.Resume()
			} else {
				s.Printer.Pause()
			}
		}
	case "start", "restart":
		apiError(w, http.StatusConflict, "Select a file with print=true to start it")
		return
	default:
		apiError(w, http.StatusBadRequest, "Unknown command")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveConnection(w http.ResponseWriter, r *http.Request) {
	text, _ := s.stateFlags()
	writeJSON(w, http.StatusOK, map[string]any{
		"current": map[string]any{"state": text, "port": "klipper", "baudrate": 250000, "printerProfile": "_default"},
		"options": map[string]any{"ports": []string{"klipper"}, "baudrates": []int{250000},
			"printerProfiles": []map[string]string{{"id": "_default", "name": "Printdeck"}},
			"portPreference":  "klipper", "baudratePreference": 250000, "printerProfilePreference": "_default", "autoconnect": true},
	})
}

func (s *Server) serveConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Command string `json:"command"`
	}
	if !decode(w, r, &req) {
		return
	}
	switch req.Command {
	case "connect":
		if !s.Printer.State().Connected {
			s.Printer.Connect()
		}
	case "disconnect", "fake_ack":
		// Klipper stays connected; there is nothing to do.
	default:
		apiError(w, http.StatusBadRequest, "Unknown command")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
