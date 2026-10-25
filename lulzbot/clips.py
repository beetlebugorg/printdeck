"""Printed cable clips for the pod cables.

Each clip has the same cradle: a C around the cable, with a mouth a little narrower than the cable.
The cable presses in through the mouth. Two kinds of base:

- the rail clip: a key in the front slot of the TAZ rail, and an M3 screw into a roll-in nut in the
  slot (the same nut as the bracket clips);
- the tape clip: a flat base for a square of double-sided tape (12 x 12 mm, 1 mm thick foam tape),
  for places off the rail. The two-cable tape clip holds the power and printer cables side by side.

Clip coordinates: the mounting face is z = 0, the clip stands toward +z, and the cable runs along y.
"""

import math
import os

from build123d import Box, Cylinder, Pos, Rot

CABLE_HOLE = 4.6            # fits the power cable (4.0) and the USB cable (4.5)
MOUTH = 3.6
CRADLE_WALL = 1.6
CRADLE_LEN = 10.0           # along the cable
TAPE = 1.0                  # the foam tape under a tape clip
BASE_T = 2.0
PAIR = 7.0                  # the spacing of the two cables of the two-cable clip
SLOT_KEY = 5.8              # the key in the 6.2 mm front slot of the rail


def cradle(x=0.0, z0=BASE_T):
    """A C around a cable along y, its bottom on z0, at x. Returns (solid, cable center z)."""
    r_in = CABLE_HOLE / 2
    r_out = r_in + CRADLE_WALL
    zc = z0 + r_in + 0.4
    c = Pos(x, 0, zc) * Rot(90, 0, 0) * Cylinder(r_out, CRADLE_LEN)
    c = c + Pos(x, 0, (z0 + zc) / 2) * Box(2 * r_out, CRADLE_LEN, zc - z0)
    c = c - Pos(x, 0, zc) * Rot(90, 0, 0) * Cylinder(r_in, CRADLE_LEN + 2)
    # The mouth at the top, a little narrower than the cable.
    c = c - Pos(x, 0, zc + r_out) * Box(MOUTH, CRADLE_LEN + 2, 2 * r_out)
    return c, zc


def rail_clip():
    """The rail clip: a base on the front face of the rail, a key in the slot, an M3 screw through
    the middle into a roll-in nut. The cradle is beside the screw."""
    w, l = 12.0, 22.0
    base = Pos(0, 0, BASE_T / 2) * Box(w, l, BASE_T)
    key = Pos(0, 0, -1.0) * Box(SLOT_KEY, l, 2.0)
    c, zc = cradle(0.0)
    c = Pos(0, 6.0, 0) * c
    b = base + key + c
    # The screw: behind the cradle along the cable, counterbored for an M3 cap head.
    b = b - Pos(0, -6.0, 0) * Cylinder(1.7, 20)
    b = b - Pos(0, -6.0, BASE_T + 5) * Cylinder(3.1, 10)
    return b, zc


def tape_clip(pair=False):
    """The tape clip: a flat base on a tape square, one cradle or two side by side."""
    w = 14.0 + (PAIR if pair else 0.0)
    base = Pos(0, 0, BASE_T / 2) * Box(w, 14.0, BASE_T)
    xs = (-PAIR / 2, PAIR / 2) if pair else (0.0,)
    b = base
    zc = 0.0
    for x in xs:
        c, zc = cradle(x)
        b = b + c
    return b, zc


# The cable center over the mounting face, for the routes: a tape clip sits on its tape.
RAIL_CABLE_H = rail_clip()[1]
TAPE_CABLE_H = tape_clip()[1] + TAPE


def export(out_stl):
    from build123d import export_stl
    for name, (s, _) in (("clip_rail", rail_clip()), ("clip_tape", tape_clip()), ("clip_tape_pair", tape_clip(True))):
        export_stl(s, os.path.join(out_stl, f"{name}.stl"), tolerance=0.01, angular_tolerance=0.1)
        bb = s.bounding_box()
        print(f"{name}.stl: {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x {bb.max.Z - bb.min.Z:.1f} mm")


if __name__ == "__main__":
    export(os.path.join(os.path.dirname(os.path.abspath(__file__)), "stl"))
