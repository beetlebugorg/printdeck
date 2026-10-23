#!/usr/bin/env python3
"""Printdeck LulzBot: check that the printed roll-in nut rolls into the slot of the rail.
Run with: uv run --no-project --with build123d --with shapely --with scipy python roll_nut.py

The check is in the section of the rail across the slot, at the upper clip, from the LulzBot model.
The nut can turn about the slot axis and move across the slot and in and out. The check makes a
grid of all these positions (0.05 mm, 1 degree), marks each position where the section of the
nut touches the rail with ROLL_CLEAR of clearance, and finds a path of free positions from
outside the rail to the place of the nut behind the lips. The section of the nut includes the
hex nut in it. Then it checks the place of the nut: pulled up, its shoulders are on the lips at
ROLL_NUT_TOP.

Output: build/roll_nut.json, and the result on stdout ("RESULT: OK").
"""

import json
import math
import os
import sys

import numpy as np
import shapely
from build123d import Align, Box, Pos, Solid
from scipy import ndimage
from scipy.signal import fftconvolve
from shapely import affinity
from shapely.geometry import Polygon, box

import parts as P
import shape as S
import workhorse as WH

HERE = os.path.dirname(os.path.abspath(__file__))
H = 0.05                                   # the grid step, mm
ANGLES = np.arange(-120, 120.01, 1.0)      # the turn of the nut about the slot axis, degrees


def rail_section():
    """The section of the rail across the slot at the upper clip, as a shapely polygon in (w, d):
    w across the slot from its middle, d behind the rail face."""
    yc = S.CLIPS_Y[0]
    inv = P.LOC.inverse()
    cut = Pos(S.RAIL_FRONT_X - 25, yc - 0.01, S.RAIL_SLOT_Z - 15) * Box(30, 0.02, 30, align=(Align.MIN,) * 3)
    rings = []
    for sh in WH.load("final_frame_01").shells():
        if sh.bounding_box().max.X <= 300:
            continue
        s = inv * sh
        bb = s.bounding_box()
        if not (bb.min.Y < yc < bb.max.Y) or bb.max.X < S.RAIL_FRONT_X - 30 or bb.min.X > S.RAIL_FRONT_X + 1:
            continue
        try:
            c = Solid(s) & cut
        except Exception:
            continue
        if c is None or c.volume == 0:
            continue
        f = max(c.faces(), key=lambda f: abs(f.normal_at().Y))
        for w in [f.outer_wire()] + list(f.inner_wires()):
            pts = []
            for e in w.edges():
                for i in range(8):
                    p = e.position_at(i / 8)
                    pts.append((p.Z - S.RAIL_SLOT_Z, S.RAIL_FRONT_X - p.X))
            rings.append(pts)
    rail = Polygon(rings[0]).buffer(0)
    for r in rings[1:]:
        rail = rail.difference(Polygon(r).buffer(0))
    return rail.intersection(box(-9, 0, 9, 9))


def nut_section():
    """The section of the printed nut with the hex nut in it, with the origin at the middle of its
    top face, d down."""
    f, t = S.HEX_NUT
    body = Polygon(S.roll_nut_section())
    return body.union(box(-f / 2, S.HEX_TOP, f / 2, S.HEX_TOP + t))


def roll_path(rail, nut):
    """True when a path of free positions goes from outside the rail to the place of the nut."""
    w0, w1, d0, d1 = -9.0, 9.0, -14.0, 7.5
    ws, ds = np.arange(w0, w1, H), np.arange(d0, d1, H)
    gw, gd = np.meshgrid(ws, ds, indexing="ij")
    railm = shapely.contains_xy(rail.buffer(S.ROLL_CLEAR), gw, gd).astype(np.float32)
    free = np.zeros((len(ws), len(ds), len(ANGLES)), bool)
    for k, ang in enumerate(ANGLES):
        q = affinity.rotate(nut, ang, origin=(0, 0))
        n = int(math.ceil((max(abs(v) for v in q.bounds) + H) / H))
        kx = np.arange(-n, n + 1) * H
        kw, kd = np.meshgrid(kx, kx, indexing="ij")
        ker = shapely.contains_xy(q, kw, kd).astype(np.float32)
        # The touch at origin (i, j): the sum of rail[i + a - n, j + b - n] * ker[a, b].
        hit = fftconvolve(railm, ker[::-1, ::-1], mode="full")[n:n + len(ws), n:n + len(ds)]
        free[:, :, k] = hit < 0.5
    lab, _ = ndimage.label(free, structure=ndimage.generate_binary_structure(3, 1))
    k0 = int(np.argmin(abs(ANGLES)))
    goal = lab[int(round(-w0 / H)), int(round((S.RAIL_LIP + S.NUT_GAP - d0) / H)), k0]
    outside = set(np.unique(lab[:, int(round((-13 - d0) / H)), :])) - {0}
    return goal != 0 and goal in outside


def pull_up(rail, nut):
    """The depth of the top face when the nut is pulled up onto the lips."""
    for i in range(300):
        d = S.RAIL_LIP + S.NUT_GAP - i * 0.01
        q = affinity.translate(nut, 0, d)
        if q.intersection(rail).area > 1e-4:
            return d + 0.01
    return None


def main():
    rail = rail_section()
    nut = nut_section()
    problems = []
    ok = roll_path(rail, nut)
    if not ok:
        problems.append(f"the roll-in nut does not roll into the slot with {S.ROLL_CLEAR} mm of clearance")
    top = pull_up(rail, nut)
    b = nut.bounds
    if top is None or abs(top - S.ROLL_NUT_TOP) > 0.05:
        problems.append(f"pulled up, the nut stops at {top} mm behind the rail face, not at ROLL_NUT_TOP")
    width = b[2] - b[0]
    result = {"rolls_in": bool(ok), "clearance": S.ROLL_CLEAR, "width": round(width, 2),
              "thick": S.ROLL_NUT_T, "top": round(top, 2) if top else None}
    os.makedirs(os.path.join(HERE, "build"), exist_ok=True)
    json.dump(result, open(os.path.join(HERE, "build", "roll_nut.json"), "w"), indent=1)
    print(f"  roll-in nut: {width:.2f} mm wide, {S.ROLL_NUT_T:.1f} mm thick; rolls in with {S.ROLL_CLEAR} mm of "
          f"clearance: {'yes' if ok else 'NO'}; pulled up, its top face is {top:.2f} mm behind the rail face")
    for p in problems:
        print("  CLASH: " + p)
    print("RESULT: " + ("CLASHES" if problems else "OK"))
    sys.exit(1 if problems else 0)


if __name__ == "__main__":
    main()
