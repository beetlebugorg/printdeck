"""Gantry camera arm: the printed parts and the context, in build123d. The frame is the gantry
frame of shape.py (origin at the nozzle tip with the head at X 293, x right, y back, z up)."""

import math

from build123d import Align, Box, Circle, Cylinder, Polygon, Pos, RegularPolygon, Rot, extrude

import shape as S


def box(xr, yr, zr):
    """A box from three (min, max) ranges."""
    return Pos(xr[0], yr[0], zr[0]) * Box(xr[1] - xr[0], yr[1] - yr[0], zr[1] - zr[0], align=None)


def teardrop_y(x, z, r, y0, y1, up=1):
    """A hole along y with its point to +z (up=1) or -z (up=-1): the part prints with that side up.
    Rot(-90, 0, 0) turns the local +y of the outline to world -z, and the local +z to world +y."""
    a = r / math.sqrt(2)
    tip = Polygon((0, 0), (a, a), (0, 2 * a), (-a, a), align=None)
    prof = Circle(r) + tip                         # the point at local +y: world -z
    return Pos(x, y0, z) * Rot(-90, 0, 0) * extrude(prof if up < 0 else Rot(0, 0, 180) * prof, y1 - y0)


# --- The printed parts -------------------------------------------------------------------------

def clamp_plate():
    """The plate in the pocket of the X end face. It prints flat on its back face (y = FACE_Y)."""
    x0 = S.SCREW_X - S.PLATE_W / 2
    part = box((x0, x0 + S.PLATE_W), (S.FACE_Y - S.PLATE_T, S.FACE_Y), S.PLATE_Z)
    for i, z in enumerate(S.SCREW_Z):
        part -= Pos(S.SCREW_X, S.FACE_Y + 1, z) * Rot(90, 0, 0) * Cylinder(S.HOLE / 2, S.PLATE_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    # The lower head sinks in: the motor passes 9 mm in front of the screw at the right end of X.
    z = S.SCREW_Z[0]
    part -= Pos(S.SCREW_X, S.FACE_Y - S.PLATE_T - 1, z) * Rot(-90, 0, 0) * Cylinder(S.CB_D / 2, S.CB_DEPTH + 1, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return part


def upper_arm():
    """The beam above the motor and the drop beside the upright. It prints upside down: the top face
    (z = ARM_TOP) on the bed. All the walls are then vertical."""
    beam = box((S.BEAM_X0, S.DROP_X[1]), (S.BEAM_Y0, S.BEAM_Y1), (S.BEAM_Z0, S.ARM_TOP))
    drop = box(S.DROP_X, (S.BEAM_Y0, S.BEAM_Y1), (S.DROP_Z0, S.ARM_TOP))
    part = beam + drop
    # The pocket for the clamp plate (anti-turn), 0.2 mm play on each side.
    x0 = S.SCREW_X - S.PLATE_W / 2 - 0.2
    part -= box((x0, x0 + S.PLATE_W + 0.4), (S.POCKET, S.BEAM_Y1 + 1), (S.PLATE_Z[0], S.ARM_TOP + 1))
    # The upper screw: a hole through, and a counterbore from the front down to ARM_UNDER_HEAD.
    z = S.SCREW_Z[1]
    part -= teardrop_y(S.SCREW_X, z, S.HOLE / 2, S.BEAM_Y0 - 1, S.BEAM_Y1 + 1, up=-1)
    part -= teardrop_y(S.SCREW_X, z, S.CB_ARM / 2, S.BEAM_Y0 - 1, S.POCKET - S.ARM_UNDER_HEAD, up=-1)
    # The two joint bolts of the camera bracket: holes along y, nut pockets at the back.
    for z in S.JOINT_Z:
        xc = sum(S.DROP_X) / 2
        part -= teardrop_y(xc, z, S.JOINT_HOLE / 2, S.BEAM_Y0 - 1, S.BEAM_Y1 + 1, up=-1)
        side = S.NUT_AF / math.sqrt(3)
        # A hexagon with flats left and right, and a 45 degree roof over the vertex at local -y.
        # Rot(90, 0, 0) turns local -y to world -z: up in the print (the arm prints upside down).
        nut = Rot(0, 0, 30) * RegularPolygon(side, 6)
        c, h2 = S.NUT_AF / 2, side / 2
        nut += Polygon((-c, -h2), (0, -h2 - c), (c, -h2), align=None)   # counterclockwise
        part -= Pos(xc, S.BEAM_Y1 + 0.01, z) * Rot(90, 0, 0) * extrude(nut, S.NUT_DEPTH)
    return part


def camera_bracket():
    """The bracket around the front of the upright. It prints on its bottom face (z = CAM_Z[0])."""
    tongue = box(S.DROP_X, S.TONGUE_Y, S.CAM_Z)
    front = box(S.FRONT_X, S.FRONT_Y, S.CAM_Z)
    rib = box(S.RIB_X, S.RIB_Y, S.CAM_Z)
    part = tongue + front + rib
    # The joint bolts: holes along y through the tongue, heads on the front face.
    xc = sum(S.DROP_X) / 2
    for z in S.JOINT_Z:
        part -= teardrop_y(xc, z, S.JOINT_HOLE / 2, S.FRONT_Y[0] - 1, S.TONGUE_Y[1] + 1)
    # The camera slot: through the front wall, with a counterbore for the head on the back face.
    zc, h = sum(S.SLOT_Z) / 2, S.SLOT_Z[1] - S.SLOT_Z[0]
    slot = Pos(S.SLOT_X, S.FRONT_Y[0] - 1, zc) * Rot(-90, 0, 0) * extrude(_stadium(S.SLOT_W, h), S.WALL + 2)
    cb = Pos(S.SLOT_X, S.FRONT_Y[1] - S.SLOT_CB_DEPTH, zc) * Rot(-90, 0, 0) * extrude(
        _stadium(S.SLOT_CB_W, h), S.SLOT_CB_DEPTH + 1)
    part -= slot + cb
    return part


def _stadium(w, h):
    """A vertical slot outline in the local xy plane: width w, the end centres h apart along y. The
    bottom end is round, and the top end has a 45 degree point, so it prints with no support.
    After Rot(-90, 0, 0), local -y is world +z: the point is at local -y."""
    from build123d import Rectangle
    r = w / 2
    return Rectangle(w, h) + Pos(0, h / 2) * Circle(r) + Polygon((-r, -h / 2), (0, -h / 2 - r), (r, -h / 2), align=None)


def fit_arm_slice():
    """Fit test: the top 8 mm of the upper arm (the beam with the plate pocket, and the top of the
    drop). It sits on the top of the clamp plate. It checks the beam length to the upright and the
    space to the belt strip. The screw hole is lower: the clamp plate checks the screws."""
    cut = box((-500, 500), (-500, 500), (S.ARM_TOP - 8, S.ARM_TOP + 1))
    return upper_arm() & cut


def fit_bracket_slice():
    """Fit test: the top 10 mm of the camera bracket (the tongue, the front wall and the rib). It
    checks the gap around the front of the upright."""
    return camera_bracket() & box((-500, 500), (-500, 500), (S.CAM_Z[1] - 10, S.CAM_Z[1] + 1))


# --- The context -------------------------------------------------------------------------------

def screws():
    """The two M3 socket heads: the lower one sunk in the plate, the upper one in the beam."""
    out = []
    z0 = S.SCREW_Z[0]
    out.append(Pos(S.SCREW_X, S.FACE_Y - S.PLATE_T + S.CB_DEPTH, z0) * Rot(90, 0, 0) * Cylinder(S.M3_HEAD_D / 2, S.M3_HEAD_H, align=(Align.CENTER, Align.CENTER, Align.MIN)))
    z1 = S.SCREW_Z[1]
    y = S.POCKET - S.ARM_UNDER_HEAD
    out.append(Pos(S.SCREW_X, y, z1) * Rot(90, 0, 0) * Cylinder(S.M3_HEAD_D / 2, S.M3_HEAD_H, align=(Align.CENTER, Align.CENTER, Align.MIN)))
    return out


def pivot_point():
    """The pivot of the U bracket: CAM_PIVOT_H in front of the base, at the height of the screw."""
    return (S.SLOT_X, S.FRONT_Y[0] - S.CAM_PIVOT_H, S.LENS_Z)


def camera(target=None):
    """The B0561 in its U bracket. The base is on the front face of the bracket, on the 1/4"-20
    screw; the pivot axis is vertical. The case turns on the pivot until the lens looks at target
    (the nozzle at the middle of the bed: 150 mm left of the right end)."""
    target = target or nozzle(-150.0)
    px, py, pz = pivot_point()
    w, hgt, d = S.CAM_CASE
    case = box((px - d / 2, px + d / 2), (py - w / 2, py + w / 2), (pz - hgt / 2, pz + hgt / 2))
    case += Pos(px - d / 2, py, pz) * Rot(0, -90, 0) * Cylinder(7.78, 3.8, align=(Align.CENTER, Align.CENTER, Align.MIN))
    yaw = math.degrees(math.atan2(target[1] - py, target[0] - px)) - 180.0
    case = Pos(px, py, pz) * Rot(0, 0, yaw) * Pos(-px, -py, -pz) * case
    y0 = S.FRONT_Y[0]
    base = Pos(px, y0, pz) * Rot(90, 0, 0) * Cylinder(S.CAM_BASE_D / 2, 1.4, align=(Align.CENTER, Align.CENTER, Align.MIN))
    legs = None
    for sgn in (-1, 1):
        z0 = pz + sgn * (hgt / 2 + 0.2)
        leg = box((px - 5, px + 5), (py - 6, y0 - 1.4), (min(z0, z0 + sgn * 1.4), max(z0, z0 + sgn * 1.4)))
        legs = leg if legs is None else legs + leg
    cross = box((px - 5, px + 5), (y0 - 2.8, y0 - 1.4), (pz - hgt / 2 - 1.6, pz + hgt / 2 + 1.6))
    return case, base + legs + cross


def lens_point(target=None):
    target = target or nozzle(-150.0)
    px, py, pz = pivot_point()
    a = math.atan2(target[1] - py, target[0] - px)
    r = S.CAM_CASE[2] / 2 + 3.8
    return (px + r * math.cos(a), py + r * math.sin(a), pz)


def head(dx=0.0):
    """(name, shape) of the space that the tool head fills near the arm, with the head dx from the
    right end. From the measured clearances at the right end of X: in front of the X end face the
    head comes to 9 mm from the lower screw head (under HEAD_SPLIT_Z) and to 25 mm from the upper
    screw head (over it). Its right side (the BLTouch mount tab) comes to 11 mm from the upright."""
    x1 = S.UP_X0 - 11.0 + dx
    x0 = x1 - 120.0
    low = box((x0, x1), (-250.0, S.FACE_Y - S.M3_HEAD_H - S.LOWER_CLEAR), (0.0, S.HEAD_SPLIT_Z))
    high = box((x0, x1), (-250.0, S.FACE_Y - S.M3_HEAD_H - S.UPPER_CLEAR), (S.HEAD_SPLIT_Z, 140.0))
    tab = box((x1 - 12.0, x1), (S.UP_Y0 + 8.0, S.FACE_Y), S.TAB_Z)
    hot = Pos(x1 - 77.0, S.NOZZLE_Y_EST, 0) * Cylinder(2.5, 6, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return [("head under the lower screw", low), ("head at the upper screw", high), ("BLTouch tab", tab),
            ("nozzle", hot)]


def nozzle(dx=0.0):
    """The estimated nozzle tip, with the head dx from the right end."""
    return (S.UP_X0 - 11.0 - 77.0 + dx, S.NOZZLE_Y_EST, 0.0)


def x_end():
    """The X end front: the face with the two screws, the raised belt strip left of it, the step
    right of it, and the body behind. Only the face and the strip are measured."""
    f0 = S.SCREW_X - S.FACE_W / 2
    f1 = f0 + S.FACE_W
    body = box((f0 - S.STRIP_W, f1 + 8), (S.FACE_Y, S.FACE_Y + 40), S.FACE_Z)
    strip = box((f0 - S.STRIP_W, f0), (S.FACE_Y - S.STRIP_PROUD, S.FACE_Y), S.FACE_Z)
    return body + strip


def upright(z_shift=0.0):
    """The front right upright (2020, with a slot in each face), and the corner plates as no-go
    boxes on its front face, for the gantry at Z = z_shift. The frame moves by -Z in the gantry
    frame."""
    zr = (-200 - z_shift, 600 - z_shift)
    up = box((S.UP_X0, S.UP_X0 + S.UP_W), (S.UP_Y0, S.UP_Y0 + S.UP_W), zr)
    for cx, cy, w, d in ((S.UP_X0 + 10, S.UP_Y0, 6.2, 6), (S.UP_X0 + 10, S.UP_Y0 + 20, 6.2, 6),
                         (S.UP_X0, S.UP_Y0 + 10, 6, 6.2), (S.UP_X0 + 20, S.UP_Y0 + 10, 6, 6.2)):
        up -= box((cx - w / 2, cx + w / 2), (cy - d / 2, cy + d / 2), (zr[0] - 1, zr[1] + 1))
    px = (S.UP_X0 - S.PLATE_REACH, S.UP_X0 + S.UP_W)
    py = (S.UP_Y0 - S.PLATE_PROUD, S.UP_Y0)
    top = box(px, py, (S.TOP_PLATE_Z - z_shift, 600 - z_shift))
    bottom = box(px, py, (-200 - z_shift, S.BOTTOM_PLATE_Z - z_shift))
    return up, top, bottom
