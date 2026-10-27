package camera

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

// The sizes of the V4L2 structs on a 64-bit system (linux/videodev2.h).
func TestStructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the sizes are for a 64-bit system")
	}
	for name, got := range map[string]uintptr{
		"v4l2_capability": unsafe.Sizeof(v4l2Capability{}), "v4l2_fmtdesc": unsafe.Sizeof(v4l2FmtDesc{}),
		"v4l2_format": unsafe.Sizeof(v4l2Format{}), "v4l2_frmsizeenum": unsafe.Sizeof(v4l2FrmSizeEnum{}),
		"v4l2_requestbuffers": unsafe.Sizeof(v4l2RequestBuffers{}), "v4l2_buffer": unsafe.Sizeof(v4l2Buffer{}),
		"v4l2_streamparm": unsafe.Sizeof(v4l2StreamParm{}),
	} {
		want := map[string]uintptr{"v4l2_capability": 104, "v4l2_fmtdesc": 64, "v4l2_format": 208, "v4l2_frmsizeenum": 44,
			"v4l2_requestbuffers": 20, "v4l2_buffer": 88, "v4l2_streamparm": 204}[name]
		if got != want {
			t.Errorf("%s: %d bytes, want %d", name, got, want)
		}
	}
	if vidiocQueryCap != 0x80685600 || vidiocSFmt != 0xc0d05605 || vidiocDQBuf != 0xc0585611 || vidiocStreamOn != 0x40045612 ||
		vidiocSCtrl != 0xc008561c {
		t.Errorf("ioctl numbers: %x %x %x %x %x", vidiocQueryCap, vidiocSFmt, vidiocDQBuf, vidiocStreamOn, vidiocSCtrl)
	}
}

// TestCapture reads frames from a real camera: PRINTDECK_CAMERA=/dev/video0. It writes the
// frames to PRINTDECK_CAMERA_OUT when that is set.
func TestCapture(t *testing.T) {
	path := os.Getenv("PRINTDECK_CAMERA")
	if path == "" {
		t.Skip("PRINTDECK_CAMERA is not set")
	}
	var dev Device
	for _, d := range listDevices() {
		t.Logf("found %s: %s (%s)", d.Path, d.Name, d.ID)
		if d.Path == path {
			dev = d
		}
	}
	if dev.Path == "" {
		t.Fatalf("%s is not in the list of cameras", path)
	}
	for _, rot := range []int{0, 180} {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		var frames []*Frame
		var f Format
		err := capture(ctx, dev, CamSettings{Rotate: rot}, func(x Format) { f = x }, func(fr *Frame) { frames = append(frames, fr) })
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("turn %d: %s: %d frames in 4 s", rot, f, len(frames))
		if len(frames) < 5 {
			t.Fatalf("only %d frames", len(frames))
		}
		if out := os.Getenv("PRINTDECK_CAMERA_OUT"); out != "" {
			last := frames[len(frames)-1]
			os.WriteFile(filepath.Join(out, "capture-"+map[int]string{0: "0", 180: "180"}[rot]+".jpg"), last.JPEG(), 0o644)
			os.WriteFile(filepath.Join(out, "capture-small.jpg"), last.Small(320).JPEG(), 0o644)
		}
	}
}
