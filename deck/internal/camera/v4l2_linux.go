package camera

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The V4L2 API of Linux (linux/videodev2.h): the structs and the ioctls that the capture uses.

type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

type v4l2FmtDesc struct {
	Index       uint32
	Type        uint32
	Flags       uint32
	Description [32]byte
	PixelFormat uint32
	MbusCode    uint32
	Reserved    [3]uint32
}

type v4l2PixFormat struct {
	Width, Height, PixelFormat, Field, BytesPerLine, SizeImage, Colorspace, Priv uint32
	Flags, YCbCrEnc, Quantization, XferFunc                                      uint32
}

// v4l2Format is struct v4l2_format: its union has pointers, so it starts at the alignment of a
// pointer.
type v4l2Format struct {
	Type uint32
	Fmt  struct {
		_   [0]uintptr
		Pix v4l2PixFormat
		_   [200 - unsafe.Sizeof(v4l2PixFormat{})]byte
	}
}

type v4l2FrmSizeEnum struct {
	Index       uint32
	PixelFormat uint32
	Type        uint32
	Size        [6]uint32 // discrete: width, height; stepwise: min w, max w, step w, min h, max h, step h
	Reserved    [2]uint32
}

type v4l2RequestBuffers struct {
	Count, Type, Memory, Capabilities uint32
	Flags                             uint8
	Reserved                          [3]uint8
}

type v4l2Buffer struct {
	Index, Type, BytesUsed, Flags, Field uint32
	Timestamp                            unix.Timeval
	Timecode                             [16]byte
	Sequence, Memory                     uint32
	M                                    uintptr // the offset of the buffer, for mmap
	Length, Reserved2, RequestFD         uint32
}

type v4l2StreamParm struct {
	Type uint32
	// struct v4l2_captureparm, in the union of 200 bytes.
	Capability, CaptureMode uint32
	Numerator, Denominator  uint32 // the time of a frame
	ExtendedMode, ReadBufs  uint32
	Reserved                [4]uint32
	_                       [200 - 40]byte
}

const (
	capVideoCapture = 0x00000001
	capStreaming    = 0x04000000
	capDeviceCaps   = 0x80000000
	capTimePerFrame = 0x1000

	bufTypeVideoCapture = 1
	memoryMMAP          = 1
	bufFlagError        = 0x40
	frmSizeDiscrete     = 1
)

// ioc is the number of an ioctl: _IOC(dir, 'V', nr, size).
func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'V'<<8 | nr }

const (
	iocRead  = 2
	iocWrite = 1
	iocRW    = 3
)

var (
	vidiocQueryCap      = ioc(iocRead, 0, unsafe.Sizeof(v4l2Capability{}))
	vidiocEnumFmt       = ioc(iocRW, 2, unsafe.Sizeof(v4l2FmtDesc{}))
	vidiocSFmt          = ioc(iocRW, 5, unsafe.Sizeof(v4l2Format{}))
	vidiocReqBufs       = ioc(iocRW, 8, unsafe.Sizeof(v4l2RequestBuffers{}))
	vidiocQueryBuf      = ioc(iocRW, 9, unsafe.Sizeof(v4l2Buffer{}))
	vidiocQBuf          = ioc(iocRW, 15, unsafe.Sizeof(v4l2Buffer{}))
	vidiocDQBuf         = ioc(iocRW, 17, unsafe.Sizeof(v4l2Buffer{}))
	vidiocStreamOn      = ioc(iocWrite, 18, unsafe.Sizeof(int32(0)))
	vidiocStreamOff     = ioc(iocWrite, 19, unsafe.Sizeof(int32(0)))
	vidiocGParm         = ioc(iocRW, 21, unsafe.Sizeof(v4l2StreamParm{}))
	vidiocSParm         = ioc(iocRW, 22, unsafe.Sizeof(v4l2StreamParm{}))
	vidiocEnumFrameSize = ioc(iocRW, 74, unsafe.Sizeof(v4l2FrmSizeEnum{}))
	vidiocGCtrl         = ioc(iocRW, 27, unsafe.Sizeof(v4l2Control{}))
	vidiocSCtrl         = ioc(iocRW, 28, unsafe.Sizeof(v4l2Control{}))
	vidiocQueryCtrl     = ioc(iocRW, 36, unsafe.Sizeof(v4l2QueryCtrl{}))
)

// v4l2QueryCtrl is struct v4l2_queryctrl: the range of a control.
type v4l2QueryCtrl struct {
	ID, Type                        uint32
	Name                            [32]byte
	Minimum, Maximum, Step, Default int32
	Flags                           uint32
	Reserved                        [2]uint32
}

// ctrlFlagDisabled is V4L2_CTRL_FLAG_DISABLED: the camera does not have the control.
const ctrlFlagDisabled = 0x0001

// v4l2Control is struct v4l2_control: a control of the camera.
type v4l2Control struct {
	ID    uint32
	Value int32
}

// The controls that flip the picture in the camera (V4L2_CID_HFLIP and V4L2_CID_VFLIP).
const (
	cidHFlip = 0x00980914
	cidVFlip = 0x00980915
)

// setFlip sets the flip controls of the camera to on, and reports that the camera has both. The
// controls stay set in the camera after the capture, so the deck clears them for a camera with no
// turn.
func setFlip(fd int, on bool) bool {
	v := int32(0)
	if on {
		v = 1
	}
	ok := true
	for _, id := range []uint32{cidHFlip, cidVFlip} {
		c := v4l2Control{ID: id, Value: v}
		if ioctl(fd, vidiocSCtrl, unsafe.Pointer(&c)) != nil {
			ok = false
		}
	}
	if on && !ok {
		// A camera with one of the two controls: no flip in the camera.
		setFlip(fd, false)
	}
	return ok
}

// The focus controls (V4L2_CID_FOCUS_ABSOLUTE and V4L2_CID_FOCUS_AUTO).
const (
	cidFocusAbsolute = 0x009a090a
	cidFocusAuto     = 0x009a090c
)

// focuser sets the focus of a camera with a focus control.
type focuser struct {
	fd      int
	f       Focus
	hasAuto bool // the camera has the autofocus control
	def     int  // the default lens position
	set     bool // the camera has the focus of f
}

// newFocuser returns the focuser of a camera, or nil for a camera with no lens position control.
func newFocuser(fd int) *focuser {
	q := v4l2QueryCtrl{ID: cidFocusAbsolute}
	if ioctl(fd, vidiocQueryCtrl, unsafe.Pointer(&q)) != nil || q.Flags&ctrlFlagDisabled != 0 || q.Maximum <= q.Minimum {
		return nil
	}
	z := &focuser{fd: fd, def: int(q.Default),
		f: Focus{Min: int(q.Minimum), Max: int(q.Maximum), Step: max(1, int(q.Step))}}
	a := v4l2QueryCtrl{ID: cidFocusAuto}
	z.hasAuto = ioctl(fd, vidiocQueryCtrl, unsafe.Pointer(&a)) == nil && a.Flags&ctrlFlagDisabled == 0
	return z
}

// apply sets the focus of the settings s in the camera, and reports that the focus changed. A
// camera with no autofocus control goes to its default lens position for auto.
func (z *focuser) apply(s CamSettings) bool {
	auto := !s.FixedFocus && z.hasAuto
	pos := z.def
	if s.FixedFocus {
		pos = z.f.Clamp(s.Focus)
	}
	if z.set && auto == z.f.Auto && (auto || pos == z.f.Pos) {
		return false
	}
	if z.hasAuto {
		v := int32(0)
		if auto {
			v = 1
		}
		c := v4l2Control{ID: cidFocusAuto, Value: v}
		ioctl(z.fd, vidiocSCtrl, unsafe.Pointer(&c))
	}
	if auto {
		z.read()
	} else {
		// The autofocus control is off first: a camera can refuse a lens position while it is on.
		c := v4l2Control{ID: cidFocusAbsolute, Value: int32(pos)}
		ioctl(z.fd, vidiocSCtrl, unsafe.Pointer(&c))
		z.f.Pos = pos
	}
	z.f.Auto, z.set = auto, true
	return true
}

// read reads the lens position of the autofocus, and reports that it changed.
func (z *focuser) read() bool {
	c := v4l2Control{ID: cidFocusAbsolute}
	if ioctl(z.fd, vidiocGCtrl, unsafe.Pointer(&c)) != nil || int(c.Value) == z.f.Pos {
		return false
	}
	z.f.Pos = int(c.Value)
	return true
}

// fdCtrl is the controls of a V4L2 device (ctrlDev).
type fdCtrl int

func (fd fdCtrl) query(id uint32) (PTZRange, bool) {
	q := v4l2QueryCtrl{ID: id}
	if ioctl(int(fd), vidiocQueryCtrl, unsafe.Pointer(&q)) != nil || q.Flags&ctrlFlagDisabled != 0 {
		return PTZRange{}, false
	}
	return PTZRange{Min: int(q.Minimum), Max: int(q.Maximum), Step: max(1, int(q.Step)), Default: int(q.Default)}, true
}

func (fd fdCtrl) get(id uint32) (int, error) {
	c := v4l2Control{ID: id}
	err := ioctl(int(fd), vidiocGCtrl, unsafe.Pointer(&c))
	return int(c.Value), err
}

func (fd fdCtrl) set(id uint32, v int) error {
	c := v4l2Control{ID: id, Value: int32(v)}
	return ioctl(int(fd), vidiocSCtrl, unsafe.Pointer(&c))
}

// focusEvery is the time between two checks of the focus and the PTZ position in the capture:
// the settings, and with autofocus the lens position.
const focusEvery = 250 * time.Millisecond

// errBandwidth is the error of a camera that gets too little USB bandwidth: the cameras on one USB
// controller share its bandwidth.
var errBandwidth = errors.New("too little USB bandwidth for this camera: move a camera to another USB port")

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
		if e == unix.EINTR {
			continue
		}
		if e != 0 {
			return e
		}
		return nil
	}
}

// fourcc is the code of a pixel format, for example "YUYV".
func fourcc(s string) uint32 {
	return uint32(s[0]) | uint32(s[1])<<8 | uint32(s[2])<<16 | uint32(s[3])<<24
}

func fourccName(v uint32) string {
	return string([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

// formats are the pixel formats that the deck reads, the best first: a camera that sends JPEG
// frames costs the least CPU.
var formats = []string{"MJPG", "JPEG", "YU12", "NV12", "YUYV"}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// cameraName is the name of a camera from its card text. UVC cameras often repeat the name:
// "Cam Link 4K: Cam Link 4K".
func cameraName(card string) string {
	if a, b, ok := strings.Cut(card, ": "); ok && strings.TrimSpace(a) == strings.TrimSpace(b) {
		return strings.TrimSpace(a)
	}
	return card
}

// listDevices returns the USB cameras: the video devices that capture pictures in a format that
// the deck reads. Other nodes of the same camera (metadata) and the video devices of the board
// (the codecs and the camera port of the Raspberry Pi) are not in the list.
func listDevices() []Device {
	paths, _ := filepath.Glob("/dev/video*")
	var out []Device
	for _, p := range paths {
		if d, ok := probe(p); ok {
			out = append(out, d)
		}
	}
	return assignIDs(out, deviceLinks("/dev/v4l/by-id/*"), deviceLinks("/dev/v4l/by-path/*"))
}

// deviceLinks returns the links of a glob by their device: the link that ends in -index0 when a
// device has more than one.
func deviceLinks(glob string) map[string]string {
	out := map[string]string{}
	links, _ := filepath.Glob(glob)
	for _, l := range links {
		if t, err := filepath.EvalSymlinks(l); err == nil {
			if _, ok := out[t]; !ok || strings.HasSuffix(l, "-index0") {
				out[t] = l
			}
		}
	}
	return out
}

// probe reads the capabilities of a device, and reports that it is a USB camera with a format of
// formats.
func probe(path string) (Device, bool) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return Device{}, false
	}
	defer unix.Close(fd)
	var c v4l2Capability
	if ioctl(fd, vidiocQueryCap, unsafe.Pointer(&c)) != nil {
		return Device{}, false
	}
	caps := c.Capabilities
	if caps&capDeviceCaps != 0 {
		caps = c.DeviceCaps
	}
	if caps&capVideoCapture == 0 || caps&capStreaming == 0 {
		return Device{}, false
	}
	if cString(c.Driver[:]) != "uvcvideo" && !strings.HasPrefix(cString(c.BusInfo[:]), "usb-") {
		return Device{}, false
	}
	if _, ok := pickFormat(fd); !ok {
		return Device{}, false
	}
	return Device{Path: path, Name: cameraName(cString(c.Card[:]))}, true
}

// pickFormat returns the best format of formats that the device has.
func pickFormat(fd int) (string, bool) {
	have := map[string]bool{}
	for i := uint32(0); i < 64; i++ {
		d := v4l2FmtDesc{Index: i, Type: bufTypeVideoCapture}
		if ioctl(fd, vidiocEnumFmt, unsafe.Pointer(&d)) != nil {
			break
		}
		have[fourccName(d.PixelFormat)] = true
	}
	for _, f := range formats {
		if have[f] {
			return f, true
		}
	}
	return "", false
}

// The sizes of the capture. The deck asks a camera for its largest size up to want: a camera
// that sends JPEG frames sends them at that size. The deck converts the pictures of a camera
// that sends raw pictures to JPEG at rawMax at most, rawFPS times a second.
var (
	want       = image.Pt(1280, 720)
	rawMax     = image.Pt(960, 540)
	jpegFPS    = 15
	rawFPS     = 10
	noFrameFor = 5 * time.Second // a camera with no frame for this time has failed
	warmUp     = 300 * time.Millisecond
)

// pickSize returns the size of the format to ask for: the largest size up to want, or the
// smallest size when each size is larger.
func pickSize(fd int, f string) (int, int) {
	var best, small image.Point
	area := func(p image.Point) int { return p.X * p.Y }
	consider := func(p image.Point) {
		if p.X <= want.X && p.Y <= want.Y && area(p) > area(best) {
			best = p
		}
		if small == (image.Point{}) || area(p) < area(small) {
			small = p
		}
	}
	for i := uint32(0); i < 128; i++ {
		e := v4l2FrmSizeEnum{Index: i, PixelFormat: fourcc(f)}
		if ioctl(fd, vidiocEnumFrameSize, unsafe.Pointer(&e)) != nil {
			break
		}
		if e.Type == frmSizeDiscrete {
			consider(image.Pt(int(e.Size[0]), int(e.Size[1])))
			continue
		}
		// Stepwise or continuous sizes: the size closest to want in the range.
		minW, maxW, stepW := int(e.Size[0]), int(e.Size[1]), max(1, int(e.Size[2]))
		minH, maxH, stepH := int(e.Size[3]), int(e.Size[4]), max(1, int(e.Size[5]))
		w := max(minW, min(maxW, minW+(want.X-minW)/stepW*stepW))
		h := max(minH, min(maxH, minH+(want.Y-minH)/stepH*stepH))
		consider(image.Pt(w, h))
		break
	}
	if best == (image.Point{}) {
		best = small
	}
	if best == (image.Point{}) {
		best = want
	}
	return best.X, best.Y
}

// capture runs the capture of a device: see captureFunc.
func capture(ctx context.Context, d Device, l link) error {
	fd, err := unix.Open(d.Path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer unix.Close(fd)
	name, ok := pickFormat(fd)
	if !ok {
		return errors.New("the camera has no format that the deck reads")
	}
	w, h := pickSize(fd, name)
	var vf v4l2Format
	vf.Type = bufTypeVideoCapture
	vf.Fmt.Pix = v4l2PixFormat{Width: uint32(w), Height: uint32(h), PixelFormat: fourcc(name)}
	if err := ioctl(fd, vidiocSFmt, unsafe.Pointer(&vf)); err != nil {
		if err == unix.EBUSY {
			return errors.New("another program uses the camera")
		}
		return fmt.Errorf("set the format: %w", err)
	}
	pix := vf.Fmt.Pix
	name, w, h = fourccName(pix.PixelFormat), int(pix.Width), int(pix.Height)
	mjpeg := name == "MJPG" || name == "JPEG"
	// A camera that flips its picture itself keeps the JPEG frames with no change for a turn of
	// 180°. Else the deck turns each picture. rot is the turn that the deck makes.
	s := l.settings()
	rot := s.Rotate
	if setFlip(fd, rot == 180) {
		rot = 0
	}
	fps := rawFPS
	if mjpeg && rot == 0 {
		fps = jpegFPS
	}
	inFPS := setRate(fd, fps)
	f := Format{In: name, InW: w, InH: h, InFPS: inFPS, OutFPS: fps}
	if mjpeg {
		f.In = "MJPEG"
		f.OutW, f.OutH = w, h
		if rot != 0 {
			f.OutW, f.OutH = outSize(w, h, rawMax.X, rawMax.Y)
		}
	} else {
		f.OutW, f.OutH = outSize(w, h, rawMax.X, rawMax.Y)
	}
	f.OutW, f.OutH = TurnedSize(f.OutW, f.OutH, rot)
	if inFPS > 0 && float64(f.OutFPS) > inFPS {
		f.OutFPS = max(1, int(inFPS+0.5))
	}

	bufs, err := mapBuffers(fd)
	if err != nil {
		return err
	}
	defer func() {
		for _, b := range bufs {
			unix.Munmap(b)
		}
		rb := v4l2RequestBuffers{Count: 0, Type: bufTypeVideoCapture, Memory: memoryMMAP}
		ioctl(fd, vidiocReqBufs, unsafe.Pointer(&rb))
	}()
	for i := range bufs {
		b := v4l2Buffer{Index: uint32(i), Type: bufTypeVideoCapture, Memory: memoryMMAP}
		if err := ioctl(fd, vidiocQBuf, unsafe.Pointer(&b)); err != nil {
			return fmt.Errorf("queue a buffer: %w", err)
		}
	}
	typ := int32(bufTypeVideoCapture)
	if err := ioctl(fd, vidiocStreamOn, unsafe.Pointer(&typ)); err != nil {
		if err == unix.ENOSPC {
			return errBandwidth
		}
		return fmt.Errorf("start the stream: %w", err)
	}
	defer ioctl(fd, vidiocStreamOff, unsafe.Pointer(&typ))
	l.format(f)
	z := newFocuser(fd)
	if z != nil {
		z.apply(s)
		l.focus(z.f)
	}
	pz := newPTZCtl(fdCtrl(fd))
	if pz != nil {
		pz.apply(s)
		l.reportPTZ(pz.p)
	}
	lastFocus := time.Now()

	start := time.Now()
	lastFrame, lastEmit := start, time.Time{}
	every := time.Second / time.Duration(f.OutFPS)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		if time.Since(lastFrame) > noFrameFor {
			return errNoSignal
		}
		if (z != nil || pz != nil) && time.Since(lastFocus) >= focusEvery {
			lastFocus = time.Now()
			cs := l.settings()
			if z != nil && (z.apply(cs) || (z.f.Auto && z.read())) {
				l.focus(z.f)
			}
			if pz != nil && pz.apply(cs) {
				l.reportPTZ(pz.p)
			}
		}
		n, err := unix.Poll(fds, 250)
		if err != nil && err != unix.EINTR {
			return fmt.Errorf("poll: %w", err)
		}
		if n <= 0 {
			continue
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP) != 0 {
			return errors.New("the camera is gone")
		}
		b := v4l2Buffer{Type: bufTypeVideoCapture, Memory: memoryMMAP}
		if err := ioctl(fd, vidiocDQBuf, unsafe.Pointer(&b)); err != nil {
			if err == unix.EAGAIN {
				continue
			}
			if err == unix.ENODEV {
				return errors.New("the camera is gone")
			}
			return fmt.Errorf("read a frame: %w", err)
		}
		now := time.Now()
		if b.Flags&bufFlagError == 0 && b.BytesUsed > 0 && int(b.Index) < len(bufs) {
			lastFrame = now
			// The first frames of a camera can be dark or broken. A frame comes when it is due.
			if now.Sub(start) >= warmUp && now.Sub(lastEmit) >= every-every/8 {
				data := bufs[b.Index][:min(int(b.BytesUsed), len(bufs[b.Index]))]
				if fr := makeFrame(name, data, w, h, int(pix.BytesPerLine), f, rot); fr != nil {
					lastEmit = now
					l.emit(fr)
				}
			}
		}
		if err := ioctl(fd, vidiocQBuf, unsafe.Pointer(&b)); err != nil {
			return fmt.Errorf("queue a buffer: %w", err)
		}
	}
	return nil
}

// makeFrame makes a frame of a picture of the camera, turned clockwise by rot degrees, or returns
// nil for a broken picture.
func makeFrame(name string, data []byte, w, h, bpl int, f Format, rot int) *Frame {
	if name == "MJPG" || name == "JPEG" {
		b, jw, jh, err := fixJPEG(data)
		if err != nil {
			return nil
		}
		if rot == 0 {
			return newFrame(b, jw, jh, nil)
		}
		p := decodeTurned(b, jw, jh, f.OutW, f.OutH, rot)
		if p == nil {
			return nil
		}
		return picFrame(p)
	}
	y, cb, cr, err := rawPlanes(name, data, w, h, bpl)
	if err != nil {
		return nil
	}
	fr := picFrame(toYCbCr(f.OutW, f.OutH, y, cb, cr, rot))
	fr.blank = blank(y)
	return fr
}

// setRate asks the camera for fps frames a second, and returns the rate that it uses, or 0 when
// the camera does not tell.
func setRate(fd, fps int) float64 {
	p := v4l2StreamParm{Type: bufTypeVideoCapture}
	if ioctl(fd, vidiocGParm, unsafe.Pointer(&p)) != nil {
		return 0
	}
	if p.Capability&capTimePerFrame != 0 {
		p.Numerator, p.Denominator = 1, uint32(fps)
		ioctl(fd, vidiocSParm, unsafe.Pointer(&p))
	}
	if p.Numerator == 0 || p.Denominator == 0 {
		return 0
	}
	return float64(p.Denominator) / float64(p.Numerator)
}

// mapBuffers asks for the buffers of the frames, and maps them.
func mapBuffers(fd int) ([][]byte, error) {
	rb := v4l2RequestBuffers{Count: 4, Type: bufTypeVideoCapture, Memory: memoryMMAP}
	if err := ioctl(fd, vidiocReqBufs, unsafe.Pointer(&rb)); err != nil {
		return nil, fmt.Errorf("ask for buffers: %w", err)
	}
	if rb.Count < 2 {
		return nil, errors.New("too few buffers")
	}
	var bufs [][]byte
	for i := uint32(0); i < rb.Count; i++ {
		b := v4l2Buffer{Index: i, Type: bufTypeVideoCapture, Memory: memoryMMAP}
		if err := ioctl(fd, vidiocQueryBuf, unsafe.Pointer(&b)); err != nil {
			unmapAll(bufs)
			return nil, fmt.Errorf("query a buffer: %w", err)
		}
		m, err := unix.Mmap(fd, int64(uint32(b.M)), int(b.Length), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			unmapAll(bufs)
			return nil, fmt.Errorf("map a buffer: %w", err)
		}
		bufs = append(bufs, m)
	}
	return bufs, nil
}

func unmapAll(bufs [][]byte) {
	for _, b := range bufs {
		unix.Munmap(b)
	}
}
