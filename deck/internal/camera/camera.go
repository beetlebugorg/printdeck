// Package camera reads the USB cameras of the deck (V4L2 devices, such as webcams or HDMI capture
// sticks) and gives their pictures as JPEG frames to the viewers: the webcam of the API, the
// camera page of the deck UI, and the time lapse. Each camera has a capture of its own.
//
//	camera.go     the devices and the frames
//	settings.go   the settings of each camera, the names, and the roles
//	hub.go        the capture of each camera runs only while a viewer watches it, and one
//	              capture feeds all viewers of the camera
//	scale.go      the conversion of raw pictures to JPEG, at a smaller size
//	calib.go      the calibration: the projection from the places of the printer to the picture
//	views.go      the views that follow the printer: the toolhead, the first layer, the part
//	render.go     the crops of the views, and the dark blur around a part
//	jpeg.go       the Huffman tables that some MJPEG cameras leave out of their frames
//	v4l2_linux.go the devices and the capture with the V4L2 ioctls
//
// A camera that sends MJPEG gives its JPEG frames with no change. The deck converts the pictures
// of a camera that sends raw pictures (YUYV, NV12, YU12) to JPEG at a smaller size and rate, to
// keep the CPU use low. The frames stay in memory only.
package camera

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"time"
)

// Device is a camera that the deck found.
type Device struct {
	// Path is the device file, for example /dev/video0.
	Path string
	// ID is the name of the setting: the link in /dev/v4l/by-id, which stays the same when the
	// camera goes into another USB port, or Path when the device has no link. Two cameras of the
	// same model often have one by-id link for both. Then the ID of each is its link in
	// /dev/v4l/by-path, which stays the same while the camera stays in its USB port.
	ID string
	// Alias is the by-id link of a camera with a by-path ID, or "". A setting of an older deck
	// can have it.
	Alias string
	// Name is the name of the camera, for example "C270 HD WEBCAM".
	Name string
}

// Is reports that id is the ID, the path or the alias of the device.
func (d Device) Is(id string) bool {
	return id != "" && (id == d.ID || id == d.Path || id == d.Alias)
}

// Format tells what the camera sends, and what the deck makes of it.
type Format struct {
	// In is the format of the camera: "MJPEG", "YUYV", "NV12" or "YU12".
	In         string
	InW, InH   int
	InFPS      float64 // the rate of the camera, or 0 when it is not known
	OutW, OutH int     // the size of the JPEG frames
	OutFPS     int     // the most frames a second that the deck makes
}

// String is the format for the screen, for example "MJPEG 1280×720, 15 fps" or
// "YUYV 1920×1080 to JPEG 960×540, 10 fps".
func (f Format) String() string {
	s := fmt.Sprintf("%s %d×%d", f.In, f.InW, f.InH)
	if f.In != "MJPEG" || f.OutW != f.InW {
		s += fmt.Sprintf(" to JPEG %d×%d", f.OutW, f.OutH)
	}
	return s + fmt.Sprintf(", %d fps", f.OutFPS)
}

// Frame is a picture of the camera as a JPEG. It is an image.Image too, for the deck UI: it
// decodes the JPEG when it is drawn. A Frame does not change.
type Frame struct {
	// Seq is the number of the frame: each new frame has a larger number.
	Seq uint64
	// Time is the time of the capture.
	Time time.Time

	cam string // the ID of the camera of the frame, or ""

	jpeg  []byte
	w, h  int
	blank bool // the picture has one color: an HDMI capture with no signal

	mu     sync.Mutex
	pic    *image.YCbCr   // the picture, or nil until it is decoded
	smalls map[int]*Frame // the smaller frames by width (Small)
	order  []int          // the widths of smalls, the oldest first
	err    error          // the decode error of the JPEG

	plans     map[string]*viewPlan // the plan of each view, or nil for no plan (views.go)
	views     map[string]*Frame    // the pictures of the views by view and width
	viewOrder []string             // the keys of views, the oldest first
}

// newFrame makes a frame of a JPEG of w x h pixels. pic is the picture of the JPEG, or nil.
func newFrame(b []byte, w, h int, pic *image.YCbCr) *Frame {
	return &Frame{jpeg: b, w: w, h: h, pic: pic, Time: time.Now()}
}

// Blank reports that the picture has one color, as an HDMI capture sends with no signal.
func (f *Frame) Blank() bool { return f.blank }

// picFrame makes a frame of a picture. The frame encodes its JPEG when a viewer asks for it, so
// a frame that no viewer takes costs no encode.
func picFrame(pic *image.YCbCr) *Frame {
	return &Frame{w: pic.Rect.Dx(), h: pic.Rect.Dy(), pic: pic, Time: time.Now()}
}

// JPEG returns the JPEG of the frame. It encodes the picture the first time.
func (f *Frame) JPEG() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.jpeg == nil && f.pic != nil {
		b, err := encodeJPEG(f.pic)
		if err != nil {
			return nil
		}
		f.jpeg = b
	}
	return f.jpeg
}

// Size is the size of the picture in pixels.
func (f *Frame) Size() image.Point { return image.Pt(f.w, f.h) }

// Picture returns the picture of the frame. It decodes the JPEG the first time.
func (f *Frame) Picture() image.Image {
	p := f.picture()
	if p == nil {
		return image.NewGray(image.Rect(0, 0, f.w, f.h))
	}
	return p
}

func (f *Frame) picture() *image.YCbCr {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pic != nil || f.err != nil {
		return f.pic
	}
	img, err := jpeg.Decode(bytes.NewReader(f.jpeg))
	if err != nil {
		f.err = err
		return nil
	}
	switch p := img.(type) {
	case *image.YCbCr:
		f.pic = p
	case *image.Gray:
		// A gray camera: the picture with no color.
		y := image.NewYCbCr(p.Rect, image.YCbCrSubsampleRatio420)
		for r := 0; r < p.Rect.Dy(); r++ {
			copy(y.Y[r*y.YStride:], p.Pix[r*p.Stride:r*p.Stride+p.Rect.Dx()])
		}
		for i := range y.Cb {
			y.Cb[i], y.Cr[i] = 128, 128
		}
		f.pic = y
	default:
		f.err = fmt.Errorf("camera: a JPEG of %T", img)
	}
	return f.pic
}

// The image.Image of the frame. At is slow: draw the Picture.
func (f *Frame) ColorModel() color.Model { return color.YCbCrModel }
func (f *Frame) Bounds() image.Rectangle { return image.Rect(0, 0, f.w, f.h) }
func (f *Frame) At(x, y int) color.Color {
	if p := f.picture(); p != nil {
		return p.At(x, y)
	}
	return color.Gray{}
}

// smallKeep is the most smaller sizes that a frame keeps.
const smallKeep = 4

// Small returns the frame at a smaller width w, with the same shape. The width goes down to a
// multiple of 16, and is at least 64. A width that is not smaller than the frame returns the
// frame. The frame keeps the smaller frames, so that the viewers of one size share one JPEG.
func (f *Frame) Small(w int) *Frame {
	w = max(64, w/16*16)
	if w >= f.w {
		return f
	}
	f.mu.Lock()
	if s, ok := f.smalls[w]; ok {
		f.mu.Unlock()
		return s
	}
	f.mu.Unlock()
	pic := f.picture()
	if pic == nil {
		return f
	}
	h := max(2, (f.h*w/f.w+1)/2*2)
	s := picFrame(scaleYCbCr(pic, w, h, false))
	s.Seq, s.Time, s.blank, s.cam = f.Seq, f.Time, f.blank, f.cam
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.smalls[w]; ok {
		return old // another viewer made it first
	}
	if f.smalls == nil {
		f.smalls = map[int]*Frame{}
	}
	f.smalls[w] = s
	f.order = append(f.order, w)
	if len(f.order) > smallKeep {
		delete(f.smalls, f.order[0])
		f.order = f.order[1:]
	}
	return s
}

// FrameOf makes a frame of a JPEG, for example for a test.
func FrameOf(b []byte) (*Frame, error) {
	b, w, h, err := fixJPEG(b)
	if err != nil {
		return nil, err
	}
	return newFrame(b, w, h, nil), nil
}
