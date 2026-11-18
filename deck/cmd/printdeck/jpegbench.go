package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"
	"runtime"
	"sort"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/jpegdec"
)

// sysCameras are the camera hubs of the printers, for the measurement of the JPEG decode.
var sysCameras []*camera.Hub

// serveJPEGBench measures the decode and the encode of the JPEG code of the deck on a frame of
// the main camera (GET /system/jpeg, text): the decode at each scale and with 1, 2 and 4
// threads, the decode of a crop, and the encode of the frame at 1920 pixels. It uses all the
// cores for about a second, so it does not run while a printer prints.
func serveJPEGBench(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if sysPrinter != nil && sysPrinter.State().Busy() {
		http.Error(w, "a printer prints: the measurement waits for the end of the print", http.StatusConflict)
		return
	}
	var f *camera.Frame
	for _, h := range sysCameras {
		src, ok := h.Source(camera.RoleMain)
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		f, _ = src.Snapshot(ctx)
		cancel()
		if f != nil {
			break
		}
	}
	if f == nil || f.JPEG() == nil {
		http.Error(w, "no frame of a camera", http.StatusServiceUnavailable)
		return
	}
	b := f.JPEG()
	sz := f.Size()
	fmt.Fprintf(w, "frame %dx%d, %d KB, %d cores\n", sz.X, sz.Y, len(b)/1024, runtime.NumCPU())
	// time returns the median and the smallest time of n runs of fn, in ms.
	timeOf := func(n int, fn func() error) (float64, float64, error) {
		var ts []float64
		for range n {
			s := time.Now()
			if err := fn(); err != nil {
				return 0, 0, err
			}
			ts = append(ts, time.Since(s).Seconds()*1000)
		}
		sort.Float64s(ts)
		return ts[len(ts)/2], ts[0], nil
	}
	crop := image.Rect(sz.X*3/8, sz.Y*3/8, sz.X*5/8, sz.Y*5/8)
	for _, c := range []struct {
		name  string
		shift int
		rect  image.Rectangle
	}{
		{"whole", 0, image.Rectangle{}}, {"1/2", 1, image.Rectangle{}}, {"1/4", 2, image.Rectangle{}},
		{"1/8", 3, image.Rectangle{}}, {"crop 1/4 x 1/4", 0, crop},
	} {
		for _, th := range []int{1, 2, 4} {
			med, low, err := timeOf(7, func() error {
				_, err := jpegdec.DecodeThreads(bytes.NewReader(b), c.shift, c.rect, th)
				return err
			})
			if err != nil {
				fmt.Fprintf(w, "decode %s: %v\n", c.name, err)
				break
			}
			fmt.Fprintf(w, "decode %-15s %d threads: median %6.1f ms, best %6.1f ms\n", c.name, th, med, low)
		}
	}
	small := f.Small(1920)
	if p, ok := small.Picture().(*image.YCbCr); ok {
		for _, th := range []int{1, 2, 4} {
			med, low, _ := timeOf(7, func() error {
				_, err := jpegdec.EncodeThreads(p, 90, th)
				return err
			})
			fmt.Fprintf(w, "encode %dx%d q90  %d threads: median %6.1f ms, best %6.1f ms\n", p.Rect.Dx(), p.Rect.Dy(), th, med, low)
		}
	}
}
