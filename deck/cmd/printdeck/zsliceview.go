//go:build zslice

package main

import (
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
	"printdeck/internal/zsview"
)

// zsliceRenderer draws the picture of a print with the print view of zslice and the GPU
// (printer.LiveRenderer): the real beads of the G-code, on the dark window of the display, in
// the colour of the roll. One goroutine owns the Vulkan view, with its thread locked.
type zsliceRenderer struct {
	dir string // the G-code folder of the printer
	req chan zsliceReq
}

type zsliceReq struct {
	path  string
	pos   int64
	layer int
	hex   string
	out   chan image.Image
}

// startZsliceView returns the zslice renderer of the printer with the G-code folder dir, or nil
// when the deck has no zslice or no Vulkan: the deck then draws its own picture.
func startZsliceView(dir string) printer.LiveRenderer {
	z := &zsliceRenderer{dir: dir, req: make(chan zsliceReq)}
	ok := make(chan bool)
	go z.run(ok)
	if !<-ok {
		return nil
	}
	return z
}

func (z *zsliceRenderer) Draw(path string, pos int64, layer int, hex string) (image.Image, bool) {
	out := make(chan image.Image, 1)
	select {
	case z.req <- zsliceReq{path: path, pos: pos, layer: layer, hex: hex, out: out}:
	case <-time.After(2 * time.Second):
		return nil, false
	}
	img := <-out
	return img, img != nil
}

func (z *zsliceRenderer) run(started chan<- bool) {
	runtime.LockOSThread() // the Vulkan driver can keep state on the thread
	w, h := printer.PreviewW, printer.PreviewH
	v, err := zsview.NewView(w, h, false)
	if err != nil {
		log.Printf("zslice view: %v: the deck draws its own picture", err)
		started <- false
		return
	}
	v.SetDark(true)
	started <- true
	var g *zsview.Gcode
	cur, curHex := "", "-"
	for r := range z.req {
		if r.path != cur {
			if g != nil {
				g.Close()
				g = nil
			}
			cur = ""
			ng, err := zsview.OpenGcode(filepath.Join(z.dir, r.path))
			if err == nil {
				err = v.Load(ng, 0, 0)
			}
			if err != nil {
				log.Printf("zslice view: %s: %v", r.path, err)
				if ng != nil {
					ng.Close()
				}
				r.out <- nil
				continue
			}
			g, cur, curHex = ng, r.path, "-"
		}
		if r.hex != curHex {
			if rgb, ok := hexRGB(r.hex); ok {
				v.Colors(false, rgb)
			} else {
				v.Colors(true, 0) // no roll colour: a colour for each feature
			}
			curHex = r.hex
		}
		if r.layer > 0 {
			v.SetLayer(r.layer - 1)
		} else {
			v.SetPosition(uint64(max(0, r.pos)))
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if err := v.Draw(); err == nil {
			err = v.Read(img.Pix, img.Stride)
		}
		if err != nil {
			log.Printf("zslice view: draw: %v", err)
			r.out <- nil
			continue
		}
		r.out <- img
	}
}

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
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
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
	}()
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
