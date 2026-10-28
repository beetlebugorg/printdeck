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
// keep the CPU use low. A turn of 90° or 270°, and a turn of 180° on a camera with no flip
// controls, also needs the decode, at the same smaller size and rate. The frames stay in memory
// only.
package camera

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"time"

	"printdeck/internal/jpegdec"
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
	OutW, OutH int     // the size of the JPEG frames, after the turn
	OutFPS     int     // the most frames a second that the deck makes
}

// Focus is the focus control of a camera.
type Focus struct {
	// Min and Max are the range of the lens position, and Step the smallest change.
	Min, Max, Step int
	// Auto reports that the camera focuses itself.
	Auto bool
	// Pos is the lens position now: the setting, or with Auto the position that the camera
	// reports.
	Pos int
}

// Clamp returns the lens position nearest to pos in the range, on a step.
func (f Focus) Clamp(pos int) int {
	pos = max(f.Min, min(f.Max, pos))
	if f.Step > 1 {
		pos = f.Min + (pos-f.Min+f.Step/2)/f.Step*f.Step
		if pos > f.Max {
			pos -= f.Step
		}
	}
	return pos
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
	pic    *image.YCbCr    // the picture, or nil until it is decoded
	scaled [3]*image.YCbCr // the picture decoded at 1/2, 1/4 and 1/8 of its size, or nil (pictureFor)
	smalls map[int]*Frame  // the smaller frames by width (Small)
	order  []int           // the widths of smalls, the oldest first
	err    error           // the decode error of the JPEG

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
	// A gray camera has a picture with no color.
	if f.pic = toYCbCrPicture(img); f.pic == nil {
		f.err = fmt.Errorf("camera: a JPEG of %T", img)
	}
	return f.pic
}

// pictureFor returns a picture of the frame for a smaller frame of width w: the whole picture when
// it is decoded, or else the JPEG decoded at 1/2, 1/4 or 1/8 of its size, as small as it can be
// with w pixels or more. The decode at a smaller size is in the DCT domain (jpegdec): it costs
// less than the decode of the whole picture, and leaves less to scale. The frame keeps each
// picture.
func (f *Frame) pictureFor(w int) *image.YCbCr {
	shift := 0
	for s := 3; s >= 1; s-- {
		if (f.w+1<<s-1)>>s >= w {
			shift = s
			break
		}
	}
	f.mu.Lock()
	if f.pic != nil || f.jpeg == nil || shift == 0 {
		f.mu.Unlock()
		return f.picture()
	}
	if p := f.scaled[shift-1]; p != nil {
		f.mu.Unlock()
		return p
	}
	b := f.jpeg
	f.mu.Unlock()
	img, err := jpegdec.DecodeScaled(bytes.NewReader(b), shift)
	if err != nil {
		return f.picture()
	}
	p := toYCbCrPicture(img)
	if p == nil {
		return f.picture()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if old := f.scaled[shift-1]; old != nil {
		return old // another viewer made it first
	}
	f.scaled[shift-1] = p
	return p
}

// toYCbCrPicture returns a decoded JPEG as a YCbCr picture: a gray picture gets no color. It
// returns nil for another kind of picture.
func toYCbCrPicture(img image.Image) *image.YCbCr {
	switch p := img.(type) {
	case *image.YCbCr:
		return p
	case *image.Gray:
		y := image.NewYCbCr(p.Rect, image.YCbCrSubsampleRatio420)
		for r := 0; r < p.Rect.Dy(); r++ {
			copy(y.Y[r*y.YStride:], p.Pix[r*p.Stride:r*p.Stride+p.Rect.Dx()])
		}
		for i := range y.Cb {
			y.Cb[i], y.Cr[i] = 128, 128
		}
		return y
	}
	return nil
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
	pic := f.pictureFor(w)
	if pic == nil {
		return f
	}
	h := max(2, (f.h*w/f.w+1)/2*2)
	if pic.Rect.Dx() != w || pic.Rect.Dy() != h || pic.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		pic = scaleYCbCr(pic, w, h, 0)
	}
	s := picFrame(pic)
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

// ForWidth returns the frame for a viewer of width w: the frame itself when w is 3/4 of its width
// or more, and else Small(w). The frame itself keeps the JPEG of the camera, so a large viewer
// costs no decode and no encode. The viewer scales the picture.
func (f *Frame) ForWidth(w int) *Frame {
	if w*4 >= f.w*3 {
		return f
	}
	return f.Small(w)
}

// FrameOf makes a frame of a JPEG, for example for a test.
func FrameOf(b []byte) (*Frame, error) {
	b, w, h, err := fixJPEG(b)
	if err != nil {
		return nil, err
	}
	return newFrame(b, w, h, nil), nil
}
