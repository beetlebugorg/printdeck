# Makes the meshes of the Printdeck pod and of the LulzBot Mini 2 for the 3D window (Sim3D.qml),
# from the model of the pod (lulzbot/pod.py) and the Mini 2 meshes in lulzbot/build/mini2:
#
#   cd lulzbot && uv run --no-project --with build123d --with trimesh --with numpy --with scipy \
#     --with networkx --with rtree --with shapely --with pyrender --with PyOpenGL \
#     --with fast-simplification python ../desktop/cad/deck.py
#
# It needs lulzbot/build/workhorse (workhorse.py) and lulzbot/build/mini2 (the Mini 2 meshes from the
# hibiscus_assembled.fcstd of LulzBot).
#
# The files have the format of cad/export.py: triangles of 3 vertices of 10 bytes, the position as
# 3 int16 in steps of 1/50 mm and the normal as 3 int8.
#
# - pod_*.bin: the pod in pod coordinates (pod.py: u to the right, v up, w out of the glass), one file
#   for each color. The cables, their plugs and the screen rectangle are not drawn.
# - taz_*.bin: the TAZ bracket (with the wedge), its screw, the two rail clips with their screws and
#   T-nuts, and the cable clips on the cover, in the scene of the TAZ (cad/export.py).
# - mini2_*.bin: the Mini 2, its bracket, its screws and its cable clips, in the scene of the Mini 2
#   (mini_scene below). The printer is in four groups that move apart (frame, bed, gantry,
#   carriage), one file for each group and kind.
# - The "camera" group of each printer: the C270 webcam on its mount (camera.py).
#
# meshes/deck.json lists the files with their colors, the place of the pod on each printer (the
# position and the rotation of the pod coordinates in the scene, and the directions v and w of the pod
# in the scene), the active area of the display, and the places of the Mini 2 that Sim3D.qml needs.
import glob, json, math, os, re, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
LULZBOT = os.path.join(ROOT, "lulzbot")
OUT = os.path.join(HERE, "..", "meshes")
MINI_CFG = os.path.join(ROOT, "deck", "internal", "printer", "sim", "mini2.cfg")
sys.path.insert(0, LULZBOT)

import numpy as np
import trimesh
import fast_simplification
from build123d import export_stl

import pod as POD
import camera as C
import kitview as K
import parts as P
import shape as S

# The material of each color of pod.py: the metalness and the roughness. Other colors are plastic.
FINISH = {
    "#0D0E12": (0.2, 0.08),   # the cover glass
    "#8C9097": (0.6, 0.35),   # the metal back of the display
    "#C9CDD2": (0.7, 0.3),    # the standoffs
    "#30343A": (0.6, 0.4),    # the Active Cooler
    "#6C7078": (0.6, 0.4),    # the channel of the display back
    "#B9BEC6": (0.7, 0.3),    # the screws
    "#C9A227": (0.7, 0.3),    # the heat-set inserts
    "#C4C6C8": (0.7, 0.3),    # the T-nuts of the rail clips
    "#2A2A2C": (0.7, 0.35),   # the black screws of the rail clips
    "#1F4BD1": (0.1, 0.2),    # the screen of the Mini 2 LCD
    "#C4C8CF": (0.35, 0.4),   # the Mini 2 frame
    "#B0B4BC": (0.35, 0.4),   # the other Mini 2 parts
    "#C9A24B": (0.6, 0.3),    # the brass nozzle of the Mini 2
}


def finish(color):
    m, r = FINISH.get(color, (0.0, 0.6))
    return {"color": color, "metal": m, "rough": r}


def write_bin(name, tris):
    """Writes the triangles (n x 3 x 3, in the scene) to meshes/NAME.bin, and returns the size."""
    tris = np.asarray(tris, float)
    n = np.cross(tris[:, 1] - tris[:, 0], tris[:, 2] - tris[:, 0])
    ln = np.linalg.norm(n, axis=1)
    keep = ln > 1e-9
    tris, n = tris[keep], n[keep] / ln[keep][:, None]
    q = np.round(tris * 50)
    assert np.abs(q).max() < 32767, f"{name}: a vertex is out of the range of int16"
    rec = np.zeros((len(tris), 3), dtype=[("p", "<i2", 3), ("n", "i1", 3), ("x", "u1")])
    rec["p"] = q.astype("<i2")
    rec["n"] = np.round(n * 127).astype("i1")[:, None, :]
    data = rec.tobytes()
    with open(os.path.join(OUT, name + ".bin"), "wb") as f:
        f.write(data)
    return len(data)


def decimate(m, faces):
    """The mesh with about the number of faces, or the mesh when it has fewer."""
    if len(m.faces) <= faces:
        return m
    v, f = fast_simplification.simplify(np.asarray(m.vertices, np.float32), np.asarray(m.faces, np.int32),
                                        target_count=int(faces))
    return trimesh.Trimesh(v, f, process=False)


def transformed(m, T):
    m = m.copy()
    m.apply_transform(T)
    return m


def stl(shape, tmp, name, tol=0.05):
    p = os.path.join(tmp, name + ".stl")
    export_stl(shape, p, tolerance=tol, angular_tolerance=0.2)
    return trimesh.load(p, force="mesh")


def concat(ms):
    return trimesh.util.concatenate(ms) if len(ms) > 1 else ms[0]


# ---------------------------------------------------------------- the scenes
# The scene of the TAZ (cad/export.py): a half turn about the vertical axis, and an offset.
TAZ_SCENE = np.array([[-1, 0, 0, 294.9], [0, 1, 0, 540.6], [0, 0, -1, 55.1], [0, 0, 0, 1]], float)


def mini_scene(tip, bed_top, offset_x):
    """The scene of the Mini 2 from its CAD (x to the right, y to the back, z up): a point of the
    printer (x, y, z) is (x, z, -y) in the scene, as on the TAZ. The top of the bed is at Y 0, the tip
    of the nozzle is in the plane Z 0, and the X of the scene is the X of the printer."""
    return np.array([[1, 0, 0, offset_x], [0, 0, 1, -bed_top], [0, -1, 0, tip[1]], [0, 0, 0, 1]], float)


def rotation(T):
    """The position and the rotation (w, x, y, z) of a rigid transform."""
    R = T[:3, :3]
    w = math.sqrt(max(0.0, 1 + R[0, 0] + R[1, 1] + R[2, 2])) / 2
    x = math.copysign(math.sqrt(max(0.0, 1 + R[0, 0] - R[1, 1] - R[2, 2])) / 2, R[2, 1] - R[1, 2])
    y = math.copysign(math.sqrt(max(0.0, 1 - R[0, 0] + R[1, 1] - R[2, 2])) / 2, R[0, 2] - R[2, 0])
    z = math.copysign(math.sqrt(max(0.0, 1 - R[0, 0] - R[1, 1] + R[2, 2])) / 2, R[1, 0] - R[0, 1])
    return [round(float(c), 4) for c in T[:3, 3]], [round(c, 6) for c in (w, x, y, z)]


# ---------------------------------------------------------------- the pod
# The parts of hardware() that are not drawn: the cables and their plugs, and the screen rectangle
# (the deck UI covers the active area).
POD_SKIP = {"screen", "power_plug", "printer_plug", "dsi_ribbon", "power_wire"}


def pod_meshes(tmp):
    """The pod in pod coordinates: {color: mesh}. The Pi is the boxes of its board and its ports
    (pod.pi_parts): the STEP model of the Pi has 1.6 million faces, and most of it is in the case."""
    by = {}
    for name, shape, c in [("pod_body", POD.body(), "#1B1C20")] + POD.hardware():
        if name not in POD_SKIP:
            by.setdefault(c, []).append(stl(shape, tmp, name))
    # The printed parts keep their faces. The screws and the inserts have many small faces.
    return {c: decimate(concat(ms), 30000 if c in ("#1B1C20", "#2B2E35", "#2A2B30") else 5000)
            for c, ms in by.items()}


def taz_parts(tmp):
    """The parts on the TAZ in the mount coordinates of pod.py: (group, color, mesh)."""
    out = [("bracket", "#2A2B30", stl(POD.bracket(), tmp, "taz_bracket", 0.03)),
           ("bracket_screws", "#B9BEC6", stl(POD.bracket_screws("taz"), tmp, "taz_bracket_screws"))]
    # The bracket stands on two rail clips (parts.clip) at pod.CLIP_Y, with their screws and T-nuts.
    keep = S.CLIPS_Y
    S.CLIPS_Y = POD.CLIP_Y
    try:
        clips = concat([stl(P.clip(y), tmp, f"rail_clip{i}") for i, y in enumerate(POD.CLIP_Y)])
        screws = concat([stl(s, tmp, n) for n, s, _ in P.clip_screws()])
        nuts = concat([stl(s, tmp, n) for n, s, _ in P.hammer_nuts()])
    finally:
        S.CLIPS_Y = keep
    out += [("rail_clips", "#E8762B", clips), ("rail_clip_screws", "#2A2A2C", screws),
            ("rail_clip_screws", "#C4C6C8", nuts)]
    for n, shape, _ in POD.clips("taz"):
        out.append((n, "#E8762B", stl(shape, tmp, "taz_" + n)))
    return out


def mini_parts(tmp):
    """The parts on the Mini 2 in the coordinates of its CAD: (group, color, mesh)."""
    out = [("bracket", "#2A2B30", stl(POD.mini_bracket(), tmp, "mini2_bracket", 0.03)),
           ("bracket_screws", "#B9BEC6", stl(POD.bracket_screws("mini2"), tmp, "mini2_bracket_screws"))]
    for n, shape, _ in POD.clips("mini2"):
        out.append((n, "#E8762B", stl(shape, tmp, "mini2_" + n)))
    return out


# More groups of parts on each printer, in the frame of the CAD of the printer (the frame "world" of
# pod.setups: the CAD of LulzBot for the TAZ, the Mini 2 meshes for the Mini 2). Each item is a function
# f(tmp) that returns a list of (group, color, mesh), where the mesh is a trimesh mesh or a build123d
# shape. The parts go on the frame of the printer, in the color of the item (FINISH gives the
# metalness and the roughness).
def camera_parts(name):
    """The C270 webcam and its mount on the frame of the printer (camera.py), as one group. camera.py
    has them in the printer frame of pod.setups, and its "world" matrix moves them to the CAD."""
    def f(tmp):
        world = POD.setups()[name]["world"]
        out = []
        for n, shape, c in C.parts(name):
            m = stl(shape, tmp, f"{name}_{n}")
            m.apply_transform(world)
            out.append(("camera", c, m))
        return out
    return f


EXTRA = {"taz": [camera_parts("taz")], "mini2": [camera_parts("mini2")]}


def extra_parts(name, tmp):
    out = []
    for f in EXTRA[name]:
        for i, (group, c, m) in enumerate(f(tmp)):
            out.append((group, c, m if isinstance(m, trimesh.Trimesh) else stl(m, tmp, f"{name}_{group}_{i}")))
    return out


# ---------------------------------------------------------------- the Mini 2
MINI = os.path.join(LULZBOT, "build", "mini2")
# The group of each file: the parts that move with the bed (Y), the gantry (Z) and the carriage
# (X on the gantry). The other files are the frame. The cable chains do not bend: the X chain moves
# with the gantry, and the Y and Z chains stay with the frame.
MINI_GROUP = {"hibiscus_y_axis_01": "bed", "x_gantry_01": "gantry", "x_chain_assem_01": "gantry",
              "x_carriage_01": "carriage", "aerostruder_v2_01": "carriage"}
# The faces of each file in the window, after the decimation.
MINI_FACES = {"hibiscus_frame_assembled_01": 40000, "hibiscus_y_axis_01": 16000, "hibiscus_y_axis_guide_01": 6000,
              "x_gantry_01": 20000, "x_carriage_01": 16000, "aerostruder_v2_01": 30000, "x_chain_assem_01": 5000,
              "y_chain_assem_01": 4000, "z_chain_assem_01": 4000, "z_axis_left_01": 6000, "z_axis_right_01": 6000,
              "lcd_assem_01": 8000, "belts_01": 3000}


def mini_config():
    """The probe points of bed_tilt and the z_offset of the probe in the sim config of the Mini 2."""
    text = open(MINI_CFG).read()
    sec = re.search(r"^\[bed_tilt\]\n(.*?)^speed", text, re.S | re.M).group(1)
    pts = [tuple(map(float, m)) for m in re.findall(r"(-?[\d.]+)\s*,\s*(-?[\d.]+)", sec)]
    z_off = float(re.search(r"^\[probe\]\n(?:.*\n)*?z_offset:\s*([-\d.]+)", text, re.M).group(1))
    return pts, z_off


def mini_printer():
    """The Mini 2 meshes in the coordinates of its CAD: {(group, kind): [meshes]}, and the places
    that Sim3D.qml needs."""
    parts = {}
    total = {}
    for f in sorted(glob.glob(os.path.join(MINI, "*.stl"))):
        name = os.path.basename(f)[:-4]
        group = MINI_GROUP.get(name, "frame")
        m = trimesh.load(f, force="mesh")
        total[name] = len(m.faces)
        for b in m.split(only_watertight=False):
            e, lo = b.extents, b.bounds[0]
            g, kind = group, "metal"
            if name == "hibiscus_y_axis_01" and e[0] > 160 and e[1] > 160 and e[2] < 4 and lo[2] > 80:
                kind = "surface"                     # the PEI sheet, 170 x 170 x 3
            elif name == "hibiscus_y_axis_01" and 20 < e[0] < 24 and 20 < e[1] < 24 and e[2] < 2:
                kind = "washer"                      # the washers of the probe, on the corners
            elif name == "aerostruder_v2_01" and lo[2] < m.bounds[0][2] + 1.0:
                kind = "nozzle"                      # the nozzle: the lowest piece
            elif name == "aerostruder_v2_01" and 20 < e[0] < 25 and lo[2] < m.bounds[0][2] + 7.5 and e[2] < 15:
                kind = "heater"                      # the heater block over it
            elif name == "lcd_assem_01":
                kind = "screen" if e[1] < 1.5 and e[0] > 80 else "lcd"
            elif name == "belts_01":
                # The clamps of the X belt go with the carriage, the X belt and its pulleys with the
                # gantry. The Y belt and the Z belts stay with the frame.
                if e[0] < 15 and 268 < lo[2] < 275 and -215 < lo[0] < -100:
                    g = "carriage"
                elif lo[2] > 200 and e[2] < 25:
                    g = "gantry"
            parts.setdefault((g, kind, name), []).append(b)
    # The places in the CAD: the tip of the nozzle, the top of the PEI, the washers and the screen
    # window of the LCD.
    v = concat(parts[("carriage", "nozzle", "aerostruder_v2_01")]).vertices
    tip = v[v[:, 2] < v[:, 2].min() + 0.05]
    places = {"tip": [float(tip[:, 0].mean()), float(tip[:, 1].mean()), float(v[:, 2].min())],
              "bed_top": float(concat(parts[("bed", "surface", "hibiscus_y_axis_01")]).bounds[1][2])}
    ws = parts[("bed", "washer", "hibiscus_y_axis_01")]
    places["washers"] = [[float(c) for c in w.bounds.mean(axis=0)] for w in ws]
    places["washer_top"] = float(max(w.bounds[1][2] for w in ws))
    places["screen"] = lcd_window(parts)
    # Each piece of a file gets its share of the faces of the file. A small piece (the PEI sheet, the
    # nozzle) keeps up to 2000 faces.
    out = {}
    for (g, kind, name), bs in parts.items():
        m = concat(bs)
        share = MINI_FACES.get(name, 8000) * len(m.faces) / total[name]
        out.setdefault((g, kind), []).append(decimate(m, max(share, min(len(m.faces), 2000))))
    return out, places


def lcd_window(parts):
    """The window of the stock LCD: the part of the screen plate that shows through the bezel. Rays
    from the front find it. Returns the center (x, y, z) on the front of the plate and the size."""
    screen = concat(parts[("frame", "screen", "lcd_assem_01")])
    lcd = concat([screen] + parts[("frame", "lcd", "lcd_assem_01")])
    lo, hi = screen.bounds
    X, Z = np.meshgrid(np.arange(lo[0], hi[0], 0.25), np.arange(lo[2], hi[2], 0.25))
    o = np.c_[X.ravel(), np.full(X.size, lo[1] - 40.0), Z.ravel()]
    d = np.tile([0.0, 1.0, 0.0], (len(o), 1))
    loc, ri, _ = lcd.ray.intersects_location(o, d, multiple_hits=False)
    p = o[ri[np.abs(loc[:, 1] - lo[1]) < 0.3]]
    x0, x1, z0, z1 = p[:, 0].min() - 0.125, p[:, 0].max() + 0.125, p[:, 2].min() - 0.125, p[:, 2].max() + 0.125
    return {"center": [float((x0 + x1) / 2), float(lo[1]), float((z0 + z1) / 2)],
            "size": [round(float(x1 - x0), 2), round(float(z1 - z0), 2)]}


MINI_COLOR = {"surface": "#B8923F", "nozzle": "#C9A24B", "heater": "#C4C6C8", "lcd": POD.MINI_LCD_COLOR, "screen": POD.MINI_LCD_SCREEN_COLOR}


def main():
    os.makedirs(OUT, exist_ok=True)
    for name in os.listdir(OUT):
        if name.startswith(("deck_", "pod_", "taz_", "mini2_")) and name.endswith(".bin"):
            os.remove(os.path.join(OUT, name))
    sizes = {}
    index = {"pod": []}
    tmp = tempfile.mkdtemp(prefix="printdeck-deck-")

    # The pod, in pod coordinates.
    for c, m in sorted(pod_meshes(tmp).items()):
        name = "pod_" + c[1:].lower()
        sizes[name] = write_bin(name, m.triangles)
        index["pod"].append({"mesh": name, **finish(c)})
    # The active area of the display, on the front of the glass (w = 0). The deck UI goes there.
    index["screen"] = {"position": [0.0, POD.GLASS[1] / 2, 0.0], "size": list(POD.AA)}

    # The TAZ: the pod and its parts in the frame of the mount, the CAD (pod.setups), then the scene.
    mount = K.mount_matrix()
    pod_to_scene = TAZ_SCENE @ mount @ POD.matrix(POD.POD_LOC)
    pos, rot = rotation(pod_to_scene)
    up, front = (pod_to_scene[:3, :3] @ np.eye(3)[:, 1:]).T.round(4).tolist()
    index["taz"] = {"position": pos, "rotation": rot, "up": up, "front": front, "parts": []}
    by = {}
    for group, c, m in taz_parts(tmp):
        by.setdefault((group, c), []).append(transformed(m, TAZ_SCENE @ mount))
    for group, c, m in extra_parts("taz", tmp):
        by.setdefault((group, c), []).append(transformed(m, TAZ_SCENE))
    for (group, c), ms in sorted(by.items()):
        name = f"taz_{group}_{c[1:].lower()}"
        sizes[name] = write_bin(name, concat(ms).triangles)
        index["taz"]["parts"].append({"group": group, "mesh": name, **finish(c)})

    # The Mini 2. The middle of the washers of the CAD is the middle of the probe points of the sim
    # config (the sim washers are 4 mm out from those points).
    printer, pl = mini_printer()
    pts, z_off = mini_config()
    probe_mid = np.mean(np.array(pts), axis=0)
    wash_mid = np.mean(np.array(pl["washers"]), axis=0)
    offset_x = float(probe_mid[0] - wash_mid[0])     # printer X = CAD x + offset_x
    offset_y = float(probe_mid[1] - wash_mid[1])     # printer Y of the bed = CAD y + offset_y
    T = mini_scene(pl["tip"], pl["bed_top"], offset_x)
    tip = (T @ np.array([*pl["tip"], 1.0]))[:3]
    scr = (T @ np.array([*pl["screen"]["center"], 1.0]))[:3]
    pod_to_scene = T @ POD.matrix(POD.MINI_LOC)
    pos, rot = rotation(pod_to_scene)
    up, front = (pod_to_scene[:3, :3] @ np.eye(3)[:, 1:]).T.round(4).tolist()
    index["mini2"] = {
        "position": pos, "rotation": rot, "up": up, "front": front,
        # The tip of the nozzle in the scene at the pose of the CAD.
        "tip": [round(float(c), 3) for c in tip],
        # At the pose, the point of the bed at printer Y y is at Z bedY - y of the scene.
        "bedY": round(float(pl["tip"][1] + offset_y), 3),
        # The tops of the washers over the top of the bed: the CAD, and the sim (the z_offset).
        "washerTop": round(pl["washer_top"] - pl["bed_top"], 3), "washerSim": z_off,
        # The window of the LCD: its middle on the front of the screen, and its size.
        "lcd": {"position": [round(float(c), 3) for c in scr], "size": pl["screen"]["size"]},
        "parts": []}
    by = {}
    for (g, kind), ms in printer.items():
        c = MINI_COLOR.get(kind, "#C4C8CF" if g == "frame" else "#B0B4BC")
        by.setdefault((g, kind, c), []).extend(transformed(m, T) for m in ms)
    # The pod parts and the extra parts are on the frame: their group is the kind.
    for group, c, m in mini_parts(tmp) + extra_parts("mini2", tmp):
        by.setdefault(("frame", group, c), []).append(transformed(m, T))
    for (g, kind, c), ms in sorted(by.items()):
        name = f"mini2_{g}_{kind}" if kind in ("metal", "surface", "washer", "nozzle", "heater", "lcd", "screen") \
            else f"mini2_{g}_{kind}_{c[1:].lower()}"
        sizes[name] = write_bin(name, concat(ms).triangles)
        index["mini2"]["parts"].append({"group": g, "kind": kind, "mesh": name, **finish(c)})

    with open(os.path.join(OUT, "deck.json"), "w") as f:
        json.dump(index, f, indent=1)
    for prefix in ("pod_", "taz_", "mini2_"):
        n = sum(s for k, s in sizes.items() if k.startswith(prefix))
        print(f"{prefix}*.bin: {sum(1 for k in sizes if k.startswith(prefix))} files, {n / 1e6:.2f} MB")
    print("Mini 2 CAD:", pl, "probe middle", probe_mid.tolist(), "z_offset", z_off)
    print(json.dumps({k: v for k, v in index.items() if k not in ("pod",)}, indent=None)[:3000])


if __name__ == "__main__":
    main()
