package printer

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"printdeck/internal/klipper/toolhead"
	"sort"
	"strings"
	"sync"

	"printdeck/internal/gcode"
	"printdeck/internal/maxmode"
	"printdeck/internal/stl"
)

const (
	PreviewW, PreviewH = 428, 350
	maxModelSize       = 80 << 20 // larger G-code files are not drawn
)

// Source gives the View the files of the printer.
type Source interface {
	// Open opens a file of the file store: the G-code of a job, or an STL model.
	Open(path string) (io.ReadCloser, error)
	// Models lists the STL models in the file store.
	Models() []File
}

// View keeps the picture of the model that is printing.
//
// The G-code of the print is read once. It tells how far the print has come. If there is one object
// on the bed and an STL with a matching name, in the file store or in ModelDir, the picture is that
// model, shaded, with the printed height in colour. Otherwise it is the toolpath from the G-code.
type View struct {
	src      Source
	ModelDir string // a folder with more STL files, besides the ones in the file store

	mu        sync.Mutex
	file      string // fileKey of the loaded G-code
	gc        *gcode.Model
	mesh      *stl.Mesh
	loading   bool
	rendering bool
	img       image.Image
	imgPos    int64

	// The scrubber of the screen. view is the layer to show (counting from 1), or -1 for the print as it is now.
	view     int
	viewImg  image.Image
	viewBusy bool

	est     estimate          // the time left of the print
	limits  toolhead.Limits   // the motion limits of the printer, for the plan of a print
	maxMode *maxmode.Settings // the [max_mode] section of the printer when it has enable: True, or nil
	plan    *plan             // the plan of the loaded G-code, or nil

	records map[string]*recordPic // the pictures of finished prints (RecordPicture)
	files   map[string]*recordPic // the pictures of files with no thumbnail (FilePicture)
	color   string                // the color of the roll in the printer, for the print now
	// startColor is the color of the roll in the printer at the start of the print: the color of
	// the part before the first color change of the file (gcode.Model.Changes).
	startColor string

	layerKey   string    // the file of layerTimes
	layerTimes []float64 // the seconds of each layer of the print (LayerTimes)

	// Rolls are the rolls of filament, or nil. The picture of a print has the color of its roll.
	Rolls   *Spools
	fileRun bool // a picture of a file is drawn now

	boxGC          *gcode.Model // the G-code of boxAll and boxFirst
	boxAll         Box          // the extents of all the extruding moves of boxGC
	boxFirst       Box          // the extents of the moves of its first layer
	boxOK, firstOK bool
	boxBody        Box  // the extents of the moves from the second layer on (PartBox)
	bodyOK         bool // boxBody has a move
}

// Box is a box of the moves of a print, in mm, in the coordinates of the G-code.
type Box struct {
	Min, Max [3]float64
}

// add puts the move s in the box. ok is false for an empty box.
func (b *Box) add(s gcode.Seg, ok bool) {
	for _, p := range [][3]float32{{s.X0, s.Y0, s.Z0}, {s.X1, s.Y1, s.Z1}} {
		for i := range 3 {
			v := float64(p[i])
			if !ok || v < b.Min[i] {
				b.Min[i] = v
			}
			if !ok || v > b.Max[i] {
				b.Max[i] = v
			}
		}
		ok = true
	}
}

// PrintBox returns the extents of the extruding moves of the G-code of the print of st: the box
// of all the moves, with its top at the height of the move at the file position upto, and the
// box of the moves of the first layer. With no print in st, it is the G-code of the last print.
// ok is false with no G-code, or while the G-code of a new print loads. The camera of the deck
// follows the part with them.
func (v *View) PrintBox(st State, upto int64) (part, first Box, partOK, firstOK bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	gc := v.gc
	if gc == nil || len(gc.Segs) == 0 || (st.Busy() && v.file != fileKey(st)) {
		return part, first, false, false
	}
	v.loadBoxes(gc)
	part, first = v.boxAll, v.boxFirst
	if top := float64(gc.ZAt(upto)); top < part.Max[2] {
		part.Max[2] = max(top, first.Max[2])
	}
	return part, first, v.boxOK, v.firstOK
}

// loadBoxes computes the boxes of the moves of gc, once for each G-code.
func (v *View) loadBoxes(gc *gcode.Model) {
	if v.boxGC == gc {
		return
	}
	v.boxGC, v.boxOK, v.firstOK, v.bodyOK = gc, false, false, false
	v.boxAll, v.boxFirst, v.boxBody = Box{}, Box{}, Box{}
	// The first layer: the moves from the first layer mark up to the second, or the moves at the
	// height of the first move for a file with no layer marks.
	lo, hi := gc.LayerStart(1), gc.LayerEnd(1)
	z0 := gc.Segs[0].Z1
	for _, s := range gc.Segs {
		v.boxAll.add(s, v.boxOK)
		v.boxOK = true
		inFirst := s.Pos > lo && s.Pos <= hi
		if len(gc.LayerPos) == 0 {
			inFirst = s.Z1 <= z0+0.01
		}
		if inFirst {
			v.boxFirst.add(s, v.firstOK)
			v.firstOK = true
		} else if s.Pos > hi || len(gc.LayerPos) == 0 {
			v.boxBody.add(s, v.bodyOK)
			v.bodyOK = true
		}
	}
	if !v.bodyOK {
		// A print of one layer.
		v.boxBody, v.bodyOK = v.boxFirst, v.firstOK
	}
}

// PartBox returns the extents of the extruding moves of the part of the print of st, from its
// second layer on, so the box has no purge line, skirt or brim of the first layer. The box of a
// print of one layer is its first layer. ok is false with no G-code, or while the G-code of a new
// print loads. The time lapse parks and crops with it.
func (v *View) PartBox(st State) (part Box, ok bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	gc := v.gc
	if gc == nil || len(gc.Segs) == 0 || (st.Busy() && v.file != fileKey(st)) {
		return part, false
	}
	v.loadBoxes(gc)
	return v.boxBody, v.bodyOK
}

// SetLimits sets the motion limits of the printer, and the max mode settings for a printer that
// has max mode on (or nil). The next G-code that loads gets a plan.
func (v *View) SetLimits(l toolhead.Limits, mm *maxmode.Settings) {
	v.mu.Lock()
	v.limits, v.maxMode = l, mm
	v.mu.Unlock()
}

func NewView(src Source) *View { return &View{src: src, view: -1} }

// Preview returns the picture of the print, or nil. It is the layer of the scrubber when there is one,
// and the print as it is now otherwise.
func (v *View) Preview() image.Image {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.view >= 0 && v.viewImg != nil {
		return v.viewImg
	}
	return v.img
}

// LayerTimes returns the seconds of each layer of the print, from the plan of the slicer: the
// first item is layer 1. It returns nil when the G-code is not loaded or has no times.
func (v *View) LayerTimes() []float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.gc == nil {
		return nil
	}
	if v.layerKey != v.file {
		v.layerKey, v.layerTimes = v.file, layerTimes(v.gc)
	}
	return v.layerTimes
}

// LayerLeft returns the seconds of the plan of the slicer from the file position pos to the end
// of the layer (1 is the first layer), and the seconds from pos to the end of the file. It
// returns false when the G-code is not loaded or has no times.
func (v *View) LayerLeft(pos int64, layer int) (toLayerEnd, toEnd float64, ok bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	gc := v.gc
	if gc == nil || layer < 1 || layer > len(gc.LayerPos) {
		return 0, 0, false
	}
	now, total, ok := gc.TimeAt(pos)
	if !ok {
		return 0, 0, false
	}
	end := total
	if layer < len(gc.LayerPos) {
		end, _, _ = gc.TimeAt(gc.LayerPos[layer])
	}
	return max(0, end-now), max(0, total-now), true
}

// layerTimes is the seconds of each layer of gc, or nil.
func layerTimes(gc *gcode.Model) []float64 {
	n := len(gc.LayerPos)
	if n == 0 {
		return nil
	}
	if _, _, ok := gc.TimeAt(0); !ok {
		return nil
	}
	out := make([]float64, n)
	for i := range out {
		end := int64(math.MaxInt64)
		if i+1 < n {
			end = gc.LayerPos[i+1]
		}
		t0, _, _ := gc.TimeAt(gc.LayerPos[i])
		t1, _, _ := gc.TimeAt(end)
		out[i] = max(0, t1-t0)
	}
	return out
}

// ViewLayer returns the layer of the scrubber, or -1 when the preview follows the print.
func (v *View) ViewLayer() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.view
}

// SetViewLayer shows the model up to a layer (0 is nothing). A negative layer follows the print again.
// The picture is drawn in the background, and only for the newest layer when the scrubber moves fast.
func (v *View) SetViewLayer(layer int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.view = max(layer, -1)
	if v.view < 0 {
		v.viewImg = nil
		return
	}
	if v.gc != nil && !v.viewBusy {
		v.viewBusy = true
		go v.renderView()
	}
}

func (v *View) renderView() {
	for {
		v.mu.Lock()
		layer, gc, mesh, hex := v.view, v.gc, v.mesh, v.startColor
		if layer < 0 || gc == nil {
			v.viewBusy = false
			v.mu.Unlock()
			return
		}
		v.mu.Unlock()

		img := drawAt(gc, mesh, gc.LayerEnd(layer), gc.LayerStart(layer), v.palette(gc, hex))

		v.mu.Lock()
		if v.view == layer { // still the one that is wanted
			v.viewImg, v.viewBusy = img, false
			v.mu.Unlock()
			return
		}
		v.mu.Unlock() // the scrubber moved on: draw the new layer
	}
}

// Update follows the job: it reads the G-code of a new job, and draws the print again when it moved.
func (v *View) Update(st State) {
	hex := ""
	if v.Rolls != nil {
		if sp, ok := v.Rolls.In(); ok {
			hex = rollHex(sp.Color, sp.Label())
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if hex != v.color {
		v.color, v.img = hex, nil // draw again in the new color
	}
	if v.file != fileKey(st) || v.gc == nil || len(v.gc.Changes) == 0 {
		v.startColor = hex // a print with no color change has the color of the roll now
	}
	if !st.Busy() || st.FilePath == "" {
		v.img, v.imgPos, v.view, v.viewImg = nil, 0, -1, nil
		return
	}
	if v.file != fileKey(st) {
		v.img, v.imgPos, v.view, v.viewImg = nil, 0, -1, nil // not the picture of another file while this one loads
		if !v.loading && st.FileSize <= maxModelSize {
			v.loading = true
			go v.loadModel(st.FilePath, fileKey(st))
		}
		return
	}
	// Draw again when the print has moved. A render is about 30 ms.
	if v.gc != nil && !v.rendering && (v.img == nil || st.FilePos != v.imgPos) {
		v.rendering = true
		go v.render(v.gc, v.mesh, st.FilePos, v.startColor)
	}
}

// fileKey names the file of a job by its path, size and upload time, so a file that is sliced again
// and uploaded under the same name is read again.
func fileKey(st State) string {
	return fmt.Sprintf("%s|%d|%d", st.FilePath, st.FileSize, st.FileDate)
}

func (v *View) loadModel(path, key string) {
	var gc *gcode.Model
	if body, err := v.src.Open(path); err == nil {
		gc, _ = gcode.Parse(body)
		body.Close()
	}
	// The plan of the print, on the motion planner with the limits of the printer.
	v.mu.Lock()
	limits, mm := v.limits, v.maxMode
	v.mu.Unlock()
	var pl *plan
	if gc != nil && limits.MaxVelocity > 0 {
		pl = planGcode(func() (io.ReadCloser, error) { return v.src.Open(path) }, gc.StartEnd, limits, mm)
	}
	// An STL is one object. With more on the bed, only the toolpath shows them all.
	var mesh *stl.Mesh
	if gc != nil && gc.Objects <= 1 {
		mesh = v.findSTL(path)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.loading = false
	// On failure the key is still stored, so the same file is not tried again on each update.
	v.file, v.gc, v.mesh, v.img, v.view, v.plan = key, gc, mesh, nil, -1, pl
}

// stem makes names comparable: "LTAZ_lower_panel.gcode" and "Lower Panel.STL" give a match.
func stem(name string) string {
	name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(name))
}

// match returns how well an STL name fits a G-code name: 0 is no match, and higher is closer.
// The names match when one contains the other. The closest is the one with the least left over,
// so "inky_frame.gcode" matches "Inky Frame.stl" before "frame.stl".
func match(gcodeName, stlName string) int {
	g, s := stem(gcodeName), stem(stlName)
	if len(s) < 4 || len(g) < 4 || !(strings.Contains(g, s) || strings.Contains(s, g)) {
		return 0
	}
	return 1000 - (max(len(g), len(s)) - min(len(g), len(s)))
}

// findSTL looks for the model of a G-code file in ModelDir and in the file store, and reads the closest
// match. ModelDir wins when both have an equally good match.
func (v *View) findSTL(gcodePath string) *stl.Mesh {
	type candidate struct {
		open  func() (io.ReadCloser, error)
		score int
	}
	var cands []candidate
	if v.ModelDir != "" {
		files, _ := filepath.Glob(filepath.Join(v.ModelDir, "*"))
		for _, f := range files {
			if strings.EqualFold(filepath.Ext(f), ".stl") {
				if n := match(gcodePath, f); n > 0 {
					cands = append(cands, candidate{func() (io.ReadCloser, error) { return os.Open(f) }, n})
				}
			}
		}
	}
	for _, f := range v.src.Models() {
		if n := match(gcodePath, f.Name); n > 0 {
			cands = append(cands, candidate{func() (io.ReadCloser, error) { return v.src.Open(f.Path) }, n})
		}
	}
	// The best first; a file that does not read gives way to the next.
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	for _, cd := range cands {
		if r, err := cd.open(); err == nil {
			m, _ := stl.Parse(r)
			r.Close()
			if m != nil {
				return m
			}
		}
	}
	return nil
}

var (
	printedColor = color.RGBA{0xff, 0x9f, 0x43, 255}
	hotColor     = color.RGBA{0xff, 0xf0, 0xb0, 255} // the layer that prints now
	ghostColor   = color.RGBA{0x55, 0x5e, 0x6d, 255}
	pathGhost    = color.RGBA{0x2c, 0x33, 0x3f, 255}
)

// drawAt draws the print as it is at file offset pos, with the layer that starts at curStart as the
// one in progress, in the colors of palette: a color for each part of a print with color changes.
func drawAt(gc *gcode.Model, mesh *stl.Mesh, pos, curStart int64, colors []color.RGBA) *image.RGBA {
	col := colors[0]
	if mesh != nil {
		top := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
		low := 0.0
		if curStart > 0 {
			low = float64(gc.ZAt(curStart-1)) / float64(max(gc.MaxZ, 0.001))
		}
		if pos < 0 {
			top = -1
		}
		return mesh.Render(PreviewW, PreviewH, low, top, col, hotColor, ghostColor)
	}
	return gc.RenderColors(PreviewW, PreviewH, pos, curStart, colors, hotColor, pathGhost)
}

// palette is the colors of the parts of the print of gc: start, the color of the roll at the start
// of the print, then the color of the roll of each color change of the file.
func (v *View) palette(gc *gcode.Model, start string) []color.RGBA {
	out := []color.RGBA{ModelColor(start)}
	for _, ch := range gc.Changes {
		c := ""
		if v.Rolls != nil {
			if sp, ok := v.Rolls.Get(ch.Roll); ok {
				c = rollHex(sp.Color, sp.Label())
			}
		}
		out = append(out, ModelColor(c))
	}
	return out
}

func (v *View) render(gc *gcode.Model, mesh *stl.Mesh, pos int64, start string) {
	img := drawAt(gc, mesh, pos, gc.LayerStart(gc.LayerAt(pos)), v.palette(gc, start))
	v.mu.Lock()
	v.img, v.imgPos, v.rendering = img, pos, false
	v.mu.Unlock()
}

// PreviewSTL draws the model view of a part from an STL file, printed up to the height frac (0 to
// 1), with the layer under frac hot. The manual shows it as the print that runs.
func PreviewSTL(r io.Reader, frac float64) (image.Image, error) {
	m, err := stl.Parse(r)
	if err != nil {
		return nil, err
	}
	return m.Render(PreviewW, PreviewH, max(0, frac-0.04), frac, printedColor, hotColor, ghostColor), nil
}

// recordPic is the picture of a finished print, drawn once.
type recordPic struct {
	img     image.Image
	loading bool
}

// RecordPicture returns the picture of a finished print: the model, and for a print that did not
// complete, the part that printed in colour and the rest as a ghost. The first call starts the
// drawing in the background and reports loading. img is nil when the file is gone.
func (v *View) RecordPicture(r Record) (img image.Image, loading bool) {
	hex := ColorFromName(r.Spool)
	if v.Rolls != nil {
		if sp, ok := v.Rolls.Get(r.SpoolID); ok {
			hex = rollHex(sp.Color, sp.Label())
		}
	}
	k := fmt.Sprintf("%s|%d|%d|%s", r.Path, r.Pos, r.End.Unix(), hex)
	v.mu.Lock()
	defer v.mu.Unlock()
	if p, ok := v.records[k]; ok {
		return p.img, p.loading
	}
	if v.records == nil || len(v.records) >= 8 {
		v.records = map[string]*recordPic{}
	}
	p := &recordPic{loading: true}
	v.records[k] = p
	go func() {
		img := v.drawRecord(r, hex)
		v.mu.Lock()
		p.img, p.loading = img, false
		v.mu.Unlock()
	}()
	return nil, true
}

// FilePictureSize is the width and the height of the picture of a file (FilePicture).
const FilePictureSize = 240

// FilePicture returns a picture of the G-code of a file in the color hex of its filament, for a
// file that has no thumbnail from the slicer. It returns nil while the picture is drawn, and for a file that has no extruding moves.
// The pictures are drawn one at a time: a call while another file draws returns nil, and a later
// call starts the file.
func (v *View) FilePicture(f File, hex string) image.Image {
	k := fmt.Sprintf("%s|%d|%s", f.Path, f.Date.Unix(), hex)
	v.mu.Lock()
	defer v.mu.Unlock()
	if p, ok := v.files[k]; ok {
		return p.img
	}
	if v.fileRun {
		return nil
	}
	if v.files == nil || len(v.files) >= 64 {
		v.files = map[string]*recordPic{}
	}
	p := &recordPic{loading: true}
	v.files[k] = p
	v.fileRun = true
	go func() {
		var img image.Image
		if body, err := v.src.Open(f.Path); err == nil {
			gc, _ := gcode.Parse(io.LimitReader(body, maxModelSize))
			body.Close()
			if gc != nil && len(gc.Segs) > 0 {
				img = gc.RenderColors(FilePictureSize, FilePictureSize, math.MaxInt64, math.MaxInt64, v.palette(gc, hex), hotColor, pathGhost)
			}
		}
		v.mu.Lock()
		p.img, p.loading, v.fileRun = img, false, false
		v.mu.Unlock()
	}()
	return nil
}

// drawRecord draws the picture of a finished print, or returns nil.
func (v *View) drawRecord(r Record, hex string) image.Image {
	col := ModelColor(hex)
	body, err := v.src.Open(r.Path)
	if err != nil {
		return nil
	}
	gc, _ := gcode.Parse(body)
	body.Close()
	if gc == nil {
		return nil
	}
	pos := int64(math.MaxInt64)
	if r.Result != "complete" && r.Pos > 0 {
		pos = r.Pos
	}
	if gc.Objects <= 1 {
		if mesh := v.findSTL(r.Path); mesh != nil {
			top := 1.0
			if pos != math.MaxInt64 {
				top = float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
			}
			return mesh.Render(PreviewW, PreviewH, top, top, col, hotColor, ghostColor)
		}
	}
	return gc.RenderColors(PreviewW, PreviewH, pos, math.MaxInt64, v.palette(gc, hex), hotColor, pathGhost)
}

// Toolpath gives the extruding moves of the print from move from, up to the file position upto:
// the part that is printed. key names the G-code of the print: when it is not key, the moves start
// at 0, for a new print. It returns the key of the print, the index of the first move, and the
// moves, at most max of them.
func (v *View) Toolpath(key string, from int, upto int64, max int) (string, int, []gcode.Seg) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.gc == nil {
		return "", 0, nil
	}
	if key != v.file {
		from = 0
	}
	from = min(from, len(v.gc.Segs))
	end := from
	for end < len(v.gc.Segs) && end-from < max && v.gc.Segs[end].Pos <= upto {
		end++
	}
	return v.file, from, v.gc.Segs[from:end]
}

// rollHex is the color of a roll: its color, or the color word of its name, or "".
func rollHex(hex, name string) string {
	if _, ok := ParseHex(hex); ok {
		return hex
	}
	return ColorFromName(name)
}

// ModelPicture draws the model of the print of st at the size w x h, for an app: the printed part
// up to the file position of st, and the rest of the model. With mono, the picture is gray with
// three values only: the printed part is white, the rest is a checker of white and black, and
// the background is black, for a display of dots. Else it has the colors of the screen of the deck
// on a transparent background. It returns nil when the G-code of the print is not loaded.
func (v *View) ModelPicture(st State, w, h int, mono bool) image.Image {
	v.mu.Lock()
	gc, mesh, hex := v.gc, v.mesh, v.color
	if v.file != fileKey(st) {
		gc = nil
	}
	v.mu.Unlock()
	if gc == nil {
		return nil
	}
	pos := st.FilePos
	curStart := gc.LayerStart(gc.LayerAt(pos))
	if !mono {
		col := ModelColor(hex)
		if mesh != nil {
			top := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
			return mesh.Render(w, h, top, top, col, col, ghostColor)
		}
		return gc.Render(w, h, pos, curStart, col, hotColor, pathGhost)
	}
	// Red for the printed part and blue for the rest: the shading changes the brightness of a
	// pixel, and the larger channel still tells the two apart.
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	var img *image.RGBA
	if mesh != nil {
		top := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
		img = mesh.Render(w, h, top, top, red, red, blue)
	} else {
		img = gc.Render(w, h, pos, math.MaxInt64, red, red, blue)
	}
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			switch {
			case c.A < 128:
			case c.R >= c.B:
				out.Pix[y*out.Stride+x] = 255
			case (x+y)%2 == 0:
				out.Pix[y*out.Stride+x] = 255
			}
		}
	}
	return out
}
