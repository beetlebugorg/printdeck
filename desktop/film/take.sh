#!/bin/sh
# Records a take of the film of the Printdeck, with the parts that film.sh made in OUT. It builds
# the deck and the desktop app, starts the simulator on its own, and starts the print of the
# plaque at the layer of the letters (resume.py). Then it opens the 3D window in film mode, records
# the take, adds the steppers to the soundtrack (steppers.py), and encodes OUT/printdeck-film.mp4.
#
#   desktop/film/film.sh OUT        (once: the plaque, the music, the voice, and the cards)
#   desktop/film/take.sh OUT [FROM TO FPS]
#
# FROM TO FPS are the seconds of the film and the frames a second (0 80 30). "0 80 1" makes a
# fast preview. It needs Go, CMake, Ninja, Qt 6, uv, and ffmpeg.
set -eu
FILM="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$FILM/../.." && pwd)"
OUT="$(cd "$1" && pwd)"
shift
RANGE="${*:-0 80 30}"
SIM="$HOME/.config/Printdeck Sim"
LAYER=8 # the first layer of the letters

(cd "$REPO/deck" && go build -o "$OUT/bin/printdeck" ./cmd/printdeck)
cmake -S "$REPO/desktop" -B "$OUT/window" -G Ninja -DCMAKE_BUILD_TYPE=Release > /dev/null
ninja -C "$OUT/window" > /dev/null

# The simulator runs on its own, so that the window can attach to it (hello.txt).
"$OUT/bin/printdeck" app -sim > "$OUT/hello.txt" 2> "$OUT/app.log" &
APP=$!
trap 'kill $APP ${WINDOW:-} 2> /dev/null || true' EXIT
while [ ! -S "$SIM/klippy.sock" ] || [ ! -s "$OUT/hello.txt" ]; do sleep 1; done
sleep 5
mkdir -p "$SIM/gcodes"
cp "$OUT/printdeck-plaque.gcode" "$SIM/gcodes/"
python3 "$FILM/resume.py" "$OUT/printdeck-plaque.gcode" "$LAYER" "$SIM/klippy.sock"

QT_FORCE_STDERR_LOGGING=1 PRINTDECK_FILM="$OUT/take.mp4" PRINTDECK_ATTACH="$OUT/hello.txt" \
  "$OUT/window/Printdeck" -sim > "$OUT/window.log" 2>&1 &
WINDOW=$!
sleep 20
rm -f "$OUT/take.mp4"
echo "$RANGE" > "$OUT/take.mp4.go"
while [ -f "$OUT/take.mp4.go" ]; do sleep 2; done

uv run --no-project --with numpy python "$FILM/steppers.py" "$OUT/take.mp4.motion.csv" \
  "$OUT/soundtrack.wav" "$OUT/soundtrack-steppers.wav"
ffmpeg -loglevel error -y -i "$OUT/take.mp4" -i "$OUT/soundtrack-steppers.wav" -map 0:v -map 1:a \
  -vf noise=alls=2:allf=t -c:v libx264 -preset slow -crf 21 -pix_fmt yuv420p \
  -c:a aac -b:a 192k -shortest -movflags +faststart "$OUT/printdeck-film.mp4"
echo "wrote $OUT/printdeck-film.mp4"
