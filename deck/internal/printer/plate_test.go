package printer

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printdeck/internal/gcode"
	"printdeck/internal/stl"
)

// tazMachine is the TAZ Workhorse of printer/printer.cfg: the travel of X and Y, and the 4
// points of bed_tilt.
var tazMachine = Machine{MinX: -50, MaxX: 293, MinY: -17, MaxY: 308,
	TiltPoints: [][2]float64{{-10, -10}, {290, -10}, {290, 290}, {-10, 290}}}

// enderMachine is the Ender 3 of the example config: a 235 x 235 mm bed.
var enderMachine = Machine{MaxX: 235, MaxY: 235}

// boxGcode is the G-code of a square tube with sides from lo to hi, n layers of 0.2 mm, with a
// skirt 5 mm out on the first layer.
func boxGcode(lo, hi float64, n int) string {
	var g strings.Builder
	g.WriteString("G90\nM83\nG1 X0 Y0 Z0.2 F3000\n")
	sq := func(a, b float64) {
		fmt.Fprintf(&g, "G1 X%g Y%g\nG1 X%g Y%g E1\nG1 X%g Y%g E1\nG1 X%g Y%g E1\nG1 X%g Y%g E1\n", a, a, b, a, b, b, a, b, a, a)
	}
	for l := 0; l < n; l++ {
		fmt.Fprintf(&g, ";LAYER:%d\nG1 Z%.1f\n", l, 0.2*float64(l+1))
		if l == 0 {
			g.WriteString(";TYPE:SKIRT\n")
			sq(lo-5, hi+5)
		}
		g.WriteString(";TYPE:WALL-OUTER\n")
		sq(lo, hi)
	}
	return g.String()
}

// sharePic loads the G-code src in a View, as the print of a busy printer at the file position
// pos, and draws its picture for the share page.
func sharePic(t testing.TB, src string, mesh *stl.Mesh, pos int64, b Bed) image.Image {
	t.Helper()
	gc, err := gcode.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	st := State{Printing: true, FilePath: "a.gcode", FileSize: int64(len(src)), FilePos: pos}
	v := &View{gc: gc, mesh: mesh, file: fileKey(st), view: -1}
	img := v.SharePicture(st, b)
	if img == nil {
		t.Fatal("no picture")
	}
	return img
}

// pixelAt returns the pixel of img where the point (x, y, z) of the bed is, with the camera of
// platePicture for the part box part on bed b.
func pixelAt(b Bed, part Box, top, x, y, z float64) image.Point {
	_, lo, hi := plateOf(b, part, true)
	cam := plateCamera(PreviewW, PreviewH, lo, hi, part, true, top)
	u, v := gcode.Project(x, y, z)
	return image.Pt(int(cam.X+u*cam.Scale), int(cam.Y+v*cam.Scale))
}

// near reports that the colors a and b differ by at most d in each channel.
func near(a, b color.RGBA, d int) bool {
	diff := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return diff(a.R, b.R) <= d && diff(a.G, b.G) <= d && diff(a.B, b.B) <= d
}

// isPEI reports that c is the color of the PEI sheet, with the shade of the lights.
func isPEI(c color.RGBA) bool {
	for f := 0.6; f <= 0.9; f += 0.01 {
		s := color.RGBA{uint8(float64(peiColor.R) * f), uint8(float64(peiColor.G) * f), uint8(float64(peiColor.B) * f), 255}
		if near(c, s, 6) {
			return true
		}
	}
	return false
}

func TestBedOf(t *testing.T) {
	b := BedOf(tazMachine, "")
	if !b.PEI || b.Min != [2]float64{0, 0} || b.Max != [2]float64{280, 280} || len(b.Washers) != 4 || b.Washers[0] != [2]float64{-14, -14} || b.Washers[2] != [2]float64{294, 294} {
		t.Errorf("TAZ from bed_tilt: %+v", b)
	}
	if b := BedOf(Machine{MinX: -50, MaxX: 293, MinY: -17, MaxY: 308}, "LulzBot TAZ Workhorse"); !b.PEI || b.Max != [2]float64{280, 280} || len(b.Washers) != 4 {
		t.Errorf("TAZ Workhorse from the model: %+v", b)
	}
	if b := BedOf(enderMachine, "Creality Ender3"); b.PEI || b.Washers != nil || b.Min != [2]float64{0, 0} || b.Max != [2]float64{235, 235} {
		t.Errorf("Ender 3: %+v", b)
	}
	if b := BedOf(Machine{MinX: -5, MaxX: 200, MinY: -3, MaxY: 210}, ""); b.Min != [2]float64{0, 0} || b.Max != [2]float64{200, 210} {
		t.Errorf("negative position_min: %+v", b)
	}
	if b := BedOf(Machine{HasMesh: true, MeshMin: [2]float64{20, 20}, MeshMax: [2]float64{180, 180}}, ""); b.Min != [2]float64{10, 10} || b.Max != [2]float64{190, 190} {
		t.Errorf("bed mesh: %+v", b)
	}
	if b := BedOf(Machine{}, ""); b.Known() {
		t.Errorf("no size: %+v", b)
	}
}

// The picture of a small part on the TAZ: the part is large, the PEI sheet is around it, and the
// part is at its place on the sheet.
func TestSharePictureTAZ(t *testing.T) {
	src := boxGcode(100, 140, 100)
	b := BedOf(tazMachine, "")
	img := sharePic(t, src, nil, int64(len(src)/2), b).(*image.RGBA)
	if img.Bounds() != image.Rect(0, 0, PreviewW, PreviewH) {
		t.Fatalf("size %v", img.Bounds())
	}
	// The plate is in the background around the part: no black and no transparent pixels.
	pei := 0
	for y := 0; y < PreviewH; y++ {
		for x := 0; x < PreviewW; x++ {
			c := img.RGBAAt(x, y)
			if c.A != 255 {
				t.Fatalf("pixel %d,%d is not opaque: %v", x, y, c)
			}
			if isPEI(c) {
				pei++
			}
		}
	}
	if pei < PreviewW*PreviewH/3 {
		t.Errorf("%d pixels of PEI, want a third of the picture", pei)
	}
	// The corners of the picture are PEI: the part is small on the bed, so the picture is in the
	// sheet.
	for _, p := range []image.Point{{2, 2}, {PreviewW - 3, 2}, {2, PreviewH - 3}, {PreviewW - 3, PreviewH - 3}} {
		if c := img.RGBAAt(p.X, p.Y); !isPEI(c) {
			t.Errorf("corner %v is %v, want PEI", p, c)
		}
	}
	// The part is large: the box of 40 mm is more than a third of the width of the picture.
	part := Box{Min: [3]float64{100, 100, 0.2}, Max: [3]float64{140, 140, 20}}
	a, c := pixelAt(b, part, 20, 100, 140, 0), pixelAt(b, part, 20, 140, 100, 0)
	if c.X-a.X < PreviewW/3 {
		t.Errorf("the part is small: %v to %v", a, c)
	}
	// The printed wall is at its place: the front corner of the box at the bottom has the color of
	// the print.
	p := pixelAt(b, part, 20, 100, 100, 1)
	found := false
	for dy := -3; dy <= 3 && !found; dy++ {
		for dx := -3; dx <= 3; dx++ {
			if c := img.RGBAAt(p.X+dx, p.Y+dy); c.R > c.B+40 && !isPEI(c) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("no print at the corner of the part, pixel %v", p)
	}
	// The inside of the tube shows the PEI sheet.
	if p := pixelAt(b, part, 20, 132, 132, 0); !isPEI(img.RGBAAt(p.X, p.Y)) {
		t.Errorf("the inside of the part at %v is %v", p, img.RGBAAt(p.X, p.Y))
	}
}

// A model mesh has the place of the G-code on the bed.
func TestSharePictureMesh(t *testing.T) {
	// A block of 40 x 40 x 20 mm at the origin of the mesh.
	c := func(i int) [3]float32 {
		return [3]float32{float32(i & 1 * 40), float32(i >> 1 & 1 * 40), float32(i >> 2 & 1 * 20)}
	}
	mesh := &stl.Mesh{MinZ: 0, MaxZ: 20}
	for _, f := range [][4]int{{0, 1, 3, 2}, {4, 5, 7, 6}, {0, 1, 5, 4}, {2, 3, 7, 6}, {0, 2, 6, 4}, {1, 3, 7, 5}} {
		mesh.Tris = append(mesh.Tris, stl.Tri{V: [3][3]float32{c(f[0]), c(f[1]), c(f[2])}}, stl.Tri{V: [3][3]float32{c(f[0]), c(f[2]), c(f[3])}})
	}
	src := boxGcode(100, 140, 100)
	b := BedOf(tazMachine, "")
	img := sharePic(t, src, mesh, int64(len(src)), b).(*image.RGBA)
	part := Box{Min: [3]float64{100, 100, 0}, Max: [3]float64{140, 140, 20}}
	for _, pt := range [][3]float64{{120, 120, 20}, {105, 105, 20}, {135, 135, 20}} {
		p := pixelAt(b, part, 20, pt[0], pt[1], pt[2])
		if c := img.RGBAAt(p.X, p.Y); isPEI(c) || c.R < c.B+40 {
			t.Errorf("the top of the model at %v is %v", pt, c)
		}
	}
	if p := pixelAt(b, part, 20, 165, 165, 0); !isPEI(img.RGBAAt(p.X, p.Y)) {
		t.Errorf("the plate behind the model is %v", img.RGBAAt(p.X, p.Y))
	}
}

// A part that is large on the bed shows the whole bed: the washers at the corners, and the
// background around the plate.
func TestSharePictureWholeBed(t *testing.T) {
	src := boxGcode(30, 250, 5)
	b := BedOf(tazMachine, "")
	img := sharePic(t, src, nil, int64(len(src)), b).(*image.RGBA)
	part := Box{Min: [3]float64{30, 30, 0.2}, Max: [3]float64{250, 250, 1}}
	_, lo, hi := plateOf(b, part, true)
	if lo[0] > -14 || hi[0] < 294 {
		t.Fatalf("the picture is from %v to %v, want the whole bed", lo, hi)
	}
	for _, w := range b.Washers {
		p := pixelAt(b, part, 1, w[0]+washerR*0.6, w[1], 0)
		if c := img.RGBAAt(p.X, p.Y); !near(c, color.RGBA{0xa2, 0xa4, 0xa6, 255}, 40) {
			t.Errorf("washer %v at %v is %v", w, p, c)
		}
	}
	// The top corners of the picture are the background.
	if c := img.RGBAAt(1, 1); !near(c, shareTop, 4) {
		t.Errorf("background %v, want %v", c, shareTop)
	}
}

// Another printer has a plain gray plate, and no washers.
func TestSharePictureGeneric(t *testing.T) {
	src := boxGcode(100, 130, 50)
	b := BedOf(enderMachine, "Creality Ender3")
	img := sharePic(t, src, nil, int64(len(src)), b).(*image.RGBA)
	// Some shades of the orange part are near the PEI color. The plate has none.
	gray, pei := 0, 0
	for y := 0; y < PreviewH; y++ {
		for x := 0; x < PreviewW; x++ {
			c := img.RGBAAt(x, y)
			if isPEI(c) {
				pei++
			}
			if near(c, color.RGBA{0x3d, 0x3f, 0x42, 255}, 12) {
				gray++
			}
		}
	}
	if pei > PreviewW*PreviewH/100 {
		t.Errorf("%d pixels of PEI", pei)
	}
	if gray < PreviewW*PreviewH/3 {
		t.Errorf("%d pixels of the gray plate, want a third of the picture", gray)
	}
	// A part near the edge of the bed: the edge of the plate and the background show.
	src = boxGcode(5, 30, 20)
	img = sharePic(t, src, nil, int64(len(src)), b).(*image.RGBA)
	if c := img.RGBAAt(1, PreviewH/2); !near(c, color.RGBA{0x1b, 0x1c, 0x1a, 255}, 10) {
		t.Errorf("left edge %v, want the background", c)
	}
}

// A G-code with no moves, and a printer with no bed size, give a picture of the plate.
func TestSharePictureEmpty(t *testing.T) {
	src := "G90\nM82\nG1 X10 Y10 Z0.2\n"
	for _, b := range []Bed{BedOf(tazMachine, ""), {}} {
		img := sharePic(t, src, nil, 0, b).(*image.RGBA)
		if c := img.RGBAAt(PreviewW/2, PreviewH/2); c.A != 255 || c == (color.RGBA{0, 0, 0, 255}) {
			t.Errorf("bed %+v: middle %v", b, c)
		}
	}
	src = boxGcode(100, 140, 10)
	img := sharePic(t, src, nil, int64(len(src)), Bed{}).(*image.RGBA)
	if c := img.RGBAAt(1, 1); !near(c, color.RGBA{0x3d, 0x3f, 0x42, 255}, 12) {
		t.Errorf("no bed size: corner %v, want the plate", c)
	}
	// No print: no picture.
	gc, _ := gcode.Parse(strings.NewReader(src))
	v := &View{gc: gc, view: -1}
	if img := v.SharePicture(State{}, Bed{}); img != nil {
		t.Error("a picture with no print")
	}
}

// BenchmarkPreview is the picture of the deck screens now, for the comparison with
// BenchmarkSharePicture.
func BenchmarkPreview(b *testing.B) {
	gc := benchGcode(b)
	for b.Loop() {
		drawAt(gc, nil, gc.Segs[len(gc.Segs)/2].Pos, 0, []color.RGBA{printedColor})
	}
}

func BenchmarkSharePicture(b *testing.B) {
	gc := benchGcode(b)
	v := &View{gc: gc, view: -1}
	v.loadBoxes(gc)
	bed := BedOf(tazMachine, "")
	for b.Loop() {
		platePicture(gc, nil, v.boxBody, v.bodyOK, gc.Segs[len(gc.Segs)/2].Pos, 0, []color.RGBA{printedColor}, bed)
	}
}

// BenchmarkPlate is the plate alone.
func BenchmarkPlate(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, PreviewW, PreviewH))
	p, lo, hi := plateOf(BedOf(tazMachine, ""), Box{Min: [3]float64{100, 100, 0}, Max: [3]float64{150, 150, 0}}, true)
	cam := plateCamera(PreviewW, PreviewH, lo, hi, Box{}, false, 0)
	for b.Loop() {
		gcode.DrawPlate(img, cam, p, shareTop, shareBottom)
	}
}

// benchGcode is the G-code of the benchmarks: the file of PRINTDECK_BENCH_GCODE, or the two
// layers of testdata.
func benchGcode(b *testing.B) *gcode.Model {
	path := os.Getenv("PRINTDECK_BENCH_GCODE")
	if path == "" {
		path = "testdata/two-layers.gcode"
	}
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	gc, err := gcode.Parse(f)
	if err != nil || len(gc.Segs) == 0 {
		b.Fatal("no moves", err)
	}
	return gc
}

// TestPlateExamples writes example pictures to the folder PRINTDECK_PLATE_DIR, for a look: the
// G-code of testdata and the G-code files of PRINTDECK_PLATE_GCODE (a list with commas) on the
// TAZ, and a box on an Ender 3. PRINTDECK_PLATE_STL is a model for the first file of the list.
func TestPlateExamples(t *testing.T) {
	dir := os.Getenv("PRINTDECK_PLATE_DIR")
	if dir == "" {
		t.Skip("PRINTDECK_PLATE_DIR is not set")
	}
	write := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	taz := BedOf(tazMachine, "")
	files := []string{"testdata/two-layers.gcode"}
	if l := os.Getenv("PRINTDECK_PLATE_GCODE"); l != "" {
		files = append(files, strings.Split(l, ",")...)
	}
	for i, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		write("taz-"+name+".png", sharePic(t, src, nil, int64(len(src)/2), taz))
		if s := os.Getenv("PRINTDECK_PLATE_STL"); s != "" && i == 1 {
			f, err := os.Open(s)
			if err != nil {
				t.Fatal(err)
			}
			mesh, err := stl.Parse(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			write("taz-"+name+"-model.png", sharePic(t, src, mesh, int64(len(src)/2), taz))
		}
	}
	src := boxGcode(100, 140, 100)
	write("taz-box.png", sharePic(t, src, nil, int64(len(src)/2), taz))
	src = boxGcode(30, 250, 10)
	write("taz-big.png", sharePic(t, src, nil, int64(len(src)/2), taz))
	ender := BedOf(enderMachine, "Creality Ender3")
	src = boxGcode(90, 130, 100)
	write("ender3-box.png", sharePic(t, src, nil, int64(len(src)/2), ender))
	src = boxGcode(5, 40, 60)
	write("ender3-edge.png", sharePic(t, src, nil, int64(len(src)/2), ender))
}
