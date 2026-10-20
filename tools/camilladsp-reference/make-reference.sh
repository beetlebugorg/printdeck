#!/bin/sh
# Makes the reference data of the Go equalizer (deck/internal/eq): the real CamillaDSP filters a test
# signal with the filters of docker/camilladsp/config.yml, at the gains of each case.
#
#   tools/camilladsp-reference/make-reference.sh
set -eu

REPO=$(cd "$(dirname "$0")/../.." && pwd)
# The CamillaDSP version that Printdeck used before the Go equalizer.
VERSION=4.1.3
CACHE=${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}/camilladsp-$VERSION
OUT=$REPO/deck/internal/eq/testdata
mkdir -p "$CACHE" "$OUT"
if [ ! -x "$CACHE/camilladsp" ]; then
  curl -fsSL "https://github.com/HEnquist/camilladsp/releases/download/v$VERSION/camilladsp-linux-amd64.tar.gz" \
    | tar -xz -C "$CACHE"
fi

# The input: 0.5 s of the stereo test signal of the Vorbis decoder tests (float32, interleaved).
head -c 176400 "$REPO/deck/third_party/go-librespot/vorbis/testdata/stereo160.f32" > "$OUT/input.f32"

# name, then the gains of bass, lowmid, mid, highmid, treble (dB). The preamp is minus the
# largest boost, as eq.Patch sets it.
while read -r name g1 g2 g3 g4 g5; do
  pre=$(printf '%s\n' 0 "$g1" "$g2" "$g3" "$g4" "$g5" | sort -g | tail -1)
  cat > "$CACHE/$name.yml" <<YML
devices:
  samplerate: 44100
  chunksize: 1024
  capture:
    type: RawFile
    channels: 2
    filename: "$OUT/input.f32"
    format: F32_LE
  playback:
    type: File
    channels: 2
    filename: "$OUT/$name.f32"
    format: F32_LE
filters:
  preamp:
    type: Gain
    parameters: {gain: -$pre, scale: dB}
  eq_bass:
    type: Biquad
    parameters: {type: Lowshelf, freq: 80, gain: $g1, slope: 6}
  eq_lowmid:
    type: Biquad
    parameters: {type: Peaking, freq: 250, gain: $g2, q: 1.0}
  eq_mid:
    type: Biquad
    parameters: {type: Peaking, freq: 1000, gain: $g3, q: 1.0}
  eq_highmid:
    type: Biquad
    parameters: {type: Peaking, freq: 3500, gain: $g4, q: 1.0}
  eq_treble:
    type: Biquad
    parameters: {type: Highshelf, freq: 10000, gain: $g5, slope: 6}
pipeline:
  - type: Filter
    channels: [0, 1]
    names: [preamp, eq_bass, eq_lowmid, eq_mid, eq_highmid, eq_treble]
YML
  "$CACHE/camilladsp" -l error "$CACHE/$name.yml"
  echo "$name $g1 $g2 $g3 $g4 $g5" >> "$OUT/cases.txt.new"
done <<CASES
flat 0 0 0 0 0
bass 6 2 0 0 0
loud 5 1 0 1 4
voice -3 -1 2 3 0
treble 0 0 0 2 5
max 12 12 12 12 12
min -12 -12 -12 -12 -12
mixed 12 -12 7.5 -4.5 12
CASES
mv "$OUT/cases.txt.new" "$OUT/cases.txt"
