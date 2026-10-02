#!/usr/bin/env bash
# Render the printable parts in stl/, the step images, and the build PDF.
# Run this script again after you change a value in speakdeck.scad.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p stl build/img build/mesh preview
rm -f build/img/*.png build/mesh/*.stl

# Render at double size and then make the image smaller. This gives smooth edges.
finish() {
  magick "$1" -fuzz 2% -fill white -opaque "#f8f8f8" -resize 50% \
    -fuzz 1% -trim +repage -bordercolor white -border 24 "$1"
}

# Solid meshes of the printed parts, in their position in the case.
for p in body frame top_frame rear sled tray; do
  openscad -D "part=\"raw_$p\"" -o "build/mesh/$p.stl" speakdeck.scad
done

for p in body frame top_frame rear sled tray cap pad; do
  openscad -D mesh=true -D "part=\"$p\"" --export-format binstl -o "stl/$p.stl" speakdeck.scad
  openscad -D mesh=true -D "part=\"$p\"" -o "build/img/part_$p.png" --imgsize=1400,1200 --colorscheme=Tomorrow \
    --projection=o --autocenter --viewall --camera=0,0,0,55,0,25,500 speakdeck.scad
  finish "build/img/part_$p.png"
done

# shot <step> <camera rotation> [rear]
shot() {
  local rear=false
  [ "${3:-}" = "rear" ] && rear=true
  openscad -D mesh=true -D 'part="step"' -D "step=$1" -D "view_rear=$rear" -o "build/img/step$1.png" \
    --imgsize=3200,2600 --colorscheme=Tomorrow --projection=o --autocenter --viewall \
    --camera="0,0,0,$2,500" speakdeck.scad
  finish "build/img/step$1.png"
}
shot 1 62,0,25 rear
shot 2 50,0,205
shot 3 66,0,35
shot 4 62,0,30 rear
shot 5 52,0,35
shot 6 55,0,205
shot 7 62,0,30 rear
shot 8 60,0,205
shot 9 62,0,30 rear
shot 10 68,0,35 rear
shot 12 62,0,30 rear
shot 13 72,0,20

openscad -D mesh=true -D 'part="assembly"' -o preview/front.png --imgsize=2000,1800 --colorscheme=Tomorrow --autocenter --viewall \
  --camera=0,0,0,65,0,30,500 speakdeck.scad
openscad -D mesh=true -D 'part="assembly"' -D view_rear=true -o preview/back.png --imgsize=2000,1800 --colorscheme=Tomorrow \
  --autocenter --viewall --camera=0,0,0,65,0,30,500 speakdeck.scad
openscad -D mesh=true -o preview/exploded.png --imgsize=2800,2000 --colorscheme=Tomorrow --autocenter --viewall \
  --camera=0,0,0,62,0,40,500 speakdeck.scad
finish preview/exploded.png
finish preview/front.png
finish preview/back.png

python3 build/filament.py
python3 build/make_html.py
chromium --headless --disable-gpu --no-pdf-header-footer \
  --print-to-pdf="$PWD/speakdeck-build.pdf" "file://$PWD/build/build.html"
echo "Wrote speakdeck-build.pdf"
