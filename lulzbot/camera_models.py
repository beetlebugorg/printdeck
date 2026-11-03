"""Models of the camera to buy, and its bracket. The camera is an Arducam B0561 16 MP autofocus camera
on the arm of camera.py, at the front right corner of the printer, aimed at the bed from above. The sizes in
B0561 are caliper measurements where the comment says "measured". The other sizes come from two
product photos: a front view with two official sizes (36 mm and 44 mm), and a perspective view. ELP
has the ELP-USB12MP01-L281 board for comparison only. Its sizes are guesses, and the model does not
place it.

The B0561 is a black metal case with a raised front plate, a short lens barrel at the middle of the
face, and the autofocus block at the right of the barrel. A U bracket of sheet metal holds the case
on two pivot screws at its sides. The round base of the U has a through 1/4"-20 thread at its
center. The JST B4B-ZR socket is on the back of the case, and the cable plugs straight in from the
back.

The link holds the base of the U with one 1/4"-20 x 1/4" screw. The pivot of the U sets the tilt.
The link is a printed part (camera-link_fdm.stl), the same for the TAZ and the Mini 2. Its tongue goes
between the lugs of the arm, where the eyelet of the C270 was in the first design of the arm. The M3 x
16 hinge screw goes through the lugs and the tongue, and holds the link level. The level plate of the
link is in front of the arm. The camera hangs under the plate and tilts down on its pivot. The link
prints on the top face of its plate, with no supports.

The camera has its own cable to a USB-A port of the Pi in the pod (CAMERA_PORTS), along the route of
the C270 cable. report() prints the length of the route and the cable length to buy (cable_plan()).

Camera coordinates: the origin is the middle of the front face of the case (under the front plate),
z out of the lens along the optical axis, y up in the picture, x = y cross z. lens_point() is the
middle of the front of the lens barrel: the views of live.py and camera_preview.py start there (the
pinhole of camera_options.py). At 300 mm and more from the bed, the true optical center a few mm
behind it changes the views very little. Bracket coordinates: the origin is the middle of the bottom
face of the base, y from the base to the pivot, x along the pivot axis.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with shapely --with rtree --with pyrender --with PyOpenGL --with pillow python camera_models.py
It prints the fit of the camera on the arm over its tilt range, the gaps to the printers, and the
cable lengths.
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

# The link on the arm (hinge coordinates of camera.py: the pin along x, z forward, at no tilt). The
# tongue has the width of the C270 eyelet, so it fits between the lugs with the same gaps. It goes
# from under the pin axis up to the top face of the plate, so it stands on the bed with the plate.
LINK_TONGUE = (C.EYELET[0], C.EYELET[1], C.EYELET[2])   # the width, the height under the plate top, the depth behind the pin
LINK_PLATE = (44.0, 4.0)                # the plate: width (x) and thickness (y); it goes from the lugs to past the base
LINK_Z = C.LUG_TOP_Z + 1.0              # the back edge of the plate, 1 mm in front of the lugs
# The 1/4"-20 x 1/4" screw from the top of the plate into the thread of the base: a socket cap head,
# 3/8" across and 1/4" high. It goes through the 4 mm plate and the 2 mm boss, and through the
# 1.4 mm base, 0.95 mm into the U.
BASE_SCREW = dict(d=6.35, length=6.35, head=(9.53, 6.35))
LINK_PRINT = Rot(-90, 0, 0)             # the print position: the top face of the plate on the bed

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
    the U is under the plate (hinge y = 0). The hinge hole has a teardrop point toward hinge -y: that
    is up in the print position."""
    tw, th, tb = LINK_TONGUE
    pw, pt = LINK_PLATE
    zc = link_base_z()
    z1 = zc + B0561["base_d"] / 2 + 2.0
    # The tongue stops 0.5 mm short of the base of the U, under the plate.
    s = box(-tw / 2, tw / 2, -th / 2, pt, -tb, zc - B0561["base_d"] / 2 - 0.5)
    s = s + box(-pw / 2, pw / 2, 0.0, pt, LINK_Z, z1)
    s = s - C.teardrop(C.SCREW_HOLE / 2, tw + 2, (0, 0, 0), (1, 0, 0), (0, -1, 0))
    s = s - Pos(0, pt / 2, zc) * (Location(Plane.XZ) * Cylinder(B0561["base_hole"] / 2 + 0.2, pt + 2))
    return s


def link(name):
    """The link on the arm, in printer coordinates."""
    return C.hinge_loc(name) * link_hinge()


def base_screw_hinge():
    """The 1/4"-20 x 1/4" screw of the base, in hinge coordinates: the head on the top of the plate."""
    b = BASE_SCREW
    hd, hh = b["head"]
    top = LINK_PLATE[1]
    z = link_base_z()
    s = Pos(0, top + hh / 2, z) * (Location(Plane.XZ) * Cylinder(hd / 2, hh))
    s = s + Pos(0, top - b["length"] / 2, z) * (Location(Plane.XZ) * Cylinder(b["d"] / 2 - 0.05, b["length"]))
    return s - Pos(0, top + hh, z) * (Location(Plane.XZ) * Cylinder(hd * 0.22, hh * 1.2))


def base_screw(name):
    return C.hinge_loc(name) * base_screw_hinge()


def arm_mount(name, pitch=None):
    """[(part, shape, color)] of the camera on the arm and the link, in printer coordinates."""
    cam, br = arm_frame(name, pitch)
    return ([("camera_link", link(name), C.ORANGE), ("camera_base_screw", base_screw(name), P.SCREW_COLOR)]
            + camera_parts(cam, br, "mount"))


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
# The camera has its own 1 m cable, from the JST plug at the camera to a USB-A plug in a port of the
# Pi. The two USB-A stacks of the Pi 5 (pi_parts in deckview.py) each have a lower and an upper port.
# The printer cable is in the lower USB 2 port, and the camera cable in the upper USB 2 port.
CAMERA_PORTS = {"mount": ("pi_usb2", True)}
# The USB-A end of the B0561 cable: the same sizes as the USB-A end of the printer cable (a common
# cable, unmeasured). 30 mm out of the port.
USBA_OUT = 30.0
# In the two-cable tape clips of camera.cable_clips (clips.tape_clip(pair=True)), the camera cable is
# 3.5 mm to one side of the middle of the clip.
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
    of the plug along the port (pod v)."""
    import numpy as np
    T = P.matrix(C.pod_loc(name))
    _, e = pi_plug(where)
    e = np.array(e)
    v = np.array([0.0, 1.0, 0.0])
    pts = [e + 60 * v, e + 20 * v, e]
    return [(T @ np.append(q, 1.0))[:3] for q in pts]


# Dark red: the cable of the camera on the arm.
CABLE_COLORS = {"camera_cable": "#8B2A2A", "camera_ext_cable": "#3A3A3A"}


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
    cam = arm_frame(name)[0]
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
        return out
    zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
    line = [np.array([-30.0, -244.0, zt]), np.array([-300.0, -244.0, zt])]
    tail = pi_tail(name, "mount")
    pts = pre + [c270[-6]] + line + tail
    flags = [False] * (len(pre) + 1) + [True] * 2 + [False] * len(tail)
    out["camera_cable"] = (pts, flags, SIDE, d0, into)
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
    """The plugs at the Pi (the camera plug, the printer plug, the power plug) against each other
    and against the pod (the body, the frame, the mount) and the bracket, in pod coordinates. Returns a
    list of problems."""
    sh = {"camera plug": pi_plug("mount")[0]}
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
    on the arm ("camera"), the bed, and T. The bed has "back": the direction to the back of the
    printer."""
    import numpy as np
    mv = lambda p: [float(x) for x in (T @ np.append(p, 1.0))[:3]]
    rot = lambda d: [float(x) for x in T[:3, :3] @ np.asarray(d, float)]
    pl = C.PLACES[name]
    f = C.look(name, pl["yaw"], pl["pitch"])
    up = np.cross(C.unit(np.cross(f, C.axes(name)[0])), f)
    eye = mount_lens(name)
    mount = {"eye": mv(eye), "focal": mv(eye + 300.0 * f), "up": rot(up), "fov": 2 * C.half_fov()[1]}
    bed = C.bed_view(name, T)
    bed["back"] = rot(C.axes(name)[1])
    return {"camera": mount, "bed": bed,
            "printer_to_view": [[float(x) for x in row] for row in T]}


# The C270 parts that the camera on the arm replaces (the arm, the clamp, the screws, and the clips
# stay). The camera has its own cable and USB-A plug.
C270_PARTS = ("camera_body", "camera_lens", "camera_cable_end", "camera_cable", "camera_plug")


def meshes(name, tmp, cam=None):
    """The meshes of the live window: the C270 mount parts of camera.meshes (cam) less the C270 itself,
    and the camera with its bracket, in printer coordinates: {name: (mesh, color)}."""
    import trimesh
    from build123d import export_stl
    out = {k: v for k, v in (cam or {}).items() if k not in C270_PARTS}
    for k, sh, c in arm_mount(name):
        p = os.path.join(tmp, f"{name}_{k}.stl")
        export_stl(sh, p, tolerance=0.03, angular_tolerance=0.1)
        out[k] = (trimesh.load(p, force="mesh"), c)
    p = os.path.join(tmp, f"{name}_camera_plug.stl")
    export_stl(plug_printer(name, "mount")[0], p, tolerance=0.05, angular_tolerance=0.2)
    out["camera_plug"] = (trimesh.load(p, force="mesh"), "#151515")
    return out


# ---------------------------------------------------------------- the checks
def check_arm(name):
    """The link against the arm, the clamp, and the two screws. The case and its keepout on the arm
    over the tilt range, against the U bracket, the link, the arm, the clamp, and the screws. Returns
    a list of problems."""
    bad = []
    fixed = [("link", link(name)), ("arm", C.arm(name)), ("clamp", C.CLAMPS[name]()),
             ("hinge screw", C.hinge_screw(name)), ("base screw", base_screw(name))]
    for k, sh in fixed[1:]:
        v = (sh & fixed[0][1]).volume
        if v > 0.01:
            bad.append(f"the {k} and the link: {v:.2f} mm3")
    _, br = arm_frame(name)
    u, _ = bracket_model()
    for k, sh in fixed:
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


def report():
    import tempfile
    import numpy as np
    import trimesh
    from build123d import export_stl
    for name in ("taz", "mini2"):
        bad = check_arm(name)
        print(f"{name}: camera on the arm: " + ("; ".join(bad) if bad else "no touch over the tilt range"))
        printer = P.setups()[name]["printer"]()
        printer = printer + C.stock_lcd(name, printer)
        world = P.setups()[name]["world"]
        tmp = tempfile.mkdtemp()
        ms = []
        for k, sh, _ in arm_mount(name):
            f = os.path.join(tmp, f"{k}.stl")
            export_stl(sh, f, tolerance=0.1, angular_tolerance=0.3)
            ms.append(trimesh.load(f, force="mesh"))
        gap = P.clearance(ms, world, printer, n=8000)
        print(f"{name}: camera on the arm, its bracket, and the link: {gap:.1f} mm from the printer")
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
