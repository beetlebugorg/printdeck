package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/gpujpeg"
	"printdeck/internal/jpegdec"
	"printdeck/internal/turbojpeg"
)

// benchGPU is the GPU of the measurement, opened at the first measurement.
var benchGPU = sync.OnceValues(gpujpeg.Open)

// sysCameras are the camera hubs of the printers, for the measurement of the JPEG decode.
var sysCameras []*camera.Hub

// serveJPEGBench measures the decode and the encode of the JPEG code of the deck on a frame of
// the main camera (GET /system/jpeg, text): the decode at each scale and with 1, 2 and 4
// threads, the decode of a crop, the encode of the frame at half size, and the same with
// libjpeg-turbo when the deck has it. With ?gpu=1 it
// also measures the GPU: the first use compiles the shader in the driver. It uses all the
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
	// Each line goes out at once, and the measurement stops when the heap grows past heapStop.
	flush := func() {
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}
	const heapStop = 1 << 30
	tooLarge := func() bool {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.HeapAlloc > heapStop {
			fmt.Fprintf(w, "stop: the heap is %d MB\n", m.HeapAlloc>>20)
			return true
		}
		fmt.Fprintf(w, "  (heap %d MB)\n", m.HeapAlloc>>20)
		flush()
		return false
	}
	fmt.Fprintf(w, "frame %dx%d, %d KB, %d cores\n", sz.X, sz.Y, len(b)/1024, runtime.NumCPU())
	flush()
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
			if tooLarge() {
				return
			}
		}
	}
	// libjpeg-turbo on the same frame: one thread, with the SIMD code of the CPU.
	if turbojpeg.Available {
		for shift := range 4 {
			med, low, err := timeOf(7, func() error { _, err := turbojpeg.Decode(b, shift); return err })
			if err != nil {
				fmt.Fprintf(w, "libjpeg-turbo decode: %v\n", err)
				break
			}
			fmt.Fprintf(w, "libjpeg-turbo decode 1/%d:     median %6.1f ms, best %6.1f ms\n", 1<<shift, med, low)
		}
		if p, err := turbojpeg.Decode(b, 1); err == nil {
			med, low, _ := timeOf(7, func() error { _, err := turbojpeg.Encode(p, 90); return err })
			fmt.Fprintf(w, "libjpeg-turbo encode %dx%d q90: median %6.1f ms, best %6.1f ms\n", p.Rect.Dx(), p.Rect.Dy(), med, low)
		}
		if tooLarge() {
			return
		}
	}
	// The Huffman decode once, then the pixels from its coefficients on the CPU and on the GPU.
	for _, th := range []int{1, 4} {
		med, low, err := timeOf(7, func() error {
			s, err := jpegdec.DecodeSparseThreads(b, th)
			if err == nil {
				s.Release()
			}
			return err
		})
		if err != nil {
			fmt.Fprintf(w, "sparse: %v\n", err)
			break
		}
		fmt.Fprintf(w, "huffman only     %d threads: median %6.1f ms, best %6.1f ms\n", th, med, low)
		if tooLarge() {
			return
		}
	}
	if s, err := jpegdec.DecodeSparse(b); err == nil {
		med, low, _ := timeOf(7, func() error { _, err := s.Render(0, image.Rectangle{}); return err })
		fmt.Fprintf(w, "pixels on CPU    whole:     median %6.1f ms, best %6.1f ms\n", med, low)
		med, low, _ = timeOf(7, func() error { _, err := s.Render(1, image.Rectangle{}); return err })
		fmt.Fprintf(w, "pixels on CPU    1/2:       median %6.1f ms, best %6.1f ms\n", med, low)
		if tooLarge() {
			s.Release()
			return
		}
		if r.URL.Query().Get("gpu") != "1" {
			fmt.Fprintf(w, "GPU: add ?gpu=1 to measure it\n")
		} else if g, err := benchGPU(); err != nil {
			fmt.Fprintf(w, "GPU: %v\n", err)
		} else if in, err := s.GPUInput(); err != nil {
			fmt.Fprintf(w, "GPU input: %v\n", err)
		} else {
			med, low, _ = timeOf(7, func() error { _, err := s.GPUInput(); return err })
			fmt.Fprintf(w, "GPU input table  (CPU):     median %6.1f ms, best %6.1f ms\n", med, low)
			if tooLarge() {
				s.Release()
				return
			}
			med, low, err := timeOf(7, func() error { _, err := g.Render(in); return err })
			if err != nil {
				fmt.Fprintf(w, "GPU render: %v\n", err)
			} else {
				fmt.Fprintf(w, "pixels on GPU    whole:     median %6.1f ms, best %6.1f ms (%s)\n", med, low, g.Name)
				got, _ := g.Render(in)
				want, _ := s.Render(0, image.Rectangle{})
				fmt.Fprintf(w, "GPU pixels same as CPU: %v\n", samePictures(got, want))
			}
		}
		s.Release()
	}
	// The encode of the frame at half size (4:2:2), the picture of the libjpeg-turbo encode too.
	half, _ := jpegdec.DecodeScaled(bytes.NewReader(b), 1)
	if p, ok := half.(*image.YCbCr); ok {
		for _, th := range []int{1, 2, 4} {
			med, low, _ := timeOf(7, func() error {
				_, err := jpegdec.EncodeThreads(p, 90, th)
				return err
			})
			fmt.Fprintf(w, "encode %dx%d q90  %d threads: median %6.1f ms, best %6.1f ms\n", p.Rect.Dx(), p.Rect.Dy(), th, med, low)
		}
	}
}

// samePictures reports that two pictures have the same pixels.
func samePictures(a, b image.Image) bool {
	if a == nil || b == nil || a.Bounds() != b.Bounds() {
		return false
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}
