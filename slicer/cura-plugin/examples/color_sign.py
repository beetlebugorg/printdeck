# A sample for the Colors tool of the Cura plugin: a plate with raised letters on its top.
#
#   uv run --no-project --with build123d python color_sign.py OUT
#
# It writes OUT/color-sign.3mf with two objects, "plate" and "letters", at their places: open it in
# Cura, select the letters, and give them another roll in the Colors tool. It also writes
# OUT/plate.stl and OUT/letters.stl around 0, 0, for CuraEngine, which adds the middle of the bed.
import sys
from pathlib import Path

from build123d import (Align, BuildPart, BuildSketch, Mesher, Mode, Plane, Pos, RectangleRounded, Text,
                       export_stl, extrude)

OUT = Path(sys.argv[1] if len(sys.argv) > 1 else ".")
OUT.mkdir(parents=True, exist_ok=True)

W, D, H = 100.0, 34.0, 3.0  # the plate, mm
RAISE = 0.8  # the height of the letters over the plate
CX, CY = 140.0, 140.0  # the middle of the bed
# The font of the deck, from the watch app in the repository.
FONT = Path(__file__).resolve().parents[3] / "watch/Shared/Fonts/IBMPlexMono-SemiBold.ttf"

with BuildPart() as plate:
    with BuildSketch():
        RectangleRounded(W, D, 5)
    extrude(amount=H)
plate.part.label = "plate"

with BuildPart() as letters:
    with BuildSketch(Plane.XY.offset(H)):
        Text("PRINTDECK", font_size=13, font_path=str(FONT), align=(Align.CENTER, Align.CENTER))
    extrude(amount=RAISE)
letters.part.label = "letters"

on_bed = Pos(CX, CY, 0)
p, l = on_bed * plate.part, on_bed * letters.part
p.label, l.label = "plate", "letters"

mesher = Mesher()
mesher.add_shape(p)
mesher.add_shape(l)
mesher.write(str(OUT / "color-sign.3mf"))
export_stl(plate.part, str(OUT / "plate.stl"))
export_stl(letters.part, str(OUT / "letters.stl"))
print(f"wrote {OUT}/color-sign.3mf: the plate {W:g} x {D:g} x {H:g} mm, letters {RAISE:g} mm on its top")
