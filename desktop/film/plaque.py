# The part that the simulated printer prints in the video (film.sh): a plaque with the word
# PRINTDECK in raised letters, in the font of the deck.
#
#   uv run --no-project --with build123d python desktop/film/plaque.py OUT.stl
import os, sys
from build123d import Box, Text, Align, BuildPart, BuildSketch, Locations, extrude, fillet, Axis, export_stl

HERE = os.path.dirname(os.path.abspath(__file__))
FONT = os.path.join(HERE, "..", "..", "deck", "internal", "gfx", "fonts", "IBMPlexMono-SemiBold.ttf")

with BuildPart() as plaque:
    Box(150, 36, 1.2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    fillet(plaque.edges().filter_by(Axis.Z), radius=4)
    with BuildSketch(plaque.faces().sort_by(Axis.Z)[-1]):
        Text("PRINTDECK", font_size=20, font_path=FONT, align=(Align.CENTER, Align.CENTER))
    extrude(amount=3.6)

export_stl(plaque.part, sys.argv[1])
print("plaque", plaque.part.bounding_box())
