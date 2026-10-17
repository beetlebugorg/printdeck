#!/usr/bin/env python3
"""Write one Cura profile for each part of the Speakerdeck Slate, slice each part with
CuraEngine for the real filament use, and check that no part needs supports.

Printer: LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle, 2.85 mm PLA. The profiles use the
shared case settings (case/cura/make_profiles.py), with these changes:
  - The slab, the chin, and the back plate print with the front face on the bed.
  - The slab and the chin are mostly wall: 20 % grid infill.
  - No part uses supports. The support check slices each part again with supports on
    (support angle 60 degrees, supports everywhere). It must give 0 g of support.

The profile names start with "SpeakerdeckSlate_", so that the other installers do not remove
them.

Usage:
    make_profiles.py            write the files to slate/cura/profiles/, build/filament.json,
                                and build/supports.json
    make_profiles.py --install  also copy them into the Cura configuration (close Cura first)
    make_profiles.py --no-slice do not slice (faster)
"""

import collections
import importlib.util
import json
import math
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
SLATE = HERE.parent
OUT = HERE / "profiles"
BUILD = SLATE / "build"

_spec = importlib.util.spec_from_file_location("case_profiles", SLATE.parent / "case" / "cura" / "make_profiles.py")
case = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(case)

FRONT = {**case.FACE_DOWN, "infill_sparse_density": 20, "infill_pattern": "grid"}

PROFILES = [
    case.profile("Slab", "Slate Slab", "slab.stl", {**FRONT, "cool_min_layer_time": 10}),
    case.profile("Chin", "Slate Chin", "chin.stl", {**FRONT, "cool_min_layer_time": 12}),
    case.profile("Back", "Slate Back", "back.stl", {
        **FRONT, "infill_sparse_density": 40, "cool_min_layer_time": 12}),
    case.profile("Foot", "Slate Foot", "foot.stl", {
        "layer_height_0": 0.3, "infill_sparse_density": 20, "infill_pattern": "grid",
        "cool_min_layer_time": 10, "adhesion_type": "skirt", "skirt_line_count": 3,
        "xy_offset_layer_0": -0.15}),
]

SUPPORT_CHECK = {"support_enable": True, "support_angle": 60, "support_type": "everywhere"}

ENGINE = Path("/opt/ultimaker-cura/CuraEngine")
DEFS = case.DEFINITIONS
LINE = 0.5
FACTOR = {"grid": 2, "lightning": 1, "gyroid": 1}
GRAMS_PER_MM = math.pi * 1.425 ** 2 * 1.24e-3


def engine_settings(values):
    """CuraEngine does not compute the settings that Cura derives from others; set them here."""
    v = dict(values)
    walls = round(v.get("wall_thickness", 1.5) / LINE)
    layers = math.ceil(v["top_bottom_thickness"] / v["layer_height"] - 1e-6)
    d = v.get("infill_sparse_density", 20)
    pattern = v.get("infill_pattern", "grid")
    v.update({"wall_line_count": walls, "top_layers": layers, "bottom_layers": layers,
              "infill_line_distance": LINE * 100 / d * FACTOR.get(pattern, 1) if d < 100 else LINE,
              "infill_pattern": pattern, "material_diameter": 2.85, "roofing_layer_count": 0,
              "flooring_layer_count": 0})
    if v.get("adhesion_type") == "brim":
        v.setdefault("brim_line_count", 12)
    return v


def slice_part(profile, extra=None, tag=""):
    """Slice one part. Return the grams of filament for each line type, or None."""
    values = engine_settings({**case.COMMON, **profile["values"], **(extra or {})})
    args = []
    for key, value in values.items():
        args += ["-s", f"{key}={case.fmt(value).lower() if isinstance(value, bool) else value}"]
    gcode = BUILD / "slice" / (Path(profile["stl"]).stem + tag + ".gcode")
    gcode.parent.mkdir(parents=True, exist_ok=True)
    gcode.unlink(missing_ok=True)
    env = dict(os.environ, LD_LIBRARY_PATH=str(ENGINE.parent),
               CURA_ENGINE_SEARCH_PATH=":".join([str(DEFS), "/opt/ultimaker-cura/share/cura/resources/definitions",
                                                 "/opt/ultimaker-cura/share/cura/resources/extruders"]))
    cmd = ["/lib64/ld-linux-x86-64.so.2", str(ENGINE), "slice", "-j", str(DEFS / "lulzbot_taz_workhorse_se.def.json")]
    cmd += args + ["-e0"] + args + ["-l", str(SLATE / "stl" / profile["stl"]), "-o", str(gcode)]
    # CuraEngine can deadlock in its G-code writer with more than one thread (-m1 above).
    # If it still stalls, stop it after 4 minutes and try again.
    for _ in range(3):
        try:
            subprocess.run(cmd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=int(os.environ.get("SLICE_TIMEOUT", 240)))
            break
        except subprocess.TimeoutExpired:
            gcode.unlink(missing_ok=True)
    if not gcode.exists():
        return None
    grams = collections.Counter()
    kind, e, last = "none", 0.0, 0.0
    for line in gcode.open():
        if line.startswith(";TYPE:"):
            kind = line[6:].strip()
            continue
        s = line.split(";")[0].strip()
        if s.startswith("G92"):
            m = re.search(r"E([-\d.]+)", s)
            last = float(m.group(1)) if m else last
        elif s.startswith(("G0", "G1")):
            m = re.search(r"E([-\d.]+)", s)
            if m:
                e = float(m.group(1))
                if e > last:
                    grams[kind] += (e - last) * GRAMS_PER_MM
                last = e
    return grams


def run(profile):
    normal = slice_part(profile)
    support = slice_part(profile, SUPPORT_CHECK, "_support_check")
    return profile, normal, support


def main():
    scopes = case.setting_scopes("fdmprinter.def.json")
    scopes.update(case.setting_scopes("fdmextruder.def.json"))
    OUT.mkdir(exist_ok=True)
    for path in OUT.glob("*.inst.cfg"):
        path.unlink()
    for profile in PROFILES:
        values = {**case.COMMON, **profile["values"]}
        pid = profile["id"].replace("Speakerdeck_", "SpeakerdeckSlate_")
        g = {k: v for k, v in values.items() if k in scopes and not scopes[k]}
        x = {k: v for k, v in values.items() if k in scopes and scopes[k]}
        case.write(OUT / f"{pid}.inst.cfg", profile["name"], profile["quality_type"], g)
        case.write(OUT / f"{pid}_e0.inst.cfg", profile["name"], profile["quality_type"], x, 0)
    if "--no-slice" not in sys.argv:
        filament, supports = {}, {}
        # One slice at a time: parallel CuraEngine runs stalled.
        for profile, normal, support in map(run, PROFILES):
            stem = Path(profile["stl"]).stem
            if normal is None or support is None:
                print(f"{profile['name']:24s} slice failed")
                continue
            total = sum(normal.values())
            sup = support.get("SUPPORT", 0.0) + support.get("SUPPORT-INTERFACE", 0.0)
            filament[stem] = {"grams": round(total), "metres": round(total / GRAMS_PER_MM / 1000, 1)}
            supports[stem] = round(sup, 2)
            print(f"{profile['name']:24s} {profile['stl']:16s} {total:5.0f} g   support check: {sup:.2f} g")
        (BUILD / "filament.json").write_text(json.dumps(filament, indent=1))
        (BUILD / "supports.json").write_text(json.dumps(supports, indent=1))
    if "--install" in sys.argv:
        install()


def install():
    if subprocess.run(["pgrep", "-x", "UltiMaker-Cura"], capture_output=True).returncode == 0:
        sys.exit("Cura is running. Close Cura, then install again.")
    for path in case.CURA.glob("SpeakerdeckSlate_*.inst.cfg"):
        path.unlink()
    for path in sorted(OUT.glob("*.inst.cfg")):
        shutil.copy(path, case.CURA / path.name)
    print(f"Installed {len(list(OUT.glob('*.inst.cfg')))} files in {case.CURA}")


if __name__ == "__main__":
    main()
