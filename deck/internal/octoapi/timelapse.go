package octoapi

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
)

// The time lapse videos of the deck, as the timelapse API of OctoPrint:
//
//	GET /api/timelapse                     the videos, the newest first
//	GET /downloads/timelapse/{name}        the file of a video, with Range requests
//	GET /downloads/timelapse/{name}/still  the JPEG of the last frame of a video
//
// Each video of the list also has the print of its history record, its frames and its length,
// under "printdeck". The deck sends the files as they are on the card: an app that needs another
// format converts the video itself.

// Timelapse is what the API needs of the time lapse of the deck (timelapse.Runner).
type Timelapse interface {
	Settings() timelapse.Settings
	Info() timelapse.Info
	VideoPath(name string) (string, bool)
}

// timelapseFields are the fields of the Server for the time lapse.
type timelapseFields struct {
	// Timelapse is the time lapse of the deck, or nil.
	Timelapse Timelapse
	// History gives the finished prints, or is nil. A video goes with the record that has its
	// name.
	History func() []printer.Record
}

// videoJSON is a video of the list. name, size, bytes, date and url are the fields of OctoPrint.
type videoJSON struct {
	Name      string         `json:"name"`
	Size      string         `json:"size"`
	Bytes     int64          `json:"bytes"`
	Date      string         `json:"date"`
	URL       string         `json:"url"`
	Printdeck videoExtraJSON `json:"printdeck"`
}

// videoExtraJSON is what the deck adds to a video: the time (RFC 3339 in UTC, whole seconds), the
// frames, the length, the still, and the print of the video, or null for a video with no record.
type videoExtraJSON struct {
	Time   time.Time       `json:"time"`
	Frames int             `json:"frames"`
	FPS    int             `json:"fps"`
	Length float64         `json:"length"` // seconds
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Still  string          `json:"still"`
	Print  *videoPrintJSON `json:"print"`
}

// videoPrintJSON is the history record of the print of a video.
type videoPrintJSON struct {
	File     string    `json:"file"`
	Path     string    `json:"path"`
	Result   string    `json:"result"`
	End      time.Time `json:"end"`
	Duration float64   `json:"duration"` // seconds
}

func (s *Server) serveTimelapse(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"config": map[string]any{"type": "off"}, "enabled": false, "files": []videoJSON{},
		"unrendered": []any{}}
	if s.Timelapse == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var recs []printer.Record
	if s.History != nil {
		recs = s.History()
	}
	files := []videoJSON{}
	for _, v := range s.Timelapse.Info().Videos {
		path, ok := s.Timelapse.VideoPath(v.Name)
		if !ok {
			continue
		}
		u := "/downloads/timelapse/" + url.PathEscape(v.Name)
		j := videoJSON{Name: v.Name, Size: humanSize(v.Size), Bytes: v.Size, Date: v.Time.Format("2006-01-02 15:04"), URL: u,
			Printdeck: videoExtraJSON{Time: v.Time.UTC().Truncate(time.Second), Still: u + "/still"}}
		if m, err := timelapse.OpenMovie(path); err == nil {
			e := &j.Printdeck
			e.Frames, e.FPS, e.Length, e.Width, e.Height = m.Len(), m.FPS, m.Length().Seconds(), m.W, m.H
			m.Close()
		}
		for _, rec := range recs {
			if rec.Video == v.Name {
				j.Printdeck.Print = &videoPrintJSON{File: rec.File, Path: rec.Path, Result: rec.Result, End: rec.End.UTC().Truncate(time.Second),
					Duration: rec.Duration.Seconds()}
				break
			}
		}
		files = append(files, j)
	}
	out["files"] = files
	if s.Timelapse.Settings().On {
		out["config"], out["enabled"] = map[string]any{"type": "zchange"}, true
	}
	writeJSON(w, http.StatusOK, out)
}

// videoFile opens the video of the request, or answers 404.
func (s *Server) videoFile(w http.ResponseWriter, r *http.Request) (*os.File, os.FileInfo, bool) {
	if s.Timelapse == nil {
		apiError(w, http.StatusNotFound, "File not found")
		return nil, nil, false
	}
	path, ok := s.Timelapse.VideoPath(r.PathValue("name"))
	if !ok {
		apiError(w, http.StatusNotFound, "File not found")
		return nil, nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		apiError(w, http.StatusNotFound, "File not found")
		return nil, nil, false
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		apiError(w, http.StatusNotFound, "File not found")
		return nil, nil, false
	}
	return f, fi, true
}

// serveVideo sends the file of a video. A phone loads a video in parts (Range requests).
func (s *Server) serveVideo(w http.ResponseWriter, r *http.Request) {
	f, fi, ok := s.videoFile(w, r)
	if !ok {
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "video/quicktime")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// serveStill sends the JPEG of the last frame of a video, as it is in the file.
func (s *Server) serveStill(w http.ResponseWriter, r *http.Request) {
	f, fi, ok := s.videoFile(w, r)
	if !ok {
		return
	}
	defer f.Close()
	m, err := timelapse.ReadMovie(f, fi.Size())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "The video cannot be read")
		return
	}
	b, err := m.JPEG(m.Len() - 1)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "The video cannot be read")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(b)
}

// humanSize is a size in bytes as OctoPrint writes it, for example "1.2MB".
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
