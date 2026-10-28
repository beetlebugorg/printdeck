package timelapse

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"printdeck/internal/camera"
)

// The frames of a print can go to the next deck program. After an update during a print, the deck
// program of the new slot replaces the running one, and the print continues. Before the switch,
// SaveHandoff writes the frames of the print and the first layer picture to a file in RAM (/run).
// The next deck program reads the file with LoadHandoff, and its video of the print has the
// frames from the start of the print.

// handoffMagic is the first line of a handoff file.
const handoffMagic = "printdeck-timelapse-handoff 1\n"

// HandoffWait is the longest time that a runner with the frames of a handoff waits for the state
// of the print. After it, the runner ends the print as for a print that ended.
var HandoffWait = 2 * time.Minute

// handoffHeader is the JSON line after handoffMagic. The JPEGs of the frames come after it: each
// one has its length first, in 4 bytes (big endian).
type handoffHeader struct {
	File      string          `json:"file"`
	Size      image.Point     `json:"size"`
	Every     int             `json:"every"`
	Layer     int             `json:"layer"`
	CropSet   bool            `json:"crop_set"`
	CropSrc   image.Point     `json:"crop_src"`
	Crop      image.Rectangle `json:"crop"`
	CropSize  image.Point     `json:"crop_size"`
	ParkY     float64         `json:"park_y"`
	HasParkY  bool            `json:"has_park_y"`
	Frames    int             `json:"frames"`
	First     bool            `json:"first"` // the first layer picture is the last JPEG
	FirstAt   time.Time       `json:"first_at"`
	FirstFile string          `json:"first_file"`
}

// SaveHandoff stops the runner and writes the frames of the print now to path, through a
// temporary file. After it, the runner captures no pictures. With no print, it removes path.
func (r *Runner) SaveHandoff(path string) error {
	r.stepMu.Lock() // wait for a picture that the runner captures now
	defer r.stepMu.Unlock()
	r.mu.Lock()
	r.frozen = true
	rec := r.rec
	h := handoffHeader{File: r.file, CropSet: r.crop.set, CropSrc: r.crop.src, Crop: r.crop.r, CropSize: r.crop.size,
		ParkY: r.parkY, HasParkY: !math.IsNaN(r.parkY), FirstAt: r.firstAt, FirstFile: r.firstFile}
	if !h.HasParkY {
		h.ParkY = 0
	}
	var frames [][]byte
	if rec != nil {
		frames = rec.Frames()
		h.Size, h.Every, h.Layer = rec.size, rec.every, rec.layer
	}
	var first []byte
	if r.first != nil {
		first = r.first.JPEG()
	}
	r.mu.Unlock()
	if rec == nil || len(frames) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	h.Frames = len(frames)
	if first != nil {
		h.First = true
		frames = append(frames, first)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".timelapse-*")
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(tmp, 1<<20)
	err = writeHandoff(w, h, frames)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func writeHandoff(w io.Writer, h handoffHeader, frames [][]byte) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, handoffMagic+string(b)+"\n"); err != nil {
		return err
	}
	for _, f := range frames {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(f)))
		if _, err := w.Write(n[:]); err != nil {
			return err
		}
		if _, err := w.Write(f); err != nil {
			return err
		}
	}
	return nil
}

// LoadHandoff reads the frames of a print from the file path of SaveHandoff, and removes the
// file. Call it before Run. The runner then continues the print of the file when the state of the
// print shows it, and ends it as a print that ended when the state shows no print or another
// file. It reports that it read frames.
func (r *Runner) LoadHandoff(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer os.Remove(path)
	defer f.Close()
	h, frames, err := readHandoff(bufio.NewReader(f))
	if err != nil {
		return false, fmt.Errorf("timelapse: the handoff file: %w", err)
	}
	rec := &Recorder{size: h.Size, every: max(1, h.Every), layer: h.Layer}
	for _, b := range frames[:h.Frames] {
		rec.frames = append(rec.frames, b)
		rec.bytes += len(b)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rec, r.file = rec, h.File
	r.crop = lapseCrop{set: h.CropSet, src: h.CropSrc, r: h.Crop, size: h.CropSize}
	r.parkY = math.NaN()
	if h.HasParkY {
		r.parkY = h.ParkY
	}
	if h.First {
		if fr, err := camera.FrameOf(frames[h.Frames]); err == nil {
			r.first, r.firstAt, r.firstFile = fr, h.FirstAt, h.FirstFile
		}
	}
	r.restored, r.restoredAt = true, time.Now()
	return true, nil
}

// maxHandoffFrame is the largest JPEG of a handoff file.
const maxHandoffFrame = 16 << 20

func readHandoff(r *bufio.Reader) (handoffHeader, [][]byte, error) {
	var h handoffHeader
	magic := make([]byte, len(handoffMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != handoffMagic {
		return h, nil, errors.New("not a handoff file")
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return h, nil, err
	}
	if err := json.Unmarshal(line, &h); err != nil {
		return h, nil, err
	}
	n := h.Frames
	if h.First {
		n++
	}
	if h.Frames < 1 || h.Frames > 2*MaxFrames || h.Size.X < 1 || h.Size.Y < 1 {
		return h, nil, fmt.Errorf("bad header: %d frames of %v", h.Frames, h.Size)
	}
	frames := make([][]byte, 0, n)
	for range n {
		var l [4]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return h, nil, err
		}
		size := binary.BigEndian.Uint32(l[:])
		if size == 0 || size > maxHandoffFrame {
			return h, nil, fmt.Errorf("bad frame size %d", size)
		}
		b := make([]byte, size)
		if _, err := io.ReadFull(r, b); err != nil {
			return h, nil, err
		}
		frames = append(frames, b)
	}
	return h, frames, nil
}
