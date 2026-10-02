#!/usr/bin/env python3
"""Estimate the filament for each STL file and write filament.json.

The estimate uses the volume and the surface area of each mesh. It is not a
slicer result. A slicer gives the correct value for your print settings.
"""

import json
import math
import struct
from pathlib import Path

CASE = Path(__file__).resolve().parent.parent

# Print settings for the estimate.
DENSITY = 1.24  # g/cm3, PLA
FILAMENT_D = 2.85  # mm
SHELL = 1.2  # mm, walls and top and bottom layers
INFILL = 0.15
# Extra material for supports, as a fraction of the part.
SUPPORT = {"body": 0.08}

# (file name, quantity, label, Cura profile)
PARTS = [
    ("body", 1, "Body", "Speakerdeck - Body 0.3"),
    ("base", 1, "Base plate", "Speakerdeck - Base 0.3"),
    ("frame", 1, "Display frame", "Speakerdeck - Display Frame 0.2"),
    ("inky_frame", 1, "Inky frame", "Speakerdeck - Inky Frame 0.1 slow"),
    ("cap", 1, "Caps, 8 on one plate", "Speakerdeck - Caps 0.2"),
    ("pad", 1, "Inky pad", "Speakerdeck - Pad 0.2"),
]


def triangles(path):
    """Read the triangles of a binary or ASCII STL file."""
    data = path.read_bytes()
    count = struct.unpack_from("<I", data, 80)[0] if len(data) >= 84 else 0
    if len(data) == 84 + 50 * count:
        for i in range(count):
            v = struct.unpack_from("<9f", data, 84 + 50 * i + 12)
            yield v[0:3], v[3:6], v[6:9]
        return
    points = []
    for line in data.decode("ascii", "replace").splitlines():
        words = line.split()
        if words and words[0] == "vertex":
            points.append(tuple(float(w) for w in words[1:4]))
            if len(points) == 3:
                yield tuple(points)
                points = []


def measure(path):
    """Return the volume in mm3, the area in mm2, and the size in mm."""
    volume = 0.0
    area = 0.0
    low = [math.inf] * 3
    high = [-math.inf] * 3
    for a, b, c in triangles(path):
        cross = (
            (b[1] - a[1]) * (c[2] - a[2]) - (b[2] - a[2]) * (c[1] - a[1]),
            (b[2] - a[2]) * (c[0] - a[0]) - (b[0] - a[0]) * (c[2] - a[2]),
            (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]),
        )
        volume += (a[0] * cross[0] + a[1] * cross[1] + a[2] * cross[2]) / 6
        area += math.sqrt(sum(x * x for x in cross)) / 2
        for p in (a, b, c):
            for i in range(3):
                low[i] = min(low[i], p[i])
                high[i] = max(high[i], p[i])
    return abs(volume), area, [high[i] - low[i] for i in range(3)]


def main():
    per_gram_m = 1 / (DENSITY * math.pi * (FILAMENT_D / 2) ** 2)  # metres of filament for 1 g
    rows = []
    for name, quantity, label, profile in PARTS:
        volume, area, size = measure(CASE / "stl" / f"{name}.stl")
        shell = min(volume, area * SHELL)
        printed = (shell + (volume - shell) * INFILL) * (1 + SUPPORT.get(name, 0))
        grams = printed / 1000 * DENSITY * quantity
        rows.append({
            "name": name,
            "label": label,
            "file": f"stl/{name}.stl",
            "quantity": quantity,
            "profile": profile,
            "size": [round(s, 1) for s in size],
            "grams": round(grams, 1),
            "metres": round(grams * per_gram_m, 1),
        })
    result = {
        "settings": {"material": "PLA", "density": DENSITY, "filament_d": FILAMENT_D, "shell": SHELL, "infill": INFILL},
        "parts": rows,
        "total_grams": round(sum(r["grams"] for r in rows)),
        "total_metres": round(sum(r["metres"] for r in rows), 1),
    }
    (CASE / "build" / "filament.json").write_text(json.dumps(result, indent=2) + "\n")
    for r in rows:
        print(f'{r["file"]:22s} x{r["quantity"]}  {r["grams"]:7.1f} g  {r["metres"]:6.1f} m')
    print(f'total {result["total_grams"]} g, {result["total_metres"]} m')


if __name__ == "__main__":
    main()
