#!/usr/bin/env bash
# Build the Speakerdeck Slate: the parts in build123d (fit check, STEP, STL), the mesh check,
# the Cura profiles with the filament and the support check, the renders, and the build PDF.
# Takes about 1 minute (more if CuraEngine stalls and the slice runs again).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p build
uv run --no-project --with build123d python model.py | tee build/model.log
grep -q "RESULT: OK" build/model.log || { echo "fit check failed"; exit 1; }
# Mesh check: every STL must be one closed part.
uv run --no-project --with numpy --with scipy --with trimesh --with networkx python - <<'PY'
import sys, trimesh
bad = False
for n in ("slab", "chin", "back", "foot"):
    m = trimesh.load(f"stl/{n}.stl")
    ok = m.is_watertight and m.is_winding_consistent and m.body_count == 1
    print(f"  stl/{n}.stl: {'closed, one part' if ok else 'NOT CLEAN'}")
    bad |= not ok
sys.exit(1 if bad else 0)
PY
# Cura profiles, the filament of each part, and the support check (must be 0 g).
python3 cura/make_profiles.py
python3 - <<'PY'
import json, sys
s = json.load(open("build/supports.json"))
bad = {k: v for k, v in s.items() if v > 0}
print("  support check:", "0 g for all parts" if not bad else f"SUPPORT NEEDED {bad}")
sys.exit(1 if bad or len(s) != 4 else 0)
PY
mkdir -p build/fc_home
export FREECAD_USER_HOME="$PWD/build/fc_home"
xvfb-run -a -s "-screen 0 1920x1080x24" freecad render.py > /dev/null 2>&1
cat build/render.log
for f in build/img/raw/*.png; do
  magick "$f" -trim +repage -background white -alpha remove -alpha off -bordercolor white -border 40 "build/img/$(basename "$f")"
done
python3 make_pdf.py
