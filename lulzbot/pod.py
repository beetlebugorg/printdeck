"""The deck pod: the Raspberry Pi Touch Display 2 (5 inch), with the Pi 5 and its Active Cooler on the
back of the display, in a printed case that fits close around them. A bracket for each printer holds
the pod: the TAZ Workhorse bracket is here.

The glass of the display is the front of the pod. It is wider than the case, so the case is a smaller
body behind the glass. The body is thick at the bottom, where the Pi is, and thin at the top, where
only the display is.

The ports are the Pi's own ports: the USB-C power port in the right wall, and the USB-A and Ethernet
ports in the bottom wall. The printer cable plugs straight into the Pi.

Holding the display: two hooks in the bottom of the body catch the bottom edge of the display frame,
and two screws through the back of the body go into the top tabs of the display. The Pi covers the
bottom tabs.

The pod hangs on the bracket: a dovetail on the back of the pod drops into a slot in the bracket from
above. The bracket stands on the two rail clips and the cover screw.

Pod coordinates: u across the glass to the right (seen from the front), v up from the bottom edge of
the glass, w out of the glass (the front of the glass is w = 0). The Pi is on the back: w < 0.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with pyrender --with PyOpenGL python pod.py [OUT_DIR | export]
"""

import math
import os
import sys

from build123d import (Box, Cylinder, Location, Plane, Polygon, Pos, Rectangle, RectangleRounded, Rot,
                       extrude, fillet, offset)

import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))

# ---------------------------------------------------------------- the display (product brief, 5 inch)
GLASS = (91.46, 143.4)          # the cover glass: across, up
GLASS_R = 5.0
LENS = 0.7                      # the thickness of the glass at its border
AA = (62.1, 110.4)              # the active area, in the middle of the glass
FRAME = (72.96, 122.74)         # the metal back of the module
FRAME_V0 = 12.8                 # the bottom of the frame over the bottom of the glass
FRAME_W = 8.5                   # the back of the frame behind the front of the glass
TABS = [(u, v) for u in (-25.5, 25.5) for v in (19.85, 19.85 + 103.7)]   # mounting tabs (51 x 103.7)
TAB_W = 11.5                    # the face of a tab behind the front of the glass
TAB_SIZE = (8.0, 6.0)
PI_HOLE_UV = [(u, v) for u in (-24.5, 24.5) for v in (33.91, 33.91 + 58.0)]   # the Pi standoffs (49 x 58)
STANDOFF_W = 14.92              # the tips of the standoffs: the bottom of the Pi board

# ---------------------------------------------------------------- the case
WALL = 2.0
GAP = 1.0                       # around the frame and the Pi
BODY_U = FRAME[0] / 2 + GAP + WALL                # half width: 39.48
BODY_V = (3.0, FRAME_V0 + FRAME[1] + GAP + WALL)   # bottom and top: 3.0 to 138.5
BODY_R = 6.0
FRONT_W = -(LENS + 0.05)        # the front rim of the body, against the back of the glass
PI_TOP_W = STANDOFF_W + 1.6 + 16.0                 # the top of the USB ports: 32.5
DEEP_W = PI_TOP_W + GAP + WALL                     # the back of the body where the Pi is: 35.5
THIN_W = TAB_W + 2.5            # the back of the body over the display only: 14.0
STEP_V = (100.0, 112.0)         # the slope between the deep and the thin part


def pi_uvw(x, y, z):
    """A point of the Pi (STEP coordinates: x along the 85 with the USB ports at +x, y along the 56 with
    the USB-C edge at y = 0, z up from the bottom of the board) on the back of the display: the USB
    end down, the USB-C edge to the right."""
    return (28.0 - y, 33.91 + 61.5 - x, -STANDOFF_W - z)


def uvw_box(u0, u1, v0, v1, w0, w1):
    return Pos((u0 + u1) / 2, (v0 + v1) / 2, (w0 + w1) / 2) * Box(abs(u1 - u0), abs(v1 - v0), abs(w1 - w0))


def pi_box(x0, x1, y0, y1, z0, z1):
    a, b = pi_uvw(x0, y0, z0), pi_uvw(x1, y1, z1)
    return uvw_box(min(a[0], b[0]), max(a[0], b[0]), min(a[1], b[1]), max(a[1], b[1]), min(a[2], b[2]), max(a[2], b[2]))


def pi_parts():
    import deckview as V
    return V.pi_parts()


def outer():
    """The body envelope: the rounded outline, with the side profile (deep at the bottom, thin at the
    top)."""
    v0, v1 = BODY_V
    plan = Pos(0, (v0 + v1) / 2, 0) * RectangleRounded(2 * BODY_U, v1 - v0, BODY_R)
    o = Pos(0, 0, -DEEP_W) * extrude(plan, amount=DEEP_W + FRONT_W)
    # The side profile in (v, w), on a plane across u.
    prof = Polygon((v0 - 1, FRONT_W + 1), (v0 - 1, -DEEP_W - 1), (STEP_V[0], -DEEP_W - 1), (STEP_V[0], -DEEP_W),
                   (STEP_V[1], -THIN_W), (v1 + 1, -THIN_W), (v1 + 1, FRONT_W + 1), align=None)
    side = extrude(Plane.YZ * prof, amount=BODY_U + 1, both=True)
    o = o & side
    back = o.edges().filter_by(lambda e: e.center().Z < FRONT_W - 0.5)
    for r in (4.0, 3.0, 2.0):
        try:
            return fillet(back, r)
        except Exception:
            pass
    return o


def ports_cut():
    """The openings of the Pi's ports, with room for the plug bodies."""
    p = pi_parts()
    cut = None
    for name, m in (("pi_ethernet", 1.5), ("pi_usb3", 2.0), ("pi_usb2", 2.0)):
        x0, x1, y0, y1, z0, z1 = p[name]
        b = pi_box(x0, x1, y0 - m, y1 + m, z0 - 1.0, z1 + 1.2)
        bb = b.bounding_box()
        c = uvw_box(bb.min.X, bb.max.X, BODY_V[0] - 1, bb.max.Y - 4, bb.min.Z, bb.max.Z)
        cut = c if cut is None else cut + c
    # The USB-C port in the right wall: room for the plug of the official supply (12 x 7).
    u, v, w = pi_uvw(11.2, -1.0, 1.6 + 1.6)
    cut = cut + uvw_box(u, BODY_U + 1, v - 6.6, v + 6.6, w - 4.0, w + 4.0)
    return cut


def vents():
    """Slots in the back over the fan of the cooler, and in the left wall beside the heatsink."""
    x0, x1, y0, y1, z0, z1 = pi_parts()["cooler"]
    b = pi_box(x0, x1, y0, y1, z0, z1).bounding_box()
    cut = None
    v = b.min.Y + 6
    while v < b.max.Y - 6:
        s = uvw_box(b.min.X + 6, b.max.X - 6, v - 1.2, v + 1.2, -DEEP_W - 1, -DEEP_W + WALL + 1)
        cut = s if cut is None else cut + s
        v += 5.0
    v = b.min.Y + 4
    while v < b.max.Y - 4:
        cut = cut + uvw_box(-BODY_U - 1, -BODY_U + WALL + 1, v - 1.2, v + 1.2, -DEEP_W + 6, -STANDOFF_W - 4)
        v += 5.0
    return cut


DOVE = (10.0, 14.0, 3.0, (STEP_V[1] + 3.0, 136.0))    # the dovetail on the back: root, tip, height, v range


def dovetail(clr=0.0, v_ext=0.0):
    r, t, h, (va, vb) = DOVE
    prof = Polygon((-r / 2 - clr, -THIN_W + 0.5), (r / 2 + clr, -THIN_W + 0.5), (t / 2 + clr, -THIN_W - h - clr),
                   (-t / 2 - clr, -THIN_W - h - clr), align=None)
    # Along v: the profile is in (u, w), on a plane across v.
    pl = Plane(origin=(0, va - clr, 0), x_dir=(1, 0, 0), z_dir=(0, 1, 0))
    return extrude(pl * Polygon(*[(p.X, -p.Y) for p in prof.vertices()], align=None), amount=vb - va + 2 * clr + v_ext)


def body():
    o = outer()
    front = o.faces().sort_by(lambda f: f.center().Z)[-1]
    s = offset(o, amount=-WALL, openings=[front])
    # The bosses of the two top tabs, and their screw holes (M2.5 from the back).
    for u, v in TABS:
        if v < 100:
            continue
        s = s + Pos(u, v, -(TAB_W + THIN_W) / 2) * Cylinder(3.0, THIN_W - TAB_W)
        s = s - Pos(u, v, -(TAB_W + THIN_W) / 2) * Cylinder(1.45, THIN_W - TAB_W + 2)
    # The hooks at the bottom: they catch the bottom edge of the frame (beside the Pi).
    for sgn in (-1, 1):
        u0, u1 = sorted((sgn * 29.5, sgn * (BODY_U - WALL + 0.01)))
        s = s + uvw_box(u0, u1, BODY_V[0] + WALL - 0.01, FRAME_V0 + 2.5, -FRAME_W - 2.2, -FRAME_W - 0.2)
        s = s - uvw_box(u0 - 1, u1 + 1, BODY_V[0] + WALL + 1.0, FRAME_V0 + 0.3, -FRAME_W - 0.25, FRONT_W)
    s = s - ports_cut()
    s = s - vents()
    s = s + dovetail()
    return s


# ---------------------------------------------------------------- the place on the TAZ Workhorse
TILT = S.TILT
SIN, COS = math.sin(TILT), math.cos(TILT)
U = (0.0, 0.0, -1.0)
V = (-SIN, COS, 0.0)
W = (COS, SIN, 0.0)
X_BACK = S.BACK_X[0]                     # the bracket stands on the flanges of the rail clips
PLATE = 4.0
GLASS_TOP_Y = 262.0 - S.Y_BASE           # under the logo
POD_Z = S.RAIL_SLOT_Z - 10.0 + GLASS[0] / 2   # the right edge of the glass 10 mm past the rail
# The top back corner of the deep part (the Pi) is 2 mm in front of the plate. The bracket is a wedge
# between the plate and the thin back over it.
_deep = (STEP_V[0], -DEEP_W)
POD_O = (X_BACK + PLATE + 2.0 + _deep[0] * SIN - _deep[1] * COS, GLASS_TOP_Y - GLASS[1] * COS, POD_Z)
POD_LOC = Location(Plane(origin=POD_O, x_dir=U, z_dir=W))


def to_mount(u, v, w):
    return tuple(POD_O[i] + u * U[i] + v * V[i] + w * W[i] for i in range(3))


def mbox(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(abs(x1 - x0), abs(y1 - y0), abs(z1 - z0))


COVER_SCREW = (S.X_FRONT - 6.1, 240.0 - S.Y_BASE)
CLIP_Y = (165.0, 207.0)
BAR_Y = (212.0, 240.0)


def bracket():
    """The TAZ bracket, in mount coordinates: a leg on the rail with the two clips, a bar across the top
    to the cover screw, and a wedge with the slot for the dovetail of the pod."""
    zc = S.RAIL_SLOT_Z
    k0, k1 = S.CLIP_KEY[0] / 2 + S.CLR, S.CLIP_KEY[1] / 2 + S.CLR
    leg = mbox(X_BACK, X_BACK + PLATE, CLIP_Y[0] - 14, BAR_Y[1], zc - 10, zc + 10)
    bar = mbox(X_BACK, X_BACK + PLATE, BAR_Y[0], BAR_Y[1], zc - 10, 3.3)
    b = leg + bar
    # The nub on the cover screw: a tab along the cover, its slot open at the bottom.
    x_s, y_s = COVER_SCREW
    nub = mbox(x_s - 9.0, X_BACK + 0.01, BAR_Y[0], BAR_Y[1], 0.0, 3.3)
    b = b + nub
    b = b - mbox(x_s - 3.5, x_s + 3.5, BAR_Y[0] - 1, y_s, -1, 4.3) - Pos(x_s, y_s, 1.65) * Cylinder(1.8, 6)
    # The dovetail slots of the clips (the same as the deck case).
    kd = S.BACK_X[1] - S.BACK_X[0]
    for yc in CLIP_Y:
        d0, d1 = yc - S.CLIP_L / 2 - S.CLIP_SLACK, yc + S.CLIP_L / 2 + S.CLIP_SLACK
        dov = Polygon((zc - k0, X_BACK - 0.01), (zc + k0, X_BACK - 0.01), (zc + k1, X_BACK + kd), (zc - k1, X_BACK + kd), align=None)
        b = b - Location(Plane(origin=(0, d0, 0), x_dir=(0, 0, 1), z_dir=(0, 1, 0))) * extrude(Plane.XY * dov, amount=d1 - d0)
        b = b - mbox(X_BACK - 1, X_BACK + kd + 0.2, d0 - S.CLIP_DROP, d0 + 0.5, zc - k1 - 0.2, zc + k1 + 0.2)
    # Zip-tie anchors on the front of the leg, under the pod: for the slack of the cables and to
    # take a pull off the plugs.
    for y in (CLIP_Y[0] - 6.0, CLIP_Y[0] + 14.0):
        add, cut = tie_anchor(X_BACK + PLATE - 0.01, y, zc, "taz")
        b = b + add - cut
    return b + wedge(POD_LOC, mbox(X_BACK + PLATE - 0.01, X_BACK + 400, -1000, 1000, -1000, 1000))


def tie_anchor(x0, y0, z0, axis):
    """A zip-tie anchor: a bridge on a plate, with a 4 x 2 mm passage under it for a tie up to
    3.6 mm wide. axis is the direction of the passage ("y" or "z"); the bridge stands out of the plate
    toward +x (TAZ) or -y (Mini 2): give the plate face as x0 or y0."""
    if axis == "taz":
        b = mbox(x0, x0 + 5.0, y0 - 6.0, y0 + 6.0, z0 - 4.0, z0 + 4.0)
        return b, mbox(x0 + 0.01, x0 + 2.4, y0 - 7.0, y0 + 7.0, z0 - 2.2, z0 + 2.2)
    b = mbox(x0 - 6.0, x0 + 6.0, y0 - 5.0, y0, z0 - 4.0, z0 + 4.0)
    return b, mbox(x0 - 7.0, x0 + 7.0, y0 - 2.4, y0 - 0.01, z0 - 2.2, z0 + 2.2)


def wedge(loc, front):
    """The wedge of a bracket: from the plate (front: the space in front of it) to the thin back of
    the pod at loc, behind the dovetail, with the slot of the dovetail open at the top."""
    va = DOVE[3][0]
    w = (loc * uvw_box(-13, 13, va - 2.0, BODY_V[1], -THIN_W - 80, -THIN_W - 0.3)) & front
    return w - loc * dovetail(0.25, 40.0)


def frame(u, v, w, o):
    return Location(Plane(origin=o, x_dir=u, z_dir=w))


# ---------------------------------------------------------------- the place on the Mini 2
# The Mini 2 model (build/mini2, from LulzBot's FreeCAD files): z up, the front toward -y. The front
# of the frame is at y = -252. The stock LCD stays: it hangs on the top corner of the right upright,
# on two frame screws. The pod goes on the top corner of the left upright, on the two mirror screws,
# with the same tilt and dovetail as on the TAZ.
MINI_U = (1.0, 0.0, 0.0)
MINI_W = (0.0, -COS, SIN)
MINI_V = (0.0, SIN, COS)
MINI_PLATE_Y = -252.0                     # the back of the bracket: the front of the frame
MINI_GLASS_TOP_Z = 375.0
MINI_X = -305.0                           # the mirror of the middle of the stock LCD
MINI_SCREWS = [(-334.1, 364.0), (-358.1, 352.0)]   # the frame screws in the left upright (x, z)
_front_y = MINI_PLATE_Y - PLATE - 2.0
MINI_O = (MINI_X, _front_y - (_deep[0] * MINI_V[1] + _deep[1] * MINI_W[1]), MINI_GLASS_TOP_Z - GLASS[1] * COS)
MINI_LOC = frame(MINI_U, MINI_V, MINI_W, MINI_O)


def mini_bracket():
    """The Mini 2 bracket: a plate on the front of the left upright, held by the two frame screws of
    its top corner (longer screws in the same holes), and the wedge with the dovetail slot."""
    top = to_frame(MINI_LOC, 0, BODY_V[1], -THIN_W)[2]
    plate = mbox(-366.0, MINI_X + 15.0, MINI_PLATE_Y - PLATE, MINI_PLATE_Y, top - 60.0, 372.0)
    for x, z in MINI_SCREWS:
        plate = plate - Pos(x, MINI_PLATE_Y - PLATE / 2, z) * Rot(90, 0, 0) * Cylinder(1.7, PLATE + 2)
    # Zip-tie anchors at the bottom of the plate, beside the pod: for the cables and their slack.
    for z in (top - 54.0, top - 36.0):
        add, cut = tie_anchor(-359.0, MINI_PLATE_Y - PLATE + 0.01, z, "mini")
        plate = plate + add - cut
    return plate + wedge(MINI_LOC, mbox(-1000, 1000, -1000, MINI_PLATE_Y - PLATE + 0.01, -1000, 1000))


def to_frame(loc, u, v, w):
    p = loc * Pos(u, v, w)
    return tuple(p.position)


PARTS = [("pod_body", body, "#1B1C20"), ("bracket", bracket, "#2A2B30"), ("mini_bracket", mini_bracket, "#2A2B30")]


# ---------------------------------------------------------------- the bought parts, for the check and the renders
def hardware():
    out = []
    glass = extrude(Pos(0, GLASS[1] / 2, -LENS) * RectangleRounded(GLASS[0], GLASS[1], GLASS_R), amount=LENS)
    out.append(("glass", glass, "#0D0E12"))
    out.append(("screen", uvw_box(-AA[0] / 2, AA[0] / 2, GLASS[1] / 2 - AA[1] / 2, GLASS[1] / 2 + AA[1] / 2, 0.0, 0.1), "#22324F"))
    out.append(("frame", uvw_box(-FRAME[0] / 2, FRAME[0] / 2, FRAME_V0, FRAME_V0 + FRAME[1], -FRAME_W, -LENS), "#8C9097"))
    for i, (u, v) in enumerate(TABS):
        out.append((f"tab{i}", uvw_box(u - TAB_SIZE[0] / 2, u + TAB_SIZE[0] / 2, v - TAB_SIZE[1] / 2, v + TAB_SIZE[1] / 2, -TAB_W, -FRAME_W), "#8C9097"))
    for i, (u, v) in enumerate(PI_HOLE_UV):
        out.append((f"standoff{i}", Pos(u, v, -(FRAME_W + STANDOFF_W) / 2) * Cylinder(2.5, STANDOFF_W - FRAME_W), "#C9CDD2"))
    for n, b in pi_parts().items():
        out.append((n, pi_box(*b), "#30343A" if n == "cooler" else ("#1F6B45" if n == "pi_board" else "#C9CDD2")))
    # The plugs: the power plug from the right, the printer cable into a USB 2 port from below.
    u, v, w = pi_uvw(11.2, -1.0, 3.2)
    out.append(("power_plug", uvw_box(u + 6.5, u + 6.5 + 24.0, v - 6.0, v + 6.0, w - 3.5, w + 3.5), "#F2F2F2"))
    x0, x1, y0, y1, z0, z1 = pi_parts()["pi_usb2"]
    b = pi_box(x0, x1, y0, y1, z0, z0 + 7.5).bounding_box()
    out.append(("printer_plug", uvw_box(b.min.X - 1.5, b.max.X + 1.5, b.min.Y - 30.0, b.min.Y + 2.0, b.min.Z - 0.6, b.max.Z + 0.6), "#151515"))
    return out


ALLOWED = {frozenset(p) for p in [("power_plug", "pi_usbc"), ("printer_plug", "pi_usb2"), ("glass", "screen"),
                                  ("glass", "frame"), ("frame", "pod_body")]}


def check(printed, hw):
    pi = set(pi_parts())
    items = [(n, s) for n, s, _ in printed] + [(n, s) for n, s, _ in hw]
    bad = []
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            (a, sa), (c, sc) = items[i], items[j]
            if frozenset((a, c)) in ALLOWED or (a in pi and c in pi):
                continue
            if (a.startswith("tab") or a.startswith("standoff")) and c in ("frame", "pi_board"):
                continue
            ba, bc = sa.bounding_box(), sc.bounding_box()
            if (ba.max.X < bc.min.X or bc.max.X < ba.min.X or ba.max.Y < bc.min.Y or bc.max.Y < ba.min.Y
                    or ba.max.Z < bc.min.Z or bc.max.Z < ba.min.Z):
                continue
            v = (sa & sc).volume
            if v > 0.5:
                bad.append((a, c, round(v, 1)))
    return bad


def matrix(loc):
    import numpy as np
    t = loc.wrapped.Transformation()
    m = np.eye(4)
    for i in range(3):
        for j in range(4):
            m[i, j] = t.Value(i + 1, j + 1)
    return m


def mini_printer():
    import glob
    import trimesh
    out = []
    for f in sorted(glob.glob(os.path.join(HERE, "build", "mini2", "*.stl"))):
        if "lcd" not in os.path.basename(f):
            out.append((trimesh.load(f, force="mesh"), "#C4C8CF" if "frame" in f else "#B0B4BC"))
    return out


def setups():
    """Each printer: the place of the pod, the frame of the bracket in world coordinates, the bracket,
    and the printer meshes (world)."""
    import numpy as np
    import kitview as K
    import render as R
    return {
        "taz": dict(loc=POD_LOC, world=K.mount_matrix(), bracket=bracket,
                    printer=lambda: R.printer_items() + [(R.logo_mark(), "#DDDDDD")], up=(0, 1, 0),
                    views=[((-0.75, -0.2, 0.62), "printer"), ((-0.6, -0.25, 0.75), "front")]),
        "mini2": dict(loc=MINI_LOC, world=np.eye(4), bracket=mini_bracket, printer=mini_printer, up=(0, 0, 1),
                      views=[((0.45, 1.0, -0.35), "printer"), ((0.5, 1.0, -0.3), "front")]),
    }


def local_meshes(body_local, hw_local, tmp):
    """The pod and its parts as meshes in pod coordinates."""
    import numpy as np
    import trimesh
    from build123d import export_stl
    meshes = {}
    for n, s, c in [("pod_body", body_local, "#1B1C20")] + hw_local:
        p = os.path.join(tmp, n + ".stl")
        export_stl(s, p, tolerance=0.03, angular_tolerance=0.1)
        meshes[n] = (trimesh.load(p, force="mesh"), c)
    # The Pi from its STEP model, for the looks: STEP -> pod.
    pth = os.path.join(HERE, "build", "ref", "pi5.stl")
    if os.path.exists(pth):
        m = trimesh.load(pth, force="mesh")
        T = np.eye(4)
        T[:3, :3] = np.array([[0, -1, 0], [-1, 0, 0], [0, 0, -1]], float)
        T[:3, 3] = (28.0, 33.91 + 61.5, -STANDOFF_W)
        m.apply_transform(T)
        for n in list(meshes):
            if n.startswith("pi_"):
                del meshes[n]
        meshes["pi5"] = (m, "#1F6B45")
    return meshes


# The live window shows each printer in the frame of the pod, z up: x = u to the right, z = v up,
# and the glass faces -y.
Z_UP = [[1, 0, 0, 0], [0, 0, -1, 0], [0, 1, 0, 0], [0, 0, 0, 1]]


def main(out, stills=True, which=("taz", "mini2")):
    import numpy as np
    import trimesh
    from build123d import export_stl
    import render as R
    import viewer
    os.makedirs(out, exist_ok=True)
    tmp = os.path.join(out, "mesh")
    os.makedirs(tmp, exist_ok=True)
    body_local = body()
    hw_local = hardware()
    bad = check([("pod_body", body_local, "")], hw_local)
    for a, c, v in bad:
        print(f"overlap: {a} and {c}: {v} mm3")
    print("no overlaps" if not bad else f"{len(bad)} overlaps")
    pod = local_meshes(body_local, hw_local, tmp)
    zup = np.array(Z_UP, float)
    for name, st in setups().items():
        if name not in which:
            continue
        loc = st["loc"]
        br = st["bracket"]()
        print(f"{name}: bracket and pod overlap: {(br & (loc * body_local)).volume:.1f} mm3, bracket {br.volume / 1000:.1f} cm3")
        p = os.path.join(tmp, f"{name}_bracket.stl")
        export_stl(br, p, tolerance=0.03, angular_tolerance=0.1)
        brm = trimesh.load(p, force="mesh")
        pod_to_world = st["world"] @ matrix(loc)
        world_to_view = zup @ np.linalg.inv(pod_to_world)
        printer = st["printer"]()
        # The window: the pod as it is, the bracket and the printer moved into the frame of the pod.
        items = [(n, zup_m(m, zup), c) for n, (m, c) in pod.items()]
        b = brm.copy()
        b.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("bracket", b, "#2A2B30"))
        by = {}
        for m, c in printer:
            by.setdefault(c, []).append(m)
        for i, (c, ms) in enumerate(by.items()):
            m = trimesh.util.concatenate(ms)
            m.apply_transform(world_to_view)
            items.append((f"printer{i}", m, c))
        # The cables, in the frame of the printer, then in the world and in the view.
        frame_to_world = st["world"]
        for cab, pts in routes(name, matrix(loc)).items():
            m, path = cable_mesh(pts, CABLE_D[cab])
            print(f"{name}: {cab} cable route {length(path) / 1000:.2f} m")
            mv = m.copy()
            mv.apply_transform(zup @ np.linalg.inv(matrix(loc)))
            items.append((f"{cab}_cable", mv, CABLE_COLOR[cab]))
            mw = m.copy()
            mw.apply_transform(frame_to_world)
            st.setdefault("cables", []).append((mw, CABLE_COLOR[cab]))
        p = os.path.join(tmp, f"{name}_clips.stl")
        export_stl(clips(name), p, tolerance=0.05, angular_tolerance=0.2)
        cm = trimesh.load(p, force="mesh")
        cv = cm.copy()
        cv.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("cable_clips", cv, "#E8762B"))
        cm.apply_transform(frame_to_world)
        st["cables"].append((cm, "#E8762B"))
        p = os.path.join(tmp, f"{name}_usb_b.stl")
        export_stl(usb_b_plugs(name), p, tolerance=0.05, angular_tolerance=0.2)
        plug = trimesh.load(p, force="mesh")
        pv_ = plug.copy()
        pv_.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("usb_b_plug", pv_, "#151515"))
        plug.apply_transform(frame_to_world)
        st["cables"].append((plug, "#151515"))
        gap = clearance([m for m, _ in pod.values()], pod_to_world, printer)
        print(f"{name}: the pod is {gap:.1f} mm from the printer at the closest point")
        note = ("no overlaps" if not bad else f"{len(bad)} overlaps") + f", {gap:.1f} mm from the printer"
        viewer.publish(items, name, note)
        if not stills:
            continue
        allm = []
        for n, (m, c) in pod.items():
            m = m.copy()
            m.apply_transform(pod_to_world)
            allm.append((m, c))
        b = brm.copy()
        b.apply_transform(st["world"])
        allm.append((b, "#2A2B30"))
        allm += st.get("cables", [])
        for d, tag in st["views"]:
            R.render((printer if tag == "printer" else []) + allm, d, st["up"], os.path.join(out, f"{name}-{tag}.png"),
                     size=(1200, 1200))
    bb = body_local.bounding_box()
    print(f"body {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x {bb.max.Z - bb.min.Z:.1f} mm, "
          f"{body_local.volume / 1000:.1f} cm3")
    return bad


def clearance(pod_meshes, pod_to_world, printer, n=20000):
    """The smallest distance from the surface of the pod to the printer, in mm (0 when they touch or
    cross), to about 0.5 mm: points on the pod, and a point tree of the printer surface within 30 mm
    of the pod."""
    import numpy as np
    import trimesh
    from scipy.spatial import cKDTree
    pod = trimesh.util.concatenate([m.copy() for m in pod_meshes])
    pod.apply_transform(pod_to_world)
    lo, hi = pod.bounds[0] - 30, pod.bounds[1] + 30
    cloud = []
    for m, _ in printer:
        b = m.bounds
        if not (np.all(b[1] >= lo) and np.all(b[0] <= hi)):
            continue
        tri = m.triangles
        keep = np.all((tri.max(axis=1) >= lo) & (tri.min(axis=1) <= hi), axis=1)
        if not keep.any():
            continue
        sub = m.submesh([np.nonzero(keep)[0]], append=True)
        k = int(min(300000, max(1000, sub.area / 0.25)))
        cloud.append(sub.sample(k))
        cloud.append(sub.vertices)
    if not cloud:
        return float("inf")
    tree = cKDTree(np.vstack(cloud))
    d, _ = tree.query(np.vstack([pod.sample(n), pod.vertices]), k=1)
    return float(d.min())


# ---------------------------------------------------------------- the cables
CABLE_D = {"power": 4.0, "printer": 4.5}
CABLE_COLOR = {"power": "#F2F2F2", "printer": "#151515"}
BEND_R = 15.0


TAZ_USB_B = (305.0, -84.0)       # mount (y, z) of the USB-B port on the rear face (x = -85): about
USB_B_PLUG = 28.0                # the length of the USB-B plug body outside the port
MINI_USB_B = (-377.4, 73.5)      # (x, z) of the USB-B port in the front of the Mini 2 electronics case
MINI_CASE_Y = -254.0             # the front of the case
MINI_UPRIGHT_X = -335.0          # the power cable runs down the left upright, right of the case
TAZ_COVER_Y = 7.5                # the two-cable clips on the side cover: the middle of the pair


def clips(name):
    """The printed cable clips (clips.py), placed in the frame of the printer."""
    import clips as C
    out = []
    if name == "taz":
        rail, _ = C.rail_clip()
        for y in (45.0, 90.0, 135.0):          # the cradles are 6 mm lower, on the straight run
            out.append(Location(Plane(origin=(S.RAIL_FRONT_X, y, S.RAIL_SLOT_Z), x_dir=(0, 0, 1), z_dir=(1, 0, 0))) * rail)
        pair, _ = C.tape_clip(True)
        for x in (50.0, -10.0, -70.0):
            out.append(Location(Plane(origin=(x, TAZ_COVER_Y, C.TAPE), x_dir=(0, 1, 0), z_dir=(0, 0, 1))) * pair)
    else:
        one, _ = C.tape_clip()
        for z in (60.0, 140.0, 220.0):
            out.append(Location(Plane(origin=(MINI_UPRIGHT_X, MINI_PLATE_Y - C.TAPE, z), x_dir=(1, 0, 0), z_dir=(0, -1, 0))) * one)
    s = out[0]
    for o in out[1:]:
        s = s + o
    return s


def usb_b_plugs(name):
    """The USB-B plug in the port of the printer, in the coordinates of the printer's frame."""
    if name == "taz":
        py, pz = TAZ_USB_B
        return mbox(-85.0 - USB_B_PLUG, -85.0, py - 5.5, py + 5.5, pz - 6.0, pz + 6.0)
    px, pz = MINI_USB_B
    return mbox(px - 6.0, px + 6.0, MINI_CASE_Y - USB_B_PLUG, MINI_CASE_Y, pz - 5.5, pz + 5.5)


def cable_starts():
    """Where each cable leaves its plug, in pod coordinates: (point, direction)."""
    u, v, w = pi_uvw(11.2, -1.0, 3.2)
    x0, x1, y0, y1, z0, z1 = pi_parts()["pi_usb2"]
    b = pi_box(x0, x1, y0, y1, z0, z0 + 7.5).bounding_box()
    return {"power": ((u + 6.5 + 24.0, v, w), (1.0, 0.0, 0.0)),
            "printer": (((b.min.X + b.max.X) / 2, b.min.Y - 30.0, (b.min.Z + b.max.Z) / 2), (0.0, -1.0, 0.0))}


def routes(name, pod_to_frame):
    """The route of each cable in the coordinates of the printer's frame: from the end of its plug,
    straight on for 20 mm, then through the waypoints. The port of the printer is a guess (marked)."""
    import numpy as np
    out = {}
    for cab, (p, d) in cable_starts().items():
        P = pod_to_frame @ np.array([*p, 1.0])
        D = pod_to_frame[:3, :3] @ np.array(d)
        p0, p1 = P[:3], P[:3] + 20.0 * D
        x, y, z = p1
        if name == "taz":
            # Mount coordinates: x to the front, y up (the desk is y = -14), z out of the cover.
            desk = -12.0
            import clips as C
            rz, rx = S.RAIL_SLOT_Z, S.RAIL_FRONT_X + C.RAIL_CABLE_H
            cz = C.TAPE_CABLE_H                        # the cables on the cover clips
            yp, yu = TAZ_COVER_Y - C.PAIR / 2, TAZ_COVER_Y + C.PAIR / 2
            if cab == "power":
                # To the front rail, down its slot in printed clips, across the bottom of the front
                # to the cover side, back along the lower edge of the cover, and out behind the
                # printer to the outlet.
                way = [(x, y, rz - 6.0), (rx + 6.0, y - 25.0, rz), (rx, y - 45.0, rz), (rx, 30.0, rz), (rx + 6, yp, rz + 15.0),
                       (rx + 6, yp, -10.0), (rx - 4.0, yp, cz), (60.0, yp, cz), (-80.0, yp, cz), (-100.0, desk, cz),
                       (-250.0, desk, cz)]
            else:
                # Under the pod to the cover side, back along the outside of the cover, round the
                # rear corner, and up the rear face to the USB-B port of the RAMBo. The port is
                # left of the second extruder connector (seen from behind), at about its height.
                # Down the front under the pod, along the lower edge of the cover with the power
                # cable, round the rear corner and up the rear face to the port.
                py, pz = TAZ_USB_B
                way = [(x, 30.0, z), (rx + 12, yu, z + 10.0), (rx + 12, yu, -10.0), (rx - 4.0, yu, cz), (60.0, yu, cz),
                       (-80.0, yu, cz), (-94.0, yu, -8.0), (-94.0, py, -8.0), (-94.0, py, pz),
                       (-85.0 - USB_B_PLUG, py, pz)]
        else:
            # The Mini 2: z up (the desk is z = -7), the front toward -y.
            desk = -5.0
            if cab == "power":
                # Back behind the pod, down the front of the left upright (clear of the opening and
                # of the stock LCD), and out to the left on the desk.
                import clips as C
                ux, uy = MINI_UPRIGHT_X, MINI_PLATE_Y - C.TAPE_CABLE_H
                way = [(x + 4, y + 10, z - 20), (x, uy, z - 45.0), (ux, uy, z - 60.0),
                       (ux, uy, 30.0), (ux, MINI_PLATE_Y - 20.0, desk), (-480.0, MINI_PLATE_Y - 40.0, desk),
                       (-560.0, MINI_PLATE_Y - 80.0, desk)]
            else:
                # Down the front of the left upright to the USB-B port in the front of the
                # electronics case; the plug goes in from the front.
                px, pz = MINI_USB_B
                way = [(x, y, 160.0), (px, y, 120.0), (px, y, pz + 20), (px, MINI_CASE_Y - USB_B_PLUG - 12, pz),
                       (px, MINI_CASE_Y - USB_B_PLUG, pz)]
        out[cab] = [tuple(p0), tuple(p1)] + way
    return out


def rounded(points, r=BEND_R, steps=8):
    """A path through the points with a round bend of radius about r at each corner."""
    import numpy as np
    pts = [np.array(p, float) for p in points]
    out = [pts[0]]
    for i in range(1, len(pts) - 1):
        a, b, c = pts[i - 1], pts[i], pts[i + 1]
        la, lc = np.linalg.norm(b - a), np.linalg.norm(c - b)
        k = min(r, la / 2, lc / 2)
        p, q = b + (a - b) / la * k, b + (c - b) / lc * k
        for t in np.linspace(0, 1, steps):
            out.append((1 - t) ** 2 * p + 2 * (1 - t) * t * b + t ** 2 * q)
    out.append(pts[-1])
    return np.array(out)


def cable_mesh(points, d):
    import trimesh
    from shapely.geometry import Point
    path = rounded(points)
    return trimesh.creation.sweep_polygon(Point(0, 0).buffer(d / 2, 12), path), path


def length(path):
    import numpy as np
    return float(np.linalg.norm(np.diff(path, axis=0), axis=1).sum())


def zup_m(m, zup):
    m = m.copy()
    m.apply_transform(zup)
    return m


if __name__ == "__main__":
    args = [a for a in sys.argv[1:] if a != "live"]
    main(args[0] if args else os.path.join(HERE, "build", "pod"), stills="live" not in sys.argv)
