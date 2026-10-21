"""Printdeck Slate: a fit test for the display opening. Writes stl/fit_display.stl.

The test is a ring of the front skin of the slab: the screen opening, and the LCD pocket with
a wall of MARGIN around it. The pocket has 0.3 mm of clearance around the LCD, so it holds the
display in place without the bosses. The test prints face down, the same as the slab.

Use: put the display glass down into the pocket and turn the two over. The dark LCD border
must show equally on all four sides of the opening. If it does not, measure it on each side
and change VIEW_OFF in shape.py.
Run: uv run --no-project --with build123d python fit_display.py
"""

import os

from build123d import Pos, Rot, export_stl

import parts as P
import shape as S

MARGIN = 2.0                        # wall around the LCD pocket


def fit_display():
    """The display area of the slab, in slab coordinates."""
    a, b, c = S.DISP_C
    ha, hb = S.LCD_WH[0] / 2 + MARGIN, S.LCD_WH[1] / 2 + MARGIN
    return P.slab() & P.box(a - ha, a + ha, b - hb, b + hb, -S.WALL, 1)


def main():
    s = fit_display()
    # Print pose: the front face on the bed.
    pose = Rot(180, 0, 0) * s
    bb = pose.bounding_box()
    pose = Pos(-bb.center().X, -bb.center().Y, -bb.min.Z) * pose
    os.makedirs("stl", exist_ok=True)
    export_stl(pose, "stl/fit_display.stl", tolerance=0.01, angular_tolerance=0.1)
    bb = pose.bounding_box()
    print(f"wrote stl/fit_display.stl: {bb.size.X:.0f} x {bb.size.Y:.0f} x {bb.size.Z:.1f} mm, "
          f"volume {pose.volume / 1000:.1f} cm3, valid {pose.is_valid}, {len(pose.solids())} solid")


if __name__ == "__main__":
    main()
