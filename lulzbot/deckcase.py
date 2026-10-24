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


# ---------------------------------------------------------------- fasteners
# MODE "resin": one black body, captive nuts in pockets with a side slot (heat-set inserts do not
# work in cured resin). MODE "fdm": the navy shell and the orange L, heat-set inserts.
MODE = "resin"
NUT = {3: (5.5, 2.4), 2.5: (5.0, 2.0)}       # hex nut: across the flats, thickness
NUT_CLR = 0.25
HOLE = {3: 3.4, 2.5: 2.9}                     # clearance holes


def hexagon(af, h):
    """A hexagonal prism, across flats af, height h, centred, along z."""
    r = af / math.sqrt(3)
    pts = [(r * math.cos(math.radians(60 * k)), r * math.sin(math.radians(60 * k))) for k in range(6)]
    return extrude(Plane.XY * Polygon(*pts, align=None), amount=h / 2, both=True)


def nut_trap(size, depth_out=20.0):
    """The pocket of a captive nut at the origin (axis z), and the slot to slide it in along +x."""
    af, t = NUT[size]
    af, t = af + 2 * NUT_CLR, t + 2 * NUT_CLR
    return hexagon(af, t) + Pos(depth_out / 2, 0, 0) * Box(depth_out, af, t)


# ---------------------------------------------------------------- the body

EAR_BOSS_R = 5.0                              # wide enough for the nut pocket in resin
BOARD_FRONT_W = -S.LIP + BOARD_FRONT          # w of the front of the display board (-8.35)


def display_bosses():
    """The four bosses of the display board ears, from the face to the board, with a hole and a
    captive M3 nut. An M3 x 8 screw from behind the board goes through the nut and stops short of
    the face."""
    add, cut = None, None
    for a, b in EARS:
        u, v = a, DISP_V + b
        bw0, bw1 = -SKIN + 0.01, BOARD_FRONT_W
        boss = FACE_LOC * Pos(u, v, (bw0 + bw1) / 2) * Cylinder(EAR_BOSS_R, bw0 - bw1)
        hole = FACE_LOC * Pos(u, v, (BOARD_FRONT_W - 2.35) / 2 - 0.01) * Cylinder(HOLE[3] / 2, abs(BOARD_FRONT_W + 2.35))
        # The nut 1.35 mm in front of the board; its slot opens toward the middle of the face, so it
        # stays inside the case (the nut goes in before the board).
        side = 1 if a > 0 else -1
        trap = FACE_LOC * Pos(u, v, BOARD_FRONT_W + 1.35 + NUT[3][1] / 2) * Rot(0, 0, 180 if side > 0 else 0) * nut_trap(3, 12.0)
        add = boss if add is None else add + boss
        c = hole + trap
        cut = c if cut is None else cut + c
    return add, cut


# The back plate is held by four M3 screws from the back, into blocks in the corners of the body.
# The blocks are the depth of the back opening; the nut is 5 mm in from the back.
BLOCK = 11.0
BLOCK_D = 10.0
CORNER_IN = WALL + BLOCK / 2 + 3.0            # from the outer walls to the screw axis


def corner_points():
    yb, yt = Y_BOT + CORNER_IN + 4.0, Y_TOP - CORNER_IN
    zr, zl = Z_RIGHT + CORNER_IN, Z_LEFT - CORNER_IN
    return [(yt, zr), (yt, zl), (yb, zr), (yb, zl)]


def corner_blocks():
    add, cut = None, None
    x0 = X_BACK + T_BACK
    for y, z in corner_points():
        blk = box(x0, x0 + BLOCK_D, y - BLOCK / 2, y + BLOCK / 2, z - BLOCK / 2, z + BLOCK / 2)
        # Joined to the nearest walls so that it is not a floating block.
        sy = 1 if y > (Y_TOP + Y_BOT) / 2 else -1
        sz = 1 if z > Z_MID else -1
        blk = blk + box(x0, x0 + BLOCK_D, y, y + sy * (CORNER_IN), z - BLOCK / 2, z + BLOCK / 2)
        blk = blk + box(x0, x0 + BLOCK_D, y - BLOCK / 2, y + BLOCK / 2, z, z + sz * CORNER_IN)
        hole = box(x0 - 1, x0 + BLOCK_D - 1.5, y - HOLE[3] / 2, y + HOLE[3] / 2, z - HOLE[3] / 2, z + HOLE[3] / 2)
        # The nut slot opens toward the middle of the case (up or down), never through a wall.
        trap = Location(Plane(origin=(x0 + 5.0, y, z), x_dir=(0, -sy, 0), z_dir=(1, 0, 0))) * nut_trap(3, BLOCK)
        add = blk if add is None else add + blk
        c = hole + trap
        cut = c if cut is None else cut + c
    return add, cut


# The nub on the cover screw: a tab on the left wall that reaches back along the cover to the cover
# screw at 240 mm. Its slot is open at the bottom, and wide front to back: loosen the screw and lift.
COVER_SCREW = (S.X_FRONT - 6.1, 240.0 - S.Y_BASE)
NUB_T = 4.0
NUB_H = 22.0
NUB_SLOT = (7.0, 3.6)                          # front to back, across (the screw is M3)


def nub():
    x_s, y_s = COVER_SCREW
    x0 = x_s - 9.0
    tab = box(x0, X_BACK + 12.0, y_s - NUB_H / 2, y_s + NUB_H / 2, 0.0, NUB_T)
    # Round the free end.
    tab = fillet(tab.edges().filter_by(Axis.Z).filter_by(lambda e: e.center().X < x0 + 0.1), 6.0)
    slot = box(x_s - NUB_SLOT[0] / 2, x_s + NUB_SLOT[0] / 2, y_s - NUB_H, y_s + NUB_SLOT[1] / 2, -1, NUB_T + 1)
    slot = slot + Pos(x_s, y_s, NUB_T / 2) * Cylinder(NUB_SLOT[1] / 2, NUB_T + 2)
    return tab, slot


# The floor: the Pi's own USB-C port is over a hole with a short guide for the plug, and the
# printer socket (Adafruit 908) is in the floor near the right side.
PLUG = (12.0, 7.0)                             # the plug of the official supply: across, front to back
SOCKET_FLANGE = (38.3, 14.65, 30.0)            # length, width, screw spacing (Adafruit 908 drawing)
SOCKET_BODY = (20.0, 11.0)                     # the body inside [E]


def body():
    s = shell()
    # The screen opening and the pocket of the glass, in face coordinates.
    oa, ob = AA[0] + 2 * S.OPEN_MARGIN, AA[1] + 2 * S.OPEN_MARGIN
    s = s - FACE_LOC * Pos(0, V_AA, 0) * Box(oa, ob, 10)
    pa, pb = LCM[0] + 2 * S.CLR, LCM[1] + 2 * S.CLR
    s = s - FACE_LOC * Pos(0, DISP_V, -S.LIP - 3.0) * Box(pa, pb, 6.0)
    add, cut = display_bosses()
    s = s + add - cut
    add, cut = corner_blocks()
    s = s + add - cut
    tab, slot = nub()
    s = s + tab - slot
    # The floor openings.
    px, py, pz = pi_port("usbc")
    s = s - box(px - PLUG[1] / 2 - 0.5, px + PLUG[1] / 2 + 0.5, Y_BOT - 1, Y_BOT + WALL + 1, pz - PLUG[0] / 2 - 0.5, pz + PLUG[0] / 2 + 0.5)
    sx, sz = socket_xz()
    s = s - box(sx - SOCKET_BODY[1] / 2, sx + SOCKET_BODY[1] / 2, Y_BOT - 1, Y_BOT + WALL + 1, sz - SOCKET_BODY[0] / 2, sz + SOCKET_BODY[0] / 2)
    for d in (-1, 1):
        s = s - box(sx - 1.7, sx + 1.7, Y_BOT - 1, Y_BOT + WALL + 1, sz + d * SOCKET_FLANGE[2] / 2 - 1.7, sz + d * SOCKET_FLANGE[2] / 2 + 1.7)
    return s


# ---------------------------------------------------------------- the back plate and the Pi

# The Pi 5 stands on the back plate: its board parallel to the back, the Active Cooler to the front,
# the USB-C edge down over the floor, the USB ports toward the right side (the flash drive points
# right). Pi STEP coordinates (ref/raspberrypi): x along the 85 (the USB ports at +x), y along the
# 56 (the USB-C edge at y = 0), z up from the bottom of the board.
PI_STANDOFF = 4.0
PI_HOLES = [(3.5, 3.5), (61.5, 3.5), (3.5, 52.5), (61.5, 52.5)]
PI_LEFT_Z = Z_LEFT - WALL - 2.5               # the left edge of the board (STEP x = 0)
PI_Y0 = Y_BOT + WALL + 18.0                   # STEP y = 0 (room under it for the HDMI plug)
PI_X0 = X_BACK + T_BACK + PI_STANDOFF         # the bottom of the board
PORTS = {"usbc": (11.2, -1.0, 3.2), "hdmi0": (25.8, -1.0, 3.0), "hdmi1": (39.2, -1.0, 3.0)}


def pi_to_mount(x, y, z):
    return (PI_X0 + z, PI_Y0 + y, PI_LEFT_Z - x)


def pi_port(name):
    x, y, z = PORTS[name]
    return pi_to_mount(x, y, 1.6 + z / 2)


def socket_xz():
    """The printer socket: under the flash drive, in front of the Ethernet port, near the right wall."""
    return X_BACK + T_BACK + 12.0, Z_RIGHT + WALL + 16.0


CLIP_SLOT_Y = S.CLIPS_Y                       # the two rail clips (shape.py), mount y


def back():
    """The back plate: inside the back opening of the body, flush with its back."""
    m = WALL + 0.3
    t = 2.0                                   # the plate; pads and bosses are thicker
    yb, yt = Y_BOT + m, Y_TOP - m
    zr, zl = Z_RIGHT + m, Z_LEFT - m
    p = box(X_BACK, X_BACK + t, yb, yt, zr, zl)
    p = fillet(p.edges().filter_by(Axis.X), R_EDGE - WALL - 0.3)
    # Ribs across the plate, on its inside.
    for y in (yb + 60.0, yb + 120.0):
        p = p + box(X_BACK + t - 0.01, X_BACK + t + 3.0, y - 1.0, y + 1.0, zr + 2, zl - 2)
    # Screws into the corner blocks: countersunk from the back.
    for y, z in corner_points():
        p = p - Pos(X_BACK + t / 2, y, z) * Rot(0, 90, 0) * Cylinder(HOLE[3] / 2, t + 2)
    # The Pi: four standoffs with captive M2.5 nuts in the back of the plate (the slot opens down).
    for hx, hy in PI_HOLES:
        x, y, z = pi_to_mount(hx, hy, 0)
        p = p + Pos(X_BACK + (t + PI_STANDOFF) / 2, y, z) * Rot(0, 90, 0) * Cylinder(3.2, t + PI_STANDOFF)
        p = p - Pos(X_BACK + (t + PI_STANDOFF) / 2, y, z) * Rot(0, 90, 0) * Cylinder(HOLE[2.5] / 2, t + PI_STANDOFF + 2)
    # The clip pads: the plate is 3.5 thick around each dovetail slot, and the slot and its window
    # (for the key to come in and out when the deck is lifted) go through.
    zc = S.RAIL_SLOT_Z
    k0, k1 = S.CLIP_KEY[0] / 2 + S.CLR, S.CLIP_KEY[1] / 2 + S.CLR
    for yc in CLIP_SLOT_Y:
        d0, d1 = yc - S.CLIP_L / 2 - S.CLIP_SLACK, yc + S.CLIP_L / 2 + S.CLIP_SLACK
        p = p + box(X_BACK, X_BACK + 3.5, d0 - S.CLIP_DROP - 4, d1 + 4, zc - k1 - 5, zc + k1 + 5)
        dov = Polygon((zc - k0, X_BACK - 0.01), (zc + k0, X_BACK - 0.01), (zc + k1, X_BACK + 3.51), (zc - k1, X_BACK + 3.51), align=None)
        slot = Location(Plane(origin=(0, d0, 0), x_dir=(0, 0, 1), z_dir=(0, 1, 0))) * extrude(Plane.XY * dov, amount=d1 - d0)
        p = p - slot
        p = p - box(X_BACK - 1, X_BACK + 4.5, d0 - S.CLIP_DROP, d0 + 0.5, zc - k1 - 0.2, zc + k1 + 0.2)
    return p


PARTS = [("body", body, "#1E1F24"), ("back", back, "#3A3D45")]


# ---------------------------------------------------------------- the FDM version: two colors

# The split between the navy shell and the orange L. The L is the back plate and the chin: the
# chin is the part of the case under a straight line across the face, just under the screen. The
# shell is the face, the top and the sides above that line; it holds the display.
SPLIT_FACE_V = -(BORDER + AA[1] + 6.0)       # on the face: 6 mm under the active area
SPLIT_Y = face_pt(0, SPLIT_FACE_V, 0)[1]     # the line is level: the chin is under this height


def halves():
    """(shell, chin): the body cut level at SPLIT_Y."""
    b = body()
    big = 600.0
    above = Pos(0, SPLIT_Y + big / 2, 0) * Box(big, big, big)
    return b & above, b - above


if MODE == "fdm":
    PARTS = [("top", lambda: halves()[0], "#2C3E73"), ("chin", lambda: halves()[1], "#E8762B"),
             ("back", back, "#E8762B")]


if __name__ == "__main__":
    print(f"face {WIDTH:.1f} x {FACE_LEN:.1f}; {Y_BOT + S.Y_BASE:.1f} to {Y_TOP + S.Y_BASE:.1f} mm over the chassis bottom; "
          f"depth {TOP_DEPTH:.0f} at the top, {face_x(Y_BOT) - X_BACK:.1f} at the bottom")
    for name, fn, _ in PARTS:
        sh = fn()
        bb = sh.bounding_box()
        print(f"{name}: {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x {bb.max.Z - bb.min.Z:.1f} mm, "
              f"{sh.volume / 1000:.1f} cm3, valid {sh.is_valid}")
