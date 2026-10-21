"""Printdeck LulzBot: a fit test for the display opening. Writes stl/fit_display.stl.

The test is the part of the head face around the screen: the opening, the LCD pocket, and the
four bosses, with a wall of MARGIN around the board. It prints face down, the same as the head.
Put the display glass down into the pocket, and screw the board to the bosses. The dark LCD
border must show 0.5 mm on all four sides of the opening. If it does not, measure it on each
side and change AA_OFF_HV in shape.py.
Run: uv run --no-project --with build123d python fit_display.py
"""

import os

from build123d import Pos, Rot, export_stl

import parts as P
import shape as S

MARGIN = 5.0                        # around the board: the bosses are in it


def fit_display():
    """The display area of the head face, in head coordinates."""
    hh, hv = S.BOARD_B + MARGIN, S.EAR_A + MARGIN
    keep = P.box(-hh, hh, -hv, hv, -S.LIP + S.BOARD_C[1] - 0.01, 0.5)
    return P.head() & keep


def main():
    s = fit_display()
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
