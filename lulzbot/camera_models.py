"""Placeholder models of the cameras to buy, and placeholder brackets for them. Two Arducam B0561
16 MP autofocus cameras: one on the arm of the C270 mount, one straight down over the middle of the
bed. Arducam publishes no drawing of the B0561 that we have. The sizes in B0561 come from two
product photos: a front view with two official sizes (36 mm and 44 mm), and a perspective view.
Each value says where it comes from. Measure the camera with calipers when it arrives and replace
the values. ELP has the ELP-USB12MP01-L281 board for comparison only (guesses, not placed).

The B0561 is a black metal case with a raised front plate, a short lens barrel at the middle of the
face, and the autofocus block at the right of the barrel. A U bracket of sheet metal holds the case
on two pivot screws at the middle of its sides. The base of the U has a round center boss with a
hole for one screw (the thread is unknown). The JST B4B-ZR socket is on the back of the case, and the
1 m cable plugs straight in from the back.

Each placeholder bracket is a printed plate that the base of the U screws onto with one center
screw. The pivot of the U sets the tilt.
- On the arm (camera.py), in place of the C270: a link with a tongue on the hinge pin (where the
  eyelet of the C270 is), held level by the hinge screw, and a level plate in front of the arm. The
  camera hangs under the plate and tilts down on its pivot.
- Over the bed: a plate on the top rails and a wall that stands up from it. The base of the U is on
  the face of the wall, so the pivot is level, and the camera looks straight down at no tilt.
  - TAZ: an arm on the top face of the top front rail, out to the wall over the middle of the bed.
  - Mini 2: a plate on the top face of the top front rail, with the wall at its back edge.
The brackets are placeholders for the layout and the views: they are not a final design, and there
are no STL files of them.

Each camera has its own cable to its own USB-A port of the Pi in the pod (CAMERA_PORTS). cable_route()
follows the route of the C270 cable, with the two camera cables side by side in the clips.

Camera coordinates: the origin is the middle of the front face of the case (under the front plate),
z out of the lens along the optical axis, y up in the picture, x = y cross z. lens_point() is the
middle of the front of the lens barrel: the views of live.py and camera_preview.py start there (the
pinhole of camera_options.py). At 300 mm and more from the bed, the true optical center a few mm
behind it changes the views very little. Bracket coordinates: the origin is the middle of the bottom
face of the base, y from the base to the pivot, x along the pivot axis.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with shapely --with rtree --with pyrender --with PyOpenGL --with pillow python camera_models.py
It prints the fit of the camera on the arm over its tilt range, the gaps to the printers, the gap of
the top camera to the X axis at the full print height, and the cable routes.
"""

import math
import os

from build123d import Cylinder, Location, Plane, Pos, Rot

import camera as C
import pod as P

HERE = os.path.dirname(os.path.abspath(__file__))

# ---------------------------------------------------------------- the cameras
# "official": a size on the product photo with dimensions. "front photo": measured on that photo,
# scaled by the official 36 mm width (about 20.3 px/mm), about 0.5 mm. "perspective photo": estimated
# from the other product photo, about 2 mm.
B0561 = dict(
    case=(36.0, 36.0),                # official: the width; the front face is square on the front photo
    depth=12.0,                       # perspective photo: front face to back face, without the front plate
    plate=(24.0, 24.0, 2.0),          # front photo: the raised plate, centered on the face; perspective photo: 2 mm proud
    plate_holes=(3.0, 1.5),           # front photo: 4 small holes about 3 mm in from the corners; diameter a guess
    barrel=(15.0, 3.0),               # front photo: 15 mm across, centered on the face; perspective photo: 3 mm past the plate
    af=(4.5, 6.0, 4.0),               # front photo: the autofocus block right of the barrel, 4.5 wide, 6 tall, at the
                                      # height of the lens; perspective photo: 4 mm past the plate
    socket=(8.5, 4.0, (-6.0, -11.0)), # perspective photo: the JST B4B-ZR socket on the back face, width, height, middle
                                      # (x, y in camera coordinates); the place is about 2 mm
    plug=6.0,                         # guess: the ZHR-4 plug and its strain relief, straight back
    cable=(3.0, 10.0),                # guess: the cable of the plug, diameter and bend radius
    cable_length=1000.0,              # Arducam: the USB-A to JST B4B-ZR cable is 1 m
    # The U bracket.
    total=44.0,                       # official: the top of the case to the bottom of the base
    sheet=1.4,                        # front photo: the legs, just outside the sides of the case
    leg_gap=0.2,                      # front photo: the gap from the case side to the leg
    leg_w=10.0,                       # perspective photo: the width of a leg along the depth of the case
    screws=43.5,                      # front photo: the overall width over the pivot screw heads
    pivot=(0.0, -6.0),                # front photo: the pivot axis is at the case mid-height (y); perspective
                                      # photo: about the middle of the depth (z)
    base_d=36.0,                      # perspective photo: the round base, about the width of the case
    boss=(4.5, 2.0),                  # front photo: the center boss under the base, diameter; height a guess
    base_hole=4.0,                    # guess: the hole of the base screw; the thread is unknown (1/4"-20, M5, or M6)
)
ELP = dict(board=(38.0, 38.0, 1.6), holes=(34.0, 2.2), holder=(14.0, 14.0, 12.0), lens=(14.0, 17.0, 8.0))   # guesses

CAMERA_COLORS = {"case": "#1A1B1E", "lens": "#30343A", "bracket": "#3A3C40", "connector": "#F2F2EE",
                 "keepout": "#D9534F"}
BRACKET_COLOR = "#8E6CC8"               # placeholder printed brackets: violet, so they read as not final

# The link on the C270 arm (hinge coordinates of camera.py: the pin along x, z forward, at no tilt).
LINK_TONGUE = (C.EYELET[0], C.EYELET[1], C.EYELET[2])   # the width, height, and depth behind the pin of the C270 eyelet
LINK_PLATE = (44.0, 4.0)                # the plate: width (x) and thickness (y); it goes from the lugs to past the base
LINK_Z = C.LUG_TOP_Z + 1.0              # the back edge of the plate, 1 mm in front of the lugs

# The top brackets. TAZ: the two top rails are 2020 at x -84 to -64 and 66 to 86, y 466 to 486
# (measured in the model, at the middle of the bed). Mini 2: the top front rail has its top face at
# z 373 and goes back to about y -180 (measured in the model).
TAZ_RAILS = dict(top=486.0, x=((-84.0, -64.0), (66.0, 86.0)))
MINI_RAIL_TOP = 373.0
BRACKET_T = 4.0                         # the printed plates and walls
BRACKET_W = 40.0                        # across the bed
# The lens over the bed. The base of the U (36 mm round, on the wall) is 18 mm under the pivot, and the
# pivot is 11 mm over the lens. TAZ: the X axis goes up to about 476 at the full print height, so the
# lens is at 490 and the base 7 mm over it. Mini 2: the base must clear the top of the rail.
TOP_LENS_UP = {"taz": 490.0, "mini2": 381.0}
# The full print height (published LulzBot sizes, not checked on the printers) and the moving parts
# that go up with the tool head.
PRINT_Z = {"taz": 285.0, "mini2": 180.0}
GANTRY = {"taz": ("x_axis", "aerostruder", "extruder_cap"),
          "mini2": ("x_gantry", "aerostruder", "x_carriage", "x_chain")}


def box(x0, x1, y0, y1, z0, z1):
    return C.box(x0, x1, y0, y1, z0, z1)


def unit(v):
    return C.unit(v)


# ---------------------------------------------------------------- the B0561 and its U bracket
def lens_point():
    """The middle of the front of the lens barrel, in camera coordinates."""
    return (0.0, 0.0, B0561["plate"][2] + B0561["barrel"][1])


def pivot_point():
    """The middle of the pivot axis, in camera coordinates."""
    return (0.0, *B0561["pivot"])


def pivot_height():
    """The pivot over the bottom face of the base (bracket y)."""
    return B0561["total"] - B0561["case"][1] / 2 + B0561["pivot"][0]


def case_model():
    """The case, the lens, the socket, and the keepout of the plug and the cable bend, in camera
    coordinates: {part: shape}."""
    d = B0561
    w, h = d["case"]
    dep = d["depth"]
    pw, ph, pt = d["plate"]
    case = box(-w / 2, w / 2, -h / 2, h / 2, -dep, 0.0) + box(-pw / 2, pw / 2, -ph / 2, ph / 2, 0.0, pt)
    hi, hd = d["plate_holes"]
    for sx in (-1, 1):
        for sy in (-1, 1):
            case = case - Pos(sx * (pw / 2 - hi), sy * (ph / 2 - hi), pt) * Cylinder(hd / 2, 1.0)
    bd, bl = d["barrel"]
    lens = Pos(0, 0, pt + bl / 2) * Cylinder(bd / 2, bl)
    aw, ah, al = d["af"]
    # The block is right of the barrel as you look at the camera: camera -x.
    lens = lens + box(-bd / 2 - aw + 0.5, -bd / 2 + 0.5, -ah / 2, ah / 2, pt, pt + al)
    sw, sh, (sx, sy) = d["socket"]
    sock = box(sx - sw / 2, sx + sw / 2, sy - sh / 2, sy + sh / 2, -dep - 0.3, -dep + 0.2)
    cd, R = d["cable"]
    z0 = -dep - d["plug"]
    keep = box(sx - sw / 2, sx + sw / 2, sy - sh / 2, sy + sh / 2, z0, -dep - 0.3)
    # The bend: the cable leaves the plug straight back, then goes down (camera -y), toward the near edge.
    keep = keep + box(sx - cd, sx + cd, sy - R - cd, sy + cd, z0 - R - cd, z0)
    return {"case": case, "lens": lens, "connector": sock, "keepout": keep}


def bracket_model():
    """The U bracket and the pivot screws, in bracket coordinates."""
    d = B0561
    t, g, lw = d["sheet"], d["leg_gap"], d["leg_w"]
    yp = pivot_height()
    xi = d["case"][0] / 2 + g
    xo = xi + t
    # The round base, the bottom of the U on it, and the two legs up to the pivot.
    s = Pos(0, t / 2, 0) * (Location(Plane.XZ) * Cylinder(d["base_d"] / 2, t))
    s = s + box(-xo, xo, t, 2 * t, -lw / 2, lw / 2)
    for sgn in (-1, 1):
        x0, x1 = sorted((sgn * xi, sgn * xo))
        s = s + box(x0, x1, t, yp + lw / 2, -lw / 2, lw / 2)
    bd, bh = d["boss"]
    s = s + Pos(0, -bh / 2, 0) * (Location(Plane.XZ) * Cylinder(bd / 2, bh))
    s = s - Location(Plane.XZ) * Cylinder(d["base_hole"] / 2, 20.0)
    head = (d["screws"] / 2 - xo)
    screws = None
    for sgn in (-1, 1):
        c = Pos(sgn * (xo + head / 2), yp, 0) * (Location(Plane.YZ) * Cylinder(2.5, head))
        screws = c if screws is None else screws + c
    return s, screws


def placed(lens, f, u, n):
    """The case and the bracket for a camera with its lens at lens (printer coordinates), looking
    along f, with u up in the picture, and the base of the U toward -n from the pivot (n: from the base
    to the pivot). Returns (the camera Location, the bracket Location)."""
    import numpy as np
    f, u, n = unit(f), unit(u), unit(n)
    x = np.cross(u, f)
    R = np.column_stack([x, u, f])
    origin = np.asarray(lens, float) - R @ np.array(lens_point())
    cam = Location(Plane(origin=tuple(origin), x_dir=tuple(x), z_dir=tuple(f)))
    pivot = origin + R @ np.array(pivot_point())
    bz = np.cross(x, n)
    br = Location(Plane(origin=tuple(pivot - pivot_height() * n), x_dir=tuple(x), z_dir=tuple(bz)))
    return cam, br


def camera_parts(cam, br, tag):
    """[(part, shape, color)] of a placed camera."""
    m = case_model()
    s, screws = bracket_model()
    out = [(f"camera_{tag}_{k}", cam * v, CAMERA_COLORS[k]) for k, v in m.items()]
    out += [(f"camera_{tag}_bracket", br * s, CAMERA_COLORS["bracket"]),
            (f"camera_{tag}_pivot_screws", br * screws, P.SCREW_COLOR)]
    return out


def base_point(br):
    import numpy as np
    return np.array(tuple(br.position))


# ---------------------------------------------------------------- the camera on the C270 arm
def arm_frame(name, pitch=None):
    """(camera Location, bracket Location) of the camera on the arm at the tilt pitch (the set tilt
    when None): hinge coordinates at no tilt, the base under the link plate."""
    import numpy as np
    H = C.hinge_loc(name)
    p = math.radians(C.PLACES[name]["pitch"] if pitch is None else pitch)
    pivot = np.array([0.0, -pivot_height(), link_base_z()])
    f = np.array([0.0, -math.sin(p), math.cos(p)])
    u = np.array([0.0, math.cos(p), math.sin(p)])
    lc = np.array(lens_point()) - np.array(pivot_point())
    lens_h = pivot + lc[0] * np.cross(u, f) + lc[1] * u + lc[2] * f
    # Hinge coordinates to printer coordinates.
    pt = lambda v: np.array(tuple((H * Pos(*v)).position))
    d = lambda v: pt(v) - pt((0.0, 0.0, 0.0))
    return placed(pt(lens_h), d(f), d(u), d((0.0, -1.0, 0.0)))


def link_base_z():
    """The middle of the base under the link plate (hinge z)."""
    return LINK_Z + B0561["base_d"] / 2 + 2.0


def link_hinge():
    """The link in hinge coordinates: the tongue between the lugs of the arm, with the hole of the M3
    hinge screw, and the level plate in front of the lugs with the hole of the base screw. The base of
    the U is under the plate (hinge y = 0)."""
    tw, th, tb = LINK_TONGUE
    pw, pt = LINK_PLATE
    zc = link_base_z()
    z1 = zc + B0561["base_d"] / 2 + 2.0
    s = box(-tw / 2, tw / 2, -th / 2, th / 2, -tb, LINK_Z + 0.5)
    s = s + box(-pw / 2, pw / 2, 0.0, pt, LINK_Z, z1)
    s = s + box(-tw / 2, tw / 2, -th / 2, pt, LINK_Z - 0.5, LINK_Z + 4.0)
    s = s - Location(Plane.YZ) * Cylinder(C.SCREW_HOLE / 2, tw + 2)
    s = s - Pos(0, pt / 2, zc) * (Location(Plane.XZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, pt + 2))
    return s


def arm_mount(name, pitch=None):
    """[(part, shape, color)] of the camera on the arm and the link, in printer coordinates."""
    cam, br = arm_frame(name, pitch)
    return [("camera_link", C.hinge_loc(name) * link_hinge(), BRACKET_COLOR)] + camera_parts(cam, br, "mount")


# ---------------------------------------------------------------- the camera over the bed
def top_frame(name):
    """(camera Location, bracket Location, the lens point) of the camera over the middle of the bed:
    straight down, the back of the printer up in the picture, the base of the U on a wall in front."""
    import numpy as np
    up, back, _ = C.axes(name)
    c = np.array(C.BEDS[name][0], float)
    lens = c + (TOP_LENS_UP[name] - float(np.dot(c, up))) * up
    cam, br = placed(lens, -up, back, back)
    return cam, br, lens


def top_bracket(name):
    """The printed bracket over the bed, in printer coordinates: the wall that the base screws onto,
    and the plate on the top front rail."""
    import numpy as np
    up, back, right = C.axes(name)
    cam, br, lens = top_frame(name)
    o = base_point(br)                         # the middle of the bottom face of the base, on the wall
    t, w = BRACKET_T, BRACKET_W
    if name == "taz":
        # x is to the front: the wall from x0 (the base) to x0 + t, from the rail top up; the arm on the
        # top face of the front rail out to the wall.
        x0, zc = float(o[0]), float(o[2])
        top = float(o[1]) + B0561["base_d"] / 2 + 4.0
        s = box(x0, x0 + t, TAZ_RAILS["top"], top, zc - w / 2, zc + w / 2)
        s = s + box(x0, TAZ_RAILS["x"][1][1], TAZ_RAILS["top"], TAZ_RAILS["top"] + t, zc - w / 2, zc + w / 2)
        s = s - Pos(x0 + t / 2, float(o[1]), zc) * (Location(Plane.YZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, t + 2))
        for z in (zc - 12.0, zc + 12.0):         # M3 into a roll-in T-nut in the top slot of the front rail
            s = s - Pos(76.0, TAZ_RAILS["top"] + t / 2, z) * (Location(Plane.XZ) * Cylinder(1.7, t + 2))
        return s
    # Mini 2: y is to the back. The wall from y0 - t to y0 (the base), on the plate on the rail top.
    y0, xc = float(o[1]), float(o[0])
    top = float(o[2]) + B0561["base_d"] / 2 + 4.0
    s = box(xc - w / 2, xc + w / 2, y0 - t, y0, MINI_RAIL_TOP, top)
    s = s + box(xc - w / 2, xc + w / 2, -250.0, y0, MINI_RAIL_TOP, MINI_RAIL_TOP + t)
    s = s - Pos(xc, y0 - t / 2, float(o[2])) * (Location(Plane.XZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, t + 2))
    for x in (xc - 12.0, xc + 12.0):             # M3 screws into the rail top
        s = s - Pos(x, -235.0, MINI_RAIL_TOP + t / 2) * Cylinder(1.7, t + 2)
    return s


def top_mount(name):
    cam, br, _ = top_frame(name)
    return [("camera_top_plate", top_bracket(name), BRACKET_COLOR)] + camera_parts(cam, br, "top")


def mount_lens(name):
    import numpy as np
    cam, _ = arm_frame(name)
    return np.array(tuple((cam * Pos(*lens_point())).position))


# ---------------------------------------------------------------- the cables
# Each camera has its own 1 m cable, from the JST plug at the camera to a USB-A plug in its own port of
# the Pi. The two USB-A stacks of the Pi 5 (pi_parts in deckview.py) each have a lower and an upper
# port; the printer cable is in the lower USB 2 port. The RP1 chip of the Pi 5 has two USB host
# controllers, each with one USB 3 port and one USB 2 port. Which physical ports share a controller is
# not checked here (not in the files we have). The cameras go to the lower USB 3 port and the upper USB
# 2 port: one in each stack, one lower and one upper. The camera on the arm is in the upper USB 2 port,
# so the two camera cables do not cross on their way to the Pi.
CAMERA_PORTS = {"mount": ("pi_usb2", True), "top": ("pi_usb3", False)}
# The USB-A end of the B0561 cable: the same sizes as the USB-A end of the printer cable (a common
# cable, not measured). 30 mm out of the port.
USBA_OUT = 30.0
# Where the two camera cables share the tape clips of camera.cable_clips, they lie side by side in a
# two-cable clip (clips.tape_clip(pair=True)): each 3.5 mm to one side of the middle of the clip.
SIDE = 3.5


def pi_plug(where):
    """The USB-A plug of the camera cable at where in its port, in pod coordinates: (shape, the point
    where the cable leaves the strain relief)."""
    stack, upper = CAMERA_PORTS[where]
    x0, x1, y0, y1, z0, z1 = P.pi_parts()[stack]
    zl = z1 - 7.5 if upper else z0
    b = P.pi_box(x0, x1, y0, y1, zl, zl + 7.5).bounding_box()
    c = ((b.min.X + b.max.X) / 2, b.max.Y, (b.min.Z + b.max.Z) / 2)
    face = Pos(*c) * Rot(0, 0, -90)
    return face * P.cable_plug(*P.USBA_PLUG), (c[0], c[1] + USBA_OUT, c[2])


def plug_printer(name, where):
    """The plug and the end of its cable, in printer coordinates."""
    import numpy as np
    sh, end = pi_plug(where)
    T = P.matrix(C.pod_loc(name))
    return C.pod_loc(name) * sh, (T @ np.array([*end, 1.0]))[:3]


def side_by_side(route, flags, n, off):
    """Move each flagged point of the route off to one side of the path, by off: the side is n (the
    face of the clips) cross the direction of the path between the flagged points (the clips)."""
    import numpy as np
    idx = [i for i, f in enumerate(flags) if f]
    out = list(route)
    for k, i in enumerate(idx):
        a, b = idx[max(k - 1, 0)], idx[min(k + 1, len(idx) - 1)]
        d = unit(route[b] - route[a])
        out[i] = route[i] + off * unit(np.cross(n, d))
    return out


def pi_tail(name, where):
    """The end of the route at the Pi, in printer coordinates, from the clips to the plug: straight out
    of the plug along the port (pod v). On the TAZ the printer cable turns over the USB 3 stack 28 mm
    out of its port, so the cable from the lower USB 3 port turns back (pod -w) under it first, into
    the lane of the upper ports. On the Mini 2 the printer cable turns the other way."""
    import numpy as np
    T = P.matrix(C.pod_loc(name))
    _, e = pi_plug(where)
    e = np.array(e)
    v, w = np.array([0.0, 1.0, 0.0]), np.array([0.0, 0.0, 1.0])
    if name == "taz" and CAMERA_PORTS[where] == ("pi_usb3", False):
        pts = [e + 60 * v - 9 * w, e + 22 * v - 9 * w, e + 16 * v, e]
    else:
        pts = [e + 60 * v, e + 20 * v, e]
    return [(T @ np.append(q, 1.0))[:3] for q in pts]


def cable_route(name, where):
    """The route of the cable of the camera at where ("mount" or "top") to its own USB port of the Pi,
    in printer coordinates: out of the plug, through the bend, along the route of the C270 cable
    (camera.cable_points) and its clips beside the other camera cable, and into its USB-A plug."""
    import numpy as np
    import clips as K
    d = B0561
    dep = d["depth"]
    _, _, (sx, sy) = d["socket"]
    cd, R = d["cable"]
    z0 = -dep - d["plug"]
    cam = arm_frame(name)[0] if where == "mount" else top_frame(name)[0]
    p = lambda x, y, z: np.array(tuple((cam * Pos(x, y, z)).position))
    start = [p(sx, sy, -dep), p(sx, sy, z0), p(sx, sy - R, z0 - R)]
    c270 = [np.asarray(q, float) for q in C.cable_points(name)]
    b = np.array(C.BEDS[name][0])
    if name == "taz":
        # The clips are on the front faces of the top front rail and the front left upright. The C270
        # route: ..., [100, 470, -690], the rail (3 points), the upright (2 points), then the plug.
        n = np.array([1.0, 0.0, 0.0])
        rail = c270[-6:-3]
        tail = pi_tail(name, where)
        if where == "mount":
            mid = c270[2:-7]
            rail = c270[-7:-6] + rail
            route = start + mid + rail + tail
            flags = [False] * (len(start) + len(mid)) + [True] * len(rail) + [False] * len(tail)
        else:
            f = C.TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
            o = base_point(top_frame(name)[1])
            top = float(o[1]) + B0561["base_d"] / 2 + 10.0
            mid = [np.array([float(o[0]) - 10.0, top, b[2]]), np.array([f, top, b[2]])]
            join = [np.array([f, 476.0, b[2] + 30.0])] + rail
            route = start + mid + join + tail
            flags = [False] * (len(start) + len(mid)) + [True] * len(join) + [False] * len(tail)
        # The cable over the bed comes down onto the rail from above: it is in the upper lane, and on the
        # left on the upright.
        off = SIDE if where == "mount" else -SIDE
        return side_by_side(route, flags, n, off)
    # Mini 2: the clips are on the top of the frame along its front edge. The C270 route: ..., the
    # clip line (2 points), then the plug.
    n = np.array([0.0, 0.0, 1.0])
    zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
    line = c270[-5:-3]
    tail = pi_tail(name, where)
    if where == "mount":
        mid = c270[2:-5]
        route = start + mid + line + tail
        flags = [False] * (len(start) + len(mid)) + [True] * len(line) + [False] * len(tail)
    else:
        o = base_point(top_frame(name)[1])
        top = float(o[2]) + B0561["base_d"] / 2 + 10.0
        mid = [np.array([b[0], float(o[1]) + 10.0, top]), np.array([b[0], -244.0, top])]
        join = [np.array([b[0] - 30.0, -244.0, zt]), line[-1]]
        # Along the back lane past the other camera cable, then forward into the tail.
        tail = [np.array([tail[0][0], line[-1][1] + SIDE, tail[0][2]])] + tail
        route = start + mid + join + tail
        flags = [False] * (len(start) + len(mid)) + [True] * len(join) + [False] * len(tail)
    # The cable over the bed comes onto the line of clips from the back: it is in the back lane.
    return side_by_side(route, flags, n, SIDE if where == "mount" else -SIDE)


def cable(name, where):
    """(the mesh of the cable, its length in mm)."""
    m, path = P.cable_mesh(cable_route(name, where), B0561["cable"][0])
    return m, P.length(path)


def check_pi_end(name):
    """The plugs at the Pi (the two camera plugs, the printer plug, the power plug) against each other
    and against the pod (the body, the frame, the mount) and the bracket, in pod coordinates. Returns a
    list of problems."""
    sh = {"camera plug (mount)": pi_plug("mount")[0], "camera plug (top)": pi_plug("top")[0]}
    for n, s, _ in P.hardware():
        if n in ("printer_plug", "power_plug"):
            sh[n.replace("_", " ")] = s
    br = (P.bracket if name == "taz" else P.mini_bracket)()
    others = {"pod body": P.body(), "pod frame": P.pod_frame(), "pod mount": P.mount(),
              "bracket": C.pod_loc(name).inverse() * br}
    bad = []
    names = list(sh)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            v = (sh[a] & sh[b]).volume
            if v > 0.01:
                bad.append(f"{a} and {b}: {v:.2f} mm3")
        for b, o in others.items():
            v = (sh[a] & o).volume
            if v > 0.01:
                bad.append(f"{a} and the {b}: {v:.2f} mm3")
    return bad


# ---------------------------------------------------------------- the views for the live window
def camera_views(name, T):
    """The views of the live window, moved by the 4 x 4 matrix T from printer coordinates: the camera
    on the arm ("camera"), the camera over the bed ("camera_top"), the bed, and T."""
    import numpy as np
    mv = lambda p: [float(x) for x in (T @ np.append(p, 1.0))[:3]]
    rot = lambda d: [float(x) for x in T[:3, :3] @ np.asarray(d, float)]
    pl = C.PLACES[name]
    f = C.look(name, pl["yaw"], pl["pitch"])
    up = np.cross(C.unit(np.cross(f, C.axes(name)[0])), f)
    eye = mount_lens(name)
    mount = {"eye": mv(eye), "focal": mv(eye + 300.0 * f), "up": rot(up), "fov": 2 * C.half_fov()[1]}
    _, _, lens = top_frame(name)
    _, back, _ = C.axes(name)
    down = -np.asarray(C.axes(name)[0], float)
    top = {"eye": mv(lens), "focal": mv(lens + 300.0 * down), "up": rot(back), "fov": 2 * C.half_fov()[1]}
    return {"camera": mount, "camera_top": top, "bed": C.bed_view(name, T),
            "printer_to_view": [[float(x) for x in row] for row in T]}


# The C270 parts that the camera on the arm replaces (the arm, the clamp, the screws, and the clips
# stay). The cameras have their own cables and USB-A plugs.
C270_PARTS = ("camera_body", "camera_lens", "camera_cable_end", "camera_cable", "camera_plug")


def meshes(name, tmp, cam=None):
    """The meshes of the live window: the C270 mount parts of camera.meshes (cam) less the C270 itself,
    and the two cameras with their brackets, in printer coordinates: {name: (mesh, color)}."""
    import trimesh
    from build123d import export_stl
    out = {k: v for k, v in (cam or {}).items() if k not in C270_PARTS}
    for k, sh, c in arm_mount(name) + top_mount(name):
        p = os.path.join(tmp, f"{name}_{k}.stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        out[k] = (trimesh.load(p, force="mesh"), c)
    # Dark red: the cable of the camera on the arm. Dark blue: the cable of the camera over the bed.
    out["camera_cable"] = (cable(name, "mount")[0], "#8B2A2A")
    out["camera_top_cable"] = (cable(name, "top")[0], "#2B4C7E")
    for where, k in (("mount", "camera_plug"), ("top", "camera_top_plug")):
        p = os.path.join(tmp, f"{name}_{k}.stl")
        export_stl(plug_printer(name, where)[0], p, tolerance=0.05, angular_tolerance=0.2)
        out[k] = (trimesh.load(p, force="mesh"), "#151515")
    return out


# ---------------------------------------------------------------- the checks
def check_arm(name):
    """The case and its keepout on the arm over the tilt range, against the U bracket, the link, the
    arm, the clamp, and the hinge screw. Returns a list of problems."""
    bad = []
    fixed = [("link", C.hinge_loc(name) * link_hinge()), ("arm", C.arm(name)), ("clamp", C.CLAMPS[name]()),
             ("hinge screw", C.hinge_screw(name))]
    _, br = arm_frame(name)
    u, _ = bracket_model()
    for k, sh in fixed[1:]:
        v = (sh & (br * u)).volume
        if v > 0.01:
            bad.append(f"the {k} and the U bracket: {v:.2f} mm3")
    fixed.append(("U bracket", br * u))
    for p in C.pitches():
        cam, _ = arm_frame(name, p)
        for k, sh in case_model().items():
            for n2, other in fixed:
                v = ((cam * sh) & other).volume
                if v > 0.01:
                    bad.append(f"the {k} at {p:.0f} deg and the {n2}: {v:.2f} mm3")
    return bad


def check_top(name):
    """The case and its keepout over the bed against the U bracket and the printed bracket."""
    bad = []
    cam, br, _ = top_frame(name)
    s, _ = bracket_model()
    for k, sh in case_model().items():
        for n2, other in (("U bracket", br * s), ("printed bracket", top_bracket(name))):
            v = ((cam * sh) & other).volume
            if v > 0.01:
                bad.append(f"the {k} and the {n2}: {v:.2f} mm3")
    return bad


def gantry_gap(name, solids):
    """The smallest vertical gap from the top of the moving parts at the full print height to the
    shapes, where they are over each other (mm), and the rise of the tool head."""
    import glob
    import numpy as np
    import trimesh
    import tempfile
    from build123d import export_stl
    import kitview as K
    up = np.asarray(C.axes(name)[0], float)
    d = os.path.join(HERE, "build", "workhorse" if name == "taz" else "mini2")
    parts = []
    for f in sorted(glob.glob(os.path.join(d, "*.stl"))):
        if any(g in os.path.basename(f) for g in GANTRY[name]):
            m = trimesh.load(f, force="mesh")
            if name == "taz":
                m.apply_transform(np.linalg.inv(K.mount_matrix()))
            parts.append((os.path.basename(f), m))
    nozzle = min(float((m.vertices @ up).min()) for n, m in parts if "aerostruder" in n)
    bed = float(np.dot(C.BEDS[name][0], up))
    rise = PRINT_Z[name] - (nozzle - bed)
    g = trimesh.util.concatenate([m for _, m in parts])
    g.apply_translation(up * rise)
    tmp = tempfile.mkdtemp()
    best = float("inf")
    for i, s in enumerate(solids):
        f = os.path.join(tmp, f"s{i}.stl")
        export_stl(s, f, tolerance=0.1, angular_tolerance=0.3)
        m = trimesh.load(f, force="mesh")
        pts = m.sample(6000)
        locs, idx, _ = g.ray.intersects_location(pts, np.tile(-up, (len(pts), 1)), multiple_hits=False)
        if len(locs):
            best = min(best, float(((pts[idx] - locs) @ up).min()))
    return best, rise


def report():
    import tempfile
    import numpy as np
    import trimesh
    from build123d import export_stl
    for name in ("taz", "mini2"):
        bad = check_arm(name)
        print(f"{name}: camera on the arm: " + ("; ".join(bad) if bad else "no touch over the tilt range"))
        bad = check_top(name)
        print(f"{name}: camera over the bed: " + ("; ".join(bad) if bad else "no touch with its brackets"))
        printer = P.setups()[name]["printer"]()
        printer = printer + C.stock_lcd(name, printer)
        tmp = tempfile.mkdtemp()
        for label, items in (("camera on the arm, its bracket, and the link", arm_mount(name)),
                             ("camera over the bed and its U bracket", top_mount(name)[1:])):
            ms = []
            for k, sh, _ in items:
                f = os.path.join(tmp, f"{k}.stl")
                export_stl(sh, f, tolerance=0.1, angular_tolerance=0.3)
                ms.append(trimesh.load(f, force="mesh"))
            gap = P.clearance(ms, P.setups()[name]["world"], printer, n=8000)
            print(f"{name}: {label}: {gap:.1f} mm from the printer")
        gap, rise = gantry_gap(name, [sh for _, sh, _ in top_mount(name)])
        _, _, lens = top_frame(name)
        h = float(np.dot(lens - np.array(C.BEDS[name][0]), C.axes(name)[0]))
        print(f"{name}: top lens {h:.1f} mm over the bed; at the full print height ({PRINT_Z[name]:.0f} mm, "
              f"the tool head {rise:.0f} mm up from the model) the moving parts are {gap:.1f} mm under the camera "
              f"and its brackets")
        print(f"{name}: arm lens at {np.round(mount_lens(name), 1).tolist()} (printer coordinates), "
              f"{np.linalg.norm(mount_lens(name) - C.lens_point(name)):.1f} mm from the C270 lens")
        bad = check_pi_end(name)
        print(f"{name}: plugs at the Pi: " + ("; ".join(bad) if bad else "no touch with each other, the pod, or the bracket"))
        for where in ("mount", "top"):
            _, n = cable(name, where)
            have = B0561["cable_length"]
            stack, upper = CAMERA_PORTS[where]
            print(f"{name}: {where} camera cable route {n:.0f} mm to the {'upper' if upper else 'lower'} {stack} port "
                  f"(the cable is {have:.0f} mm: "
                  f"{'long enough' if have >= n else f'{n - have:.0f} mm short'})")


if __name__ == "__main__":
    report()
