//go:build vulkan

// Package gpujpeg makes the pixels of a JPEG on the GPU: the inverse DCT of each block in a
// Vulkan compute shader (idct.comp), from the coefficients of the Huffman decode of jpegdec
// (jpegdec.Sparse). The pixels are the same as the pixels of jpegdec on the CPU.
//
// The package builds only with the tag vulkan, and links the Vulkan loader. Make idct.spv
// again after a change to idct.comp:
//
//	glslc --target-env=vulkan1.1 -O idct.comp -o idct.spv
package gpujpeg

/*
#cgo LDFLAGS: -lvulkan
#include <stdlib.h>
#include "vk.h"
*/
import "C"

import (
	_ "embed"
	"fmt"
	"image"
	"sync"
	"unsafe"

	"printdeck/internal/jpegdec"
)

//go:embed idct.spv
var spv []byte

// Available reports that the deck has the GPU code.
const Available = true

// GPU is a compute pipeline on the GPU. One picture runs at a time.
type GPU struct {
	mu   sync.Mutex
	c    *C.gj_ctx
	Name string // the name of the device
}

// Open opens the first GPU with a compute queue.
func Open() (*GPU, error) {
	code := C.CBytes(spv)
	defer C.free(code)
	var c *C.gj_ctx
	var name [256]C.char
	if rc := C.gj_init((*C.uint32_t)(code), C.size_t(len(spv)), &c, &name[0], C.size_t(len(name))); rc != 0 {
		return nil, fmt.Errorf("gpujpeg: no GPU (error %d)", int(rc))
	}
	return &GPU{c: c, Name: C.GoString(&name[0])}, nil
}

// Close frees the GPU.
func (g *GPU) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.c != nil {
		C.gj_free(g.c)
		g.c = nil
	}
}

// Render makes the picture of in on the GPU. The picture has its own memory.
func (g *GPU) Render(in *jpegdec.GPUInput) (image.Image, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.c == nil {
		return nil, fmt.Errorf("gpujpeg: closed")
	}
	nb := len(in.Blocks) / 4
	nw := 0
	for _, w := range in.Words {
		nw += len(w)
	}
	if rc := C.gj_reserve(g.c, C.size_t(nb), C.size_t(nw), C.size_t(in.OutLen)); rc != 0 {
		return nil, fmt.Errorf("gpujpeg: buffers (error %d)", int(rc))
	}
	if nb > 0 {
		C.gj_put_blocks(g.c, (*C.uint32_t)(unsafe.Pointer(&in.Blocks[0])), C.size_t(nb))
	}
	at := 0
	for _, w := range in.Words {
		if len(w) > 0 {
			C.gj_put_words(g.c, C.size_t(at), (*C.uint32_t)(unsafe.Pointer(&w[0])), C.size_t(len(w)))
		}
		at += len(w)
	}
	C.gj_put_quant(g.c, (*C.int32_t)(unsafe.Pointer(&in.Quant[0][0])))
	strides := [3]C.uint32_t{C.uint32_t(in.Strides[0]), C.uint32_t(in.Strides[1]), C.uint32_t(in.Strides[2])}
	if rc := C.gj_run(g.c, C.uint32_t(nb), &strides[0]); rc != 0 {
		return nil, fmt.Errorf("gpujpeg: run (error %d)", int(rc))
	}
	out := make([]byte, in.OutLen)
	copy(out, unsafe.Slice((*byte)(unsafe.Pointer(C.gj_out(g.c))), in.OutLen))
	return in.Picture(out), nil
}
