"""The Slate v4: the pod on a desk stand. The pod (pod.py) is the navy top, with no change. The stand is
the orange bottom: one printed part with no hardware.

The stand is a slim spine and a flat foot. The spine is behind the pod, parallel to its back. Its top
has the tapered slot of the wedge (pod.wedge), so the dovetail of the mount drops into it as it drops
into a printer bracket. The pod leans back by pod.TILT (15 degrees), the same as on the printers and
on the earlier Slate. The spine gets thicker toward the foot. The foot has two windows, and the pod
hangs over the front window. The bottom of the pod is clear of the foot.

Two diamond holes through the spine take twist ties for the cables down the back of the spine. The
power cable leaves the left wall of the pod, outside the foot.

World coordinates: x to the right, y to the back, z up. The desk is z = 0, and the bottom front edge
of the glass is at y = 0.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with shapely
--with networkx python slate.py [export | check | render]

export writes stl/pod/fdm/slate-stand_fdm.stl and stl/pod/resin/slate-stand_resin.stl. check prints
the overlap, the overhang and the tip checks. render writes renders/slate-*.png (headless EGL: set
PYOPENGL_PLATFORM=egl, and add --with pyrender --with pillow).
"""

import math
import os
import sys

from build123d import (Box, Location, Plane, Polygon, Pos, Rot, RectangleRounded, chamfer, extrude)

import pod as P

HERE = os.path.dirname(os.path.abspath(__file__))

TILT = P.TILT
SIN, COS = math.sin(TILT), math.cos(TILT)
U, V, W = (1.0, 0.0, 0.0), (0.0, SIN, COS), (0.0, -COS, SIN)

# The spine: as wide as the wedge (pod.wedge), from the top of the wedge down to the foot. The wall
# behind the slot is SKIN at the top. The spine gets thicker toward the foot, to ROOT at the top of
# the foot (along w).
SPINE_U = 13.0
SPINE_TOP = P.MOUNT_V[1]
SPINE_W0 = -P.BACK_W - 0.3               # the front face of the spine: as the wedge, 0.3 behind the mount
SKIN = 3.0
ROOT = 12.0
# The foot: a flat plate with round corners and two windows.
FOOT_T = 4.0
FOOT_HALF = 40.0                         # half width: 5.7 mm in from the sides of the glass
# The front is 6 mm behind the glass, as on the earlier Slate. The back is far enough back that a push
# on the screen slides the stand on rubber feet before it tips (check()).
FOOT_Y = (6.0, 125.0)
FOOT_R = 10.0
RIM = 11.0                               # the rim around the windows
TOP_EDGE = 1.0                           # the chamfer of the top edge of the foot
GAP = 6.0                                # the lowest point of the pod over the top of the foot
# The twist-tie holes: diamonds through the spine, across x. Their sides rise at 50 degrees.
TIE_V = (30.0, 62.0)                     # v of the holes
TIE_HALF = 1.6                           # half width (y) of the diamond; its half height is HALF x SLOPE
TIE_IN = 3.5                             # the middle of a hole, in from the back face of the spine
COLORS = {"navy": "#1F2B4D", "orange": "#E8742A"}


def pod_loc(h0):
    return Location(Plane(origin=(0.0, 0.0, h0), x_dir=U, z_dir=W))


def to_world(h0, v, w):
    """(y, z) of the pod point (v, w)."""
    return v * SIN - w * COS, h0 + v * COS + w * SIN


def pod_parts():
    """The printed parts of the pod and the glass, in pod coordinates."""
    glass = [s for n, s, _ in P.hardware() if n == "glass"][0]
    return {"pod_body": P.body(), "pod_frame": P.pod_frame(), "pod_mount": P.mount(), "glass": glass}


def pod_height(parts):
    """h0: the height of the bottom front edge of the glass, so that the lowest point of the pod is GAP
    over the foot."""
    low = min((pod_loc(0.0) * s).bounding_box().min.Z for s in parts.values())
    return FOOT_T + GAP - low


def spine(h0):
    w0 = SPINE_W0
    w_top = w0 - (P.DOVE[2] + P.DOVE_CLR - 0.3 + SKIN)          # the slot tip and SKIN behind it
    v_foot = (FOOT_T - h0 - w0 * SIN) / COS                     # the front face meets the top of the foot
    v_low = v_foot - 40.0
    k = (ROOT - (w0 - w_top)) / (SPINE_TOP - v_foot)            # the thickness grows by k for each mm down
    w_low = w0 - ROOT - k * (v_foot - v_low)
    prof = Polygon((SPINE_TOP, w0), (SPINE_TOP, w_top), (v_low, w_low), (v_low, w0), align=None)
    pl = Plane(origin=(0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    s = pod_loc(h0) * extrude(pl * prof, amount=SPINE_U, both=True)
    s = s & (Pos(0, 0, 250) * Box(500, 500, 500))
    s = s - pod_loc(h0) * P.dovetail(P.DOVE_CLR, 40.0)
    # The twist-tie holes, in the world (y, z) plane, so that their sides rise at 50 degrees.
    for v in TIE_V:
        wb = w0 - ROOT - k * (v_foot - v) + TIE_IN
        y, z = to_world(h0, v, wb)
        a, b = TIE_HALF, TIE_HALF * P.SLOPE
        d = Polygon((y - a, z), (y, z - b), (y + a, z), (y, z + b), align=None)
        s = s - Plane(origin=(0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0)) * extrude(d, amount=SPINE_U + 1, both=True)
    return s, v_foot, k


def foot(h0, v_foot, k):
    y0, y1 = FOOT_Y
    plate = extrude(Pos(0, (y0 + y1) / 2, 0) * RectangleRounded(2 * FOOT_HALF, y1 - y0, FOOT_R), amount=FOOT_T)
    top = plate.edges().filter_by(lambda e: e.center().Z > FOOT_T - 0.01)
    plate = chamfer(top, TOP_EDGE)
    bed = plate.edges().filter_by(lambda e: e.center().Z < 0.01)
    plate = chamfer(bed, P.FOOT, angle=math.degrees(math.atan(P.SLOPE)), reference=plate.faces().sort_by().first)
    # The windows: in front of the spine and behind it.
    ys0, _ = to_world(h0, v_foot, SPINE_W0)
    ys1, _ = to_world(h0, v_foot, SPINE_W0 - ROOT)
    x = FOOT_HALF - RIM
    for a, b in ((y0 + RIM, ys0 - 1.0), (ys1 + 6.0, y1 - RIM)):
        plate = plate - extrude(Pos(0, (a + b) / 2, -1) * RectangleRounded(2 * x, b - a, FOOT_R - RIM / 2), amount=FOOT_T + 2)
    return plate


def stand(clr=None):
    """The stand in world coordinates, and h0. clr is the gap of the slot (pod.DOVE_CLR)."""
    keep = P.DOVE_CLR
    if clr is not None:
        P.DOVE_CLR = clr
    try:
        parts = pod_parts()
        h0 = pod_height(parts)
        s, v_foot, k = spine(h0)
        return s + foot(h0, v_foot, k), h0, parts
    finally:
        P.DOVE_CLR = keep


# ---------------------------------------------------------------- the tip check
# The masses of the bought parts are estimates (no part was weighed): the display 120 g, the Pi 5
# 46 g, the Active Cooler 22 g. The screws, the inserts and the cables are out. The printed parts are
# solid: PLA 1.24 g/cm3, the resin 1.1 g/cm3 (profiles/peopoly-phenom/resin.json). The FDM stand has
# infill: a CuraEngine slice with the pla-0.20-standard profile gives 37 g, FDM_FILL of the solid mass.
DISPLAY_G, PI_G, COOLER_G = 120.0, 46.0, 22.0
DENSITY = {"fdm": 1.24, "resin": 1.1}
FDM_FILL = 0.67
MU = {"bare foot (PLA on wood)": 0.3, "rubber feet": 0.6}


def masses(stand_shape, h0, parts, process):
    """(name, grams, (y, z)) of each mass, in the world."""
    import numpy as np
    loc = pod_loc(h0)
    out = []
    for n in ("pod_body", "pod_frame", "pod_mount"):
        s = loc * parts[n]
        c = s.center()
        out.append((n, s.volume / 1000 * DENSITY[process], (c.Y, c.Z)))
    out.append(("display", DISPLAY_G, to_world(h0, P.FRAME_V0 + P.FRAME[1] / 2, -P.FRAME_W / 2)))
    pc = P.pi_box(0, 85, 0, 56, 0, 1.6).center()
    out.append(("pi5", PI_G, to_world(h0, pc.Y, pc.Z)))
    cb = P.pi_box(*P.pi_parts()["cooler"]).center()
    out.append(("cooler", COOLER_G, to_world(h0, cb.Y, cb.Z)))
    c = stand_shape.center()
    fill = FDM_FILL if process == "fdm" else 1.0
    out.append(("stand", stand_shape.volume / 1000 * DENSITY[process] * fill, (c.Y, c.Z)))
    return out


def tip(stand_shape, h0, parts, process):
    import numpy as np
    ms = masses(stand_shape, h0, parts, process)
    m = sum(g for _, g, _ in ms)
    yc = sum(g * p[0] for _, g, p in ms) / m
    zc = sum(g * p[1] for _, g, p in ms) / m
    mg = m / 1000 * 9.81
    bb = stand_shape.bounding_box()
    yf, yr, xh = bb.min.Y, bb.max.Y, bb.max.X
    r = {"mass_g": m, "com_y": yc, "com_z": zc, "front_y": yf, "back_y": yr,
         "tip_back_deg": math.degrees(math.atan2(yr - yc, zc)), "tip_front_deg": math.degrees(math.atan2(yc - yf, zc)),
         "tip_side_deg": math.degrees(math.atan2(xh, zc))}
    # A finger pushes the glass along -w, at a point (v) on the middle line of the screen. The stand
    # tips back on the back edge of the foot when the moment of the push is more than the moment of
    # the weight.
    aa0 = P.GLASS[1] / 2 - P.AA[1] / 2
    for name, v in (("top", aa0 + P.AA[1]), ("middle", P.GLASS[1] / 2)):
        y, z = to_world(h0, v, 0.0)
        arm = COS * z - SIN * (yr - y)
        r[f"push_{name}_N"] = mg * (yr - yc) / arm if arm > 0 else math.inf
    # The push slides the stand when its part along the desk is more than the friction.
    for k, mu in MU.items():
        r[f"slide_{k}_N"] = mu * mg / (COS - mu * SIN)
    return r


def check():
    import trimesh
    s, h0, parts = stand()
    loc = pod_loc(h0)
    print(f"h0 (the bottom front edge of the glass over the desk): {h0:.1f} mm")
    print(f"stand: {s.volume / 1000:.1f} cm3, {s.bounding_box().size.X:.1f} x {s.bounding_box().size.Y:.1f} x "
          f"{s.bounding_box().size.Z:.1f} mm")
    bad = 0
    for n, p in parts.items():
        v = (s & (loc * p)).volume
        print(f"overlap with {n}: {v:.2f} mm3")
        bad += v > 0.01
    # The pod lifts DOVE_LIFT up the slot to come off: check that path too.
    for d in (P.DOVE_LIFT / 2, P.DOVE_LIFT):
        v = sum((s & (loc * Pos(0, d, 0) * p)).volume for p in parts.values())
        print(f"overlap with the pod lifted {d:.0f} mm: {v:.2f} mm3")
        bad += v > 0.01
    for process in ("fdm", "resin"):
        r = tip(s, h0, parts, process)
        print(f"{process}: mass {r['mass_g']:.0f} g, center of mass y {r['com_y']:.1f} z {r['com_z']:.1f}; foot y "
              f"{r['front_y']:.1f} to {r['back_y']:.1f}")
        print(f"  tip angle: back {r['tip_back_deg']:.1f} deg, front {r['tip_front_deg']:.1f} deg, side "
              f"{r['tip_side_deg']:.1f} deg")
        print(f"  push to tip back: top of the screen {r['push_top_N']:.2f} N, middle {r['push_middle_N']:.2f} N")
        for k in MU:
            print(f"  push to slide, {k}: {r[f'slide_{k}_N']:.2f} N")
    return bad


# ---------------------------------------------------------------- the print files
# FDM: the stand prints on the bottom of its foot, as it stands on the desk. The slot then has the
# same angle to the bed as the slot of the wedge, which prints standing. Resin: the stand turns
# RESIN_ROT, so the spine is vertical and the bottom of the foot faces the build plate at 15 degrees.
# It is on supports RESIN_LIFT over the plate. The supports go on the bottom of the foot. The walls
# of the slot face away from w = 0 and the end of the slot faces w = 0: only with the spine vertical
# does no face of the slot face the plate (slot_down()).
RESIN_ROT = Rot(15, 0, 0)


def print_parts():
    out = []
    for process in ("fdm", "resin"):
        s, _, _ = stand(P.DOVE_CLR_BY[process])
        if process == "fdm":
            out.append((process, f"slate-stand_{process}.stl", P.on_bed(s, Rot(0, 0, 0))))
        else:
            out.append((process, f"slate-stand_{process}.stl", P.on_bed(s, RESIN_ROT, P.RESIN_LIFT)))
    return out


def export():
    from build123d import export_stl
    for process, name, shape in print_parts():
        p = os.path.join(P.PRINT_DIR, process, name)
        export_stl(shape, p, tolerance=0.01, angular_tolerance=0.1)
        bb = shape.bounding_box()
        print(f"{process}/{name}: {bb.size.X:.1f} x {bb.size.Y:.1f} x {bb.size.Z:.1f} mm")


def mesh_check():
    """The mesh check of make.sh on the FDM file, and the overhang check of pod.py. For the resin file,
    the area that faces the build plate in the slot (it must be 0: no supports in the slot)."""
    import trimesh
    p = os.path.join(P.PRINT_DIR, "fdm", "slate-stand_fdm.stl")
    m = trimesh.load(p, force="mesh")
    ok = m.is_watertight and m.is_winding_consistent and m.body_count == 1
    nz, zc, area = m.face_normals[:, 2], m.triangles_center[:, 2], m.area_faces
    above = zc > m.bounds[0][2] + 0.05
    flat = area[above & (nz < -0.999)].sum()
    steep = area[above & (nz < -math.cos(math.radians(40))) & (nz >= -0.999)].sum()
    print(f"  {os.path.relpath(p, HERE)}: {'closed, one part' if ok else 'NOT CLEAN'}; flat ceilings {flat:.1f} mm2, "
          f"faces under 40 deg {steep:.1f} mm2")
    need, short, span, long_ = P.overhang_report(m)
    print(f"  pod.py overhang check: needs support {need:.1f} mm2; bridges {short:.1f} mm2, longest {span:.1f} mm")
    r = trimesh.load(os.path.join(P.PRINT_DIR, "resin", "slate-stand_resin.stl"), force="mesh")
    rok = r.is_watertight and r.body_count == 1
    down = slot_down()
    print(f"  resin file: {'closed, one part' if rok else 'NOT CLEAN'}; slot faces toward the plate {down:.1f} mm2")
    return ok and rok and flat <= 1.0 and steep <= 1.0 and need <= 1.0 and down <= 1.0


def slot_down():
    """The area of the faces of the slot that face the build plate in the resin orientation."""
    import tempfile
    import numpy as np
    import trimesh
    from build123d import export_stl
    s, h0, _ = stand(P.DOVE_CLR_BY["resin"])
    p = os.path.join(tempfile.mkdtemp(), "stand.stl")
    export_stl(s, p, tolerance=0.01, angular_tolerance=0.1)
    m = trimesh.load(p, force="mesh")
    # The faces in the slot, from the pod coordinates of their centers.
    c = m.triangles_center
    v = c[:, 1] * SIN + (c[:, 2] - h0) * COS
    w = -c[:, 1] * COS + (c[:, 2] - h0) * SIN
    r, t, h, (va, vb) = P.DOVE
    slot = (np.abs(c[:, 0]) < t / 2 + 1.0) & (w < -P.BACK_W) & (w > -P.BACK_W - h - 1.0) & (v > va - 21.0)
    rot = np.array(RESIN_ROT.wrapped.Transformation().VectorialPart().Row(3).Coord())
    nz = m.face_normals @ rot
    return float(m.area_faces[slot & (nz < -0.01)].sum())


# ---------------------------------------------------------------- the renders
def render():
    import tempfile
    import numpy as np
    import trimesh
    from build123d import export_stl
    import render as R
    s, h0, parts = stand()
    loc = pod_loc(h0)
    navy, orange = COLORS["navy"], COLORS["orange"]
    color = {"pod_body": navy, "pod_frame": navy, "pod_mount": navy, "glass": "#0D0E12", "screen": "#22324F",
             "power_plug": "#F2F2F2", "printer_plug": "#151515", "cooler": "#30343A"}
    shown = [("stand", s, orange)] + [(n, loc * p, color[n]) for n, p in parts.items()]
    for n, sh, _ in P.hardware():
        if n in ("screen", "power_plug", "printer_plug") or n.startswith("pi_usb") or n == "pi_ethernet":
            shown.append((n, loc * sh, color.get(n, "#C9CDD2")))
    tmp = tempfile.mkdtemp()
    items = []
    for n, sh, c in shown:
        p = os.path.join(tmp, n + ".stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        items.append((trimesh.load(p, force="mesh"), c))
    out = os.path.join(HERE, "renders")
    # The render looks along view_dir: from the front left and above, and from the back right.
    R.render(items, (0.55, 0.8, -0.45), (0, 0, 1), os.path.join(out, "slate-front.png"), size=(1400, 1600))
    R.render(items, (-0.6, -0.75, -0.35), (0, 0, 1), os.path.join(out, "slate-back.png"), size=(1400, 1600))
    R.render(items, (1, 0, 0), (0, 0, 1), os.path.join(out, "slate-side.png"), size=(1400, 1400))


if __name__ == "__main__":
    if "export" in sys.argv:
        export()
        sys.exit(0 if mesh_check() else 1)
    if "render" in sys.argv:
        render()
        sys.exit()
    sys.exit(1 if check() else 0)
