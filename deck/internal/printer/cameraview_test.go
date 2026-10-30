package printer

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/gcode"
)

// The share picture uses the view of the camera when the deck has one, with the whole plate in
// the background. PRINTDECK_PICTURES=<folder> saves the pictures, to look at them.
func TestSharePictureCameraView(t *testing.T) {
	src := boxGcode(100, 140, 100)
	b := BedOf(tazMachine, "")
	gc, err := gcode.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	st := State{Printing: true, FilePath: "a.gcode", FileSize: int64(len(src)), FilePos: int64(len(src) / 2)}
	pic := func(cv func() (gcode.View, bool)) *image.RGBA {
		v := &View{gc: gc, file: fileKey(st), view: -1, CameraView: cv}
		return v.SharePicture(st, b).(*image.RGBA)
	}
	def := pic(nil)
	same := pic(func() (gcode.View, bool) { return gcode.View{}, false })
	turned := pic(func() (gcode.View, bool) { return gcode.NewView(-37, 30), true })
	if string(def.Pix) != string(same.Pix) {
		t.Error("a camera with no view changed the picture")
	}
	if string(def.Pix) == string(turned.Pix) {
		t.Error("the camera view did not change the picture")
	}
	for y := range PreviewH {
		for x := range PreviewW {
			if c := turned.RGBAAt(x, y); c.A != 255 || c.R+c.G+c.B == 0 {
				t.Fatalf("a black or clear pixel at (%d, %d): %v", x, y, c)
			}
		}
	}
	if dir := os.Getenv("PRINTDECK_PICTURES"); dir != "" {
		for name, img := range map[string]*image.RGBA{"default.png": def, "turned.png": turned} {
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
	}
}
