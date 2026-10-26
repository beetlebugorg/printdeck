"""Printdeck pod: a fit test for the display holes of the pod frame. Writes stl/pod/fdm/fit_holes_fdm.stl.

The test is a thin ladder with the four display holes of the frame, at the same positions and
height: the four holes go through a 2 mm bar into the four raised tabs. The side bars are outside the Pi, and the bottom bar has a gap for the loop of
the ribbon, so the test fits with the Pi on the display. It prints flat on its back, with
supports off. Put the display glass down, and lay the test on the back of the display. The four
holes must line up with the holes of the display, the M2.5 screws must go in straight, and the
ladder must lie flat on all four tabs. If the holes do not line up, change HOLE_DV in pod.py.
Run: uv run --no-project --with build123d python fit_holes.py
"""

import os

from build123d import Cylinder, Pos, export_stl

import pod as P

BAR = 6.0                           # the width of the cross bars
SIDE_U = (31.0, 35.0)               # the side bars: outside the Pi (28) and its standoffs (27)


def fit_holes():
    """The ladder, in the frame coordinates."""
    w0, w1 = -P.SPLIT_W, -P.SPLIT_W + P.FLOOR
    vb, vt = P.TABS[0][1], P.TABS[-1][1]
    s = None
    for side in (-1, 1):
        bar = P.uvw_box(side * SIDE_U[0], side * SIDE_U[1], vb - BAR / 2, vt + BAR / 2, w0, w1)
        s = bar if s is None else s + bar
    s = s + P.uvw_box(-SIDE_U[1], SIDE_U[1], vt - BAR / 2, vt + BAR / 2, w0, w1)
    gap = (P.RIBBON_LOOP_U[0] - 0.6, P.RIBBON_LOOP_U[1] + 0.6)
    for a, b in ((-SIDE_U[1], gap[0]), (gap[1], SIDE_U[1])):
        s = s + P.uvw_box(a, b, vb - BAR / 2, vb + BAR / 2, w0, w1)
    # The holes, as in pod_frame().
    for u, v in P.TABS:
        s = s - Pos(u, v, w0 + P.FLOOR / 2) * Cylinder(P.DISPLAY_HOLE, P.FLOOR + 2.0)
    return s


def main():
    s = Pos(0, 0, P.SPLIT_W) * fit_holes()
    out = os.path.join(P.HERE, "stl", "pod", "fdm", "fit_holes_fdm.stl")
    export_stl(s, out, tolerance=0.01, angular_tolerance=0.1)
    bb = s.bounding_box()
    print(f"wrote {out}: {bb.size.X:.1f} x {bb.size.Y:.1f} x {bb.size.Z:.1f} mm, bed {bb.min.Z:.2f}, "
          f"volume {s.volume / 1000:.1f} cm3, valid {s.is_valid}, {len(s.solids())} solid")


if __name__ == "__main__":
    main()
