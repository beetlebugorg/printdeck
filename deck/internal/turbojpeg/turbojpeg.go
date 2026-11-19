//go:build turbojpeg

// Package turbojpeg decodes and encodes JPEG with libjpeg-turbo (the TurboJPEG 3 API), for a
// comparison with jpegdec (/system/jpeg). It decodes to Y'CbCr planes at 1/1, 1/2, 1/4 or 1/8 of
// the size, as jpegdec does, so the two do the same work.
//
// The package builds only with the tag turbojpeg, and links libturbojpeg.
package turbojpeg

/*
#cgo LDFLAGS: -lturbojpeg
#include <stdlib.h>
#include <turbojpeg.h>

// td_init makes a handle (tj3Init can be a macro).
static tjhandle td_init(int compress) { return tj3Init(compress ? TJINIT_COMPRESS : TJINIT_DECOMPRESS); }

// td_decode decodes the JPEG jpeg at num/denom of its size into the planes y, cb and cr with
// the strides ys and cs. With y NULL, it only reads the header into w, h and subsamp.
static int td_decode(tjhandle t, const unsigned char *jpeg, size_t n, int num, int denom,
		unsigned char *y, unsigned char *cb, unsigned char *cr, int ys, int cs,
		int *w, int *h, int *subsamp) {
	if (tj3DecompressHeader(t, jpeg, n) != 0) return -1;
	*w = tj3Get(t, TJPARAM_JPEGWIDTH);
	*h = tj3Get(t, TJPARAM_JPEGHEIGHT);
	*subsamp = tj3Get(t, TJPARAM_SUBSAMP);
	tjscalingfactor sf = {num, denom};
	if (tj3SetScalingFactor(t, sf) != 0) return -2;
	if (!y) return 0;
	unsigned char *planes[3] = {y, cb, cr};
	int strides[3] = {ys, cs, cs};
	return tj3DecompressToYUVPlanes8(t, jpeg, n, planes, strides) == 0 ? 0 : -3;
}

// td_encode encodes the planes as a JPEG of the quality. *out is for tj3Free.
static int td_encode(tjhandle t, const unsigned char *y, const unsigned char *cb, const unsigned char *cr,
		int w, int ys, int cs, int h, int subsamp, int quality, unsigned char **out, size_t *n) {
	tj3Set(t, TJPARAM_SUBSAMP, subsamp);
	tj3Set(t, TJPARAM_QUALITY, quality);
	const unsigned char *planes[3] = {y, cb, cr};
	int strides[3] = {ys, cs, cs};
	*out = NULL;
	*n = 0;
	return tj3CompressFromYUVPlanes8(t, planes, w, strides, h, out, n) == 0 ? 0 : -1;
}
*/
import "C"

import (
	"errors"
	"image"
	"sync"
	"unsafe"
)

// Available reports that the deck has libjpeg-turbo.
const Available = true

var decoders = sync.Pool{New: func() any { return C.td_init(0) }}
var encoders = sync.Pool{New: func() any { return C.td_init(1) }}

func ratioOf(subsamp C.int) (image.YCbCrSubsampleRatio, bool) {
	switch subsamp {
	case C.TJSAMP_444:
		return image.YCbCrSubsampleRatio444, true
	case C.TJSAMP_422:
		return image.YCbCrSubsampleRatio422, true
	case C.TJSAMP_420:
		return image.YCbCrSubsampleRatio420, true
	case C.TJSAMP_440:
		return image.YCbCrSubsampleRatio440, true
	case C.TJSAMP_411:
		return image.YCbCrSubsampleRatio411, true
	}
	return 0, false
}

// Decode decodes the JPEG b at 1/2^shift of its size (shift 0 to 3).
func Decode(b []byte, shift int) (*image.YCbCr, error) {
	if len(b) == 0 {
		return nil, errors.New("turbojpeg: no data")
	}
	t := decoders.Get().(C.tjhandle)
	defer decoders.Put(t)
	jp := (*C.uchar)(unsafe.Pointer(&b[0]))
	var w, h, ss C.int
	denom := C.int(1) << max(0, min(3, shift))
	if C.td_decode(t, jp, C.size_t(len(b)), 1, denom, nil, nil, nil, 0, 0, &w, &h, &ss) != 0 {
		return nil, errors.New("turbojpeg: the header")
	}
	ratio, ok := ratioOf(ss)
	if !ok {
		return nil, errors.New("turbojpeg: the subsampling")
	}
	sw, sh := (int(w)+int(denom)-1)/int(denom), (int(h)+int(denom)-1)/int(denom)
	// libjpeg-turbo writes whole chroma samples: the planes have an even width and height.
	full := image.NewYCbCr(image.Rect(0, 0, (sw+3)&^3, (sh+1)&^1), ratio)
	p := full.SubImage(image.Rect(0, 0, sw, sh)).(*image.YCbCr)
	if C.td_decode(t, jp, C.size_t(len(b)), 1, denom, (*C.uchar)(&p.Y[0]), (*C.uchar)(&p.Cb[0]), (*C.uchar)(&p.Cr[0]),
		C.int(p.YStride), C.int(p.CStride), &w, &h, &ss) != 0 {
		return nil, errors.New("turbojpeg: the decode: " + C.GoString(C.tj3GetErrorStr(t)))
	}
	return p, nil
}

// Encode encodes p as a JPEG of the quality.
func Encode(p *image.YCbCr, quality int) ([]byte, error) {
	var ss C.int
	switch p.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
		ss = C.TJSAMP_444
	case image.YCbCrSubsampleRatio422:
		ss = C.TJSAMP_422
	case image.YCbCrSubsampleRatio420:
		ss = C.TJSAMP_420
	default:
		return nil, errors.New("turbojpeg: the subsampling")
	}
	t := encoders.Get().(C.tjhandle)
	defer encoders.Put(t)
	y := p.Y[p.YOffset(p.Rect.Min.X, p.Rect.Min.Y):]
	cb := p.Cb[p.COffset(p.Rect.Min.X, p.Rect.Min.Y):]
	cr := p.Cr[p.COffset(p.Rect.Min.X, p.Rect.Min.Y):]
	var out *C.uchar
	var n C.size_t
	if C.td_encode(t, (*C.uchar)(&y[0]), (*C.uchar)(&cb[0]), (*C.uchar)(&cr[0]), C.int(p.Rect.Dx()), C.int(p.YStride),
		C.int(p.CStride), C.int(p.Rect.Dy()), ss, C.int(quality), &out, &n) != 0 {
		return nil, errors.New("turbojpeg: the encode: " + C.GoString(C.tj3GetErrorStr(t)))
	}
	defer C.tj3Free(unsafe.Pointer(out))
	return C.GoBytes(unsafe.Pointer(out), C.int(n)), nil
}
