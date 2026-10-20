#!/bin/sh
# Makes the reference data of the pure Go audio decoders in deck/third_party/go-librespot: test
# files from the real encoders, and their samples from the C decoders of upstream go-librespot.
#
#   tools/librespot-reference/make-reference.sh
set -eu

REPO=$(cd "$(dirname "$0")/../.." && pwd)
TOOL=$REPO/tools/librespot-reference
VTEST=$REPO/deck/third_party/go-librespot/vorbis/testdata
MTEST=$REPO/deck/third_party/go-librespot/mp3/testdata
mkdir -p "$VTEST" "$MTEST"
docker build -q -t printdeck-librespot-ref "$TOOL" > /dev/null
run() {
  docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
    -v "$TOOL:/tool" -v "$VTEST:/v" -v "$MTEST:/m" -w /tool printdeck-librespot-ref sh -c "$1"
}

# The test files: music-like signals (a chord with a sweep and noise) at the bit rates and the
# rates of Spotify, a mono file, and a file shorter than one page.
run '
set -e
gen() { ffmpeg -loglevel error -y -f lavfi -i "$1" -t "$2" -ac "$3" -ar "$4" -c:a libvorbis -b:a "$5" "/v/$6.ogg"; }
SIG="aevalsrc=0.3*sin(2*PI*220*t)+0.2*sin(2*PI*277*t)+0.2*sin(2*PI*(330+200*t)*t)+0.05*(random(0)-0.5)|0.3*sin(2*PI*330*t)+0.2*sin(2*PI*440*t)+0.1*(random(1)-0.5):s=44100"
gen "$SIG" 5 2 44100 96k stereo96
gen "$SIG" 5 2 44100 160k stereo160
gen "$SIG" 5 2 44100 320k stereo320
gen "sine=f=1000:sample_rate=22050" 3 1 22050 64k mono22k
gen "$SIG" 0.05 2 44100 160k short
go mod tidy >/dev/null 2>&1
for f in stereo96 stereo160 stereo320 mono22k short; do go run . vorbis /v/$f.ogg /v/$f; done
'

# The MP3 test files: LAME at the bit rates of Spotify, stereo and mono, and the fixture of
# upstream (sine_mono.mp3). libmpg123 decodes them gapless, with the LAME tag.
run '
set -e
gen() { ffmpeg -loglevel error -y -f lavfi -i "$1" -t "$2" -ac "$3" -ar "$4" -c:a libmp3lame -b:a "$5" "/m/$6.mp3"; }
SIG="aevalsrc=0.3*sin(2*PI*220*t)+0.2*sin(2*PI*277*t)+0.2*sin(2*PI*(330+200*t)*t)+0.05*(random(0)-0.5)|0.3*sin(2*PI*330*t)+0.2*sin(2*PI*440*t)+0.1*(random(1)-0.5):s=44100"
gen "$SIG" 3 2 44100 160k stereo160
gen "$SIG" 3 2 44100 320k stereo320
gen "$SIG" 3 1 44100 96k mono96
gen "sine=f=440:sample_rate=22050" 2 1 22050 64k mono22k
for f in stereo160 stereo320 mono96 mono22k sine_mono; do
  go run . mp3 /m/$f.mp3 /m/$f
  go run . mp3seek /m/$f.mp3 /m/$f.seek
done
'
