"""Gantry camera arm: the fit check, then the STEP files, the STL files in print orientation, the
meshes for the images (build/mesh), build/manifest.json and build/parts.json.
Prints RESULT: OK or RESULT: CLASHES.

    uv run --no-project --with build123d python model.py
"""

import json
import math
import os

from build123d import Compound, Pos, Rot, export_step, export_stl

import parts as P
import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")

PRINTED = {
    "clamp_plate": (P.clamp_plate, "back"),
    "upper_arm": (P.upper_arm, "top"),
    "camera_bracket": (P.camera_bracket, "bottom"),
    "fit_arm_slice": (P.fit_arm_slice, "top"),
    "fit_bracket_slice": (P.fit_bracket_slice, "bottom"),
}


def overlap(a, b):
    i = a & b
    return 0.0 if i is None else sum(s.volume for s in i.solids())


def stiffness():
    """The first natural frequency of the camera on the arm, from two cantilevers in series: the
    drop (from the beam down to the bracket joint) and the bracket front wall (from the tongue to
    the camera screw). PETG E = 2000 MPa; the camera and its U bracket about 45 g."""
    e, m = 2000.0, 0.045
    # Bending in x (the camera swings left and right): the drop section 9 (x) by 11.9 (y).
    bx = S.DROP_X[1] - S.DROP_X[0]
    by = S.BEAM_Y1 - S.BEAM_Y0
    l1 = S.BEAM_Z0 - sum(S.JOINT_Z) / 2
    k1x = 3 * e * (by * bx ** 3 / 12) / l1 ** 3
    k1y = 3 * e * (bx * by ** 3 / 12) / l1 ** 3
    # The front wall: 8 (y) by 44 (z), from the tongue to the camera screw, bending in y.
    l2 = S.SLOT_X - S.DROP_X[1]
    h = S.CAM_Z[1] - S.CAM_Z[0]
    k2y = 3 * e * (h * S.WALL ** 3 / 12) / l2 ** 3
    kx = k1x
    ky = 1 / (1 / k1y + 1 / k2y)
    f = lambda k: math.sqrt(k * 1000 / m) / (2 * math.pi)
    return {"left_right_hz": round(f(kx)), "front_back_hz": round(f(ky))}


def check(problems):
    plate, arm, bracket = P.clamp_plate(), P.upper_arm(), P.camera_bracket()
    for n, s in (("clamp plate", plate), ("upper arm", arm), ("camera bracket", bracket)):
        if len(s.solids()) != 1 or not s.is_valid:
            problems.append(f"{n}: {len(s.solids())} solids, valid {s.is_valid}")
    cam_case, cam_bracket = P.camera()
    heads = P.screws()
    ours = [("clamp plate", plate), ("upper arm", arm), ("camera bracket", bracket), ("camera", cam_case),
            ("U bracket", cam_bracket)] + [(f"screw head {i}", h) for i, h in enumerate(heads)]
    xend = P.x_end()
    # 1. Nothing overlaps the X end, except the plate on its face.
    for n, s in ours:
        v = overlap(s, xend)
        if v > 0.01:
            problems.append(f"{n} overlaps the X end: {v:.2f} mm3")
    # 2. The parts do not overlap each other (the plate sits in the pocket of the arm).
    pairs = [("clamp plate", plate, "upper arm", arm), ("upper arm", arm, "camera bracket", bracket),
             ("camera", cam_case, "camera bracket", bracket), ("U bracket", cam_bracket, "camera bracket", bracket),
             ("screw head 1", heads[1], "upper arm", arm), ("screw head 0", heads[0], "clamp plate", plate)]
    for an, a, bn, b in pairs:
        v = overlap(a, b)
        if v > 0.01:
            problems.append(f"{an} overlaps {bn}: {v:.2f} mm3")
    # 3. The tool head passes the arm at each X, and at the right end (dx = 0) with a margin.
    gap_parts = [(n, s) for n, s in ours]
    for dx in [0.0, -10.0, -30.0, -60.0, -100.0, -150.0, -200.0, -260.0, -343.0]:
        for hn, hs in P.head(dx):
            for n, s in gap_parts:
                v = overlap(s, hs)
                if v > 0.01:
                    problems.append(f"head at X {293 + dx:.0f}: {hn} hits the {n} ({v:.2f} mm3)")
    margins = {}
    # 4. The frame: the upright and the corner plates, for Z from 0 to Z max.
    zs = list(range(0, int(S.Z_MAX) + 1, 11)) + [S.Z_MAX]
    for z in zs:
        up, top, bottom = P.upright(z)
        for n, s in ours:
            for fn, f in (("the upright", up), ("the top corner plate", top), ("the bottom corner plate", bottom)):
                v = overlap(s, f)
                if v > 0.01:
                    problems.append(f"Z {z:.0f}: the {n} hits {fn} ({v:.2f} mm3)")
                    break
    # 5. The install path: the arm (with the plate) comes in from the front, and the bracket too.
    up, top, bottom = P.upright(150.0)
    for step in range(2, 41, 2):
        for n, s in (("upper arm", arm), ("camera bracket", bracket)):
            moved = Pos(0, -step, 0) * s
            v = overlap(moved, up)
            if v > 0.01:
                problems.append(f"install path: the {n} hits the upright {step} mm in front of its place")
    # 6. The lower screw head: room to the motor at the right end of X.
    head_front = S.FACE_Y - S.PLATE_T          # the head is flush with the plate
    motor_front = S.FACE_Y - S.M3_HEAD_H - S.LOWER_CLEAR   # 9 mm in front of the original head
    space = {"lower plate to motor": round(head_front - motor_front, 1),
             "beam to the head at the upper screw": round(S.BEAM_Y0 - (S.FACE_Y - S.M3_HEAD_H - S.UPPER_CLEAR), 1),
             "beam over the motor top": round(S.BEAM_Z0 - S.HEAD_SPLIT_Z, 1),
             "drop to the BLTouch tab": round(S.DROP_X[0] - S.TAB_X1, 1),
             "drop to the upright": round(S.UP_X0 - S.DROP_X[1], 1),
             "bracket top under the top plate at Z max": round(S.TOP_PLATE_Z - S.Z_MAX - S.CAM_Z[1], 1),
             "bracket to the upright face": S.FRONT_GAP,
             "beam front behind the upright face": round(S.BEAM_Y0 - S.UP_Y0, 1)}
    if space["lower plate to motor"] < 2.0:
        problems.append(f"only {space['lower plate to motor']} mm from the lower plate to the motor")
    return plate, arm, bracket, space, margins


def print_pose(shape, face):
    if face == "back":       # the back (y max) on the bed
        s = Rot(-90, 0, 0) * shape
    elif face == "top":      # upside down: the top (z max) on the bed
        s = Rot(180, 0, 0) * shape
    else:
        s = shape
    bb = s.bounding_box()
    return Pos(-bb.min.X, -bb.min.Y, -bb.min.Z) * s


def main():
    problems = []
    plate, arm, bracket, space, margins = check(problems)
    for p in problems:
        print("  CLASH: " + p)
    print("  space: " + ", ".join(f"{k} {v} mm" for k, v in space.items()))
    st = stiffness()
    print(f"  stiffness: the camera on the arm, about {st['left_right_hz']} Hz left-right, "
          f"{st['front_back_hz']} Hz front-back (input shaper peaks about 40 to 60 Hz)")
    lp = P.lens_point()
    print(f"  lens: x {lp[0]:.1f}, y {lp[1]:.1f}, z {lp[2]:.1f} (slot {S.SLOT_Z[0]} to {S.SLOT_Z[1]} mm over the tip); "
          f"{math.dist(lp, P.nozzle(0.0)):.0f} to {math.dist(lp, P.nozzle(-343.0)):.0f} mm to the nozzle over the X travel")
    if not problems:
        print("  fit check: the parts are clear of the X end, the tool head at each X, the upright and the corner "
              "plates at each Z, and on the install path.")

    mesh_dir = os.path.join(BUILD, "mesh")
    for d in ("cad", "stl", mesh_dir):
        os.makedirs(os.path.join(HERE, d), exist_ok=True)
    for d in (mesh_dir, os.path.join(HERE, "cad"), os.path.join(HERE, "stl")):
        for f in os.listdir(d):
            os.remove(os.path.join(d, f))
    manifest, summary = [], {"parts": {}, "space": space, "stiffness": st}

    def mesh(name, shape, mat, group, tol=0.05):
        export_stl(shape, os.path.join(mesh_dir, name + ".stl"), tolerance=tol, angular_tolerance=0.1)
        manifest.append({"name": name, "file": name + ".stl", "material": mat, "group": group})

    for name, (fn, face) in PRINTED.items():
        part = fn()
        if not name.startswith("fit_"):
            export_step(part, os.path.join(HERE, "cad", name + ".step"))
        pose = print_pose(part, face)
        export_stl(pose, os.path.join(HERE, "stl", name + ".stl"), tolerance=0.01, angular_tolerance=0.05)
        bb = pose.bounding_box()
        summary["parts"][name] = {"size": [round(bb.size.X, 1), round(bb.size.Y, 1), round(bb.size.Z, 1)],
                                  "volume_cm3": round(part.volume / 1000, 1)}
        print(f"  {name:18s} print size {bb.size.X:5.1f} x {bb.size.Y:5.1f} x {bb.size.Z:5.1f} mm, "
              f"volume {part.volume / 1000:4.1f} cm3, valid {pose.is_valid}, {len(pose.solids())} solid")

    cam_case, cam_bracket = P.camera()
    mesh("clamp_plate", plate, "petg", "plate")
    mesh("upper_arm", arm, "petg", "arm")
    mesh("camera_bracket", bracket, "petg", "bracket")
    mesh("camera", cam_case, "camera", "camera")
    mesh("camera_ubracket", cam_bracket, "steel", "camera")
    for i, h in enumerate(P.screws()):
        mesh(f"screw_{i}", h, "steel", "screws")
    mesh("x_end", P.x_end(), "frame", "x_end")
    up, top, bottom = P.upright(150.0)
    mesh("upright", P.box((S.UP_X0, S.UP_X0 + S.UP_W), (S.UP_Y0, S.UP_Y0 + S.UP_W), (-20, 140)) & P.upright(0.0)[0],
         "frame", "upright")
    for hn, hs in P.head(0.0):
        if hn != "nozzle":
            mesh("head_" + hn.replace(" ", "_"), P.box((S.UP_X0 - 140, S.UP_X0 + 40), (-130, 40), (-10, 130)) & hs,
                 "keepout" if hn != "BLTouch tab" else "bltouch", "head")
    assembly = [plate, arm, bracket, cam_case, cam_bracket] + P.screws() + [P.x_end()]
    export_step(Compound(children=assembly), os.path.join(HERE, "cad", "gantry-camera-arm-assembly.step"))

    with open(os.path.join(BUILD, "parts.json"), "w") as fh:
        json.dump(summary, fh, indent=1)
    with open(os.path.join(BUILD, "manifest.json"), "w") as fh:
        json.dump({"parts": manifest}, fh, indent=1)
    print("RESULT: " + ("CLASHES" if problems else "OK"))


if __name__ == "__main__":
    main()
