// Package webui is the web page of the Printdeck for a phone: it follows the print and the music
// live, and does the basic actions (pause, resume, cancel, preheat, start a file, upload a file,
// play and pause). The login of the remote screen protects it.
package webui

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"path"
	"sort"
	"time"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
)

//go:embed app.html
var appPage []byte

// Printer is what the page needs from the printer (ui.Printer has it).
type Printer interface {
	State() printer.State
	Files() []printer.File
	LoadFiles()
	Preview() image.Image
	Pause()
	Resume()
	Cancel()
	Heat(tool, bed int)
	PrintFile(path string)
}

// Music is what the page needs from Spotify (ui.Music has it).
type Music interface {
	State() spotify.State
	Cover() image.Image
	Toggle()
	Next()
	Previous()
}

// Volume is the amp.
type Volume interface {
	Volume() int
	Max() int
	Set(int)
}

// Server serves the page and its API.
type Server struct {
	Printer Printer
	Music   Music // nil without the speaker
	Volume  Volume
	Store   *gcodestore.Store
	// PreheatTool and PreheatBed are the temperatures of the preheat button.
	PreheatTool, PreheatBed int
	// Auth lets a request through only with a valid login session.
	Auth func(http.HandlerFunc) http.HandlerFunc
}

// maxUpload is the largest G-code upload.
const maxUpload = 512 << 20

// Register adds the page and its API to mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.Auth(s.servePage))
	mux.HandleFunc("GET /api/deck/state", s.Auth(s.serveState))
	mux.HandleFunc("GET /api/deck/events", s.Auth(s.serveEvents))
	mux.HandleFunc("GET /api/deck/files", s.Auth(s.serveFiles))
	mux.HandleFunc("POST /api/deck/action", s.Auth(s.serveAction))
	mux.HandleFunc("POST /api/deck/upload", s.Auth(s.serveUpload))
	mux.HandleFunc("GET /api/deck/preview.png", s.Auth(s.servePreview))
	mux.HandleFunc("GET /api/deck/cover.jpg", s.Auth(s.serveCover))
}

func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(appPage)
}

// state is the JSON of the page.
type state struct {
	Printer printerState `json:"printer"`
	Music   *musicState  `json:"music,omitempty"`
	Volume  int          `json:"volume"`
	MaxVol  int          `json:"maxVolume"`
	Preheat [2]int       `json:"preheat"`
}

type printerState struct {
	Reachable   bool    `json:"reachable"`
	Connected   bool    `json:"connected"`
	NeedsSetup  bool    `json:"needsSetup"`
	Text        string  `json:"text"`
	Err         string  `json:"error,omitempty"`
	Printing    bool    `json:"printing"`
	Paused      bool    `json:"paused"`
	File        string  `json:"file,omitempty"`
	Completion  float64 `json:"completion"`
	TimeLeft    int     `json:"timeLeft"` // seconds, or -1 when unknown
	Activity    string  `json:"activity,omitempty"`
	Layer       int     `json:"layer"`
	Layers      int     `json:"layers"`
	Tool        float64 `json:"tool"`
	ToolTarget  float64 `json:"toolTarget"`
	Bed         float64 `json:"bed"`
	BedTarget   float64 `json:"bedTarget"`
	FirmwareOld bool    `json:"firmwareOld"`
	Preview     string  `json:"preview"` // changes when the picture of the model changes
}

type musicState struct {
	Authorized bool   `json:"authorized"`
	Active     bool   `json:"active"`
	Playing    bool   `json:"playing"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Position   int    `json:"position"` // seconds
	Duration   int    `json:"duration"` // seconds
	Cover      string `json:"cover"`    // changes when the cover changes
}

func (s *Server) state() state {
	st := s.Printer.State()
	ps := printerState{Reachable: st.Reachable, Connected: st.Connected, NeedsSetup: st.NeedsSetup,
		Text: st.Text, Err: st.Err, Printing: st.Printing, Paused: st.Paused, File: st.File,
		Completion: st.Completion, TimeLeft: -1, Activity: st.Activity, Layer: st.Layer, Layers: st.Layers,
		Tool: round1(st.Tool), ToolTarget: st.ToolTarget, Bed: round1(st.Bed), BedTarget: st.BedTarget,
		FirmwareOld: st.FirmwareOld}
	if st.HasTimeLeft {
		ps.TimeLeft = int(st.TimeLeft.Seconds())
	}
	if st.Busy() {
		ps.Preview = fmt.Sprintf("%s:%d", st.FilePath, st.Layer)
	}
	out := state{Printer: ps, Volume: -1, Preheat: [2]int{s.PreheatTool, s.PreheatBed}}
	if s.Volume != nil {
		out.Volume, out.MaxVol = s.Volume.Volume(), s.Volume.Max()
	}
	if s.Music != nil {
		m := s.Music.State()
		out.Music = &musicState{Authorized: m.Authorized, Active: m.Active, Playing: m.Playing,
			Title: m.Title, Artist: m.Artist, Album: m.Album,
			Position: int(m.Progress(time.Now()).Seconds()), Duration: int(m.Duration.Seconds()), Cover: m.URI}
	}
	return out
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func (s *Server) serveState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.state())
}

// serveEvents sends the state as a server-sent event when it changes. It looks once a second, only
// while a page is open. The position of the song changes each second while it plays.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var last []byte
	idle := 0
	for {
		b, _ := json.Marshal(s.state())
		if !bytes.Equal(b, last) {
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			last, idle = b, 0
		} else if idle++; idle >= 30 {
			fmt.Fprint(w, ": ping\n\n") // keeps the connection open through proxies
			flusher.Flush()
			idle = 0
		}
		select {
		case <-tick.C:
		case <-r.Context().Done():
			return
		}
	}
}

type file struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Date int64  `json:"date"`
}

func (s *Server) serveFiles(w http.ResponseWriter, r *http.Request) {
	s.Printer.LoadFiles()
	files := s.Printer.Files()
	sort.Slice(files, func(i, j int) bool { return files[i].Date.After(files[j].Date) })
	out := make([]file, 0, len(files))
	for _, f := range files {
		out = append(out, file{f.Path, f.Name, f.Date.Unix()})
	}
	writeJSON(w, out)
}

type action struct {
	Action string `json:"action"`
	Path   string `json:"path,omitempty"`
	Value  int    `json:"value,omitempty"`
}

// serveAction does one action of the page.
func (s *Server) serveAction(w http.ResponseWriter, r *http.Request) {
	var a action
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&a); err != nil {
		httpError(w, http.StatusBadRequest, "bad request")
		return
	}
	busy := s.Printer.State().Busy()
	switch a.Action {
	case "pause":
		s.Printer.Pause()
	case "resume":
		s.Printer.Resume()
	case "cancel":
		s.Printer.Cancel()
	case "preheat":
		s.Printer.Heat(s.PreheatTool, s.PreheatBed)
	case "cooldown":
		s.Printer.Heat(0, 0)
	case "print":
		if busy {
			httpError(w, http.StatusConflict, "A print runs already.")
			return
		}
		if a.Path == "" || path.Clean("/"+a.Path) != "/"+a.Path {
			httpError(w, http.StatusBadRequest, "bad file")
			return
		}
		s.Printer.PrintFile(a.Path)
	case "music-toggle", "music-next", "music-previous":
		if s.Music == nil {
			httpError(w, http.StatusNotFound, "no music")
			return
		}
		map[string]func(){"music-toggle": s.Music.Toggle, "music-next": s.Music.Next,
			"music-previous": s.Music.Previous}[a.Action]()
	case "volume":
		if s.Volume == nil {
			httpError(w, http.StatusNotFound, "no volume")
			return
		}
		s.Volume.Set(a.Value)
	default:
		httpError(w, http.StatusBadRequest, "unknown action")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveUpload saves an uploaded G-code file in the folder of the printer, and starts it when the
// form asks for that.
func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	f, h, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "no file")
		return
	}
	defer f.Close()
	saved, err := s.Store.Save(h.Filename, f)
	if errors.Is(err, gcodestore.ErrBadName) {
		httpError(w, http.StatusBadRequest, "Only G-code files (.gcode) can be printed.")
		return
	} else if err != nil {
		log.Printf("web page upload: %v", err)
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Printer.LoadFiles()
	if r.FormValue("print") == "1" && !s.Printer.State().Busy() {
		s.Printer.PrintFile(saved)
	}
	writeJSON(w, map[string]string{"path": saved})
}

func (s *Server) servePreview(w http.ResponseWriter, r *http.Request) {
	img := s.Printer.Preview()
	if img == nil {
		http.NotFound(w, r)
		return
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b.Bytes())
}

func (s *Server) serveCover(w http.ResponseWriter, r *http.Request) {
	if s.Music == nil {
		http.NotFound(w, r)
		return
	}
	img := s.Music.Cover()
	if img == nil {
		http.NotFound(w, r)
		return
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b.Bytes())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
