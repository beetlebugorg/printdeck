"""Placeholder models of the cameras to buy, and placeholder brackets for them. Two Arducam B0561
16 MP autofocus cameras: one on the arm of the C270 mount, one straight down over the middle of the
bed. Arducam publishes no drawing of the B0561 that we have. The sizes in B0561 come from two
product photos: a front view with two official sizes (36 mm and 44 mm), and a perspective view.
The comment on each value gives its source. Measure the camera with calipers when it arrives and
replace the values. ELP has the ELP-USB12MP01-L281 board for comparison only. Its sizes are
guesses, and the model does not place it.

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

Each camera has its own 1 m cable to its own USB-A port of the Pi in the pod (CAMERA_PORTS), along the
route of the C270 cable, with the two camera cables side by side in the clips. The cable that the
route does not use wraps on a printed drum on the bracket of each camera, and the camera on the arm of
the TAZ has a 0.5 m extension (cable_specs()).

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
    case=(35.84, 35.84),              # measured: width and height
    depth=11.15,                      # measured: front face to back face, without the front plate
    plate=(24.74, 24.62, 2.76),       # measured: the raised plate, centered on the face; 13.91 deep over all
    plate_holes=(3.0, 1.5),           # front photo: 4 small holes about 3 mm in from the corners; diameter a guess
    barrel=(15.56, 1.0),              # measured: 15.56 mm across, centered on the face, 1 mm past the plate
    af=(4.5, 6.0, 4.0),               # front photo: the autofocus block right of the barrel, 4.5 wide, 6 tall, at the
                                      # height of the lens; perspective photo: 4 mm past the plate
    socket=(8.5, 4.0, (-6.67, -13.92)),  # measured: the JST B4B-ZR socket on the back face, 2 mm over the bottom
                                      # edge and 7 mm in from the right edge (seen from the back); width and
                                      # height from the perspective photo
    plug=6.0,                         # guess: the ZHR-4 plug and its strain relief, straight back
    cable=(3.0, 10.0),                # guess: the cable of the plug, diameter and bend radius
    cable_length=1000.0,              # Arducam: the USB-A to JST B4B-ZR cable is 1 m
    # The U bracket.
    total=44.53,                      # measured: the top of the case to the bottom of the base
    pivot_h=26.11,                    # measured: the pivot axis to the bottom of the base
    sheet=1.4,                        # front photo: the legs, just outside the sides of the case
    leg_gap=0.2,                      # front photo: the gap from the case side to the leg
    leg_w=10.0,                       # perspective photo: the width of a leg along the depth of the case
    screws=43.52,                     # measured: the overall width over the pivot screw heads
    pivot=(0.14, -4.22),              # measured: 18.06 mm over the bottom of the case (y), 4.22 mm behind the
                                      # front face (z)
    base_d=25.75,                     # measured: the round base, with slots around the center hole
    boss=(4.5, 2.0),                  # front photo: the center boss under the base, diameter; height a guess
    base_hole=6.35,                   # measured: a through 1/4"-20 thread (a camera screw); an M5 is loose in it
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
# The cable drum on the Mini 2 wall: the height of its axis.
MINI_DRUM_Z = 404.0
# The full print height (published LulzBot sizes, unchecked on the printers) and the moving parts
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
    return B0561["pivot_h"]


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
    z1 = max(z1, arm_drum_z() + DRUM["flange"] / 2 + 2.0)
    pw = max(pw, DRUM["flange"] + 4.0)
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
        # The arm on the rail top, wide and long enough for the cable drum (drum_frame()).
        wd = max(w, DRUM["flange"] + 4.0)
        s = s + box(x0, TAZ_RAILS["x"][1][1] + 6.0, TAZ_RAILS["top"], TAZ_RAILS["top"] + t, zc - wd / 2, zc + wd / 2)
        s = s - Pos(x0 + t / 2, float(o[1]), zc) * (Location(Plane.YZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, t + 2))
        for z in (zc - 12.0, zc + 12.0):         # M3 into a roll-in T-nut in the top slot of the front rail
            s = s - Pos(76.0, TAZ_RAILS["top"] + t / 2, z) * (Location(Plane.XZ) * Cylinder(1.7, t + 2))
        return s
    # Mini 2: y is to the back. The wall from y0 - t to y0 (the base), on the plate on the rail top.
    y0, xc = float(o[1]), float(o[0])
    top = float(o[2]) + B0561["base_d"] / 2 + 4.0
    # The wall, wide and tall enough for the cable drum on its front face (drum_frame()).
    wd = max(w, DRUM["flange"] + 4.0)
    top = max(top, MINI_DRUM_Z + DRUM["flange"] / 2 + 2.0)
    s = box(xc - wd / 2, xc + wd / 2, y0 - t, y0, MINI_RAIL_TOP, top)
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


# ---------------------------------------------------------------- the extra cable
# Each camera cable is 1000 mm (Arducam), measured here over the whole cable with its plugs: the JST
# plug (6 mm out of the socket) and the USB-A plug (12 mm metal and 30 mm overmold and strain relief)
# leave CAMERA_CABLE mm of cable between the plug ends. That is an assumption: Arducam does not say how
# the 1 m is measured.
CAMERA_CABLE = 1000.0 - 6.0 - 12.0 - 30.0
# The extension for the camera on the arm of the TAZ: 0.5 m USB 2.0 A male to A female (to buy), the
# same measure. Its female end is a socket body of about 20 x 13 x 8 mm; the USB-A plug of the camera
# cable goes in it (its 12 mm metal inside the body). Cable between the ends: 500 - 42 - 20.
EXTENSION = dict(total=500.0, socket=(20.0, 13.0, 8.0), d=4.0)
EXTENSION_CABLE = EXTENSION["total"] - 12.0 - 30.0 - EXTENSION["socket"][0]
JOINT = 30.0 + EXTENSION["socket"][0]    # the camera USB-A overmold and the socket body, end to end
# The cable drum: the extra cable of each camera wraps on a printed drum on its bracket. One M3 screw
# through the middle holds it. It prints on its base
# flange; the top flange is a 45 degree cone, so it needs no support. The cable goes in through a
# notch in the base flange and out through a notch in the top flange, which holds the end. On the arm
# an M3 screw from under the link plate goes into a heat-set insert in the drum; on the brackets over
# the bed, into an insert in the bracket.
DRUM = dict(core=40.0, length=18.0, flange=52.0, base_t=2.0, top_t=1.2, notch=3.4, hole=3.4)
PITCH = 3.3                               # each turn of the 3 mm cable on the drum


def drum_radius():
    """The radius of the center of the cable on the drum."""
    return DRUM["core"] / 2 + B0561["cable"][0] / 2 + 0.2


def drum_model():
    """The drum in its print position: the axis along z, the base flange on z = 0."""
    from build123d import Cone
    d = DRUM
    rf, rc = d["flange"] / 2, d["core"] / 2
    s = Pos(0, 0, d["base_t"] / 2) * Cylinder(rf, d["base_t"])
    s = s + Pos(0, 0, d["base_t"] + d["length"] / 2) * Cylinder(rc, d["length"])
    cone = rf - rc
    z = d["base_t"] + d["length"]
    s = s + Pos(0, 0, z + cone / 2) * Cone(rc, rf, cone)
    s = s + Pos(0, 0, z + cone + d["top_t"] / 2) * Cylinder(rf, d["top_t"])
    s = s - Pos(0, 0, 20) * Cylinder(d["hole"] / 2, 60)
    # The notches: in the base flange for the cable in, in the top flange for the end.
    s = s - box(rc + 0.5, rf + 1.0, -d["notch"] / 2, d["notch"] / 2, -1, d["base_t"] + 0.01)
    s = s - Pos(-(rc + (rf - rc) / 2 + 0.5), 0, 0) * box(-(rf - rc) / 2 - 1, (rf - rc) / 2 + 1,
                                                       -d["notch"] / 2, d["notch"] / 2, z + cone - 0.5, z + cone + d["top_t"] + 1)
    return s


def drum_frame(name, where):
    """The drum as a Location in printer coordinates: its z is the axis, out of the bracket face. The
    cable goes on at drum x = -R, along drum +y.
    - On the arm: on the top of the link plate, on the screw of the base of U. The cable comes up past
      the side of the plate and goes on toward the arm (hinge -z).
    - TAZ, over the bed: on the arm of the bracket on the top front rail, in front of the wall. The cable
      comes past the side of the wall and goes on toward the front.
    - Mini 2, over the bed: on the front face of the wall. The cable comes over the wall and goes on
      down."""
    if where == "mount":
        H = C.hinge_loc(name)
        return H * Location(Plane(origin=(0.0, LINK_PLATE[1], arm_drum_z()), x_dir=(1, 0, 0), z_dir=(0, 1, 0)))
    _, br, _ = top_frame(name)
    o = base_point(br)
    if name == "taz":
        return Location(Plane(origin=(65.0, TAZ_RAILS["top"] + BRACKET_T, float(o[2]) - 5.0), x_dir=(0, 0, 1),
                              z_dir=(0, 1, 0)))
    return Location(Plane(origin=(float(o[0]), float(o[1]) - BRACKET_T, MINI_DRUM_Z), x_dir=(-1, 0, 0), z_dir=(0, -1, 0)))


def arm_drum_z():
    """The axis of the drum on the link plate (hinge z): its flange 1 mm in front of the lugs of the arm."""
    return LINK_Z + DRUM["flange"] / 2 + 1.0


def drum_helix(name, where, turns):
    """The cable on the drum: a helix from the base flange, turns turns, in printer coordinates. It
    starts at drum x = -R (the cable going along drum +y) and turns about the drum axis."""
    import numpy as np
    L = drum_frame(name, where)
    R = drum_radius()
    h0 = DRUM["base_t"] + B0561["cable"][0] / 2 + 0.3
    n = max(8, int(turns * 72))
    th = np.linspace(0.0, 2 * np.pi * turns, n + 1)
    pts = np.stack([-R * np.cos(th), R * np.sin(th), h0 + PITCH * th / (2 * np.pi)], axis=1)
    return np.array([tuple((L * Pos(*p)).position) for p in pts])


def joint_frame(name):
    """The joint of the camera cable and the extension (TAZ, camera on the arm): (the end of the camera
    cable, the start of the extension). It lies along the lower lane of the top front rail, on a tie
    tab, before the cable of the camera over the bed joins the rail."""
    import numpy as np
    z1 = JOINT_Z
    a = np.array([C.TAZ_UPRIGHT["front"] + TIE_TAB["height"] + EXTENSION["socket"][2] / 2, 476.0 - SIDE, z1 - JOINT])
    return a, a + np.array([0.0, 0.0, JOINT])


JOINT_Z = -315.0                          # the end of the joint on the rail (printer z); see report()
TIE_TAB = dict(base=(12.0, 14.0), height=6.0, slot=(4.2, 1.8))


def tie_tab():
    """A zip-tie tab in its print position (base on z = 0, a square of foam tape under it): a block with
    a tunnel along y for a 2.5 to 3.6 mm zip tie, the roof of the tunnel a 45 degree point."""
    from build123d import Polygon, extrude
    bx, by = TIE_TAB["base"]
    h = TIE_TAB["height"]
    sw, sh = TIE_TAB["slot"]
    s = box(-bx / 2, bx / 2, -by / 2, by / 2, 0.0, h)
    z0 = 1.0
    prof = Polygon((-sw / 2, z0), (sw / 2, z0), (sw / 2, z0 + sh), (0.0, z0 + sh + sw / 2), (-sw / 2, z0 + sh), align=None)
    tunnel = Location(Plane(origin=(0, by / 2 + 1, 0), x_dir=(1, 0, 0), z_dir=(0, -1, 0))) * extrude(prof, by + 2)
    return s - tunnel


def joint_meshes(name):
    """The joint on the TAZ: the USB-A plug of the camera cable in the socket of the extension, and the
    tie tab under it, in printer coordinates: [(part, shape, color)]."""
    import numpy as np
    a, b = joint_frame(name)
    # The plug points to +x of cable_plug with the face of its overmold at 0: here along +z, the face
    # 30 mm from the end of the camera cable.
    plug = Location(Plane(origin=tuple(a + np.array([0.0, 0.0, 30.0])), x_dir=(0, 0, 1), z_dir=(1, 0, 0))) * \
        P.cable_plug(*P.USBA_PLUG)
    sl, sw, sh = EXTENSION["socket"]
    sock = box(b[0] - sh / 2, b[0] + sh / 2, b[1] - sw / 2, b[1] + sw / 2, b[2] - sl, b[2])
    tab = Location(Plane(origin=(C.TAZ_UPRIGHT["front"], b[1], b[2] - JOINT / 2), x_dir=(0, 0, 1), z_dir=(1, 0, 0))) * tie_tab()
    return [("camera_joint", plug + sock, "#151515"), ("camera_joint_tab", tab, "#E8762B")]


def drum_meshes(name):
    """The two drums in printer coordinates: [(part, shape, color)]."""
    return [(f"camera_{w}_drum", drum_frame(name, w) * drum_model(), "#E8762B") for w in ("mount", "top")]


def overhang(shape):
    """The print check of make.sh on a shape in its print position: (one closed part, flat ceilings,
    faces under 40 degrees) in mm2."""
    import math as m
    import tempfile
    import trimesh
    from build123d import export_stl
    f = os.path.join(tempfile.mkdtemp(), "part.stl")
    export_stl(shape, f, tolerance=0.02, angular_tolerance=0.1)
    t = trimesh.load(f)
    nz, zc, area = t.face_normals[:, 2], t.triangles_center[:, 2], t.area_faces
    above = zc > t.bounds[0][2] + 0.05
    flat = area[above & (nz < -0.999)].sum()
    steep = area[above & (nz < -m.cos(m.radians(40))) & (nz >= -0.999)].sum()
    return bool(t.is_watertight and t.body_count == 1), float(flat), float(steep)


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
# cable, unmeasured). 30 mm out of the port.
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


# Dark red: the cable of the camera on the arm. Dark blue: the cable of the camera over the bed.
CABLE_COLORS = {"camera_cable": "#8B2A2A", "camera_top_cable": "#2B4C7E", "camera_ext_cable": "#3A3A3A"}


def clip_points(name):
    """The cable centers in the two-cable clips of camera.cable_clips: both lanes of each clip."""
    import numpy as np
    import clips as K
    if name == "taz":
        f = C.TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
        out = [(f, 476.0 + s * SIDE, z) for z in (-560.0, -400.0, -240.0) for s in (-1, 1)]
        out += [(f, 380.0, C.S_RAIL_SLOT_Z + s * SIDE) for s in (-1, 1)]
    else:
        zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
        out = [(x, -244.0 + s * SIDE, zt) for x in (-50.0, -165.0, -280.0) for s in (-1, 1)]
    return [np.array(p) for p in out]


def _p(L, x, y, z):
    import numpy as np
    return np.array(tuple((L * Pos(x, y, z)).position))


def _start(name, where):
    """The end of the JST plug and the direction out of it, in printer coordinates."""
    import numpy as np
    d = B0561
    _, _, (sx, sy) = d["socket"]
    cam = arm_frame(name)[0] if where == "mount" else top_frame(name)[0]
    end = _p(cam, sx, sy, -d["depth"] - d["plug"])
    return end, end - _p(cam, sx, sy, -d["depth"])


def _off_drum(helix, end):
    """A point past the drum flange on the tangent of the helix: before its start (end 0) or after its
    end (end -1). From the cable radius to past the flange along a tangent is under 22 mm."""
    import numpy as np
    if end == 0:
        t = helix[1] - helix[0]
        return helix[0] - 22.0 * t / np.linalg.norm(t)
    t = helix[-1] - helix[-2]
    return helix[-1] + 22.0 * t / np.linalg.norm(t)


def _routes(name, turns):
    """The drawn routes of the camera cables with the given turns on the drums: {cable: (points,
    flags for the lanes, lane offset, the helix, start direction, end direction, port)}."""
    import numpy as np
    import clips as K
    c270 = [np.asarray(q, float) for q in C.cable_points(name)]
    H = C.hinge_loc(name)
    h = lambda x, y, z: _p(H, x, y, z)
    R = drum_radius()
    hy = LINK_PLATE[1] + DRUM["base_t"] + B0561["cable"][0] / 2 + 0.3
    zc = link_base_z()
    out = {}
    T = P.matrix(C.pod_loc(name))
    into = -(T[:3, :3] @ np.array([0.0, 1.0, 0.0]))
    # The camera on the arm: up past the side of the link plate, onto the drum, off it beside its end.
    s0, d0 = _start(name, "mount")
    hx = drum_helix(name, "mount", turns["camera_cable"])
    pre = [s0, h(-34.0, -8.0, zc + 20.0), h(-34.0, hy, zc + 30.0), _off_drum(hx, 0)]
    post = [_off_drum(hx, -1)]
    b = np.array(C.BEDS[name][0])
    if name == "taz":
        f = C.TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
        a, j1 = joint_frame(name)
        post += [h(-36.0, 34.0, zc + 18.0), h(-40.0, -10.0, zc + 18.0)]
        lanes = [c270[-8], np.array([f, 476.0, -600.0]), np.array([f, 476.0, a[2] - 20.0])]
        pts = pre + list(hx) + post + lanes + [a]
        flags = [False] * (len(pre) + len(hx) + len(post) + 1) + [True, True, False]
        out["camera_cable"] = (pts, flags, SIDE, hx, d0, np.array([0.0, 0.0, 1.0]), None)
        tail = pi_tail(name, "mount")
        rail = [np.array([f, 476.0, -250.0]), np.array([f, 476.0, -200.0]), np.array([f, 440.0, C.S_RAIL_SLOT_Z]),
                np.array([f, 380.0, C.S_RAIL_SLOT_Z])]
        pts = [j1] + rail + tail
        out["camera_ext_cable"] = (pts, [False] + [True] * 4 + [False] * len(tail), SIDE, None,
                                   np.array([0.0, 0.0, 1.0]), into, "mount")
        # The camera over the bed: past the side of the wall onto the drum, off it to the front rail.
        s1, d1 = _start(name, "top")
        hx = drum_helix(name, "top", turns["camera_top_cable"])
        e = hx[0]
        pre = [s1, np.array([20.0, 500.0, e[2]]), np.array([e[0] - 22.0, e[1], e[2]])]
        x1 = _off_drum(hx, -1)
        post = [x1, np.array([f + 4.0, 500.0, x1[2]])]
        join = [np.array([f, 476.0, JOINT_Z + 15.0])] + rail
        tail = pi_tail(name, "top")
        pts = pre + list(hx) + post + join + tail
        flags = [False] * (len(pre) + len(hx) + len(post)) + [True] * len(join) + [False] * len(tail)
        out["camera_top_cable"] = (pts, flags, -SIDE, hx, d1, into, "top")
        return out
    zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
    post += [h(34.0, 34.0, zc + 18.0)]
    line = [np.array([-30.0, -244.0, zt]), np.array([-300.0, -244.0, zt])]
    tail = pi_tail(name, "mount")
    pts = pre + list(hx) + post + [c270[-6]] + line + tail
    flags = [False] * (len(pre) + len(hx) + len(post) + 1) + [True] * 2 + [False] * len(tail)
    out["camera_cable"] = (pts, flags, SIDE, hx, d0, into, "mount")
    s1, d1 = _start(name, "top")
    hx = drum_helix(name, "top", turns["camera_top_cable"])
    e = hx[0]
    o = base_point(top_frame(name)[1])
    over = MINI_DRUM_Z + DRUM["flange"] / 2 + 12.0
    pre = [s1, np.array([e[0], float(o[1]) + 10.0, over]), np.array([e[0], e[1], over]), np.array([e[0], e[1], e[2] + 20.0])]
    x1 = _off_drum(hx, -1)
    post = [x1]
    join = [np.array([b[0] - 30.0, -244.0, zt]), line[-1]]
    tail = pi_tail(name, "top")
    tail = [np.array([tail[0][0], line[-1][1] + SIDE, tail[0][2]])] + tail
    pts = pre + list(hx) + post + join + tail
    flags = [False] * (len(pre) + len(hx) + len(post)) + [True] * len(join) + [False] * len(tail)
    out["camera_top_cable"] = (pts, flags, -SIDE, hx, d1, into, "top")
    return out


def cable_specs(name):
    """The specs of the camera cables for cable_physics.py (printer coordinates), each at its real
    length: the turns on each drum take the cable that the route does not use. On the TAZ the camera on
    the arm has the extension: its cable ends at the joint, and the extension (its last span gets the
    rest of its length) goes on to the Pi."""
    import numpy as np
    import cable_physics as CP
    lane_n = np.array([1.0, 0.0, 0.0]) if name == "taz" else np.array([0.0, 0.0, 1.0])

    def build(turns):
        specs = []
        for n, (pts, flags, off, hx, d0, d1, port) in _routes(name, turns).items():
            pts = side_by_side([np.asarray(q, float) for q in pts], flags, lane_n, off)
            if n == "camera_ext_cable":
                specs.append(CP.make_spec(n, pts, EXTENSION["d"], clip_points(name), start_dir=d0, end_dir=d1,
                                          total=EXTENSION_CABLE, remainder="last"))
                continue
            sp = CP.make_spec(n, pts, B0561["cable"][0], clip_points(name), start_dir=d0, end_dir=d1, coil=hx,
                              total=CAMERA_CABLE)
            sp["turns"] = turns[n]
            specs.append(sp)
        return specs

    def used(n, t):
        sp = next(x for x in build({"camera_cable": t, "camera_top_cable": t}) if x["name"] == n)
        return sum(L for _, _, L in CP.lengths(sp)) - CAMERA_CABLE

    # The turns of each drum: the root of the length used less the cable (bisection: the exit of the
    # drum moves round it with the turns, so the length is not smooth in the turns).
    turns = {}
    for n in ("camera_cable", "camera_top_cable"):
        lo, hi = 0.25, 6.0
        flo = used(n, lo)
        for _ in range(40):
            mid = (lo + hi) / 2
            fm = used(n, mid)
            if (fm > 0) == (flo > 0):
                lo, flo = mid, fm
            else:
                hi = mid
        turns[n] = (lo + hi) / 2
    return build(turns)


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
    extra = drum_meshes(name) + (joint_meshes(name) if name == "taz" else [])
    for k, sh, c in arm_mount(name) + top_mount(name) + extra:
        p = os.path.join(tmp, f"{name}_{k}.stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        out[k] = (trimesh.load(p, force="mesh"), c)
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


def check_drums(name):
    """The drums against the arm, the clamp, the link, the brackets, and the cameras with their
    keepouts. Returns a list of problems."""
    bad = []
    s, _ = bracket_model()
    for where in ("mount", "top"):
        d = drum_frame(name, where) * drum_model()
        if where == "mount":
            cam, br = arm_frame(name)
            others = [("arm", C.arm(name)), ("clamp", C.CLAMPS[name]()), ("hinge screw", C.hinge_screw(name)),
                      ("link", C.hinge_loc(name) * link_hinge())]
        else:
            cam, br, _ = top_frame(name)
            others = [("bracket", top_bracket(name))]
        others += [(k, cam * v) for k, v in case_model().items()] + [("U bracket", br * s)]
        for n, o in others:
            v = (d & o).volume
            if v > 0.01:
                bad.append(f"the {where} drum and the {n}: {v:.2f} mm3")
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
        bad = check_drums(name)
        print(f"{name}: drums: " + ("; ".join(bad) if bad else "no touch with the arm, the brackets, or the cameras"))
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
        import cable_physics as CP
        for sp in cable_specs(name):
            ls = CP.lengths(sp)
            drawn = sum(dr for k, dr, _ in ls if k != "fixed")
            coil = sum(L for k, _, L in ls if k == "fixed")
            total = sum(L for _, _, L in ls)
            print(f"{name}: {sp['name']}: {total:.0f} mm between the plugs, {drawn:.0f} mm of drawn route, "
                  + (f"{coil:.0f} mm on the drum ({sp['turns']:.2f} turns, {2 * drum_radius():.1f} mm across)"
                     if coil else "no drum"))

if __name__ == "__main__":
    report()
