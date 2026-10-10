# Adaptive Speed (Cura post-processing script)

`AdaptiveSpeed.py` changes the speed of each extrusion move from the geometry of its path. Quality comes first:

- Straight runs (corner to corner, over all the short segments) go faster, the longer the faster, with a boost for each kind of move. The outer wall is not boosted.
- Curves go only as fast as `v = sqrt(a * R)`. Short paths and small wall loops (pins, small holes) go slower.
- Overhangs and bridges go slower. Each move is checked against the lines of the layer below, at five points across its width.
- Each outer wall loop keeps one speed, so the surface has no bands from speed changes.
- No move melts more plastic than Cura already does (or than **Maximum flow**). No layer gets faster than Cura's Minimum Layer Time.
- Only `F` changes. X, Y, Z, and E stay as Cura made them.

Install: copy `AdaptiveSpeed.py` into the `scripts` folder of Cura's configuration folder (Help > Show Configuration Folder), restart Cura, then Extensions > Post Processing > Modify G-Code > Add a script > Adaptive Speed.

Without Cura: `python3 AdaptiveSpeed.py in.gcode out.gcode [key=value ...]`. It prints the estimated time before and after.

Tests: `python3 -m unittest test_adaptive_speed` in this folder. `GCODE_FILES=a.gcode,b.gcode` also checks real files: only the speeds may change.
