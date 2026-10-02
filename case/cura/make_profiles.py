#!/usr/bin/env python3
"""Write one Cura profile for each Speakerdeck part.

Printer: LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle, 2.85 mm PLA.
The profiles are Cura "quality_changes" containers, one global file and one
extruder file for each profile. This follows the Workhorse profiles that are
already in the Cura configuration on this computer.

Usage:
    make_profiles.py            write the files to case/cura/profiles/
    make_profiles.py --install  also copy them into the Cura configuration

Close Cura before you install. Cura reads the profiles when it starts.

Flow limit: this tool head melts approximately 9.5 mm3/s. Each profile keeps
layer height x line width (0.5 mm) x speed below 9 mm3/s.
"""

import json
import shutil
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "profiles"
CURA = Path.home() / ".local/share/cura/5.13/quality_changes"
# The Cura setting definitions. They tell which settings are global and which are for the extruder.
DEFINITIONS = Path.home() / ".local/share/cura-workhorse-profile"
SETTING_VERSION = 27

# Settings that all profiles share. They come from the LulzBot profiles for this tool head.
COMMON = {
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

# quality_type: "normal" is the 0.1 mm base, "draft" is 0.2 mm, "verydraft" is 0.3 mm.
PROFILES = [
    {
        # Large part, 233 mm high. The top of the case is on the bed.
        # Supports hold the display opening and the Inky cavity. The frames hide those surfaces.
        "id": "Speakerdeck_Body",
        "name": "Speakerdeck - Body 0.3",
        "stl": "body.stl",
        "quality_type": "verydraft",
        "values": {
            "layer_height": 0.3,
            "layer_height_0": 0.425,
            "top_bottom_thickness": 1.2,
            "infill_sparse_density": 15,
            "speed_print": 50,
            "speed_infill": 60,
            "speed_wall": 45,
            "speed_wall_0": 35,
            "speed_wall_x": 50,
            "speed_topbottom": 40,
            "speed_layer_0": 18,
            "speed_travel": 150,
            "speed_travel_layer_0": 100,
            "speed_slowdown_layers": 2,
            "speed_support": 45,
            "speed_support_interface": 30,
            "cool_min_layer_time": 10,
            "material_print_temperature": 230,
            "material_print_temperature_layer_0": 215,
            "adhesion_type": "brim",
            "brim_line_count": 8,
            "support_enable": True,
            "support_type": "everywhere",
            "support_angle": 55,
            "support_pattern": "zigzag",
            "support_infill_rate": 12,
            "support_z_distance": 0.3,
            "support_xy_distance": 0.8,
            "support_roof_enable": True,
            "support_interface_height": 0.9,
        },
    },
    {
        # Flat plate with four posts and pins for the Pi.
        "id": "Speakerdeck_Base",
        "name": "Speakerdeck - Base 0.3",
        "stl": "base.stl",
        "quality_type": "verydraft",
        "values": {
            "layer_height": 0.3,
            "layer_height_0": 0.425,
            "top_bottom_thickness": 1.2,
            "infill_sparse_density": 30,
            "speed_print": 50,
            "speed_infill": 60,
            "speed_wall": 45,
            "speed_wall_0": 35,
            "speed_wall_x": 50,
            "speed_topbottom": 40,
            "speed_layer_0": 18,
            "speed_travel": 150,
            "speed_travel_layer_0": 100,
            "speed_slowdown_layers": 2,
            # The posts and the pins are small. Give each layer time to become cool.
            "cool_min_layer_time": 15,
            "material_print_temperature": 230,
            "material_print_temperature_layer_0": 215,
            "adhesion_type": "brim",
            "brim_line_count": 5,
            "support_enable": False,
        },
    },
    {
        # Thin, visible part. The front face is on the bed.
        # The lip around the display is 1.0 mm: 5 layers of 0.2 mm, with a 0.2 mm first layer.
        "id": "Speakerdeck_Display_Frame",
        "name": "Speakerdeck - Display Frame 0.2",
        "stl": "frame.stl",
        "quality_type": "draft",
        "values": {
            "layer_height": 0.2,
            "layer_height_0": 0.2,
            "top_bottom_thickness": 1.0,
            "infill_sparse_density": 100,
            "speed_print": 30,
            "speed_infill": 30,
            "speed_wall": 25,
            "speed_wall_0": 20,
            "speed_wall_x": 25,
            "speed_topbottom": 25,
            "speed_layer_0": 12,
            "speed_travel": 120,
            "speed_travel_layer_0": 80,
            "speed_slowdown_layers": 2,
            "cool_min_layer_time": 15,
            "material_print_temperature": 220,
            "material_print_temperature_layer_0": 220,
            # A brim leaves marks on the visible edge. Use a skirt.
            "adhesion_type": "skirt",
            "skirt_line_count": 3,
            # Make the first layer smaller, so that the frame fits in its indent.
            "xy_offset_layer_0": -0.15,
            "support_enable": False,
        },
    },
    {
        # The part with the most detail: a 0.6 mm lip and 3 mm bars.
        # Slow and thin, so that the details stay sharp.
        # The lip is 0.2 mm (first layer) + 4 x 0.1 mm.
        "id": "Speakerdeck_Inky_Frame",
        "name": "Speakerdeck - Inky Frame 0.1 slow",
        "stl": "inky_frame.stl",
        "quality_type": "normal",
        "values": {
            "layer_height": 0.1,
            "layer_height_0": 0.2,
            "top_bottom_thickness": 1.0,
            "infill_sparse_density": 100,
            "speed_print": 20,
            "speed_infill": 20,
            "speed_wall": 18,
            "speed_wall_0": 12,
            "speed_wall_x": 18,
            "speed_topbottom": 18,
            "speed_layer_0": 8,
            "speed_travel": 100,
            "speed_travel_layer_0": 60,
            "speed_slowdown_layers": 3,
            "small_hole_max_size": 5,
            "small_feature_speed_factor": 50,
            "small_feature_speed_factor_0": 50,
            "cool_min_layer_time": 20,
            # Slow, thin layers need less heat.
            "material_print_temperature": 215,
            "material_print_temperature_layer_0": 215,
            "adhesion_type": "skirt",
            "skirt_line_count": 3,
            "xy_offset_layer_0": -0.15,
            "support_enable": False,
        },
    },
    {
        # Eight small caps on one plate.
        "id": "Speakerdeck_Caps",
        "name": "Speakerdeck - Caps 0.2",
        "stl": "cap.stl",
        "quality_type": "draft",
        "values": {
            "layer_height": 0.2,
            "layer_height_0": 0.3,
            "top_bottom_thickness": 1.0,
            "infill_sparse_density": 100,
            "speed_print": 30,
            "speed_infill": 30,
            "speed_wall": 25,
            "speed_wall_0": 20,
            "speed_wall_x": 25,
            "speed_topbottom": 25,
            "speed_layer_0": 12,
            "speed_travel": 120,
            "speed_travel_layer_0": 80,
            "cool_min_layer_time": 20,
            "material_print_temperature": 220,
            "material_print_temperature_layer_0": 220,
            "adhesion_type": "skirt",
            "skirt_line_count": 3,
            "support_enable": False,
        },
    },
    {
        # Flat spacer behind the Inky wHAT. In PLA it is rigid.
        "id": "Speakerdeck_Pad",
        "name": "Speakerdeck - Pad 0.2",
        "stl": "pad.stl",
        "quality_type": "draft",
        "values": {
            "layer_height": 0.2,
            "layer_height_0": 0.3,
            "top_bottom_thickness": 0.6,
            "infill_sparse_density": 100,
            "speed_print": 35,
            "speed_infill": 35,
            "speed_wall": 30,
            "speed_wall_0": 25,
            "speed_wall_x": 30,
            "speed_topbottom": 30,
            "speed_layer_0": 15,
            "speed_travel": 120,
            "cool_min_layer_time": 10,
            "material_print_temperature": 220,
            "material_print_temperature_layer_0": 220,
            "adhesion_type": "skirt",
            "skirt_line_count": 3,
            "xy_offset_layer_0": -0.15,
            "support_enable": False,
        },
    },
]


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


def main():
    scopes = setting_scopes("fdmprinter.def.json")
    scopes.update(setting_scopes("fdmextruder.def.json"))
    OUT.mkdir(exist_ok=True)
    for profile in PROFILES:
        values = {**COMMON, **profile["values"]}
        unknown = sorted(key for key in values if key not in scopes)
        global_values = {k: v for k, v in values.items() if k in scopes and not scopes[k]}
        extruder_values = {k: v for k, v in values.items() if k in scopes and scopes[k]}
        write(OUT / f'{profile["id"]}.inst.cfg', profile["name"], profile["quality_type"], global_values)
        write(OUT / f'{profile["id"]}_e0.inst.cfg', profile["name"], profile["quality_type"], extruder_values, 0)
        flow = values["layer_height"] * 0.5 * max(values[k] for k in values if k.startswith("speed_") and
                                                    k not in ("speed_travel", "speed_travel_layer_0", "speed_slowdown_layers"))
        print(f'{profile["name"]:36s} {profile["stl"]:16s} max flow {flow:4.1f} mm3/s'
              + (f"  dropped: {unknown}" if unknown else ""))
    if "--install" in sys.argv:
        for path in sorted(OUT.glob("*.inst.cfg")):
            shutil.copy(path, CURA / path.name)
        print(f"Installed {len(list(OUT.glob('*.inst.cfg')))} files in {CURA}")


if __name__ == "__main__":
    main()
