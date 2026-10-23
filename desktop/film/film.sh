#!/bin/sh
# Makes the parts of the film of the Printdeck: the G-code of the plaque that the simulator prints
# during the film, the music, the soundtrack with the announcer, and the title cards. take.sh
# records the film with the 3D window of the desktop app.
#
#   desktop/film/film.sh OUT        (OUT is a folder; the voice model goes to OUT/voices)
#
# It needs uv (build123d, numpy, kokoro-onnx), Go, CuraEngine in /opt/ultimaker-cura, and ffmpeg.
set -eu
FILM="$(cd "$(dirname "$0")" && pwd)"
OUT="$(mkdir -p "$1" && cd "$1" && pwd)"

uv run --no-project --with build123d python "$FILM/plaque.py" "$OUT/plaque.stl"
python3 "$FILM/slice.py" "$OUT/plaque.stl" "$OUT/printdeck-plaque.gcode"
uv run --no-project --with numpy python "$FILM/music.py" "$OUT/music.wav"
(cd "$FILM/../../deck" && go run ./cmd/filmcards "$FILM/cards.json" "$OUT/cards")
for f in kokoro-v1.0.onnx voices-v1.0.bin; do
  [ -f "$OUT/voices/$f" ] || curl -sSL --create-dirs -o "$OUT/voices/$f" \
    "https://github.com/thewh1teagle/kokoro-onnx/releases/download/model-files-v1.0/$f"
done
uv run --no-project --with kokoro-onnx --with numpy python "$FILM/voice.py" \
  "$OUT/voices/kokoro-v1.0.onnx" "$OUT/voices/voices-v1.0.bin" bm_george "$OUT/music.wav" "$OUT/soundtrack.wav"

echo "The parts are in $OUT. To record the film: desktop/film/take.sh $OUT"
