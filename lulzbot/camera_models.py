"""Placeholder models of the cameras to buy, and placeholder brackets for them. Two Arducam B0561
16 MP autofocus cameras: one on the arm of the C270 mount, one straight down over the middle of the
bed. Neither vendor publishes a drawing or a STEP file, so every size in DIMS is a guess from typical
boards of this kind. Measure each one with calipers when the cameras arrive and replace the value.
DIMS also has the ELP-USB12MP01-L281 with a 6 mm M12 lens, for comparison only.

- On the arm (camera.py), in place of the C270: a link plate has a tongue on the hinge pin, where the
  eyelet of the C270 is, and four bosses for the board. The connector and its cable go to the side
  away from the post of the arm.
- Over the bed: a plate on the top of the frame. The autofocus module goes down through a hole in
  the plate, and its front is flush with the bottom face of the plate.
  - TAZ: the plate spans the two top rails (2020, front and back), on their top faces. It is above
    the X axis at the full print height (report()).
  - Mini 2: the plate is on the top face of the top front rail and goes back past its back edge,
    over the middle of the bed.

The brackets are placeholders for the layout and the views: they are not a final design, and there
are no STL files of them.

The B0561 has a 1 m cable from its JST B4B-ZR plug to a USB-A plug (Arducam). cable_route() is the
route of each cable to the USB port of the Pi in the pod, along the route of the C270 cable.

Camera coordinates (each camera): the origin is the middle of the front face of the board (the lens
side), z out of the lens along the optical axis, y up in the picture, x = y cross z. lens_point() is
the middle of the front of the lens: the views of live.py and camera_preview.py start there (the
pinhole of camera_options.py). At 300 mm and more from the bed, the true optical center a few mm
behind it changes the views very little.

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

# ---------------------------------------------------------------- the cameras (all values are guesses)
# board: width (x), height (y), thickness. holes: spacing (square), diameter. lens: the M12 holder
# (x, y, height over the board), and the lens (diameter, length, the length inside the holder).
# module: the autofocus module (x, y, height over the board). connector: on the back of the board
# (x, y, height behind the board) and its middle (x, y). plug: the space behind the connector for
# the plug. cable: diameter and bend radius; the cable leaves the plug straight back and bends away
# from the middle of the board, toward the nearest edge.
DIMS = {
    "elp": dict(
        board=(38.0, 38.0, 1.6),          # guess: ELP lists a 38 x 38 mm board; 1.6 mm is a typical PCB
        holes=(34.0, 2.2),                # guess: M2 holes at the corners; some ELP boards have them 32 mm apart
        holder=(14.0, 14.0, 12.0),        # guess: a typical M12 lens holder
        lens=(14.0, 17.0, 8.0),           # guess: a 6 mm M12 lens is 15 to 20 mm long; 8 mm in the holder thread
        connector=(6.0, 10.0, 6.0, (15.5, 0.0)),    # guess: 4-pin 2.0 mm wire-to-board header at the middle of one side
        plug=8.0,                         # guess: the plug and its strain relief
        cable=(4.0, 15.0),                # guess: a round USB 2.0 cable
    ),
    "b0561": dict(
        board=(32.0, 32.0, 1.6),          # guess: Arducam lists a 32 x 32 mm board
        holes=(28.0, 2.2),                # guess: M2 holes at the corners
        module=(10.0, 10.0, 6.0),         # guess: a typical autofocus module, 3.95 mm lens
        connector=(3.5, 7.5, 4.6, (14.0, 0.0)),     # guess: JST B4B-ZR (1.5 mm pitch, top entry) at the middle of one side
        plug=6.0,                         # guess: the ZHR-4 plug and its wires
        cable=(3.0, 10.0),                # guess: the 4-wire cable of the plug
        cable_length=1000.0,              # Arducam: the USB-A to JST B4B-ZR cable is 1 m
    ),
}
CAMERA_COLORS = {"board": "#1E5C3A", "lens": "#202226", "connector": "#F2F2EE", "keepout": "#D9534F"}
BRACKET_COLOR = "#8E6CC8"               # placeholder brackets: violet, so they read as not final

# The camera of each position (kind in DIMS).
MOUNT_CAMERA = "b0561"
TOP_CAMERA = "b0561"

# The link on the C270 arm (hinge coordinates of camera.py: the pin along x, z forward).
LINK_TONGUE = (C.EYELET[0], C.EYELET[1], C.EYELET[2])   # the width, height, and depth behind the pin of the C270 eyelet
LINK_MARGIN = 2.0                       # the plate is this much larger than the board on each side
LINK_T = 3.0
LINK_PLATE_Z = C.LUG_TOP_Z + 1.0        # the back face of the plate, 1 mm in front of the lugs
STANDOFF = (5.0, 3.0)                   # the bosses under the board: diameter, height
# The turn of the board about its lens axis on each printer: the connector and the cable go to the
# side away from the post of the arm. On the Mini 2 the picture is upside down: the software turns it
# 180 degrees.
MOUNT_ROLL = {"taz": 0.0, "mini2": 180.0}
HINGE_HOLE = C.SCREW_HOLE

# The top plates. TAZ: the two top rails are 2020 at x -84 to -64 and 66 to 86, y 466 to 486
# (measured in the model, at the middle of the bed). Mini 2: the top front rail has its top face at
# z 373 (measured in the model).
TAZ_RAILS = dict(top=486.0, x=((-84.0, -64.0), (66.0, 86.0)))
MINI_RAIL_TOP = 373.0
TOP_PLATE_T = {"taz": 4.0, "mini2": 3.0}
TOP_PLATE_W = 40.0                      # across the bed (along the rails)
# The module goes through the plate. Its front is flush with the bottom face: module height = plate +
# standoff, so the standoff is the rest.
# The full print height (published LulzBot sizes, not checked on the printers) and the moving parts
# that go up with the tool head.
PRINT_Z = {"taz": 285.0, "mini2": 180.0}
GANTRY = {"taz": ("x_axis", "aerostruder", "extruder_cap"),
          "mini2": ("x_gantry", "aerostruder", "x_carriage", "x_chain")}


def box(x0, x1, y0, y1, z0, z1):
    return C.box(x0, x1, y0, y1, z0, z1)


def lens_point(kind):
    """The middle of the front of the lens, in camera coordinates."""
    d = DIMS[kind]
    if kind == "elp":
        hw, hh, hz = d["holder"]
        ld, ll, lin = d["lens"]
        return (0.0, 0.0, hz - lin + ll)
    return (0.0, 0.0, d["module"][2])


def model(kind):
    """The camera in camera coordinates: {part: shape}. The keepout is the plug and the bend of the
    cable: keep the bracket out of it."""
    d = DIMS[kind]
    bw, bh, bt = d["board"]
    sp, hd = d["holes"]
    board = box(-bw / 2, bw / 2, -bh / 2, bh / 2, -bt, 0.0)
    for sx in (-1, 1):
        for sy in (-1, 1):
            board = board - Pos(sx * sp / 2, sy * sp / 2, -bt / 2) * Cylinder(hd / 2, bt + 1.0)
    if kind == "elp":
        hw, hh, hz = d["holder"]
        ld, ll, lin = d["lens"]
        lens = box(-hw / 2, hw / 2, -hh / 2, hh / 2, 0.0, hz) + Pos(0, 0, hz - lin + ll / 2) * Cylinder(ld / 2, ll)
    else:
        mx, my, mz = d["module"]
        lens = box(-mx / 2, mx / 2, -my / 2, my / 2, 0.0, mz)
    cw, ch, cz, (cx, cy) = d["connector"]
    con = box(cx - cw / 2, cx + cw / 2, cy - ch / 2, cy + ch / 2, -bt - cz, -bt)
    cd, R = d["cable"]
    z0 = -bt - cz - d["plug"]
    keep = box(cx - cw / 2, cx + cw / 2, cy - ch / 2, cy + ch / 2, z0, -bt - cz)
    # The bend: the cable goes straight back, then out toward the nearest edge. The keepout is the box
    # of the bend.
    if abs(cx) > abs(cy):
        sx = math.copysign(1.0, cx)
        xa, xb = sorted((cx - sx * cd, cx + sx * (R + cd)))
        keep = keep + box(xa, xb, cy - cd, cy + cd, z0 - R - cd, z0)
    else:
        sy = math.copysign(1.0, cy)
        ya, yb = sorted((cy - sy * cd, cy + sy * (R + cd)))
        keep = keep + box(cx - cd, cx + cd, ya, yb, z0 - R - cd, z0)
    return {"board": board, "lens": lens, "connector": con, "keepout": keep}


def hole_points(kind):
    sp = DIMS[kind]["holes"][0] / 2
    return [(sx * sp, sy * sp) for sx in (-1, 1) for sy in (-1, 1)]


# ---------------------------------------------------------------- the camera on the C270 arm
def mount_hinge(name):
    """The camera frame on the arm in hinge coordinates: the board on the bosses of the link."""
    bt = DIMS[MOUNT_CAMERA]["board"][2]
    return Pos(0, 0, LINK_PLATE_Z + LINK_T + STANDOFF[1] + bt) * Rot(0, 0, MOUNT_ROLL[name])


def link_hinge(name):
    """The link plate in hinge coordinates: the tongue between the lugs of the arm, with the hole of
    the M3 hinge screw, and the plate with four bosses for the board and a window for the plug."""
    tw, th, tb = LINK_TONGUE
    bw, bh, _ = DIMS[MOUNT_CAMERA]["board"]
    px, py, pt = bw + 2 * LINK_MARGIN, bh + 2 * LINK_MARGIN, LINK_T
    z0 = LINK_PLATE_Z
    s = box(-tw / 2, tw / 2, -th / 2, th / 2, -tb, z0 + 0.5)
    s = s + box(-px / 2, px / 2, -py / 2, py / 2, z0, z0 + pt)
    for hx, hy in hole_points(MOUNT_CAMERA):
        s = s + Pos(hx, hy, z0 + pt + STANDOFF[1] / 2) * Cylinder(STANDOFF[0] / 2, STANDOFF[1])
        s = s - Pos(hx, hy, z0 + pt) * Cylinder(0.8, 2 * (pt + STANDOFF[1]))      # M2 self-tapping pilot
    s = s - Location(Plane.YZ) * Cylinder(HINGE_HOLE / 2, tw + 2)
    m = model(MOUNT_CAMERA)
    return s - padded_box(mount_hinge(name) * (m["connector"] + m["keepout"]), 1.0)


def padded_box(sh, pad):
    """The bounding box of sh, pad larger on each side, as a solid."""
    b = sh.bounding_box()
    return box(b.min.X - pad, b.max.X + pad, b.min.Y - pad, b.max.Y + pad, b.min.Z - pad, b.max.Z + pad)


def arm_mount(name, pitch=None):
    """The camera on the arm and its link at the tilt pitch (the set tilt when None), in printer
    coordinates: [(part, shape, color)]."""
    L = C.cam_loc(name, pitch)
    E = L * mount_hinge(name)
    return ([("camera_link", L * link_hinge(name), BRACKET_COLOR)] +
            [(f"camera_mount_{k}", E * s, CAMERA_COLORS[k]) for k, s in model(MOUNT_CAMERA).items()])


def mount_lens(name):
    import numpy as np
    return np.array(tuple((C.cam_loc(name) * mount_hinge(name) * Pos(*lens_point(MOUNT_CAMERA))).position))


# ---------------------------------------------------------------- the camera over the bed
def top_frame(name):
    """(the camera frame over the bed as a Location in printer coordinates, the lens point)."""
    import numpy as np
    up, back, _ = C.axes(name)
    c = np.array(C.BEDS[name][0], float)
    bottom = TAZ_RAILS["top"] if name == "taz" else MINI_RAIL_TOP
    # The bed center, up to the bottom face of the plate. The frame axes: z down, y to the back.
    lens = c + (bottom - float(np.dot(c, up))) * up
    f = -up
    x = np.cross(back, f)
    origin = lens - f * lens_point(TOP_CAMERA)[2]
    return Location(Plane(origin=tuple(origin), x_dir=tuple(x), z_dir=tuple(f))), lens


def top_plate(name):
    """The plate on the top of the frame, in printer coordinates. In camera coordinates (z down) the
    bottom face of the plate is at the front of the module (the lens), and bosses hold the board over
    the plate."""
    import numpy as np
    L, lens = top_frame(name)
    _, back, _ = C.axes(name)
    t = TOP_PLATE_T[name]
    mz = DIMS[TOP_CAMERA]["module"][2]
    standoff = mz - t
    to_y = lambda p: float(np.dot(np.asarray(p, float) - lens, back))      # printer point to camera y
    if name == "taz":
        # From the back rail to the front rail, two M3 screws into a roll-in T-nut in each rail top.
        ys = [to_y((x, 0, 0)) for x in (TAZ_RAILS["x"][0][0], TAZ_RAILS["x"][1][1])]
        feet = [to_y(((a + b) / 2, 0, 0)) for a, b in TAZ_RAILS["x"]]
    else:
        # From y -250 on the top of the rail to y -150, past its back edge; two M3 screws into the rail.
        ys = [to_y((0, -250.0, 0)), to_y((0, -150.0, 0))]
        feet = [to_y((0, -235.0, 0))]
    ya, yb = sorted(ys)
    zt, zb = mz - t, mz
    s = box(-TOP_PLATE_W / 2, TOP_PLATE_W / 2, ya, yb, zt, zb)
    for hx, hy in hole_points(TOP_CAMERA):
        s = s + Pos(hx, hy, zt - standoff / 2) * Cylinder(STANDOFF[0] / 2, standoff)
        s = s - Pos(hx, hy, zt) * Cylinder(0.8, 2 * (t + standoff))      # M2 self-tapping pilot
    mx, my, _ = DIMS[TOP_CAMERA]["module"]
    s = s - box(-mx / 2 - 1.0, mx / 2 + 1.0, -my / 2 - 1.0, my / 2 + 1.0, zt - 1.0, zb + 1.0)
    for fy in feet:
        for fx in (-12.0, 12.0):
            s = s - Pos(fx, fy, (zt + zb) / 2) * Cylinder(1.7, t + 2)
    return L * s


def top_mount(name):
    L, _ = top_frame(name)
    return ([("camera_top_plate", top_plate(name), BRACKET_COLOR)] +
            [(f"camera_top_{k}", L * s, CAMERA_COLORS[k]) for k, s in model(TOP_CAMERA).items()])


# ---------------------------------------------------------------- the cables
def cable_route(name, where):
    """The route of the cable of the camera at where ("mount" or "top") to the USB port of the Pi, in
    printer coordinates: out of the plug, through the bend, then along the route of the C270 cable
    (camera.cable_points) and its clips."""
    import numpy as np
    import clips as K
    kind = MOUNT_CAMERA if where == "mount" else TOP_CAMERA
    d = DIMS[kind]
    bt = d["board"][2]
    _, _, cz, (cx, cy) = d["connector"]
    cd, R = d["cable"]
    z0 = -bt - cz - d["plug"]
    sx = math.copysign(1.0, cx)
    L = C.cam_loc(name) * mount_hinge(name) if where == "mount" else top_frame(name)[0]
    p = lambda x, y, z: np.array(tuple((L * Pos(x, y, z)).position))
    start = [p(cx, cy, -bt - cz), p(cx, cy, z0), p(cx + sx * R, cy, z0 - R)]
    c270 = C.cable_points(name)
    if where == "mount":
        return start + c270[2:]
    if name == "taz":
        # Up over the plate, to the front face of the top front rail, then along the C270 route.
        f = C.TAZ_UPRIGHT["front"] + K.TAPE_CABLE_H
        b = np.array(C.BEDS[name][0])
        return start + [np.array([f, TAZ_RAILS["top"] + 8.0, b[2]]), np.array([f, 476.0, b[2] + 20.0])] + c270[-6:]
    # Over the plate to the front edge of the top of the frame, then along the C270 route.
    zt = C.MINI_TOP_Z + K.TAPE_CABLE_H
    b = np.array(C.BEDS[name][0])
    return start + [np.array([b[0], -244.0, MINI_RAIL_TOP + TOP_PLATE_T[name] + 6.0]), np.array([b[0] - 30.0, -244.0, zt])] + \
        c270[-4:]


def cable(name, where):
    """(the mesh of the cable, its length in mm)."""
    kind = MOUNT_CAMERA if where == "mount" else TOP_CAMERA
    m, path = P.cable_mesh(cable_route(name, where), DIMS[kind]["cable"][0])
    return m, P.length(path)


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
    L, lens = top_frame(name)
    _, back, _ = C.axes(name)
    down = -np.asarray(C.axes(name)[0], float)
    top = {"eye": mv(lens), "focal": mv(lens + 300.0 * down), "up": rot(back), "fov": 2 * C.half_fov()[1]}
    return {"camera": mount, "camera_top": top, "bed": C.bed_view(name, T),
            "printer_to_view": [[float(x) for x in row] for row in T]}


# The C270 parts that the camera on the arm replaces (the arm, the clamp, the screws, the clips, and the
# USB plug in the Pi stay). camera_cable is the new cable.
C270_PARTS = ("camera_body", "camera_lens", "camera_cable_end", "camera_cable")


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
    out["camera_cable"] = (cable(name, "mount")[0], "#151515")
    out["camera_top_cable"] = (cable(name, "top")[0], "#151515")
    return out


# ---------------------------------------------------------------- the checks
def check_arm(name):
    """The camera on the arm, its keepout, and the link against the arm, the clamp, and the hinge screw
    over the tilt range. Returns a list of problems."""
    bad = []
    arm, clamp, screw = C.arm(name), C.CLAMPS[name](), C.hinge_screw(name)
    for p in C.pitches():
        for k, sh, _ in arm_mount(name, p):
            for n, other in (("arm", arm), ("clamp", clamp), ("hinge screw", screw)):
                if k == "camera_link" and n == "hinge screw":
                    continue
                v = (sh & other).volume
                if v > 0.01:
                    bad.append(f"{k} at {p:.0f} deg and the {n}: {v:.2f} mm3")
    return bad


def gantry_gap(name, solids):
    """The smallest vertical gap from the top of the moving parts at the full print height to the
    shapes, where they are over each other (mm)."""
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
        # Rays down from points on the shape: the first hit on the gantry.
        pts = m.sample(4000)
        locs, idx, _ = g.ray.intersects_location(pts, np.tile(-up, (len(pts), 1)), multiple_hits=False)
        if len(locs):
            best = min(best, float(((pts[idx] - locs) @ up).min()))
    return best, rise


def report():
    import numpy as np
    for name in ("taz", "mini2"):
        bad = check_arm(name)
        print(f"{name}: camera on the arm: " + ("; ".join(bad) if bad else "no touch over the tilt range"))
        printer = P.setups()[name]["printer"]()
        printer = printer + C.stock_lcd(name, printer)
        import trimesh
        import tempfile
        from build123d import export_stl
        tmp = tempfile.mkdtemp()
        for label, items in (("camera on the arm and link", arm_mount(name)), ("camera over the bed", top_mount(name)[1:])):
            ms = []
            for k, sh, _ in items:
                f = os.path.join(tmp, f"{k}.stl")
                export_stl(sh, f, tolerance=0.1, angular_tolerance=0.3)
                ms.append(trimesh.load(f, force="mesh"))
            gap = P.clearance(ms, P.setups()[name]["world"], printer, n=8000)
            print(f"{name}: {label}: {gap:.1f} mm from the printer")
        gap, rise = gantry_gap(name, [sh for _, sh, _ in top_mount(name)])
        L, lens = top_frame(name)
        h = float(np.dot(lens - np.array(C.BEDS[name][0]), C.axes(name)[0]))
        print(f"{name}: top lens {h:.1f} mm over the bed; at the full print height ({PRINT_Z[name]:.0f} mm, "
              f"the tool head {rise:.0f} mm up from the model) the moving parts are {gap:.1f} mm under the top plate "
              f"and the camera")
        print(f"{name}: arm lens at {np.round(mount_lens(name), 1).tolist()} (printer coordinates), "
              f"{np.linalg.norm(mount_lens(name) - C.lens_point(name)):.1f} mm from the C270 lens")
        for where, kind in (("mount", MOUNT_CAMERA), ("top", TOP_CAMERA)):
            _, n = cable(name, where)
            have = DIMS[kind].get("cable_length")
            print(f"{name}: {where} camera cable route {n:.0f} mm to the Pi" +
                  (f" (the cable is {have:.0f} mm: {'long enough' if have >= n else f'{n - have:.0f} mm short'})" if have else ""))


if __name__ == "__main__":
    report()
