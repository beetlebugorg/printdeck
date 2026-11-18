//go:build zslice

package main

import (
	"sync"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/ui"
	"printdeck/internal/zsview"
)

// The GPU work of the deck runs on one goroutine with its thread locked: the Vulkan driver can keep
// state on the thread. Each job runs there, one at a time.
var (
	zsJobs     chan func()
	zsJobsOnce sync.Once
)

// onGPU runs f on the GPU thread, and waits for it, at most 5 s. It reports false when f did not
// run.
func onGPU(f func()) bool {
	zsJobsOnce.Do(func() {
		zsJobs = make(chan func())
		go func() {
			runtime.LockOSThread()
			for job := range zsJobs {
				job()
			}
		}()
	})
	done := make(chan struct{})
	select {
	case zsJobs <- func() { f(); close(done) }:
	case <-time.After(5 * time.Second):
		return false
	}
	<-done
	return true
}

// zsFile is a G-code file of a view, read once.
type zsFile struct {
	path string
	g    *zsview.Gcode
}

// open reads the file at path when it is not the file of f. It reports false when the file does
// not read.
func (f *zsFile) open(path string) bool {
	if f.path == path && f.g != nil {
		return true
	}
	if f.g != nil {
		f.g.Close()
	}
	f.path, f.g = "", nil
	g, err := zsview.OpenGcode(path)
	if err != nil {
		log.Printf("zslice view: %s: %v", path, err)
		return false
	}
	f.path, f.g = path, g
	return true
}

// zsliceRenderer draws the picture of a print with the print view of zslice and the GPU
// (printer.LiveRenderer): the real beads of the G-code, on the dark window of the display, in
// the colour of the roll.
type zsliceRenderer struct {
	dir    string // the G-code folder of the printer
	v      *zsview.View
	file   zsFile
	loaded string
	hex    string
}

// startZsliceView returns the zslice renderer of the printer with the G-code folder dir, or nil
// when the deck has no zslice or no Vulkan: the deck then draws its own picture.
func startZsliceView(dir string) printer.LiveRenderer {
	z := &zsliceRenderer{dir: dir, hex: "-"}
	var err error
	if !onGPU(func() { z.v, err = zsview.NewView(printer.PreviewW, printer.PreviewH, false) }) || err != nil {
		log.Printf("zslice view: %v: the deck draws its own picture", err)
		return nil
	}
	onGPU(func() { z.v.SetDark(true) })
	return z
}

func (z *zsliceRenderer) Draw(path string, pos int64, layer int, hex string) (image.Image, bool) {
	var img *image.RGBA
	onGPU(func() {
		full := filepath.Join(z.dir, path)
		if !z.file.open(full) {
			return
		}
		if z.loaded != full {
			if err := z.v.Load(z.file.g, 0, 0); err != nil {
				log.Printf("zslice view: %s: %v", path, err)
				return
			}
			z.loaded, z.hex = full, "-"
		}
		if hex != z.hex {
			if rgb, ok := hexRGB(hex); ok {
				z.v.Colors(false, rgb)
			} else {
				z.v.Colors(true, 0) // no roll colour: a colour for each feature
			}
			z.hex = hex
		}
		if layer > 0 {
			z.v.SetLayer(layer - 1)
		} else {
			z.v.SetPosition(uint64(max(0, pos)))
		}
		img = drawView(z.v)
	})
	return img, img != nil
}

// drawView draws a frame of v, and returns its pixels, or nil.
func drawView(v *zsview.View) *image.RGBA {
	w, h := v.Size()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	err := v.Draw()
	if err == nil {
		err = v.Read(img.Pix, img.Stride)
	}
	if err != nil {
		log.Printf("zslice view: draw: %v", err)
		return nil
	}
	return img
}

// zsliceViewer is the 3D view of a finished print that the finger turns (ui.ModelViewer): the
// last print of the home screen.
type zsliceViewer struct {
	dir    string
	v      *zsview.View
	file   zsFile
	loaded string
	layers int
}

// startZsliceViewer returns the viewer of the files of the printer with the G-code folder dir, or
// nil with no zslice or no Vulkan.
func startZsliceViewer(dir string) ui.ModelViewer {
	z := &zsliceViewer{dir: dir}
	var err error
	if !onGPU(func() { z.v, err = zsview.NewView(320, 240, false) }) || err != nil {
		return nil
	}
	onGPU(func() { z.v.SetDark(true); z.v.Colors(true, 0) })
	return z
}

func (z *zsliceViewer) load(path string) bool {
	full := filepath.Join(z.dir, path)
	if !z.file.open(full) {
		return false
	}
	if z.loaded != full {
		if err := z.v.Load(z.file.g, 0, 0); err != nil {
			return false
		}
		in, _ := z.file.g.Info()
		z.loaded, z.layers = full, in.Layers
	}
	return true
}

func (z *zsliceViewer) Frame(path string, layer, w, h int, hex string) (image.Image, int, bool) {
	var img *image.RGBA
	layers := 0
	onGPU(func() {
		if !z.load(path) {
			return
		}
		if vw, vh := z.v.Size(); vw != w || vh != h {
			z.v.Resize(w, h)
		}
		if rgb, ok := hexRGB(hex); ok {
			z.v.Colors(false, rgb)
		} else {
			z.v.Colors(true, 0)
		}
		if layer <= 0 || layer > z.layers {
			layer = z.layers
		}
		z.v.SetLayer(layer - 1)
		img, layers = drawView(z.v), z.layers
	})
	return img, layers, img != nil
}

func (z *zsliceViewer) Orbit(phase int, x, y float32) {
	action := map[int]int{0: zsview.TouchDown, 1: zsview.TouchMove, 2: zsview.TouchUp}[phase]
	onGPU(func() { z.v.Touch(1, action, x, y) })
}

// Zoom zooms in (step > 0) or out with a pinch of two fingers in the middle of the view.
func (z *zsliceViewer) Zoom(step int) {
	onGPU(func() {
		w, h := z.v.Size()
		cx, cy := float32(w)/2, float32(h)/2
		d0, d1 := float32(40), float32(40*1.25)
		if step < 0 {
			d0, d1 = d1, d0
		}
		z.v.Touch(101, zsview.TouchDown, cx-d0, cy)
		z.v.Touch(102, zsview.TouchDown, cx+d0, cy)
		z.v.Touch(101, zsview.TouchMove, cx-d1, cy)
		z.v.Touch(102, zsview.TouchMove, cx+d1, cy)
		z.v.Touch(101, zsview.TouchUp, cx-d1, cy)
		z.v.Touch(102, zsview.TouchUp, cx+d1, cy)
	})
}

func (z *zsliceViewer) Reset() { onGPU(func() { z.v.Reset() }) }

// hexRGB reads "#rrggbb".
func hexRGB(hex string) (uint32, bool) {
	s := strings.TrimPrefix(hex, "#")
	if len(s) != 6 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	return uint32(v), err == nil
}

// gpuCheck draws a frame of the newest G-code file in dir with the print view of zslice, and
// returns what happened: for the system page, to check the GPU of a deck with no print.
func gpuCheck(dir string) map[string]any {
	out := map[string]any{"zslice": true}
	path := newestGcode(dir)
	if path == "" {
		out["error"] = "no G-code file on the deck"
		return out
	}
	out["file"] = filepath.Base(path)
	done := make(chan struct{})
	go onGPU(func() {
		defer close(done)
		t0 := time.Now()
		g, err := zsview.OpenGcode(path)
		if err != nil {
			out["error"] = err.Error()
			return
		}
		defer g.Close()
		out["read_ms"] = time.Since(t0).Milliseconds()
		in, _ := g.Info()
		out["layers"], out["beads"] = in.Layers, in.Beads
		t1 := time.Now()
		var v *zsview.View
		// The messages of the Vulkan loader too: the drivers that it finds, and why one fails.
		os.Setenv("VK_LOADER_DEBUG", "error,warn,driver")
		msgs := captureOutput(func() { v, err = zsview.NewView(printer.PreviewW, printer.PreviewH, false) })
		os.Unsetenv("VK_LOADER_DEBUG")
		if msgs != "" {
			out["messages"] = msgs // what zslice and the Vulkan driver wrote
		}
		if err != nil {
			out["error"] = err.Error()
			return
		}
		defer v.Close()
		out["view_ms"] = time.Since(t1).Milliseconds()
		if err := v.Load(g, 0, 0); err != nil {
			out["error"] = err.Error()
			return
		}
		buf := make([]byte, printer.PreviewW*printer.PreviewH*4)
		var frames []int64
		for i, f := range []float64{0.5, 0.51, 0.52} {
			v.SetPosition(uint64(float64(in.Size) * f))
			t := time.Now()
			if err := v.Draw(); err == nil {
				err = v.Read(buf, printer.PreviewW*4)
			}
			if err != nil {
				out["error"] = fmt.Sprintf("frame %d: %v", i, err)
				return
			}
			frames = append(frames, time.Since(t).Microseconds())
		}
		out["frame_us"] = frames
		out["ok"] = true
	})
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		out["error"] = "no answer in 60 s"
	}
	return out
}

// newestGcode returns the newest .gcode file in dir, or "".
func newestGcode(dir string) string {
	entries, _ := os.ReadDir(dir)
	best, bestT := "", time.Time{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gcode") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(bestT) {
			best, bestT = filepath.Join(dir, e.Name()), fi.ModTime()
		}
	}
	return best
}

// captureOutput runs f, and returns what the process wrote to its standard output and its
// standard error during f (at most 16 KB): the messages of zslice and of the Vulkan driver.
func captureOutput(f func()) string {
	r, w, err := os.Pipe()
	if err != nil {
		f()
		return ""
	}
	out1, _ := syscall.Dup(1)
	out2, _ := syscall.Dup(2)
	syscall.Dup3(int(w.Fd()), 1, 0)
	syscall.Dup3(int(w.Fd()), 2, 0)
	got := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(r, 16<<10))
		io.Copy(io.Discard, r)
		got <- string(b)
	}()
	f()
	syscall.Dup3(out1, 1, 0)
	syscall.Dup3(out2, 2, 0)
	syscall.Close(out1)
	syscall.Close(out2)
	w.Close()
	s := <-got
	r.Close()
	os.Stderr.WriteString(s) // the journal keeps the messages too
	return s
}
