#!/usr/bin/env python3
"""Printdeck Slate: build the parts, check the fit, and write the files.
Run with: uv run --no-project --with build123d python model.py

Fit check (a true boolean test of every pair whose boxes overlap):
  - no hardware part, plug space, or cable touches a printed part,
  - no two printed parts overlap,
  - no two hardware parts or cables overlap, except the pairs in MATES (a plug in its
    socket, a cable in its plug, a screw in its board).
Output:
  cad/<part>.step            each printed part, in its position in the assembly
  cad/slate-assembly.step    all parts and the hardware, with colours
  stl/<part>.stl             each printed part, in its print orientation
  build/mesh/*.stl           the meshes for the renders (world position), build/manifest.json
"""

import json
import os
import sys
import time

from build123d import Color, Compound, Pos, Rot, export_step, export_stl

import parts as P
import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")

COLOURS = {
    "pla_navy": "#1F2B4D", "pla_orange": "#E8742A", "pla_gray": "#80868E",
    "black_glass": "#050505", "lcd_metal": "#8C9096", "pcb_green": "#1F6B45", "pcb_blue": "#2C5F9E",
    "terminal_green": "#2E8B57", "steel": "#B8BCC2", "alu_dark": "#3A3D42", "label_black": "#1A1A1A",
    "black_plastic": "#161616", "gold": "#D9B248", "brass": "#D4AC52", "post_red": "#B71C1C",
    "post_black": "#121212", "nickel": "#CFCFCF",
}

# name: (material, print orientation). "front" prints with the front face on the bed
# (up is -c), "rear" with the rear face on the bed (up is +c), "world" as it stands.
PRINTED = {
    "slab": ("pla_navy", "front"),
    "chin": ("pla_orange", "front"),
    "back": ("pla_orange", "front"),
    "foot": ("pla_gray", "world"),
}

# Groups for the renders and the exploded views.
GROUPS = {"ssd_cable": "ssd_cable", "fan_cable": "fan_cable", "plug_fan": "pi_plugs", "stub_": "cables",
          "display_": "display", "ssd_": "ssd", "pi_": "pi", "amp_": "pi", "plug_pi": "pi_plugs",
          "plug_touch": "pi_plugs", "plug_ssd": "pi_plugs", "plug_printer": "pi_plugs", "plug_amp": "pi_plugs",
          "fan_": "fan", "foot_screw": "foot_screws", "side_screw0": "side_screw_r", "side_screw1": "side_screw_l", "cable_": "cables", "tie": "cables"}

# Hardware pairs that touch on purpose: (prefix, prefix). A pair matches if one name starts
# with the first prefix and the other with the second.
MATES = [
    ("pi_standoff", "pi_board"), ("pi_standoff", "amp_board"), ("pi_screw", "amp_board"), ("pi_standoff", "pi_screw"),
    ("pi_header", "amp_header"), ("pi_soc", "pi_heatsink"),
    ("display_screw", "display_board"), ("display_lcd", "display_glass"), ("display_board", "display_lcd"),
    ("display_sockets", "display_board"), ("display_plug", "display_sockets"),
    ("ssd_label", "ssd_drive"), ("ssd_adapter", "ssd_drive"), ("ssd_usb", "ssd_adapter"),
    ("ssd_cable_metal", "ssd_usb"), ("ssd_cable_metal", "ssd_cable_plug"), ("ssd_cable_plug", "ssd_cable_relief"),
    ("ssd_cable_relief", "ssd_cable"), ("fan_hub", "fan_blade"), ("fan_frame", "fan_blade"),
    ("fan_frame", "fan_cable"), ("plug_fan", "fan_cable"), ("plug_fan", "pi_fan_socket"),
    ("cable_hdmi", "display_plug_hdmi"), ("cable_hdmi", "plug_pi_hdmi"),
    ("cable_usb", "display_plug_usb"), ("cable_usb", "plug_touch_usb"),
    ("ssd_cable", "plug_ssd_usb"), ("stub_power", "plug_pi_power"), ("stub_printer", "plug_printer_usb"),
    ("stub_amp_power", "plug_amp_power"), ("stub_speaker", "amp_terminals"),
    ("plug_amp_power", "amp_jack"), ("amp_terminals", "amp_board"), ("amp_jack", "amp_board"),
    ("amp_cap", "amp_board"), ("amp_coil", "amp_board"), ("amp_header", "amp_board"),
]


def hex_rgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))


def log(msg):
    print(msg, flush=True)


def build():
    t = time.time()
    printed = {
        "slab": P.to_world(P.slab()),
        "chin": P.to_world(P.chin()),
        "back": P.to_world(P.back()),
        "foot": P.foot_world(),
    }
    log(f"  printed parts built in {time.time() - t:.0f} s")
    hw, zones = [], []
    for builder in (P.display, P.ssd, P.pi_stack, P.fan, P.side_screws):
        p, z = builder()
        hw += p
        zones += z
    hw += P.cables()
    hw = [(n, P.to_world(s), m) for n, s, m in hw]
    zones = [(n, P.to_world(s)) for n, s in zones]
    hw += P.foot_screws()
    return printed, hw, zones


def boxes_meet(a, b):
    """True if the two bounding boxes share any space. (BoundBox.overlaps() is False when
    one box is inside the other, so it does not work for this.)"""
    p, q = a.bounding_box(), b.bounding_box()
    return all(getattr(p.min, k) < getattr(q.max, k) and getattr(q.min, k) < getattr(p.max, k) for k in "XYZ")


def overlap(a, b):
    if not boxes_meet(a, b):
        return 0.0
    common = a & b                   # None when the two shapes do not touch
    return 0.0 if common is None else common.volume


def mates(m, n):
    return any((m.startswith(x) and n.startswith(y)) or (m.startswith(y) and n.startswith(x)) for x, y in MATES)


def check(printed, hw, zones):
    problems = []
    items = [(n, s) for n, s, _ in hw if not n.startswith("tie")] + zones
    for hn, hs in items:
        for pn, ps in printed.items():
            v = overlap(hs, ps)
            if v > 0.5:
                problems.append(f"{hn} touches {pn}: {v:.1f} mm3")
    names = list(printed)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            v = overlap(printed[a], printed[b])
            if v > 1.0:
                problems.append(f"{a} overlaps {b}: {v:.1f} mm3")
    parts = [(n, s) for n, s, _ in hw if not n.startswith("tie")]
    for i, (m, a) in enumerate(parts):
        for n, b in parts[i + 1:]:
            if mates(m, n):
                continue
            v = overlap(a, b)
            if v > 0.5:
                problems.append(f"{m} touches {n}: {v:.1f} mm3")
    return problems


def print_pose(name, shape):
    """The part in its print orientation: on the bed (Z = 0), centred on X and Y."""
    orient = PRINTED[name][1]
    if orient == "world":
        s = shape
    else:
        local = P.LOC.inverse() * shape
        s = Rot(180, 0, 0) * local if orient == "front" else local
    bb = s.bounding_box()
    return Pos(-(bb.min.X + bb.max.X) / 2, -(bb.min.Y + bb.max.Y) / 2, -bb.min.Z) * s


def export(printed, hw):
    for d in ("cad", "stl", os.path.join("build", "mesh")):
        os.makedirs(os.path.join(HERE, d), exist_ok=True)
    mesh_dir = os.path.join(BUILD, "mesh")
    for f in os.listdir(mesh_dir):
        os.remove(os.path.join(mesh_dir, f))
    manifest = []
    children = []
    summary = {"parts": {}, "cables": {}}
    for name, shape in printed.items():
        mat = PRINTED[name][0]
        export_step(shape, os.path.join(HERE, "cad", name + ".step"))
        pose = print_pose(name, shape)
        export_stl(pose, os.path.join(HERE, "stl", name + ".stl"), tolerance=0.01, angular_tolerance=0.1)
        export_stl(shape, os.path.join(mesh_dir, name + ".stl"), tolerance=0.02, angular_tolerance=0.15)
        manifest.append({"name": name, "file": name + ".stl", "material": mat, "group": name})
        bb = pose.bounding_box()
        summary["parts"][name] = {"size": [round(bb.size.X, 1), round(bb.size.Y, 1), round(bb.size.Z, 1)],
                                  "volume_cm3": round(shape.volume / 1000, 1)}
        log(f"  {name:11s} print size {bb.size.X:5.1f} x {bb.size.Y:5.1f} x {bb.size.Z:5.1f} mm, "
            f"volume {shape.volume / 1000:6.1f} cm3")
        c = shape.copy() if hasattr(shape, "copy") else shape
        c.label = name
        c.color = Color(*hex_rgb(COLOURS[mat]))
        children.append(c)
    for name, shape, mat in hw:
        group = next((g for p, g in GROUPS.items() if name.startswith(p)), "other")
        export_stl(shape, os.path.join(mesh_dir, "hw_" + name + ".stl"), tolerance=0.05, angular_tolerance=0.2)
        manifest.append({"name": "hw_" + name, "file": "hw_" + name + ".stl", "material": mat, "group": group})
        c = shape
        c.label = name
        c.color = Color(*hex_rgb(COLOURS.get(mat, "#888888")))
        children.append(c)
    export_step(Compound(children=children, label="printdeck-slate"), os.path.join(HERE, "cad", "slate-assembly.step"))
    # Cable length, plug end to plug end: the centre line, plus the plugs (30 mm for each
    # straight display plug and the USB-A plug, MICRO_B_LEN for the micro-B plug). The fan
    # cable is the centre line only.
    plugs = {"hdmi": 60, "usb": 60, "ssd": P.MICRO_B_LEN + 30, "fan": 0}
    for k, pts in P.cable_paths().items():
        summary["cables"][k] = round(P.cable_length(pts) + plugs[k])
    c = summary["cables"]
    log(f"  cables, plug to plug: HDMI {c['hdmi']} mm, USB {c['usb']} mm, drive {c['ssd']} mm, fan {c['fan']} mm")
    with open(os.path.join(BUILD, "parts.json"), "w") as fh:
        json.dump(summary, fh, indent=1)
    with open(os.path.join(BUILD, "manifest.json"), "w") as fh:
        json.dump({"parts": manifest, "screen": P.screen_corners()}, fh, indent=1)


def main():
    printed, hw, zones = build()
    problems = check(printed, hw, zones)
    for p in problems:
        log("  CLASH: " + p)
    if not problems:
        log("  fit check: all hardware, plug spaces, and cables are clear of the printed parts; no parts overlap")
    if "--no-export" not in sys.argv:
        export(printed, hw)
    log("RESULT: " + ("CLASHES" if problems else "OK"))


if __name__ == "__main__":
    main()
