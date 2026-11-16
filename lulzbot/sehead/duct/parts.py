"""SE tool head duct, blower bracket and BLTouch bracket: the printed parts, the hardware and the
context, in build123d. The frame is the head frame of shape.py (origin at the nozzle tip, x right,
y back, z up)."""

import math

from build123d import Align, Box, Circle, Cylinder, Plane, Polygon, Pos, RegularPolygon, Rot, Vector, extrude

import cad
import shape as S


def box(xr, yr, zr):
    """A box from three (min, max) ranges."""
    return Pos(xr[0], yr[0], zr[0]) * Box(xr[1] - xr[0], yr[1] - yr[0], zr[1] - zr[0], align=None)


def _unit(v):
    v = Vector(*v)
    return v / v.length


def hole(p, axis, r, length, up):
    """A round hole from p along axis, with a 45 degree point toward the print direction up (a
    teardrop). up must not be parallel to axis."""
    a = _unit(axis)
    u = Vector(*up)
    u = _unit(u - a * u.dot(a))
    k = r / math.sqrt(2)
    tip = Polygon((0, 0), (k, -k), (r * math.sqrt(2), 0), (k, k), align=None)
    pl = Plane(origin=p, x_dir=u, z_dir=a)
    return extrude(pl * (Circle(r) + tip), length, dir=a)


def prism(p0, p1, profile, e0=0.0, e1=0.0):
    """A straight prism in the x-y plane from p0 to p1 (x, y), longer by e0 and e1 at the ends.
    profile: (s, z) points of the cross section, s across the line (to its left)."""
    d = _unit((p1[0] - p0[0], p1[1] - p0[1], 0))
    length = math.hypot(p1[0] - p0[0], p1[1] - p0[1]) + e0 + e1
    start = (p0[0] - d.X * e0, p0[1] - d.Y * e0, 0)
    left = Vector(-d.Y, d.X, 0)
    pl = Plane(origin=start, x_dir=left, z_dir=d)
    return extrude(pl * Polygon(*profile, align=None), length, dir=d)


def gable(w, z0, zw):
    """The cross section of a channel: w wide, the walls from z0 to zw, a 45 degree roof above."""
    return [(-w / 2, z0), (w / 2, z0), (w / 2, zw), (0, zw + w / 2), (-w / 2, zw)][::-1]


def rect(w, z0, z1):
    return [(-w / 2, z0), (-w / 2, z1), (w / 2, z1), (w / 2, z0)]


# --- The duct ---------------------------------------------------------------------------------

def ring_path():
    """The centre line of the ring: the right arm from the front, the back arm, the 45 degree turn
    round the BLTouch, the left arm to the front."""
    h = S.CH_W / 2 + S.WALL
    cx, cy = S.BLT_X[1], S.BLT_Y[0]           # the corner of the BLTouch body nearest the duct
    y_lc = cy - cx + S.RING_L - (h + S.CORNER_GAP) * math.sqrt(2)
    x_bc = S.RING_L + (S.RING_B - y_lc)
    return [(S.RING_R, S.LINK_Y[0] + S.CH_W / 2), (S.RING_R, S.RING_B), (x_bc, S.RING_B),
            (S.RING_L, y_lc), (S.RING_L, S.RING_FRONT + S.WALL + S.CH_W / 2)]


def path_prisms(pts, profile, h):
    """The prisms of a path. The free ends are longer by h; at a joint each prism is longer by
    h * tan(turn / 2): the outer corner closes, and no corner sticks out."""
    def turn(i):
        a = math.atan2(pts[i][1] - pts[i - 1][1], pts[i][0] - pts[i - 1][0])
        b = math.atan2(pts[i + 1][1] - pts[i][1], pts[i + 1][0] - pts[i][0])
        t = abs((b - a + math.pi) % (2 * math.pi) - math.pi)
        return h * math.tan(t / 2) + 0.01
    out = None
    n = len(pts)
    for i in range(n - 1):
        e0 = h if i == 0 else turn(i)
        e1 = h if i == n - 2 else turn(i + 1)
        seg = prism(pts[i], pts[i + 1], profile, e0, e1)
        out = seg if out is None else out + seg
    return out


def halfspace(point, normal):
    """The space on the side of the plane through point to which normal points."""
    pl = Plane(origin=point, z_dir=normal)
    return pl.location * Box(400, 400, 200, align=(Align.CENTER, Align.CENTER, Align.MIN))


def mitred_prisms(pts, profile, e_end):
    """The prisms of a path, cut at the bisector plane of each joint: the roofs of two prisms meet
    in one plane, and no edge points down. The free ends are longer by e_end."""
    n = len(pts)
    dirs = [_unit((pts[i + 1][0] - pts[i][0], pts[i + 1][1] - pts[i][1], 0)) for i in range(n - 1)]
    out = None
    for i in range(n - 1):
        seg = prism(pts[i], pts[i + 1], profile, e_end if i == 0 else 20.0, e_end if i == n - 2 else 20.0)
        if i > 0:
            seg = seg & halfspace((pts[i][0], pts[i][1], -50), dirs[i - 1] + dirs[i])
        if i < n - 2:
            seg = seg & halfspace((pts[i + 1][0], pts[i + 1][1], -50), -(dirs[i] + dirs[i + 1]))
        out = seg if out is None else out + seg
    return out


def link_channels():
    """The y ranges of the two channels under the motor."""
    w = (S.OUTLET_IN_Y[1] - S.LINK_Y[0] - S.WALL) / 2
    return w, [(S.LINK_Y[0], S.LINK_Y[0] + w), (S.OUTLET_IN_Y[1] - w, S.OUTLET_IN_Y[1])]


def plenum_box():
    return ((S.MOTOR_X1 + 0.6, S.BLOWER_BOX[0][1]), (S.OUTLET_IN_Y[0] - 2.5, S.XEND_FRONT_Y - 1.0))


def duct_shell():
    h = S.CH_W / 2 + S.WALL
    pts = ring_path()
    prof = rect(2 * h, S.DUCT_Z0, S.DUCT_Z1)
    shell = path_prisms(pts, prof, h)
    # The link under the motor, the plenum under the outlet, the block at the front right.
    px, py = plenum_box()
    shell += box((S.RING_R, px[0] + 1), (S.LINK_Y[0] - S.WALL, S.OUTLET_IN_Y[1] + S.WALL), (S.DUCT_Z0, S.DUCT_Z1))
    shell += box(px, py, (S.DUCT_Z0, S.DUCT_Z1))
    # The front of the right arm goes on to the front of the strut: the strut stands on it.
    shell += box((S.OPEN_X[1], S.RING_R + S.CH_W / 2 + S.WALL), (S.FRONT_TAB_Y[0], S.LINK_Y[0]), (S.DUCT_Z0, S.DUCT_Z1))
    return shell


def duct_cavity():
    pts = ring_path()
    prof = gable(S.CH_W, S.CH_Z0, S.DUCT_Z1 - S.WALL - S.CH_W / 2)
    cav = mitred_prisms(pts, prof, S.CH_W / 2)
    w, ys = link_channels()
    lprof = gable(w, S.CH_Z0, S.DUCT_Z1 - S.WALL - w / 2)
    for y0, y1 in ys:
        yc = (y0 + y1) / 2
        cav += prism((S.RING_R, yc), (sum(S.OUTLET_IN_X) / 2, yc), lprof)
    # The plenum: the inside of the lip, open to the blower outlet at the top.
    lx, ly = lip_inside()
    cav += box(lx, ly, (S.CH_Z0, S.DUCT_Z1 + S.INLET_LIP + 1))
    return cav


def lip_outside():
    return ((S.OUTLET_IN_X[0] + S.LIP_PLAY, S.OUTLET_IN_X[1] - S.LIP_PLAY),
            (S.OUTLET_IN_Y[0] + S.LIP_PLAY, S.OUTLET_IN_Y[1] - S.LIP_PLAY))


def lip_inside():
    (x0, x1), (y0, y1) = lip_outside()
    return (x0 + S.LIP_T, x1 - S.LIP_T), (y0 + S.LIP_T, y1 - S.LIP_T)


def outlets():
    """(start, direction) of each outlet: from inside the channel, through the inner wall, toward
    TARGET."""
    t = Vector(*S.TARGET)
    z = S.CH_Z0 + 2.0
    out = []
    xr = S.OPEN_X[1] + S.WALL + 2.0
    for y in (-12.0, -5.0, 2.0, 9.0):
        out.append(Vector(xr, y, z))
    yb = S.OPEN_Y[1] + S.WALL + 2.0
    for x in (14.0, 7.0, 0.0, -6.0):
        out.append(Vector(x, yb, z))
    xl = S.OPEN_X[0] - S.WALL - 2.0
    for y in (-10.0, -3.0, 4.0):
        out.append(Vector(xl, y, z))
    return [(p, _unit(t - p)) for p in out]


def outlet_cut(p, d):
    """A channel from p along d: OUTLET_W wide, its walls OUTLET_H high, a 45 degree roof."""
    up = Vector(0, 0, 1)
    u = _unit(up - d * up.dot(d))
    w, h = S.OUTLET_W, S.OUTLET_H
    prof = Polygon((-h / 2, -w / 2), (h / 2, -w / 2), (h / 2 + w / 2, 0), (h / 2, w / 2), (-h / 2, w / 2), align=None)
    pl = Plane(origin=p - d * 1.0, x_dir=u, z_dir=d)
    return extrude(pl * prof, 12.0, dir=d)


def duct():
    """The U round the hot end, the link under the motor, the plenum under the blower outlet, the
    lip into the outlet, the front tab and the post. It prints on its bottom face (z = DUCT_Z0)."""
    part = duct_shell()
    # The front tab between the Aero and the motor, on the front bottom motor screw, and the strut
    # under the motor to the right arm. Left of the right arm the underside slopes at 45 degrees up
    # from the top of the duct: no overhang, and the hot end stays in view from the front.
    tab = box(S.FRONT_TAB_X, S.FRONT_TAB_Y, (S.DUCT_Z0, S.FRONT_TAB_LOW[1]))
    tab += box(S.FRONT_TAB_X, (S.FRONT_TAB_Y[0], S.FRONT_TAB_LOW[0]), (S.DUCT_Z0, S.FRONT_TAB_Z[1]))
    tab += box((S.FRONT_TAB_X[0], S.RING_R + S.CH_W / 2 + S.WALL), S.FRONT_TAB_Y, (S.DUCT_Z0, S.STRUT_Z1))
    tab -= _wedge_x(S.OPEN_X[1])                          # left of the right arm: a 45 degree slope
    part += tab
    # The post at the back left, under the head of the long screw of the Aero.
    part += box(S.POST_X, S.POST_Y, (S.DUCT_Z0, S.POST_Z[1]))
    # The lip into the blower outlet (the cavity makes it hollow).
    part += box(*lip_outside(), (S.DUCT_Z1 - 1, S.DUCT_Z1 + S.INLET_LIP))
    part -= duct_cavity()
    for p, d in outlets():
        part -= outlet_cut(p, d)
    # The screw holes, with the point up.
    y, z = S.FRONT_BOLTS[0]
    part -= hole((S.FRONT_TAB_X[0] - 1, y, z), (1, 0, 0), S.HOLE / 2, S.FRONT_TAB_X[1] - S.FRONT_TAB_X[0] + 2, (0, 0, 1))
    y, z = S.POST_SCREW
    part -= hole((S.POST_X[0] - 1, y, z), (1, 0, 0), S.HOLE / 2, S.POST_X[1] - S.POST_X[0] + 2, (0, 0, 1))
    return part


def _wedge_x(x1):
    """The space under a 45 degree slope that rises from the duct top at x1 toward -x."""
    z0, big = S.DUCT_Z1, 60.0
    pl = Plane(origin=(0, big, 0), x_dir=(1, 0, 0), z_dir=(0, -1, 0))   # local (x, z)
    poly = Polygon(*_ccw([(x1, z0 - big), (x1, z0), (x1 - big, z0 + big), (x1 - big, z0 - big)]), align=None)
    return extrude(pl * poly, 2 * big, dir=(0, -1, 0))


def fit_lip():
    """The fit test of the lip: the lip on a thin frame. Push it into the blower outlet."""
    (x0, x1), (y0, y1) = lip_outside()
    frame = box((x0 - 3, x1 + 3), (y0 - 3, y1 + 3), (S.DUCT_Z1 - 1.5, S.DUCT_Z1))
    frame += box((x0, x1), (y0, y1), (S.DUCT_Z1 - 1, S.DUCT_Z1 + S.INLET_LIP))
    lx, ly = lip_inside()
    return frame - box(lx, ly, (S.DUCT_Z1 - 2, S.DUCT_Z1 + S.INLET_LIP + 1))


# --- The blower bracket ------------------------------------------------------------------------

def blower_bracket():
    """The plate on the end cap and the stiff front flange. It prints on its outer face
    (x = BLOWER_X0): the countersinks and the low ear nut open at the bed and at the top."""
    x0, x1 = S.MOTOR_X1, S.BLOWER_X0
    part = box((x0, x1), S.BRK_Y, (S.BLOWER_Z0, S.BRK_Z[1]))
    part += box((S.FLANGE_X0, x1), (S.BRK_Y[0], S.BRK_Y[0] + S.FLANGE_T), (S.BLOWER_Z0, S.BRK_Z[1]))
    # The boss of the low ear, on the back of the plate, and a rib from it to the flange.
    y, z = S.EAR_LOW
    part += Pos(S.BOSS_X0, y, z) * Rot(0, 90, 0) * Cylinder(S.BOSS_D / 2, x1 - S.BOSS_X0, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part += box((S.BOSS_X0, x0), (S.BRK_Y[0], y), (z - 2.0, z + 2.0))
    # The two front motor screws: countersunk from the outer face.
    for y, z in S.FRONT_BOLTS:
        part -= Pos(x0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, S.BRACKET_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
        part -= Pos(x1 + 0.01, y, z) * Rot(0, -90, 0) * _cone(S.CSK_D / 2)
    # The top ear: a square nut in the back face, the screw from the blower side.
    y, z = S.EAR_TOP
    part -= Pos(x0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, S.BRACKET_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part -= box((x0 - 1, x0 + S.SQ_NUT_T), (y - S.SQ_NUT / 2, y + S.SQ_NUT / 2), (z - S.SQ_NUT / 2, z + S.SQ_NUT / 2))
    # The low ear: through the boss, a hex nut in the back face of the boss.
    y, z = S.EAR_LOW
    part -= Pos(S.BOSS_X0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, x1 - S.BOSS_X0 + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part -= Pos(S.BOSS_X0 - 1, y, z) * Rot(0, 90, 0) * extrude(RegularPolygon(S.NUT_AF / math.sqrt(3), 6), S.NUT_DEPTH + 1)
    return part


def _cone(r):
    """A 90 degree countersink cone: the wide end (radius r) at z = 0, the point at z = r."""
    from build123d import Cone
    return Cone(r, 0.01, r, align=(Align.CENTER, Align.CENTER, Align.MIN))


# --- The BLTouch bracket -----------------------------------------------------------------------

def _fan_frame():
    """The centre of the heat sink fan in (y, z), the unit vector along its back bottom edge, and the
    unit vector out of that edge."""
    by, bz = S.FAN_BACK
    ly, lz = S.FAN_BOTTOM
    cy, cz = ly, bz                                        # the fan is turned 45 degrees
    along = _unit((by - ly, bz - lz, 0))
    out = _unit(((by + ly) / 2 - cy, (bz + lz) / 2 - cz, 0))
    return (cy, cz), along, out


def _yz_plate(points, x0, x1):
    """A plate from x0 to x1 with the outline points (y, z)."""
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    poly = Polygon(*points, align=None)
    return extrude(pl * poly, x1 - x0)


def bltouch_bracket():
    """The pads under two heat sink fan screws, the strip between them outside the fan, the block
    to the shelf, and the shelf over the BLTouch ears. It prints on its back face (y = SHELF_Y1)."""
    x0, x1 = S.FAN_X0 - S.PAD_T, S.FAN_X0
    (cy, cz), along, out = _fan_frame()
    # The strip outside the back bottom edge of the fan, from pad to pad, and the bottom pad. The
    # outlines have their sides along the edge and across it: in the print every face toward the
    # bed slopes at 45 degrees, and no corner points down.
    by, bz = S.FAN_BACK
    ly, lz = S.FAN_BOTTOM
    edge = math.hypot(by - cy, lz - cz) / math.sqrt(2)     # the screw line: 16.25 from the centre
    length = math.hypot(by - ly, bz - lz)

    def at(t, s):                                          # t along the edge from the bottom screw, s out of it
        return (ly + along.X * t + out.X * s, lz + along.Y * t + out.Y * s)
    s0 = 20.0 - edge                                       # the edge of the fan frame (40 mm fan)
    r = S.PAD_D / 2
    strip = [at(-r, s0), at(length, s0), at(length, s0 + S.STRIP_W), at(-r, s0 + S.STRIP_W)]
    pad = [at(-r, -r), at(r, -r), at(r, s0 + S.STRIP_W), at(-r, s0 + S.STRIP_W)]
    part = _yz_plate(_ccw(strip), x0, x1) + _yz_plate(_ccw(pad), x0, x1)
    # The block from the back pad to the shelf, and the shelf.
    part += box((S.BLOCK_X0, x1), (S.FAN_BACK[0] - S.PAD_D / 2, S.SHELF_Y[1]), (S.EARS_Z, S.FAN_BACK[1] + S.PAD_D / 2))
    part += box(S.SHELF_X, S.SHELF_Y, (S.EARS_Z, S.SHELF_Z1))
    # The fan screws: holes along x, the point to -y (up in the print); a counterbore through the
    # block for the head of the back screw.
    for y, z in (S.FAN_BACK, S.FAN_BOTTOM):
        part -= hole((S.BLOCK_X0 - 1, y, z), (1, 0, 0), S.HOLE / 2, x1 - S.BLOCK_X0 + 2, (0, -1, 0))
    y, z = S.FAN_BACK
    part -= hole((S.BLOCK_X0 - 1, y, z), (1, 0, 0), 3.4, x0 - S.BLOCK_X0 + 1, (0, -1, 0))
    # The BLTouch: two screws down through the shelf and the ears (nuts under the ears), and the
    # diamond hole for the wires.
    for x in S.BLT_HOLES:
        part -= hole((x, S.PIN_Y, S.EARS_Z - 1), (0, 0, 1), S.HOLE / 2, S.SHELF_Z1 - S.EARS_Z + 2, (0, -1, 0))
    c = S.CABLE_DIAG / 2
    pl = Plane(origin=(S.PIN_X, S.PIN_Y, S.EARS_Z - 1), x_dir=(1, 0, 0), z_dir=(0, 0, 1))
    part -= extrude(pl * Polygon((c, 0), (0, c), (-c, 0), (0, -c), align=None), S.SHELF_Z1 - S.EARS_Z + 2)
    return part


def _ccw(pts):
    a = sum(pts[i][0] * pts[(i + 1) % len(pts)][1] - pts[(i + 1) % len(pts)][0] * pts[i][1] for i in range(len(pts)))
    return pts if a > 0 else pts[::-1]


# --- The hardware and the BLTouch --------------------------------------------------------------

def bltouch():
    """The BLTouch Smart, pin up: the body, the ears with round ends."""
    body = box(S.BLT_X, S.BLT_Y, (S.BLT_GAP, S.EARS_Z - S.BLT_EAR_T))
    r = S.BLT_EAR_W / 2
    half = S.BLT_FLANGE_L / 2 - r
    ears = box((S.PIN_X - half, S.PIN_X + half), (S.PIN_Y - r, S.PIN_Y + r), (S.EARS_Z - S.BLT_EAR_T, S.EARS_Z))
    for x in (S.PIN_X - half, S.PIN_X + half):
        ears += Pos(x, S.PIN_Y, S.EARS_Z - S.BLT_EAR_T) * Cylinder(r, S.BLT_EAR_T, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part = body + ears
    for x in S.BLT_HOLES:
        part -= Pos(x, S.PIN_Y, S.EARS_Z - S.BLT_EAR_T - 1) * Cylinder(1.6, S.BLT_EAR_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return part


def _screw(p, axis, length, head_d=5.5, head_h=3.0, d=3.0):
    """A socket head screw: the head at p, the shank along axis."""
    a = _unit(axis)
    pl = Plane(origin=p, z_dir=a)
    head = extrude(pl * Circle(head_d / 2), -head_h)
    shank = extrude(pl * Circle(d / 2), length)
    return head + shank


def _csk_screw(p, axis, length):
    """A countersunk screw: the top of the head at p, the cone and the shank along axis."""
    from build123d import Cone
    a = _unit(axis)
    pl = Plane(origin=p, z_dir=a)
    head = pl * Cone(3.36, 1.5, 1.86, align=(Align.CENTER, Align.CENTER, Align.MIN))
    shank = extrude(pl * Circle(1.5), length)
    return head + shank


def bracket_screws():
    """The two front motor screws (countersunk) and the two blower screws."""
    out = [_csk_screw((S.BLOWER_X0, y, z), (-1, 0, 0), 20) for y, z in S.FRONT_BOLTS]
    x_out = S.BLOWER_X0 + S.BLOWER_T
    y, z = S.EAR_TOP
    out.append(_screw((x_out, y, z), (-1, 0, 0), S.EAR_SCREW))
    y, z = S.EAR_LOW
    out.append(_screw((x_out, y, z), (-1, 0, 0), S.LOW_SCREW))
    return out


def bltouch_hardware():
    """The two fan screws (button heads, longer), and the two BLTouch screws and nuts."""
    x0 = S.FAN_X0 - S.PAD_T
    out = [_screw((x0, y, z), (1, 0, 0), S.FAN_SCREW, 5.7, 1.65) for y, z in (S.FAN_BACK, S.FAN_BOTTOM)]
    for x in S.BLT_HOLES:
        out.append(_screw((x, S.PIN_Y, S.SHELF_Z1), (0, 0, -1), S.BLT_SCREW))
    nuts = [Pos(x, S.PIN_Y, S.EARS_Z - S.BLT_EAR_T - 2.4) * extrude(RegularPolygon(5.5 / math.sqrt(3), 6), 2.4)
            for x in S.BLT_HOLES]
    return out, nuts


# --- The context from the CAD ------------------------------------------------------------------

def head_parts():
    """(name, shape) of the SE head parts that stay: the Aero, the mount, the motor, the heat sink fan."""
    return [(k, cad.se(k)) for k in ("aero", "mount", "motor", "hs_fan")]


def blower():
    """The CAD blower at its new place."""
    s = cad.se("blower")
    m = S.BLOWER_MOVE
    # X -> z, Y -> -x, Z -> -y: the plane maps local x to world z and local z to world -y.
    pl = Plane(origin=m, x_dir=(0, 0, 1), z_dir=(0, -1, 0))
    return pl.location * s


def connector_room():
    """The motor connector and the room for its plug and cable."""
    (x0, x1), (y0, y1), (z0, z1) = S.CONNECTOR
    return box((x0, x1), (y0, y1), (z0 - S.PLUG_ROOM, z1))


def left_x_end():
    """The left X end, at X home (the head at X min)."""
    xs = cad._x_axis()
    return [cad._carriage_place(xs[i], -S.X_HOME_SHIFT) for i in (2, 11, 13, 14, 15, 16, 19, 26, 27, 28, 32, 36)]
