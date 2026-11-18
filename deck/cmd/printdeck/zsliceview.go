//go:build zslice

package main

import (
	"image"
	"log"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
