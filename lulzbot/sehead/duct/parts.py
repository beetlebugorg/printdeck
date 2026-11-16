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
    """The centre line of the channel: from the plenum under the blower outlet, left under the motor,
    45 degrees back to the short right arm, the back arm, the 45 degree turn round the BLTouch, and
    the left arm to the front."""
    h = S.CH_W / 2 + S.WALL
    cx, cy = S.BLT_X[1], S.BLT_Y[0]           # the corner of the BLTouch body nearest the duct
    y_lc = cy - cx + S.RING_L - (h + S.CORNER_GAP) * math.sqrt(2)
    x_bc = S.RING_L + (S.RING_B - y_lc)
    y_r0 = S.LINK_Y + (S.LINK_TURN_X - S.RING_R)
    return [(sum(S.OUTLET_IN_X) / 2, S.LINK_Y), (S.LINK_TURN_X, S.LINK_Y), (S.RING_R, y_r0),
            (S.RING_R, S.RING_B), (x_bc, S.RING_B), (S.RING_L, y_lc), (S.RING_L, S.RING_FRONT + S.WALL + S.CH_W / 2)]


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


def plenum_box():
    return ((S.MOTOR_X1 + 0.6, S.BLOWER_BOX[0][1]), (S.OUTLET_IN_Y[0] - 2.5, S.XEND_FRONT_Y - 1.0))


def band(pts, h):
    """The outline of a band h each side of an open path, with mitred corners and ends h longer."""
    import numpy as np
    p = [np.array(q, float) for q in pts]
    d0 = (p[1] - p[0]) / np.linalg.norm(p[1] - p[0])
    d1 = (p[-1] - p[-2]) / np.linalg.norm(p[-1] - p[-2])
    p[0] = p[0] - d0 * h
    p[-1] = p[-1] + d1 * h
    left, right = [], []
    for i in range(len(p)):
        dirs = []
        if i > 0:
            dirs.append((p[i] - p[i - 1]) / np.linalg.norm(p[i] - p[i - 1]))
        if i < len(p) - 1:
            dirs.append((p[i + 1] - p[i]) / np.linalg.norm(p[i + 1] - p[i]))
        n = [np.array((-d[1], d[0])) for d in dirs]
        m = sum(n) / np.linalg.norm(sum(n))
        k = h / float(np.dot(m, n[0]))
        left.append(tuple(p[i] + m * k))
        right.append(tuple(p[i] - m * k))
    return _ccw(left + right[::-1])


def rect2d(xr, yr):
    return [(xr[0], yr[0]), (xr[1], yr[0]), (xr[1], yr[1]), (xr[0], yr[1])]


def duct_outline():
    """The plan of the duct: the channel, the plenum under the outlet, and the foot. One face, the
    corners rounded."""
    from build123d import Polygon as Poly
    h = S.CH_W / 2 + S.WALL
    px, py = plenum_box()
    face = Poly(*band(ring_path(), h), align=None)
    face = face + Poly(*_ccw(rect2d(px, py)), align=None)
    return _round2d(face, S.CORNER_R)


def duct_shell():
    face = duct_outline()
    shell = extrude(face, S.DUCT_Z1 - S.DUCT_Z0, dir=(0, 0, 1))
    return soften(Pos(0, 0, S.DUCT_Z0) * shell, S.TOP_CHAMFER)


def soften(part, c):
    """Chamfer the edges of the top face with c. In the print the chamfer faces up."""
    from build123d import Axis, chamfer
    try:
        top = part.faces().group_by(Axis.Z)[-1]
        t = chamfer([e for f in top for e in f.edges()], c)
        if t.is_valid and len(t.solids()) == 1:
            part = t
    except Exception:
        pass
    return part


def duct_cavity():
    pts = ring_path()
    prof = gable(S.CH_W, S.CH_Z0, S.DUCT_Z1 - S.WALL - S.CH_W / 2)
    cav = mitred_prisms(pts, prof, S.CH_W / 2)
    # The plenum: the inside of the lip, open to the blower outlet at the top.
    lx, ly = lip_inside()
    cav += box(lx, ly, (S.CH_Z0, S.DUCT_Z1 + S.INLET_LIP + 1))
    # The floor of the plenum: parallel to the slope under it, a wall of WALL between them.
    px, _ = plenum_box()
    return cav - _front_slope(S.SLOPE_Y0 + S.WALL * math.sqrt(2), px[0], px[1] + 5)


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
    for y in (3.0, 10.0):
        out.append(Vector(xr, y, z))
    yb = S.OPEN_Y[1] + S.WALL + 2.0
    for x in (14.0, 7.0, 0.0, -6.0):
        out.append(Vector(x, yb, z))
    xl = S.OPEN_X[0] - S.WALL - 2.0
    for y in (-10.0, -3.0, 4.0):
        out.append(Vector(xl, y, z))
    # The 45 degree part of the link: two outlets from the right front.
    p0 = Vector(S.LINK_TURN_X, S.LINK_Y, z)
    p1 = Vector(S.RING_R, S.LINK_Y + S.LINK_TURN_X - S.RING_R, z)
    for f in ():
        c = p0 + (p1 - p0) * f
        out.append(c + _unit(((t - c).X, (t - c).Y, 0)) * 2.0)   # 2 mm in from the centre line
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
    """The channel round the hot end (open at the front and the front right), the plenum under the
    blower outlet, the lip into the outlet, the foot under the blower bracket and the post at the
    back left. It prints on its bottom face (z = DUCT_Z0)."""
    part = duct_shell()
    # The post at the back left, under the head of the long screw of the Aero.
    part += box(S.POST_X, S.POST_Y, (S.DUCT_Z0, S.POST_Z[1]))
    # The foot: a short pillar from the bed up under the boss of the blower bracket.
    px, _ = plenum_box()
    foot = box((S.FOOT_X0, px[0] + 1), (S.FOOT_Y[0], S.LINK_Y - S.CH_W / 2), (S.DUCT_Z0, S.BLOWER_Z0 - 0.3))   # back to the link
    part += foot
    # The underside of the plenum and the foot rises at 45 degrees to the front: the gantry camera
    # sees the bed under them, and the air in the plenum slides back into the link.
    part -= _front_slope(S.FOOT_SLOPE_Y0, S.SLOPE_X0, px[0])
    part -= _front_slope(S.SLOPE_Y0, px[0], px[1] + 5)
    # The lip into the blower outlet (the cavity makes it hollow).
    part += box(*lip_outside(), (S.DUCT_Z1 - 1, S.DUCT_Z1 + S.INLET_LIP))
    part -= duct_cavity()
    for p, d in outlets():
        part -= outlet_cut(p, d)
    # The screw holes: the post (along x, the point up), the foot (up through it).
    y, z = S.POST_SCREW
    part -= hole((S.POST_X[0] - 1, y, z), (1, 0, 0), S.HOLE / 2, S.POST_X[1] - S.POST_X[0] + 2, (0, 0, 1))
    fx, fy = S.FOOT_SCREW
    part -= Pos(fx, fy, 0) * Cylinder(S.HOLE / 2, 40, align=(Align.CENTER, Align.CENTER, Align.MIN))
    return part


def _front_slope(y0, x0, x1):
    """The space under a 45 degree plane that meets the bed at y0 and rises toward -y, from x0 to x1."""
    z0, big = S.DUCT_Z0, 60.0
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    poly = Polygon(*_ccw([(y0, z0 - 1), (y0, z0), (y0 - big, z0 + big), (y0 - big, z0 - 1)]), align=None)
    return extrude(pl * poly, x1 - x0, dir=(1, 0, 0))


def spacer_placed():
    """The spacer at its place: on the front bottom motor screw, between the Aero and the motor."""
    y, z = S.FRONT_BOLTS[0]
    return Pos(S.MOTOR_X0 - S.SPACER_T, y, z) * Rot(0, 90, 0) * motor_spacer()


def foot_screw():
    fx, fy = S.FOOT_SCREW
    return _screw((fx, fy, S.DUCT_Z0), (0, 0, 1), S.FOOT_SCREW_L)   # the head under the foot


def motor_spacer():
    """The spacer between the Aero and the motor, on the front bottom motor screw (in place of the
    tab of the stock duct). It prints flat."""
    return (Cylinder(S.SPACER_D / 2, S.SPACER_T, align=(Align.CENTER, Align.CENTER, Align.MIN))
            - Cylinder(S.HOLE / 2, S.SPACER_T, align=(Align.CENTER, Align.CENTER, Align.MIN)))


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

def _round2d(face, r):
    """Round each corner of a 2D face with r, or with a smaller radius where r does not fit."""
    from build123d import fillet
    f = _face_of(face)
    i = 0
    while i < len(f.vertices()):
        v = f.vertices()[i]
        done = False
        for rr in (r, r * 0.5, r * 0.25):
            try:
                g = _face_of(fillet([v], rr))
                if g.is_valid and abs(g.area - f.area) < 0.5 * rr * rr + 1e-6 + f.area * 0.01:
                    f, done = g, True
                    break
            except Exception:
                pass
        i += 2 if done else 1
    return f


def bracket_outline():
    """The outline of the blower bracket in (y, z): round round the blower, square over the end cap."""
    from build123d import Rectangle, Circle as C2, Pos as P2
    cy, cz = S.blower_point(38.9, 0, 44.8)[1:]              # the centre of the blower wheel
    (_, _), (y0, y1), (bz0, _) = S.BLOWER_BOX
    shape = P2(cy, cz) * C2(26.5)
    shape += P2((S.FRONT_BOLTS[0][0] - 6 + y1) / 2, (bz0 + S.BRK_Z[1]) / 2) * Rectangle(y1 - S.FRONT_BOLTS[0][0] + 6, S.BRK_Z[1] - bz0)
    for y, z in (S.EAR_TOP, S.EAR_LOW):
        shape += P2(y, z) * C2(S.BOSS_D / 2)
    clip = P2((y0 - 2 + y1) / 2, (bz0 + S.BRK_Z[1]) / 2) * Rectangle(y1 - y0 + 2, S.BRK_Z[1] - bz0)
    return _round2d(shape & clip, 3.0)


def blower_bracket():
    """The plate on the end cap, round round the blower, and a thick lobe in front of the motor that
    holds the low ear. It prints on its outer face (x = BLOWER_X0): the countersinks open at the bed,
    the nut pockets at the top."""
    x0, x1 = S.MOTOR_X1, S.BLOWER_X0
    out = bracket_outline()
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    part = extrude(pl * out, x1 - x0, dir=(1, 0, 0))
    # The lobe in front of the motor: the plate is thicker there, back to BOSS_X0.
    front = out & Pos((S.BLOWER_BOX[1][0] - 5 + S.MOTOR_Y[0] - 0.6) / 2, 50, 0) * Box(
        S.MOTOR_Y[0] - 0.6 - S.BLOWER_BOX[1][0] + 5, 200, 1)
    pl2 = Plane(origin=(S.BOSS_X0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    lobe = extrude(pl2 * _round2d(_face_of(front), 2.0), x0 - S.BOSS_X0 + 0.01, dir=(1, 0, 0))
    part += lobe
    # The two front motor screws: countersunk from the outer face.
    for y, z in S.FRONT_BOLTS:
        part -= Pos(x0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, S.BRACKET_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
        part -= Pos(x1 + 0.01, y, z) * Rot(0, -90, 0) * _cone(S.CSK_D / 2)
    # The top ear: a square nut in the back face, the screw from the blower side.
    y, z = S.EAR_TOP
    part -= Pos(x0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, S.BRACKET_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part -= box((x0 - 1, x0 + S.SQ_NUT_T), (y - S.SQ_NUT / 2, y + S.SQ_NUT / 2), (z - S.SQ_NUT / 2, z + S.SQ_NUT / 2))
    # The boss for the foot screw of the duct, under the motor in front of the connector: a hole
    # for the thread of the screw, the point up in the print (-x).
    part += box((S.FOOT_X0, x1), S.FOOT_Y, (S.BLOWER_Z0, S.FOOT_BOSS_Z1))
    fx, fy = S.FOOT_SCREW
    part -= hole((fx, fy, S.BLOWER_Z0 - 1), (0, 0, 1), S.SCREW_HOLE / 2, S.FOOT_BOSS_Z1 - S.BLOWER_Z0, (-1, 0, 0))
    # The low ear: through the lobe, a hex nut in the back face of the lobe.
    y, z = S.EAR_LOW
    part -= Pos(S.BOSS_X0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, x1 - S.BOSS_X0 + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part -= Pos(S.BOSS_X0 - 1, y, z) * Rot(0, 90, 0) * extrude(RegularPolygon(S.NUT_AF / math.sqrt(3), 6), S.NUT_DEPTH + 1)
    return part


def _face_of(shape):
    """The 2D face of a sketch result (the largest face)."""
    fs = shape.faces() if hasattr(shape, "faces") else [shape]
    return max(fs, key=lambda f: f.area)


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


def _yz_extrude(face, x0, x1):
    """Extrude a face drawn in (y, z) from x0 to x1."""
    from build123d import Location
    f = _face_of(face)
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return extrude(pl * f, x1 - x0, dir=(1, 0, 0))


def _yz_plate(points, x0, x1):
    """A plate from x0 to x1 with the outline points (y, z)."""
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    poly = Polygon(*points, align=None)
    return extrude(pl * poly, x1 - x0)


def bltouch_bracket():
    """The plate on the heat sink fan (two pads under the fan screws and a strip outside the fan
    edge), the body behind the fan with the shelf over the BLTouch ears, and the cover round the
    BLTouch. It prints on its outer face (x = FAN_X0 - PAD_T): every wall stands up from the bed."""
    x0, x1 = S.FAN_X0 - S.PAD_T, S.FAN_X0
    (cy, cz), along, out = _fan_frame()
    by, bz = S.FAN_BACK
    ly, lz = S.FAN_BOTTOM
    edge = math.hypot(by - cy, lz - cz) / math.sqrt(2)     # the screw line: 16.25 from the centre
    length = math.hypot(by - ly, bz - lz)

    def at(t, s):                                          # t along the edge from the bottom screw, s out of it
        return (ly + along.X * t + out.X * s, lz + along.Y * t + out.Y * s)
    s0 = 20.0 - edge                                       # the edge of the fan frame (40 mm fan)
    r = S.PAD_D / 2
    from build123d import Polygon as Poly
    strip = [at(-r, s0), at(length, s0), at(length, s0 + S.STRIP_W), at(-r, s0 + S.STRIP_W)]
    pad = [at(-r, -r), at(r, -r), at(r, s0 + S.STRIP_W), at(-r, s0 + S.STRIP_W)]
    back = rect2d((by - r, 22.0), (bz - r, bz + r))                   # the back pad, out to the body
    arm = Poly(*_ccw(strip), align=None) + Poly(*_ccw(pad), align=None) + Poly(*_ccw(back), align=None)
    part = _yz_extrude(_round2d(arm, 2.5), x0, x1)
    # The side plate: the arm and the left wall of the cover in one outline, 1.5 mm thick.
    gp, t = S.COVER_GAP, S.COVER_T
    yf, yb = S.BLT_Y[0] - gp - t, S.BLT_Y[1] + gp + t
    wall = Poly(*_ccw(rect2d((yf, yb), (S.COVER_Z0, S.EARS_Z + 1.0))), align=None)
    side = _round2d(arm + wall + Poly(*_ccw(rect2d((yf, 22.0), (S.EARS_Z - 2, bz))), align=None), 2.5)
    part += _yz_extrude(side, x0, x0 + t)
    # The body: behind the two back edges of the fan, from the outer face to BODY_X1.
    g = S.EDGE_GAP
    z0, zc = S.EARS_Z, (S.FAN_EDGE_TOP - S.FAN_EDGE) / 2
    ztop = S.SHELF_Z1 + 11.0
    prof = [(z0 + S.FAN_EDGE + g, z0), (S.EAR_BACK_Y, z0), (S.EAR_BACK_Y, z0 + 4.5), (S.BACK_Y, z0 + 4.5),
            (S.BACK_Y, ztop), (S.FAN_EDGE_TOP - ztop + g, ztop), (zc + S.FAN_EDGE + g, zc)]
    part += _yz_plate(_ccw(prof), x0, S.BODY_X1)
    # The cover: the back wall, and the front wall over the duct (the side plate is the left wall).
    xr = S.BLT_X[1] + 0.8
    part += box((x0, xr), (yb - t, yb), (S.COVER_Z0, S.COVER_Z1))
    part += box((x0, xr), (yf, yf + t), (S.COVER_FRONT_Z0, S.COVER_Z1))
    # The holes. The fan screws go through the plate (vertical in the print). The BLTouch screws come
    # up from under the ears and cut their thread; the point of the hole is up in the print (+x).
    for y, z in (S.FAN_BACK, S.FAN_BOTTOM):
        part -= Pos(x0 - 1, y, z) * Rot(0, 90, 0) * Cylinder(S.HOLE / 2, S.PAD_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    for y in S.BLT_HOLES:
        part -= hole((S.PIN_X, y, z0 - 1), (0, 0, 1), S.SCREW_HOLE / 2, S.BLT_SCREW - S.BLT_EAR_T + 2, (1, 0, 0))
    c = S.CABLE_DIAG / 2
    pl = Plane(origin=(S.PIN_X, S.PIN_Y, z0 - 1), x_dir=(1, 0, 0), z_dir=(0, 0, 1))
    part -= extrude(pl * Polygon((c, 0), (0, c), (-c, 0), (0, -c), align=None), ztop - z0 + 2, dir=(0, 0, 1))
    return part


def _ccw(pts):
    a = sum(pts[i][0] * pts[(i + 1) % len(pts)][1] - pts[(i + 1) % len(pts)][0] * pts[i][1] for i in range(len(pts)))
    return pts if a > 0 else pts[::-1]


# --- The hardware and the BLTouch --------------------------------------------------------------

def bltouch():
    """The BLTouch Smart, pin up: the body, the ears (front to back) with round ends."""
    body = box(S.BLT_X, S.BLT_Y, (S.BLT_GAP, S.EARS_Z - S.BLT_EAR_T))
    r = S.BLT_EAR_W / 2
    half = S.BLT_FLANGE_L / 2 - r
    ears = box((S.PIN_X - r, S.PIN_X + r), (S.PIN_Y - half, S.PIN_Y + half), (S.EARS_Z - S.BLT_EAR_T, S.EARS_Z))
    for y in (S.PIN_Y - half, S.PIN_Y + half):
        ears += Pos(S.PIN_X, y, S.EARS_Z - S.BLT_EAR_T) * Cylinder(r, S.BLT_EAR_T, align=(Align.CENTER, Align.CENTER, Align.MIN))
    part = body + ears
    for y in S.BLT_HOLES:
        part -= Pos(S.PIN_X, y, S.EARS_Z - S.BLT_EAR_T - 1) * Cylinder(1.6, S.BLT_EAR_T + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
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
    """The two fan screws (button heads, longer), and the two BLTouch screws from under the ears."""
    x0 = S.FAN_X0 - S.PAD_T
    out = [_screw((x0, y, z), (1, 0, 0), S.FAN_SCREW, 5.7, 1.65) for y, z in (S.FAN_BACK, S.FAN_BOTTOM)]
    for y in S.BLT_HOLES:
        out.append(_screw((S.PIN_X, y, S.EARS_Z - S.BLT_EAR_T), (0, 0, 1), S.BLT_SCREW))
    return out, []


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
