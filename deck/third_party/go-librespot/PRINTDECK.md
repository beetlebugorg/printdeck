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
| `mp3/decoder.go` | Pure Go: `github.com/hajimehoshi/go-mp3` in place of libmpg123, with the gapless trimming and the exact seek of libmpg123 |
| `mp3/decoder_ref_test.go`, `mp3/testdata/` | New: the comparison with the libmpg123 decoder |
| `flac/decoder.go` | A stub: this build cannot play FLAC (it needs the PlayPlay plugin, see `daemon/app.go`) |
| `output/output.go` | New: `RegisterBackend`, for the ALSA backend of the deck |
| `output/driver-alsa.go`, `output/mixer-alsa.go`, `output/driver-alsa-stub.go` | The cgo ALSA driver builds only with cgo |

The decoders keep the behavior of the C versions, also where it is surprising. For example, the
Vorbis decoder does not decode the last partial 4096-byte chunk of a stream, as upstream.

Known differences: the MP3 decoder gives 16-bit samples (libmpg123 gives float; the difference is
below 6e-5), it uses about 10 times the CPU time of libmpg123, and it cannot play MPEG 2.5.

`tools/librespot-reference` makes the reference data with the C decoders of upstream.
