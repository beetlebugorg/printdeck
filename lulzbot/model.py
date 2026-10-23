#!/usr/bin/env python3
"""Printdeck LulzBot: build the parts, check the fit, and write the files.
Run with: uv run --no-project --with build123d python model.py

Fit check (a true boolean test of every pair whose boxes overlap):
  - no hardware part or cable touches a printed part,
  - no two printed parts overlap,
  - no two hardware parts or cables overlap, except the pairs in MATES,
  - no printed part, hardware part, or cable touches the printer (the LulzBot model), except
    the screws in the cover holes and in the rail, and the parts in the cable holes of the cover,
  - the cover screws are on the holes of cover PP-FP0164 (within the slots), and the rail
    screws are on the centre of the front slot of the rail,
  - the printer USB and the power cable go out of the rear wall of the pod without a touch,
  - the head with the back panel hangs on the rail: at each step of the 8 mm drop onto the rail
    screws, it touches no fixed part,
  - the pod slides on: at each step of its 45 degree path, it touches no fixed part,
  - the fan sits in the snaps of the grille: the catches are under the fan, the funnel is over
    it, and the prongs bend less than the strain limit,
  - the grille with the fan goes into the pod straight from the outside and touches no part.
Output:
  cad/<part>.step            each printed part, in its position on the printer
  cad/lulzbot-assembly.step  all parts and the hardware, with colours, on the printer
  stl/<part>.stl             each printed part, in its print orientation
  build/mesh/*.stl           the meshes for the renders (printer coordinates), build/manifest.json
"""

import json
import math
import os
import sys
import time

from build123d import Align, Box, Color, Compound, Pos, Rot, Solid, export_step, export_stl

import parts as P
import shape as S
import workhorse as WH

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")
PLA = 1.24                         # g/cm3

COLOURS = {
    "pla_navy": "#1F2B4D", "pla_orange": "#E8742A", "pla_white": "#F2F2EE", "pla_gray": "#80868E",
    "black_glass": "#050505", "lcd_metal": "#8C9096", "pcb_green": "#1F6B45", "steel": "#B8BCC2",
    "black_steel": "#202226", "alu_dark": "#3A3D42", "black_plastic": "#161616", "nickel": "#CFCFCF",
    "brass": "#C9A64B",
}

# name: (material, print orientation, STL name). Orientations: "face" the face of the head on
# the bed, "rear" the rear face of the back panel on the bed, "outer" the outer face of the pod
# on the bed, "flat" as it is (the face against the cover on the bed), "end" the slot axis up (the
# clip).
PRINTED = {
    "head": ("pla_navy", "face", "head"),
    "back": ("pla_orange", "rear", "back"),
    "pod": ("pla_navy", "outer", "pod"),
    "plate": ("pla_orange", "flat", "plate"),
    "grille": ("pla_white", "flat", "grille"),
    "clip_a": ("pla_orange", "end", "clip"),
    "clip_b": ("pla_orange", "end", None),
}

MATES = [
    ("display_glass", "display_lcd"), ("display_screw", "display_board"), ("display_sockets", "display_board"),
    ("display_plug", "display_sockets"), ("pi_screw", "pi_board"), ("pi_soc", "pi_heatsink"), ("pi_rp1", "pi_heatsink"),
    ("pi_sdcard", "pi_board"), ("plug_pi_power", "pi_usbc"), ("plug_pi_hdmi", "pi_hdmi0"),
    ("plug_touch_usb", "pi_usb3"), ("plug_drive_usb", "pi_usb3"), ("plug_touch_usb", "plug_drive_usb"),
    ("plug_printer_usb", "pi_usb2"), ("cable_drive", "plug_drive_usb"), ("cable_drive", "ssd_cable_relief"),
    ("ssd_adapter", "ssd_drive"), ("ssd_usb", "ssd_adapter"), ("ssd_cable_metal", "ssd_usb"),
    ("ssd_cable_metal", "ssd_cable_plug"), ("ssd_cable_plug", "ssd_cable_relief"), ("drive_screw", "ssd_drive"), ("clip_screw", "hammer_nut"),
    ("cable_hdmi", "display_plug_hdmi"), ("cable_hdmi", "plug_pi_hdmi"), ("cable_touch", "display_plug_usb"),
    ("cable_touch", "plug_touch_usb"), ("cable_printer", "plug_printer_usb"), ("cable_power", "plug_pi_power"),
    ("plug_pi_fan", "pi_fan_socket"), ("cable_fan", "plug_pi_fan"), ("fan_hub", "fan_blade"), ("fan_frame", "fan_blade"),
]

# Parts that go into the printer on purpose: the mounting screws into the cover and the PEM
# nuts. (The clip screws and their hammer nuts are in the slot of the rail and do not touch it.)
INTO_PRINTER = [("mount_screw", "cover"), ("mount_screw", "chassis")]

# The hardware list of the guide comes from the hardware parts in the model: each screw, washer,
# and insert is in the model at its place. (prefix, key, item, box, where). The boxes are the
# hardware that the user has; STOCK is the count in each box.
HARDWARE = [
    ("mount_screw", "m3x10", "M3 × 10 socket cap head", "VIGRUE M3 kit", "plate on the cover (upper pair of PP-FP0164)"),
    ("mount_washer", "m3_washer", "M3 washer", "M3 washers", "under the cover screws"),
    ("head_screw", "m3x10", "M3 × 10 socket cap head", "VIGRUE M3 kit", "back panel into the head"),
    ("flange_screw", "m3x12", "M3 × 12 socket cap head", "VIGRUE M3 kit", "plate flange into the back panel"),
    ("bottom_screw", "m3x8", "M3 × 8 socket cap head", "VIGRUE M3 kit", "pod onto the plate"),
    ("drive_screw", "m3x6", "M3 × 6 socket cap head", "VIGRUE M3 kit", "drive on the back panel"),
    ("display_screw", "m3x6", "M3 × 6 socket cap head", "VIGRUE M3 kit", "display board (it cuts its thread)"),
    ("pi_screw", "m3x6", "M3 × 6 socket cap head", "VIGRUE M3 kit", "Pi (it cuts its thread)"),
    ("clip_screw", "m3x8", "M3 × 8 socket cap head", "VIGRUE M3 kit", "rail clip into its T-nut"),
    ("hammer_nut", "m3_hammer", "M3 roll-in T-nut with spring ball, for 2020 extrusion, 6 mm slot", "buy: uxcell M3 roll-in "
     "spring T-nut with ball, 2020 series (Amazon B09MFT1PKF)", "rail clips"),
    ("insert_head_back", "ins_m3x7", "M3 × 7 heat-set insert", "FFVRVSS insert kit", "head, back screws (3 to 7 mm fits)"),
    ("insert_back_flange", "ins_m3x7", "M3 × 7 heat-set insert", "FFVRVSS insert kit", "back panel, flange screws (3 to 7 mm fits)"),
    ("insert_pod_block", "ins_m3x5", "M3 × 5 heat-set insert", "FFVRVSS insert kit", "plate, pod screw blocks (3 to 5 mm fits)"),
]
STOCK = {"m3x6": 55, "m3x8": 55, "m3x10": 55, "m3x12": 55, "m3_washer": 300, "ins_m3x5": 50, "ins_m3x7": 30,
         "m3_hammer": 2}


def hardware_list(hw):
    """Count the hardware parts of the model by item. Every screw, washer, and insert must have
    an entry in HARDWARE."""
    out, problems = {}, []
    for name, _, _ in hw:
        if not name.startswith(("mount_", "head_screw", "flange_", "bottom_", "drive_screw", "display_screw",
                                "pi_screw", "clip_screw", "hammer_nut", "insert_")):
            continue
        entry = next((h for h in HARDWARE if name.startswith(h[0])), None)
        if entry is None:
            problems.append(f"{name} is not in the hardware list")
            continue
        _, key, item, box, where = entry
        e = out.setdefault(key, {"item": item, "box": box, "count": 0, "places": {}})
        e["count"] += 1
        e["places"][where] = e["places"].get(where, 0) + 1
    for key, e in out.items():
        if e["count"] > STOCK.get(key, 0):
            problems.append(f"{e['item']}: {e['count']} needed, {STOCK.get(key, 0)} in stock")
    return out, problems


GROUPS = {"display_": "display", "pi_": "pi", "plug_": "pi", "cable_": "cables", "mount_screw": "mount_screws",
          "bottom_screw": "bottom_screws", "head_screw": "head_screws", "ssd_": "drive",
          "drive_screw": "drive_screws",
          "flange_screw": "flange_screws", "clip_": "clips", "hammer_nut": "clips", "fan_": "fan",
          "mount_washer": "mount_screws", "insert_": "inserts"}


def hex_rgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))


def log(msg):
    print(msg, flush=True)


def build():
    t = time.time()
    head_local = P.head()
    printed = {
        "head": P.head_to_mount(head_local),
        "back": P.back(),
        "pod": P.pod(),
        "plate": P.plate(),
        "grille": P.grille(),
        "clip_a": P.clip(S.CLIPS_Y[0]),
        "clip_b": P.clip(S.CLIPS_Y[1]),
    }
    log(f"  printed parts built in {time.time() - t:.0f} s")
    hw = (P.display() + P.pi5() + P.mount_screws() + P.bottom_screws() + P.drive() + P.head_screws()
          + P.flange_screws() + P.clip_screws() + P.hammer_nuts() + P.inserts() + P.fan() + P.cables())
    return printed, head_local, hw


def boxes_meet(a, b):
    p, q = a.bounding_box(), b.bounding_box()
    return all(getattr(p.min, k) < getattr(q.max, k) and getattr(q.min, k) < getattr(p.max, k) for k in "XYZ")


def overlap(a, b):
    if not boxes_meet(a, b):
        return 0.0
    common = a & b
    return 0.0 if common is None else common.volume


def pair_in(m, n, pairs):
    return any((m.startswith(x) and n.startswith(y)) or (m.startswith(y) and n.startswith(x)) for x, y in pairs)


# The screws that cut their own thread in a printed part: they go into a 2.5 mm hole.
SELF_TAP = [("display_screw", "head"), ("pi_screw", "plate")]


def check_case(printed, hw):
    problems = []
    for hn, hs, _ in hw:
        for pn, ps in printed.items():
            if pair_in(hn, pn, SELF_TAP):
                continue
            v = overlap(hs, ps)
            if v > 0.5:
                problems.append(f"{hn} touches {pn}: {v:.1f} mm3")
    names = list(printed)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            v = overlap(printed[a], printed[b])
            if v > 1.0:
                problems.append(f"{a} overlaps {b}: {v:.1f} mm3")
    for i, (m, a, _) in enumerate(hw):
        for n, b, _ in hw[i + 1:]:
            if pair_in(m, n, MATES):
                continue
            v = overlap(a, b)
            if v > 0.5:
                problems.append(f"{m} touches {n}: {v:.1f} mm3")
    return problems


def printer_solids():
    """The parts of the printer near the mount, as solids, in printer coordinates."""
    out = {}
    for name, s in WH.control_box_parts().items():
        out[name] = Solid(s)
    for name in ("final_frame_01", "x_axis_01", "y_bed_01", "aerostruder_v2_HS0_5_01",
                 "electronics_chassis_mount_front_v1_01", "electronics_chassis_mount_back_v1_01",
                 "electronics_chassis_mount_back_v1_02", "interconnect_strain_relief_01"):
        shp = WH.load(name)
        for i, sh in enumerate(shp.shells()):
            if sh.bounding_box().max.X > 300:
                try:
                    out[f"{name}_{i}"] = Solid(sh)
                except Exception:
                    out[f"{name}_{i}"] = sh
    return out


def check_printer(world_printed, world_hw):
    problems = []
    printer = printer_solids()
    items = list(world_printed.items()) + [(n, s) for n, s, _ in world_hw]
    for n, s in items:
        for pn, ps in printer.items():
            if pair_in(n, pn, INTO_PRINTER):
                continue
            v = overlap(s, ps)
            if v > 0.5:
                problems.append(f"{n} touches the printer ({pn}): {v:.1f} mm3")
    b = world_printed["plate"].bounding_box()
    if b.min.Y < WH.CHASSIS["y"][0] - 0.01 or b.max.Y > WH.CHASSIS["y"][1] + 0.01 \
            or b.min.Z < WH.CHASSIS["z"][0] - 3.5 or b.max.Z > WH.CHASSIS["z"][1] + 0.01:
        # The flange at the front edge goes 3.2 mm past the front face of the chassis.
        problems.append("the plate is not inside the outline of the chassis side face")
    if abs(b.min.X - WH.COVER_FACE_X) > 0.01:
        problems.append(f"the plate is not on the cover face: X {b.min.X:.2f}")
    return problems


def check_screw_holes():
    """The cover screws are to be confirmed: check them against the cover holes of the LulzBot
    drawings, and report the result. A screw on no hole is a problem only once the positions
    are confirmed."""
    problems, lines = [], []
    for cover, holes in WH.COVER_HOLES.items():
        world = [WH.cover_hole_world(x, y) for x, y in holes]
        on = 0
        for x, y in S.MOUNT_SCREWS:
            p = P.LOC * Pos(x, y, 0)
            q = (p.position.X, p.position.Y, p.position.Z)
            if min(math.dist(q, w) for w in world) < 0.05:
                on += 1
        lines.append(f"{cover}: {on} of {len(S.MOUNT_SCREWS)} cover screws on its holes")
    # The printer has cover PP-FP0164: each screw must be within the slot of its pad.
    world = [WH.cover_hole_world(x, y) for x, y in WH.COVER_HOLES["PP-FP0164"]]
    for i, (x, y) in enumerate(S.MOUNT_SCREWS):
        p = P.LOC * Pos(x, y, 0)
        q = (p.position.X, p.position.Y, p.position.Z)
        w = min(world, key=lambda w: math.dist(q, w))
        if abs(w[2] - q[2]) > S.MOUNT_SLOT_W / 2 - 1.5 or abs(w[1] - q[1]) > S.MOUNT_SLOT / 2:
            problems.append(f"cover screw {i} is not within its slot over a PP-FP0164 hole")
        lines.append(f"cover screw {i}: {S.COVER_SCREWS_MM[i][0]:.1f} mm above the chassis bottom, "
                     f"{S.COVER_SCREWS_MM[i][1]:.1f} mm from the front face; PP-FP0164 hole "
                     f"{w[1] - WH.CHASSIS['y'][0]:.1f} mm up, {w[2] - WH.CHASSIS_FRONT_Z:.1f} "
                     f"mm from the front face ({S.COVER_SCREW_NOTES[i]})")
    for yc in S.CLIPS_Y:
        p = P.LOC * Pos(S.RAIL_FRONT_X, yc, S.RAIL_SLOT_Z)
        if abs(p.position.X - 399.6) > 0.01 or abs(p.position.Z - (-10.0)) > 0.01:
            problems.append(f"rail clip at y {yc} is not at the slot centre X 399.6 on the rail face Z -10")
    return problems, lines


def moved(items, off):
    return [(n, off * s) for n, s in items]


def check_paths(printed, hw):
    """The head with the back panel drops CLIP_DROP onto the keys of the rail clips, and the pod
    slides 45 degrees onto its hooks. The cables bend, so they are not in this check."""
    problems = []
    hwd = {n: s for n, s, _ in hw}
    on_head = [("head", printed["head"]), ("back", printed["back"])] + \
              [(n, s) for n, s in hwd.items() if n.startswith(("display_", "head_screw", "ssd_", "drive_screw",
                                                                "insert_display", "insert_head", "insert_back"))]
    on_plate = [("plate", printed["plate"])] + \
               [(n, s) for n, s in hwd.items() if n.startswith(("pi_", "plug_", "mount_", "insert_pi", "insert_pod"))] + \
               [(n, printed[n]) for n in ("clip_a", "clip_b")] + \
               [(n, s) for n, s in hwd.items() if n.startswith("hammer_nut")]
    printer = printer_solids()
    near = [(n, s) for n, s in printer.items() if n in ("chassis", "cover") or n == "final_frame_01_21"]
    # The head drops CLIP_DROP onto the clip keys; before that it comes straight from the front.
    d = S.CLIP_DROP
    steps = [(0, d * f, 0) for f in (0.2, 0.4, 0.6, 0.8, 1.0)] + [(dx, d, 0) for dx in (3.0, 6.0, 12.0)]
    for dx, dy, dz in steps:
        off = Pos(dx, dy, dz)
        for mn, ms in moved(on_head, off):
            for fn, fs in on_plate:
                v = overlap(ms, fs)
                if v > 0.5:
                    problems.append(f"head drop ({dx:.0f}, {dy:.0f}): {mn} touches {fn}: {v:.1f} mm3")
            w = P.to_world(ms)
            for fn, fs in near:
                v = overlap(w, fs)
                if v > 0.5:
                    problems.append(f"head drop ({dx:.0f}, {dy:.0f}): {mn} touches the printer ({fn}): {v:.1f} mm3")
    fixed = on_plate + on_head
    # Along the 45 degree path until the fins are out of the slots (7.5 mm), then straight out.
    slide = [(0, P.E[0] * s, P.E[1] * s) for s in (1.5, 3.0, 4.5, 6.0, 7.5)]
    # Then 4 mm to the rear (the screw heads at the back of the head are in front of the pod),
    # and straight out.
    slide += [(-2.0, P.E[0] * 7.5, P.E[1] * 7.5 + 1.0), (-4.0, P.E[0] * 7.5, P.E[1] * 7.5 + 2.0)]
    slide += [(-4.0, P.E[0] * 7.5, P.E[1] * 7.5 + dz) for dz in (6.0, 12.0, 20.0, 28.0)]
    for dx, dy, dz in slide:
        s_mm = math.hypot(dy, dz)
        off = Pos(dx, dy, dz)
        for mn, ms in moved([("pod", printed["pod"])], off):
            for fn, fs in fixed:
                v = overlap(ms, fs)
                if v > 0.5:
                    problems.append(f"pod slide {s_mm:.1f} mm: {mn} touches {fn}: {v:.1f} mm3")
    # The grille with the fan goes in last, straight into the pocket from the outside. The fan
    # cable bends, so it is not in this check.
    fixed += [("pod", printed["pod"])] + [(n, s) for n, s in hwd.items() if n.startswith("cable_") and n != "cable_fan"]
    on_grille = [("grille", printed["grille"])] + [(n, s) for n, s in hwd.items() if n.startswith("fan_")]
    for dz in (2.0, 5.0, 10.0, 15.0, 25.0, 40.0):
        for mn, ms in moved(on_grille, Pos(0, 0, dz)):
            for fn, fs in fixed:
                v = overlap(ms, fs)
                if v > 0.5:
                    problems.append(f"grille in {dz:.0f} mm: {mn} touches {fn}: {v:.1f} mm3")
    return problems


def check_fan(printed, hw):
    """The fan sits in the snaps of the grille: 0.2 mm lower, it goes into the four catches; it can
    move up by FAN_PLAY to the funnel, and no more; the prongs bend less than 1.2 %."""
    problems = []
    frame = next(s for n, s, _ in hw if n == "fan_frame")
    g = printed["grille"]
    down = overlap(Pos(0, 0, -0.2) * frame, g)
    w, _, d = S.PRONG
    if down < 4 * 0.2 * w * (d - 0.1) * 0.9:          # the top 0.2 mm of each 45 degree catch
        problems.append(f"the fan is not on the four catches: {down:.1f} mm3 under it")
    if overlap(Pos(0, 0, S.FAN_PLAY - 0.05) * frame, g) > 0.5:
        problems.append("the funnel of the bump presses on the fan")
    if overlap(Pos(0, 0, S.FAN_PLAY + 0.15) * frame, g) < 0.5:
        problems.append("the funnel of the bump does not stop the fan")
    strain, length = P.prong_strain()
    if strain > 0.012:
        problems.append(f"the prongs bend {strain * 100:.1f} %: more than 1.2 %")
    z0, _ = P.fan_z()
    log(f"  fan: on four catches {d:.1f} mm deep, {S.FAN_PLAY:.1f} mm of play; prongs {length:.1f} mm long, "
        f"{S.PRONG[1]:.1f} mm thick, strain {strain * 100:.2f} %; {z0 - S.PI_Z - 1.5 - 2.2 - S.HEATSINK_H:.1f} mm "
        f"of air over a {S.HEATSINK_H:.0f} mm heatsink; the bump is {S.BUMP_TOP - S.POD_Z:.1f} mm out of the pod")
    return problems


def print_pose(name, shape, head_local):
    orient = PRINTED[name][1]
    if orient == "face":
        s = Rot(180, 0, 0) * head_local
    elif orient == "rear":
        s = Rot(0, -90, 0) * shape
    elif orient == "outer":
        s = Rot(180, 0, 0) * shape
    elif orient == "end":
        s = Rot(90, 0, 0) * shape
    else:
        s = shape
    bb = s.bounding_box()
    return Pos(-(bb.min.X + bb.max.X) / 2, -(bb.min.Y + bb.max.Y) / 2, -bb.min.Z) * s


def export(printed, head_local, world_printed, world_hw):
    for d in ("cad", "stl", os.path.join("build", "mesh")):
        os.makedirs(os.path.join(HERE, d), exist_ok=True)
    mesh_dir = os.path.join(BUILD, "mesh")
    for f in os.listdir(mesh_dir):
        os.remove(os.path.join(mesh_dir, f))
    manifest, children = [], []
    hb = world_printed["head"].bounding_box()
    summary = {"parts": {}, "cables": {}}
    for name, shape in world_printed.items():
        mat, _, stl = PRINTED[name]
        export_step(shape, os.path.join(HERE, "cad", name + ".step"))
        pose = print_pose(name, printed[name], head_local)
        if stl:
            export_stl(pose, os.path.join(HERE, "stl", stl + ".stl"), tolerance=0.01, angular_tolerance=0.1)
            export_stl(pose, os.path.join(mesh_dir, "print_" + stl + ".stl"), tolerance=0.02, angular_tolerance=0.15)
        export_stl(shape, os.path.join(mesh_dir, name + ".stl"), tolerance=0.02, angular_tolerance=0.15)
        manifest.append({"name": name, "file": name + ".stl", "material": mat, "group": name})
        bb = pose.bounding_box()
        vol = shape.volume / 1000
        summary["parts"][name] = {"size": [round(bb.size.X, 1), round(bb.size.Y, 1), round(bb.size.Z, 1)],
                                  "volume_cm3": round(vol, 1), "grams_solid": round(vol * PLA, 1), "material": mat}
        log(f"  {name:10s} print size {bb.size.X:5.1f} x {bb.size.Y:5.1f} x {bb.size.Z:5.1f} mm, "
            f"volume {vol:5.1f} cm3, {vol * PLA:5.1f} g solid")
        c = shape
        c.label = name
        c.color = Color(*hex_rgb(COLOURS[mat]))
        children.append(c)
    for name, shape, mat in world_hw:
        group = next((g for p, g in GROUPS.items() if name.startswith(p)), "other")
        export_stl(shape, os.path.join(mesh_dir, "hw_" + name + ".stl"), tolerance=0.05, angular_tolerance=0.2)
        manifest.append({"name": "hw_" + name, "file": "hw_" + name + ".stl", "material": mat, "group": group})
        c = shape
        c.label = name
        c.color = Color(*hex_rgb(COLOURS.get(mat, "#888888")))
        children.append(c)
    export_step(Compound(children=children, label="printdeck-lulzbot"), os.path.join(HERE, "cad", "lulzbot-assembly.step"))
    # The printer near the mount, for the line-art renders: the chassis, the cover, and the front
    # left rail, cut to the height of the mount.
    y0 = P.Y0 + S.POD_Y0 - 40.0
    y1 = WH.CHASSIS["y"][1] + 30.0
    cut = Pos(300, y0, -250) * Box(400, y1 - y0, 600, align=(Align.MIN, Align.MIN, Align.MIN))
    cb = WH.control_box_parts()
    frame = WH.load("final_frame_01").shells()[21]
    for name, shape in (("chassis", Solid(cb["chassis"])), ("cover", WH.cover_world("PP-FP0164")), ("rail", Solid(frame))):
        part = shape & cut
        export_stl(part, os.path.join(mesh_dir, f"printer_{name}.stl"), tolerance=0.1, angular_tolerance=0.2)
        manifest.append({"name": f"printer_{name}", "file": f"printer_{name}.stl", "material": "printer",
                         "group": "printer"})
    export_stl(WH.cover_world("PP-FP0164"), os.path.join(mesh_dir, "printer_cover_full.stl"), tolerance=0.1,
               angular_tolerance=0.2)
    # Cable length, plug end to plug end: the centre line, plus the plugs (30 mm for a USB-A
    # plug and for the display HDMI plug, 20 mm for a micro HDMI plug, 32 mm for the micro USB
    # plug, 28 mm for a USB-C plug). For the printer and the power cable, the length is to the
    # rear face of the chassis.
    plugs = {"hdmi": 50, "touch": 62, "drive": 52, "printer": 30, "power": 28, "fan": 0}
    for k, pts in P.cable_paths().items():
        if k in ("printer", "power"):
            pts = [p for p in pts if p[0] > -S.X_FRONT] + [(-S.X_FRONT, pts[-1][1], pts[-1][2])]
        summary["cables"][k] = round(P.cable_length(pts) + plugs[k])
    c = summary["cables"]
    log(f"  cables, plug to plug: HDMI {c['hdmi']} mm, touch USB {c['touch']} mm, drive {c['drive']} mm; "
        f"to the rear face of the chassis: "
        f"printer USB {c['printer']} mm, power {c['power']} mm; fan, from the groove to the plug, {c['fan']} mm")
    # Where the head is on the printer.
    face_top = P.LOC * Pos(*S.HEAD_TOP, 0)
    summary["head"] = {
        "size_world": [round(hb.size.X, 1), round(hb.size.Y, 1), round(hb.size.Z, 1)],
        "x_range": [round(hb.min.X, 1), round(hb.max.X, 1)],
        "bottom_above_chassis_bottom": round(hb.min.Y - WH.CHASSIS["y"][0], 1),
        "top_above_chassis_bottom": round(hb.max.Y - WH.CHASSIS["y"][0], 1),
        "front_of_chassis_face": round(WH.CHASSIS_FRONT_Z - hb.min.Z, 1),
        "face_top_edge_z": round(face_top.position.Z, 1),
    }
    pb = world_printed["pod"].bounding_box()
    summary["pod"] = {"size_world": [round(pb.size.X, 1), round(pb.size.Y, 1), round(pb.size.Z, 1)]}
    log(f"  head on the printer: {summary['head']}")
    log(f"  pod on the printer: {summary['pod']}")
    hlist, _ = hardware_list(world_hw)
    with open(os.path.join(BUILD, "hardware.json"), "w") as fh:
        json.dump({"items": hlist, "cover_confirmed": S.COVER_SCREWS_CONFIRMED, "cover_notes": S.COVER_SCREW_NOTES,
                   "cover_screws_mm": S.COVER_SCREWS_MM, "inserts": S.INSERTS}, fh, indent=1, ensure_ascii=False)
    with open(os.path.join(BUILD, "parts.json"), "w") as fh:
        json.dump(summary, fh, indent=1)
    with open(os.path.join(BUILD, "manifest.json"), "w") as fh:
        json.dump({"parts": manifest, "screen": P.screen_corners()}, fh, indent=1)


def main():
    printed, head_local, hw = build()
    problems = check_case(printed, hw)
    log(f"  mount check: {len(problems)} problems")
    paths = check_paths(printed, hw)
    log(f"  assembly path check: {len(paths)} problems")
    paths += check_fan(printed, hw)
    holes, lines = check_screw_holes()
    for line in lines:
        log("  " + line)
    world_printed = {n: P.to_world(s) for n, s in printed.items()}
    world_hw = [(n, P.to_world(s), m) for n, s, m in hw]
    printer = check_printer(world_printed, world_hw)
    _, hwp = hardware_list(hw)
    printer += hwp
    log(f"  printer check: {len(printer)} problems")
    problems += paths + holes + printer
    for p in problems:
        log("  CLASH: " + p)
    if not problems:
        log("  fit check: the display, the Pi, the fan, the screws, the inserts, and the cables are clear of the "
            "printed parts and the printer; no parts overlap; the head hangs on the clips, the pod slides on, and the "
            "grille with the fan goes in; the fan sits in its snaps; the clips and their hammer nuts are on the rail "
            "slot; the hardware is in stock")
    if "--no-export" not in sys.argv:
        export(printed, head_local, world_printed, world_hw)
    log("RESULT: " + ("CLASHES" if problems else "OK"))


if __name__ == "__main__":
    main()
