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
    # The wedge: from the plate to the back of the pod, behind the dovetail.
    r, t, h, (va, vb) = DOVE
    ya, yb = to_mount(0, va - 2.0, -THIN_W)[1], to_mount(0, vb, -THIN_W)[1]
    wedge = mbox(X_BACK + PLATE - 0.01, X_BACK + 60, ya - 10, yb + 10, POD_Z - 13, POD_Z + 13)
    wedge = wedge & (POD_LOC * uvw_box(-13, 13, va - 2.0, vb + 30, -THIN_W - 40, -THIN_W - 0.3))
    wedge = wedge - POD_LOC * dovetail(0.25, 40.0)
    b = b + wedge
    # A rib from the bar down to the wedge.
    return b


PARTS = [("pod_body", lambda: POD_LOC * body(), "#1B1C20"), ("bracket", bracket, "#2A2B30")]


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


def publish_live(meshes, printer, bad):
    """Show the model in the live window (live.py). The printer goes once: its parts by color."""
    import trimesh
    import viewer
    import numpy as np
    import kitview as K
    # The window shows the pod in its own frame, z up: x = u to the right, z = v up, and the glass
    # faces -y. The printer goes in the same frame.
    P = np.eye(4)
    P[:3, 0], P[:3, 1], P[:3, 2], P[:3, 3] = U, V, W, POD_O
    Z_UP = np.array([[1, 0, 0, 0], [0, 0, -1, 0], [0, 1, 0, 0], [0, 0, 0, 1]], float)
    to_pod = Z_UP @ np.linalg.inv(K.mount_matrix() @ P)

    def local(m):
        m = m.copy()
        m.apply_transform(to_pod)
        return m
    note = "no overlaps" if not bad else f"{len(bad)} overlaps"
    viewer.publish([(n, local(m), c) for n, (m, c) in meshes.items()], "pod", note)
    if not os.path.exists(os.path.join(viewer.OUT, "printer.json")):
        by = {}
        for m, c in printer:
            by.setdefault(c, []).append(m)
        viewer.publish([(f"printer{i}", local(trimesh.util.concatenate(ms)), c) for i, (c, ms) in enumerate(by.items())], "printer")


def main(out, stills=True):
    import numpy as np
    import trimesh
    from build123d import export_stl
    import kitview as K
    import render as R
    os.makedirs(out, exist_ok=True)
    body_local = body()
    hw_local = hardware()
    bad = check([("pod_body", body_local, "")], hw_local)
    for a, c, v in bad:
        print(f"overlap: {a} and {c}: {v} mm3")
    print("no overlaps" if not bad else f"{len(bad)} overlaps")
    br = bracket()
    pod_back = (POD_LOC * body_local)
    gap = (br & pod_back).volume
    print(f"bracket and pod overlap: {gap:.1f} mm3")
    M = K.mount_matrix()
    tmp = os.path.join(out, "mesh")
    os.makedirs(tmp, exist_ok=True)
    meshes = {}
    items = [("pod_body", pod_back, "#1B1C20"), ("bracket", br, "#2A2B30")] + [(n, POD_LOC * s, c) for n, s, c in hw_local]
    for n, s, c in items:
        p = os.path.join(tmp, n + ".stl")
        export_stl(s, p, tolerance=0.03, angular_tolerance=0.1)
        m = trimesh.load(p, force="mesh")
        m.apply_transform(M)
        meshes[n] = (m, c)
    # The Pi from its STEP model, for the looks: STEP -> pod -> mount -> world.
    pth = os.path.join(HERE, "build", "ref", "pi5.stl")
    if os.path.exists(pth):
        m = trimesh.load(pth, force="mesh")
        T = np.eye(4)
        T[:3, :3] = np.array([[0, -1, 0], [-1, 0, 0], [0, 0, -1]], float)
        T[:3, 3] = (28.0, 33.91 + 61.5, -STANDOFF_W)
        P = np.eye(4)
        P[:3, 0], P[:3, 1], P[:3, 2], P[:3, 3] = U, V, W, POD_O
        m.apply_transform(T)
        m.apply_transform(P)
        m.apply_transform(M)
        for n in list(meshes):
            if n.startswith("pi_"):
                del meshes[n]
        meshes["pi5"] = (m, "#1F6B45")
    allm = list(meshes.values())
    printer = R.printer_items() + [(R.logo_mark(), "#DDDDDD")]
    publish_live(meshes, printer, bad)
    if not stills:
        return bad
    R.render(printer + allm, (-0.75, -0.2, 0.62), (0, 1, 0), os.path.join(out, "pod-printer.png"), size=(1200, 1200))
    R.render(allm, (-0.6, -0.25, 0.75), (0, 1, 0), os.path.join(out, "pod-front.png"), size=(1000, 1200))
    R.render(allm, (0.05, -0.15, 1.0), (0, 1, 0), os.path.join(out, "pod-side.png"), size=(900, 1200))
    R.render(allm, (-0.3, -0.9, 0.45), (0, 1, 0), os.path.join(out, "pod-under.png"), size=(1200, 1000))
    inside = [v for k, v in meshes.items() if k not in ("pod_body", "bracket", "glass", "screen")]
    R.render(inside + [meshes["pod_body"]], (0.9, -0.2, 0.4), (0, 1, 0), os.path.join(out, "pod-back.png"), size=(1000, 1200))
    R.render(inside, (0.9, -0.2, 0.4), (0, 1, 0), os.path.join(out, "pod-inside.png"), size=(1000, 1200))
    bb = body_local.bounding_box()
    print(f"body {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x {bb.max.Z - bb.min.Z:.1f} mm, "
          f"{body_local.volume / 1000:.1f} cm3; bracket {br.volume / 1000:.1f} cm3")
    return bad


if __name__ == "__main__":
    args = [a for a in sys.argv[1:] if a != "live"]
    main(args[0] if args else os.path.join(HERE, "build", "pod"), stills="live" not in sys.argv)
