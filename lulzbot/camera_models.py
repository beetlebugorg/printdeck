"""Models of the cameras to buy, and the brackets for them. Two Arducam B0561 16 MP autofocus
cameras: one on the arm of the C270 mount, one straight down over the middle of the bed. The sizes in
B0561 are caliper measurements where the comment says "measured". The other sizes come from two
product photos: a front view with two official sizes (36 mm and 44 mm), and a perspective view. ELP
has the ELP-USB12MP01-L281 board for comparison only. Its sizes are guesses, and the model does not
place it.

The B0561 is a black metal case with a raised front plate, a short lens barrel at the middle of the
face, and the autofocus block at the right of the barrel. A U bracket of sheet metal holds the case
on two pivot screws at its sides. The round base of the U has a through 1/4"-20 thread at its
center. The JST B4B-ZR socket is on the back of the case, and the cable plugs straight in from the
back.

Each bracket holds the base of the U with one screw. The pivot of the U sets the tilt.
- On the arm (camera.py), in place of the C270: a link with a tongue on the hinge pin (where the
  eyelet of the C270 is), held level by the hinge screw, and a level plate in front of the arm. The
  camera hangs under the plate and tilts down on its pivot. The link is a placeholder (violet), with
  no STL file.
- Over the bed: a wall that stands up, with the base of the U on its face, so the pivot is level and
  the camera looks straight down at no tilt.
  - TAZ: a printed part (orange) on the top front rail only (top_bracket_taz()), so the space over
    the back of the bed stays free for the filament tube. It grips the top face and the inside face
    of the rail with six M3 x 8 screws into roll-in T-nuts in the two slots. The camera hangs behind
    the rail, 27 mm in front of the middle of the bed. pod.py export writes the STL file, and
    stl/pod/README.md has the parts and the assembly.
  - Mini 2: a placeholder plate (violet) on the top face of the top front rail, with the wall at its
    back edge. It has no STL file.

Each camera has its own cable to its own USB-A port of the Pi in the pod (CAMERA_PORTS), along the
route of the C270 cable, with the two camera cables side by side in the clips. report() prints the
length of each route and the cable length to buy (cable_plan()).

Camera coordinates: the origin is the middle of the front face of the case (under the front plate),
z out of the lens along the optical axis, y up in the picture, x = y cross z. lens_point() is the
middle of the front of the lens barrel: the views of live.py and camera_preview.py start there (the
pinhole of camera_options.py). At 300 mm and more from the bed, the true optical center a few mm
behind it changes the views very little. Bracket coordinates: the origin is the middle of the bottom
face of the base, y from the base to the pivot, x along the pivot axis.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with shapely --with rtree --with pyrender --with PyOpenGL --with pillow python camera_models.py
It prints the fit of the camera on the arm over its tilt range, the gaps to the printers, the gap of
the top camera to the X axis at the full print height, the print check and the stiffness of the TAZ
top bracket, and the cable lengths.
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
PRINTED_COLOR = C.ORANGE                # the printed parts with STL files

# The link on the C270 arm (hinge coordinates of camera.py: the pin along x, z forward, at no tilt).
LINK_TONGUE = (C.EYELET[0], C.EYELET[1], C.EYELET[2])   # the width, height, and depth behind the pin of the C270 eyelet
LINK_PLATE = (44.0, 4.0)                # the plate: width (x) and thickness (y); it goes from the lugs to past the base
LINK_Z = C.LUG_TOP_Z + 1.0              # the back edge of the plate, 1 mm in front of the lugs

# The top brackets. TAZ: the two top rails are 2020 at x -84 to -64 and 66 to 86, y 466 to 486
# (measured in the model, at the middle of the bed). Mini 2: the top front rail has its top face at
# z 373 and goes back to about y -180 (measured in the model).
TAZ_RAILS = dict(top=486.0, x=((-84.0, -64.0), (66.0, 86.0)))
MINI_RAIL_TOP = 373.0
BRACKET_T = 4.0                         # the Mini 2 placeholder: the plates and walls
BRACKET_W = 40.0                        # the Mini 2 placeholder: across the bed
# The lens over the bed. The pivot is 8 mm over the lens (the plate, the barrel, and the pivot depth),
# and the round base (25.75 mm) is on the wall with its center at the height of the pivot. TAZ: the X
# axis goes up to about 476 at the full print height, so the lens is at 490 and the bottom of the base
# 9 mm over the X axis. Mini 2: the base must clear the top of the rail.
TOP_LENS_UP = {"taz": 490.0, "mini2": 381.0}
# The TAZ top bracket: a block on the top front rail (printer coordinates: x to the front, y up). The
# camera hangs behind the rail, TOP_FORWARD in front of the middle of the bed, so the back half of the
# space over the bed stays free for the filament tube. It prints on its right end (printer z up).
TOP = dict(wall=19.0,           # the camera wall, from the floor of the seat to its front face
           width=60.0,          # the camera wall, along the rail
           height=520.0,        # the top of the camera wall (printer y)
           plate=5.0,           # the plate on the top face of the rail
           inner=(5.0, 466.0),  # the plate on the inside face of the rail: thickness, bottom (printer y)
           length=124.0,        # the plates and the web, along the rail
           web=6.0,             # the web along the back of the part, from the inside plate up to the top
           fillet=4.0,          # the 45 degree fillet between the web and the top plate
           screws=(20.0, 50.0), # the M3 screws, each side of the middle: top slot (the first), inside slot (both)
           seat=(26.25, 1.2),   # the seat of the round base (25.75): diameter at its floor, depth
           hole=6.6,            # the clearance hole of the 1/4"-20 stud
           notch=6.5,           # the cable notch over the camera wall
           notch_y=510.5,       # the bottom of the cable notch
           cable_dz=-13.0)      # the cable down the front face of the wall, from the middle (printer z)
# The lens in front of the middle of the bed (mm, toward the front). TAZ: the back face of the camera
# wall is flush with the back face of the inside plate, and the case is 12 mm behind the rail.
TOP_FORWARD = {"taz": 27.2, "mini2": 0.0}
INSIDE_SLOT_Y = 476.0                   # the slot in the inside face of the TAZ top front rail (printer y)
# The base fixing: a 1/4"-20 stud (a set screw with no head), 30 mm long, in the thread of the base,
# flush with its top. It goes through the camera wall, and a 1/4"-20 hex nut (7/16", 11.1 mm across the
# flats, 5.6 mm thick) on the flat front face of the wall clamps it. The other choice: a 1/4"-20 x 7/8"
# cap screw from the front face into the base.
STUD = dict(length=30.0, nut=(11.1, 5.6))
SCREWS_14 = (22.2,)                     # the length of the other choice
RAIL_SLOTS = (-74.0, 76.0)              # the top slots of the two TAZ top rails (printer x)
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
    """(camera Location, bracket Location, the lens point) of the camera over the bed, TOP_FORWARD in
    front of its middle: straight down, the back of the printer up in the picture, the base of the U on a wall in front."""
    import numpy as np
    up, back, _ = C.axes(name)
    c = np.array(C.BEDS[name][0], float)
    lens = c + (TOP_LENS_UP[name] - float(np.dot(c, up))) * up - TOP_FORWARD[name] * np.asarray(back, float)
    cam, br = placed(lens, -up, back, back)
    return cam, br, lens


def prism_x(pts, x0, x1):
    """A prism along printer x from x0 to x1, its section the polygon pts of (z, y) points."""
    from build123d import Polygon, extrude
    pl = Plane(origin=(0, 0, 0), x_dir=(0, 0, 1), z_dir=(-1, 0, 0))
    s = extrude(pl * Polygon(*pts, align=None), amount=x1 - x0)
    # extrude goes along the normal of the face, and the order of the points sets it.
    return Pos(x0 - s.bounding_box().min.X, 0, 0) * s


def prism_y(pts, y0, y1):
    """A prism along printer y from y0 to y1, its section the polygon pts of (x, z) points."""
    from build123d import Polygon, extrude
    pl = Plane(origin=(0, 0, 0), x_dir=(1, 0, 0), z_dir=(0, 1, 0))
    s = extrude(pl * Polygon(*[(x, -z) for x, z in pts], align=None), amount=y1 - y0)
    return Pos(0, y0 - s.bounding_box().min.Y, 0) * s


def top_places():
    """The places of the TAZ top bracket in printer coordinates: (xw, yb, zc, zs). xw: the floor of the
    base seat (the bottom face of the base). yb, zc: the axis of the base bolt (zc is also the middle
    of the bridge). zs: the middle of the JST socket."""
    _, br, _ = top_frame("taz")
    o = base_point(br)
    return float(o[0]), float(o[1]), float(o[2]), float(_start("taz", "top")[0][2])


def top_bracket_taz():
    """The TAZ top bracket, in printer coordinates: a block on the top front rail only. A plate on the
    top face of the rail and a plate on its inside face grip the rail. A deep web goes up from the
    inside plate along the full length, and the camera wall stands on the top plate in front of the
    web, over the rail. The base of the U is in a seat on the back face of the web and the wall
    (toward -x), and the camera hangs behind the rail. A 1/4"-20 stud in the thread of the base goes
    through the wall, and a nut on the flat front face of the wall clamps it. Two M3 screws go down
    through the wall into roll-in T-nuts in the top slot, and four go through the inside plate into
    T-nuts in the inside slot. The cable goes over the wall through a notch, down its front face, and
    over the front edge of the top plate.

    The part prints on its right end (printer z up), so the layers are in the plane of the load of
    the camera. The right end of the wall and the side of the cable notch slope at 50 degrees, so
    every face that faces down is 45 degrees or more from the bed."""
    from build123d import Cone
    T = TOP
    xw, yb, zc, zs = top_places()
    y0, y1 = TAZ_RAILS["top"], T["height"]
    yp = y0 + T["plate"]
    (xi, xf) = TAZ_RAILS["x"][1]
    ti, yi = T["inner"]
    xb = xi - ti                               # the back face of the inside plate
    sd, sdep = T["seat"]
    xr = xw - sdep                             # the back face of the web and the wall (toward the camera)
    xk = xw + T["wall"]                        # the front face of the camera wall (the nut)
    xe = xr + T["web"]                         # the front face of the web
    hw, hl, f = T["width"] / 2, T["length"] / 2, T["fillet"]
    slope = math.tan(math.radians(50.0))
    s = box(min(xb, xr), xf, y0, yp, zc - hl, zc + hl) + box(min(xb, xr), xi, yi, y0 + 0.01, zc - hl, zc + hl)
    s = s + box(xr, xe, y0, y1, zc - hl, zc + hl)
    s = s + prism_y([(xe - 0.01, zc - hw - slope * (xk - xe)), (xk, zc - hw), (xk, zc + hw), (xe - 0.01, zc + hw)], y0, y1)
    s = s + prism_z_fillet(xe, yp, f, zc - hl, zc + hl)
    # The seat of the base: a cone with its side 40 degrees from the face of the wall, so its top is 50
    # degrees from the bed and prints with no support.
    sr = sd / 2 + sdep / math.tan(math.radians(40.0))
    seat = Location(Plane(origin=(xw - sdep / 2, yb, zc), x_dir=(0, 0, 1), z_dir=(-1, 0, 0))) * \
        Cone(sd / 2, sr, sdep)
    seat = seat + Pos(xr - 2.0, yb, zc) * (Location(Plane.YZ) * Cylinder(sr, 4.0))
    s = s - seat
    up = (0, 0, 1)                             # the print direction: the teardrops point up
    s = s - C.teardrop(T["hole"] / 2, 60.0, (xw, yb, zc), (1, 0, 0), up)
    # The cable notch over the camera wall: its +z side slopes at 50 degrees.
    w = T["notch"]
    n0 = T["notch_y"]
    notch = [(zs - w / 2, n0), (zs + w / 2, n0), (zs + w / 2 + (y1 + 1.0 - n0) * slope, y1 + 1.0), (zs - w / 2, y1 + 1.0)]
    s = s - prism_x(notch, xr - 1.0, xk + 1.0)
    # The screws: down through the wall into the top slot, with a long counterbore from the top of the
    # wall, and through the inside plate into the inside slot.
    for x, z in top_screw_places():
        s = s - C.teardrop(1.7, 20.0, (x, y0, z), (0, 1, 0), up)
        s = s - C.teardrop(C.COUNTERBORE[0] / 2, 60.0, (x, y0 + C.COUNTERBORE[1] + 30.0, z), (0, 1, 0), up)
    for y, z in inner_screw_places():
        s = s - C.counterbored((xb, y, z), (-1, 0, 0), up, ti)
    return s


def prism_z_fillet(x, y, f, z0, z1):
    """A 45 degree fillet along printer z from z0 to z1, in the corner of a face toward +x at x and a
    face toward +y at y."""
    from build123d import Polygon, extrude
    pl = Plane(origin=(0, 0, z0), x_dir=(1, 0, 0), z_dir=(0, 0, 1))
    s = extrude(pl * Polygon((x - 0.01, y - 0.01), (x + f, y - 0.01), (x - 0.01, y + f), align=None), amount=z1 - z0)
    return Pos(0, 0, z0 - s.bounding_box().min.Z) * s


def top_screw_places():
    """(x, z) of the two M3 screws down through the camera wall into the top slot of the front rail."""
    _, _, zc, _ = top_places()
    return [(RAIL_SLOTS[1], zc + sg * TOP["screws"][0]) for sg in (-1, 1)]


def inner_screw_places():
    """(y, z) of the four M3 screws through the inside plate into the inside slot of the front rail."""
    _, _, zc, _ = top_places()
    return [(INSIDE_SLOT_Y, zc + sg * d) for d in TOP["screws"] for sg in (-1, 1)]


def top_hardware_taz():
    """[(part, shape, color)] of the bought parts of the TAZ top bracket: the six M3 x 8 screws, the
    six roll-in T-nuts, and the 1/4"-20 stud with its nut."""
    y0 = TAZ_RAILS["top"]
    xi = TAZ_RAILS["x"][1][0]
    xb = xi - TOP["inner"][0]
    screws, nuts = [], []
    for x, z in top_screw_places():
        screws.append(P.screw(3, C.TNUT_SCREW, (x, y0 + C.COUNTERBORE[1], z), (0, 1, 0)))
        nuts.append(box(x - 5.0, x + 5.0, y0 - 5.8, y0 - 1.8, z - 3.0, z + 3.0))
    for y, z in inner_screw_places():
        screws.append(P.screw(3, C.TNUT_SCREW, (xb + TOP["inner"][0] - C.COUNTERBORE[1], y, z), (-1, 0, 0)))
        nuts.append(box(xi + 1.8, xi + 5.8, y - 5.0, y + 5.0, z - 3.0, z + 3.0))
    join = lambda xs: xs[0] if len(xs) == 1 else xs[0] + join(xs[1:])
    rod, nut = stud()
    return [("camera_top_screws", join(screws), P.SCREW_COLOR), ("camera_top_tnuts", join(nuts), "#9AA0A8"),
            ("camera_top_stud", rod, P.SCREW_COLOR), ("camera_top_nut", nut, "#9AA0A8")]


def stud_places():
    """The places of the stud and its nut along printer x: (the top end of the stud, its far end, the
    nut from, the nut to). The stud top is flush with the top of the bottom of the U, on the round base
    (two sheets in bracket_model())."""
    xw, _, _, _ = top_places()
    top = xw - 2 * B0561["sheet"]
    xk = xw + TOP["wall"]
    return top, top + STUD["length"], xk, xk + STUD["nut"][1]


def stud():
    """The 1/4"-20 stud and its nut, in printer coordinates."""
    from build123d import RegularPolygon, extrude
    _, yb, zc, _ = top_places()
    top, end, n0, n1 = stud_places()
    along = lambda x0, x1, r: Pos((x0 + x1) / 2, yb, zc) * (Location(Plane.YZ) * Cylinder(r, x1 - x0))
    nf = STUD["nut"][0]
    nut = Location(Plane(origin=(n0, yb, zc), x_dir=(0, 0, 1), z_dir=(1, 0, 0))) * \
        extrude(RegularPolygon(nf / math.sqrt(3.0), 6), amount=n1 - n0)
    return along(top, end, 3.1), nut - along(n0 - 1, n1 + 1, 3.1)


def stud_fit():
    """The 1/4"-20 fixings of the TAZ top bracket, in mm: (the stud past the nut, the gap from the top
    of the stud to the case at no tilt, and for each length of SCREWS_14 (the gap from its tip to the
    case at no tilt, the length in the base))."""
    xw, _, _, _ = top_places()
    top, end, _, n1 = stud_places()
    cam, _, _ = top_frame("taz")
    case = (cam * case_model()["case"]).bounding_box()
    xk = xw + TOP["wall"]
    screws = [(xk - L - case.max.X, min(L - TOP["wall"], 2 * B0561["sheet"])) for L in SCREWS_14]
    return end - n1, top - case.max.X, screws


def top_bracket(name):
    """The bracket over the bed, in printer coordinates. TAZ: top_bracket_taz(). Mini 2: the placeholder
    wall that the base screws onto, and the plate on the top front rail."""
    if name == "taz":
        return top_bracket_taz()
    cam, br, lens = top_frame(name)
    o = base_point(br)                         # the middle of the bottom face of the base, on the wall
    t, w = BRACKET_T, BRACKET_W
    # Mini 2: y is to the back. The wall from y0 - t to y0 (the base), on the plate on the rail top.
    y0, xc = float(o[1]), float(o[0])
    top = float(o[2]) + B0561["base_d"] / 2 + 4.0
    s = box(xc - w / 2, xc + w / 2, y0 - t, y0, MINI_RAIL_TOP, top)
    s = s + box(xc - w / 2, xc + w / 2, -250.0, y0, MINI_RAIL_TOP, MINI_RAIL_TOP + t)
    s = s - Pos(xc, y0 - t / 2, float(o[2])) * (Location(Plane.XZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, t + 2))
    for x in (xc - 12.0, xc + 12.0):             # M3 screws into the rail top
        s = s - Pos(x, -235.0, MINI_RAIL_TOP + t / 2) * Cylinder(1.7, t + 2)
    # A notch in the wall for the plug and the bend of the cable.
    k = (cam * case_model()["keepout"]).bounding_box()
    return s - box(k.min.X - 0.5, k.max.X + 0.5, k.min.Y - 0.5, k.max.Y + 0.5, k.min.Z - 0.5, k.max.Z + 20.0)


def top_mount(name):
    """[(part, shape, color)] of the camera over the bed and its bracket, in printer coordinates. The
    first item is the printed bracket."""
    cam, br, _ = top_frame(name)
    color = PRINTED_COLOR if name == "taz" else BRACKET_COLOR
    return [("camera_top_plate", top_bracket(name), color)] + camera_parts(cam, br, "top")


def printed():
    """(file name, shape in printer coordinates, the rotation to its print position) of the printed
    camera parts with STL files: the TAZ top bracket. It prints on its right end: printer z up."""
    return [("camera-top-taz", top_bracket_taz(), Rot(0, 0, 0))]


def mount_lens(name):
    import numpy as np
    cam, _ = arm_frame(name)
    return np.array(tuple((cam * Pos(*lens_point())).position))


# ---------------------------------------------------------------- the cable lengths
# A cable length as sold is from end to end with the plugs: the JST ZHR-4 plug (6 mm out of the
# socket) and the USB-A plug (12 mm of metal and 30 mm of overmold and strain relief). That is an
# assumption: the sellers do not say how they measure it.
PLUG_ENDS = 6.0 + 12.0 + 30.0
# The service allowance: cable for the tilt of the camera, and to unplug each end with the cable in
# its clips.
SERVICE = 100.0
# Lengths of USB-A to JST 4-pin cables that sellers list (mm). The B0561 comes with a 1 m cable.
STOCK_LENGTHS = (300.0, 500.0, 1000.0, 1500.0, 2000.0)


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


def _routes(name):
    """The drawn routes of the camera cables: {cable: (points, flags for the lanes, lane offset, start
    direction, end direction)}. Each route follows the C270 cable along the frame to the Pi."""
    import numpy as np
    import clips as K
    c270 = [np.asarray(q, float) for q in C.cable_points(name)]
    H = C.hinge_loc(name)
    h = lambda x, y, z: _p(H, x, y, z)
    zc = link_base_z()
    z0 = C.ARM_Z[0]
    out = {}
    T = P.matrix(C.pod_loc(name))
    into = -(T[:3, :3] @ np.array([0.0, 1.0, 0.0]))
    # The camera on the arm: up past the side of the link plate on the side of the post, over the beam
    # of the arm, and down the back of the post (the C270 route) to the clamp.
    xs = math.copysign(34.0, C.POST[name]["x"])
    s0, d0 = _start(name, "mount")
    pre = [s0, h(xs, -8.0, zc + 20.0), h(xs, C.CABLE_LOOP, LINK_Z + 4.0), h(xs, C.CABLE_LOOP, z0 - 4.0)] + c270[4:6]
    if name == "taz":
        f = C.TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
        rail = [np.array([f, 476.0, -250.0]), np.array([f, 476.0, -200.0]), np.array([f, 440.0, C.S_RAIL_SLOT_Z]),
                np.array([f, 380.0, C.S_RAIL_SLOT_Z])]
        lanes = [c270[-8], np.array([f, 476.0, -600.0])] + rail
        tail = pi_tail(name, "mount")
        pts = pre + lanes + tail
        flags = [False] * (len(pre) + 1) + [True] * (len(lanes) - 1) + [False] * len(tail)
        out["camera_cable"] = (pts, flags, SIDE, d0, into)
        # The camera over the bed: up out of the plug, through the notch over the camera wall, down its
        # front face beside the nut, over the front edge of the top plate, down the front face of the
        # rail, and along the rail with the other camera cable.
        s1, d1 = _start(name, "top")
        xw, _, zb, zs = top_places()
        yn = TOP["notch_y"] + B0561["cable"][0] / 2 + 0.5
        yf = TAZ_RAILS["top"] + TOP["plate"] + B0561["cable"][0] / 2 + 0.5
        zcab = zb + TOP["cable_dz"]
        xf = TAZ_RAILS["x"][1][1]
        xk = xw + TOP["wall"]
        pre = [s1, np.array([s1[0] + 4.0, yn + 4.0, zs]), np.array([xk + 3.0, yn, zs]),
               np.array([xk + 3.0, yf + 3.0, zcab]), np.array([xf - 1.0, yf, zcab]),
               np.array([xf + 3.0, yf - 4.0, zcab]), np.array([f, 482.0, zcab + 8.0])]
        lane = [np.array([f, 476.0, zcab + 40.0])] + rail
        tail = pi_tail(name, "top")
        pts = pre + lane + tail
        flags = [False] * len(pre) + [True] * len(lane) + [False] * len(tail)
        out["camera_top_cable"] = (pts, flags, -SIDE, d1, into)
        return out
    zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
    line = [np.array([-30.0, -244.0, zt]), np.array([-300.0, -244.0, zt])]
    tail = pi_tail(name, "mount")
    pts = pre + [c270[-6]] + line + tail
    flags = [False] * (len(pre) + 1) + [True] * 2 + [False] * len(tail)
    out["camera_cable"] = (pts, flags, SIDE, d0, into)
    # The camera over the bed: up out of the plug, over the wall, down its front face to the rail.
    s1, d1 = _start(name, "top")
    o = base_point(top_frame(name)[1])
    b = np.array(C.BEDS[name][0])
    over = float(o[2]) + B0561["base_d"] / 2 + 12.0
    y0 = float(o[1]) - BRACKET_T
    pre = [s1, np.array([s1[0], float(o[1]) + 6.0, over]), np.array([s1[0], y0 - 8.0, over]),
           np.array([s1[0], y0 - 12.0, zt + 15.0])]
    join = [np.array([b[0] - 30.0, -244.0, zt]), line[-1]]
    tail = pi_tail(name, "top")
    tail = [np.array([tail[0][0], line[-1][1] + SIDE, tail[0][2]])] + tail
    pts = pre + join + tail
    flags = [False] * len(pre) + [True] * len(join) + [False] * len(tail)
    out["camera_top_cable"] = (pts, flags, -SIDE, d1, into)
    return out


def cable_specs(name):
    """The specs of the camera cables for cable_physics.py (printer coordinates). Each cable has the
    length of its drawn route with the slack of cable_physics.py."""
    import numpy as np
    import cable_physics as CP
    lane_n = np.array([1.0, 0.0, 0.0]) if name == "taz" else np.array([0.0, 0.0, 1.0])
    specs = []
    for n, (pts, flags, off, d0, d1) in _routes(name).items():
        pts = side_by_side([np.asarray(q, float) for q in pts], flags, lane_n, off)
        specs.append(CP.make_spec(n, pts, B0561["cable"][0], clip_points(name), start_dir=d0, end_dir=d1))
    return specs


def cable_plan(spec):
    """The cable to buy for a camera cable spec: (the cable between the plug ends along the route, the
    length needed end to end with the plugs and the service allowance, the stock length to buy, the
    cable left over with that length, the length to crimp a cable to, from end to end) in mm."""
    import cable_physics as CP
    route = sum(L for _, _, L in CP.lengths(spec))
    need = route + PLUG_ENDS + SERVICE
    buy = next((L for L in STOCK_LENGTHS if L >= need), None)
    crimp = 10.0 * math.ceil(need / 10.0)
    return route, need, buy, (buy - need if buy else None), crimp


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
    extra = top_hardware_taz() if name == "taz" else []
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
    """The case and its keepout over the bed against the U bracket, the printed bracket, and (TAZ) the
    bought parts of the bracket; the U bracket against the printed bracket. Returns a list of problems."""
    bad = []
    cam, br, _ = top_frame(name)
    s, _ = bracket_model()
    plate = top_bracket(name)
    others = [("U bracket", br * s), ("printed bracket", plate)]
    hw = top_hardware_taz() if name == "taz" else []
    others += [(k.replace("camera_top_", "").replace("_", " "), sh) for k, sh, _ in hw]
    for k, sh in case_model().items():
        for n2, other in others:
            v = ((cam * sh) & other).volume
            if v > 0.01:
                bad.append(f"the {k} and the {n2}: {v:.2f} mm3")
    for n2, other in [("printed bracket", plate)] + others[2:]:
        v = ((br * s) & other).volume
        if v > 0.01:
            bad.append(f"the U bracket and the {n2}: {v:.2f} mm3")
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


# A guess: the B0561 with its U bracket. Arducam gives no weight that we have.
B0561_MASS = 0.06              # kg


def top_stiffness():
    """A beam model of the camera wall of the TAZ top bracket: a cantilever from the top face of the
    rail up to the axis of the stud, a solid section of the full wall. The rail and the screws are
    rigid in the model. Returns (the lever from the inside face of the rail to the lens, the lever
    from the wall to the lens, the wall section (along the rail, thick), the down stiffness at the
    lens, the stiffness across the rail and along it at the stud (N/mm), the lowest first frequency
    (Hz), the sag of the lens under the camera (mm))."""
    xw, yb, _, _ = top_places()
    _, _, lens = top_frame("taz")
    w, t = TOP["width"], TOP["wall"] + TOP["seat"][1]
    h = yb - TAZ_RAILS["top"]
    a = xw - float(lens[0])
    E = C.E_PETG
    I_x, I_z = w * t ** 3 / 12.0, t * w ** 3 / 12.0
    k_down = E * I_x / (a * a * h)
    k_x, k_z = 3 * E * I_x / h ** 3, 3 * E * I_z / h ** 3
    k = min(k_down, k_x, k_z)
    f = math.sqrt(k * 1000.0 / B0561_MASS) / (2 * math.pi)
    return TAZ_RAILS["x"][1][0] - float(lens[0]), a, (w, t), k_down, k_x, k_z, f, B0561_MASS * 9.81 / k_down


def bracket_touch(name, printer, T, n=30000):
    """How far the printer goes into the bracket over the bed (mm, 0 when it does not)."""
    return C.base_touch(name, printer, T, n=n, shape=top_bracket(name))


def report():
    import tempfile
    import numpy as np
    import trimesh
    from build123d import export_stl
    import cable_physics as CP
    for name in ("taz", "mini2"):
        bad = check_arm(name)
        print(f"{name}: camera on the arm: " + ("; ".join(bad) if bad else "no touch over the tilt range"))
        bad = check_top(name)
        print(f"{name}: camera over the bed: " + ("; ".join(bad) if bad else "no touch with its bracket and screws"))
        printer = P.setups()[name]["printer"]()
        printer = printer + C.stock_lcd(name, printer)
        world = P.setups()[name]["world"]
        tmp = tempfile.mkdtemp()
        for label, items in (("camera on the arm, its bracket, and the link", arm_mount(name)),
                             ("camera over the bed and its U bracket", top_mount(name)[1:])):
            ms = []
            for k, sh, _ in items:
                f = os.path.join(tmp, f"{k}.stl")
                export_stl(sh, f, tolerance=0.1, angular_tolerance=0.3)
                ms.append(trimesh.load(f, force="mesh"))
            gap = P.clearance(ms, world, printer, n=8000)
            print(f"{name}: {label}: {gap:.1f} mm from the printer")
        print(f"{name}: the printer goes {bracket_touch(name, printer, world):.2f} mm into the bracket over the bed")
        gap, rise = gantry_gap(name, [sh for _, sh, _ in top_mount(name)])
        _, _, lens = top_frame(name)
        h = float(np.dot(lens - np.array(C.BEDS[name][0]), C.axes(name)[0]))
        print(f"{name}: top lens {h:.1f} mm over the bed; at the full print height ({PRINT_Z[name]:.0f} mm, "
              f"the tool head {rise:.0f} mm up from the model) the moving parts are {gap:.1f} mm under the camera "
              f"and its bracket")
        if name == "taz":
            past, top_gap, screws = stud_fit()
            print(f"taz: the {STUD['length']:.0f} mm stud goes {past:.1f} mm ({past / 1.27:.1f} threads) past the nut, "
                  f"its top {top_gap:.1f} mm from the case; " + "; ".join(
                      f"a {L:.1f} mm screw from the front face has its tip {g:.1f} mm from the case, {e:.1f} mm in the base"
                      for L, (g, e) in zip(SCREWS_14, screws)))
            lr, lw, (w, t), kd, kx, kz, f, sag = top_stiffness()
            print(f"taz: top bracket: the lens is {lr:.0f} mm behind the inside face of the front rail and {lw:.0f} mm "
                  f"from the camera wall; the wall is {w:.0f} x {t:.1f} mm; down {kd:.0f} N/mm at the lens, across "
                  f"{kx:.0f} N/mm, along {kz:.0f} N/mm at the stud; first frequency {f:.0f} Hz with "
                  f"{B0561_MASS * 1000:.0f} g; sag {sag * 1000:.1f} um")
            m = top_bracket_taz()
            print(f"taz: top bracket: {m.volume / 1000:.1f} cm3, about {m.volume * 1.27e-3:.0f} g of PETG solid")
        print(f"{name}: arm lens at {np.round(mount_lens(name), 1).tolist()} (printer coordinates), "
              f"{np.linalg.norm(mount_lens(name) - C.lens_point(name)):.1f} mm from the C270 lens")
        bad = check_pi_end(name)
        print(f"{name}: plugs at the Pi: " + ("; ".join(bad) if bad else "no touch with each other, the pod, or the bracket"))
        for sp in cable_specs(name):
            route, need, buy, extra, crimp = cable_plan(sp)
            what = (f"buy {buy / 1000:g} m ({extra:.0f} mm extra to coil and tie)" if buy else "longer than 2 m")
            print(f"{name}: {sp['name']}: {route:.0f} mm of cable between the plugs on the route; with the plugs "
                  f"and {SERVICE:.0f} mm of service allowance {need:.0f} mm: {what}, or crimp to {crimp:.0f} mm")


if __name__ == "__main__":
    report()
