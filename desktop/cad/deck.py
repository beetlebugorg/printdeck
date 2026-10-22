# Makes the meshes of the Printdeck on the TAZ Workhorse for the 3D window (Sim3D.qml), from the
# model of the LulzBot mount (lulzbot/, run lulzbot/make.sh first):
#
#   python3 desktop/cad/deck.py
#
# The mount is in the coordinates of the CAD of LulzBot, as cad/export.py, so the meshes go to the
# group of the frame, in the same scene coordinates and the same file format: deck_navy,
# deck_orange and deck_white.
import math, os, struct

HERE = os.path.dirname(os.path.abspath(__file__))
MESH = os.path.join(HERE, "..", "..", "lulzbot", "build", "mesh")
OUT = os.path.join(HERE, "..", "meshes")

# The parts of the mount, by colour. The display, the Pi and the hardware are not shown: the
# 3D window draws the screen on the face.
PARTS = {
    "deck_navy": ["head", "pod"],
    "deck_orange": ["back", "plate", "clip_a", "clip_b", "clip_nut_a", "clip_nut_b"],
    "deck_white": ["grille"],
}

# The scene from the CAD, as in cad/export.py.
OFFSET = (294.9, 540.6, 55.1)


def triangles(path):
    """The triangles of a binary or text STL file, in the CAD coordinates."""
    data = open(path, "rb").read()
    if data[:5] == b"solid" and b"facet" in data[:300]:
        import re
        v = [tuple(map(float, m)) for m in re.findall(rb"vertex\s+(\S+)\s+(\S+)\s+(\S+)", data)]
        return [v[i:i + 3] for i in range(0, len(v) - 2, 3)]
    n = struct.unpack_from("<I", data, 80)[0]
    return [[struct.unpack_from("<3f", data, 84 + 50 * i + 12 + 12 * k) for k in range(3)] for i in range(n)]


def scene(p):
    return (-p[0] + OFFSET[0], p[1] + OFFSET[1], -p[2] + OFFSET[2])


for name, parts in PARTS.items():
    with open(os.path.join(OUT, name + ".bin"), "wb") as f:
        for part in parts:
            for tri in triangles(os.path.join(MESH, part + ".stl")):
                a, b, c = (scene(p) for p in tri)
                u = [b[i] - a[i] for i in range(3)]
                w = [c[i] - a[i] for i in range(3)]
                n = (u[1] * w[2] - u[2] * w[1], u[2] * w[0] - u[0] * w[2], u[0] * w[1] - u[1] * w[0])
                ln = math.sqrt(sum(x * x for x in n)) or 1
                n = tuple(x / ln for x in n)
                for p in (a, b, c):
                    f.write(struct.pack("<3h3bx", *(round(x * 50) for x in p), *(round(x * 127) for x in n)))
    print(name, os.path.getsize(os.path.join(OUT, name + ".bin")))
