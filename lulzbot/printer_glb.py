"""The 3D models of the printers for the printer pages of the cloud (docs/specs/printer-pages.md).

taz: the TAZ Workhorse with the deck pod, from the 3D window of the model (build/viewer/taz, made by
`pod.py live`). The printer meshes come from the LulzBot source files (ref/lulzbot, GPLv3 or later
and CC BY-SA 4.0), so taz.glb has the same licenses.

phenom: the Peopoly Phenom, from boxes. No CAD file of the Phenom is in the repo, so the sizes
are approximate: the case, the UV screen, the vat, the build plate and the Z column.

Each GLB is in mm, with Y up and the front of the printer toward +Z. The node
"bed" has its origin at the middle of the bed. Its X is the X of the G-code, its -Z is the Y of
the G-code (to the back), and its Y is the Z of the G-code (up).

Run: uv run --no-project --with trimesh --with fast-simplification --with numpy python printer_glb.py
(writes ../cloud/deploy/web/assets/models/<printer>.glb)
"""

import json
import os
import sys

import numpy as np
import trimesh

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "cloud", "deploy", "web", "assets", "models")

# The parts of the TAZ window that go in the model: the printer and the outside of the pod.
TAZ_PARTS = ("printer", "pod_body", "glass", "screen", "pod_frame", "pod_mount", "bracket", "printer_plug",
             "cover_clips", "printer_cable", "power_cable", "usb_b_plug")

# The count of the triangles of a model, at most.
MAX_TRIANGLES = 150_000

# The coordinates of the 3D window (the printer frame of `pod.py live`: the front of the printer
# toward +X, the control box toward +Z, Y up) to the scene: a turn of 90 degrees about Y.
WINDOW_TO_SCENE = np.array([[0.0, 0.0, -1.0, 0.0], [0.0, 1.0, 0.0, 0.0], [1.0, 0.0, 0.0, 0.0], [0.0, 0.0, 0.0, 1.0]])


def linear(v):
    """The linear value of an sRGB value from 0 to 1: glTF colors are linear."""
    return v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4


def material(hex_color):
    c = [linear(int(hex_color[i:i + 2], 16) / 255) for i in (1, 3, 5)]
    return trimesh.visual.material.PBRMaterial(baseColorFactor=c + [1.0], metallicFactor=0.0, roughnessFactor=0.75,
                                               doubleSided=True)


def reduce(meshes, limit):
    """The meshes with about limit triangles in total, each reduced by the same share."""
    total = sum(len(m.faces) for m, _ in meshes)
    if total <= limit:
        return meshes
    share = limit / total
    out = []
    for m, c in meshes:
        n = max(12, int(len(m.faces) * share))
        if n < len(m.faces):
            m = m.simplify_quadric_decimation(face_count=n)
        out.append((m, c))
    return out


def write(name, meshes, bed):
    """Writes OUT/<name>.glb: one mesh for each color, and the node "bed" at the matrix bed."""
    by = {}
    for m, c in meshes:
        by.setdefault(c.upper(), []).append(m)
    joined = [(trimesh.util.concatenate(ms), c) for c, ms in by.items()]
    joined = reduce(joined, MAX_TRIANGLES)
    sc = trimesh.Scene()
    for i, (m, c) in enumerate(joined):
        m = m.copy()
        m.merge_vertices()
        m.visual = trimesh.visual.TextureVisuals(material=material(c))
        sc.add_geometry(m, node_name=f"part{i}", geom_name=f"part{i}")
    sc.graph.update(frame_from=sc.graph.base_frame, frame_to="bed", matrix=bed)
    os.makedirs(OUT, exist_ok=True)
    path = os.path.join(OUT, name + ".glb")
    with open(path, "wb") as fh:
        fh.write(sc.export(file_type="glb", include_normals=False))
    tris = sum(len(m.faces) for m, _ in joined)
    print(f"{path}: {tris} triangles, {len(joined)} colors, {os.path.getsize(path) / 1e6:.1f} MB")


def bed_matrix(center, x, up):
    """The matrix of the node bed: the origin at center, the G-code X along x, Z along up, and Y
    along the cross product (to the back)."""
    x, up = np.asarray(x, float), np.asarray(up, float)
    back = np.cross(up, x)
    m = np.eye(4)
    # Node X = G-code X, node Y = G-code Z (up), node Z = -(G-code Y).
    m[:3, 0], m[:3, 1], m[:3, 2], m[:3, 3] = x, up, -back, center
    return m


def taz():
    vdir = os.path.join(HERE, "build", "viewer")
    with open(os.path.join(vdir, "taz.json")) as fh:
        info = json.load(fh)
    view_to_scene = WINDOW_TO_SCENE @ np.linalg.inv(np.asarray(info["views"]["printer_to_view"]))
    meshes = []
    for p in info["parts"]:
        if not p["name"].startswith(TAZ_PARTS) or p["name"].startswith("camera"):
            continue
        path = os.path.join(vdir, "taz", p["file"])
        m = trimesh.load(path, force="mesh")
        if len(m.faces) == 0:
            continue
        m.apply_transform(view_to_scene)
        meshes.append((m, p["color"]))
    bed = info["views"]["bed"]
    rot = view_to_scene[:3, :3]
    center = (view_to_scene @ np.append(bed["center"], 1.0))[:3]
    up = rot @ np.asarray(bed["normal"])
    # The G-code X goes to the right of the printer, seen from the front: the scene +X, flat on the
    # bed.
    x = np.array([1.0, 0.0, 0.0])
    x -= up * x.dot(up)
    write("taz", meshes, bed_matrix(center, x / np.linalg.norm(x), up))


def box(x0, x1, y0, y1, z0, z1):
    m = trimesh.creation.box(extents=(x1 - x0, y1 - y0, z1 - z0))
    m.apply_translation(((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2))
    return m


def phenom():
    """The Peopoly Phenom from boxes (approximate). The build volume is 276 x 155 x 400 mm; the
    case is about 440 x 375 x 760 mm. The origin is the middle of the floor."""
    w, d = 440.0, 375.0
    base_top = 210.0  # the top of the lower case: the UV screen and the vat
    parts = [
        (box(-w / 2, w / 2, 0, base_top, -d / 2, d / 2), "#2B2D31"),                 # the lower case
        (box(-150, 150, base_top, base_top + 2, -95, 95), "#1C2B38"),                 # the UV screen
        (box(-165, 165, base_top, base_top + 45, -105, 105), "#C9CDD2"),              # the vat
        (box(-160, 160, base_top + 2, base_top + 40, -100, 100), "#D9A521"),          # the resin in the vat
        (box(-w / 2, w / 2, 0, 760, -d / 2, -d / 2 + 60), "#2B2D31"),                 # the back column
        (box(-30, 30, base_top + 45, 740, -d / 2 + 60, -d / 2 + 90), "#8E959C"),      # the Z rail
        (box(-60, 60, 420, 450, -d / 2 + 60, -50), "#8E959C"),                        # the arm of the plate
        (box(-140, 140, 400, 412, -80, 80), "#B9BEC6"),                               # the build plate
        (box(-w / 2, w / 2, 755, 760, -d / 2, d / 2), "#E8762B"),                     # the top of the cover
    ]
    # The cover: thin orange walls, open to look in.
    for x0, x1, z0, z1 in ((-w / 2, -w / 2 + 4, -d / 2, d / 2), (w / 2 - 4, w / 2, -d / 2, d / 2)):
        parts.append((box(x0, x1, base_top, 755, z0, z1), "#E8762B"))
    # The bed node is on the build plate, upside down: a resin printer builds under the plate.
    bed = np.eye(4)
    bed[:3, 1] = [0, -1, 0]
    bed[:3, 2] = [0, 0, 1]
    bed[:3, 3] = [0, 400, 0]
    write("phenom", parts, bed)


if __name__ == "__main__":
    for name in sys.argv[1:] or ["taz", "phenom"]:
        globals()[name]()
