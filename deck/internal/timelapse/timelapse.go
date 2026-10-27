// Package timelapse makes a time lapse video of a print with the camera of the deck, and the
// picture of the first layer.
//
//	timelapse.go  the settings, and the frames of a print in memory
//	runner.go     follows the park of the Klipper host (printdeck_timelapse) and captures the pictures
//	mov.go        the video file: QuickTime with JPEG frames, with no encoder
//
// The Klipper host parks the toolhead at each layer change and waits. The deck captures the
// picture, and the print continues. The frames stay in memory during the print. At the end of the print, the
// deck writes one video file to the state folder.
package timelapse

import (
	"fmt"
	"image"
	"io"
	"strings"
)

// Settings are the time lapse settings of the deck.
type Settings struct {
	// On adds a frame at each layer change of a print.
	On bool
	// Park moves the toolhead out of the picture for each frame of the time lapse.
	Park bool
	// First is FirstOff, FirstPicture or FirstPause: the picture of the first layer, with a pause
	// for a check with FirstPause. The picture of the first layer always has a park.
	First int
}

// The values of Settings.First, as FIRST_LAYER of PRINTDECK_TIMELAPSE.
const (
	FirstOff     = 0
	FirstPicture = 1
	FirstPause   = 2
)

// DefaultSettings are the settings with no file: no time lapse, no first layer picture, and the
// park on for the frames.
var DefaultSettings = Settings{Park: true}

// ParseSettings reads the settings from the timelapse file of the deck: lines of key=value.
func ParseSettings(b []byte) Settings {
	s := DefaultSettings
	for _, l := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
		switch k {
		case "on":
			s.On = v == "1"
		case "park":
			s.Park = v == "1"
		case "first":
			switch v {
			case "1":
				s.First = FirstPicture
			case "2":
				s.First = FirstPause
			default:
				s.First = FirstOff
			}
		}
	}
	return s
}

// Bytes is the text of the settings for the timelapse file.
func (s Settings) Bytes() []byte {
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	return []byte(fmt.Sprintf("on=%d\npark=%d\nfirst=%d\n", b(s.On), b(s.Park), s.First))
}

// The limits of the frames of one print in memory. At a limit, the recorder keeps every second
// frame and adds a frame at every second layer from then on: the video has the whole print.
const (
	MaxFrames = 600
	MaxBytes  = 96 << 20
	// MaxWidth is the largest width of a frame. A larger picture is made smaller.
	MaxWidth = 1280
)

// Recorder keeps the frames of the time lapse of one print in memory.
type Recorder struct {
	frames [][]byte
	size   image.Point
	bytes  int
	every  int // the layers between two frames
	layer  int // the layer of the last frame
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{every: 1} }

// Every is the count of layers between two frames: 1, 2, 4 and so on.
func (r *Recorder) Every() int { return r.every }

// Len is the count of frames.
func (r *Recorder) Len() int { return len(r.frames) }

// Size is the size of the frames.
func (r *Recorder) Size() image.Point { return r.size }

// Add adds the JPEG b of the size sz, the picture of the layer. The first frame sets the size of
// the video: a frame of another size is not added. It reports that the frame is added.
func (r *Recorder) Add(b []byte, sz image.Point, layer int) bool {
	if len(b) == 0 || sz.X < 1 || sz.Y < 1 {
		return false
	}
	if len(r.frames) == 0 {
		r.size = sz
	} else if sz != r.size {
		return false
	}
	if len(r.frames) > 0 && layer > 0 && layer%r.every != 0 {
		return false
	}
	r.frames = append(r.frames, b)
	r.bytes += len(b)
	r.layer = layer
	for len(r.frames) > MaxFrames || r.bytes > MaxBytes {
		r.thin()
	}
	return true
}

// AddLast adds the last frame of a print, at any layer.
func (r *Recorder) AddLast(b []byte, sz image.Point) bool {
	if len(r.frames) == 0 || sz != r.size || len(b) == 0 {
		return false
	}
	r.frames = append(r.frames, b)
	r.bytes += len(b)
	return true
}

// thin keeps every second frame, and doubles the layers between two frames.
func (r *Recorder) thin() {
	keep := r.frames[:0]
	r.bytes = 0
	for i, f := range r.frames {
		if i%2 == 0 {
			keep = append(keep, f)
			r.bytes += len(f)
		}
	}
	clear(r.frames[len(keep):])
	r.frames = keep
	r.every *= 2
}

// Rate is the frames a second of the video: 30 for a long video, and fewer for a short one, so
// that the video is about 10 seconds or longer. It is at least 5.
func Rate(frames int) int { return max(5, min(30, frames/10)) }

// WriteVideo writes the video of the frames to w, and holds the last frame for one second.
func (r *Recorder) WriteVideo(w io.Writer) error {
	if len(r.frames) == 0 {
		return fmt.Errorf("timelapse: no frames")
	}
	fps := Rate(len(r.frames))
	frames := make([][]byte, 0, len(r.frames)+fps)
	frames = append(frames, r.frames...)
	for range fps {
		frames = append(frames, r.frames[len(r.frames)-1])
	}
	return WriteMOV(w, frames, r.size.X, r.size.Y, fps)
}
