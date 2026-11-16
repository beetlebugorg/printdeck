"""The SE tool head, the X carriage and the right X end from the LulzBot CAD, in the head frame of
shape.py (the origin is the nozzle tip; x to the right, y to the back, z up).

- The SE tool head: lulzbot/ref/lulzbot/sl_toolhead/Achemon_sphynx_assem.fcstd (LulzBot SL/SE tool
  head, download.lulzbot.com/TAZ/accessories/tool_heads/SE_0.5mm/). Its axes are the head axes:
  X to the right, Y to the back, Z up. A move by -SE_TIP puts the nozzle tip on the origin.
- The X carriage and the X end: x_axis_01 of lulzbot/ref/lulzbot/final_assembly.fcstd (read by
  lulzbot/workhorse.py). Its axes are X to the left, Y up, Z to the back. The stock tool head of
  that assembly hangs from the same two mount inserts as the SE head, and its nozzle tip is at the
  same place to 0.1 mm. The move in _carriage_place puts those inserts on the SE inserts.
"""

import functools
import os
import sys
import xml.etree.ElementTree as ET
import zipfile

from build123d import Pos, Rot, import_brep

import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
LULZBOT = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.append(LULZBOT)  # after this folder: lulzbot/ has its own shape.py and parts.py
import workhorse as W  # noqa: E402

SE_FCSTD = os.path.join(LULZBOT, "ref", "lulzbot", "sl_toolhead", "Achemon_sphynx_assem.fcstd")

# The objects of the SE assembly that the parts come near (the object names of the file).
SE = {
    "aero": "Mirrored_Aero_01",                   # the Titan Aero: heat sink, heat break, block, nozzle
    "mount": "Beefy_extruder_mount_v2_01",        # the printed mount on the X carriage
    "motor": "NEMA17_half_size_01",
    "hs_fan": "_0mm_fan_step1_01",                # the 40 mm fan on the heat sink (left side)
    "blower": "_0mm_blower_01",                   # the 50 x 15 mm blower
    "duct": "Universal_blower_duct_V1_01",        # the stock duct (replaced)
}
# The four screws of the heat sink fan, and the two screws that hold the stock duct to the Aero.
SE_SCREWS = {
    "fan_back": "M3x12BHCS_02", "fan_front": "M3x12BHCS_03",
    "fan_bottom": "M3x12BHCS_04", "fan_top": "M3x12BHCS_05",
    "duct_side": "M3x12BHCS_01", "duct_bottom": "M3x12BHCS_06",
}

# x_axis_01 solids: the carriage and its bearing holders, the belt clamp, the rods, the belt.
CARRIAGE = [1, 3, 4, 5, 9, 10, 12, 17, 18, 33, 34, 35]
RODS = [0, 7, 29]
XEND = [6, 8, 20, 21, 22, 23, 24, 25]     # the right X end (the idler end)


@functools.lru_cache(maxsize=None)
def _se_files():
    with zipfile.ZipFile(SE_FCSTD) as z:
        doc = ET.fromstring(z.read("Document.xml"))
        files = {}
        for o in doc.find("ObjectData"):
            for p in o.find("Properties"):
                if p.get("name") == "Shape" and len(list(p)):
                    files[o.get("name")] = list(p)[0].get("file")
        out = os.path.join(LULZBOT, "build", "se")
        os.makedirs(out, exist_ok=True)
        paths = {}
        for name, f in files.items():
            path = os.path.join(out, f)
            if not os.path.exists(path):
                with open(path, "wb") as fh:
                    fh.write(z.read(f))
            paths[name] = path
    return paths


@functools.lru_cache(maxsize=None)
def se(key):
    """One object of the SE head, in the head frame."""
    name = SE.get(key) or SE_SCREWS[key]
    return Pos(*(-v for v in S.SE_TIP)) * import_brep(_se_files()[name])


def _carriage_place(shape, dx=0.0):
    # final assembly -> SE: x = 120.75 - X, y = Z - 90, z = Y + 401.3; then the move by -SE_TIP.
    t = S.SE_TIP
    return Pos(120.75 - t[0] + dx, -90.0 - t[1], 401.3 - t[2]) * Rot(0, 0, 180) * Rot(90, 0, 0) * shape


@functools.lru_cache(maxsize=None)
def _x_axis():
    return list(W.load("x_axis_01"))


@functools.lru_cache(maxsize=None)
def carriage():
    """The X carriage parts near the head. They move with the head in X."""
    xs = _x_axis()
    return [_carriage_place(xs[i]) for i in CARRIAGE]


@functools.lru_cache(maxsize=None)
def rods():
    xs = _x_axis()
    return [_carriage_place(xs[i]) for i in RODS]


@functools.lru_cache(maxsize=None)
def x_end():
    """The right X end with the head at the right end of X (X max)."""
    xs = _x_axis()
    return [_carriage_place(xs[i], S.XEND_SHIFT) for i in XEND]
