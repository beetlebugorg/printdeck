# Makes the meshes of the Printdeck on the TAZ Workhorse for the 3D window (Sim3D.qml), from the
# model of the LulzBot mount (lulzbot/, run lulzbot/make.sh first):
#
#   python3 desktop/cad/deck.py
#
# The mount is in the coordinates of the CAD of LulzBot, as cad/export.py, so the meshes are in the
# same scene coordinates and file format, in the group of the frame. Each assembly group of the
# build booklet (lulzbot/lineart/render.mjs) and each material is one file
# deck_GROUP_MATERIAL.bin, so the film can put the groups on the printer one at a time.
# meshes/deck.json lists them. The cables are not drawn.
import json, math, os, struct

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "..", "..", "lulzbot", "build")
OUT = os.path.join(HERE, "..", "meshes")

# The groups of the booklet: the inserts, the screws of the Pi, the display and the clips have
# groups of their own (REGROUP and PREFIX of render.mjs).
PREFIX = [("hw_insert_head_back", "ins_head"), ("hw_insert_back_flange", "ins_back"),
          ("hw_insert_clip_nut", "ins_nut"), ("hw_insert_pod_block", "ins_block"), ("hw_pi_screw", "pi_screws"),
          ("hw_display_screw", "display_screws"), ("hw_clip_screw", "clip_screws"), ("clip_", "clips")]
SKIP = {"printer", "cables", "printer_cable", "power", "pi_plugs", "display_cables", "drive_cable"}

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


def group_of(part):
    for prefix, group in PREFIX:
        if part["name"].startswith(prefix):
            return group
    if part["name"].startswith(("hw_cable_", "hw_plug_")):
        return "cables"
    return part["group"]


manifest = json.load(open(os.path.join(BUILD, "manifest.json")))
files = {}  # (group, material) -> list of STL names
for part in manifest["parts"]:
    g = group_of(part)
    if g not in SKIP:
        files.setdefault((g, part["material"]), []).append(part["file"])

for name in os.listdir(OUT):
    if name.startswith("deck_"):
        os.remove(os.path.join(OUT, name))
index = []
for (group, material), stls in sorted(files.items()):
    name = f"deck_{group}_{material}"
    with open(os.path.join(OUT, name + ".bin"), "wb") as f:
        for stl in stls:
            for tri in triangles(os.path.join(BUILD, "mesh", stl)):
                a, b, c = (scene(p) for p in tri)
                u = [b[i] - a[i] for i in range(3)]
                w = [c[i] - a[i] for i in range(3)]
                n = (u[1] * w[2] - u[2] * w[1], u[2] * w[0] - u[0] * w[2], u[0] * w[1] - u[1] * w[0])
                ln = math.sqrt(sum(x * x for x in n)) or 1
                n = tuple(x / ln for x in n)
                for p in (a, b, c):
                    f.write(struct.pack("<3h3bx", *(round(x * 50) for x in p), *(round(x * 127) for x in n)))
    index.append({"group": group, "material": material, "mesh": name})
with open(os.path.join(OUT, "deck.json"), "w") as f:
    json.dump(index, f, indent=1)
print(len(index), "meshes:", sorted({i["group"] for i in index}))
