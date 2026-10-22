#!/bin/sh
# Exports the parts of the test print as STL files, and a picture of the two parts.
set -e
cd "$(dirname "$0")"
for p in cube cross; do
    openscad -q -D "part=\"$p\"" -o "$p.stl" cube.scad
done
openscad -q -D 'part="all"' --imgsize=600,500 --viewall --autocenter --colorscheme=Tomorrow -o cube.png cube.scad
