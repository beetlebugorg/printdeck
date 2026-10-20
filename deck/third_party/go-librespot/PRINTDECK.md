# go-librespot in Printdeck

This is go-librespot v0.10.5 (https://github.com/devgianlu/go-librespot, GPL-3.0, see LICENSE),
copied into the Printdeck repo. `deck/go.mod` replaces the module with this folder.

Printdeck builds one static program with no cgo. These files differ from upstream:

| File | Change |
|---|---|
| `vorbis/ogg.go` | New: the Ogg framing, a Go port of the parts of libogg that the decoder uses |
| `vorbis/decoder.go` | Pure Go: the Ogg framing of `ogg.go` and the Vorbis decoder of `github.com/jfreymuth/vorbis`, in place of libogg and libvorbis |
| `vorbis/metadata.go` | The Ogg framing of `ogg.go` in place of libogg |
| `vorbis/decoder_ref_test.go`, `vorbis/testdata/` | New: the comparison with the libvorbis decoder |

The decoders keep the behavior of the C versions, also where it is surprising. For example, the
Vorbis decoder does not decode the last partial 4096-byte chunk of a stream, as upstream.

`tools/librespot-reference` makes the reference data with the C decoders of upstream.
