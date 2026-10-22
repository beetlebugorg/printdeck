# Slices the plaque of the video (plaque.py) for the simulated TAZ Workhorse, with the PLA
# settings of the mount (lulzbot/cura/make_profiles.py), and writes the G-code with the PRINT_START
# and PRINT_END macros of the Printdeck printer.
#
#   python3 desktop/film/slice.py PLAQUE.stl OUT.gcode
import os, re, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "lulzbot" / "cura"))
import make_profiles as mp

stl, out = Path(sys.argv[1]).resolve(), Path(sys.argv[2])
prof = mp.profile("Plaque", "Plaque", str(stl), {**mp.FLAT, "infill_sparse_density": 20})
if mp.slice_part(prof) is None:
    sys.exit("the slice failed")
gcode = (mp.BUILD / "slice" / (stl.stem + ".gcode")).read_text()

# The start and the end of the file are the ones of the Printdeck printer in Cura: PRINT_START does
# the wipe, the washer probe and the prime line, and PRINT_END ends the print.
body = gcode[gcode.index(";LAYER_COUNT:"):]
body = body[:body.index(";TIME_ELAPSED:", body.rindex(";LAYER:"))]
# ";TIME:" of CuraEngine is a placeholder: the last ";TIME_ELAPSED:" is the print time.
time = str(int(float(re.findall(r";TIME_ELAPSED:([\d.]+)", gcode)[-1])))
head = f""";FLAVOR:Marlin
;TIME:{time}
;Layer height: 0.3
;TARGET_MACHINE.NAME:LulzBot TAZ Workhorse | SE | 0.5 mm
;PRINTDECK_MATERIAL: PLA
;PRINTDECK_MATERIAL_NAME: Orange PLA
PRINT_START BED=60 EXTRUDER=220 WIPE=160 LAYER=0.3 SPEED=18
M82 ;absolute extrusion mode
G92 E0
G1 F900 E-1.5
"""
# No retract here: the file is in absolute extrusion (M82), and PRINT_END ends the print.
tail = """M140 S0
M107
PRINT_END
M104 S0
;End of Gcode
"""
Path(out).write_text(head + body + tail)
print(out, "print time", int(time) // 60, "min")
