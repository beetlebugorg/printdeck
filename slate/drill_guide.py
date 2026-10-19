"""Speakerdeck Slate: a drill guide for the chin screw holes in a slab that was printed without
them. Writes stl/drill_guide.stl. A new slab has the holes, so it does not need the guide.

The guide is a plate with three lips. It goes on the outer face of a side wall of the slab:
one lip under the bottom edge, one on the front face, and one on the rear face. The hole in
the plate is on the screw axis (SIDE_SCREW in shape.py). The guide is symmetric front to rear,
so turn it over for the other side.

Use: take the chin off. Push the guide onto the bottom of a side wall until the bottom lip
touches the edge. Drill a 3.2 mm hole through the side wall, then do the other side.
Run: uv run --no-project --with build123d python drill_guide.py
"""

import os

from build123d import Pos, Rot, export_stl

import parts as P
import shape as S

PLATE = 8.0                         # thickness: the drill stays square to the wall
LIP = 4.0
CLR = 0.15                          # between the front and rear lips and the slab faces


def guide():
    """The guide on the right side wall, in slab coordinates."""
    a0 = P.A_OUT
    a1 = a0 + PLATE
    b0, b1 = S.CHIN_B - 6.0, S.CHIN_B + 16.0
    cf, cr = CLR, -S.D - CLR
    s = P.box(a0, a1, b0, b1, cr - LIP, cf + LIP)
    s = s + P.box(P.A_IN + 0.2, a1, b0, S.CHIN_B, cr - LIP, cf + LIP)       # under the bottom edge
    s = s + P.box(P.A_IN + 0.2, a1, b0, b1, cf, cf + LIP)                   # on the front face
    s = s + P.box(P.A_IN + 0.2, a1, b0, b1, cr - LIP, cr)                   # on the rear face
    b, c = S.SIDE_SCREW
    return s - P.cyl_a(b, c, 1.65, a0 - 1, a1 + 1)


def main():
    s = guide()
    # Print pose: the outer face of the plate on the bed, the lips up, the hole upright.
    pose = Rot(0, 90, 0) * s
    bb = pose.bounding_box()
    pose = Pos(-bb.center().X, -bb.center().Y, -bb.min.Z) * pose
    os.makedirs("stl", exist_ok=True)
    export_stl(pose, "stl/drill_guide.stl", tolerance=0.01, angular_tolerance=0.1)
    bb = pose.bounding_box()
    print(f"wrote stl/drill_guide.stl: {bb.size.X:.0f} x {bb.size.Y:.0f} x {bb.size.Z:.0f} mm, "
          f"valid {pose.is_valid}, {len(pose.solids())} solid")


if __name__ == "__main__":
    main()
