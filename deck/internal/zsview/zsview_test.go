//go:build zslice

package zsview

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The print view reads a G-code file, and draws the print up to an offset and up to a layer. It
// runs on a computer with Vulkan; ZSVIEW_SHOTS keeps the pictures.
func TestPrintView(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	path := os.Getenv("ZSVIEW_GCODE")
	if path == "" {
		path = "../gcodemeta/testdata/cura.gcode"
	}
	t0 := time.Now()
	g, err := OpenGcode(path)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	in, err := g.Info()
	if err != nil || in.Layers == 0 || in.Beads == 0 {
		t.Fatalf("info: %+v %v", in, err)
	}
	t.Logf("read %d bytes, %d layers, %d beads in %v", in.Size, in.Layers, in.Beads, time.Since(t0))
	mid := in.Size / 2
	if l := g.LayerAt(mid); l <= 0 || l >= in.Layers {
		t.Errorf("layer at the middle: %d of %d", l, in.Layers)
	}
	v, err := NewView(440, 700, false)
	if errors.Is(err, ErrNoVulkan) {
		t.Skip("no Vulkan")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.Load(g, 280, 280); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 440*700*4)
	shot := func(name string) {
		t1 := time.Now()
		if err := v.Draw(); err != nil {
			t.Fatal(err)
		}
		if err := v.Read(buf, 440*4); err != nil {
			t.Fatal(err)
		}
		d := time.Since(t1)
		colors := map[[3]byte]bool{}
		for i := 0; i < len(buf); i += 4 * 97 {
			colors[[3]byte{buf[i], buf[i+1], buf[i+2]}] = true
		}
		if len(colors) < 8 {
			t.Errorf("%s: a frame of %d colours", name, len(colors))
		}
		t.Logf("%s: draw and read in %v", name, d)
		if dir := os.Getenv("ZSVIEW_SHOTS"); dir != "" {
			img := &image.RGBA{Pix: buf, Stride: 440 * 4, Rect: image.Rect(0, 0, 440, 700)}
			f, _ := os.Create(filepath.Join(dir, name+".png"))
			png.Encode(f, img)
			f.Close()
		}
	}
	v.SetPosition(mid)
	shot("position")
	v.SetPosition(mid + 2000)
	shot("position-later")
	v.SetLayer(in.Layers / 3)
	shot("layer")
	v.Touch(1, TouchDown, 200, 300)
	v.Touch(1, TouchMove, 260, 320)
	v.Touch(1, TouchUp, 260, 320)
	shot("orbit")
	v.Reset()
	v.SetDark(true)
	if err := v.Colors(false, 0xff7a30); err != nil {
		t.Fatal(err)
	}
	v.SetPosition(mid)
	shot("dark")
	if err := v.Load(g, 0, 0); err != nil { // a bed around the part
		t.Fatal(err)
	}
	v.SetPosition(mid)
	shot("dark-fit")
	png, err := g.Thumbnail(120, 120, true, 0)
	if err != nil || len(png) < 100 {
		t.Fatalf("thumbnail: %d bytes, %v", len(png), err)
	}
}
