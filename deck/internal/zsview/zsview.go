//go:build zslice

// Package zsview is the binding of the print view of zslice (include/zslice-app.h, "a print
// view"): it reads a G-code file with the byte offset of each move, and draws the print with the
// GPU (Vulkan) into an image of the view. The deck draws that image as its 3D view of a print.
//
// The package builds only with the tag zslice, and with the library of zslice:
//
//	CGO_CFLAGS=-I<zslice>/include CGO_LDFLAGS=-L<zslice>/zig-out/lib go build -tags zslice
//
// Each handle belongs to one goroutine at a time. The goroutine of a View must call
// runtime.LockOSThread: the Vulkan driver can keep state on the thread.
package zsview

/*
#cgo LDFLAGS: -l:libzslice.a -lm -lpthread -ldl
#include <stdlib.h>
#include <zslice-app.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Available reports that the deck has the print view of zslice.
const Available = true

// Errors of the calls.
var (
	ErrNoVulkan = errors.New("zsview: no Vulkan for the view")
	ErrBusy     = errors.New("zsview: the view has no frame yet")
	ErrNoResult = errors.New("zsview: no result")
)

func check(op string, rc C.int) error {
	switch rc {
	case C.ZS_OK:
		return nil
	case C.ZS_NOT_SUPPORTED:
		return ErrNoVulkan
	case C.ZS_BUSY:
		return ErrBusy
	case C.ZS_NO_RESULT:
		return ErrNoResult
	}
	return fmt.Errorf("zsview: %s: error %d", op, int(rc))
}

// Gcode is a G-code file that zslice read.
type Gcode struct{ g *C.zs_gcode }

// Info is the summary of a G-code file.
type Info struct {
	Size, Moves, Beads uint64
	Layers             int
	TimeS, FilamentMM  float64
	Box                [6]float64 // min x, y, z, then max x, y, z (mm)
}

// Layer is one layer of a G-code file: its bytes, its height, and its print times.
type Layer struct {
	Start, End   uint64
	Z, Height    float64
	StartS, EndS float64
}

// State is the printer at a byte offset of the file.
type State struct {
	Layer   int
	Move    uint64
	Feature int // a zslice feature of the last bead, or -1
	Travel  bool
	Speed   float64 // mm/s
	X, Y, Z float64
}

// OpenGcode reads the G-code file at path.
func OpenGcode(path string) (*Gcode, error) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	var g *C.zs_gcode
	if err := check("open", C.zs_gcode_open(cp, &g)); err != nil {
		return nil, err
	}
	return &Gcode{g: g}, nil
}

// Close frees the file. A View that shows it must be closed or load another file first.
func (g *Gcode) Close() {
	if g != nil && g.g != nil {
		C.zs_gcode_free(g.g)
		g.g = nil
	}
}

// Info returns the summary of the file.
func (g *Gcode) Info() (Info, error) {
	var in C.zs_gcode_info
	if err := check("info", C.zs_gcode_get_info(g.g, &in)); err != nil {
		return Info{}, err
	}
	out := Info{Size: uint64(in.size), Moves: uint64(in.moves), Beads: uint64(in.beads), Layers: int(in.layers),
		TimeS: float64(in.time_s), FilamentMM: float64(in.filament_mm)}
	for i := range out.Box {
		out.Box[i] = float64(in.box[i])
	}
	return out, nil
}

// Layer returns the layer i (from 0).
func (g *Gcode) Layer(i int) (Layer, error) {
	var l C.zs_gcode_layer
	if err := check("layer", C.zs_gcode_get_layer(g.g, C.size_t(i), &l)); err != nil {
		return Layer{}, err
	}
	return Layer{Start: uint64(l.start), End: uint64(l.end), Z: float64(l.z), Height: float64(l.height),
		StartS: float64(l.start_s), EndS: float64(l.end_s)}, nil
}

// LayerAt returns the layer at the byte offset.
func (g *Gcode) LayerAt(offset uint64) int { return int(C.zs_gcode_layer_at(g.g, C.uint64_t(offset))) }

// StateAt returns the printer at the byte offset. ok is false before the first move.
func (g *Gcode) StateAt(offset uint64) (State, bool) {
	var s C.zs_gcode_state
	if C.zs_gcode_state_at(g.g, C.uint64_t(offset), &s) != C.ZS_OK {
		return State{}, false
	}
	return State{Layer: int(s.layer), Move: uint64(s.move), Feature: int(s.feature), Travel: s.travel != 0,
		Speed: float64(s.speed), X: float64(s.x), Y: float64(s.y), Z: float64(s.z)}, true
}

// Thumbnail returns a PNG of the file, w x h pixels, with a clear background: one colour for each
// feature, or the colour rgb (0xRRGGBB).
func (g *Gcode) Thumbnail(w, h int, byFeature bool, rgb uint32) ([]byte, error) {
	var p *C.uint8_t
	var n C.size_t
	if err := check("thumbnail", C.zs_gcode_thumbnail_png(g.g, C.uint32_t(w), C.uint32_t(h), cbool(byFeature), C.uint32_t(rgb), &p, &n)); err != nil {
		return nil, err
	}
	defer C.zs_buffer_free(p, n)
	return C.GoBytes(unsafe.Pointer(p), C.int(n)), nil
}

// View is an offscreen print view: it draws into an image that it owns.
type View struct {
	v    *C.zs_view
	w, h int
}

// NewView makes a reduced print view of w x h pixels. With bgra, Read gives the bytes in the
// order of a Linux framebuffer (B, G, R, A); else R, G, B, A.
func NewView(w, h int, bgra bool) (*View, error) {
	mode := C.int(C.ZS_VIEW_REDUCED)
	if bgra {
		mode |= C.ZS_VIEW_BGRA
	}
	var v *C.zs_view
	if err := check("view", C.zs_view_offscreen(C.uint32_t(w), C.uint32_t(h), mode, &v)); err != nil {
		return nil, err
	}
	return &View{v: v, w: w, h: h}, nil
}

// Close frees the view.
func (v *View) Close() {
	if v != nil && v.v != nil {
		C.zs_view_free(v.v)
		v.v = nil
	}
}

// Size returns the size of the view in pixels.
func (v *View) Size() (w, h int) { return v.w, v.h }

// Resize changes the size of the view.
func (v *View) Resize(w, h int) {
	C.zs_view_resize(v.v, C.uint32_t(w), C.uint32_t(h))
	v.w, v.h = w, h
}

// Load shows the file g. bedX and bedY are the bed size in mm, or 0 for a bed around the part.
// Keep g open while the view shows it.
func (v *View) Load(g *Gcode, bedX, bedY float64) error {
	return check("load", C.zs_view_load_gcode(v.v, g.g, C.double(bedX), C.double(bedY)))
}

// SetPosition shows the print at the byte offset of the printer: the moves read, the nozzle, and
// the top layer lit.
func (v *View) SetPosition(offset uint64) { C.zs_view_set_position(v.v, C.uint64_t(offset)) }

// SetLayer shows whole layers up to the layer i (the scrubber).
func (v *View) SetLayer(i int) { C.zs_view_set_layer(v.v, C.size_t(i)) }

// Colors sets one colour for each feature, or the filament colour rgb (0xRRGGBB).
func (v *View) Colors(byFeature bool, rgb uint32) error {
	return check("colors", C.zs_view_gcode_colors(v.v, cbool(byFeature), C.uint32_t(rgb)))
}

// SetDark sets the night look.
func (v *View) SetDark(on bool) { C.zs_view_set_dark(v.v, cbool(on)) }

// PrintLook shows the nozzle at the position, and the top layer lit.
func (v *View) PrintLook(nozzle, lit bool) { C.zs_view_print_look(v.v, cbool(nozzle), cbool(lit)) }

// The actions of a touch.
const (
	TouchDown   = C.ZS_TOUCH_DOWN
	TouchMove   = C.ZS_TOUCH_MOVE
	TouchUp     = C.ZS_TOUCH_UP
	TouchCancel = C.ZS_TOUCH_CANCEL
)

// Touch gives a finger to the view: one finger orbits, two fingers zoom and pan. x and y are in
// the pixels of the view.
func (v *View) Touch(id int64, action int, x, y float32) {
	C.zs_view_touch(v.v, C.int64_t(id), C.int(action), C.float(x), C.float(y))
}

// Reset goes back to the start view.
func (v *View) Reset() { C.zs_view_reset(v.v) }

// Draw draws a frame.
func (v *View) Draw() error { return check("draw", C.zs_view_draw(v.v)) }

// Read copies the last frame into dst, stride bytes from one row to the next.
func (v *View) Read(dst []byte, stride int) error {
	if len(dst) < stride*v.h || stride < 4*v.w {
		return errors.New("zsview: the buffer is too small for the view")
	}
	return check("read", C.zs_view_read(v.v, (*C.uint8_t)(unsafe.Pointer(&dst[0])), C.size_t(len(dst)), C.size_t(stride)))
}

func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
