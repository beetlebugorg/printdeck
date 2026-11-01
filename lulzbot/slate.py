"""The Slate v4: the desk case of Printdeck. It holds the Raspberry Pi Touch Display 2 (5 inch, tall) and the
Raspberry Pi 5 with its Active Cooler. The navy case is the top, and the orange base is the bottom. Two
M3 screws hold the case on the base.

The case has the shape of the pod (pod.py): the frame around the display, and the body around the Pi,
with the ports of the Pi flush with its walls. pod.py makes the geometry, and this file uses it with no
change.
The body has two rings on its inner floor, at the holes of the pod mount, with a pocket for an M3 hex
nut in each.
- Resin: the frame and the body are one part, slate-case_resin.stl. The display goes in from the front
  with the Pi on its back, and four M2.5 screws from the box of the display hold it. Two wells through
  the back give access to the two lower screws.
- FDM: one part needs supports, so the case is two parts, slate-frame_fdm.stl and slate-body_fdm.stl,
  with four M3 x 6 screws into heat-set inserts, as the frame and the body of the pod.

The base is one part, after the chin and the back plate of the earlier Slate:
- the chin: a band across the full width of the glass, under its bottom edge, with a lip in front of
  the glass;
- the cheeks: the two sides of the chin go up beside the frame, under the edges of the glass, to below
  the ports in the left wall;
- the ribs under the bottom of the case;
- the back plate behind the body, GAP behind it, so the air goes to the vents in the back of the body;
- the boss at the top of the back plate and the pad at its bottom, on the back of the body. Two M3 x 8
  screws from behind go through the boss and the back of the body into the nuts, and pull the back of
  the body onto the boss and the pad. The case is complete before it goes on the base;
- the foot: a flat plate on the desk, back to FOOT_BACK.

The case leans back by pod.TILT (15 degrees), as the earlier Slate.

World coordinates: x to the right, y to the back, z up. The desk is z = 0, and the bottom front edge
of the glass is at y = 0, z = H0. Case coordinates (u, v, w) are the pod coordinates of pod.py.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with shapely
--with networkx python slate.py [export | check | render]

export writes the print files to stl/slate/ and checks them. check prints the overlap, the tip and the
stiffness checks. render writes renders/slate-*.png (headless EGL: set PYOPENGL_PLATFORM=egl, and add
--with pyrender --with pillow).
"""

import math
import os
import sys

from build123d import Box, Circle, Location, Plane, Polygon, Pos, Rot, RectangleRounded, chamfer, extrude

import pod as P

HERE = os.path.dirname(os.path.abspath(__file__))
PRINT_DIR = os.path.join(HERE, "stl", "slate")

TILT = P.TILT
SIN, COS = math.sin(TILT), math.cos(TILT)
U, V, W = (1.0, 0.0, 0.0), (0.0, SIN, COS), (0.0, -COS, SIN)

H0 = 8.5                                 # the bottom front edge of the glass over the desk
FIT_BY = {"fdm": 0.3, "resin": 0.15}     # the gap between the case and the base, on each side
SIDE = P.GLASS[0] / 2                    # the half width of the base: flush with the glass
LIP = (1.5, 3.0)                         # the lip in front of the glass: thickness, height over the glass edge
CHIN_W = -6.0                            # the back face of the chin, under the glass
CHEEK_TOP = 30.0                         # the cheeks end below the USB-C plug (v 35.6)
RIBS = (-24.0, 0.0, 24.0, -32.0, 32.0)   # u of the ribs under the case; the frame corners start at u 35.1
RIB_T = 4.0
FACE = -P.DEEP_W - 0.05                  # the face of the boss and the pad: as the face of the pod mount
GAP = 5.0                                # the air gap between the body and the back plate
PLATE_T = 3.0
PLATE_U = 20.0                           # the half width of the back plate: clear of the wells to the display screws
TOP = P.MOUNT_V[1]                       # the top of the back plate and the boss
BOSS = (P.MOUNT_U,)                      # the half width of the boss
CBORE_SKIN = 3.0                         # the boss under the screw head: 3 + 2 (the back) + 2.4 (the nut) = M3 x 8
BOSS_V0 = 84.0                           # the bottom of the boss at the back plate; it rises at 50 degrees to FACE
PAD = (14.0, 10.0, 26.0)                 # the pad on the back of the body: half width, v range
# The wells to the two lower display screws (resin case). They are outside the back plate, so a
# screwdriver for the M2.5 screws goes in from behind.
WELL = (3.0, 1.5)                        # the bore and the wall of a well
TIES = [(u, v) for u in (-5.0, 5.0) for v in (45.0, 75.0)]   # the twist-tie holes in the back plate
# The foot: a flat plate. The back is far enough back that a push on the screen slides the base on
# rubber feet before it tips (check()).
FOOT_T = 4.0
FOOT_BACK = 125.0
FOOT_R = 10.0
FOOT_RIM = 11.0
TOP_EDGE = 1.0                           # the chamfer of the top edge of the foot
COLORS = {"navy": "#1F2B4D", "orange": "#E8742A"}


def case_loc(h0=H0):
    return Location(Plane(origin=(0.0, 0.0, h0), x_dir=U, z_dir=W))


def to_world(v, w, h0=H0):
    """(y, z) of the case point (v, w)."""
    return v * SIN - w * COS, h0 + v * COS + w * SIN


def v_at(z, w, h0=H0):
    """The v of the case point at height z and depth w."""
    return (z - h0 - w * SIN) / COS


def teardrop(u, v, w0, w1, r):
    """A hole along w with a point at the top: its two upper sides rise at 50 degrees (pod.SLOPE), so
    the base prints standing with no support."""
    a = math.radians(50)
    d = r / math.cos(a)
    sk = Circle(r) + Polygon((0, d), (r * math.sin(a), r * math.cos(a)), (-r * math.sin(a), r * math.cos(a)), align=None)
    return Pos(u, v, (w0 + w1) / 2) * extrude(sk, amount=abs(w1 - w0) / 2, both=True)


def diamond(u, v, w0, w1, a):
    sk = Polygon((u - a, v), (u, v - a * P.SLOPE), (u + a, v), (u, v + a * P.SLOPE), align=None)
    return Pos(0, 0, (w0 + w1) / 2) * extrude(sk, amount=abs(w1 - w0) / 2, both=True)


# ---------------------------------------------------------------- the case
# The nut rings: on the inner floor of the body at pod.MOUNT_SCREWS, NUT_RING high. The Active Cooler is
# 2 mm over them. The USB 3 port starts at v 99.9, so the rings end at RING_V1. The nut presses into its
# pocket from the front.
NUT_RING = (5.0, 3.0)                    # radius, height
RING_V1 = 99.4
NUT_FIT = {"fdm": 0.05, "resin": 0.1}    # the pocket over the nut, on each flat: a press fit


def body_slate(process):
    """The body of the pod with the two nut rings."""
    s = P.body()
    floor = -P.DEEP_W + P.FLOOR
    r, h = NUT_RING
    for u, v in P.MOUNT_SCREWS:
        ring = Pos(u, v, floor + h / 2 - 0.01) * extrude(Circle(r), amount=h / 2 + 0.01, both=True)
        s = s + (ring - P.uvw_box(u - r - 1, u + r + 1, RING_V1, v + r + 1, floor - 1, floor + h + 1))
        af = P.NUT[0] + 2 * NUT_FIT[process]
        rr = af / math.sqrt(3)
        hexa = Polygon(*[(rr * math.cos(math.radians(60 * k)), rr * math.sin(math.radians(60 * k))) for k in range(6)],
                       align=None)
        s = s - Pos(u, v, floor + h - P.NUT[1] - 0.2) * extrude(hexa, amount=P.NUT[1] + 1.0)
        s = s - Pos(u, v, floor) * extrude(Circle(1.7), amount=h + 1, both=True)
    return s


def case_resin():
    """The resin case: the frame and the body of the pod as one part, with the wells to the lower
    display screws. The frame and the body touch at pod.SPLIT_W: a ring between them joins them."""
    keep = P.RESIN
    P.RESIN = True
    try:
        fr, bd = P.pod_frame(), body_slate("resin")
    finally:
        P.RESIN = keep
    B = P.rounded_box
    ring = B(-P.DEEP_U, P.DEEP_U, P.DEEP_V0, P.DEEP_V1, -P.SPLIT_W - 0.2, -P.SPLIT_W + 0.05, P.DEEP_R)
    ring = ring - B(-P.DEEP_U + P.WALL, P.DEEP_U - P.WALL, P.DEEP_V0 + P.WALL, P.DEEP_V1 - P.WALL, -P.SPLIT_W - 1,
                    -P.SPLIT_W + 1, P.DEEP_R - P.WALL) - P.ports_cut()
    s = fr + bd + ring
    for u, v in P.TABS[:2]:
        r, t = WELL
        tube = Pos(u, v, (-P.DEEP_W - P.SPLIT_W) / 2) * extrude(Circle(r + t), amount=(P.DEEP_W - P.SPLIT_W) / 2, both=True)
        s = s + tube
        s = s - Pos(u, v, (-P.DEEP_W - 1 - P.SPLIT_W) / 2) * extrude(Circle(r), amount=(P.DEEP_W + 1 - P.SPLIT_W) / 2 - 0.01,
                                                                    both=True)
    return s


def case_fdm():
    """The FDM case: the frame of the pod, and the body with the nut rings."""
    return P.pod_frame(), body_slate("fdm")


# ---------------------------------------------------------------- the base
def boss_slope():
    """The space under the boss: a plane through (v BOSS_V0, w at the back plate) that rises at 50
    degrees toward FACE. Below it there is no boss."""
    wp = FACE - GAP
    big = 1000.0
    n = (0.0, -1.0, P.SLOPE)                             # (u, v, w): the normal, away from the boss
    return Location(Plane(origin=(0, BOSS_V0, wp), z_dir=n)) * Pos(0, 0, big / 2) * Box(big, big, big)


def base(process):
    """The base in case coordinates, down past the desk (stand() cuts it at the desk)."""
    fit = FIT_BY[process]
    B = P.uvw_box
    wp = FACE - GAP                                      # the front face of the back plate
    wr = wp - PLATE_T
    v_low = v_at(0.0, LIP[0]) - 5.0                     # below the desk at the front, the lowest point
    front = P.LENS + fit                                 # the cheeks end behind the glass
    s = B(-PLATE_U, PLATE_U, v_low, TOP, wr, wp)                           # the back plate
    s = s + B(-SIDE, SIDE, v_low, -fit, CHIN_W, LIP[0])                    # the chin
    s = s + B(-SIDE, SIDE, v_low, LIP[1], fit, LIP[0])                     # the lip
    cheek_u = P.BODY_U + fit
    for sg in (-1, 1):
        s = s + B(sg * cheek_u, sg * SIDE, v_low, CHEEK_TOP, -P.SPLIT_W - 3.0, -front)
    for u in RIBS:
        s = s + B(u - RIB_T / 2, u + RIB_T / 2, v_low, P.DEEP_V0, wp, CHIN_W + 0.01)
    s = s + B(-PAD[0], PAD[0], v_low, PAD[2], wp - 0.5, FACE)             # the pad
    s = s + (B(-BOSS[0], BOSS[0], BOSS_V0, TOP, wp - 0.5, FACE) - boss_slope())
    # The screws: from behind, through the boss and the back of the body, into the nuts. The head is in
    # a counterbore, CBORE_SKIN from FACE.
    for u, v in P.MOUNT_SCREWS:
        s = s - teardrop(u, v, FACE + 0.5, wr - 1.0, 1.7)
        s = s - teardrop(u, v, FACE - CBORE_SKIN, wr - 1.0, P.S.CBORE[3][0] / 2 + 0.1)
    # The twist-tie holes.
    for u, v in TIES:
        s = s - diamond(u, v, wp + 1.0, wr - 1.0, 1.6)
    return s


def foot():
    wr = FACE - GAP - PLATE_T
    y0, _ = to_world(v_at(0.0, LIP[0]), LIP[0])
    y1 = FOOT_BACK
    plate = extrude(Pos(0, (y0 + y1) / 2, 0) * RectangleRounded(2 * SIDE, y1 - y0, FOOT_R), amount=FOOT_T)
    top = plate.edges().filter_by(lambda e: e.center().Z > FOOT_T - 0.01)
    plate = chamfer(top, TOP_EDGE)
    bed = plate.edges().filter_by(lambda e: e.center().Z < 0.01)
    plate = chamfer(bed, P.FOOT, angle=math.degrees(math.atan(P.SLOPE)), reference=plate.faces().sort_by().first)
    # Two windows: under the ribs (the ribs stand on the desk), and behind the back plate.
    ya, _ = to_world(v_at(FOOT_T, wr), wr)
    yc, _ = to_world(v_at(0.0, CHIN_W), CHIN_W)
    x = SIDE - FOOT_RIM
    for a, b in ((yc + 3.0, ya - 3.0), (ya + 6.0, y1 - FOOT_RIM)):
        plate = plate - extrude(Pos(0, (a + b) / 2, -1) * RectangleRounded(2 * x, b - a, min(FOOT_R - FOOT_RIM / 2, (b - a) / 2 - 0.1)),
                                amount=FOOT_T + 2)
    return plate


def stand(process="resin"):
    """The base in world coordinates."""
    c = (case_loc() * base(process)) & (Pos(0, 0, 250) * Box(500, 500, 500))
    return c + foot()


def case_parts(process="resin"):
    """The printed case parts and the glass, in case coordinates."""
    glass = [s for n, s, _ in P.hardware() if n == "glass"][0]
    if process == "resin":
        return {"case": case_resin(), "glass": glass}
    fr, bd = case_fdm()
    return {"frame": fr, "body": bd, "glass": glass}


# ---------------------------------------------------------------- the checks
def hardware_check(shell):
    """The overlaps of the resin case with the display, the Pi, the cables and the screws, with the
    pairs that pod.check allows for the body and the frame."""
    hw = [(n, s, c) for n, s, c in P.hardware() if n not in ("pod_frame", "pod_mount") and not n.startswith("screw_body")
          and not n.startswith("screw_mount") and not n.startswith("insert_")]
    bad = []
    for n, s, _ in hw:
        if frozenset(("pod_body", n)) in P.ALLOWED or frozenset(("pod_frame", n)) in P.ALLOWED:
            continue
        bb, cb = s.bounding_box(), shell.bounding_box()
        if bb.max.X < cb.min.X or bb.min.X > cb.max.X:
            continue
        v = (shell & s).volume
        if v > 0.5:
            bad.append((n, round(v, 1)))
    return bad


# The masses of the bought parts are estimates (no part was weighed): the display 120 g, the Pi 5
# 46 g, the Active Cooler 22 g. The screws, the inserts and the cables are out. The printed parts are
# solid: PLA 1.24 g/cm3, the resin 1.1 g/cm3 (profiles/peopoly-phenom/resin.json). FDM parts have
# infill: FDM_FILL of the solid mass (from a CuraEngine slice with the pla-0.20-standard profile).
DISPLAY_G, PI_G, COOLER_G = 120.0, 46.0, 22.0
DENSITY = {"fdm": 1.24, "resin": 1.1}
FDM_FILL = 0.79
MU = {"bare foot (PLA on wood)": 0.3, "rubber feet": 0.6}


def tip(stand_shape, parts, process):
    loc = case_loc()
    fill = FDM_FILL if process == "fdm" else 1.0
    ms = []
    for n, s in parts.items():
        if n == "glass":
            continue
        w = loc * s
        c = w.center()
        ms.append((n, w.volume / 1000 * DENSITY[process], (c.Y, c.Z)))
    ms.append(("display", DISPLAY_G, to_world(P.FRAME_V0 + P.FRAME[1] / 2, -P.FRAME_W / 2)))
    pc = P.pi_box(0, 85, 0, 56, 0, 1.6).center()
    ms.append(("pi5", PI_G, to_world(pc.Y, pc.Z)))
    cb = P.pi_box(*P.pi_parts()["cooler"]).center()
    ms.append(("cooler", COOLER_G, to_world(cb.Y, cb.Z)))
    c = stand_shape.center()
    ms.append(("base", stand_shape.volume / 1000 * DENSITY[process] * fill, (c.Y, c.Z)))
    m = sum(g for _, g, _ in ms)
    yc = sum(g * p[0] for _, g, p in ms) / m
    zc = sum(g * p[1] for _, g, p in ms) / m
    mg = m / 1000 * 9.81
    bb = stand_shape.bounding_box()
    yf, yr, xh = bb.min.Y, bb.max.Y, bb.max.X
    r = {"mass_g": m, "base_g": ms[-1][1], "com_y": yc, "com_z": zc, "front_y": yf, "back_y": yr,
         "tip_back_deg": math.degrees(math.atan2(yr - yc, zc)), "tip_front_deg": math.degrees(math.atan2(yc - yf, zc)),
         "tip_side_deg": math.degrees(math.atan2(xh, zc))}
    # A finger pushes the glass along -w, at a point on the middle line of the screen. The base tips
    # back on the back edge of the foot when the moment of the push is more than the moment of the
    # weight.
    aa0 = P.GLASS[1] / 2 - P.AA[1] / 2
    for name, v in (("top", aa0 + P.AA[1]), ("middle", P.GLASS[1] / 2)):
        y, z = to_world(v, 0.0)
        arm = COS * z - SIN * (yr - y)
        r[f"push_{name}_N"] = mg * (yr - yc) / arm if arm > 0 else math.inf
    for k, mu in MU.items():
        r[f"slide_{k}_N"] = mu * mg / (COS - mu * SIN)
    return r


# A beam estimate of the bend under a push of PUSH on the top of the screen, along -w. The base is a
# beam along v, fixed at the foot. The case is rigid. The moment at v is the push times (v_top - v),
# and the bend at the top of the screen is the integral of M (v_top - v) / (E I). E is an estimate for
# printed PLA and for ABS-like resin. The foot, the desk and the contacts are rigid here.
PUSH = 5.0
E_BY = {"fdm": 2500.0, "resin": 2000.0}
# The first v4 stand (a pod on a stand): a spine 26 mm wide, 6.05 mm thick at v 118.1 and 12 mm at
# v 1.3, where it met the foot. The pod hung on the dovetail only, at v 103 to 116.
OLD = (26.0, (118.1, 6.05), (1.3, 12.0), 110.0)


def second_moment(rects):
    """I about the middle axis along u, of the rectangles (width along u, w0, w1)."""
    a = [(b * abs(w1 - w0), (w0 + w1) / 2, b, abs(w1 - w0)) for b, w0, w1 in rects]
    area = sum(x[0] for x in a)
    wc = sum(x[0] * x[1] for x in a) / area
    return sum(b * h ** 3 / 12 + ar * (c - wc) ** 2 for ar, c, b, h in a)


def bend(I_of, v0, v1, v_push, E, n=400):
    dv = (v1 - v0) / n
    return sum(PUSH * (v_push - (v0 + (i + 0.5) * dv)) ** 2 / (E * I_of(v0 + (i + 0.5) * dv)) * dv for i in range(n))


def stiffness(process):
    fit = FIT_BY[process]
    E = E_BY[process]
    aa0 = P.GLASS[1] / 2 - P.AA[1] / 2
    v_push = aa0 + P.AA[1]
    b, (va, ta), (vb, tb), v_hold = OLD
    old = bend(lambda v: b * (ta + (tb - ta) * (va - v) / (va - vb)) ** 3 / 12, vb, v_hold, v_push, E)
    wp = FACE - GAP
    wr = wp - PLATE_T
    cheeks = [(SIDE - P.BODY_U - fit, -P.SPLIT_W - 3.0, -(P.LENS + fit))] * 2
    ribs = [(RIB_T, wp, CHIN_W)] * len(RIBS)

    def I_base(v):
        parts = [(2 * PLATE_U, wr, wp)]
        if v < CHEEK_TOP:
            parts += cheeks + ribs
        if v < PAD[2]:
            parts += [(2 * PAD[0], wp, FACE)]
        return second_moment(parts)
    v_foot = v_at(FOOT_T, wr)
    # If the pad did nothing: the back plate alone from the foot up to the screws.
    loose = bend(I_base, v_foot, P.MOUNT_SCREWS[0][1], v_push, E)
    # The screws pull the body onto the boss and the pad, so the case and the back plate bend as one
    # above the pad. The bend is that of the base from the foot to the top of the pad.
    held = bend(I_base, v_foot, PAD[2], v_push, E)
    return old, loose, held


def check():
    bad = 0
    shell = case_resin()
    hb = hardware_check(shell)
    print(f"resin case: {shell.volume / 1000:.1f} cm3; overlaps with the hardware: {hb if hb else 'none'}")
    bad += len(hb)
    for process in ("fdm", "resin"):
        s = stand(process)
        parts = case_parts(process)
        loc = case_loc()
        bb = s.bounding_box()
        print(f"{process}: base {s.volume / 1000:.1f} cm3, {bb.size.X:.1f} x {bb.size.Y:.1f} x {bb.size.Z:.1f} mm")
        for n, p in parts.items():
            v = (s & (loc * p)).volume
            print(f"  overlap of the base with the {n}: {v:.2f} mm3")
            bad += v > 0.01
        for n in ("power_plug", "printer_plug"):
            p = [sh for nn, sh, _ in P.hardware() if nn == n][0]
            v = (s & (loc * p)).volume
            print(f"  overlap of the base with the {n}: {v:.2f} mm3")
            bad += v > 0.01
        r = tip(s, parts, process)
        print(f"  mass {r['mass_g']:.0f} g (base {r['base_g']:.0f} g), center of mass y {r['com_y']:.1f} z "
              f"{r['com_z']:.1f}; foot y {r['front_y']:.1f} to {r['back_y']:.1f}")
        print(f"  tip angle: back {r['tip_back_deg']:.1f} deg, front {r['tip_front_deg']:.1f} deg, side "
              f"{r['tip_side_deg']:.1f} deg")
        print(f"  push to tip back: top of the screen {r['push_top_N']:.2f} N, middle {r['push_middle_N']:.2f} N")
        for k in MU:
            print(f"  push to slide, {k}: {r[f'slide_{k}_N']:.2f} N")
        old, loose, held = stiffness(process)
        print(f"  bend at the top of the screen, {PUSH:.0f} N push: first v4 spine {old:.3f} mm; back plate alone "
              f"{loose:.3f} mm; case held on the boss and the pad {held:.4f} mm")
    return bad


# ---------------------------------------------------------------- the print files
# FDM: the base prints on the bottom of its foot, as it stands on the desk. The frame and the body
# print as the pod parts: flat on their backs. Resin: the base turns RESIN_ROT, so the back plate is
# vertical and the bottom of the foot faces the build plate at 15 degrees, on supports RESIN_LIFT over
# the plate. The case turns as the resin body of the pod: the open front toward the plate.
RESIN_ROT = Rot(15, 0, 0)
CASE_ROT = Rot(0, 0, 90) * Rot(-195, 0, 0) * Rot(0, -15, 0)


def print_parts():
    fr, bd = case_fdm()
    return [("fdm", "slate-base_fdm.stl", P.on_bed(stand("fdm"), Rot(0, 0, 0))),
            ("fdm", "slate-frame_fdm.stl", P.on_bed(fr, Rot(0, 0, 0))),
            ("fdm", "slate-body_fdm.stl", P.on_bed(bd, Rot(0, 0, 0))),
            ("resin", "slate-base_resin.stl", P.on_bed(stand("resin"), RESIN_ROT, P.RESIN_LIFT)),
            ("resin", "slate-case_resin.stl", P.on_bed(case_resin(), CASE_ROT, P.RESIN_LIFT))]


def export():
    from build123d import export_stl
    for process, name, shape in print_parts():
        d = os.path.join(PRINT_DIR, process)
        os.makedirs(d, exist_ok=True)
        export_stl(shape, os.path.join(d, name), tolerance=0.01, angular_tolerance=0.1)
        bb = shape.bounding_box()
        print(f"{process}/{name}: {bb.size.X:.1f} x {bb.size.Y:.1f} x {bb.size.Z:.1f} mm")


def mesh_check():
    """Each file: one closed part. FDM files: the overhang check of pod.py (no face under 45 degrees,
    bridges up to 6 mm), and for the base also the stricter check of make.sh (no flat ceiling, no face
    under 40 degrees)."""
    import glob
    import trimesh
    ok = True
    for p in sorted(glob.glob(os.path.join(PRINT_DIR, "*", "*.stl"))):
        m = trimesh.load(p, force="mesh")
        closed = m.is_watertight and m.is_winding_consistent and m.body_count == 1
        line = f"  {os.path.relpath(p, HERE)}: {'closed, one part' if closed else 'NOT CLEAN'}"
        ok &= closed
        if "_fdm" in p:
            need, short, span, _ = P.overhang_report(m)
            line += f"; needs support {need:.1f} mm2, bridges {short:.1f} mm2 (longest {span:.1f} mm)"
            ok &= need <= 1.0
            if "base" in p:
                nz, zc, area = m.face_normals[:, 2], m.triangles_center[:, 2], m.area_faces
                above = zc > m.bounds[0][2] + 0.05
                flat = area[above & (nz < -0.999)].sum()
                steep = area[above & (nz < -math.cos(math.radians(40))) & (nz >= -0.999)].sum()
                line += f"; make.sh: flat ceilings {flat:.1f} mm2, faces under 40 deg {steep:.1f} mm2"
                ok &= flat <= 1.0 and steep <= 1.0
        print(line)
    return ok


# ---------------------------------------------------------------- the renders
def render():
    import tempfile
    import trimesh
    from build123d import export_stl
    import render as R
    loc = case_loc()
    navy, orange = COLORS["navy"], COLORS["orange"]
    color = {"glass": "#0D0E12", "screen": "#22324F", "power_plug": "#F2F2F2", "printer_plug": "#151515"}
    shown = [("base", stand("resin"), orange), ("case", loc * case_resin(), navy)]
    for n, sh, _ in P.hardware():
        if n in ("glass", "screen", "power_plug", "printer_plug") or n.startswith("pi_usb") or n == "pi_ethernet":
            shown.append((n, loc * sh, color.get(n, "#C9CDD2")))
    tmp = tempfile.mkdtemp()
    items = []
    for n, sh, c in shown:
        p = os.path.join(tmp, n + ".stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        items.append((trimesh.load(p, force="mesh"), c))
    out = os.path.join(HERE, "renders")
    # The render looks along view_dir: from the front left and above, from the back right, and from the
    # left.
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
