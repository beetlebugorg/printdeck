#!/usr/bin/env python3
"""Write one Cura profile for each part of the Printdeck Slate, slice each part with
CuraEngine for the real filament use, and check that no part needs supports.

Printer: LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle, 2.85 mm PLA. The profiles start
from the shared settings below (COMMON, LAYER, FACE_DOWN), with these changes:
  - The slab, the chin, and the back plate print with the front face on the bed.
  - The slab and the chin are mostly wall: 20 % grid infill.
  - No part uses supports. The support check slices each part again with supports on
    (support angle 60 degrees, supports everywhere). It must give 0 g of support.

The profile names start with "PrintdeckSlate_", so that the other installers do not remove
them.

Usage:
    make_profiles.py            write the files to slate/cura/profiles/, build/filament.json,
                                and build/supports.json
    make_profiles.py --install  also copy them into the Cura configuration (close Cura first)
    make_profiles.py --no-slice do not slice (faster)
"""

import collections
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

CURA = Path.home() / ".local/share/cura/5.13/quality_changes"
# The Cura setting definitions. They list which settings are global and which are for the extruder.
DEFINITIONS = Path.home() / ".local/share/cura-workhorse-profile"
SETTING_VERSION = 27

# Settings that all profiles share. They come from the LulzBot profiles for this tool head.
# PLA shrinks when it cools: approximately 0.3 % horizontally and 0.2 % vertically.
# Cura makes each part larger by these values. For the best fit, print a 100 mm test bar
# in each direction, measure it, and set the values from the result.
SHRINK_XY = 100.3
SHRINK_Z = 100.2

COMMON = {
    "material_shrinkage_percentage_xy": SHRINK_XY,
    "material_shrinkage_percentage_z": SHRINK_Z,
    "layer_0_z_overlap": 0.5,
    "raft_airgap": 0.5,
    "retraction_amount": 1.5,
    "retraction_speed": 15,
    "retract_at_layer_change": True,
    "retraction_min_travel": 3,
    "cool_fan_speed_min": 100,
    "cool_fan_full_layer": 5,
    "cool_min_layer_time_fan_speed_max": 0,
    "wall_thickness": 1.5,
    "z_seam_type": "sharpest_corner",
    "z_seam_corner": "z_seam_corner_inner",
    "skin_monotonic": True,
}

# The parts print with 0.3 mm layers ("verydraft" in Cura).
# At 0.3 mm x 0.5 mm, 55 mm/s is 8.25 mm3/s. That is below the flow limit.
LAYER = {
    "layer_height": 0.3,
    "top_bottom_thickness": 1.2,
    "speed_print": 50,
    "speed_infill": 55,
    "speed_wall": 40,
    "speed_wall_0": 30,
    "speed_wall_x": 45,
    "speed_topbottom": 40,
    "speed_layer_0": 18,
    "speed_travel": 150,
    "speed_travel_layer_0": 100,
    "speed_slowdown_layers": 2,
    "material_print_temperature": 220,
    "material_print_temperature_layer_0": 215,
    "support_enable": False,
}

# The front face of the part is on the bed. The first layer is 0.3 mm, the same as
# the other layers. A smaller first layer line stops elephant foot.
FACE_DOWN = {
    "layer_height_0": 0.3,
    "infill_sparse_density": 100,
    "speed_wall_0": 25,
    "speed_topbottom": 30,
    "cool_min_layer_time": 10,
    "adhesion_type": "skirt",
    "skirt_line_count": 3,
    "xy_offset_layer_0": -0.15,
}



def profile(part, name, stl, values):
    layer = values.get("layer_height", LAYER["layer_height"])
    return {"id": f"Printdeck_{part}", "name": f"Printdeck - {name} PLA {layer}", "stl": stl,
            "quality_type": "verydraft", "values": {**LAYER, **values}}



def setting_scopes(name):
    """Map each setting to True if it is for the extruder, False if it is global."""
    scopes = {}

    def walk(node, inherited):
        for key, value in node.items():
            if not isinstance(value, dict):
                continue
            current = value.get("settable_per_extruder", inherited)
            if "label" in value or "default_value" in value or "value" in value:
                scopes[key] = current
            if "children" in value:
                walk(value["children"], current)

    walk(json.loads((DEFINITIONS / name).read_text())["settings"], True)
    return scopes


def fmt(value):
    if isinstance(value, bool):
        return "True" if value else "False"
    return str(value)


def write(path, name, quality_type, values, position=None):
    lines = ["[general]", "version = 4", f"name = {name}", "definition = fdmprinter", "",
             "[metadata]", f"setting_version = {SETTING_VERSION}", "type = quality_changes",
             f"quality_type = {quality_type}"]
    if position is not None:
        lines.append(f"position = {position}")
    lines += ["", "[values]"] + [f"{key} = {fmt(values[key])}" for key in sorted(values)]
    path.write_text("\n".join(lines) + "\n")



FRONT = {**FACE_DOWN, "infill_sparse_density": 20, "infill_pattern": "grid"}

PROFILES = [
    profile("Slab", "Slate Slab", "slab.stl", {**FRONT, "cool_min_layer_time": 10}),
    profile("Chin", "Slate Chin", "chin.stl", {**FRONT, "cool_min_layer_time": 12}),
    profile("Back", "Slate Back", "back.stl", {
        **FRONT, "infill_sparse_density": 40, "cool_min_layer_time": 12}),
    profile("Foot", "Slate Foot", "foot.stl", {
        "layer_height_0": 0.3, "infill_sparse_density": 20, "infill_pattern": "grid",
        "cool_min_layer_time": 10, "adhesion_type": "skirt", "skirt_line_count": 3,
        "xy_offset_layer_0": -0.15}),
]

SUPPORT_CHECK = {"support_enable": True, "support_angle": 60, "support_type": "everywhere"}

ENGINE = Path("/opt/ultimaker-cura/CuraEngine")
DEFS = DEFINITIONS
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
    values = engine_settings({**COMMON, **profile["values"], **(extra or {})})
    args = []
    for key, value in values.items():
        args += ["-s", f"{key}={fmt(value).lower() if isinstance(value, bool) else value}"]
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
    scopes = setting_scopes("fdmprinter.def.json")
    scopes.update(setting_scopes("fdmextruder.def.json"))
    OUT.mkdir(exist_ok=True)
    for path in OUT.glob("*.inst.cfg"):
        path.unlink()
    for profile in PROFILES:
        values = {**COMMON, **profile["values"]}
        pid = profile["id"].replace("Printdeck_", "PrintdeckSlate_")
        g = {k: v for k, v in values.items() if k in scopes and not scopes[k]}
        x = {k: v for k, v in values.items() if k in scopes and scopes[k]}
        write(OUT / f"{pid}.inst.cfg", profile["name"], profile["quality_type"], g)
        write(OUT / f"{pid}_e0.inst.cfg", profile["name"], profile["quality_type"], x, 0)
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
    for path in CURA.glob("PrintdeckSlate_*.inst.cfg"):
        path.unlink()
    for path in sorted(OUT.glob("*.inst.cfg")):
        shutil.copy(path, CURA / path.name)
    print(f"Installed {len(list(OUT.glob('*.inst.cfg')))} files in {CURA}")


if __name__ == "__main__":
    main()
