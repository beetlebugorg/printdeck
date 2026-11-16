"""SE tool head duct: the fit check, then the STEP files, the STL files in print orientation, the
meshes for the images (build/mesh), build/manifest.json and build/parts.json.
Prints RESULT: OK or RESULT: CLASHES.

    uv run --no-project --with build123d python model.py
"""

import json
import os

from build123d import Compound, Pos, Rot, export_step, export_stl

import cad as C
import parts as P
import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")

# name: (function, the face on the bed)
PRINTED = {
    "duct": (P.duct, "bottom"),
    "blower_bracket": (P.blower_bracket, "right"),
    "bltouch_bracket": (P.bltouch_bracket, "left"),
    "motor_spacer": (P.motor_spacer, "bottom"),
    "fit_lip": (P.fit_lip, "bottom"),
}


def overlap(a, b):
    ba, bb = a.bounding_box(), b.bounding_box()
    if (ba.max.X < bb.min.X or bb.max.X < ba.min.X or ba.max.Y < bb.min.Y or bb.max.Y < ba.min.Y
            or ba.max.Z < bb.min.Z or bb.max.Z < ba.min.Z):
        return 0.0
    i = a & b
    return 0.0 if i is None else sum(s.volume for s in i.solids())


def gap(a, b):
    return a.distance_to(b)


def check(problems):
    duct, brk, blt_brk = P.duct(), P.blower_bracket(), P.bltouch_bracket()
    printed = [("duct", duct), ("blower bracket", brk), ("BLTouch bracket", blt_brk)]
    for n, s in printed:
        if len(s.solids()) != 1 or not s.is_valid:
            problems.append(f"{n}: {len(s.solids())} solids, valid {s.is_valid}")
    blower, blt = P.blower(), P.bltouch()
    ours = printed + [("blower", blower), ("BLTouch", blt)]
    head = P.head_parts()
    carriage = [(f"carriage {i}", s) for i, s in enumerate(C.carriage())]
    # 1. Nothing overlaps the head that stays (the Aero, the mount, the motor, the heat sink fan),
    #    the carriage, or the room of the motor connector.
    for n, s in ours:
        for hn, hs in head + carriage + [("motor connector and plug", P.connector_room())]:
            v = overlap(s, hs)
            if v > 0.01:
                problems.append(f"the {n} overlaps the {hn}: {v:.2f} mm3")
    # 2. The parts, the blower and the BLTouch do not overlap each other. The CAD blower has a
    #    solid outlet (a 1 mm recess only): leave out the lip of the duct for that pair.
    lip = P.box(*P.lip_outside(), (S.DUCT_Z1, S.DUCT_Z1 + S.INLET_LIP + 1))
    for i in range(len(ours)):
        for j in range(i + 1, len(ours)):
            a, b = ours[i][1], ours[j][1]
            if {ours[i][0], ours[j][0]} == {"duct", "blower"}:
                a = a - lip
            v = overlap(a, b)
            if v > 0.01:
                problems.append(f"the {ours[i][0]} overlaps the {ours[j][0]}: {v:.2f} mm3")
    # 3. X max: the right X end. X home: the left X end.
    for side, ends in (("X max", C.x_end()), ("X home", P.left_x_end())):
        for n, s in ours + head:
            v = sum(overlap(s, e) for e in ends)
            if v > 0.01:
                problems.append(f"{side}: the {n} hits the X end ({v:.2f} mm3)")
    # 4. The space to the nearest parts.
    xend = Compound(children=C.x_end())
    lend = Compound(children=P.left_x_end())
    space = {
        "BLTouch body to the duct": gap(blt, duct),
        "BLTouch to the heat sink fan": gap(blt, dict(head)["hs_fan"]),
        "BLTouch to the mount": gap(blt, dict(head)["mount"]),
        "BLTouch to the carriage": min(gap(blt, s) for _, s in carriage),
        "blower to the right X end at X max": gap(blower, xend),
        "duct to the right X end at X max": gap(duct, xend),
        "BLTouch bracket to the left X end at X home": gap(blt_brk, lend),
        "BLTouch to the left X end at X home": gap(blt, lend),
        "duct to the motor connector room": gap(duct, P.connector_room()),
        "duct bottom over the nozzle tip": S.DUCT_Z0,
    }
    return duct, brk, blt_brk, {k: round(v, 2) for k, v in space.items()}


def print_pose(shape, face):
    if face == "right":      # the right face (x max) on the bed
        s = Rot(0, 90, 0) * shape
    elif face == "left":     # the left face (x min) on the bed
        s = Rot(0, -90, 0) * shape
    else:
        s = shape
    bb = s.bounding_box()
    return Pos(-bb.min.X, -bb.min.Y, -bb.min.Z) * s


def main():
    problems = []
    duct, brk, blt_brk, space = check(problems)
    for p in problems:
        print("  CLASH: " + p)
    print("  space: " + ", ".join(f"{k} {v} mm" for k, v in space.items()))
    print(f"  BLTouch pin: x_offset {S.PIN_X:.2f}, y_offset {S.PIN_Y:.2f} (printer.cfg [bltouch])")
    if not problems:
        print("  fit check: the parts are clear of the head, the carriage, the motor connector, each other, "
              "the right X end at X max and the left X end at X home.")

    mesh_dir = os.path.join(BUILD, "mesh")
    for d in ("cad", "stl", mesh_dir):
        os.makedirs(os.path.join(HERE, d), exist_ok=True)
    for d in (mesh_dir, os.path.join(HERE, "cad"), os.path.join(HERE, "stl")):
        for f in os.listdir(d):
            os.remove(os.path.join(d, f))
    manifest, summary = [], {"parts": {}, "space": space, "pin": [S.PIN_X, round(S.PIN_Y, 2)]}

    def mesh(name, shape, mat, group, tol=0.05):
        export_stl(shape, os.path.join(mesh_dir, name + ".stl"), tolerance=tol, angular_tolerance=0.1)
        manifest.append({"name": name, "file": name + ".stl", "material": mat, "group": group})

    parts = {"duct": duct, "blower_bracket": brk, "bltouch_bracket": blt_brk, "fit_lip": P.fit_lip(),
             "motor_spacer": P.motor_spacer()}
    for name, (fn, face) in PRINTED.items():
        part = parts[name]
        if not name.startswith("fit_"):
            export_step(part, os.path.join(HERE, "cad", name + ".step"))
        pose = print_pose(part, face)
        export_stl(pose, os.path.join(HERE, "stl", name + ".stl"), tolerance=0.01, angular_tolerance=0.05)
        bb = pose.bounding_box()
        summary["parts"][name] = {"size": [round(bb.size.X, 1), round(bb.size.Y, 1), round(bb.size.Z, 1)],
                                  "volume_cm3": round(part.volume / 1000, 1)}
        print(f"  {name:16s} print size {bb.size.X:5.1f} x {bb.size.Y:5.1f} x {bb.size.Z:5.1f} mm, "
              f"volume {part.volume / 1000:4.1f} cm3, valid {pose.is_valid}, {len(pose.solids())} solid")

    mesh("duct", duct, "petg", "duct")
    mesh("blower_bracket", brk, "petg", "bracket")
    mesh("bltouch_bracket", blt_brk, "petg", "bltmount")
    mesh("fit_lip", parts["fit_lip"], "petg", "fitlip")
    mesh("blower", P.blower(), "dark", "blower", tol=0.1)
    mesh("bltouch", P.bltouch(), "bltouch", "bltouch")
    for i, s in enumerate(P.bracket_screws()):
        mesh(f"bracket_screw_{i}", s, "steel", "cskscrews" if i < 2 else "blowerscrews")
    screws, nuts = P.bltouch_hardware()
    for i, s in enumerate(screws):
        mesh(f"blt_screw_{i}", s, "steel", "fanscrews" if i < 2 else "bltscrews")
    for i, n in enumerate(nuts):
        mesh(f"blt_nut_{i}", n, "steel", "bltscrews")
    for n, s in P.head_parts():
        mesh("head_" + n, s, "dark", "head", tol=0.15)
    mesh("motor_spacer", P.spacer_placed(), "petg", "spacer")
    mesh("foot_screw", P.foot_screw(), "steel", "footscrew")
    mesh("nozzle_tip", P.box((-2.5, 2.5), (-2.5, 2.5), (0, 3.0)), "bltouch", "nozzle")
    mesh("stock_duct", C.se("duct"), "dark", "stockduct", tol=0.15)
    assembly = [duct, brk, blt_brk, P.blower(), P.bltouch()]
    export_step(Compound(children=assembly), os.path.join(HERE, "cad", "se-duct-assembly.step"))

    with open(os.path.join(BUILD, "parts.json"), "w") as fh:
        json.dump(summary, fh, indent=1)
    with open(os.path.join(BUILD, "manifest.json"), "w") as fh:
        json.dump({"parts": manifest}, fh, indent=1)
    print("RESULT: " + ("CLASHES" if problems else "OK"))


if __name__ == "__main__":
    main()
