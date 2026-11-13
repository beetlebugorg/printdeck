#!/usr/bin/env python3
"""Write the bed level tests: five flat squares, one in each corner of an area and one in its
middle. Print one with the first layer at 0.25 mm, and measure each square with calipers.
About 0.30 mm on every square is a good first layer (the calipers read the tops of the lines).

    uv run --no-project --with trimesh --with numpy python make.py
"""

from pathlib import Path

import trimesh

HERE = Path(__file__).resolve().parent
H = 0.4  # mm: the squares are two layers

# name: (side of the area, side of a square). The TAZ Workhorse prints 280 x 280 mm, the Mini 2
# 160 x 160 mm.
TESTS = {"taz": (250.0, 40.0), "mini2": (150.0, 30.0)}


def squares(size, sq):
    spots = [(0, 0), (size - sq, 0), (0, size - sq), (size - sq, size - sq), ((size - sq) / 2, (size - sq) / 2)]
    parts = []
    for x, y in spots:
        b = trimesh.creation.box(extents=(sq, sq, H))
        b.apply_translation((x + sq / 2, y + sq / 2, H / 2))
        parts.append(b)
    return trimesh.util.concatenate(parts)


def main():
    for name, (size, sq) in TESTS.items():
        m = squares(size, sq)
        m.export(HERE / f"{name}.stl")
        print(f"{name}.stl: {size:.0f} x {size:.0f} mm, five {sq:.0f} mm squares, {H} mm high")


if __name__ == "__main__":
    main()
