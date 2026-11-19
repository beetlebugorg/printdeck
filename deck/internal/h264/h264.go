//go:build h264

// Package h264 decodes H.264 video with libavcodec, for the cameras that send H.264: the camera
// of a Bambu Lab printer. The deck links a libavcodec with only the H.264 decoder
// (os/configs/printdeck_defconfig).
//
// The package decodes only with the tag h264. Without it, New returns ErrNoDecoder.
package h264

/*
#cgo LDFLAGS: -lavcodec -lavutil
#include <stdlib.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavutil/frame.h>

// dec_open opens the H.264 decoder with threads.
static AVCodecContext *dec_open(int threads) {
	const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_H264);
	if (!codec) return NULL;
	AVCodecContext *ctx = avcodec_alloc_context3(codec);
	if (!ctx) return NULL;
	ctx->thread_count = threads;
	if (avcodec_open2(ctx, codec, NULL) < 0) {
		avcodec_free_context(&ctx);
		return NULL;
	}
	return ctx;
}

// dec_send sends the access unit data to the decoder. pkt has the buffer.
static int dec_send(AVCodecContext *ctx, AVPacket *pkt, const unsigned char *data, int n) {
	if (av_new_packet(pkt, n) < 0) return -1;
	memcpy(pkt->data, data, n);
	int r = avcodec_send_packet(ctx, pkt);
	av_packet_unref(pkt);
	return r;
}

static int eagain(int r) { return r == AVERROR(EAGAIN); }

static void dec_close(AVCodecContext *ctx, AVPacket *pkt, AVFrame *f) {
	av_frame_free(&f);
	av_packet_free(&pkt);
	avcodec_free_context(&ctx);
}

// The planes of a frame, for Go.
static unsigned char *plane(AVFrame *f, int i) { return f->data[i]; }
static int stride(AVFrame *f, int i) { return f->linesize[i]; }
static int is420(AVFrame *f) { return f->format == AV_PIX_FMT_YUV420P || f->format == AV_PIX_FMT_YUVJ420P; }
*/
import "C"

import (
	"errors"
	"image"
	"unsafe"
)

// Decoder is one H.264 stream. It is not safe for use by more than one goroutine.
type Decoder struct {
	ctx *C.AVCodecContext
	pkt *C.AVPacket
	f   *C.AVFrame
}

// New opens a decoder that uses threads threads (0 for the count of libavcodec).
func New(threads int) (*Decoder, error) {
	ctx := C.dec_open(C.int(threads))
	if ctx == nil {
		return nil, errors.New("h264: libavcodec has no H.264 decoder")
	}
	return &Decoder{ctx: ctx, pkt: C.av_packet_alloc(), f: C.av_frame_alloc()}, nil
}

// Close frees the decoder.
func (d *Decoder) Close() {
	if d.ctx != nil {
		C.dec_close(d.ctx, d.pkt, d.f)
		d.ctx = nil
	}
}

// Decode decodes one access unit in Annex B form. It returns the pictures that the decoder has
// ready: often one, none at the start of a stream with frame threads.
func (d *Decoder) Decode(au []byte) ([]*image.YCbCr, error) {
	if len(au) == 0 {
		return nil, nil
	}
	if r := C.dec_send(d.ctx, d.pkt, (*C.uchar)(unsafe.Pointer(&au[0])), C.int(len(au))); r < 0 && C.eagain(r) == 0 {
		return nil, errors.New("h264: the decoder refused the data")
	}
	var out []*image.YCbCr
	for C.avcodec_receive_frame(d.ctx, d.f) == 0 {
		if p := d.picture(); p != nil {
			out = append(out, p)
		}
		C.av_frame_unref(d.f)
	}
	return out, nil
}

// picture copies the 4:2:0 frame of the decoder to a picture, or returns nil for another format.
func (d *Decoder) picture() *image.YCbCr {
	f := d.f
	if C.is420(f) == 0 {
		return nil
	}
	w, h := int(f.width), int(f.height)
	p := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	copyPlane(p.Y, p.YStride, C.plane(f, 0), int(C.stride(f, 0)), w, h)
	cw, ch := (w+1)/2, (h+1)/2
	copyPlane(p.Cb, p.CStride, C.plane(f, 1), int(C.stride(f, 1)), cw, ch)
	copyPlane(p.Cr, p.CStride, C.plane(f, 2), int(C.stride(f, 2)), cw, ch)
	return p
}

func copyPlane(dst []byte, ds int, src *C.uchar, ss, w, h int) {
	s := unsafe.Slice((*byte)(unsafe.Pointer(src)), ss*(h-1)+w)
	for y := 0; y < h; y++ {
		copy(dst[y*ds:y*ds+w], s[y*ss:y*ss+w])
	}
}
