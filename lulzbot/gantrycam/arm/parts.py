"""Gantry camera arm: the printed parts and the context, in build123d. The frame is the gantry
frame of shape.py (origin at the nozzle tip with the head at X 293, x right, y back, z up)."""

import math

from build123d import Align, Box, Circle, Cylinder, Plane, Polygon, Pos, RegularPolygon, Rot, extrude

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

def teardrop_x(y, z, r, x0, x1):
    """A hole along x with its point to -z: up in the print of the upper arm (upside down).
    The plane has local x = world y, local y = world z, and the normal along world +x."""
    a = r / math.sqrt(2)
    prof = Circle(r) + Polygon((0, 0), (-a, -a), (0, -2 * a), (a, -a), align=None)   # counterclockwise
    pl = Plane(origin=(x0, y, z), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return extrude(pl * prof, x1 - x0)


def nut_slot(y, z, x0, x1):
    """A slot along x for an M3 nut on a bolt along y: NUT_T wide in y, NUT_AF high in z, with a
    45 degree roof at -z (up in the print of the upper arm)."""
    t, h = S.NUT_T / 2, S.NUT_AF / 2
    prof = Polygon((-t, h), (-t, -h), (0, -h - t), (t, -h), (t, h), align=None)
    pl = Plane(origin=(x0, y, z), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return extrude(pl * prof, x1 - x0)


def clamp_plate():
    """The plate on the left face of the X end, between its front edge and the raised part behind.
    It prints on its right face (x = FACE_X)."""
    part = box((S.FACE_X - S.PLATE_T, S.FACE_X), S.PLATE_Y, S.PLATE_Z)
    for z in S.SCREW_Z:
        part -= Pos(S.FACE_X - S.PLATE_T - 1, S.SCREW_Y, z) * Rot(0, 90, 0) * Cylinder(
            S.HOLE / 2, S.PLATE_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    # The lower head sinks in: the motor passes 9 mm left of the original head at the right end of X.
    part -= Pos(S.FACE_X - S.PLATE_T - 1, S.SCREW_Y, S.SCREW_Z[0]) * Rot(0, 90, 0) * Cylinder(
        S.CB_D / 2, S.CB_DEPTH + 1, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return part


def upper_arm():
    """The beam from the plate forward to the back of the upright, and the drop beside the inner face
    of the upright. It prints upside down: the top face (z = ARM_TOP) on the bed."""
    beam = box((S.ARM_X0, S.DROP_X[1]), (S.BEAM_Y0, S.BEAM_Y1), (S.BEAM_Z0, S.ARM_TOP))
    back = box((S.ARM_X0, S.FACE_X), (S.BEAM_Y1 - 1, S.PLATE_Y[1] + 0.2), (S.BEAM_Z0, S.ARM_TOP))
    drop = box(S.DROP_X, (S.BEAM_Y0, S.DROP_Y[1]), (S.DROP_Z0, S.ARM_TOP))
    front = box(S.DROP_X, (S.DROP_Y[0], S.BEAM_Y0 + 1), (S.DROP_Z0, S.DROP_Z1))
    part = beam + back + drop + front
    # The top of the front part slopes 45 degrees down to the front: no flat ceiling in the print,
    # and it stays under the inner corner bracket at Z max.
    run = S.BEAM_Y0 - S.DROP_Y[0]
    wedge = Polygon((S.DROP_Y[0] - 1, S.DROP_Z1 - run - 1), (S.BEAM_Y0, S.DROP_Z1),
                    (S.BEAM_Y0, S.DROP_Z1 + 1), (S.DROP_Y[0] - 1, S.DROP_Z1 + 1), align=None)  # counterclockwise
    pl = Plane(origin=(S.DROP_X[0] - 1, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    part -= extrude(pl * wedge, S.DROP_X[1] - S.DROP_X[0] + 2)
    # The pocket for the clamp plate (anti-turn), with POCKET_PLAY on each side.
    part -= box((S.FACE_X - S.PLATE_T - S.POCKET_PLAY, S.FACE_X + 1), (S.PLATE_Y[0] - S.POCKET_PLAY, S.PLATE_Y[1] + 1),
                (S.PLATE_Z[0], S.ARM_TOP + 1))
    # The upper screw: a hole through, and a counterbore from the left face.
    z = S.SCREW_Z[1]
    part -= teardrop_x(S.SCREW_Y, z, S.HOLE / 2, S.ARM_X0 - 1, S.FACE_X)
    part -= teardrop_x(S.SCREW_Y, z, S.CB_ARM / 2, S.ARM_X0 - 1, S.ARM_X0 + S.M3_HEAD_H)
    # The two joint bolts of the camera bracket: holes along y, nut slots from the left face.
    xc = sum(S.DROP_X) / 2
    for z in S.JOINT_Z:
        part -= teardrop_y(xc, z, S.JOINT_HOLE / 2, S.DROP_Y[0] - 1, S.NUT_Y + 6, up=-1)
        part -= nut_slot(S.NUT_Y, z, S.DROP_X[0] - 1, xc + S.NUT_AF / 2 + 0.4)
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
    """Fit test: the top 8 mm of the upper arm (the beam, the plate pocket, and the top of the
    drop). It sits on the top of the clamp plate. It checks the place of the screws front to back:
    the front of the beam is 0.5 mm behind the back face of the upright."""
    cut = box((-500, 500), (-500, 500), (S.ARM_TOP - 8, S.ARM_TOP + 1))
    return upper_arm() & cut


def fit_bracket_slice():
    """Fit test: the top 10 mm of the camera bracket (the tongue, the front wall and the rib). It
    checks the gap around the front of the upright."""
    return camera_bracket() & box((-500, 500), (-500, 500), (S.CAM_Z[1] - 10, S.CAM_Z[1] + 1))


# --- The context -------------------------------------------------------------------------------

def screws():
    """The two M3 socket heads: the lower one sunk in the plate, the upper one in the arm."""
    def head(x0, z):
        return Pos(x0, S.SCREW_Y, z) * Rot(0, 90, 0) * Cylinder(
            S.M3_HEAD_D / 2, S.M3_HEAD_H, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return [head(S.FACE_X - S.PLATE_T, S.SCREW_Z[0]), head(S.ARM_X0, S.SCREW_Z[1])]


def joint_hardware():
    """The two M3 x 30 socket screws from the front of the bracket into the drop, and their nuts in
    the slots of the drop. The screws go along y."""
    xc = sum(S.DROP_X) / 2
    screws_, nuts = [], []
    for z in S.JOINT_Z:
        head = Pos(xc, S.FRONT_Y[0], z) * Rot(90, 0, 0) * Cylinder(
            5.5 / 2, 3.0, align=(Align.CENTER, Align.CENTER, Align.MIN))
        shank = Pos(xc, S.FRONT_Y[0], z) * Rot(-90, 0, 0) * Cylinder(
            1.5, 30.0, align=(Align.CENTER, Align.CENTER, Align.MIN))
        screws_.append(head + shank)
        nut = Pos(xc, S.NUT_Y - 1.2, z) * Rot(-90, 0, 0) * extrude(RegularPolygon(5.5 / math.sqrt(3), 6), 2.4)
        nuts.append(nut - Pos(xc, S.NUT_Y - 2, z) * Rot(-90, 0, 0) * Cylinder(
            1.5, 4.0, align=(Align.CENTER, Align.CENTER, Align.MIN)))
    return screws_, nuts


def camera_screw():
    """The 1/4"-20 x 1/4" screw: the head in the counterbore on the back of the bracket wall, the
    thread through the slot into the base of the U bracket."""
    zc = S.LENS_Z
    head = Pos(S.SLOT_X, S.FRONT_Y[1] - S.SLOT_CB_DEPTH, zc) * Rot(-90, 0, 0) * Cylinder(
        9.5 / 2, S.SLOT_CB_DEPTH, align=(Align.CENTER, Align.CENTER, Align.MIN))
    shank = Pos(S.SLOT_X, S.FRONT_Y[1] - S.SLOT_CB_DEPTH, zc) * Rot(90, 0, 0) * Cylinder(
        6.35 / 2, S.WALL - S.SLOT_CB_DEPTH + 6.35, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return head + shank


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
    right end. From the measured clearances at the right end of X: the head comes to 9 mm left of
    the lower screw head (under HEAD_SPLIT_Z) and to 25 mm left of the upper screw head (over it).
    Its right side (the BLTouch mount tab) comes to 11 mm from the upright. Only the part of the
    head in front of the back of the X end face matters here."""
    y0, y1 = -250.0, S.FACE_Y[1] + 2.0
    low_x1 = S.FACE_X - S.M3_HEAD_H - S.LOWER_CLEAR + dx
    high_x1 = S.FACE_X - S.M3_HEAD_H - S.UPPER_CLEAR + dx
    low = box((low_x1 - 120.0, low_x1), (y0, y1), (0.0, S.HEAD_SPLIT_Z))
    high = box((high_x1 - 120.0, high_x1), (y0, y1), (S.HEAD_SPLIT_Z, 140.0))
    x1 = S.TAB_X1 + dx
    tab = box((x1 - 12.0, x1), (S.TAB_Y0, S.TAB_Y0 + 26.21), S.TAB_Z)
    mp = S.MOUNT_PLATE
    mount = box((mp[0][0] + dx, mp[0][1] + dx), mp[1], mp[2])
    bl = S.BLTOUCH
    bltouch = box((bl[0][0] + dx, bl[0][1] + dx), bl[1], bl[2])
    hot = Pos(dx, S.NOZZLE_Y_EST, 0) * Cylinder(2.5, 6, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return [("head under the lower screw", low), ("head at the upper screw", high), ("BLTouch tab", tab),
            ("BLTouch mount plate", mount), ("BLTouch", bltouch), ("nozzle", hot)]


def nozzle(dx=0.0):
    """The estimated nozzle tip, with the head dx from the right end."""
    return (dx, S.NOZZLE_Y_EST, 0.0)
