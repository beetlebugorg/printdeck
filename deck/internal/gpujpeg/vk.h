// The Vulkan compute code of package gpujpeg: one pipeline for the inverse DCT of the blocks of
// a JPEG (idct.comp), and host-visible buffers that grow when a picture needs more.
#ifndef GPUJPEG_VK_H
#define GPUJPEG_VK_H
#include <stddef.h>
#include <stdint.h>

typedef struct gj_ctx gj_ctx;

// gj_init makes the context on the first GPU with a compute queue. It returns 0, or an error code
// below 0. name gets the name of the device.
int gj_init(const uint32_t *spv, size_t spv_len, gj_ctx **out, char *name, size_t name_len);
void gj_free(gj_ctx *c);
// gj_reserve makes the buffers large enough for n_blocks blocks, n_words words and out_len bytes.
int gj_reserve(gj_ctx *c, size_t n_blocks, size_t n_words, size_t out_len);
void gj_put_blocks(gj_ctx *c, const uint32_t *blocks, size_t n_blocks);
void gj_put_words(gj_ctx *c, size_t at, const uint32_t *words, size_t n);
// gj_put_quant copies the 4 quantization tables and the natural index of each zig-zag index
// (5 x 64 values).
void gj_put_quant(gj_ctx *c, const int32_t *quant);
// gj_run runs the shader for n_blocks blocks and waits for the end.
int gj_run(gj_ctx *c, uint32_t n_blocks, const uint32_t strides[3]);
// gj_out returns the output buffer (out_len bytes of gj_reserve).
const uint8_t *gj_out(gj_ctx *c);
#endif
