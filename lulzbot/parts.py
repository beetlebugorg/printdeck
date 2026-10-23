# Printdeck LulzBot: the printed parts, the hardware, and the cables, in build123d. All shapes
# are in mount coordinates (see shape.py). The head is in head coordinates.
# LOC moves mount coordinates to the printer; HEAD_LOC moves head coordinates to the mount.
#
# Joints. Two M3 x 10 screws hold the plate on the chassis cover, in the holes of the upper pair
# of cover screws. Four M3 x 10 socket cap screws hold the head on the back panel. The back
# panel hangs on the dovetail keys of two clips in the front left rail (a hammer nut and an
# M3 x 8 screw hold each clip), and two M3 screws through a flange of the plate hold its left
# end. The pod slides up and in at 45 degrees onto two hooks on the plate, and two M3 screws go
# up through its bottom wall into the plate. The grille snaps into a pocket of the pod, and the
# fan snaps into the grille. Every hole and hook has 45 degree faces in place of overhangs, so no
# part needs supports.

import math

from build123d import (Align, Box, Circle, Cone, Cylinder, Line, Location, Plane, Polygon, Pos, Rectangle, Rot,
                       TangentArc, Vector, Wire, chamfer, extrude, fillet, sweep, Axis)

import shape as S
import workhorse as WH

# Mount to printer: x to the printer front (-Z), y up, z out of the cover (+X).
Y0 = WH.CHASSIS["y"][0] + S.Y_BASE
Z_MID = WH.CHASSIS_FRONT_Z + S.X_FRONT
LOC = Location(Plane(origin=(WH.COVER_FACE_X, Y0, Z_MID), x_dir=(0, 0, -1), z_dir=(1, 0, 0)))
_P, _Q, _R = S.head_dirs()
HEAD_LOC = Location(Plane(origin=S.head_origin(), x_dir=_P, z_dir=_R))
MIN = (Align.MIN, Align.MIN, Align.MIN)
SQ2 = math.sqrt(2)
SIN, COS = math.sin(S.TILT), math.cos(S.TILT)


# ---------------------------------------------------------------- helpers


def box(x0, x1, y0, y1, z0, z1):
    x0, x1 = sorted((x0, x1))
    y0, y1 = sorted((y0, y1))
    z0, z1 = sorted((z0, z1))
    return Pos(x0, y0, z0) * Box(x1 - x0, y1 - y0, z1 - z0, align=MIN)


def prism_x(pts_yz, x0, x1):
    """A polygon in the (y, z) plane, from x0 to x1."""
    x0, x1 = sorted((x0, x1))
    pl = Plane(origin=(x0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return extrude(pl * Polygon(*pts_yz, align=None), amount=x1 - x0, dir=pl.z_dir)


def prism_y(pts_zx, y0, y1):
    """A polygon in the (z, x) plane, from y0 to y1."""
    y0, y1 = sorted((y0, y1))
    pl = Plane(origin=(0, y0, 0), x_dir=(0, 0, 1), z_dir=(0, 1, 0))
    return extrude(pl * Polygon(*pts_zx, align=None), amount=y1 - y0, dir=pl.z_dir)


def cyl_z(x, y, r, z0, z1):
    z0, z1 = sorted((z0, z1))
    return Pos(x, y, z0) * Cylinder(r, z1 - z0, align=(Align.CENTER, Align.CENTER, Align.MIN))


def cyl_y(x, z, r, y0, y1):
    y0, y1 = sorted((y0, y1))
    return Pos(x, y0, z) * Rot(-90, 0, 0) * Cylinder(r, y1 - y0, align=(Align.CENTER, Align.CENTER, Align.MIN))


def cyl_x(y, z, r, x0, x1):
    x0, x1 = sorted((x0, x1))
    return Pos(x0, y, z) * Rot(0, 90, 0) * Cylinder(r, x1 - x0, align=(Align.CENTER, Align.CENTER, Align.MIN))


def teardrop_y(x, z, r, y0, y1, tip):
    """A hole along y with a 45 degree point toward z * tip (tip = +1 or -1): the top of the
    hole in the print orientation, so the hole does not overhang."""
    q = r / SQ2
    t = tip * r * SQ2
    pts = [(z, x), (z + tip * q, x - q), (z + t, x), (z + tip * q, x + q)]
    return cyl_y(x, z, r, y0, y1) + prism_y(pts, y0, y1)


def teardrop_x(y, z, r, x0, x1, tip):
    """A hole along x with a 45 degree point toward z * tip."""
    q = r / SQ2
    t = tip * r * SQ2
    pts = [(y, z), (y - q, z + tip * q), (y, z + t), (y + q, z + tip * q)]
    return cyl_x(y, z, r, x0, x1) + prism_x(pts, x0, x1)


def rounded_rect(x0, x1, y0, y1, r, z0, z1):
    """A box with round edges along z (seen from the front: round corners)."""
    s = box(x0, x1, y0, y1, z0, z1)
    return fillet(s.edges().filter_by(Axis.Z), r) if r > 0 else s


def slot_z(x0, x1, y, w, z0, z1):
    """A slot along x with round ends, through z."""
    return rounded_rect(x0, x1, y - w / 2, y + w / 2, w / 2 - 0.01, z0, z1)


def diamond_y(x, z, h, y0, y1):
    """A diamond hole along y: centre (x, z), half size h. Its faces are at 45 degrees."""
    return prism_y([(z - h, x), (z, x - h), (z + h, x), (z, x + h)], y0, y1)


def round_pts(a0, a1, b0, b1, r, n=6):
    """A rectangle with round corners, as a polygon in a plane."""
    pts = []
    for ca, cb, t0 in ((a1 - r, b1 - r, 0), (a0 + r, b1 - r, 90), (a0 + r, b0 + r, 180), (a1 - r, b0 + r, 270)):
        for i in range(n + 1):
            t = math.radians(t0 + 90 * i / n)
            pts.append((ca + r * math.cos(t), cb + r * math.sin(t)))
    return pts


def to_world(s):
    return LOC * s


def head_to_mount(s):
    return HEAD_LOC * s


def mount_to_head(s):
    return HEAD_LOC.inverse() * s


def back_r(q):
    """r of the back plane (the front face of the back panel) at q, in head coordinates."""
    o = S.head_origin()
    return (S.BACK_X[1] - o[0] + q * SIN) / COS


def back_y(q):
    """Mount y of the back plane at q."""
    return S.head_pt(0, q, back_r(q))[1]


def back_q(y):
    """q of the back plane at the mount height y."""
    a, b = back_y(0.0), back_y(1.0) - back_y(0.0)
    return (y - a) / b


# ---------------------------------------------------------------- hooks (pod to plate)


E = (-1 / SQ2, 1 / SQ2)          # (y, z): down and out, along the fin. The pod engages along -E.


def fin_profile(yt, grow=0.0, tip_extra=0.0, back=0.0):
    """The fin of a hook, in the (y, z) plane. yt: y of the top of the fin base on the plate.
    grow widens the fin on both 45 degree faces, tip_extra makes it longer at the tip, and
    back makes it longer behind the base (into the plate)."""
    w = S.FIN_T * SQ2
    n = (1 / SQ2, 1 / SQ2)
    L = S.FIN_L + tip_extra

    def pt(y, z, sn, se):
        return (y + sn * n[0] + se * E[0], z + sn * n[1] + se * E[1])

    a, b = (yt, S.T_PLATE), (yt - w, S.T_PLATE)
    return [pt(*a, grow, -back), pt(*a, grow, L), pt(*b, -grow, L), pt(*b, -grow, -back)]


def fin(xr, yt):
    lo, hi = sorted(xr)
    return prism_x(fin_profile(yt, back=0.8), lo + 0.2, hi - 0.2)


def hook_block(xr, yt, kind):
    """The block of a hook on the pod, with the slot for the fin. kind "wall": on the inner
    face of the rear wall, with a 45 degree front from the wall. kind "skin": on the skin."""
    inner_x, wall_x = xr
    y0, y1 = yt - 16.0, yt + 6.0
    if kind == "wall":
        sg = 1 if wall_x > 0 else -1
        blk = box(inner_x, wall_x + sg * 0.5, y0, y1, S.T_PLATE + S.CLR, S.HOOK_FRONT)
        f = S.HOOK_FRONT
        cut = prism_y([(f + 1, wall_x + sg * 1), (f - 30, wall_x - sg * 30), (f + 60, wall_x - sg * 30),
                       (f + 60, wall_x + sg * 1)], y0 - 1, y1 + 1)
        blk = blk - cut
        x0, x1 = inner_x - sg * 1, wall_x
    else:
        blk = box(inner_x, wall_x, y0, y1, S.T_PLATE + S.CLR, S.POD_Z - S.SKIN + 0.01)
        x0, x1 = inner_x - 1, wall_x + 1
    slot = prism_x(fin_profile(yt, grow=S.CLR, tip_extra=0.5, back=12.0), x0, x1)
    return blk - slot


# ---------------------------------------------------------------- head


def head():
    """The head, in head coordinates: the face (15 degrees back), the four walls, the LCD
    pocket and the opening, the display bosses, and the columns and blocks for the screws from
    the back panel. It is open at the back; the back panel closes it."""
    p0, p1 = S.HEAD_P
    q0, q1 = S.HEAD_Q
    outer = rounded_rect(p0, p1, q0, q1, S.R_EDGE, -150, 0)
    outer = chamfer(outer.edges().filter_by(lambda e: abs(e.center().Z) < 0.01), S.EDGE)
    inner = rounded_rect(p0 + S.WALL, p1 - S.WALL, q0 + S.WALL, q1 - S.WALL, S.R_IN, -151, -S.SKIN)
    s = (outer - inner) & keep_front()
    # Display (landscape): the LCD pocket in the skin (the glass lies on a 1 mm lip), the
    # opening, and the four bosses. The board ears rest on the bosses; the M3 x 6 screws cut
    # their thread in a 2.5 mm hole.
    ph, pv = S.POCKET_HV
    dp, dq = S.DISP_OFF
    s = s - box(dp - ph / 2, dp + ph / 2, dq - pv / 2, dq + pv / 2, -S.SKIN - 0.01, -S.LIP)
    oh, ov = S.AA_HV[0] + 2 * S.OPEN_MARGIN, S.AA_HV[1] + 2 * S.OPEN_MARGIN
    s = s - box(-oh / 2, oh / 2, -ov / 2, ov / 2, -S.LIP - 0.5, 1)      # at the centre of the face
    board_front = -S.LIP + S.BOARD_C[1]
    for a, b in S.EARS:
        h, v = dp + b, dq - a
        s = s + cyl_z(h, v, S.BOSS_R, board_front, -S.SKIN + 0.01)
        s = s - cyl_z(h, v, S.TAP_D / 2, board_front - 0.01, board_front + S.DISPLAY_TAP_DEPTH)
    inside = box(p0 + 0.5, p1 - 0.5, q0 + 0.5, q1 - 0.5, -150, -S.SKIN + 0.01)
    for y, z in head_screw_points():
        if z < -100:
            # A column along x (world Z) from the back panel to the skin, in the right band.
            s = s + (mount_to_head(cyl_x(y, z, S.COL_R, S.BACK_X[1], 300)) & inside)
        else:
            s = s + mount_to_head(left_block(y, z))
        s = s - mount_to_head(cyl_x(y, z, S.INSERT_D / 2, S.BACK_X[1] - 1,
                                     S.BACK_X[1] + S.INSERTS["head_back"][2] + S.INSERT_BLIND))
    return s


def keep_front():
    """In head coordinates: everything in front of the back panel."""
    return mount_to_head(box(S.BACK_X[1], 400, -300, 700, -400, 300))


def left_block(y, z):
    """A block on the inner face of the left wall of the head, at the back panel (mount
    coordinates). Its front face slopes 45 degrees from the wall, so it prints without supports."""
    zw = S.HEAD_ZC + S.HEAD_P[1] - S.WALL   # inner face of the left wall
    x0 = S.BACK_X[1]
    blk = box(x0, x0 + 10.0, y - 5.0, y + 5.0, zw - 8.0, zw + 0.5)
    cut = prism_y([(zw + 1, x0 + 11.0), (zw - 20, x0 - 10.0), (zw - 20, x0 + 40), (zw + 1, x0 + 40)], y - 6, y + 6)
    return blk - cut


def head_screw_points():
    """Mount (y, z) of the four screws from the back panel into the head."""
    out = []
    for p, q in S.HEAD_COLS:
        pt = S.head_pt(p, q, back_r(q) + 5.0)
        out.append((pt[1], pt[2]))
    zb = S.HEAD_ZC + S.HEAD_P[1] - S.WALL - 4.0
    for q in S.HEAD_BLOCKS:
        out.append((back_y(q), zb))
    return out


# ---------------------------------------------------------------- back panel


def back():
    """The back panel (mount coordinates): on the front face of the rail and the chassis, and
    in front of the pod. It closes the head."""
    x0, x1 = S.BACK_X
    p0, p1 = S.HEAD_P
    q0, q1 = S.HEAD_Q
    za, zb = S.HEAD_ZC - p1, S.HEAD_ZC - p0
    ya, yb = back_y(q0), back_y(q1)
    b = box(x0, x1, ya, yb, za, zb)
    b = fillet(b.edges().filter_by(Axis.X), S.R_EDGE)
    # The slots for the dovetail keys of the two rail clips: a window, and above it the dovetail
    # slot. The head drops CLIP_DROP from the windows onto the keys. The dovetail walls lean out
    # toward the front face (up in the print), so they do not overhang.
    zc = S.RAIL_SLOT_Z
    k0, k1 = S.CLIP_KEY[0] / 2 + S.CLR, S.CLIP_KEY[1] / 2 + S.CLR
    for yc in S.CLIPS_Y:
        d0, d1 = yc - S.CLIP_L / 2 - S.CLIP_SLACK, yc + S.CLIP_L / 2 + S.CLIP_SLACK
        b = b - prism_y([(zc - k0, x0 - 0.01), (zc + k0, x0 - 0.01), (zc + k1, x1 + 0.01), (zc - k1, x1 + 0.01)], d0, d1)
        b = b - box(x0 - 1, x1 + 1, d0 - S.CLIP_DROP, d0 + 0.5, zc - k1 - 0.2, zc + k1 + 0.2)
    # Holes for the four screws into the head and the four drive screws. Their heads are on the
    # rear face, behind the head (a socket cap head is 3 mm high: the panel is 3.5 mm from the chassis).
    for y, z in head_screw_points() + drive_holes():
        b = b - cyl_x(y, z, 1.7, x0 - 1, x1 + 1)
    # The pads under the drive, at its four holes.
    for y, z in drive_holes():
        b = b + cyl_x(y, z, 4.5, x1 - 0.01, x1 + S.DRIVE_PAD)
        b = b - cyl_x(y, z, 1.7, x0 - 1, x1 + S.DRIVE_PAD + 1)
    # Bosses with M3 heat-set inserts for the two screws through the plate flange.
    depth = S.INSERTS["back_flange"][2] + S.INSERT_BLIND
    for y, z in S.FLANGE_SCREWS:
        b = b + cyl_x(y, z, 4.5, x1 - 0.01, x0 + depth + 2.4)   # past the point of the pocket
        b = b - cyl_x(y, z, S.INSERT_D / 2, x0 - 0.01, x0 + depth)
        b = b - Pos(x0 + depth - 0.01, y, z) * Rot(0, 90, 0) * Cone(S.INSERT_D / 2, 0.15, S.INSERT_D / 2 - 0.15,
                                                                    align=(Align.CENTER, Align.CENTER, Align.MIN))
    for y0, y1, wz0, wz1 in S.BACK_WINDOWS:
        b = b - prism_x(round_pts(y0, y1, wz0, wz1, 5.0), x0 - 1, x1 + 1)
    # The windows for the cables, from the pod into the head.
    for y0, y1, wz0, wz1 in S.WINDOWS:
        b = b - prism_x(round_pts(y0, y1, wz0, wz1, 4.0), x0 - 1, x1 + 6)
    return b


# ---------------------------------------------------------------- pod


def pod():
    x0, x1 = S.POD_X
    ybb = back_y(S.HEAD_Q[0])             # the bottom of the back panel
    outer = box(x0, x1, S.POD_Y0, S.H, S.T_PLATE, S.POD_Z)
    outer = fillet(outer.edges().filter_by(Axis.Z).filter_by(lambda e: e.center().X < 0), S.R_EDGE)
    outer = chamfer(outer.edges().filter_by(lambda e: abs(e.center().Z - S.POD_Z) < 0.01
                                            and not abs(e.center().X - x1) < 0.01), S.EDGE)
    # Below the back panel the pod has a front wall, flush with the back panel.
    if ybb - S.CLR > S.POD_Y0 + 0.5:
        outer = outer + box(x1 - 0.01, S.BACK_X[1], S.POD_Y0, ybb - S.CLR, S.T_PLATE, S.POD_Z)
    inner = box(-S.X_IN, x1 - S.WALL, S.POD_Y0 + S.WALL, S.H - S.WALL, S.T_PLATE - 1, S.POD_Z - S.SKIN)
    inner = fillet(inner.edges().filter_by(Axis.Z).filter_by(lambda e: e.center().X < 0), S.R_IN)
    s = outer - inner
    # Above the bottom of the back panel the front is open (the back panel closes it).
    s = s - box(x1 - S.WALL - 0.01, x1 + 5, ybb - S.CLR + S.WALL, S.H - S.WALL, S.T_PLATE - 1, S.POD_Z - S.SKIN)
    s = grille_pocket(s)
    for xr, yt, kind in S.HOOKS:
        s = s + hook_block(xr, yt, kind)
    # Cable exits: notches in the rear edge of the rear wall (open toward the plate, so the
    # notch roof faces up in the print).
    for y0, y1, z1 in S.EXITS:
        s = s - rounded_rect(-S.X_FRONT - 1, -S.X_IN + 1, y0, y1, 0, S.T_PLATE - 1, z1)
    # Bottom screws: a boss on the bottom wall, a hole, and a pocket for the head. The front
    # of the boss slopes 45 degrees. The hole and the pocket have a 45 degree point toward the
    # cover (up in the print).
    zc = S.BOTTOM_SCREW_Z
    for x in S.BOTTOM_SCREWS_X:
        b0, top = S.POD_Y0 + S.WALL, S.POD_Y0 + S.BOTTOM_BOSS_TOP
        c = top + zc + 4.0
        s = s + prism_x([(b0 - 0.01, S.T_PLATE + S.CLR), (top, S.T_PLATE + S.CLR), (top, c - top),
                         (b0 - 0.01, c - b0 + 0.01)], x - 5.0, x + 5.0)
        s = s - teardrop_y(x, zc, 1.7, S.POD_Y0 - 1, top + 1, -1)
        s = s - teardrop_y(x, zc, S.CBORE[3][0] / 2, S.POD_Y0 - 1, S.POD_Y0 + S.CBORE[3][1], -1)
    # Vents: 45 degree diamonds through the top wall and the bottom wall, over and under the Pi.
    for x in [-30.0 + 6.0 * i for i in range(10)]:
        s = s - diamond_y(x, 13.0, 2.2, S.H - S.WALL - 1, S.H + 1)
    for x in [-20.0 + 6.0 * i for i in range(6)]:
        s = s - diamond_y(x, 13.0, 2.2, S.POD_Y0 - 1, S.POD_Y0 + S.WALL + 1)
    return s


# ---------------------------------------------------------------- plate and grille


def plate():
    p = box(-S.X_FRONT, S.X_FRONT, S.POD_Y0, S.H, 0, S.T_PLATE)
    p = fillet(p.edges().filter_by(Axis.Z).filter_by(lambda e: e.center().X < 0), S.R_EDGE)
    # Pads with slots for the cover screws (the positions are to be confirmed, see shape.py).
    h = S.MOUNT_SLOT / 2
    for x, y in S.MOUNT_SCREWS:
        p = p + rounded_rect(x - S.PAD_R, x + S.PAD_R, y - h - S.PAD_R, y + h + S.PAD_R, S.PAD_R - 0.01,
                             S.T_PLATE - 0.01, S.PAD_T)
        r = S.MOUNT_SLOT_W / 2
        p = p - rounded_rect(x - r, x + r, y - h - r, y + h + r, r - 0.01, -1, S.PAD_T + 1)
    for x, y in S.PI_HOLES:
        p = p + cyl_z(x, y, S.PI_BOSS_R, S.T_PLATE - 0.01, S.PI_Z)
        p = p - cyl_z(x, y, S.TAP_D / 2, S.PI_Z - S.PI_TAP_DEPTH, S.PI_Z + 0.01)
    # Windows: the plate is a frame on the cover (less filament).
    for x0, x1, y0, y1 in S.PLATE_WINDOWS:
        p = p - rounded_rect(x0, x1, y0, y1, 5.0, -1, S.T_PLATE + 1)
    for xr, yt, _ in S.HOOKS:
        p = p + fin(xr, yt)
    y0, y1 = S.POD_Y0 + S.BLOCK_Y[0], S.POD_Y0 + S.BLOCK_Y[1]
    for x in S.BOTTOM_SCREWS_X:
        p = p + box(x - 6.0, x + 6.0, y0, y1, S.T_PLATE - 0.01, S.BOTTOM_SCREW_Z + 5.0)
        p = p - teardrop_y(x, S.BOTTOM_SCREW_Z, S.INSERT_D / 2, y0 - 1,
                           y0 + S.INSERTS["pod_block"][2] + S.INSERT_BLIND, 1)
    # The flange at the front edge: a block for each screw into the back panel, and a web that
    # connects it to the plate in front of the cables. The holes have a 45 degree point (up in
    # the print).
    fx0, fx1 = S.FLANGE_X[0], S.BACK_X[0] - S.CLR
    z0, z1 = S.FLANGE_Z
    for y, z in S.FLANGE_SCREWS:
        h = S.FLANGE_HALF
        w = fx1 - 2.0                             # the rear face of the web
        # The web stands on the bed in front of the plate (the plate ends at the chassis face).
        prof = [(fx1, 0.0), (fx1, z1), (fx0, z1), (fx0, z0 + (w - fx0)), (w, z0), (w, S.T_PLATE),
                (S.X_FRONT - 0.1, S.T_PLATE), (S.X_FRONT - 0.1, 0.0)]
        p = p + prism_y([(zz, xx) for xx, zz in prof], y - h, y + h)
        p = p - teardrop_x(y, z, 1.7, fx0 - 1, fx1 + 1, 1)
    return p


def grille_pocket(s):
    """The pocket for the grille in the pod: a rim on the inner face of the skin, the pocket,
    the 45 degree ledge, the opening under it, the two grooves for the snap ridges, and the
    fingernail notch."""
    x0, x1, y0, y1 = S.GRILLE_WIN
    rw, rh = S.GRILLE_RIM
    zs = S.POD_Z - S.SKIN                        # inner face of the skin
    zp = S.POD_Z - S.GRILLE_T                    # the pocket floor (the back of the grille)
    s = s + rounded_rect(x0 - rw, x1 + rw, y0 - rw, y1 + rw, 3.0 + rw, zs - rh, zs + 0.01)
    s = s - rounded_rect(x0, x1, y0, y1, 3.0, zp, S.POD_Z + 1)
    led = S.GRILLE_LEDGE
    face = Plane(origin=(0, 0, zp), z_dir=(0, 0, -1)) * fillet(Rectangle(x1 - x0, y1 - y0).vertices(), 3.0)
    cone = extrude(Pos((x0 + x1) / 2, (y0 + y1) / 2) * face, amount=led, dir=(0, 0, -1), taper=45)
    s = s - cone
    s = s - rounded_rect(x0 + led, x1 - led, y0 + led, y1 - led, 2.0, zs - rh - 1, zp)
    # Grooves for the ridges: a 45 degree V in each side wall (horizontal in the print).
    g = S.GRILLE_RIDGE + 0.2
    zm = zp + S.GRILLE_T / 2
    ty0, ty1 = grille_tab_y()
    for sg, xw in ((-1, x0), (1, x1)):
        s = s - prism_y([(zm - g, xw), (zm, xw + sg * g), (zm + g, xw)], ty1 - 10.0, ty1 + 0.5)
    nw, nd = S.GRILLE_NOTCH
    xc = (x0 + x1) / 2
    s = s - box(xc - nw / 2, xc + nw / 2, y0 - nd, y0 + 1, zs - rh - 1, S.POD_Z + 1)
    return s


def grille_tab_y():
    """y of the root and the free end of the two tabs (the tabs are at the middle of the sides)."""
    x0, x1, y0, y1 = S.GRILLE_WIN
    yc = (y0 + y1) / 2
    L = S.GRILLE_TAB[0]
    return yc - L / 2, yc + L / 2


def grille():
    """The grille: a plate with vent slots, flush with the face of the pod, and a flexible tab on
    each long side with a ridge that clicks into the pod. The back edge has a 45 degree chamfer
    that is on the ledge of the pocket. Over the SoC, the bump holds the fan (see bump())."""
    x0, x1, y0, y1 = S.GRILLE_WIN
    c = S.GRILLE_CLR
    zp = S.POD_Z - S.GRILLE_T
    g = rounded_rect(x0 + c, x1 - c, y0 + c, y1 - c, 3.0 - c, zp, S.POD_Z)
    g = chamfer(g.edges().filter_by(lambda e: abs(e.center().Z - zp) < 0.01), 0.8)
    n = int((y1 - y0 - 12) // S.GRILLE_SLOT_PITCH) + 1
    ys = [y0 + (y1 - y0) / 2 + (i - (n - 1) / 2) * S.GRILLE_SLOT_PITCH for i in range(n)]
    for y in ys:
        if abs(y - S.FAN_XY[1]) < bump_half() + 1.0 + S.GRILLE_SLOT_W / 2:
            continue                             # the bump is here
        g = g - slot_z(x0 + 7.0, x1 - 7.0, y, S.GRILLE_SLOT_W, zp - 1, S.POD_Z + 1)
    # The tabs: a slot frees each tab from the grille, except at its root (the lower end).
    L, tw, gap = S.GRILLE_TAB
    ty0, ty1 = grille_tab_y()
    zm = zp + S.GRILLE_T / 2
    r = S.GRILLE_RIDGE
    for sg, xw in ((-1, x0 + c), (1, x1 - c)):
        xi = xw - sg * tw                        # inner face of the tab
        g = g - box(xi, xi - sg * gap, ty0, ty1 + gap, zp - 1, S.POD_Z + 1)
        g = g - box(xw + sg * 0.01, xi - sg * gap, ty1, ty1 + gap, zp - 1, S.POD_Z + 1)
        # The ridge: a 45 degree V near the free end of the tab.
        g = g + prism_y([(zm - r, xw - sg * 0.01), (zm, xw + sg * r), (zm + r, xw - sg * 0.01)], ty1 - 9.5, ty1 - 0.5)
    return bump(g)


def bump_half():
    """Half the outer width of the bump: the fan, a prong, the space behind it, and the wall."""
    return S.FAN[0] / 2 + S.PRONG[1] + S.PRONG_GAP + S.BUMP_WALL


def fan_z():
    """z of the far face of the fan (on the catches) and of the funnel at the walls of the bump."""
    z0 = S.POD_Z - S.GRILLE_T + S.PRONG[2]
    return z0, z0 + S.FAN[1] + S.FAN_PLAY - S.FAN_CLR


def bump(g):
    """The bump of the grille, over the SoC: a box with round corners out of the face, open at the
    back for the fan. Seen in the print (back down), the walls of the bump go up from the bed along
    the sides of the fan. Above the fan, a 45 degree funnel goes in from the walls to a square
    opening in the outer face, so there is no ceiling. Three vent slots go from each side of the
    opening down the funnel, at right angles to the wall below them, so every layer is on the
    layer under it. The four prongs and their catches hold the fan against the funnel. A groove
    in one corner is for the fan cable, from the fan to the back of the grille."""
    cx, cy = S.FAN_XY
    zp = S.POD_Z - S.GRILLE_T
    ho = bump_half()
    fh = S.FAN[0] / 2
    cw = fh + S.FAN_CLR                          # the inner faces of the walls
    _, zf = fan_z()
    b = rounded_rect(cx - ho, cx + ho, cy - ho, cy + ho, S.BUMP_R, zp + 0.5, S.BUMP_TOP)
    b = chamfer(b.edges().filter_by(lambda e: abs(e.center().Z - S.BUMP_TOP) < 0.01), S.EDGE)
    g = g + b
    g = g - box(cx - cw, cx + cw, cy - cw, cy + cw, zp - 1, zf)
    top = Plane(origin=(cx, cy, zf), z_dir=(0, 0, 1)) * Rectangle(2 * cw, 2 * cw)
    g = g - extrude(top, amount=S.BUMP_TOP + 1 - zf, taper=45)
    a = cw - (S.BUMP_TOP - zf)                   # half the opening in the outer face
    w, pitch = S.BUMP_SLOTS
    for k in range(4):
        m = Pos(cx, cy, 0) * Rot(0, 0, 90 * k)
        for o in (-pitch, 0.0, pitch):
            g = g - m * rounded_rect(a - 2.0, cw - 0.6, o - w / 2, o + w / 2, w / 2 - 0.01, zf, S.BUMP_TOP + 1)
        cut, prong = prong_shapes()
        g = g - m * cut
        g = g + m * prong
    gl, gd, gt = S.FAN_GROOVE
    groove = box(cw - gl, cw, cw - 0.01, cw + gd, zp - 1, gt)
    groove = groove + prism_y([(gt - 0.01, cw - gl), (gt - 0.01, cw), (gt + gl / 2, cw - gl / 2)], cw - 0.01, cw + gd)
    return g - Pos(cx, cy, 0) * groove


def prong_shapes():
    """The prong on the +x side of the fan, near the +y corner, about the centre of the fan (the
    other three are the same, turned 90 degrees each): the cut that frees it (the
    space behind it and a slit at each side, with 45 degree roofs), and the prong with its catch.
    The prong stands on the bed in the print. The catch has a 45 degree lead-in under it and a
    level top that holds the far face of the fan."""
    zp = S.POD_Z - S.GRILLE_T
    w, t, d = S.PRONG
    gap, s = S.PRONG_GAP, 0.6
    fh = S.FAN[0] / 2
    _, zr = fan_z()
    xg = fh + t                                  # the outer face of the prong
    c = S.PRONG_AT
    cut = box(xg, xg + gap, c - w / 2 - s, c + w / 2 + s, zp - 1, zr)
    cut = cut + prism_y([(zr - 0.01, xg), (zr - 0.01, xg + gap), (zr + gap / 2, xg + gap / 2)], c - w / 2 - s, c + w / 2 + s)
    for sg in (-1, 1):
        y0, y1 = sorted((c + sg * w / 2, c + sg * (w / 2 + s)))
        cut = cut + box(fh - 0.5, xg + 0.01, y0, y1, zp - 1, zr)
        cut = cut + prism_x([(y0, zr - 0.01), (y1, zr - 0.01), ((y0 + y1) / 2, zr + s / 2)], fh - 0.5, xg + 0.01)
    prong = box(fh, xg + 0.01, c - w / 2, c + w / 2, zp, zr)
    prong = prong + prism_y([(zp, fh + 0.01), (zp + d, fh - d), (zp + d, fh + 0.01)], c - w / 2, c + w / 2)
    return cut, prong


def prong_strain():
    """The strain in a prong when it bends out by the catch depth: 1.5 t d / L^2."""
    _, t, d = S.PRONG
    _, zr = fan_z()
    L = zr - (S.POD_Z - S.GRILLE_T)
    return 1.5 * t * d / L ** 2, L


# ---------------------------------------------------------------- hardware
# Each builder returns a list of (name, shape, material), in mount coordinates.


def cap(axis, a, b, seat, direction, length, size=3):
    """A socket cap head screw (ISO 4762) along axis: the head on the seat, the shank toward
    direction."""
    cyl = {"x": cyl_x, "y": cyl_y, "z": cyl_z}[axis]
    d, h = S.CAP[size]
    head = cyl(a, b, d / 2, seat, seat - direction * h)
    shank = cyl(a, b, size / 2, seat, seat + direction * length)
    return head + shank


def mount_screws():
    """M3 x 10 with an M3 washer: the plate pad (4 mm), the washer, the cover, and the PEM nut of
    the chassis."""
    out = []
    for i, (x, y) in enumerate(S.MOUNT_SCREWS):
        out.append((f"mount_washer{i}", cyl_z(x, y, 3.5, S.PAD_T, S.PAD_T + 0.5) - cyl_z(x, y, 1.6, 0, 9), "steel"))
        out.append((f"mount_screw{i}", cap("z", x, y, S.PAD_T + 0.5, -1, 10.0), "black_steel"))
    return out


def insert_tube(axis, a, b, start, direction, length):
    """A heat-set insert as a brass tube (4.0 outside, 3.0 inside), from start toward direction."""
    cyl = {"x": cyl_x, "y": cyl_y, "z": cyl_z}[axis]
    end = start + direction * length
    lo, hi = sorted((start, end))
    return cyl(a, b, S.INSERT_D / 2, lo, hi) - cyl(a, b, 1.55, lo - 1, hi + 1)


def inserts():
    """All the heat-set inserts at their places, with the length that the hardware list gives."""
    u = S.INSERT_USE
    out = []
    bf = -S.LIP + S.BOARD_C[1]
    dp, dq = S.DISP_OFF
    for i, (y, z) in enumerate(head_screw_points()):
        out.append((f"insert_head_back{i}", insert_tube("x", y, z, S.BACK_X[1], 1, u["head_back"]), "brass"))
    for i, (y, z) in enumerate(S.FLANGE_SCREWS):
        out.append((f"insert_back_flange{i}", insert_tube("x", y, z, S.BACK_X[0], 1, u["back_flange"]), "brass"))
    for i, x in enumerate(S.BOTTOM_SCREWS_X):
        out.append((f"insert_pod_block{i}", insert_tube("y", x, S.BOTTOM_SCREW_Z, S.POD_Y0 + S.BLOCK_Y[0], 1,
                                                        u["pod_block"]), "brass"))
    return out


def bottom_screws():
    """M3 x 8, up through the bottom of the pod (in a counterbore) into the plate blocks."""
    return [(f"bottom_screw{i}", cap("y", x, S.BOTTOM_SCREW_Z, S.POD_Y0 + S.CBORE[3][1], 1, 8.0), "black_steel")
            for i, x in enumerate(S.BOTTOM_SCREWS_X)]


def flange_screws():
    """M3 x 12, from inside the pod through the plate flange into the back panel."""
    return [(f"flange_screw{i}", cap("x", y, z, S.FLANGE_X[0], 1, 12.0), "black_steel")
            for i, (y, z) in enumerate(S.FLANGE_SCREWS)]


def head_screws():
    """M3 x 10, from the rear of the back panel into the head."""
    return [(f"head_screw{i}", cap("x", y, z, S.BACK_X[0], 1, 10.0), "black_steel")
            for i, (y, z) in enumerate(head_screw_points())]


def clip(yc):
    """A rail clip (mount coordinates): a flange on the rail face, and a dovetail key for the back
    panel. An M3 x 8 screw goes from a counterbore in the key through the clip into the
    hammer nut behind the lips of the slot. The clip prints on its end (+y up), so the screw hole has
    a 45 degree point toward +y."""
    zc = S.RAIL_SLOT_Z
    xr = S.RAIL_FRONT_X
    x0, x1 = S.BACK_X
    y0, y1 = yc - S.CLIP_L / 2, yc + S.CLIP_L / 2
    fw = S.CLIP_FLANGE_W / 2
    c = prism_y([(zc - fw, xr + 0.05), (zc + fw, xr + 0.05), (zc + fw, x0), (zc - fw, x0)], y0, y1)
    k0, k1 = S.CLIP_KEY[0] / 2, S.CLIP_KEY[1] / 2
    c = c + prism_y([(zc - k0, x0 - 0.01), (zc + k0, x0 - 0.01), (zc + k1, x1), (zc - k1, x1)], y0, y1)
    d, depth = S.CBORE[3]
    c = c - teardrop_up(yc, zc, 1.7, xr - 1, x1 + 1)
    c = c - teardrop_up(yc, zc, d / 2, x1 - depth, x1 + 1)
    return c


def teardrop_up(y, z, r, x0, x1):
    """A hole along x with a 45 degree point toward +y (up in the print of the clip)."""
    q = r / SQ2
    pts = [(y, z), (y + q, z - q), (y + r * SQ2, z), (y + q, z + q)]
    return cyl_x(y, z, r, x0, x1) + prism_x(pts, x0, x1)


def hammer_nuts():
    """The M3 hammer nut of each clip, in its place across the slot behind the lips: 10 mm
    across the slot, 6 mm along it, 4 mm thick, with 45 degree sides at the bottom like the V
    of the slot. The hole is the minor diameter of the M3 thread."""
    zc = S.RAIL_SLOT_Z
    a, b, t = S.HAMMER_NUT
    top = S.RAIL_FRONT_X - S.RAIL_LIP - S.NUT_GAP
    k, h = top - S.HAMMER_STRAIGHT, a / 2
    e = h - (t - S.HAMMER_STRAIGHT)
    pts = [(zc - h, top), (zc + h, top), (zc + h, k), (zc + e, top - t), (zc - e, top - t), (zc - h, k)]
    out = []
    for i, yc in enumerate(S.CLIPS_Y):
        n = prism_y(pts, yc - b / 2, yc + b / 2) - cyl_x(yc, zc, 1.25, top - t - 1, top + 1)
        out.append((f"hammer_nut{i}", n, "steel"))
    return out


def clip_screws():
    """M3 x 8, from the counterbore in the key of each clip into the hammer nut."""
    seat = S.BACK_X[1] - S.CBORE[3][1]
    return [(f"clip_screw{i}", cap("x", yc, S.RAIL_SLOT_Z, seat, -1, S.CLIP_SCREW), "black_steel")
            for i, yc in enumerate(S.CLIPS_Y)]


def drive_screws():
    """M3 x 6, from the rear of the back panel into the drive."""
    return [(f"drive_screw{i}", cap("x", y, z, S.BACK_X[0], 1, 6.0), "black_steel")
            for i, (y, z) in enumerate(drive_holes())]


def display():
    """The panel, the backpack board, the sockets, the plugs, and the screws, in portrait
    display coordinates; then turned to landscape in the head and moved to the mount."""
    la, lb = S.LCM_AB
    ta, tb = S.TP_AB
    bc0, bc1 = S.BOARD_C
    p = [
        ("display_glass", box(-ta / 2, ta / 2, -tb / 2, tb / 2, -1.5, 0), "black_glass"),
        ("display_lcd", box(-la / 2, la / 2, -lb / 2, lb / 2, -S.LCM_T, -1.5), "lcd_metal"),
        ("display_board", board(bc0, bc1), "pcb_green"),
        ("display_sockets", box(-32, 12.9, -S.BOARD_B, -66, bc0 - 6, bc0), "steel"),
        # Straight plugs, 30 mm and 28 mm past the board edge (from the Slate).
        ("display_plug_hdmi", box(S.HDMI_A - 11, S.HDMI_A + 11, -112.55, -78.55, -20.35, bc0), "black_plastic"),
        ("display_plug_usb", box(S.USB_A - 6, S.USB_A + 6, -110.55, -78.55, -17.35, bc0), "black_plastic"),
    ]
    for i, (a, b) in enumerate(S.EARS):
        p.append((f"display_screw{i}", cyl_z(a, b, 2.75, bc0, bc0 - 3.0) + cyl_z(a, b, 1.5, bc0, bc0 + 6.0 - 0.01),
                  "black_steel"))
    m = HEAD_LOC * Pos(S.DISP_OFF[0], S.DISP_OFF[1], -S.LIP) * Rot(0, 0, -90)
    return [(n, m * s, mat) for n, s, mat in p]


def board(c0, c1):
    """The backpack board: the body and the four ears with their 3 mm holes. Each ear has a
    4 mm radius around its hole at the outer corner (brd outline)."""
    b = box(-S.BOARD_A, S.BOARD_A, -S.BOARD_B, S.BOARD_B, c0, c1)
    for sa in (-1, 1):
        for sb in (-1, 1):
            ear = box(sa * S.BOARD_A, sa * S.EAR_A, sb * (S.BOARD_B - S.EAR_B), sb * S.BOARD_B, c0, c1)
            corner = ear.edges().filter_by(Axis.Z).sort_by_distance((sa * 200, sb * 200, 0))[0]
            b = b + fillet([corner], 3.99)
    for a, bb in S.EARS:
        b = b - cyl_z(a, bb, 1.5, c0 - 1, c1 + 1)
    return b


def pi5():
    """The Pi 5, from the Slate model. Pi coordinates: x toward the USB edge, y toward the
    GPIO header, z up from the bottom of the board."""
    b = 1.5
    p = [
        ("pi_board", pi_board(), "pcb_green"),
        ("pi_ethernet", box(23.5, 44.5, -25.8, -9.8, b, b + 13.5), "steel"),
        ("pi_usb3", box(27, 44.5, -5.5, 7.7, b, b + 15.6), "steel"),
        ("pi_usb2", box(27, 44.5, 12.4, 25.6, b, b + 15.6), "steel"),
        ("pi_header", box(-35.4, 15.4, 21.9, 27, b, b + 8.5), "black_plastic"),
        ("pi_usbc", box(-35.8, -26.8, -29.2, -21.8, b, b + 3.2), "steel"),
        ("pi_hdmi0", box(-20.3, -13.1, -29.2, -21.6, b, b + 3), "steel"),
        ("pi_hdmi1", box(-6.9, 0.3, -29.2, -21.6, b, b + 3), "steel"),
        ("pi_soc", box(-20.5, -3.5, -6, 11, b, b + 2.2), "nickel"),
        ("pi_rp1", box(4.5, 16.5, -4, 8, b, b + 1.2), "black_plastic"),
        # Stick-on heatsinks, the highest that fits: 20 x 20 mm on the SoC, 12 x 12 mm on the RP1.
        ("pi_heatsink_soc", heatsink(-22, -2, -7.5, 12.5, b + 2.2), "alu_dark"),
        ("pi_heatsink_rp1", heatsink(4.5, 16.5, -4, 8, b + 1.2), "alu_dark"),
        ("pi_fan_socket", box(22.8, 25.7, 21, 27, b, b + 3.6), "nickel"),
        ("pi_sdcard", box(-44.5, -38.0, -6.0, 5.0, -1.4, 0.0), "black_plastic"),
    ]
    for i, (x, y) in enumerate(S.PI_HOLES_PI):
        p.append((f"pi_screw{i}", cyl_z(x, y, 2.75, b, b + 3.0) + cyl_z(x, y, 1.5, b, b - 6.0), "black_steel"))
    p += [
        # Plugs: USB-C power and micro HDMI (bottom edge), the touch (USB 3, upper port) and the
        # printer (USB 2, lower port) at the front edge.
        ("plug_pi_power", box(-37.3, -25.3, -57.3, -29.3, b - 1.65, b + 4.85), "black_plastic"),
        ("plug_pi_hdmi", box(-22.7, -10.7, -49.3, -29.3, b - 1.5, b + 4.5), "black_plastic"),
        ("plug_touch_usb", box(44.6, 74.6, -6.4, 8.6, b + 8, b + 15), "black_plastic"),
        ("plug_drive_usb", box(44.6, 74.6, -6.4, 8.6, b + 0.8, b + 7.8), "black_plastic"),
        ("plug_printer_usb", box(44.6, 74.6, 11.5, 26.5, b + 0.8, b + 7.8), "black_plastic"),
        ("plug_pi_fan", box(23.0, 25.5, 21.5, 26.5, b + 3.6, b + 7), "nickel"),     # from the Slate
    ]
    m = Pos(S.PI_X, S.PI_Y, S.PI_Z)
    return [(n, m * s, mat) for n, s, mat in p]


def heatsink(x0, x1, y0, y1, z0):
    """A heatsink HEATSINK_H high (Pi coordinates): a 2 mm base and fins along y."""
    h = box(x0, x1, y0, y1, z0, z0 + S.HEATSINK_H)
    n = int((x1 - x0) // 4)
    for i in range(n - 1):
        x = x0 + (x1 - x0) * (i + 1) / n
        h = h - box(x - 0.8, x + 0.8, y0 - 1, y1 + 1, z0 + 2.0, z0 + S.HEATSINK_H + 1)
    return h


def fan():
    """The 30 mm fan in the bump of the grille (from the Slate): the frame, the hub, and seven
    blades. Its far face is on the catches of the prongs."""
    cx, cy = S.FAN_XY
    h, t = S.FAN[0] / 2, S.FAN[1]
    body = box(-h, h, -h, h, 0, t) - Pos(0, 0, -1) * Cylinder(14, t + 2, align=(Align.CENTER, Align.CENTER, Align.MIN))
    for x in (-12, 12):
        for y in (-12, 12):
            body = body - cyl_z(x, y, 1.65, -1, t + 1)
    p = [("fan_frame", body, "black_plastic"),
         ("fan_hub", Pos(0, 0, 0.5) * Cylinder(6.3, t - 1, align=(Align.CENTER, Align.CENTER, Align.MIN)), "black_plastic")]
    for i in range(7):
        bl = Rot(0, 0, i * 360 / 7) * Pos(9.75, 0, t / 2) * Rot(35, 0, 0) * box(-3.75, 3.75, -3.2, 3.2, -0.4, 0.4)
        p.append((f"fan_blade{i}", bl, "black_plastic"))
    m = Pos(cx, cy, fan_z()[0])
    return [(n, m * sh, mat) for n, sh, mat in p]


def pi_board():
    b = box(-42.5, 42.5, -28, 28, 0, 1.5)
    b = fillet(b.edges().filter_by(Axis.Z), 3.0)
    for x, y in S.PI_HOLES_PI:
        b = b - cyl_z(x, y, S.PI_HOLE_D / 2, -1, 3)      # drilled from 2.7 to 3.2 mm for M3
    return b


def drive_frame():
    """The drive frame (from the Slate): local x along the drive toward the adapter (+z in
    mount), local z out of the back panel (+x), local y = -y. Origin: drive centre on the panel."""
    zc = S.DRIVE_Z_ADAPTER - S.DRIVE_ADAPTER
    return Location(Plane(origin=(S.BACK_X[1] + S.DRIVE_PAD, S.DRIVE_Y, zc), x_dir=(0, 0, 1), z_dir=(1, 0, 0)))


def drive_holes():
    """Mount (y, z) of the four drive screws."""
    m = drive_frame()
    out = []
    for a, c in S.DRIVE_HOLES:
        p = m * Pos(a, c, 0)
        out.append((round(p.position.Y, 3), round(p.position.Z, 3)))
    return out


def drive_plug_end():
    m = drive_frame()
    p = m * Pos(S.DRIVE_ADAPTER + S.MICRO_B_LEN, S.DRIVE_USB, 5.4)
    return (p.position.X, p.position.Y, p.position.Z)


def drive():
    """The drive, its adapter, the micro-B plug, and the four screws (mount coordinates)."""
    u = S.DRIVE_USB
    L, A = S.DRIVE_LEN, S.DRIVE_ADAPTER
    p = [
        ("ssd_drive", box(-L, L, -34.9, 34.9, 0, 6.8), "alu_dark"),
        ("ssd_adapter", box(L, A, -33, 33, 2.2, 3.8), "pcb_green"),
        ("ssd_usb", box(78, A, u - 6.25, u + 6.25, 3.8, 7.0), "steel"),
        ("ssd_cable_metal", box(A, A + 1.7, u - 6.1, u + 6.1, 4.5, 6.3), "steel"),
        ("ssd_cable_plug", box(A + 1.7, A + 16, u - 10.5, u + 10.5, 1.9, 8.9), "black_plastic"),
        ("ssd_cable_relief", Pos(A + 16, u, 5.4) * Rot(0, 90, 0)
         * Cone(3.5, 2.25, S.MICRO_B_LEN - 16, align=(Align.CENTER, Align.CENTER, Align.MIN)), "black_plastic"),
    ]
    m = drive_frame()
    return [(n, m * sh, mat) for n, sh, mat in p] + drive_screws()


# ---------------------------------------------------------------- cables


def rounded_path(points, k=0.45, cut=15.0):
    """The polyline with a tangent arc at each bend (from the Slate)."""
    pts = [Vector(*p) for p in points]
    edges, start = [], pts[0]
    for a, b, c in zip(pts, pts[1:], pts[2:]):
        u, w = b - a, c - b
        if u.get_angle(w) < 0.5:
            continue
        d = min(k * u.length, k * w.length, cut)
        p_in, p_out = b - u.normalized() * d, b + w.normalized() * d
        if (p_in - start).length > 1e-3:
            edges.append(Line(start, p_in))
        edges.append(TangentArc(p_in, p_out, tangent=u))
        start = p_out
    edges.append(Line(start, pts[-1]))
    return Wire(edges)


def tube(points, r):
    path = rounded_path(points)
    start = Vector(*points[1]) - Vector(*points[0])
    t = sweep(Plane(origin=points[0], z_dir=start) * Circle(r), path=path)
    expect = math.pi * r * r * path.length
    assert t.is_valid and len(t.solids()) == 1 and abs(t.volume - expect) < 0.02 * expect, \
        f"tube sweep failed: {points[0]}"
    return t


def cable_length(points):
    return sum(math.dist(p, q) for p, q in zip(points, points[1:]))


def disp_plug_end(kind):
    """The cable end of a straight display plug (head coordinates). The cable leaves to the left."""
    if kind == "hdmi":
        return S.disp_ab(S.HDMI_A, -112.55, -14.85)
    return S.disp_ab(S.USB_A, -110.55, -13.35)


def cable_paths():
    """The centre lines of the cables, in mount coordinates.
    HDMI: from the Pi down, along the bottom of the pod (over the screw blocks), up at the front,
    through the upper window into the head, right behind the display board, into the plug.
    Touch: from the upper USB 3 port straight into the upper window, then right behind the board.
    Drive: from the lower USB 3 port down the front, through the lower window, into the drive plug.
    Printer: from the lower USB 2 port up the front, over the Pi, down at the rear end of the Pi,
    and out of the upper exit at the rear. Power: from the USB-C port to the lower exit.
    Fan: from the corner of the fan down the groove of the bump, over the end of the GPIO header,
    into the plug on the fan socket of the Pi.
    The printer and the power cable go 25 mm out of the rear face of the chassis."""
    def hp(p, q, r):
        return S.head_pt(p, q, r)

    xb = S.BACK_X[1]
    xr = -S.X_FRONT - 25.0
    # HDMI.
    ph = S.pi_pt(-16.7, -49.3, 1.5 + 1.5)
    hw = (xb, 166.0, 17.0)
    he = disp_plug_end("hdmi")
    hdmi = [ph, (ph[0], ph[1] - 5.0, 10.0), (ph[0] + 6.0, 121.5, 16.0), (69.0, 121.5, 17.0), (75.0, 128.0, 16.0),
            (75.0, 158.0, 16.0), (82.0, 165.0, 17.0), hw, (xb + 4.0, 171.0, 22.0),
            hp(he[0] - 9.0, he[1], he[2] - 7.0), hp(he[0] - 6.0, he[1], he[2]), hp(*he)]
    # Touch.
    pt = S.pi_pt(74.6, 1.1, 1.5 + 11.5)
    tw = (xb, 182.0, 19.0)
    te = disp_plug_end("usb")
    touch = [pt, (pt[0] + 4.0, pt[1] + 1.0, pt[2] + 0.5), tw, (xb + 4.0, 190.0, 23.0),
             hp(te[0] - 9.0, te[1], te[2] - 7.0), hp(te[0] - 6.0, te[1], te[2]), hp(*te)]
    # Drive.
    pd = S.pi_pt(74.6, 1.1, 1.5 + 4.3)
    de = drive_plug_end()
    drive_c = [pd, (pd[0] + 2.5, pd[1] - 3.0, 10.5), (82.5, 140.0, 10.5), (84.0, 131.0, 10.5), (xb, de[1], 13.0),
               (de[0], de[1], 17.5), de]
    # Printer.
    pp = S.pi_pt(74.6, 19.0, 1.5 + 4.3)
    ye = sum(S.EXITS[1][:2]) / 2
    printer = [pp, (pp[0] + 2.5, pp[1] + 3.0, 10.5), (82.0, 218.0, 10.5), (76.0, 224.0, 10.0), (-36.0, 224.0, 10.0),
               (-43.0, 217.0, 9.0), (-43.0, 137.0, 8.0), (-50.0, ye, 7.0), (-S.X_IN + 10.0, ye, 6.5), (xr, ye, 6.5)]
    # Power.
    pc = S.pi_pt(-31.3, -57.3, 1.5 + 1.6)
    yl = sum(S.EXITS[0][:2]) / 2
    power = [pc, (pc[0], pc[1] - 2.5, pc[2] - 0.6), (pc[0] - 6.0, yl, 6.0), (-S.X_IN + 10.0, yl, 6.0), (xr, yl, 6.0)]
    # Fan.
    gl, gd, _ = S.FAN_GROOVE
    cw = S.FAN[0] / 2 + S.FAN_CLR
    gx, gy = S.FAN_XY[0] + cw - gl / 2, S.FAN_XY[1] + cw + gd / 2
    pf = S.pi_pt(24.25, 24.0, 1.5 + 7.1)
    fan_c = [(gx, gy, 27.0), (gx, gy, 21.5), (18.0, 198.0, 19.5), (pf[0], pf[1], 19.0), pf]
    return {"hdmi": hdmi, "touch": touch, "drive": drive_c, "printer": printer, "power": power, "fan": fan_c}


CABLE_R = {"hdmi": 2.0, "touch": 1.75, "drive": 2.25, "printer": 2.0, "power": 2.0, "fan": 1.0}


def cables():
    return [(f"cable_{k}", tube(p, CABLE_R[k]), "black_plastic") for k, p in cable_paths().items()]


def screen_corners():
    """Corners of the visible screen in the printer coordinates."""
    ox, oy = 0.0, 0.0                    # the active area is at the centre of the face
    w, h = S.AA_HV
    out = []
    for x, y in ((ox - w / 2, oy - h / 2), (ox + w / 2, oy - h / 2), (ox + w / 2, oy + h / 2), (ox - w / 2, oy + h / 2)):
        p = LOC * HEAD_LOC * Pos(x, y, -S.LIP + 0.05)
        out.append([p.position.X, p.position.Y, p.position.Z])
    return out
