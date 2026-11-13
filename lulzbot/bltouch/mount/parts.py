"""BLTouch mount: the printed part and the context, in build123d. The frame is the head frame of
shape.py (origin at the nozzle tip, x right, y back, z up)."""

import math

from build123d import (Align, Box, Circle, Cylinder, Plane, Polygon, Pos, Rectangle, RegularPolygon, Rot,
                       extrude)

import shape as S


def box(xr, yr, zr):
    """A box from three (min, max) ranges."""
    return Pos(xr[0], yr[0], zr[0]) * Box(xr[1] - xr[0], yr[1] - yr[0], zr[1] - zr[0], align=None)


def teardrop_z(x, y, r, z0, z1):
    """A hole along z with its point to -y: the part prints with -y up."""
    a = r / math.sqrt(2)
    tip = Polygon((0, 0), (-a, -a), (0, -2 * a), (a, -a), align=None)   # counterclockwise: the face points to +z
    return Pos(x, y, z0) * extrude(Circle(r) + tip, z1 - z0)


def teardrop_x(y, z, r, x0, x1):
    """A hole along x with its point to -y."""
    t = teardrop_z(0, 0, r, 0, x1 - x0)          # along z from 0
    return Pos(x0, y, z) * Rot(0, 90, 0) * t     # z goes to x; y stays


def mount():
    """The printed mount in the head frame."""
    y0, y1 = S.SHELF_Y0, S.PART_Y1
    plate = box((S.MOTOR_X1, S.PLATE_X1), (S.PLATE_Y0, y1), (S.PLATE_Z0, S.PLATE_Z1))
    top = S.EARS_Z + S.SHELF_T
    shelf = box((S.PLATE_X1 - 0.5, S.SHELF_X1), (y0, y1), (S.EARS_Z, top))
    g = S.GUSSET
    gusset = extrude(Plane.XZ.offset(-y1) * Polygon((S.PLATE_X1 - 0.5, top - 0.5), (S.PLATE_X1 + g, top - 0.5),
                                                   (S.PLATE_X1 - 0.5, top + g)), y1 - y0)
    part = plate + shelf + gusset
    # The front motor screws: a clearance hole, and a counterbore for the head on the outer face.
    for y, z in S.FRONT_SCREWS:
        part -= teardrop_x(y, z, S.SCREW_HOLE / 2, S.MOTOR_X1 - 1, S.PLATE_X1 + 1)
        part -= teardrop_x(y, z, S.CB_D / 2, S.PLATE_X1 - S.CB_DEPTH, S.PLATE_X1 + 1)
    # The BLTouch screws: a hole through the shelf, and a nut pocket from the top.
    for y in S.BLT_HOLES:
        part -= teardrop_z(S.PIN_X, y, S.SCREW_HOLE / 2, S.EARS_Z - 1, top + 1)
        side = S.NUT_AF / math.sqrt(3)
        nut = RegularPolygon(side, 6, rotation=0)    # flats at +y and -y
        roof = Polygon((side / 2, -S.NUT_AF / 2 + 0.01), (-side / 2, -S.NUT_AF / 2 + 0.01),
                       (0, -S.NUT_AF / 2 - side / 2), align=None)   # 45 degree roof on the -y flat (up in print)
        part -= Pos(S.PIN_X, y, top - S.NUT_DEPTH) * extrude(nut + roof, S.NUT_DEPTH + 1)
    # The wires of the BLTouch go up through a diamond hole.
    d = S.CABLE_DIAG / math.sqrt(2)
    part -= Pos(S.PIN_X, S.PIN_Y, S.EARS_Z - 1) * extrude(Rot(0, 0, 45) * Rectangle(d, d), S.SHELF_T + 2)
    return part


def bltouch():
    """The BLTouch, pin up, under the shelf."""
    ears = box((S.PIN_X - S.BLT_EAR_W / 2, S.PIN_X + S.BLT_EAR_W / 2), (S.BLT_Y0, S.BLT_Y1),
               (S.EARS_Z - S.BLT_EAR_T, S.EARS_Z))
    body = box((S.PIN_X - S.BLT_BODY_D / 2, S.PIN_X + S.BLT_BODY_D / 2),
               (S.PIN_Y - S.BLT_BODY_W / 2, S.PIN_Y + S.BLT_BODY_W / 2), (S.BLT_GAP, S.EARS_Z))
    for y in S.BLT_HOLES:
        ears -= Pos(S.PIN_X, y, S.EARS_Z - 5) * Cylinder(1.6, 10, align=None)
    pin = Pos(S.PIN_X, S.PIN_Y, S.BLT_GAP - 1.0) * Cylinder(1.0, 1.0, align=None)
    return ears + body + pin


def screws():
    """The heads of the two longer front motor screws, and of the two BLTouch screws under the ears."""
    out = []
    for y, z in S.FRONT_SCREWS:
        x = S.PLATE_X1 - S.CB_DEPTH
        out.append(Pos(x, y, z) * Rot(0, 90, 0) * Cylinder(S.SCREW_HEAD_D / 2, S.SCREW_HEAD_H, align=None))
    for y in S.BLT_HOLES:
        z = S.EARS_Z - S.BLT_EAR_T - S.SCREW_HEAD_H
        out.append(Pos(S.PIN_X, y, z) * Cylinder(S.SCREW_HEAD_D / 2, S.SCREW_HEAD_H, align=None))
    return out


def context():
    """(name, shape, material): the motor, its rear screw heads, the blower, the X end and the nozzle."""
    m = box((S.MOTOR_X0, S.MOTOR_X1), (S.MOTOR_Y0, S.MOTOR_Y0 + S.MOTOR_SIDE),
            (S.MOTOR_Z0, S.MOTOR_Z0 + S.MOTOR_SIDE))
    out = [("motor", m, "dark")]
    for i, (y, z) in enumerate(S.REAR_SCREWS):
        h = Pos(S.MOTOR_X1, y, z) * Rot(0, 90, 0) * Cylinder(S.SCREW_HEAD_D / 2, S.SCREW_HEAD_H, align=None)
        out.append((f"rear screw {i}", h, "steel"))
    out.append(("blower", box(*S.BLOWER), "dark"))
    xe = S.MOTOR_X1 + S.X_END_GAP
    out.append(("X end", box((xe, xe + 30), (S.X_END_Y0, S.X_END_Y0 + 50), (-5, 90)), "frame"))
    nozzle = Pos(0, 0, 0) * Cylinder(2.5, 6, align=None) + Pos(0, 0, 6) * Box(16, 20, 12, align=(Align.CENTER, Align.CENTER, Align.MIN))
    out.append(("hot end", nozzle, "brass"))
    return out
