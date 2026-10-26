package octoapi

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/image/draw"

	"printdeck/internal/camera"
)

// The webcam of the API, at the paths of OctoPrint:
//
//	GET /webcam/snapshot   one JPEG
//	GET /webcam/stream     an MJPEG stream (multipart/x-mixed-replace)
//
// The pictures come from the camera of the deck when it has one (Camera), and else from the
// screen of the deck, one frame a second. Two parameters are for small clients, such as a watch:
//
//	w=320    the width of the pictures, from 64 to the width of the camera: smaller pictures with
//	         the same shape. The deck makes one JPEG of each width for each frame, so the viewers
//	         of one width share it.
//	fps=2    the most frames a second of the stream, from 0.1 up. The default is the rate of the
//	         camera (up to 15 frames a second), or 1 for the screen.
//
// The camera runs only while a viewer watches, and stops a few seconds after the last viewer. A
// snapshot starts it and waits for its first frame, so the first snapshot takes up to a second
// more. Snapshots a few seconds apart keep it on, and then each snapshot is the newest frame.

// Camera is what the API needs of the camera of the deck (camera.Hub).
type Camera interface {
	// On reports that the deck has a camera on.
	On() bool
	Status() camera.Status
	Snapshot(ctx context.Context) (*camera.Frame, error)
	Next(ctx context.Context, after uint64) (*camera.Frame, error)
	Watch() (stop func())
}

// webcam lets a request through with an API key, or when Allow lets it through.
func (s *Server) webcam(h http.HandlerFunc) http.HandlerFunc {
	keyed := s.key(h)
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Allow != nil && s.Allow(r) {
			h(w, r)
			return
		}
		keyed(w, r)
	}
}

// cameraOn reports that the webcam is the camera.
func (s *Server) cameraOn() bool { return s.Camera != nil && s.Camera.On() }

// webcamOn reports that the API has a webcam: the camera or the screen.
func (s *Server) webcamOn() bool { return s.cameraOn() || s.Frame != nil }

// webcamWidth is the parameter w, or 0.
func webcamWidth(r *http.Request) int {
	w, err := strconv.Atoi(r.URL.Query().Get("w"))
	if err != nil || w <= 0 {
		return 0
	}
	return max(64, min(4096, w))
}

// webcamRate is the time between two frames of a stream with the parameter fps, or def.
func webcamRate(r *http.Request, def time.Duration) time.Duration {
	f, err := strconv.ParseFloat(r.URL.Query().Get("fps"), 64)
	if err != nil || f <= 0 {
		return def
	}
	return time.Duration(float64(time.Second) / max(0.1, min(30, f)))
}

func (s *Server) serveSnapshot(w http.ResponseWriter, r *http.Request) {
	var b []byte
	switch {
	case s.cameraOn():
		f, err := s.Camera.Snapshot(r.Context())
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "No picture from the camera")
			return
		}
		if width := webcamWidth(r); width > 0 {
			f = f.Small(width)
		}
		b = f.JPEG()
	case s.Frame != nil:
		var err error
		if b, err = s.screenJPEG(webcamWidth(r)); err != nil {
			apiError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

const boundary = "printdeckframe"

// writePart writes one JPEG of the stream, and reports that the client takes it.
func writePart(w http.ResponseWriter, rc *http.ResponseController, b []byte) bool {
	fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(b))
	w.Write(b)
	w.Write([]byte("\r\n"))
	return rc.Flush() == nil
}

// serveStream sends the frames of the camera, or a frame of the screen each second, until the
// client goes.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request) {
	if !s.webcamOn() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-store")
	rc := http.NewResponseController(w)
	width := webcamWidth(r)
	if s.cameraOn() {
		s.cameraStream(w, r, rc, width)
		return
	}
	every := webcamRate(r, time.Second)
	for {
		b, err := s.screenJPEG(width)
		if err != nil || !writePart(w, rc, b) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(every):
		}
	}
}

// cameraStream sends the frames of the camera. A client that takes the frames slower than the
// camera makes them gets the newest frame each time.
func (s *Server) cameraStream(w http.ResponseWriter, r *http.Request, rc *http.ResponseController, width int) {
	stop := s.Camera.Watch()
	defer stop()
	every := webcamRate(r, 0)
	var last uint64
	for {
		// A camera with no frame for 30 seconds ends the stream: the client connects again.
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		f, err := s.Camera.Next(ctx, last)
		cancel()
		if err != nil {
			return
		}
		last = f.Seq
		if width > 0 {
			f = f.Small(width)
		}
		if !writePart(w, rc, f.JPEG()) {
			return
		}
		if every > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(every):
			}
		}
	}
}

// screenJPEG is a frame of the screen as a JPEG, width pixels wide when width is not 0.
func (s *Server) screenJPEG(width int) ([]byte, error) {
	img := s.Frame()
	if b := img.Bounds(); width > 0 && width < b.Dx() {
		small := image.NewRGBA(image.Rect(0, 0, width, max(1, b.Dy()*width/b.Dx())))
		draw.ApproxBiLinear.Scale(small, small.Rect, img, b, draw.Src, nil)
		img = small
	}
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80})
	return b.Bytes(), err
}

// cameraJSON is the camera in the Printdeck API, for the apps: its name, and the size and the
// rate of its frames. state is "idle" (no viewer now), "starting", "on" or "error".
type cameraJSON struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	FPS    int    `json:"fps,omitempty"`
	Error  string `json:"error,omitempty"`
}

// cameraInfo is the camera for the Printdeck API, or nil when the deck has no camera on.
func (s *Server) cameraInfo() *cameraJSON {
	if !s.cameraOn() {
		return nil
	}
	st := s.Camera.Status()
	return &cameraJSON{Name: st.Device.Name, State: st.State, Width: st.Format.OutW, Height: st.Format.OutH,
		FPS: st.Format.OutFPS, Error: st.Err}
}
