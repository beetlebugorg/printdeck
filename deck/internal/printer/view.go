package printer

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"printdeck/internal/klipper/toolhead"
	"sort"
	"strings"
	"sync"

	"printdeck/internal/gcode"
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

	est    estimate        // the time left of the print
	limits toolhead.Limits // the motion limits of the printer, for the plan of a print
	plan   *plan           // the plan of the loaded G-code, or nil
}

// SetLimits sets the motion limits of the printer. The next G-code that loads gets a plan.
func (v *View) SetLimits(l toolhead.Limits) {
	v.mu.Lock()
	v.limits = l
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
		layer, gc, mesh := v.view, v.gc, v.mesh
		if layer < 0 || gc == nil {
			v.viewBusy = false
			v.mu.Unlock()
			return
		}
		v.mu.Unlock()

		img := drawAt(gc, mesh, gc.LayerEnd(layer), gc.LayerStart(layer))

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
	v.mu.Lock()
	defer v.mu.Unlock()
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
		go v.render(v.gc, v.mesh, st.FilePos)
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
	limits := v.limits
	v.mu.Unlock()
	var pl *plan
	if gc != nil && limits.MaxVelocity > 0 {
		if body, err := v.src.Open(path); err == nil {
			pl = planFile(body, gc.StartEnd, limits)
			body.Close()
		}
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
	pathWarm     = color.RGBA{0xff, 0xb3, 0x6b, 255} // toolpath, low layers
	pathCool     = color.RGBA{0xff, 0x7a, 0x2e, 255} // toolpath, high layers
	pathGhost    = color.RGBA{0x2c, 0x33, 0x3f, 255}
)

// drawAt draws the print as it is at file offset pos, with the layer that starts at curStart as the one in progress.
func drawAt(gc *gcode.Model, mesh *stl.Mesh, pos, curStart int64) *image.RGBA {
	if mesh != nil {
		top := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
		low := 0.0
		if curStart > 0 {
			low = float64(gc.ZAt(curStart-1)) / float64(max(gc.MaxZ, 0.001))
		}
		if pos < 0 {
			top = -1
		}
		return mesh.Render(PreviewW, PreviewH, low, top, printedColor, hotColor, ghostColor)
	}
	return gc.Render(PreviewW, PreviewH, pos, curStart, pathWarm, pathCool, hotColor, pathGhost)
}

func (v *View) render(gc *gcode.Model, mesh *stl.Mesh, pos int64) {
	img := drawAt(gc, mesh, pos, gc.LayerStart(gc.LayerAt(pos)))
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
