"""The deck pod: the Raspberry Pi Touch Display 2 (5 inch), with the Pi 5 and its Active Cooler on the
back of the display, in a printed case that fits close around them. A bracket for each printer holds
the pod: the TAZ Workhorse bracket is here.

The glass of the display is the front of the pod. It is wider than the case, so the case is a smaller
body behind the glass. The display hangs with its ribbon end down, and the Pi is on it as Raspberry
Pi mounts it: the USB ports up, the ribbon folded around the PCIe end of the Pi. The deck turns the
picture 180 degrees (Settings, or display_rotate=180 in printdeck.txt).

The body has two parts: a thin part around the display frame, and a deep part close around the Pi. The
ports are the Pi's own ports, flush with the walls of the deep part: the USB-C power port and the two
micro HDMI ports in the left wall, and the USB-A and Ethernet ports in the top wall. The printer
cable plugs straight into the Pi from above.

The case is three printed parts. Each prints flat on its back with no support.
- the frame: the thin part round the display, with a flat back ring over the metal back of the
  display and an opening for the Pi. Four M2.5 x 4 screws from the box of the display hold it: two
  through the back ring into the raised tabs, two down wells into the holes at the top.
- the body: the deep part round the Pi. Every opening in it is open to the front, or a hole in the
  back. Four M3 x 6 screws go from the back down posts at its corners into M3 heat-set inserts in the
  frame.
- the mount: a plate with the dovetail, held to the back of the body by two M3 x 6 cap screws from
  inside the body into M3 heat-set inserts in the mount.

The pod hangs on the bracket: the dovetail of the mount drops into a slot in the bracket from
above. The bracket stands on the two rail clips and the cover screw.

Pod coordinates: u across the glass to the right (seen from the front), v up from the bottom edge of
the glass, w out of the glass (the front of the glass is w = 0). The Pi is on the back: w < 0.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with pyrender --with PyOpenGL python pod.py [OUT_DIR | live | export]

export writes the STL files for printing to stl/pod/fdm/ and stl/pod/resin/ (see stl/pod/README.md).
The resin frame and mount have captive M3 hex nuts in place of the heat-set inserts.
"""

import math
import os
import sys

from build123d import (Axis, Box, Cone, Cylinder, Kind, Location, Plane, Polygon, Pos, Rectangle, RectangleRounded, Rot,
                       chamfer, extrude, fillet, offset)

import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))

# ---------------------------------------------------------------- the display (product brief, 5 inch)
# The display hangs with the end of its ribbon connector down, as in the drawing.
GLASS = (91.46, 143.4)          # the cover glass: across, up
GLASS_R = 5.0
LENS = 0.7                      # the thickness of the glass at its border
AA = (62.1, 110.4)              # the active area, in the middle of the glass
FRAME = (72.96, 122.74)         # the metal back of the module
FRAME_V0 = 12.8                 # the bottom of the frame over the bottom of the glass
FRAME_W = 8.5                   # the back of the frame behind the front of the glass
# The two raised tabs at the bottom (the ribbon end). At the top there are only two holes in the flat
# back (v 122.3), over the ports of the Pi. On the real display, the four holes are about 1.25 mm
# nearer to the ribbon end than in the drawing (19.85): a print with the holes 1.25 mm the other
# way was further off. They move HOLE_DV toward the ribbon end.
HOLE_DV = -1.25
TABS = [(u, 19.85 + HOLE_DV) for u in (-25.5, 25.5)]
HOLES = [(u, 19.85 + HOLE_DV + 103.7) for u in (-25.5, 25.5)]
TAB_W = 11.5                    # the face of a tab behind the front of the glass
TAB_SIZE = (8.0, 6.0)
PI_HOLE_UV = [(u, v) for u in (-24.5, 24.5) for v in (33.91, 33.91 + 58.0)]   # the Pi standoffs (49 x 58)
STANDOFF_W = 14.92              # the tips of the standoffs: the bottom of the Pi board

# ---------------------------------------------------------------- the case
WALL = 3.0                      # the side walls: 6 lines of a 0.5 mm nozzle
FLOOR = 2.0                     # the backs of the frame and the body, which print flat on the bed
GAP = 1.0                       # around the frame and the Pi
BODY_U = FRAME[0] / 2 + GAP + WALL                # half width: 39.48
BODY_V = (FRAME_V0 - GAP - WALL, FRAME_V0 + FRAME[1] + GAP + WALL)   # bottom and top: 9.8 to 138.5
BODY_R = 5.4                    # the inside corners are 3.4 mm: the corners of the display frame are square
FRONT_W = -(LENS + 0.05)        # the front rim of the body, against the back of the glass
PI_TOP_W = STANDOFF_W + 1.6 + 16.0                 # the top of the USB ports: 32.5
DEEP_W = PI_TOP_W + GAP + FLOOR                    # the back of the body where the Pi is: 35.5
# The deep part around the Pi: its walls are 0.6 mm outside the board, so the faces of the ports are
# flush with the outside of the walls (USB-C on the left, USB and Ethernet at the top). The thin
# part around the rest of the display frame is only as deep as the raised tabs.
THIN_W = TAB_W + 0.2 + FLOOR                       # the back of the thin part: 13.7
PI_V0 = PI_HOLE_UV[0][1] - 3.5                     # the v of the PCIe end of the Pi (STEP x = 0): 30.41
DEEP_U = 28.0 + 0.6 + WALL                         # the half width of the deep part: 30.6
DEEP_V1 = PI_V0 + 88.2                             # the top of the deep part: the faces of the ports (STEP x 88.1)
DEEP_V0 = None                                     # the bottom of the deep part: set with the ribbon below
DEEP_R = 3.5


def pi_uvw(x, y, z):
    """A point of the Pi (STEP coordinates: x along the 85 with the USB ports at +x, y along the 56 with
    the USB-C edge at y = 0, z up from the bottom of the board) on the back of the display: the USB
    end up, the USB-C edge to the left."""
    return (y - 28.0, PI_V0 + x, -STANDOFF_W - z)


def uvw_box(u0, u1, v0, v1, w0, w1):
    return Pos((u0 + u1) / 2, (v0 + v1) / 2, (w0 + w1) / 2) * Box(abs(u1 - u0), abs(v1 - v0), abs(w1 - w0))


def pi_box(x0, x1, y0, y1, z0, z1):
    a, b = pi_uvw(x0, y0, z0), pi_uvw(x1, y1, z1)
    return uvw_box(min(a[0], b[0]), max(a[0], b[0]), min(a[1], b[1]), max(a[1], b[1]), min(a[2], b[2]), max(a[2], b[2]))


def ribbon_route():
    """The route of the DSI ribbon in (v, w): the levels under the Pi and over the cooler, the v where
    the loop of the slack starts (past the PCIe end of the Pi), and the depth of the loop below it.
    The slack bows out in a loop: half an ellipse from the level under the Pi to the level over the
    cooler, as long as the 120 mm ribbon leaves for it."""
    under = -FRAME_W - 2.3                               # on the display connector, under the Pi
    over = -STANDOFF_W - 1.6 - 12.0 - 0.5                # over the cooler
    start = PI_V0 - 2.4
    b = pi_box(*PI_DSI).bounding_box()
    end = b.min.Y - 1.2                                  # where the ribbon goes down to the port
    plug = (b.min.Z + b.max.Z) / 2
    rest = RIBBON_IN + (DISP_FPC[1] - start) + (end - start) + (plug - over) + 2.2 + RIBBON_IN
    loop = RIBBON_L - rest
    a = (under - over) / 2
    half = lambda d: math.pi * (3 * (a + d) - math.sqrt((3 * a + d) * (a + 3 * d))) / 2
    lo, hi = 0.0, 200.0
    for _ in range(60):
        mid = (lo + hi) / 2
        lo, hi = (mid, hi) if half(mid) < loop else (lo, mid)
    return under, over, start, lo


def pi_parts():
    import deckview as V
    return V.pi_parts()


def rounded_box(u0, u1, v0, v1, w0, w1, rad):
    plan = Pos((u0 + u1) / 2, (v0 + v1) / 2, 0) * RectangleRounded(u1 - u0, v1 - v0, rad)
    return Pos(0, 0, w0) * extrude(plan, amount=w1 - w0)


# The split of the two parts: the frame is in front of SPLIT_W, the body behind it.
SPLIT_W = THIN_W
SLOPE = math.tan(math.radians(50))                 # the edges that print as overhangs rise 50 degrees
# The body holds to the frame with four M3 x 6 screws: from the back, down posts at the corners of
# the body (outside the Pi, behind the thin part of the frame), into M3 heat-set inserts in the frame.
POSTS = [(u, v) for u in (-34.0, 34.0) for v in (14.0, 110.0)]
POST_R, POST_WELL, POST_FLOOR = 5.2, 3.95, 2.0     # the well takes a screwdriver shaft of 7.35 mm
INSERT = (2.0, 4.0)                                # M3 heat-set insert, 4 long: hole radius, depth
INSERT_FLOOR = 1.0                                 # the plastic behind the hole of an insert
# Resin: heat-set inserts do not work in cured resin, so the resin frame and mount have captive M3
# hex nuts (5.5 mm across the flats, 2.4 mm thick), as in deckcase.py. print_parts() sets RESIN.
RESIN = False
NUT = (5.5, 2.4)
NUT_CLR = 0.15                                     # resin prints close to size
NUT_SKIN = 1.5                                     # resin: the frame boss between its back face and the nut
# The mount: a plate with the dovetail, on the back of the body. Two M3 x 6 cap screws from inside
# the body, over the cooler, go through the back into M3 heat-set inserts in the mount.
MOUNT_T = INSERT[1] + INSERT_FLOOR
MOUNT_U = 11.0
MOUNT_V = (90.0, DEEP_V1 - 0.5)
MOUNT_SCREWS = [(-6.0, 96.0), (6.0, 96.0)]
BACK_W = DEEP_W + MOUNT_T                          # the back of the mount plate
# The opening of the frame: the Pi, its port shells and the ribbon loop go through it.
OPEN_U = (-30.0, 28.8)
OPEN_V = (26.5, DEEP_V1 + 0.3)


BACK_CHAMFER = 1.7              # the back edge of the body: a chamfer 1.7 in from the walls, at SLOPE
BACK_ROUND = 3.0                # and a round from the chamfer to the walls


def body_outer():
    # The top corners are square: the ears sit on them. The body prints on its back, so a round on
    # the back edge faces the bed and needs support. The back edge is a chamfer that rises 50 degrees
    # (SLOPE), with a round above it.
    o = rounded_box(-DEEP_U, DEEP_U, DEEP_V0, DEEP_V1 + DEEP_R + 1.0, -DEEP_W, -SPLIT_W - 0.1, DEEP_R)
    o = o - uvw_box(-DEEP_U - 1, DEEP_U + 1, DEEP_V1, DEEP_V1 + DEEP_R + 2.0, -DEEP_W - 1, -SPLIT_W + 1)
    back = o.edges().filter_by(lambda e: e.center().Z < -DEEP_W + 0.5)
    o = chamfer(back, BACK_CHAMFER, angle=math.degrees(math.atan(SLOPE)), reference=o.faces().sort_by().first)
    top = o.edges().filter_by(lambda e: abs(e.center().Z - (-DEEP_W + BACK_CHAMFER * SLOPE)) < 0.05)
    return fillet(top, BACK_ROUND)


def port_span():
    """The opening for the three ports in the top wall: (u0, u1, v0, w0, w1)."""
    p = pi_parts()
    boxes = []
    for name, m in (("pi_ethernet", 1.5), ("pi_usb3", 2.0), ("pi_usb2", 2.0)):
        x0, x1, y0, y1, z0, z1 = p[name]
        boxes.append(pi_box(x0, x1, y0 - m, y1 + m, z0 - 1.0, z1 + 1.2).bounding_box())
    return (min(bb.min.X for bb in boxes), max(bb.max.X for bb in boxes), min(bb.min.Y for bb in boxes),
            min(bb.min.Z for bb in boxes), max(bb.max.Z for bb in boxes))


def ports_cut():
    """The openings of the Pi's ports, with room for the plug bodies."""
    p = pi_parts()
    cut = None
    # One opening for the three ports in the top wall: the walls between separate openings are
    # under 1 mm, a single line of a 0.5 mm nozzle.
    u0, u1, v0, w0, w1 = port_span()
    cut = uvw_box(u0, u1, v0 + 4, BODY_V[1] + 1, w0, w1)
    # The ports reach into the top wall. Through the wall the opening goes on to the front, so the Pi
    # goes into the body from the front.
    cut = cut + uvw_box(u0, u1, DEEP_V1 - WALL - 1.0, DEEP_V1 + 0.01, w0, -SPLIT_W + 1.0)
    # The USB-C port and the two micro HDMI ports in the left wall: one notch, open to the front. In
    # the STEP model of the Pi their shells stand 1.4 mm past the edge of the board, so the Pi goes in
    # from the front through the notch. Its upper edges rise 50 degrees toward the front: the body
    # prints standing, and they need no support. Room for the plug of the official supply (12 x 7).
    u, v, w = pi_uvw(11.2, -1.0, 1.6 + 1.6)
    h0, h1 = p["pi_hdmi0"], p["pi_hdmi1"]
    hb = pi_box(min(h0[0], h1[0]) - 0.5, max(h0[1], h1[1]) + 0.5, h0[2], h0[3], 0.0, max(h0[5], h1[5]) + 0.5).bounding_box()
    wc0 = w - 4.0                                        # the back of the USB-C opening
    wh0 = hb.min.Z                                       # the back of the HDMI opening
    front = -SPLIT_W + 1.0
    rise = SLOPE
    prof = [(wc0, v - 6.6), (front, v - 6.6), (front, hb.max.Y + (front - wh0) * rise), (wh0, hb.max.Y),
            (wh0, v + 6.6 + (wh0 - wc0) * rise), (wc0, v + 6.6)]
    notch = extrude(Plane.YZ * Polygon(*[(vv, ww) for ww, vv in prof], align=None), amount=BODY_U + 1 - 27.0,
                    dir=(-1, 0, 0))
    cut = cut + Pos(-27.0, 0, 0) * notch
    return cut


def vents():
    """Slots in the back over the fan of the cooler, and in the right wall beside the heatsink."""
    x0, x1, y0, y1, z0, z1 = pi_parts()["cooler"]
    b = pi_box(x0, x1, y0, y1, z0, z1).bounding_box()
    cut = None
    u = b.min.X + 6
    while u < b.max.X - 6:
        s = uvw_box(u - 1.2, u + 1.2, b.min.Y + 6, b.max.Y - 6, -DEEP_W - 1, -DEEP_W + FLOOR + 1)
        cut = s if cut is None else cut + s
        u += 5.0
    # The slots in the right wall run front to back. The body prints flat on its back, so the top of
    # each slot is a point with sides at 50 degrees, and it needs no support.
    w0, w1 = -DEEP_W + 6, -STANDOFF_W - 4
    v = b.min.Y + 4
    while v < b.max.Y - 4:
        prof = Polygon((v - 1.2, w0), (v + 1.2, w0), (v + 1.2, w1 - 1.2 * SLOPE), (v, w1), (v - 1.2, w1 - 1.2 * SLOPE),
                       align=None)
        pl = Plane(origin=(DEEP_U - WALL - 1, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
        cut = cut + extrude(pl * prof, amount=WALL + 2)
        v += 5.0
    return cut


# The dovetail on the back: root, tip, height, v range. The pod slides down onto the bracket by
# the length of the dovetail and a little more (DOVE_LIFT).
DOVE_LIFT = 16.0
DOVE_CLR = 0.35                 # the gap round the dovetail in the slot of the bracket, for FDM
DOVE = (10.0, 14.0, 3.0, (DEEP_V1 - 15.5, DEEP_V1 - 2.5))   # clear of the wedge when lifted


def dovetail(clr=0.0, v_ext=0.0):
    """The dovetail, or with clr the slot for it: the profile offset by clr on every side."""
    r, t, h, (va, vb) = DOVE
    # Along v: the profile is in (u, -w), on a plane across v.
    prof = Polygon((-r / 2, BACK_W - 0.5), (r / 2, BACK_W - 0.5), (t / 2, BACK_W + h), (-t / 2, BACK_W + h), align=None)
    if clr:
        prof = offset(prof, amount=clr, kind=Kind.INTERSECTION)
    pl = Plane(origin=(0, va - clr, 0), x_dir=(1, 0, 0), z_dir=(0, 1, 0))
    d = extrude(pl * prof, amount=vb - va + 2 * clr + v_ext)
    if not clr:
        # The lower end of the dovetail slopes at 55 degrees: printed standing, it needs no support.
        tri = Polygon((va - 0.01, -BACK_W + 0.5), (va - 0.01, -BACK_W - h - 1.0), (va + 1.5 * (h + 1.5), -BACK_W - h - 1.0),
                      align=None)
        d = d - extrude(Plane.YZ * tri, amount=t, both=True)
    return d


def body():
    """The body: the deep part round the Pi, open at the front, with the posts for the screws to the
    frame. It prints flat on its back: every opening is open to the front, or a hole in the back."""
    inner = rounded_box(-DEEP_U + WALL, DEEP_U - WALL, DEEP_V0 + WALL, DEEP_V1 - WALL, -DEEP_W + FLOOR, -SPLIT_W + 1.0,
                        DEEP_R - WALL)
    s = body_outer() - inner
    for u, v in POSTS:
        s = s + Pos(u, v, -(DEEP_W + SPLIT_W + 0.1) / 2) * Cylinder(POST_R, DEEP_W - SPLIT_W - 0.1)
    s = s - ports_cut()
    s = s - vents()
    for u, v in POSTS:
        f = SPLIT_W + 0.1 + POST_FLOOR                   # the floor of the well, its head side
        s = s - Pos(u, v, 0) * screw_well(-DEEP_W - 1.0, -f, POST_WELL, SCREW[3][0] / 2 + 0.15)
        s = s - Pos(u, v, -(SPLIT_W + f) / 2) * Cylinder(1.7, f - SPLIT_W + 1.0)
    # The holes for the screws of the mount.
    for u, v in MOUNT_SCREWS:
        s = s - Pos(u, v, -DEEP_W + FLOOR / 2) * Cylinder(1.7, FLOOR + 2.0)
    return s


def screw_well(w0, w1, r, seat):
    """A well for a screw, open at w0 (on the bed), with the seat of the head at w1. The parts print
    with the well open on the bed, so the roof of the well is a bridge. The well narrows at 50
    degrees (SLOPE) from r to the radius of the seat, just out of the head, so the bridge is only
    the flat seat: 2 x seat across. The head is taller than the cone, so the screwdriver goes in as before."""
    h = (r - seat) * SLOPE
    well = Pos(0, 0, (w0 + w1 - h) / 2) * Cylinder(r, w1 - h - w0)
    return well + Pos(0, 0, w1 - h / 2) * Cone(r, seat, h)


def insert_hole(w0, w1, w2):
    """The hole of a heat-set insert, open at w0, the insert to w1, and the part to w2. The parts
    print with the hole open on the bed. The end of the hole is a cone (SLOPE), through the floor
    behind the insert, so the hole has no roof."""
    sgn = 1.0 if w1 > w0 else -1.0
    f = abs(w2 - w1)
    top = INSERT[0] - f / SLOPE
    hole = Pos(0, 0, (w0 + w1) / 2) * Cylinder(INSERT[0], abs(w1 - w0))
    cone = Pos(0, 0, w1 + sgn * f / 2) * Rot(0 if sgn > 0 else 180, 0, 0) * Cone(INSERT[0], top, f)
    return hole + cone + Pos(0, 0, w2) * Cylinder(top, 1.0)


def nut_pocket(w0, w1, rot=0.0):
    """The pocket of a captive M3 nut, from w0 to w1 along w, with a corner of the hexagon at the angle
    rot (degrees from +u)."""
    af, t = NUT[0] + 2 * NUT_CLR, abs(w1 - w0)
    r = af / math.sqrt(3)
    pts = [(r * math.cos(math.radians(rot + 60 * k)), r * math.sin(math.radians(rot + 60 * k))) for k in range(6)]
    return Pos(0, 0, min(w0, w1)) * extrude(Polygon(*pts, align=None), amount=t)


def mount():
    """The mount: a flat plate on the back of the body with the dovetail, and the two inserts. In
    resin, each M3 screw goes into a captive nut. The nut slides in from the side edge of the plate,
    into a pocket 1.2 mm from the face on the body. The screw pulls the nut onto that wall of the
    pocket, so the plate is in the clamp."""
    v0, v1 = MOUNT_V
    s = rounded_box(-MOUNT_U, MOUNT_U, v0, v1, -BACK_W, -DEEP_W - 0.05, 3.0)
    s = s + dovetail()
    for u, v in MOUNT_SCREWS:
        if not RESIN:
            s = s - Pos(u, v, 0) * insert_hole(-DEEP_W - 0.04, -DEEP_W - 0.05 - INSERT[1], -BACK_W)
            continue
        w0 = -DEEP_W - 0.05 - 1.2
        w1 = w0 - NUT[1] - 2 * NUT_CLR
        side = 1.0 if u > 0 else -1.0
        s = s - Pos(u, v, (-DEEP_W - BACK_W) / 2) * Cylinder(1.7, MOUNT_T + 1.0)
        s = s - Pos(u, v, 0) * nut_pocket(w0, w1)
        # The slot from the pocket to the side edge, as wide as the nut across its flats.
        af = NUT[0] + 2 * NUT_CLR
        s = s - uvw_box(u, u + side * (MOUNT_U - abs(u) + 1.0), v - af / 2, v + af / 2, w1, w0)
    return s


# The holes for the M2.5 screws into the display: 3.3 mm across, 0.4 mm on each side of the screw.
# On the first print, one hole was 0.5 to 0.8 mm off the hole of the display, so the holes are 0.4 mm
# larger than before. The holes take an error of 0.4 mm in HOLE_DV.
DISPLAY_HOLE = 1.65
FOOT = 0.5                      # the chamfer on the bed edge of the frame, across


def pod_frame():
    """The frame: the thin part round the display, with the back ring over the metal back of the
    display. It goes down to the bottom of the body, and prints flat on its back ring."""
    v0, v1 = DEEP_V0, BODY_V[1]
    s = rounded_box(-BODY_U, BODY_U, v0, v1, -SPLIT_W, FRONT_W, BODY_R)
    # The outer edge on the bed: a small chamfer at SLOPE, so the first layer does not bulge out.
    bed = s.edges().filter_by(lambda e: e.center().Z < -SPLIT_W + 0.01)
    s = chamfer(bed, FOOT, angle=math.degrees(math.atan(SLOPE)), reference=s.faces().sort_by().first)
    s = s - rounded_box(-BODY_U + WALL, BODY_U - WALL, v0 + WALL, v1 - WALL, -SPLIT_W + FLOOR, FRONT_W + 1.0, BODY_R - WALL)
    # The opening for the Pi, and a notch under it for the loop of the ribbon.
    s = s - rounded_box(OPEN_U[0], OPEN_U[1], OPEN_V[0], OPEN_V[1], -SPLIT_W - 1.0, -SPLIT_W + FLOOR + 0.5, 1.5)
    s = s - uvw_box(RIBBON_LOOP_U[0] - 0.6, RIBBON_LOOP_U[1] + 0.6, v0 + WALL - 0.01, OPEN_V[0] + 1.0, -SPLIT_W - 1.0,
                    -SPLIT_W + FLOOR + 0.5)
    # The display screws: through the back ring into the raised tabs, and down wells with a 2 mm
    # floor into the holes at the top.
    for u, v in TABS:
        s = s - Pos(u, v, -SPLIT_W + FLOOR / 2) * Cylinder(DISPLAY_HOLE, FLOOR + 2.0)
    for u, v in HOLES:
        f0, f1 = FRAME_W + 0.2, FRAME_W + 2.2
        s = s + Pos(u, v, -(f0 + SPLIT_W) / 2) * Cylinder(5.0, SPLIT_W - f0)    # from the bed: no ledge over the opening
        s = s - Pos(u, v, 0) * screw_well(-SPLIT_W - 1.0, -f1, 3.95, SCREW[2.5][0] / 2 + 0.15)
        s = s - Pos(u, v, -(f0 + f1) / 2) * Cylinder(DISPLAY_HOLE, f1 - f0 + 1.0)
    # The bosses with the inserts for the screws of the body. In resin, each boss has a captive M3
    # nut in a pocket open toward the display: the nut goes in before the display, and the display
    # holds it in. The M3 x 6 screw ends at the far face of the nut.
    for u, v in POSTS:
        if not RESIN:
            boss = INSERT[1] + INSERT_FLOOR
            s = s + Pos(u, v, -SPLIT_W + boss / 2) * Cylinder(4.0, boss)
            s = s - Pos(u, v, 0) * insert_hole(-SPLIT_W - 0.01, -SPLIT_W + INSERT[1], -SPLIT_W + boss)
            continue
        boss = NUT_SKIN + NUT[1] + 2 * NUT_CLR
        s = s + Pos(u, v, -SPLIT_W + boss / 2) * Cylinder(4.5, boss)
        s = s - Pos(u, v, -SPLIT_W) * Cylinder(1.7, 2 * boss + 2.0)
        s = s - Pos(u, v, 0) * nut_pocket(-SPLIT_W + NUT_SKIN, -SPLIT_W + boss + 0.01, 90.0)
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
POD_Z = (-102.6 - 2.0) / 2               # the middle of the front of the control box (cb_chassis)
# The top back corner of the deep part (the Pi) is in front of the plate by PLATE_GAP. The slide onto
# the dovetail runs up the tilted face, so it moves the pod toward the wall by DOVE_LIFT x sin(tilt):
# the gap is that and 3 mm more. The bracket is a wedge between the plate and the thin back over it.
PLATE_GAP = DOVE_LIFT * SIN + 3.0
_deep = (MOUNT_V[1], -BACK_W)
POD_O = (X_BACK + PLATE + PLATE_GAP + _deep[0] * SIN - _deep[1] * COS, GLASS_TOP_Y - GLASS[1] * COS, POD_Z)
POD_LOC = Location(Plane(origin=POD_O, x_dir=U, z_dir=W))


def to_mount(u, v, w):
    return tuple(POD_O[i] + u * U[i] + v * V[i] + w * W[i] for i in range(3))


def mbox(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(abs(x1 - x0), abs(y1 - y0), abs(z1 - z0))


COVER_SCREW = (S.X_FRONT - 6.1, 240.0 - S.Y_BASE)
CLIP_Y = (165.0, 207.0)


def plate_y(v):
    """The height (mount y) where the plane v of the pod meets the front of the bracket plate."""
    w = (X_BACK + PLATE - POD_O[0] + v * SIN) / COS
    return POD_O[1] + v * COS + w * SIN


# The bar goes down under the wedge, so the whole back of the wedge is on the plate.
BAR_Y = (plate_y(DOVE[3][0] - 3.0 - DOVE_CLR) - 1.0, 240.0)


NUB_GAP = 0.35                  # from the cover to the inner face of the nub
CLIP_CLR = 0.35                 # the gap on each side of the key of a rail clip in its slot
CLIP_TIP = 0.2                  # the gap at the tip of the key: 0.8 mm of plate stays in front of the slot


def bracket(with_wedge=True):
    """The TAZ bracket, in mount coordinates: a leg on the rail with the two clips, a bar across the top
    to the cover screw, and the wedge with the slot for the dovetail of the pod. The wedge is a separate
    part (wedge_part), screwed to the plate; with_wedge adds it for the checks and the window."""
    zc = S.RAIL_SLOT_Z
    leg = mbox(X_BACK, X_BACK + PLATE, CLIP_Y[0] - 14, BAR_Y[1], zc - 10, zc + 10)
    bar = mbox(X_BACK, X_BACK + PLATE, BAR_Y[0], BAR_Y[1], zc - 10, NUB_GAP + 3.3)
    b = leg + bar
    # The nub on the cover screw: a tab along the cover, its slot open at the bottom. Its inner face is
    # NUB_GAP off the cover, so the bracket goes over the edge of the case without force. The cover
    # screw pulls the nub onto the cover.
    x_s, y_s = COVER_SCREW
    nub = mbox(x_s - 9.0, X_BACK + 0.01, BAR_Y[0], BAR_Y[1], NUB_GAP, NUB_GAP + 3.3)
    b = b + nub
    b = b - mbox(x_s - 3.5, x_s + 3.5, BAR_Y[0] - 1, y_s, -1, 4.3) - Pos(x_s, y_s, 1.65) * Cylinder(1.8, 6)
    # The dovetail slots of the clips (the same as the deck case).
    # The slots are CLIP_CLR wider than the key on each side, and CLIP_TIP deeper than the key.
    kd = S.BACK_X[1] - S.BACK_X[0]
    k0, k1 = S.CLIP_KEY[0] / 2 + CLIP_CLR, S.CLIP_KEY[1] / 2 + CLIP_CLR
    for yc in CLIP_Y:
        d0, d1 = yc - S.CLIP_L / 2 - S.CLIP_SLACK, yc + S.CLIP_L / 2 + S.CLIP_SLACK
        dov = Polygon((zc - k0, X_BACK - 0.01), (zc + k0, X_BACK - 0.01), (zc + k1, X_BACK + kd),
                      (zc + k1, X_BACK + kd + CLIP_TIP), (zc - k1, X_BACK + kd + CLIP_TIP), (zc - k1, X_BACK + kd),
                      align=None)
        b = b - Location(Plane(origin=(0, d0, 0), x_dir=(0, 0, 1), z_dir=(0, 1, 0))) * extrude(Plane.XY * dov, amount=d1 - d0)
        # The window where the clip drops in. The bracket prints standing on its top, so the lower end
        # of the window closes at 45 degrees to a point, and it is not a flat roof.
        ya, hw = d0 - S.CLIP_DROP, k1 + 0.2
        win = Polygon((ya - k0, zc), (ya, zc + k0), (ya + hw - k0, zc + hw), (d0 + 0.5, zc + hw), (d0 + 0.5, zc - hw),
                      (ya + hw - k0, zc - hw), (ya, zc - k0), align=None)
        pl = Plane(origin=(X_BACK - 1, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
        b = b - extrude(pl * win, amount=kd + 1.0 + CLIP_TIP, dir=(1, 0, 0))
    b = b - wedge_groove("taz", BAR_Y[1])
    if with_wedge:
        b = b + wedge("taz")
    return b


def tie_anchor(x0, y0, z0, axis):
    """A zip-tie anchor: a bridge on a plate, with a 4 x 2 mm passage under it for a tie up to
    3.6 mm wide. axis is the direction of the passage ("y" or "z"); the bridge stands out of the plate
    toward +x (TAZ) or -y (Mini 2): give the plate face as x0 or y0."""
    if axis == "taz":
        b = mbox(x0, x0 + 5.0, y0 - 6.0, y0 + 6.0, z0 - 4.0, z0 + 4.0)
        return b, mbox(x0 + 0.01, x0 + 2.4, y0 - 7.0, y0 + 7.0, z0 - 2.2, z0 + 2.2)
    # The Mini 2 bracket prints on the back of the plate, so the top of the passage is a point with
    # sides at 45 degrees.
    b = mbox(x0 - 6.0, x0 + 6.0, y0 - 5.0, y0, z0 - 4.0, z0 + 4.0)
    prof = Polygon((y0 - 0.01, z0 - 2.2), (y0 - 0.01, z0 + 2.2), (y0 - 1.4, z0 + 2.2), (y0 - 3.6, z0), (y0 - 1.4, z0 - 2.2),
                   align=None)
    pl = Plane(origin=(x0 - 7.0, 0, 0), x_dir=(0, 1, 0), z_dir=(1, 0, 0))
    return b, extrude(pl * prof, amount=14.0, dir=(1, 0, 0))


# The wedge is a separate part. A dovetail key on its back slides down into a dovetail groove in the
# plate of the bracket, like the rail clips. The groove is open at the top and stops at the bottom, so
# the weight of the pod holds the wedge down. On both printers the pod has the same tilt and gap, so
# the wedge is the same part.
KEY = (10.0, 12.5, 2.0)                  # the key: root width, tip width, depth into the plate (1.65 mm of plate stays behind the groove)
TAZ_N, TAZ_UP, TAZ_ACROSS = (1.0, 0.0, 0.0), (0.0, 1.0, 0.0), (0.0, 0.0, 1.0)
MINI_N, MINI_UP, MINI_ACROSS = (0.0, -1.0, 0.0), (0.0, 0.0, 1.0), (1.0, 0.0, 0.0)
TAZ_FRONT = mbox(X_BACK + PLATE - 0.01, X_BACK + 400, -1000, 1000, -1000, 1000)


def wedge_frame(name):
    """(loc, n, up, across, a point on the front of the plate, the space in front of the plate)."""
    if name == "taz":
        return POD_LOC, TAZ_N, TAZ_UP, TAZ_ACROSS, (X_BACK + PLATE, 0.0, POD_Z), TAZ_FRONT
    fy = MINI_PLATE_Y - PLATE
    return MINI_LOC, MINI_N, MINI_UP, MINI_ACROSS, (MINI_X, fy, 0.0), mbox(-1000, 1000, -1000, fy + 0.01, -1000, 1000)


def wedge_bottom(name):
    """The height (along up) of the level bottom of the wedge: the lowest point of the stop under the
    slot of the dovetail."""
    import numpy as np
    loc, n, up, across, p0, _ = wedge_frame(name)
    v = DOVE[3][0] - 3.0 - DOVE_CLR
    pts = [np.array(to_frame(loc, u, v, w)) for u in (-13.0, 13.0) for w in np.linspace(-BACK_W - 0.3, -BACK_W - 30, 60)]
    pts = [q for q in pts if np.dot(q - np.array(p0), n) >= 0]
    return min(float(np.dot(q, up)) for q in pts)


def key_prism(name, clr, v0, v1):
    """The dovetail of the key (clr 0) or of the groove (clr > 0): its root on the front of the plate,
    widening into the plate, along up from v0 to v1."""
    import numpy as np
    loc, n, up, across, p0, _ = wedge_frame(name)
    r_, t, d = KEY
    o = np.array(p0, float)
    o = o - np.dot(o, up) * np.array(up) + v0 * np.array(up)
    pl = Plane(origin=tuple(o), x_dir=across, z_dir=up)
    sgn = -1.0 if np.dot(np.array([pl.y_dir.X, pl.y_dir.Y, pl.y_dir.Z]), n) > 0 else 1.0
    prof = Polygon((-r_ / 2, -0.5 * sgn), (r_ / 2, -0.5 * sgn), (t / 2, d * sgn), (-t / 2, d * sgn), align=None)
    if clr:
        prof = offset(prof, amount=clr, kind=Kind.INTERSECTION)
    return extrude(pl * prof, amount=v1 - v0, dir=up)


def key_floor(name, drop=0.0):
    """The space over the lower end of the key: a plane that rises into the plate at 50 degrees
    (SLOPE), from the root of the key on the level bottom of the wedge. With drop, the plane is lower
    by drop (the end of the groove). The TAZ bracket prints standing on its top, so the end of the
    groove is a slope and not a flat roof."""
    import numpy as np
    loc, n, up, across, p0, _ = wedge_frame(name)
    n, up = np.array(n), np.array(up)
    o = np.array(p0, float)
    o = o - np.dot(o, up) * up + (wedge_bottom(name) + 0.5 * SLOPE - drop) * up
    big = 1000.0
    return Location(Plane(origin=tuple(o), z_dir=tuple(up + SLOPE * n))) * Pos(0, 0, big / 2) * Box(big, big, big)


def wedge(name):
    """The wedge of a bracket: from the plate to the back of the pod, behind the dovetail of the mount,
    with the slot of that dovetail open at the top, a level bottom, and the key on its back."""
    import numpy as np
    loc, n, up, across, p0, front = wedge_frame(name)
    va = DOVE[3][0]
    w = (loc * uvw_box(-13, 13, va - 30.0, MOUNT_V[1], -BACK_W - 80, -BACK_W - 0.3)) & front
    yb = wedge_bottom(name)
    big = 1000.0
    above = Location(Plane(origin=tuple(np.array(up) * (yb + big / 2)), z_dir=up)) * Box(2 * big, 2 * big, big)
    w = (w & above) - loc * dovetail(DOVE_CLR, 40.0)
    top = max(float(np.dot(np.array([vv.X, vv.Y, vv.Z]), up)) for vv in w.vertices())
    return w + (key_prism(name, 0.0, yb, top) & key_floor(name))


def wedge_groove(name, top):
    """The groove for the key in the plate: from the bottom of the wedge to above the top of the
    plate."""
    yb = wedge_bottom(name)
    return key_prism(name, DOVE_CLR, yb - DOVE_CLR - 3.0, top + 1.0) & key_floor(name, DOVE_CLR * math.hypot(1, SLOPE))


def wedge_part():
    """The wedge as printed (TAZ mount coordinates): it stands on its level bottom."""
    return wedge("taz")


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
# The pod is left of the left upright, over the electronics case. The bed moves forward past the front
# of the frame between x -300 and -100, and the pod stays clear of it.
MINI_X = -380.0
MINI_TIE_X = -330.0                       # the zip-tie anchors, right of the pod on the upright
MINI_SCREWS = [(-334.1, 364.0), (-358.1, 352.0)]   # the frame screws in the left upright (x, z)
_front_y = MINI_PLATE_Y - PLATE - PLATE_GAP
MINI_O = (MINI_X, _front_y - (_deep[0] * MINI_V[1] + _deep[1] * MINI_W[1]), MINI_GLASS_TOP_Z - GLASS[1] * COS)
MINI_LOC = frame(MINI_U, MINI_V, MINI_W, MINI_O)


def mini_bracket(with_wedge=True):
    """The Mini 2 bracket: a plate on the front of the left upright, held by the two frame screws of
    its top corner (longer screws in the same holes), and the wedge with the dovetail slot (a separate
    part, screwed to the plate; with_wedge adds it)."""
    top = to_frame(MINI_LOC, 0, DEEP_V1, -DEEP_W)[2]
    plate = mbox(MINI_X - 15.0, MINI_TIE_X + 8.0, MINI_PLATE_Y - PLATE, MINI_PLATE_Y, top - 60.0, 372.0)
    for x, z in MINI_SCREWS:
        plate = plate - Pos(x, MINI_PLATE_Y - PLATE / 2, z) * Rot(90, 0, 0) * Cylinder(1.7, PLATE + 2)
    # Zip-tie anchors on the plate, right of the pod: for the printer cable and its slack.
    for z in (top - 54.0, top - 36.0):
        add, cut = tie_anchor(MINI_TIE_X, MINI_PLATE_Y - PLATE + 0.01, z, "mini")
        plate = plate + add - cut
    plate = plate - wedge_groove("mini2", 372.0)
    if with_wedge:
        plate = plate + wedge("mini2")
    return plate


def to_frame(loc, u, v, w):
    p = loc * Pos(u, v, w)
    return tuple(p.position)


PARTS = [("pod_body", body, "#1B1C20"), ("bracket", bracket, "#2A2B30"), ("mini_bracket", mini_bracket, "#2A2B30")]


# ---------------------------------------------------------------- the bought parts, for the check and the renders
SCREW = {2.5: (4.5, 2.0), 3: (5.5, 3.0)}   # size: head diameter, head height (cap or pan head)
SCREW_COLOR = "#B9BEC6"
INSERT_COLOR = "#C9A227"                  # brass


def screw(size, length, origin, out_dir, frame=None):
    """A screw: the head on origin, outside toward out_dir, the shank of length into the part (against
    out_dir). frame is a Location to place the result, or None."""
    hd, hh = SCREW[size]
    s = Pos(0, 0, hh / 2) * Cylinder(hd / 2, hh) + Pos(0, 0, -length / 2) * Cylinder(size / 2 - 0.05, length)
    s = s - Pos(0, 0, hh) * Cylinder(hd * 0.22, hh * 1.2)          # the socket, for the looks
    s = Location(Plane(origin=origin, z_dir=out_dir)) * s
    return frame * s if frame is not None else s


def pod_screws():
    """The screws of the pod, in pod coordinates:
    - the frame: four M2.5 x 4 into the display (through the back ring into the raised tabs, and
      down the wells into the top holes), from the box of the display;
    - the body: four M3 x 6 down the posts into the inserts of the frame;
    - the mount: two M3 x 6 from inside the body, through its back into the inserts of the mount;
    - the Pi: four M2.5 x 4 through the Pi into the standoffs, from the box of the display."""
    def group(items):
        out = None
        for sc in items:
            out = sc if out is None else out + sc
        return out
    fr = group([screw(2.5, 4.0, (u, v, -SPLIT_W), (0, 0, -1)) for u, v in TABS]
               + [screw(2.5, 4.0, (u, v, -FRAME_W - 2.2), (0, 0, -1)) for u, v in HOLES])
    bd = group([screw(3, 6.0, (u, v, -SPLIT_W - 0.1 - POST_FLOOR), (0, 0, -1)) for u, v in POSTS])
    mt = group([screw(3, 6.0, (u, v, -DEEP_W + FLOOR), (0, 0, 1)) for u, v in MOUNT_SCREWS])
    pi = group([screw(2.5, 4.0, (u, v, -STANDOFF_W - 1.6), (0, 0, -1)) for u, v in PI_HOLE_UV])
    # The heat-set inserts: their top is flush with the face of the part.
    ins_f = group([Pos(u, v, -SPLIT_W + INSERT[1] / 2) * insert() for u, v in POSTS])
    ins_m = group([Pos(u, v, -DEEP_W - 0.05 - INSERT[1] / 2) * insert() for u, v in MOUNT_SCREWS])
    return [("screw_frame", fr, SCREW_COLOR), ("screw_body", bd, SCREW_COLOR), ("screw_mount", mt, SCREW_COLOR),
            ("screw_pi", pi, SCREW_COLOR), ("insert_frame", ins_f, INSERT_COLOR), ("insert_mount", ins_m, INSERT_COLOR)]


def insert():
    """An M3 heat-set insert, 4 long: the knurl melts 0.3 mm into the wall of its hole."""
    return Cylinder(INSERT[0] + 0.3, INSERT[1]) - Cylinder(1.5, INSERT[1] + 1.0)


# The details of the display back, from the 5 inch drawing and the photos of the Touch Display 2
# (Raspberry Pi publishes no STEP model of it): the channel in the middle of the metal back, the
# connector of the ribbon at the bottom of the channel, and the power connector beside it. The photo
# of the parts (2026-10-24) agrees: the power connector is on the GPIO side of the Pi.
CHANNEL = (32.6, FRAME_V0 + FRAME[1] - 3.81)      # the width, and the top of the channel
DISP_FPC = (0.0, 36.9)                            # (u, v) of the ribbon connector
DISP_POWER = (21.9, 51.1)                         # (u, v) of the power connector (2 pins)
# The Pi: the display port that the ribbon uses in the photo (J4, x 47 to 49.8; its opening faces the
# PCIe end), and the GPIO header.
PI_DSI = (47.0, 49.8, 1.0, 16.6, 1.6, 3.6)
PI_GPIO = (7.1, 57.9, 49.4, 54.4, 1.6, 10.1)
PI_5V_PINS = (10.0, 52.8)                         # (x, y) of pins 2, 4 and 6 (5 V, 5 V, ground)
RIBBON_W = 11.5                                   # the 22-pin end of the standard-mini cable
RIBBON_T = 0.3
RIBBON_L = 120.0                                  # measured on the cable in the box
RIBBON_IN = 2.0                                   # the end of the ribbon in each connector
RIBBON_LOOP_U = (-8.0, 8.0)                       # the loop of the slack, in the middle under the connector, as on the deck
# The deep part goes down past the loop of the ribbon.
_r = ribbon_route()
DEEP_V0 = _r[2] - _r[3] - 0.8 - WALL


def display_details():
    out = []
    ch = uvw_box(-CHANNEL[0] / 2, CHANNEL[0] / 2, DISP_FPC[1] - 6.0, CHANNEL[1], -FRAME_W, -FRAME_W + 1.2)
    out.append(("channel", ch, "#6C7078"))
    u, v = DISP_FPC
    out.append(("display_fpc", uvw_box(u - 9.0, u + 9.0, v - 2.0, v + 2.0, -FRAME_W - 2.0, -FRAME_W), "#ECECEC"))
    u, v = DISP_POWER
    out.append(("display_power", uvw_box(u - 2.5, u + 2.5, v - 3.0, v + 3.0, -FRAME_W - 4.5, -FRAME_W), "#ECECEC"))
    out.append(("pi_dsi", pi_box(*PI_DSI), "#ECECEC"))
    out.append(("pi_gpio", pi_box(*PI_GPIO), "#1A1A1A"))
    return out


def ribbon_and_wire():
    """The DSI ribbon (the standard-mini cable in the box of the display) and the 2-wire power cable,
    from the photo of the parts. The ribbon leaves the display connector downward under the Pi, folds
    back past the PCIe end of the Pi, and goes up over the Active Cooler to the display port. The
    slack of the 120 mm ribbon bows out in a loop past the PCIe end (ribbon_route)."""
    import numpy as np
    rw, t = RIBBON_W, RIBBON_T
    b = pi_box(*PI_DSI).bounding_box()
    uc = (b.min.X + b.max.X) / 2
    u0, u1 = uc - rw / 2, uc + rw / 2
    under, over, start, depth = ribbon_route()
    fold = start
    plug = (b.min.Z + b.max.Z) / 2                       # in the display port
    end = b.min.Y - 1.2
    lu0, lu1 = RIBBON_LOOP_U
    segs = [
        uvw_box(-8.0, 8.0, start, DISP_FPC[1], under - t, under),          # out of the connector, down
        uvw_box(min(lu0, -8.0), max(lu1, 8.0), start - t, start + 1.0, under - t, under),
        uvw_box(u0, u1, start, end, over - t, over),                       # up over the cooler
        uvw_box(min(lu0, u0), max(lu1, u1), start - t, start + 1.0, over - t, over),
        uvw_box(u0, u1, end - t, end, over - t, plug),                     # down to the level of the port
        uvw_box(u0, u1, end - t, b.min.Y + 1.0, plug - t, plug),           # into the port
    ]
    # The loop: half an ellipse below start, from under the Pi to over the cooler.
    cw, aw = (under + over) / 2, (under - over) / 2
    pts = [(start - depth * math.sin(th), cw + aw * math.cos(th)) for th in np.linspace(0, math.pi, 25)]
    for (va, wa), (vb, wb) in zip(pts[:-1], pts[1:]):
        L = math.hypot(vb - va, wb - wa)
        d = (0.0, (vb - va) / L, (wb - wa) / L)
        segs.append(Location(Plane(origin=((lu0 + lu1) / 2, (va + vb) / 2, (wa + wb) / 2), x_dir=(1, 0, 0), z_dir=d))
                    * Box(lu1 - lu0, t, L + 0.3))
    r = segs[0]
    for sg in segs[1:]:
        r = r + sg
    # The power wire: from the display connector down under the Pi, around the PCIe end beside the
    # ribbon, and back up over the GPIO header to the 5 V pins.
    du, dv = DISP_POWER
    pu, pv, pw = pi_uvw(PI_5V_PINS[0], PI_5V_PINS[1], PI_GPIO[5])
    turn = PI_V0 - 1.8
    pts = [(du, dv, -FRAME_W - 4.5), (du, dv, -12.2), (18.5, dv - 8.0, -12.2), (18.5, turn, -12.2),
           (18.5, turn, pw - 3.0), (pu, turn, pw - 3.0), (pu, pv, pw - 3.0), (pu, pv, pw)]
    path = rounded(pts, r=4.0)
    wire = None
    for a, c in zip(path[:-1], path[1:]):
        L = float(np.linalg.norm(c - a))
        if L < 0.05:
            continue
        sg = Location(Plane(origin=tuple((a + c) / 2), z_dir=tuple((c - a) / L))) * Cylinder(1.2, L + 0.4)
        wire = sg if wire is None else wire + sg
    return [("dsi_ribbon", r, "#D88A2E"), ("power_wire", wire, "#C0392B")]


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
        sh = pi_box(*b)
        if n == "cooler":
            # The Active Cooler leaves the display ports at the edge of the board open.
            sh = sh - pi_box(PI_DSI[0] - 9.0, PI_DSI[1] + 2.0, -1.0, PI_DSI[3] + 1.5, 0.0, 20.0)
        out.append((n, sh, "#30343A" if n == "cooler" else ("#1F6B45" if n == "pi_board" else "#C9CDD2")))
    # The plugs: the power plug from the left, the printer cable into a USB 2 port from above.
    u, v, w = pi_uvw(11.2, -1.0, 3.2)
    out.append(("power_plug", usbc_plug(u - 6.5, v, w), "#F2F2F2"))
    x0, x1, y0, y1, z0, z1 = pi_parts()["pi_usb2"]
    b = pi_box(x0, x1, y0, y1, z0, z0 + 7.5).bounding_box()
    out += pod_screws()
    out.append(("pod_frame", pod_frame(), "#2B2E35"))
    out.append(("pod_mount", mount(), "#2A2B30"))
    out += display_details()
    out += ribbon_and_wire()
    # The USB-A plug goes down into the port: +x of the plug is -v, its width is across u.
    face = Pos((b.min.X + b.max.X) / 2, b.max.Y, (b.min.Z + b.max.Z) / 2) * Rot(0, 0, -90)
    out.append(("printer_plug", face * cable_plug(*USBA_PLUG), "#151515"))
    return out


def cable_plug(metal, body, relief, r=2.0):
    """The end of a cable: the metal plug, the overmold with round edges, and the strain relief.
    Each is (length, width, height). The plug points to +x with the face of the overmold at x = 0, and
    the cable leaves to -x at the end of the strain relief."""
    (ml, mw, mh), (bl, bw, bh), (rl, rw, rh) = metal, body, relief
    m = Pos(ml / 2, 0, 0) * Box(ml, mw, mh)
    b = fillet((Pos(-bl / 2, 0, 0) * Box(bl, bw, bh)).edges().filter_by(Axis.X), r)
    t = fillet((Pos(-bl - rl / 2 + 0.005, 0, 0) * Box(rl + 0.01, rw, rh)).edges().filter_by(Axis.X), min(rw, rh) / 2 - 0.3)
    return m + b + t


# The USB-C end of the cable of the Raspberry Pi 27W power supply, from its product brief
# (ref/raspberrypi/27w-usb-c-power-supply-product-brief.pdf): the metal plug 6.7 long, the overmold
# 19 x 12 x 7, and the strain relief. The overmold and the strain relief are 24 long.
USBC_PLUG = ((6.7, 8.25, 2.4), (19.0, 12.0, 7.0), (5.0, 5.3, 4.8))
# The USB 2.0 A and B ends of the printer cable. The metal plugs have the sizes of the USB 2.0
# standard. The overmolds have the sizes of a common cable, and were not measured. Each end is
# 30 long out of its port.
USBA_PLUG = ((12.0, 12.0, 4.5), (22.0, 16.0, 8.0), (8.0, 6.0, 6.0))
USBB_PLUG = ((9.0, 12.0, 10.9), (20.0, 16.0, 15.0), (8.0, 7.0, 7.0))


def usbc_plug(u0, v, w):
    """The power plug in the USB-C port. u0 is the face of the overmold. The plug points to +u."""
    return Pos(u0, v, w) * cable_plug(*USBC_PLUG)


ALLOWED = {frozenset(p) for p in [("power_plug", "pi_usbc"), ("printer_plug", "pi_usb2"), ("glass", "screen"),
                                  ("screw_frame", "tab0"), ("screw_frame", "tab1"), ("screw_frame", "pod_frame"),
                                  ("screw_frame", "frame"), ("screw_body", "pod_body"), ("screw_body", "pod_frame"),
                                  ("screw_mount", "pod_mount"), ("insert_frame", "pod_frame"), ("insert_mount", "pod_mount"), ("screw_mount", "pod_body"), ("pod_frame", "pod_body"),
                                  ("pod_mount", "pod_body"), ("screw_pi", "pi_board"),
                                  ("channel", "frame"), ("display_fpc", "dsi_ribbon"), ("pi_dsi", "dsi_ribbon"),
                                  ("pi_dsi", "pi_board"), ("pi_gpio", "pi_board"), ("pi_gpio", "power_wire"),
                                  ("display_power", "power_wire"), ("pi_dsi", "cooler"), ("pi_gpio", "cooler"),
                                  ("screw_pi", "standoff0"), ("screw_pi", "standoff1"), ("screw_pi", "standoff2"),
                                  ("screw_pi", "standoff3"),
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


MINI_LCD_COLOR = "#3A3B3E"
MINI_LCD_SCREEN_COLOR = "#1F4BD1"


def mini_printer():
    """The Mini 2 meshes (build/mini2, from LulzBot's hibiscus_assembled.fcstd), in the places of that
    assembly. lcd_assem_01 is the stock LCD: the case on its bracket at the top of the right upright,
    and the clips of its cable. The screen window of the LCD is the thin plate (0.8 mm) behind the
    bezel, and it is blue."""
    import glob
    import trimesh
    out = []
    for f in sorted(glob.glob(os.path.join(HERE, "build", "mini2", "*.stl"))):
        m = trimesh.load(f, force="mesh")
        if "lcd" not in os.path.basename(f):
            out.append((m, "#C4C8CF" if "frame" in f else "#B0B4BC"))
            continue
        screen, rest = [], []
        for b in m.split(only_watertight=False):
            e = b.extents
            (screen if e[1] < 1.5 and e[0] > 80.0 else rest).append(b)
        out.append((trimesh.util.concatenate(rest), MINI_LCD_COLOR))
        if screen:
            out.append((trimesh.util.concatenate(screen), MINI_LCD_SCREEN_COLOR))
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
        T[:3, :3] = np.array([[0, 1, 0], [1, 0, 0], [0, 0, -1]], float)
        T[:3, 3] = (-28.0, PI_V0, -STANDOFF_W)
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
    import camera as C
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
        clip_groups = clips(name)
        # The clip normals in pod coordinates, for the exploded view.
        R_inv = np.linalg.inv(matrix(loc))[:3, :3]
        clip_normals = {n: tuple(R_inv @ np.array(nv)) for n, _, nv in clip_groups}
        V = lambda d: [d[0], -d[2], d[1]]          # pod (u, v, w) to the view (x, y, z)
        unplug = [i for i, (t, _) in enumerate(STAGES) if t.startswith("take out the four Pi")][0]

        def ex(n):
            e = {"explode": [V(d) for d in explode_steps(n, clip_normals)]}
            if n in FLEX:
                e["hide_after"] = unplug          # unplug the ribbon and the wire first
            return e
        items = [(n, zup_m(m, zup), c, ex(n)) for n, (m, c) in pod.items()]
        b = brm.copy()
        b.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("bracket", b, "#2A2B30", ex("bracket")))
        p = os.path.join(tmp, f"{name}_bracket_screws.stl")
        export_stl(bracket_screws(name), p, tolerance=0.03, angular_tolerance=0.1)
        bs = trimesh.load(p, force="mesh")
        bsv = bs.copy()
        bsv.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("bracket_screws", bsv, SCREW_COLOR))
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
            items.append((f"{cab}_cable", mv, CABLE_COLOR[cab], ex(f"{cab}_cable")))
            mw = m.copy()
            mw.apply_transform(frame_to_world)
            st.setdefault("cables", []).append((mw, CABLE_COLOR[cab]))
        for gname, shape, _ in clip_groups:
            p = os.path.join(tmp, f"{name}_{gname}.stl")
            export_stl(shape, p, tolerance=0.05, angular_tolerance=0.2)
            cm = trimesh.load(p, force="mesh")
            cv = cm.copy()
            cv.apply_transform(zup @ np.linalg.inv(matrix(loc)))
            items.append((gname, cv, SCREW_COLOR if "screws" in gname else "#E8762B", ex(gname)))
            cm.apply_transform(frame_to_world)
            st["cables"].append((cm, "#E8762B"))
        # The webcam on the frame (camera.py), in the frame of the printer like the clips.
        cam = C.meshes(name, tmp)
        for n, (m, c) in cam.items():
            cv = m.copy()
            cv.apply_transform(zup @ np.linalg.inv(matrix(loc)))
            items.append((n, cv, c))
        for msg in C.check(name):
            print(f"{name}: camera: {msg}")
        near = printer + C.stock_lcd(name, printer)
        cgap, agap = C.printer_gap(name, near, frame_to_world)
        print(f"{name}: camera: {cgap:.1f} mm from the printer over its tilt range, the arm {agap:.1f} mm, "
              f"the base {C.base_touch(name, near, frame_to_world):.2f} mm into the frame")
        p = os.path.join(tmp, f"{name}_usb_b.stl")
        export_stl(usb_b_plugs(name), p, tolerance=0.05, angular_tolerance=0.2)
        plug = trimesh.load(p, force="mesh")
        pv_ = plug.copy()
        pv_.apply_transform(zup @ np.linalg.inv(matrix(loc)))
        items.append(("usb_b_plug", pv_, "#151515"))
        # The exploded view must not pass one part through another: the pod parts and the bracket,
        # in pod coordinates.
        check_meshes = {n: m for n, (m, c) in pod.items() if n not in FLEX}
        bl = brm.copy()
        bl.apply_transform(np.linalg.inv(matrix(loc)))
        check_meshes["bracket"] = bl
        clash = explode_check(check_meshes, clip_normals)
        for pair, stage in clash:
            print(f"{name}: exploded view: {pair[0]} passes through {pair[1]} ({stage})")
        if not clash:
            print(f"{name}: exploded view: no part passes through another")
        plug.apply_transform(frame_to_world)
        st["cables"].append((plug, "#151515"))
        gap = clearance([m for m, _ in pod.values()], pod_to_world, printer)
        print(f"{name}: the pod is {gap:.1f} mm from the printer at the closest point")
        note = ("no overlaps" if not bad else f"{len(bad)} overlaps") + f", {gap:.1f} mm from the printer"
        views = {"camera": C.camera_pose(name, zup @ np.linalg.inv(matrix(loc)))}
        viewer.publish(items, name, note, stages=[t for t, _ in STAGES], views=views)
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
        for n, (m, c) in cam.items():
            m = m.copy()
            m.apply_transform(frame_to_world)
            allm.append((m, c))
        C.views(name, printer, allm, pod_to_world, frame_to_world, out)
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
MINI_UPRIGHT_X = -357.0          # the power cable runs down the face of the left upright and the case
TAZ_COVER_Y = 7.5                # the power cable clips on the side cover, along its lower edge


def clips(name):
    """The printed cable clips (clips.py), placed in the frame of the printer, in groups:
    (name, shape, the normal of their mounting face in the frame)."""
    import clips as C
    groups = []
    one, _ = C.tape_clip()
    if name == "taz":
        # On the side cover: the power cable along the lower edge, the printer cable at the height
        # of the USB-B port. The cables run along x.
        on_cover = lambda x, y: Location(Plane(origin=(x, y, C.TAPE), x_dir=(0, 1, 0), z_dir=(0, 0, 1))) * one
        groups.append(("cover_clips", [on_cover(x, TAZ_COVER_Y) for x in (50.0, -10.0, -70.0)]
                       + [on_cover(x, TAZ_USB_B[0]) for x in (40.0, -40.0)], (0.0, 0.0, 1.0)))
    else:
        pair, _ = C.tape_clip(True)
        # On the front of the electronics case, between the frame screws (x -359, z 50, 124, 198):
        # both cables side by side, running along z.
        out = [Location(Plane(origin=(MINI_UPRIGHT_X, MINI_CASE_Y - C.TAPE, z), x_dir=(1, 0, 0), z_dir=(0, -1, 0))) * pair
               for z in (90.0, 165.0)]
        groups.append(("upright_clips", out, (0.0, -1.0, 0.0)))
    res = []
    for n, out, normal in groups:
        s = out[0]
        for o in out[1:]:
            s = s + o
        res.append((n, s, normal))
    return res


# ---------------------------------------------------------------- the exploded view
# The stages, in the order the pod comes apart: (title, {part group: (u, v, w) in pod coordinates}).
DISPLAY = ("glass", "screen", "frame", "tab", "standoff", "channel", "display_")
PI = ("pi5", "cooler", "pi_")
FLEX = ("dsi_ribbon", "power_wire")      # they move with the display and hide when the Pi comes off
POD_PARTS = DISPLAY + PI + FLEX + ("pod_body", "pod_frame", "pod_mount", "screw_frame", "screw_body", "screw_mount",
                                   "screw_pi", "insert_frame", "insert_mount")
STAGES = [
    ("unplug the power and printer cables", {("power_plug", "power_cable"): (-40.0, 0.0, 0.0),
                                             ("printer_plug", "printer_cable"): (0.0, 40.0, 0.0)}),
    ("lift the pod off the dovetail", {POD_PARTS: (0.0, DOVE_LIFT, 0.0)}),
    ("pull the pod forward off the bracket", {POD_PARTS: (0.0, 0.0, 60.0)}),
    ("take out the four screws of the body", {("screw_body",): (0.0, 0.0, -30.0)}),
    ("pull the display and the frame out of the body", {DISPLAY + PI + FLEX + ("pod_frame", "insert_frame", "screw_frame", "screw_pi"):
                                                        (0.0, 0.0, 80.0)}),
    ("take out the four screws of the frame", {("screw_frame",): (0.0, 0.0, -60.0)}),
    ("take the frame off the display", {("pod_frame", "insert_frame"): (0.0, 0.0, -40.0)}),
    ("take out the four Pi screws", {("screw_pi",): (0.0, 0.0, -60.0)}),
    ("take the Pi off the display", {PI: (0.0, 0.0, -40.0)}),
    ("take the clips off", {}),
    ("take out the two screws of the mount", {("screw_mount",): (0.0, 0.0, 30.0)}),
    ("take the mount off the body", {("pod_mount", "insert_mount"): (0.0, 0.0, -25.0)}),
]
CLIP_LIFT = 25.0



def explode_steps(part, clip_normals):
    """The move of a part in each stage, in pod coordinates."""
    out = []
    for title, moves in STAGES:
        d = (0.0, 0.0, 0.0)
        for group, v in moves.items():
            if any(part.startswith(g) for g in group):
                d = tuple(a + b for a, b in zip(d, v))
        if title.startswith("take the clips") and part in clip_normals:
            d = tuple(CLIP_LIFT * c for c in clip_normals[part])
        out.append(d)
    return out


def explode_check(meshes, clip_normals, steps=40):
    """Move the parts through the stages in small steps, and report each pair of parts that touch
    during the move and did not touch when assembled."""
    import numpy as np
    import trimesh
    names = list(meshes)
    moves = {n: np.array(explode_steps(n, clip_normals)) for n in names}
    cm = trimesh.collision.CollisionManager()
    for n in names:
        cm.add_object(n, meshes[n])
    _, base = cm.in_collision_internal(return_names=True)
    base = {frozenset(p) for p in base}
    bad = set()
    k = len(STAGES)
    for i in range(1, steps * k + 1):
        t = i / steps
        for n in names:
            off = sum(moves[n][j] * min(1.0, max(0.0, t - j)) for j in range(k))
            T = np.eye(4)
            T[:3, 3] = off
            cm.set_transform(n, T)
        hit, pairs = cm.in_collision_internal(return_names=True)
        for p in pairs:
            fp = frozenset(p)
            same = all((moves[a] == moves[b]).all() for a, b in [tuple(p)])
            if fp not in base and not same:
                bad.add((tuple(sorted(p)), STAGES[min(int(t - 1e-9), k - 1)][0]))
    return sorted(bad)


def bracket_screws(name):
    """The screws of the bracket, in the frame of the printer: on the TAZ the cover screw of the
    printer through the nub, on the Mini 2 the two frame screws (M3 x 10) through the plate."""
    if name == "taz":
        x_s, y_s = COVER_SCREW
        return screw(3, 8.0, (x_s, y_s, NUB_GAP + 3.3), (0, 0, 1))
    out = None
    for x, z in MINI_SCREWS:
        sc = screw(3, 10.0, (x, MINI_PLATE_Y - PLATE, z), (0, -1, 0))
        out = sc if out is None else out + sc
    return out


def usb_b_plugs(name):
    """The USB-B plug in the port of the printer, in the coordinates of the printer's frame."""
    if name == "taz":
        # The port is in the rear face of the RAMBo, at x = -85: the plug points to +x, upright.
        py, pz = TAZ_USB_B
        return Pos(-85.0, py, pz) * Rot(90, 0, 0) * cable_plug(*USBB_PLUG)
    # The port is in the front of the electronics case: the plug points to +y.
    px, pz = MINI_USB_B
    return Pos(px, MINI_CASE_Y, pz) * Rot(0, 0, 90) * cable_plug(*USBB_PLUG)


def cable_starts():
    """Where each cable leaves its plug, in pod coordinates: (point, direction)."""
    u, v, w = pi_uvw(11.2, -1.0, 3.2)
    x0, x1, y0, y1, z0, z1 = pi_parts()["pi_usb2"]
    b = pi_box(x0, x1, y0, y1, z0, z0 + 7.5).bounding_box()
    return {"power": ((u - 6.5 - 24.0, v, w), (-1.0, 0.0, 0.0)),
            "printer": (((b.min.X + b.max.X) / 2, b.max.Y + 30.0, (b.min.Z + b.max.Z) / 2), (0.0, 1.0, 0.0))}


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
        pod = lambda u, v, w: tuple((pod_to_frame @ np.array([u, v, w, 1.0]))[:3])
        u0, v0, w0 = p
        import clips as C
        if name == "taz":
            # Mount coordinates: x to the front, y up (the desk is y = -14), z out of the cover.
            desk = -12.0
            cz = C.TAPE_CABLE_H                        # the cables on the cover clips
            if cab == "power":
                # Out of the left side of the pod toward the cover, around the front edge of the
                # chassis, down the cover to its lower edge, back along it in clips, and out behind
                # the printer to the outlet.
                way = [(S.X_FRONT - 6.0, y - 20.0, cz), (60.0, TAZ_COVER_Y, cz), (-80.0, TAZ_COVER_Y, cz),
                       (-100.0, desk, cz), (-250.0, desk, cz)]
            else:
                # Up out of the top of the pod, over its top to the left, around the front edge of
                # the chassis onto the cover, up to the height of the USB-B port of the RAMBo, back
                # along the cover in clips, around the rear corner and along the rear face to the
                # port. The port is left of the second extruder connector (seen from behind).
                py, pz = TAZ_USB_B
                top = v0 + 20.0
                way = [pod(u0, top + 8.0, w0), pod(-BODY_U - 10.0, top + 8.0, w0), (S.X_FRONT - 6.0, py, cz),
                       (-80.0, py, cz), (-92.0, py, cz), (-92.0, py, -8.0), (-125.0, py, -8.0), (-125.0, py, pz),
                       (-85.0 - USB_B_PLUG, py, pz)]
        else:
            # The Mini 2: z up (the desk is z = -7), the front toward -y. Both cables go to the front
            # of the left upright and down it in clips, side by side.
            desk = -5.0
            ux = MINI_UPRIGHT_X
            uy = MINI_CASE_Y - C.TAPE_CABLE_H
            if cab == "power":
                # Out of the left side of the pod, down under the pod, back to the front of the
                # electronics case, down it, and out to the left on the desk.
                ux -= C.PAIR / 2
                under = to_frame(MINI_LOC, 0, DEEP_V0, -DEEP_W)[2] - 20.0
                way = [(x, y, under), (ux, uy, under - 25.0), (ux, uy, 30.0), (ux, MINI_PLATE_Y - 20.0, desk),
                       (-480.0, MINI_PLATE_Y - 40.0, desk), (-560.0, MINI_PLATE_Y - 80.0, desk)]
            else:
                # Up out of the top of the pod, over its top to the right, down the upright past the
                # tie anchors of the plate, for a zip tie, and down the front of the electronics case
                # to the USB-B port. The plug goes in from the front.
                ux += C.PAIR / 2
                px, pz = MINI_USB_B
                top = v0 + 20.0
                a1 = pod(u0, top + 8.0, w0)
                ya = MINI_PLATE_Y - PLATE - 5.0 - 2.5                     # in front of the tie anchors
                low = to_frame(MINI_LOC, 0, DEEP_V1, -DEEP_W)[2] - 70.0    # under the bracket plate
                way = [a1, pod(BODY_U + 6.0, top + 8.0, w0), (MINI_TIE_X, ya, a1[2] - 30.0), (MINI_TIE_X, ya, low),
                       (ux, uy, low - 30.0), (ux, uy, pz + 40.0),
                       (px, uy, pz + 20.0), (px, MINI_CASE_Y - USB_B_PLUG - 12, pz), (px, MINI_CASE_Y - USB_B_PLUG, pz)]
        out[cab] = [tuple(p0), tuple(p1)] + way
    return out


def rounded(points, r=BEND_R, steps=8):
    """A path through the points with a round bend of radius about r at each corner. A point on top
    of the one before it is dropped: a step of length zero has no direction, and the sweep of the
    cable twists there."""
    import numpy as np
    pts = []
    for p in points:
        p = np.array(p, float)
        if not pts or np.linalg.norm(p - pts[-1]) > 1e-3:
            pts.append(p)
    out = [pts[0]]
    for i in range(1, len(pts) - 1):
        a, b, c = pts[i - 1], pts[i], pts[i + 1]
        la, lc = np.linalg.norm(b - a), np.linalg.norm(c - b)
        k = min(r, la / 2, lc / 2)
        p, q = b + (a - b) / la * k, b + (c - b) / lc * k
        for t in np.linspace(0, 1, steps):
            out.append((1 - t) ** 2 * p + 2 * (1 - t) * t * b + t ** 2 * q)
    out.append(pts[-1])
    keep = [out[0]]
    for p in out[1:]:
        if np.linalg.norm(p - keep[-1]) > 1e-3:
            keep.append(p)
    return np.array(keep)


def cable_mesh(points, d, sides=16):
    """The cable: a tube of diameter d along the rounded path. Each ring of the tube has the frame of
    the ring before it, turned only as much as the path turns (parallel transport), so the tube has
    no twist."""
    import numpy as np
    import trimesh
    path = rounded(points)
    n = len(path)
    seg = np.diff(path, axis=0)
    seg /= np.linalg.norm(seg, axis=1)[:, None]
    tang = np.vstack([seg[:1], seg[:-1] + seg[1:], seg[-1:]])
    tang /= np.linalg.norm(tang, axis=1)[:, None]
    a = np.cross(tang[0], [0.0, 0.0, 1.0])
    if np.linalg.norm(a) < 1e-6:
        a = np.cross(tang[0], [1.0, 0.0, 0.0])
    normal = a / np.linalg.norm(a)
    ang = np.linspace(0, 2 * np.pi, sides, endpoint=False)
    rings = []
    for i in range(n):
        if i:
            normal = normal - np.dot(normal, tang[i]) * tang[i]
            normal /= np.linalg.norm(normal)
        bi = np.cross(tang[i], normal)
        rings.append(path[i] + d / 2 * (np.cos(ang)[:, None] * normal + np.sin(ang)[:, None] * bi))
    verts = np.vstack(rings + [path[:1], path[-1:]])
    faces = []
    for i in range(n - 1):
        for j in range(sides):
            a0, a1 = i * sides + j, i * sides + (j + 1) % sides
            b0, b1 = a0 + sides, a1 + sides
            faces += [(a0, a1, b1), (a0, b1, b0)]
    c0, c1 = n * sides, n * sides + 1
    for j in range(sides):
        faces.append((c0, (j + 1) % sides, j))
        last = (n - 1) * sides
        faces.append((c1, last + j, last + (j + 1) % sides))
    m = trimesh.Trimesh(verts, np.array(faces), process=True)
    m.fix_normals()
    return m, path


def length(path):
    import numpy as np
    return float(np.linalg.norm(np.diff(path, axis=0), axis=1).sum())


def zup_m(m, zup):
    m = m.copy()
    m.apply_transform(zup)
    return m


# ---------------------------------------------------------------- the STL files for printing
# Each process has its own folder and its own clearance for the dovetail. Each part is turned to
# its print orientation and centered on the bed. FDM parts have their lowest point at z = 0, resin
# parts at z = RESIN_LIFT.
PRINT_DIR = os.path.join(HERE, "stl", "pod")
DOVE_CLR_BY = {"fdm": 0.35, "resin": 0.2}
RESIN_LIFT = 6.0                # resin: the parts hang on supports, this far over the build plate


def on_bed(shape, rot, lift=0.0):
    """The shape turned by rot (a Rot or a chain of them), centered on the bed, its lowest point at
    z = lift."""
    s = rot * shape
    bb = s.bounding_box()
    return Pos(-(bb.min.X + bb.max.X) / 2, -(bb.min.Y + bb.max.Y) / 2, lift - bb.min.Z) * s


def print_parts(process):
    """(file name, shape, how many) for each printed part of the pod, in print orientation."""
    import camera
    import clips as C
    global DOVE_CLR, RESIN
    keep, DOVE_CLR, RESIN = DOVE_CLR, DOVE_CLR_BY[process], process == "resin"
    try:
        case, fr, mt, wd = body(), pod_frame(), mount(), wedge_part()
        taz, mini = bracket(with_wedge=False), mini_bracket(with_wedge=False)
    finally:
        DOVE_CLR, RESIN = keep, False
    one, _ = C.tape_clip()
    two, _ = C.tape_clip(True)
    cam = [part for n in ("taz", "mini2") for part in camera.printed(n)]
    if process == "fdm":
        # The body: flat on its back. The frame: flat on its back ring. The mount: flat on the face
        # that goes on the body, the dovetail up. The wedge: standing on its level bottom, the key and
        # the slot up. The TAZ bracket: standing on the top end of its plate, so the groove of the
        # wedge and the slots of the clips run up from the bed. The Mini 2 bracket: the frame side of
        # the plate on the bed. The clips: the base on the bed. The webcam parts print in the
        # positions that camera.printed() gives. No part needs supports.
        rots = {"case": Rot(0, 0, 0), "frame": Rot(0, 0, 0), "mount": Rot(180, 0, 0), "wedge": Rot(90, 0, 0),
                "taz": Rot(-90, 0, 0), "mini": Rot(-90, 0, 0), "clip": Rot(0, 0, 0), "camera": Rot(0, 0, 0)}
    else:
        # Resin: z = 0 is the build plate, and the slicer adds supports under each face that faces
        # it. The faces that show on the pod face away from the plate. The angles come from a search
        # of the support area, the largest layer and the cups (stl/pod/README.md).
        # - body: the open front toward the plate, turned 45 degrees about its long axis and 15
        #   degrees along it. The back and the side walls face away from the plate.
        # - frame: it stands on its bottom end, leans 20 degrees with the back ring up, and turns 15
        #   degrees. Supports go on the bottom end and inside.
        # - mount: it stands on the lower end of the dovetail, the face on the body up 15 degrees.
        # - wedge: on its level bottom, 15 degrees with the slot up, and 10 degrees across.
        # - TAZ bracket: on the top end of the plate as in FDM, the back of the plate toward the
        #   plate 20 degrees, and 10 degrees across.
        # - clips: the base toward the plate, 30 and 15 degrees.
        rots = {"case": Rot(0, 0, 90) * Rot(-165, 0, 0) * Rot(0, -45, 0),
                "frame": Rot(110, 0, 0) * Rot(0, 15, 0), "mount": Rot(75, 0, 0),
                "wedge": Rot(10, 0, 0) * Rot(0, -15, 0) * Rot(90, 0, 0),
                "taz": Rot(0, 0, 90) * Rot(10, 0, 0) * Rot(0, -20, 0) * Rot(-90, 0, 0),
                "mini": Rot(30, 0, 0) * Rot(-90, 0, 0), "clip": Rot(30, 0, 0) * Rot(0, 15, 0), "camera": Rot(30, 0, 0)}
    lift = RESIN_LIFT if process == "resin" else 0.0
    return [(f"pod-body_{process}.stl", on_bed(case, rots["case"], lift), 1),
            (f"pod-frame_{process}.stl", on_bed(fr, rots["frame"], lift), 1),
            (f"pod-mount_{process}.stl", on_bed(mt, rots["mount"], lift), 1),
            (f"pod-wedge_{process}.stl", on_bed(wd, rots["wedge"], lift), 1),
            (f"pod-bracket-taz_{process}.stl", on_bed(taz, rots["taz"], lift), 1),
            (f"pod-bracket-mini2_{process}.stl", on_bed(mini, rots["mini"], lift), 1),
            *[(f"{fn}_{process}.stl", on_bed(sh, rots["camera"] * rot, lift), 1) for fn, sh, rot in cam],
            (f"cable-clip-single_{process}.stl", on_bed(one, rots["clip"], lift), 5),
            (f"cable-clip-double_{process}.stl", on_bed(two, rots["clip"], lift), 2)]


def export_prints(out=PRINT_DIR):
    from build123d import export_stl
    for process in ("fdm", "resin"):
        d = os.path.join(out, process)
        os.makedirs(d, exist_ok=True)
        for name, shape, n in print_parts(process):
            export_stl(shape, os.path.join(d, name), tolerance=0.01, angular_tolerance=0.1)
            bb = shape.bounding_box()
            print(f"{process}/{name}: {bb.max.X - bb.min.X:.1f} x {bb.max.Y - bb.min.Y:.1f} x "
                  f"{bb.max.Z - bb.min.Z:.1f} mm, print {n}")


# ---------------------------------------------------------------- the overhang check
# A face needs support when it faces down at less than 45 degrees above the bed (the faces on the bed
# are out). A flat roof is a bridge, and it needs no support if the opening under it is BRIDGE mm
# across or less: then each line of the bridge has its two ends on the walls of the opening.
OVERHANG = 45.0
BRIDGE = 6.0


def overhangs(m, px=0.05):
    """The faces of a mesh (in print orientation) that face down: (steep, roofs). steep is a mask of
    the faces under OVERHANG degrees that are not flat. roofs is a list of (face indices, z, area,
    span) for each flat roof: span is the width of the opening under it (the diameter of the
    largest circle in the outline of the roof with its holes filled, that has no part of the layer
    under it)."""
    import numpy as np
    from scipy import ndimage
    from shapely import contains_xy
    from shapely.geometry import Polygon as SP
    from shapely.ops import unary_union
    nz = m.face_normals[:, 2]
    zc = m.triangles_center[:, 2]
    up = zc > m.bounds[0][2] + 0.05
    flat = up & (nz < -0.999)
    steep = up & (nz < -math.sin(math.radians(OVERHANG)) - 1e-3) & ~flat
    roofs = []
    zs = np.round(zc, 2)
    for z in np.unique(zs[flat]):
        idx = np.nonzero(flat & (zs == z))[0]
        tri = {i: SP(m.triangles[i][:, :2]).buffer(1e-4) for i in idx}
        roof = unary_union(list(tri.values()))
        sec = m.section(plane_origin=(0, 0, z - 0.05), plane_normal=(0, 0, 1))
        below = SP()
        for loop in (sec.discrete if sec is not None else []):
            if len(loop) > 2:
                below = below.symmetric_difference(SP(loop[:, :2]).buffer(0))
        for g in getattr(roof, "geoms", [roof]):
            if g.area < 0.05:
                continue
            full = SP(g.exterior)
            x0, y0, x1, y1 = full.buffer(1.0).bounds
            X, Y = np.meshgrid(np.arange(x0, x1, px), np.arange(y0, y1, px))
            inside = contains_xy(full, X, Y)
            d = ndimage.distance_transform_edt(~contains_xy(below, X, Y)) * px
            span = 2 * float(d[inside].max()) if inside.any() else 0.0
            faces = [i for i in idx if g.buffer(0.01).contains(tri[i].centroid)]
            roofs.append((faces, float(z), g.area, span))
    return steep, roofs


def overhang_report(m):
    """(area that needs support, area of the short bridges, the longest short bridge, the roofs
    that need support as (z, area, span))."""
    steep, roofs = overhangs(m)
    long_ = [(z, a, s) for _, z, a, s in roofs if s > BRIDGE + 0.05]
    short = [(z, a, s) for _, z, a, s in roofs if s <= BRIDGE + 0.05]
    need = float(m.area_faces[steep].sum()) + sum(a for _, a, _ in long_)
    return need, sum(a for _, a, _ in short), max([s for *_, s in short], default=0.0), long_


def check_prints(out=PRINT_DIR):
    """Print the overhang report of each FDM file."""
    import glob
    import trimesh
    worst = 0.0
    for p in sorted(glob.glob(os.path.join(out, "fdm", "*.stl"))):
        need, short, span, long_ = overhang_report(trimesh.load(p, force="mesh"))
        worst = max(worst, need)
        print(f"{os.path.basename(p):30s} needs support {need:6.1f} mm2; bridges {short:6.1f} mm2, "
              f"longest {span:.1f} mm")
        for z, a, s in long_:
            print(f"    roof at z {z:.2f}: {a:.1f} mm2, {s:.1f} mm across")
    return worst


if __name__ == "__main__":
    if "overhangs" in sys.argv:
        sys.exit(1 if check_prints() > 1.0 else 0)
    if "export" in sys.argv:
        export_prints()
        sys.exit()
    args = [a for a in sys.argv[1:] if a != "live"]
    main(args[0] if args else os.path.join(HERE, "build", "pod"), stills="live" not in sys.argv)
