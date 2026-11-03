"""Printed cable clips for the pod cables.

Each clip has the same cradle: a C around the cable, with a mouth a little narrower than the cable.
The cable presses in through the mouth. Two kinds of base:

- the rail clip: a key in the front slot of the TAZ rail, and an M3 screw into a roll-in nut in the
  slot (the same nut as the bracket clips);
- the tape clip: a flat base for a square of double-sided tape (12 x 12 mm, 1 mm thick foam tape),
  for places off the rail. The two-cable tape clip holds the power and printer cables side by side.

The slot clip has no cradle. The cable lies in the slot of a 2020 extrusion, and the clip pushes in
over it and closes the slot like a short channel cover.

Clip coordinates: the mounting face is z = 0, the clip stands toward +z, and the cable runs along y.
The slot clip goes into the slot, toward -z.
"""

import math
import os

from build123d import Box, Cylinder, Plane, Polyline, Pos, Rot, extrude, make_face

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


# The slot clip. The slot of the TAZ frame, from the rail of the LulzBot model (model.py): the
# lips are 1.75 mm thick and round, behind them the slot is 13 mm wide, and 45 degree walls go down
# to a flat bottom 5 mm wide, 6.4 mm behind the face. The opening is 5.24 mm at its narrowest part
# (measured with calipers on the printer), the same as the mesh. The back of each lip is flat only
# from 3.4 mm off the middle of the slot. Nearer the middle it is the round of the lip, and a barb
# there slips out.
#
# A flat cap lies on the rail face. Under it, two arms go into the opening. Each arm is fixed to the
# root at one end only and runs along the slot, so it is long enough to bend. A barb at the free end
# of each arm presses on the lips as the clip goes in, the arms bend in, and the barbs spring out
# onto the flat backs of the lips. The clip prints standing on the root end, so the arms bend in the
# plane of the layers.
#
# The USB cable of the B0561 is 4.45 mm (measured with calipers). On the bottom of the slot its top
# is 1.95 mm behind the face, only 0.2 mm behind the lips. The first clips did not go in, because the
# bottoms of the arms pushed on the cable. A channel along the underside of the clip now clears the
# cable, and its sides hold the cable down.
SLOT_LIP = 1.85             # the top of the barbs behind the face: 0.1 mm behind the lips
SLOT_ARMS = 4.2             # across the two arms: the same as the keys of the TAZ clamp
SLOT_ARM_GAP = 1.9          # between the arms: each arm can bend 0.95 mm in
SLOT_BARB_TIP = 3.4         # each barb tip off the middle: on the flat back of the lip
SLOT_BARB_FOOT = 2.45       # the ramp of the barb at the bottom of the arm: in the opening
SLOT_DEPTH = 2.8            # the bottom of the arms behind the face
SLOT_CAP_GAP = 0.6          # between the arms and the cap: open with a 0.5 mm nozzle
SLOT_CAP = (7.0, 0.8)       # the cap: across, thick
SLOT_LEN, SLOT_ROOT, SLOT_BARB_L = 18.0, 2.5, 5.0   # along the slot: the clip, the root, the barbs
SLOT_BOTTOM = 6.4           # the flat bottom of the slot behind the face
SLOT_CABLE = 4.45           # the USB cable of the B0561
SLOT_CABLE_CLR = 0.15       # the channel over the cable: this much larger than the cable


def _box(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(x1 - x0, y1 - y0, z1 - z0)


def slot_clip():
    """The slot clip: the cap on the face at z = 0, the root at y = 0, the arms along +y."""
    h, g, d = SLOT_ARMS / 2, SLOT_ARM_GAP / 2, SLOT_DEPTH
    tip, foot = SLOT_BARB_TIP, SLOT_BARB_FOOT
    cw, ct = SLOT_CAP
    y0 = SLOT_LEN - SLOT_BARB_L
    s = _box(-h, h, 0, SLOT_ROOT, -d, 0) + _box(-cw / 2, cw / 2, 0, SLOT_LEN, 0, ct)
    for k in (1, -1):
        s = s + _box(*sorted((k * g, k * h)), SLOT_ROOT, SLOT_LEN, -d, -SLOT_CAP_GAP)
        # The barb across the slot: a flat top on the back of the lip and a ramp down to the bottom
        # of the arm. Along the slot it starts at 56 degrees, so it prints with no overhang.
        across = [(k * x, z) for x, z in ((h - 0.01, -SLOT_LIP), (tip, -SLOT_LIP), (tip, -SLOT_LIP - 0.2),
                                          (foot, -d), (h - 0.01, -d))]
        across = extrude(Plane.XZ * make_face(Polyline(*across, close=True)), -SLOT_LEN)
        along = [(k * x, y) for x, y in ((h - 0.01, y0 - 1.5 * (tip - h)), (tip, y0), (tip, SLOT_LEN), (h - 0.01, SLOT_LEN))]
        along = Pos(0, 0, -d - 1) * extrude(make_face(Polyline(*along, close=True)), d + 1)
        s = s + (across & along)
    r = SLOT_CABLE / 2
    channel = Pos(0, SLOT_LEN / 2, -(SLOT_BOTTOM - r)) * Rot(90, 0, 0) * Cylinder(r + SLOT_CABLE_CLR, SLOT_LEN + 2)
    return s - channel


# The cable center over the mounting face, for the routes: a tape clip sits on its tape.
RAIL_CABLE_H = rail_clip()[1]
TAPE_CABLE_H = tape_clip()[1] + TAPE


def export(out_stl):
    from build123d import export_stl
    for name, (s, _) in (("clip_rail", rail_clip()), ("clip_tape", tape_clip()), ("clip_tape_pair", tape_clip(True)),
                         ("clip_slot", (slot_clip(), 0.0))):
        export_stl(s, os.path.join(out_stl, f"{name}.stl"), tolerance=0.01, angular_tolerance=0.1)
        bb = s.bounding_box()
        print(f"{name}.stl: {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x {bb.max.Z - bb.min.Z:.1f} mm")


if __name__ == "__main__":
    export(os.path.join(os.path.dirname(os.path.abspath(__file__)), "stl"))
