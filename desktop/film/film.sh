#!/bin/sh
# Makes the parts of the film of the Printdeck: the G-code of the plaque that the simulator prints
# during the film, the music, and the soundtrack with the announcer. Then it shows how to record
# the film with the 3D window of the desktop app.
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

cat <<EOF
The parts are in $OUT. To record the film:

1. Run the simulator on its own, so that a new window does not stop its print:
     printdeck app -sim > $OUT/hello.txt
2. Copy $OUT/printdeck-plaque.gcode into the gcodes folder of the simulator, and print it.
3. Open the 3D window in film mode, on that simulator:
     PRINTDECK_FILM=$OUT/take.mp4 PRINTDECK_ATTACH=$OUT/hello.txt Printdeck -sim
4. When the letters print (layer 8, about 30 minutes after the start), start the take:
     echo "0 60 30" > $OUT/take.mp4.go
   (FROM TO FPS: "0 60 1" makes a fast preview). The window deletes the file at the end.
5. Add the sound:
     ffmpeg -i $OUT/take.mp4 -i $OUT/soundtrack.wav -map 0:v -map 1:a -c:v copy -c:a aac -shortest $OUT/printdeck.mp4
EOF
