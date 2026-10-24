"""The deck case for the TAZ Workhorse: one case on the front of the chassis for the 7 inch display,
the Pi 5 with its Active Cooler, and the boot drive. The face leans back 15 degrees. The case is
deepest at the bottom, and the Pi lies across the bottom, behind the display.

Two printed parts:
- the body: the face with the screen opening, the four walls, the bosses for the display, the cut-outs
  in the bottom (the Pi's own USB-C port, and the USB-A socket for the printer), and the nub on the
  cover screw. It prints face down.
- the back: a flat plate on the flanges of the two rail clips, with the bosses for the Pi and the slots
  for the keys of the clips. Four screws from the back hold it to the body; the printer hides them.

The case touches the printer in two places at a right angle: the back on the clip flanges (the clips
set the depth), and the nub on the cover screw (a slot front to back: the screw only holds the case
against the cover). So the case cannot bow between them.

Mount coordinates are those of shape.py: x to the front of the printer, y up (0 is Y_BASE over the
bottom of the chassis), z out of the cover. Face coordinates: u across the face to the right (u = -z),
v up the face, w out of the face; the origin is the middle of the top edge of the face.
"""

import math

from build123d import (Align, Axis, Box, Cylinder, Location, Plane, Polygon, Pos, Rot, extrude, fillet, offset)

import shape as S

TILT = S.TILT
SIN, COS, TAN = math.sin(TILT), math.cos(TILT), math.tan(TILT)

# The display: Adafruit 2407 (7 inch, 800 x 480). From shape.py.
AA = (S.AA_HV[1], S.AA_HV[0])        # active area: across, along (portrait)
LCM = S.LCM_AB                        # panel: across, along
BOARD_FRONT = S.BOARD_C[1]            # the front of the board, behind the front of the glass (-7.35)
BOARD_T = S.BOARD_C[1] - S.BOARD_C[0] # 2.0 in the model
BOARD_HALF = S.BOARD_B                # half length of the board
EARS = S.EARS                         # (a, b) of the ear holes from the panel centre

BORDER = 20.0                         # the border around the screen: the sides and the top
CHIN = 30.0                           # the border at the bottom: the sockets of the display are there
R_EDGE = 10.0
WALL = 2.0                            # the walls of the body
SKIN = 2.0                            # the face
T_BACK = 3.0                          # the back plate
TOP_DEPTH = 22.0                      # from the back to the face, at the top

WIDTH = AA[0] + 2 * BORDER
FACE_LEN = AA[1] + BORDER + CHIN
X_BACK = S.BACK_X[0]                  # the back of the case, on the clip flanges
Y_TOP = 262.0 - S.Y_BASE              # under the logo
Y_BOT = Y_TOP - FACE_LEN * COS
Z_RIGHT = S.RAIL_SLOT_Z - 10.0        # the clips are 10 mm in from the right edge
Z_LEFT = Z_RIGHT + WIDTH
Z_MID = (Z_RIGHT + Z_LEFT) / 2


def face_x(y):
    """x of the face at the height y."""
    return X_BACK + TOP_DEPTH + (Y_TOP - y) * TAN


U = (0.0, 0.0, -1.0)
V = (-SIN, COS, 0.0)
W = (COS, SIN, 0.0)
O = (face_x(Y_TOP), Y_TOP, Z_MID)
FACE_LOC = Location(Plane(origin=O, x_dir=U, z_dir=W))   # u, v, w


def face_pt(u, v, w):
    return tuple(O[i] + u * U[i] + v * V[i] + w * W[i] for i in range(3))


# The display centre on the face: the active area is BORDER under the top edge.
V_AA = -(BORDER + AA[1] / 2)
DISP_V = V_AA - S.AA_OFF_HV[1] * 0     # the panel centre (the small offset of the active area is ignored here)


def disp(a, b, c):
    """Portrait display coordinates (a across, b along with the HDMI end at -b, c out of the glass) on
    the face. The HDMI end is at the bottom: b grows up the face, and a = u."""
    return (a, DISP_V + b, -S.LIP + c)


def box(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(abs(x1 - x0), abs(y1 - y0), abs(z1 - z0))


def outer():
    """The outer envelope: the side profile (the back, the tilted face, the top and the bottom),
    from Z_RIGHT to Z_LEFT, with round edges."""
    prof = Polygon((X_BACK, Y_BOT), (face_x(Y_BOT), Y_BOT), (face_x(Y_TOP), Y_TOP), (X_BACK, Y_TOP), align=None)
    s = Pos(0, 0, Z_RIGHT) * extrude(Plane.XY * prof, amount=WIDTH)
    # Round all edges except those of the back, which sits flat on the clip flanges.
    edges = s.edges().filter_by(lambda e: not (abs(e.center().X - X_BACK) < 0.01))
    return fillet(edges, R_EDGE)


def inner():
    """The inside of the body: the envelope less the walls and the face, open at the back."""
    x0 = X_BACK - 1.0
    prof = Polygon((x0, Y_BOT + WALL), (face_x(Y_BOT + WALL) - SKIN / COS, Y_BOT + WALL),
                   (face_x(Y_TOP - WALL) - SKIN / COS, Y_TOP - WALL), (x0, Y_TOP - WALL), align=None)
    s = Pos(0, 0, Z_RIGHT + WALL) * extrude(Plane.XY * prof, amount=WIDTH - 2 * WALL)
    return s


def shell():
    """The envelope, hollow: walls of WALL, open at the back."""
    o = outer()
    back = o.faces().sort_by(Axis.X)[0]
    return offset(o, amount=-WALL, openings=[back])


def body():
    s = shell()
    # The screen opening and the pocket of the glass, in face coordinates.
    oa, ob = AA[0] + 2 * S.OPEN_MARGIN, AA[1] + 2 * S.OPEN_MARGIN
    s = s - FACE_LOC * Pos(0, V_AA, 0) * Box(oa, ob, 10)
    pa, pb = LCM[0] + 2 * S.CLR, LCM[1] + 2 * S.CLR
    s = s - FACE_LOC * Pos(0, DISP_V, -S.LIP - 3.0) * Box(pa, pb, 6.0)
    return s


# The split between the navy top and the orange bottom: on the face, straight across just under the
# screen; on the sides, a straight line up and back to the back of the case, high enough for the Pi.
SPLIT_FACE_V = -(BORDER + AA[1] + 6.0)       # on the face: 6 mm under the active area
SPLIT_BACK_Y = Y_BOT + 92.0                   # on the back


def split_plane():
    """The plane of the split: through the line across the face at SPLIT_FACE_V, and through the
    line across the back at SPLIT_BACK_Y. Its normal points up (into the top part)."""
    p1 = face_pt(0, SPLIT_FACE_V, 0)
    p2 = (X_BACK, SPLIT_BACK_Y, p1[2])
    d = (p2[0] - p1[0], p2[1] - p1[1], 0.0)
    n = (-d[1], d[0], 0.0)                     # in the x-y plane, at right angles to d
    if n[1] < 0:
        n = (-n[0], -n[1], 0.0)
    return Plane(origin=p1, z_dir=n, x_dir=(0, 0, 1))


def halves():
    """The body cut at the split: (top, bottom)."""
    b = body()
    pl = split_plane()
    big = 600.0
    above = pl * Pos(0, 0, big / 2) * Box(big, big, big)
    return b & above, b - above


def top():
    return halves()[0]


def bottom():
    return halves()[1]


PARTS = [("top", top, "#2C3E73"), ("bottom", bottom, "#E8762B")]


if __name__ == "__main__":
    print(f"face {WIDTH:.1f} x {FACE_LEN:.1f}; {Y_BOT + S.Y_BASE:.1f} to {Y_TOP + S.Y_BASE:.1f} mm over the chassis bottom; "
          f"depth {TOP_DEPTH:.0f} at the top, {face_x(Y_BOT) - X_BACK:.1f} at the bottom")
    b = body()
    print("body volume", round(b.volume / 1000, 1), "cm3")
