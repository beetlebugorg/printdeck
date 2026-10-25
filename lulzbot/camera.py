"""A Logitech C270 webcam on the printer frame, aimed at the bed from the front right and above, near
the isometric view of live.py (key i). There is one mount for each printer. Each mount holds to the
frame with no drilling, and holds the camera on a short arm. Each mount is two printed parts, and
each part prints with no supports: the clamp on the frame, and the arm with the hinge (see "the
mount" below).

- TAZ Workhorse: the clamp is a saddle on the front right upright (2020 extrusion), under the corner
  plate. A key goes into the front slot and a key goes into the right slot, so the saddle fits only
  one way. Two M3 x 8 screws go into metal roll-in T-nuts in the two slots (the same T-nuts as the
  rail clips of the deck).
- Mini 2: the clamp is a plate on the right face of the front right corner. The two frame screws of
  the corner hold it: take each one out and put in a screw 5 mm longer. A lip on the top of the
  frame keys the plate. The camera is out to the right of the frame and looks in under its top rail:
  from the front right corner the stock LCD hides most of the bed.

The camera is used without its stock clip. In the reference model of the C270 (see the sizes below),
the clip holds the camera with a pin through an eyelet on the back of the camera body, and a small
screw at the end of the pin holds the pin. Take out the screw, push the pin out, and the clip comes
off. Check this on the real camera. The mount holds the bare camera body on an M3 x 16 screw through
the same eyelet, between two lugs. The screw goes through the thin lug and cuts its thread in the
thick lug. Tighten it until the camera holds its tilt: the thin lug clamps the eyelet. The mount sets
the turn of the camera, and the tilt is set by hand.

The USB cable goes along the frame to the Pi on the deck, in printed cable clips (clips.py).

Printer coordinates: on the TAZ, the mount coordinates of shape.py (x to the front, y up, z out of the
left cover, so the right of the printer is -z). On the Mini 2, the coordinates of its model (x to the
right, y to the back, z up). Camera coordinates: x along the eyelet pin (the side away from the lens is
+x), y up, z forward (out of the lens); the origin is on the pin axis, in the middle of the eyelet.
Hinge coordinates are the same with the camera level (no tilt).

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with pyrender --with PyOpenGL --with pillow --with shapely --with rtree python camera.py [OUT_DIR]
It prints the aim and the bed in the picture for each printer and each place that was checked, the
fit, the stiffness, and the print check of each STL (pod.py export writes them), and with OUT_DIR it
writes close views of the mounts and of each part in its print position.
"""

import math
import os
import sys

from build123d import Box, Cylinder, Location, Plane, Pos, RectangleRounded, Rot, loft

import pod as P

HERE = os.path.dirname(os.path.abspath(__file__))

# ---------------------------------------------------------------- the camera (Logitech C270)
# Body sizes from the C270 STEP model of the VoronUsers C270 mount by Fiction (GPLv3; not in this
# repository): https://github.com/VoronDesign/VoronUsers/tree/d5651692788228ce13bb57ea7327ec947543311c/printer_mods/Fiction/C270_mount
# They agree with the PocketForge model of the stock camera (71.00 x 31.55 face, measured):
# https://github.com/pocketforge-os/test-node-hw (mechanical/dut-fixture-v1/logitech-c270.scad).
# Logitech gives 72.91 x 31.91 x 66.64 mm with the clip. Measure the values marked [M].
BODY_X = 6.5                   # [M] the middle of the body, from the middle of the eyelet (+x, away from the lens)
BODY_BACK_Z = 2.4              # [M] the back face, in front of the pin axis
BODY = [(2.4, 57.1, 21.2), (17.7, 71.0, 31.35), (19.5, 69.6, 29.9)]   # (z, width, height): back, widest, front
EYELET = (7.0, 5.7, 3.1, 4.0)  # [M] width (x), height (y), behind the pin axis (z), the square hole
BACK_GROOVE_R = 3.0            # a groove in the back face along the pin, x -15 to 15 (the stock pin lies in it)
LENS = (-10.0, 0.0, 11.4)      # [M] x, y of the lens, and the opening in the front face
CABLE = (14.6, 4.5, 3.6, 6.0)  # [M] x, y where the cable leaves the back; the cable and the strain relief diameter
CABLE_LENGTH = 1500.0          # Logitech: 1.5 m
FOV_DIAG = 55.0                # Logitech product page: 55 degrees diagonal. The support page says 60.
ASPECT = 16 / 9                # 1280 x 720
BLACK = "#16171A"
ORANGE = "#E8762B"

# ---------------------------------------------------------------- the printers
# up, back (into the printer, level), right, in printer coordinates. The beds: the center of the top
# face of the bed in the LulzBot model (the bed is where the model has it, about the middle of its
# travel), the two directions of its sides, and the print area and height. The print sizes are the
# published LulzBot sizes (TAZ Workhorse 280 x 280 x 285 mm, Mini 2 160 x 160 x 180 mm); they were
# not checked against the models.
AXES = {"taz": ((0, 1, 0), (-1, 0, 0), (0, 0, -1)), "mini2": ((0, 0, 1), (0, 1, 0), (1, 0, 0))}
BEDS = {"taz": ((9.0, 70.7, -361.4), (1, 0, 0), (0, 0, 1), 280.0, 285.0),
        "mini2": ((-200.0, -176.0, 85.1), (1, 0, 0), (0, 1, 0), 160.0, 180.0)}

# The isometric view of live.py (key i): the eye is at (u, w, v) = (1, 1, 0.8) from the pod, so the
# view goes along ISO_POD in pod coordinates (u, v, w): to the left, down, and back. ISO_YAW and
# ISO_PITCH are its angles (set under angles()).
ISO_POD = (-1.0, -0.8, -1.0)

# The place of each camera: the lens, the turn (yaw: degrees to the right of straight back, so the
# view of live.py is negative), and the tilt down (pitch). camera.py with no arguments checks the other
# places in CANDIDATES and prints them.
PLACES = {"taz": dict(lens=(150.0, 520.0, -680.0), yaw=-60.0, pitch=52.0),
          "mini2": dict(lens=(60.0, -200.0, 355.0), yaw=-68.0, pitch=46.0)}
PITCH_RANGE = (30.0, 70.0)     # the camera turns through this range without a touch
CANDIDATES = {
    "taz": {"T1 over the front right corner, 60 mm up": (90, 550, -690),
            "T2 in front of the front right upright, y 400": (166, 400, -663),
            "T3 right of the front right upright, y 420": (110, 420, -750),
            "T4 front right corner, 45 mm forward, 55 mm right": (130, 500, -730),
            "T5 over the middle of the top front rail": (100, 550, -500),
            "T6 on the line of the i view, 160 mm forward": (246, 392, -673),
            "T7 front right corner, 110 mm forward, 80 mm up": (200, 570, -700)},
    "mini2": {"M1 front right corner, 45 mm out, 50 mm up": (45, -290, 420),
              "M2 over the front right corner": (-20, -270, 433),
              "M3 front right corner, 45 mm out, z 330": (45, -290, 330),
              "M4 on the line of the i view, in front of the stock LCD": (0, -328, 291),
              "M5 front right corner, 65 mm out and forward": (65, -315, 405),
              "M6 right side, 100 mm out, y -190": (100, -190, 375),
              "M7 right side, 75 mm out, y -190, z 350": (75, -190, 350)},
}

# ---------------------------------------------------------------- the mount
# Two printed parts for each printer. Both print with no supports.
# - The arm: a flat part. It has the two lugs of the hinge, a beam behind the camera, and a post
#   down to the clamp, all in a slab square to the hinge pin. It prints flat on the back face of the
#   slab (hinge z = ARM_Z[0]). Its faces are square to that face or face up, and the hinge holes
#   have teardrop tops.
# - The clamp: the saddle (TAZ) or the plate (Mini 2), a connector, and a tab for the post of the
#   arm. Every part of it stands straight up from its bottom, so it prints standing on its bottom.
#   Its holes lie on their side and have teardrop tops.
# The post goes into a channel on the tab (two ribs and a floor). Two M3 x 18 screws go through the
# post, from its back face, into M3 heat-set inserts in the tab. The ribs and the floor are on one side
# of the tab only, so the arm goes on one way.
LUG_GAP = 0.2                  # each side of the eyelet
LUG_THIN = 3.0                 # the lug on the lens side: it bends when the screw is tight
LUG_THICK = 6.8                # the lug on the cable side: the screw cuts its thread in it
SCREW = (3, 16.0)              # M3 x 16 socket cap screw
SCREW_HOLE, PILOT = 3.4, 2.5
COUNTERBORE = (6.2, 2.3)       # for an M3 cap head: diameter, the plastic under the head
ARM_Z = (-20.0, -6.0)          # the slab of the arm, behind the pin axis (hinge z)
BEAM_Y = (6.0, 20.0)           # the beam behind the camera, over the pin axis (hinge y)
LUG_Y = (-6.0, BEAM_Y[1])      # the lugs go down past the pin axis
LUG_TOP_Z = 6.0
POST_W = 14.0
# The post: its hinge x (past the end of the camera, so the camera turns clear of it), and the
# height of its bottom end in printer coordinates.
POST = {"taz": dict(x=-45.0, bottom=400.0), "mini2": dict(x=55.0, bottom=340.0)}
JOINT = dict(length=34.0, screws=(8.0, 26.0), tab=7.0, rib=4.0, rib_depth=6.0, insert=(2.0, 6.0))
JOINT_SCREW = 18.0             # M3 x 18: through the 14 mm post, 4 mm into the insert
CONNECTOR_T = 7.0

# ---------------------------------------------------------------- the TAZ saddle
# The front right upright: its front face (x), its right face (z), and the slots in them. The corner
# plate of the frame covers the front face from y 416 up, and the top of the frame covers the right
# face from y 470 up. The front leg of the saddle stops under the corner plate, and the right leg goes
# on up to the top.
TAZ_UPRIGHT = dict(front=86.0, right=-673.0, front_slot=-662.6, right_slot=76.0, back=66.0, left=-653.0)   # measured in the model
TAZ_SADDLE_Y = (340.0, 412.0, 466.0)            # the bottom, the top of the front leg, the top of the right leg
TAZ_SADDLE_T = 5.0
TAZ_CORNER_PLATE_Y = 416.0     # the bottom of the corner plate on the front of the upright (3 mm thick)
SLOT_KEY = (4.8, 1.5)          # the key in the slot: width, depth. The slot is 5.1 mm at the round lips (shape.py).
TNUT_SCREW = 8.0               # M3 x 8 into a roll-in T-nut: 2.3 mm of plastic, the nut 1.8 mm behind the face

# ---------------------------------------------------------------- the Mini 2 plate
# The right face of the frame (x = 2) at the front right corner, and the two frame screws of the
# corner in it. A lip goes over the top of the frame: its underside rises 45 degrees from the edge.
MINI_FACE_X = 2.0
MINI_TOP_Z = 373.0
MINI_SCREWS = [(-243.0, 340.0), (-243.0, 364.0)]   # (y, z)
MINI_PLATE = dict(t=5.0, y=(-253.0, -228.0), z=(328.0, MINI_TOP_Z), lip=10.0)
MINI_SCREW_LEN = 10.0          # [M] the frame screw is M3 in the model: use one 5 mm longer than the one you take out
CLAMP_BOTTOM = {"taz": TAZ_SADDLE_Y[0], "mini2": MINI_PLATE["z"][0]}


def unit(v):
    import numpy as np
    v = np.asarray(v, float)
    return v / np.linalg.norm(v)


def axes(name):
    import numpy as np
    return tuple(np.array(a, float) for a in AXES[name])


def look(name, yaw, pitch):
    """The direction of view in printer coordinates."""
    up, back, right = axes(name)
    y, p = math.radians(yaw), math.radians(pitch)
    return math.cos(p) * (math.cos(y) * back + math.sin(y) * right) - math.sin(p) * up


def pod_angles(d):
    """(yaw, pitch) of a direction in pod coordinates. The pod faces the front on both printers, so
    the angles are the same in the printer."""
    import numpy as np
    s, c = math.sin(P.TILT), math.cos(P.TILT)
    up, back, right = np.array([0.0, c, s]), np.array([0.0, s, -c]), np.array([1.0, 0.0, 0.0])
    d = unit(d)
    return (math.degrees(math.atan2(np.dot(d, right), np.dot(d, back))), math.degrees(math.asin(-np.dot(d, up))))


ISO_YAW, ISO_PITCH = pod_angles(ISO_POD)


def angle_between(a, b):
    import numpy as np
    return math.degrees(math.acos(float(np.clip(np.dot(unit(a), unit(b)), -1.0, 1.0))))


def cam_loc(name, pitch=None):
    """The camera frame in printer coordinates, at the tilt pitch (the set tilt when None)."""
    import numpy as np
    pl = PLACES[name]
    up = axes(name)[0]
    f = look(name, pl["yaw"], pl["pitch"] if pitch is None else pitch)
    x = unit(np.cross(up, f))
    return Location(Plane(origin=tuple(hinge_point(name)), x_dir=tuple(x), z_dir=tuple(f)))


def hinge_point(name):
    """The middle of the eyelet pin: the lens is at PLACES[name]["lens"] at the set tilt."""
    import numpy as np
    pl = PLACES[name]
    up = axes(name)[0]
    f = look(name, pl["yaw"], pl["pitch"])
    x = unit(np.cross(up, f))
    y = np.cross(f, x)
    return np.array(pl["lens"]) - (LENS[0] * x + LENS[1] * y + BODY[-1][0] * f)


def hinge_loc(name):
    return cam_loc(name, 0.0)


def c270():
    """The camera body with its eyelet, in camera coordinates: (body, lens, cable stub)."""
    secs = [Pos(BODY_X, 0, z) * RectangleRounded(w, h, 0.42 * h) for z, w, h in BODY]
    b = loft(secs)
    ew, eh, eb, hole = EYELET
    b = b - Rot(0, 90, 0) * Cylinder(BACK_GROOVE_R, 30.0)
    b = b + Pos(0, 0, (BODY_BACK_Z + 0.5 - eb) / 2) * Box(ew, eh, BODY_BACK_Z + 0.5 + eb)
    b = b - Box(ew + 2, hole, hole)
    lx, ly, ld = LENS
    zf = BODY[-1][0]
    b = b - Pos(lx, ly, zf - 1.0) * Cylinder(ld / 2, 2.2)
    lens = Pos(lx, ly, zf - 1.6) * Cylinder(3.5, 0.6)
    cx, cy, cd, sd = CABLE
    stub = Pos(cx, cy, BODY_BACK_Z - 4.0) * Cylinder(sd / 2, 8.0)
    return b, lens, stub


def pitches():
    """The tilts for the checks: the range in 2.5 degree steps, and the set tilt of each printer."""
    lo, hi = PITCH_RANGE
    n = int(round((hi - lo) / 2.5))
    return sorted({lo + (hi - lo) * i / n for i in range(n + 1)} | {pl["pitch"] for pl in PLACES.values()})


def clear_of_camera(s):
    """The shape s (hinge coordinates) less the camera body and its cable end at each tilt."""
    b, _, stub = c270()
    whole = b + stub
    for p in pitches():
        s = s - Rot(p, 0, 0) * whole
    return s


def box(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(abs(x1 - x0), abs(y1 - y0), abs(z1 - z0))


def teardrop(r, length, origin, axis, tip):
    """A round hole of radius r along axis (centered on origin, length long), with a 45 degree point
    toward tip, so its top prints with no support when the hole lies on its side."""
    import numpy as np
    from build123d import Circle, Polygon, extrude
    a, t = unit(axis), unit(tip)
    t = unit(t - np.dot(t, a) * a)
    pl = Plane(origin=tuple(origin), x_dir=tuple(np.cross(t, a)), z_dir=tuple(a))
    q = r / math.sqrt(2)
    prof = Circle(r) + Polygon((-q, q), (q, q), (0, r * math.sqrt(2)), align=None)
    return extrude(pl * prof, amount=length / 2, both=True)


def counterbored(origin, out_dir, tip, t):
    """A counterbored M3 hole through a wall of thickness t, its outer face at origin."""
    import numpy as np
    o, d = np.asarray(origin, float), unit(out_dir)
    hole = teardrop(1.7, 40.0, o - d * 20.0, d, tip)
    cb = teardrop(COUNTERBORE[0] / 2, 20.0, o + d * (10.0 - (t - COUNTERBORE[1])), d, tip)
    return hole + cb


# ---------------------------------------------------------------- the arm
def post_bottom(name):
    """The hinge y of the bottom of the post."""
    import numpy as np
    return POST[name]["bottom"] - float(np.dot(hinge_point(name), axes(name)[0]))


def clamp_bottom(name):
    import numpy as np
    return CLAMP_BOTTOM[name] - float(np.dot(hinge_point(name), axes(name)[0]))


def arm_hinge(name):
    """The arm in hinge coordinates. It prints as it is here: z up, on its face z = ARM_Z[0]."""
    import numpy as np
    g = EYELET[0] / 2 + LUG_GAP
    xp = POST[name]["x"]
    z0, z1 = ARM_Z
    yb = post_bottom(name)
    xa, xb = -g - LUG_THIN, g + LUG_THICK
    lo, hi = min(xa, xp - POST_W / 2), max(xb, xp + POST_W / 2)
    s = box(lo, hi, BEAM_Y[0], BEAM_Y[1], z0, z1)
    s = s + box(xp - POST_W / 2, xp + POST_W / 2, yb, BEAM_Y[1], z0, z1)
    for x0, x1 in ((xa, -g), (g, xb)):
        s = s + box(x0, x1, LUG_Y[0], LUG_Y[1], z0, LUG_TOP_Z)
    s = clear_of_camera(s)
    s = s - teardrop(SCREW_HOLE / 2, LUG_THIN + 2.0, ((xa - g) / 2, 0, 0), (1, 0, 0), (0, 0, 1))
    s = s - teardrop(PILOT / 2, LUG_THICK + 2.0, ((g + xb) / 2, 0, 0), (1, 0, 0), (0, 0, 1))
    for d in JOINT["screws"]:
        s = s - Pos(xp, yb + d, (z0 + z1) / 2) * Cylinder(1.7, z1 - z0 + 2.0)
    return s


def arm(name):
    return hinge_loc(name) * arm_hinge(name)


# ---------------------------------------------------------------- the clamps
def tab_hinge(name):
    """The tab, the ribs, and the floor of the channel for the post, in hinge coordinates, on the
    camera side of the arm (hinge z over ARM_Z[1]): boxes on the hinge axes, so they stand straight
    up."""
    xp = POST[name]["x"]
    z1 = ARM_Z[1]
    y0 = clamp_bottom(name)
    yb = post_bottom(name)
    y1 = yb + JOINT["length"]
    t, rib, rd = JOINT["tab"], JOINT["rib"], JOINT["rib_depth"]
    w = POST_W / 2
    s = box(xp - w - rib, xp + w + rib, y0, y1, z1, z1 + t)
    s = s + box(xp - w - rib, xp - w - 0.2, y0, y1, z1 - rd, z1 + 0.01)
    s = s + box(xp + w + 0.2, xp + w + rib, y0, y1, z1 - rd, z1 + 0.01)
    if yb - y0 > 0.5:
        s = s + box(xp - w - 0.2, xp + w + 0.2, y0, yb - 0.2, z1 - rd, z1 + 0.01)
    return s


def tab_holes_hinge(name):
    """The holes of the heat-set inserts in the tab, from its face on the post, with teardrop tops."""
    xp = POST[name]["x"]
    z1 = ARM_Z[1]
    yb = post_bottom(name)
    r, dep = JOINT["insert"]
    out = None
    for d in JOINT["screws"]:
        h = teardrop(r, dep * 2, (xp, yb + d, z1), (0, 0, 1), (0, 1, 0))
        out = h if out is None else out + h
    return out


def joint_screws(name):
    xp = POST[name]["x"]
    z0, z1 = ARM_Z
    yb = post_bottom(name)
    out = None
    for d in JOINT["screws"]:
        sc = P.screw(3, JOINT_SCREW, (xp, yb + d, z0), (0, 0, -1))
        out = sc if out is None else out + sc
    return hinge_loc(name) * out


def joint_inserts(name):
    xp = POST[name]["x"]
    z1 = ARM_Z[1]
    yb = post_bottom(name)
    r, dep = JOINT["insert"]
    out = None
    for d in JOINT["screws"]:
        ins = Pos(xp, yb + d, z1 + 2.0) * (Cylinder(r + 0.3, 4.0) - Cylinder(1.5, 5.0))
        out = ins if out is None else out + ins
    return hinge_loc(name) * out


def connector(a, b, z0, z1, up):
    """A wall standing straight up from height z0 to z1 (along up), along the level line from a to b."""
    import numpy as np
    a, b, up = np.asarray(a, float), np.asarray(b, float), unit(up)
    a = a - np.dot(a, up) * up
    b = b - np.dot(b, up) * up
    d = b - a
    L = float(np.linalg.norm(d))
    o = (a + b) / 2 + up * (z0 + z1) / 2
    return Location(Plane(origin=tuple(o), x_dir=tuple(d / L), z_dir=tuple(up))) * Box(L + CONNECTOR_T, CONNECTOR_T, z1 - z0)


def behind_tab(name):
    """The space of the arm and behind it (hinge z under the front face of the arm): the connector
    stays out of it."""
    return hinge_loc(name) * box(-500, 500, -500, 500, -500, ARM_Z[1] + 0.5)


def tab_point(name):
    """The middle of the tab, in printer coordinates."""
    import numpy as np
    H = hinge_loc(name)
    return np.array(tuple((H * Pos(POST[name]["x"], 0, ARM_Z[1] + JOINT["tab"] / 2)).position))


def taz_clamp():
    """The TAZ saddle: two legs on the front and right faces of the front right upright, a key in
    each slot, an M3 x 8 screw through each leg into a roll-in T-nut, and the connector to the tab."""
    import numpy as np
    u = TAZ_UPRIGHT
    y0, y1, y2 = TAZ_SADDLE_Y
    t = TAZ_SADDLE_T
    fx, rz = u["front"], u["right"]
    s = box(fx, fx + t, y0, y1, rz - t, u["left"] - 1.0) + box(u["back"] + 2.0, fx + t, y0, y2, rz - t, rz)
    kw, kd = SLOT_KEY
    s = s + box(fx - kd, fx + 0.01, y0, y1, u["front_slot"] - kw / 2, u["front_slot"] + kw / 2)
    s = s + box(u["right_slot"] - kw / 2, u["right_slot"] + kw / 2, y0, y2, rz - 0.01, rz + kd)
    top = POST["taz"]["bottom"] + JOINT["length"]
    s = s + (connector((fx + t - CONNECTOR_T / 2, 0, rz - t + CONNECTOR_T / 2), tab_point("taz"), y0, top, (0, 1, 0))
             - behind_tab("taz") - box(u["back"] - 5, fx, 0, 1000, rz, u["left"] + 5)
             - box(fx - 1.0, fx + 4.5, TAZ_CORNER_PLATE_Y - 1.0, 1000, rz - 1.0, rz + 80.0))
    s = s + hinge_loc("taz") * tab_hinge("taz")
    s = s - hinge_loc("taz") * tab_holes_hinge("taz")
    for o, d in taz_screw_places():
        s = s - counterbored(o, d, (0, 1, 0), t)
    return s


def taz_screw_places():
    """(head face point, direction out of the part) of the two saddle screws."""
    u = TAZ_UPRIGHT
    y0, y1, y2 = TAZ_SADDLE_Y
    t = TAZ_SADDLE_T
    return [((u["front"] + t, (y0 + y1) / 2, u["front_slot"]), (1, 0, 0)),
            ((u["right_slot"], (y0 + y2) / 2, u["right"] - t), (0, 0, -1))]


def taz_screws():
    import numpy as np
    out = None
    for o, d in taz_screw_places():
        head = tuple(np.array(o) - (TAZ_SADDLE_T - COUNTERBORE[1]) * np.array(d))
        sc = P.screw(3, TNUT_SCREW, head, d)
        out = sc if out is None else out + sc
    return out


def taz_tnuts():
    """The roll-in T-nuts in the two slots: 10 across the slot, 6 along it, 4 thick, their top 1.8 mm
    behind the face (the same as the T-nut of the rail clips)."""
    u = TAZ_UPRIGHT
    (_, ya, _), _ = taz_screw_places()[0]
    (_, yb, _), _ = taz_screw_places()[1]
    return (box(u["front"] - 5.8, u["front"] - 1.8, ya - 3, ya + 3, u["front_slot"] - 5, u["front_slot"] + 5)
            + box(u["right_slot"] - 5, u["right_slot"] + 5, yb - 3, yb + 3, u["right"] + 1.8, u["right"] + 5.8))


def mini_clamp():
    """The Mini 2 plate: on the right face of the front right corner, two frame screws through it,
    a lip over the top of the frame, and the connector to the tab."""
    from build123d import Polygon, extrude
    m = MINI_PLATE
    t, (y0, y1), (z0, z1) = m["t"], m["y"], m["z"]
    x = MINI_FACE_X
    s = box(x, x + t, y0, y1, z0, z1 + 6.0)
    lb = m["lip"]
    prof = Polygon((x + t, z1), (x + t, z1 + lb + 2.0), (x - lb, z1 + lb + 2.0), (x - lb, z1 + lb), (x, z1), align=None)
    s = s + extrude(Plane(origin=(0, y1, 0), x_dir=(1, 0, 0), z_dir=(0, -1, 0)) * prof, amount=y1 - y0)
    top = POST["mini2"]["bottom"] + JOINT["length"]
    s = s + (connector((x + t - CONNECTOR_T / 2, (y0 + y1) / 2 - 4.0, 0), tab_point("mini2"), z0, top, (0, 0, 1))
             - behind_tab("mini2") - box(x - 100, x, -400, 0, 0, 1000))
    s = s + hinge_loc("mini2") * tab_hinge("mini2")
    s = s - hinge_loc("mini2") * tab_holes_hinge("mini2")
    for y, z in MINI_SCREWS:
        s = s - counterbored((x + t, y, z), (1, 0, 0), (0, 0, 1), t)
    return s


def mini_screws():
    x = MINI_FACE_X + MINI_PLATE["t"] - COUNTERBORE[1]
    out = None
    for y, z in MINI_SCREWS:
        sc = P.screw(3, MINI_SCREW_LEN, (x, y, z), (1, 0, 0))
        out = sc if out is None else out + sc
    return out


CLAMPS = {"taz": taz_clamp, "mini2": mini_clamp}


def hinge_screw(name):
    """The hinge screw in printer coordinates: the head on the thin lug, the shank through the eyelet."""
    g = EYELET[0] / 2 + LUG_GAP
    size, length = SCREW
    hd, hh = P.SCREW[size]
    x = -g - LUG_THIN
    s = Pos(x - hh / 2, 0, 0) * Rot(0, 90, 0) * Cylinder(hd / 2, hh) \
        + Pos(x + length / 2, 0, 0) * Rot(0, 90, 0) * Cylinder(size / 2 - 0.05, length)
    return hinge_loc(name) * s


# The printed parts: (file name, shape in printer coordinates, the rotation to its print position).
def printed(name):
    import numpy as np
    H = hinge_loc(name)
    # The arm prints as it is in hinge coordinates. The clamp stands on its bottom: the TAZ y is up.
    clamp_rot = Rot(90, 0, 0) if name == "taz" else Rot(0, 0, 0)
    return [(f"camera-arm-{name}", arm(name), H.inverse()), (f"camera-clamp-{name}", CLAMPS[name](), clamp_rot)]


def parts(name):
    """(name, shape, color) of the camera and its mount on the printer name, in printer coordinates."""
    L = cam_loc(name)
    b, lens, stub = c270()
    out = [("camera_arm", arm(name), ORANGE), ("camera_clamp", CLAMPS[name](), ORANGE),
           ("camera_screw", hinge_screw(name), P.SCREW_COLOR), ("camera_joint_screws", joint_screws(name), P.SCREW_COLOR),
           ("camera_inserts", joint_inserts(name), P.INSERT_COLOR),
           ("camera_body", L * b, BLACK), ("camera_lens", L * lens, "#3A4A66"), ("camera_cable_end", L * stub, BLACK)]
    if name == "taz":
        out += [("camera_base_screws", taz_screws(), P.SCREW_COLOR), ("camera_tnuts", taz_tnuts(), "#9AA0A8")]
    else:
        out += [("camera_base_screws", mini_screws(), P.SCREW_COLOR)]
    return out


# ---------------------------------------------------------------- the stiffness
E_PETG = 2000.0                # MPa: a low value for printed PETG
CAMERA_MASS = 0.075            # kg (Logitech; with no clip it is less)


def stiffness(name):
    """A beam model of the arm and the clamp, solid sections: the side stiffness at the camera (N/mm),
    the first sway frequency (Hz) with the camera and half the arm moving, and the sag of the camera
    under its weight (mm). The post and the connector bend as cantilevers in series; the beam bends
    under the weight."""
    import numpy as np
    I_arm = POST_W * (BEAM_Y[1] - BEAM_Y[0]) ** 3 / 12.0
    Lp = max(1.0, BEAM_Y[0] - (post_bottom(name) + JOINT["length"]))
    Lb = abs(POST[name]["x"])
    k_post = 3 * E_PETG * I_arm / Lp ** 3
    a = np.array(CLAMPS_ANCHOR[name]())
    b = tab_point(name)
    up = axes(name)[0]
    Lc = float(np.linalg.norm((b - a) - np.dot(b - a, up) * up))
    h = POST[name]["bottom"] + JOINT["length"] - CLAMP_BOTTOM[name]
    k_conn = 3 * E_PETG * (h * CONNECTOR_T ** 3 / 12.0) / Lc ** 3
    k_side = 1.0 / (1.0 / k_post + 1.0 / k_conn)
    m = CAMERA_MASS + 0.5 * arm(name).volume * 1.27e-6
    f = math.sqrt(k_side * 1000.0 / m) / (2 * math.pi)
    sag = CAMERA_MASS * 9.81 * Lb ** 3 / (3 * E_PETG * I_arm)
    return k_side, f, sag


CLAMPS_ANCHOR = {"taz": lambda: (TAZ_UPRIGHT["front"] + TAZ_SADDLE_T - CONNECTOR_T / 2, 0.0,
                                 TAZ_UPRIGHT["right"] - TAZ_SADDLE_T + CONNECTOR_T / 2),
                 "mini2": lambda: (MINI_FACE_X + MINI_PLATE["t"] - CONNECTOR_T / 2, sum(MINI_PLATE["y"]) / 2 - 4.0, 0.0)}


# ---------------------------------------------------------------- the print check
def print_check(mesh):
    """The overhangs of a mesh in its print position (z up, on the bed at its lowest point):
    (the area of faces that face down more steeply than 45 degrees, the widest flat ceiling)."""
    import numpy as np
    import trimesh
    nz = mesh.face_normals[:, 2]
    zc = mesh.triangles_center[:, 2]
    above = zc > mesh.bounds[0][2] + 0.05
    steep = above & (nz < -math.cos(math.radians(45)) - 0.01) & (nz >= -0.999)
    flat = above & (nz < -0.999)
    widest = 0.0
    if flat.any():
        idx = np.nonzero(flat)[0]
        for g in trimesh.graph.connected_components(mesh.face_adjacency[np.all(np.isin(mesh.face_adjacency, idx), axis=1)],
                                                   nodes=idx):
            pts = mesh.triangles[list(g)].reshape(-1, 3)[:, :2]
            pts = pts - pts.mean(axis=0)
            w = np.linalg.svd(pts, full_matrices=False)[2]
            q = pts @ w.T
            widest = max(widest, float((q.max(axis=0) - q.min(axis=0)).min()))
    return float(mesh.area_faces[steep].sum()), widest


# ---------------------------------------------------------------- the USB cable to the deck
CABLE_LOOP = 25.0              # the loop behind the camera: the cable bends there when the tilt changes


def plug_pod():
    """The USB-A plug of the camera in the lower USB 3 port of the Pi, from above (as the printer
    plug in the USB 2 port), in pod coordinates: (shape, the point where the cable leaves it)."""
    x0, x1, y0, y1, z0, z1 = P.pi_parts()["pi_usb3"]
    b = P.pi_box(x0, x1, y0, y1, z0, z0 + 7.5).bounding_box()
    face = Pos((b.min.X + b.max.X) / 2, b.max.Y, (b.min.Z + b.max.Z) / 2) * Rot(0, 0, -90)
    return face * P.cable_plug(*P.USBA_PLUG), ((b.min.X + b.max.X) / 2, b.max.Y + 30.0, (b.min.Z + b.max.Z) / 2)


def pod_loc(name):
    return P.POD_LOC if name == "taz" else P.MINI_LOC


def plug(name):
    """The plug and the end of its cable, in printer coordinates."""
    import numpy as np
    sh, top = plug_pod()
    T = P.matrix(pod_loc(name))
    return pod_loc(name) * sh, (T @ np.array([*top, 1.0]))[:3]


def cable_clips(name):
    """The cable clips (clips.py) and their places: (shape in printer coordinates, how many)."""
    import clips as K
    one, _ = K.tape_clip()
    if name == "taz":
        # Tape clips on the front face of the top front rail of the frame, between the corner plates,
        # and one on the front left upright over the deck, under its corner plate.
        f = TAZ_UPRIGHT["front"] + K.TAPE
        out = [Location(Plane(origin=(f, 476.0, z), x_dir=(0, 1, 0), z_dir=(1, 0, 0))) for z in (-560.0, -400.0, -240.0)]
        out.append(Location(Plane(origin=(f, 380.0, S_RAIL_SLOT_Z), x_dir=(0, 0, -1), z_dir=(1, 0, 0))))
    else:
        # Tape clips on the top of the frame along its front edge, over the stock LCD.
        out = [Location(Plane(origin=(x, -244.0, MINI_TOP_Z + K.TAPE), x_dir=(0, -1, 0), z_dir=(0, 0, 1)))
               for x in (-50.0, -165.0, -280.0)]
    s = None
    for loc in out:
        c = loc * one
        s = c if s is None else s + c
    return s, len(out)


import shape as _S                                    # noqa: E402
S_RAIL_SLOT_Z = _S.RAIL_SLOT_Z


def cable_points(name):
    """The route of the camera cable in printer coordinates: out of the back of the camera, a loop
    over the lugs, along the back of the beam and down the post to the clamp, along the frame in the
    clips, and down into the plug on the deck."""
    import numpy as np
    import clips as K
    L = cam_loc(name)
    H = hinge_loc(name)
    cx, cy, cd, sd = CABLE
    p = lambda loc, x, y, z: np.array(tuple((loc * Pos(x, y, z)).position))
    _, top = plug(name)
    xp, z0 = POST[name]["x"], ARM_Z[0]
    yb = post_bottom(name)
    pts = [p(L, cx, cy, BODY_BACK_Z - 8.0), p(L, cx, cy, BODY_BACK_Z - 14.0), p(H, cx, CABLE_LOOP, -12.0),
           p(H, cx, CABLE_LOOP, z0 - 4.0), p(H, xp, CABLE_LOOP, z0 - 4.0), p(H, xp, yb + 4.0, z0 - 4.0)]
    if name == "taz":
        f = TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
        pts += [np.array([100.0, 470.0, -690.0]), np.array([f, 476.0, -600.0]), np.array([f, 476.0, -200.0]),
                np.array([f, 440.0, S_RAIL_SLOT_Z]), np.array([f, 380.0, S_RAIL_SLOT_Z]),
                np.array([top[0], top[1] + 60.0, top[2]]), np.array([top[0], top[1] + 15.0, top[2]]), top]
    else:
        zt = MINI_TOP_Z + K.TAPE_CABLE_H
        pts += [np.array([12.0, -246.0, zt + 14.0]), np.array([-30.0, -244.0, zt]), np.array([-300.0, -244.0, zt]),
                np.array([top[0] + 25.0, -260.0, top[2] + 30.0]), np.array([top[0], top[1], top[2] + 15.0]), top]
    return pts


def cable_mesh(name):
    m, path = P.cable_mesh(cable_points(name), CABLE[2])
    return m, P.length(path)


# ---------------------------------------------------------------- the bed in the picture
def half_fov(diag=None, aspect=None):
    """(across, up) half angles in degrees."""
    diag = FOV_DIAG if diag is None else diag
    aspect = ASPECT if aspect is None else aspect
    t = math.tan(math.radians(diag / 2))
    d = math.hypot(aspect, 1.0)
    return math.degrees(math.atan(t * aspect / d)), math.degrees(math.atan(t / d))


def bed_points(name, n=41, height=0.0):
    import numpy as np
    c, a, b, size, _ = BEDS[name]
    g = np.linspace(-size / 2, size / 2, n)
    up = axes(name)[0]
    return np.array([np.array(c) + i * np.array(a) + j * np.array(b) + height * up for i in g for j in g])


def lens_point(name, pitch=None):
    import numpy as np
    return np.array(tuple((cam_loc(name, pitch) * Pos(LENS[0], LENS[1], BODY[-1][0])).position))


def in_frame(name, lens, d, pts, diag=None, aspect=None):
    """For each point: True where it is in the picture of a camera at lens, looking along d, level."""
    import numpy as np
    up = axes(name)[0]
    f = unit(d)
    r = unit(np.cross(f, up))
    u = np.cross(r, f)
    q = pts - np.asarray(lens, float)
    z = q @ f
    hx, hy = (math.tan(math.radians(a)) for a in half_fov(diag, aspect))
    return (z > 0) & (np.abs(q @ r) <= hx * z) & (np.abs(q @ u) <= hy * z)


def best_aim(name, lens, diag=None, aspect=None):
    """The aim that puts the most of the print area in the picture; of the aims within 1 % of the
    most, the one nearest the view of live.py. Returns (part in the picture, yaw, pitch)."""
    import numpy as np
    pts = bed_points(name, n=31)
    iso = look(name, ISO_YAW, ISO_PITCH)
    res = []
    for yaw in np.arange(-90.0, 0.1, 2.0):
        for pitch in np.arange(20.0, 80.1, 2.0):
            res.append((float(np.mean(in_frame(name, lens, look(name, yaw, pitch), pts, diag, aspect))), yaw, pitch))
    top = max(r[0] for r in res)
    return min((r for r in res if r[0] >= top - 0.01), key=lambda r: angle_between(look(name, r[1], r[2]), iso))


def coverage(name, lens, yaw, pitch, diag=None, aspect=None):
    """(the print area in the picture, the same at half the print height) for a camera at lens."""
    import numpy as np
    d = look(name, yaw, pitch)
    return (float(np.mean(in_frame(name, lens, d, bed_points(name), diag, aspect))),
            float(np.mean(in_frame(name, lens, d, bed_points(name, n=21, height=BEDS[name][4] / 2), diag, aspect))))


# The parts of the printer models that do not move: they can hide the bed for good. The parts that
# move (the bed, the gantry, the tool head, the chains, the belts) hide it only for a while.
MOVING = ("x_axis", "aerostruder", "extruder_cap", "y_bed", "x_gantry", "x_carriage", "x_chain", "y_chain", "z_chain",
          "hibiscus_y_axis_01", "belts", "control_box_01")


def fixed_meshes(name):
    """The fixed parts of the printer, in printer coordinates (with the stock LCD on the Mini 2)."""
    import glob
    import numpy as np
    import trimesh
    import kitview as K
    d = os.path.join(HERE, "build", "workhorse" if name == "taz" else "mini2")
    out = []
    for f in sorted(glob.glob(os.path.join(d, "*.stl"))):
        if any(k in os.path.basename(f) for k in MOVING):
            continue
        m = trimesh.load(f, force="mesh")
        if name == "taz":
            m.apply_transform(np.linalg.inv(K.mount_matrix()))
        out.append(m)
    return out


def seen(name, lens, yaw, pitch, meshes=None, size=(384, 216)):
    """(the print area seen, the same at half height): in the picture, and not behind a fixed part of
    the printer. A depth render from the lens."""
    import numpy as np
    import pyrender
    import render as R
    sc = pyrender.Scene()
    for m in meshes if meshes is not None else fixed_meshes(name):
        sc.add(pyrender.Mesh.from_trimesh(m, smooth=False))
    hx, hy = half_fov()
    lens = np.asarray(lens, float)
    f = look(name, yaw, pitch)
    r = unit(np.cross(f, axes(name)[0]))
    u = np.cross(r, f)
    sc.add(pyrender.PerspectiveCamera(yfov=math.radians(2 * hy), aspectRatio=size[0] / size[1], znear=2.0, zfar=4000.0),
           pose=R.look_at(lens, lens + f * 100.0, u))
    rr = pyrender.OffscreenRenderer(*size)
    depth = rr.render(sc, flags=pyrender.RenderFlags.DEPTH_ONLY)
    rr.delete()
    out = []
    for pts in (bed_points(name, n=31), bed_points(name, n=21, height=BEDS[name][4] / 2)):
        q = pts - lens
        z = q @ f
        ix = size[0] / 2 + (q @ r) / z * (size[0] / 2) / math.tan(math.radians(hx))
        iy = size[1] / 2 - (q @ u) / z * (size[1] / 2) / math.tan(math.radians(hy))
        ins = (z > 0) & (ix >= 0) & (ix < size[0]) & (iy >= 0) & (iy < size[1])
        ok = np.zeros(len(pts), bool)
        for k in np.nonzero(ins)[0]:
            d = depth[int(iy[k]), int(ix[k])]
            ok[k] = d == 0 or d > z[k] - 4.0
        out.append(float(ok.mean()))
    return tuple(out)


def report():
    import numpy as np
    print(f"isometric view (live.py, key i): {abs(ISO_YAW):.1f} deg to the left of straight back, {ISO_PITCH:.1f} deg down")
    hx, hy = half_fov()
    print(f"C270: {FOV_DIAG:.0f} deg diagonal, {ASPECT:.2f}: {2 * hx:.1f} x {2 * hy:.1f} deg. Also checked: 60 deg, 4:3")
    print("place: the lens, the aim, the angle to the i view; the print area in the picture (and with 60 deg 4:3), "
          "seen past the fixed parts of the printer, and seen at half the print height")
    for name in ("taz", "mini2"):
        fixed = fixed_meshes(name)
        pl = PLACES[name]
        for k, lens in [("chosen", pl["lens"])] + list(CANDIDATES[name].items()):
            if k == "chosen":
                yw, pt = pl["yaw"], pl["pitch"]
            else:
                _, yw, pt = best_aim(name, lens)
            fa, _ = coverage(name, lens, yw, pt)
            fb, _ = coverage(name, lens, yw, pt, 60.0, 4 / 3)
            fs, fhs = seen(name, lens, yw, pt, fixed)
            off = angle_between(look(name, yw, pt), look(name, ISO_YAW, ISO_PITCH))
            print(f"{name} {k}: lens {tuple(round(v) for v in lens)}, {abs(yw):.0f} deg left, {pt:.0f} deg down, "
                  f"{off:.0f} deg from i; in the picture {fa * 100:.0f}% ({fb * 100:.0f}%), seen {fs * 100:.0f}%, "
                  f"half height {fhs * 100:.0f}%")


# ---------------------------------------------------------------- the checks, the meshes, and the views
def check(name):
    """The fit checks of the camera, the arm, and the clamp. Returns a list of problems (empty when it
    fits)."""
    bad = []
    a, c = arm(name), CLAMPS[name]()
    for n, sh in (("arm", a), ("clamp", c)):
        if not sh.is_valid:
            bad.append(f"the {n} is not a valid solid")
    v = (a & c).volume
    if v > 0.01:
        bad.append(f"arm and clamp: {v:.2f} mm3")
    b, _, stub = c270()
    H = hinge_loc(name)
    for p in pitches():
        cam = H * (Rot(p, 0, 0) * (b + stub))
        for n, sh in (("arm", a), ("clamp", c)):
            v = (cam & sh).volume
            if v > 0.01:
                bad.append(f"camera at {p:.0f} deg and the {n}: {v:.2f} mm3")
    v = (hinge_screw(name) & cam_loc(name) * b).volume
    if v > 0.01:
        bad.append(f"hinge screw and camera: {v:.2f} mm3")
    for n, sh in (("joint screws", joint_screws(name)),):
        v = (sh & cam_loc(name) * b).volume
        if v > 0.01:
            bad.append(f"{n} and camera: {v:.2f} mm3")
    return bad


def printer_gap(name, printer, T, n=8000):
    """The gaps to the printer (meshes in world coordinates; T: printer to world): (the camera over
    its tilt range, the arm). The clamp touches the frame on purpose. A gap over 30 mm prints as inf."""
    import trimesh
    from build123d import export_stl
    import tempfile
    b, _, stub = c270()
    H = hinge_loc(name)
    tmp = tempfile.mkdtemp()
    gaps = []
    for p in pitches():
        f = os.path.join(tmp, f"cam{p:.0f}.stl")
        export_stl(H * (Rot(p, 0, 0) * (b + stub)), f, tolerance=0.1, angular_tolerance=0.3)
        gaps.append(P.clearance([trimesh.load(f)], T, printer, n=n))
    f = os.path.join(tmp, "arm.stl")
    export_stl(arm(name), f, tolerance=0.1, angular_tolerance=0.3)
    m = trimesh.load(f)
    return min(gaps), P.clearance([m], T, printer, n=n)


def base_touch(name, printer, T, n=30000):
    """How far the frame goes into the clamp (mm, 0 when it does not): points on the printer near the
    clamp, inside the clamp."""
    import numpy as np
    import trimesh
    from build123d import export_stl
    import tempfile
    f = os.path.join(tempfile.mkdtemp(), "clamp.stl")
    export_stl(CLAMPS[name](), f, tolerance=0.02, angular_tolerance=0.1)
    bm = trimesh.load(f)
    bm.apply_transform(T)
    lo, hi = bm.bounds[0] - 1, bm.bounds[1] + 1
    worst = 0.0
    for m, _ in printer:
        if np.any(m.bounds[1] < lo) or np.any(m.bounds[0] > hi):
            continue
        tri = m.triangles
        keep = np.all((tri.max(axis=1) >= lo) & (tri.min(axis=1) <= hi), axis=1)
        if not keep.any():
            continue
        sub = m.submesh([np.nonzero(keep)[0]], append=True)
        pts = sub.sample(min(n, max(2000, int(sub.area * 4))))
        inside = bm.contains(pts)
        if inside.any():
            d = trimesh.proximity.ProximityQuery(bm).signed_distance(pts[inside])
            worst = max(worst, float(d.max()))
    return worst


def meshes(name, tmp):
    """The camera, the mount, the plug, the clips, and the cable as trimesh meshes in printer
    coordinates: {name: (mesh, color)}."""
    import trimesh
    from build123d import export_stl
    out = {}
    clips, n = cable_clips(name)
    for k, sh, c in parts(name) + [("camera_plug", plug(name)[0], "#151515"), ("camera_clips", clips, ORANGE)]:
        p = os.path.join(tmp, f"{name}_{k}.stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        out[k] = (trimesh.load(p, force="mesh"), c)
    m, length = cable_mesh(name)
    out["camera_cable"] = (m, "#151515")
    print(f"{name}: camera cable route {length:.0f} mm to the Pi on the deck (the cable is {CABLE_LENGTH:.0f} mm), "
          f"{n} cable clips")
    return out


def camera_pose(name, T):
    """The view of the camera for the live window: the eye, a point it looks at, the up direction, and
    the vertical field of view in degrees, moved by the 4 x 4 matrix T from printer coordinates."""
    import numpy as np
    pl = PLACES[name]
    f = look(name, pl["yaw"], pl["pitch"])
    up = np.cross(unit(np.cross(f, axes(name)[0])), f)
    eye = lens_point(name)
    mv = lambda p: [float(x) for x in (T @ np.append(p, 1.0))[:3]]
    return {"eye": mv(eye), "focal": mv(eye + 300.0 * f), "up": [float(x) for x in T[:3, :3] @ up],
            "fov": 2 * half_fov()[1]}


def camera_view(name, scene, T, path, size=(1280, 720)):
    """What the camera sees: a perspective render from the lens with the field of view of the C270.
    scene: (trimesh, color) in world coordinates; T: printer to world. The outline of the print area
    is orange. Returns (the print area in the picture, the part that no part of the printer hides)."""
    import numpy as np
    import pyrender
    import render as R
    from PIL import Image, ImageDraw
    sc = pyrender.Scene(bg_color=[0.93, 0.94, 0.96, 1.0], ambient_light=[0.35, 0.35, 0.35])
    for mesh, col in scene:
        mat = pyrender.MetallicRoughnessMaterial(baseColorFactor=R.hex_rgb(col) + [1.0], metallicFactor=0.0,
                                                 roughnessFactor=0.8, doubleSided=True)
        sc.add(pyrender.Mesh.from_trimesh(mesh, material=mat, smooth=False))
    pl = PLACES[name]
    Rw = T[:3, :3]
    lens = lens_point(name)
    eye = (T @ np.append(lens, 1.0))[:3]
    f = look(name, pl["yaw"], pl["pitch"])
    r = unit(np.cross(f, axes(name)[0]))
    u = np.cross(r, f)
    fw, uw = Rw @ f, Rw @ u
    pose = R.look_at(eye + fw * 1.0, eye + fw * 100.0, uw)
    hx, hy = half_fov()
    sc.add(pyrender.PerspectiveCamera(yfov=math.radians(2 * hy), aspectRatio=size[0] / size[1], znear=2.0, zfar=4000.0),
           pose=pose)
    sc.add(pyrender.DirectionalLight(intensity=2.5), pose=R.look_at(eye, eye + fw * 100 - uw * 40, uw))
    sc.add(pyrender.DirectionalLight(intensity=1.0), pose=pose)
    rr = pyrender.OffscreenRenderer(*size)
    rgb, depth = rr.render(sc)
    rr.delete()
    img = Image.fromarray(rgb)
    n = 61
    pts = bed_points(name, n=n)
    q = pts - lens
    z = q @ f
    ix = size[0] / 2 + (q @ r) / z * (size[0] / 2) / math.tan(math.radians(hx))
    iy = size[1] / 2 - (q @ u) / z * (size[1] / 2) / math.tan(math.radians(hy))
    inside = (z > 0) & (ix >= 0) & (ix < size[0]) & (iy >= 0) & (iy < size[1])
    seen = np.zeros(len(pts), bool)
    for k in np.nonzero(inside)[0]:
        d = depth[int(iy[k]), int(ix[k])]
        seen[k] = d == 0 or d > z[k] - 1.0 - 3.0
    draw = ImageDraw.Draw(img)
    edge = [(0, j) for j in range(n)] + [(i, n - 1) for i in range(n)] + [(n - 1, j) for j in range(n - 1, -1, -1)] + \
        [(i, 0) for i in range(n - 1, -1, -1)]
    draw.line([(ix[i * n + j], iy[i * n + j]) for i, j in edge if z[i * n + j] > 0], fill=(232, 118, 43), width=4)
    fin, fseen = float(inside.mean()), float(seen.mean())
    draw.rectangle([0, 0, size[0], 40], fill=(255, 255, 255))
    draw.text((12, 12), f"{name}: C270 view, {abs(pl['yaw']):.0f} deg left of straight back, {pl['pitch']:.0f} deg down, "
              f"{2 * hx:.0f} x {2 * hy:.0f} deg. Print area (orange): {fin * 100:.0f}% in the picture, {fseen * 100:.0f}% seen",
              fill=(0, 0, 0))
    os.makedirs(os.path.dirname(path), exist_ok=True)
    img.save(path)
    print(f"  wrote {path}")
    return fin, fseen


def views(name, printer, deck, pod_to_world, T, out):
    """The isometric view of live.py (key i) with the camera, the view of the camera, and both side
    by side. printer, deck: (trimesh, color) in world coordinates; T: printer to world."""
    import numpy as np
    import render as R
    from PIL import Image, ImageDraw
    printer = printer + stock_lcd(name, printer)
    Rp = pod_to_world[:3, :3]
    iso = os.path.join(out, f"{name}-iso.png")
    R.render(printer + deck, tuple(Rp @ unit(ISO_POD)), tuple(Rp @ np.array([0, 1.0, 0])), iso, size=(1280, 1280))
    cv = os.path.join(out, f"{name}-camera-view.png")
    res = camera_view(name, printer + deck, T, cv)
    a, b = Image.open(iso).convert("RGB").resize((720, 720)), Image.open(cv).convert("RGB")
    w = Image.new("RGB", (a.width + b.width + 20, max(a.height, b.height) + 40), (255, 255, 255))
    w.paste(a, (0, 40))
    w.paste(b, (a.width + 20, 40))
    ImageDraw.Draw(w).text((12, 12), f"{name}: the isometric view of live.py (left) and the view of the C270 (right)",
                           fill=(0, 0, 0))
    p = os.path.join(out, f"{name}-iso-and-camera.png")
    w.save(p)
    print(f"  wrote {p}")
    return res


def stock_lcd(name, printer):
    """The stock LCD of the Mini 2 (lcd_assem_01), when the printer meshes do not have it yet."""
    import numpy as np
    import trimesh
    if name != "mini2":
        return []
    m = trimesh.load(os.path.join(HERE, "build", "mini2", "lcd_assem_01.stl"), force="mesh")
    if any(np.allclose(p.bounds, m.bounds, atol=0.5) for p, _ in printer):
        return []
    return [(m, "#3A3B3E")]


def detail_views(out):
    """Close views of each mount on its printer corner, and each mount in its FDM print position."""
    import numpy as np
    import trimesh
    from build123d import export_stl
    import render as R
    os.makedirs(out, exist_ok=True)
    tmp = os.path.join(out, "mesh")
    os.makedirs(tmp, exist_ok=True)
    for name in ("taz", "mini2"):
        st = P.setups()[name]
        T = st["world"]
        items = []
        for k, sh, c in parts(name):
            p = os.path.join(tmp, f"detail_{name}_{k}.stl")
            export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
            m = trimesh.load(p, force="mesh")
            m.apply_transform(T)
            items.append((m, c))
        lo = np.min([m.bounds[0] for m, _ in items], axis=0) - 40
        hi = np.max([m.bounds[1] for m, _ in items], axis=0) + 40
        frame = []
        pr = st["printer"]()
        for m, c in pr + stock_lcd(name, pr):
            cm = crop(m, lo, hi)
            if cm is not None:
                frame.append((cm, c))
        up = tuple(T[:3, :3] @ axes(name)[0])
        Rw = T[:3, :3]
        _, back, right = axes(name)
        for d, tag in (((back - right) - 0.6 * axes(name)[0], "front-right"), ((back + right) - 0.4 * axes(name)[0], "front-left"),
                       ((-back - right) - 0.5 * axes(name)[0], "back-right")):
            R.render(frame + items, tuple(Rw @ d), up, os.path.join(out, f"{name}-mount-{tag}.png"), size=(1200, 1100))
        # Each printed part in its print position, on a bed plate.
        for fn, _, _ in printed(name):
            p = os.path.join(HERE, "stl", "pod", "fdm", f"{fn}_fdm.stl")
            if not os.path.exists(p):
                continue
            m = trimesh.load(p)
            (x0, y0, _), (x1, y1, _) = m.bounds
            bed = trimesh.creation.box((x1 - x0 + 30, y1 - y0 + 30, 1.0))
            bed.apply_translation(((x0 + x1) / 2, (y0 + y1) / 2, -0.5))
            R.render([(bed, "#C9CDD2"), (m, ORANGE)], (-0.55, 0.75, -0.6), (0, 0, 1), os.path.join(out, f"{fn}-print.png"),
                     size=(1200, 1000))


def crop(m, lo, hi):
    """The part of a mesh inside the box lo..hi, or None."""
    import numpy as np
    import trimesh
    if np.any(m.bounds[1] < lo) or np.any(m.bounds[0] > hi):
        return None
    for i in range(3):
        n = np.zeros(3)
        n[i] = 1
        m = trimesh.intersections.slice_mesh_plane(m, n, lo)
        if len(m.faces) == 0:
            return None
        m = trimesh.intersections.slice_mesh_plane(m, -n, hi)
        if len(m.faces) == 0:
            return None
    return m


def check_prints():
    """The print check of each webcam STL in stl/pod/fdm (pod.py export writes them)."""
    import trimesh
    for name in ("taz", "mini2"):
        for fn, _, _ in printed(name):
            p = os.path.join(HERE, "stl", "pod", "fdm", f"{fn}_fdm.stl")
            m = trimesh.load(p)
            steep, flat = print_check(m)
            ok = m.is_watertight and m.body_count == 1
            print(f"  {fn}_fdm.stl: {'one closed part' if ok else 'NOT CLEAN'}; faces under 45 deg {steep:.1f} mm2, "
                  f"the widest flat ceiling {flat:.1f} mm{'  OVERHANG' if steep > 1.0 or flat > 6.5 else ''}")


if __name__ == "__main__":
    report()
    for n in ("taz", "mini2"):
        print(n, "fit:", check(n) or "the camera turns through the range with no touch")
        k, f, sag = stiffness(n)
        print(f"{n} stiffness: {k:.1f} N/mm to the side at the camera, first sway about {f:.0f} Hz, "
              f"sag {sag:.3f} mm (beam model, PETG {E_PETG:.0f} MPa)")
    print("print check (supports off):")
    check_prints()
    if len(sys.argv) > 1:
        detail_views(sys.argv[1])
