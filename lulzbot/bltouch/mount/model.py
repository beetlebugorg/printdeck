"""BLTouch mount: the fit check, then the STEP files, the STL file in print orientation, the meshes
for the images (build/mesh), build/manifest.json and build/parts.json.
Prints RESULT: OK or RESULT: CLASHES.

    uv run --no-project --with build123d python model.py
"""

import json
import os

from build123d import Compound, Pos, Rot, export_step, export_stl

import parts as P
import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")


def overlap(a, b):
    i = a & b
    return 0.0 if i is None else sum(s.volume for s in i.solids())


def coverage():
    """The bed area that the pin reaches, and the probe offsets for printer.cfg."""
    xo, yo = S.PIN_X, S.PIN_Y
    return {"x_offset": round(xo, 1), "y_offset": round(yo, 1),
            "pin_x": [round(S.NOZZLE_X[0] + xo, 1), round(S.NOZZLE_X[1] + xo, 1)],
            "pin_y": [round(S.NOZZLE_Y[0] + yo, 1), round(S.NOZZLE_Y[1] + yo, 1)]}


def check(problems):
    part = P.mount()
    if len(part.solids()) != 1 or not part.is_valid:
        problems.append(f"mount: {len(part.solids())} solids, valid {part.is_valid}")
    blt = P.bltouch()
    heads = P.screws()
    ctx = P.context()
    # 1. Nothing overlaps the motor, the rear screws, the blower, the X end or the hot end.
    for name, shape in [("mount", part), ("BLTouch", blt)] + [(f"screw head {i}", h) for i, h in enumerate(heads)]:
        for cname, c, _ in ctx:
            v = overlap(shape, c)
            if v > 0.01:
                problems.append(f"{name} overlaps {cname}: {v:.2f} mm3")
    # 2. The BLTouch and the screw heads are clear of the mount, and the ears touch the shelf.
    for name, shape in [("BLTouch", blt)] + [(f"screw head {i}", h) for i, h in enumerate(heads)]:
        v = overlap(shape, part)
        if v > 0.01:
            problems.append(f"{name} overlaps the mount: {v:.2f} mm3")
    probe = Pos(0, 0, 0.2) * blt
    if overlap(probe, part) < 1.0:
        problems.append("the ears do not touch the bottom of the shelf")
    # 3. The plate sits flat on the motor face around each front screw.
    for y, z in S.FRONT_SCREWS:
        ring = P.box((S.MOTOR_X1, S.MOTOR_X1 + 0.3), (y + 2.2, y + 2.6), (z - 0.2, z + 0.2))
        if overlap(part, ring) < 0.01:
            problems.append(f"no plate on the motor face at the screw y {y:.1f}, z {z:.1f}")
    # 4. The BLTouch height: the bottom of the body is 2.3 to 4.3 mm over the nozzle tip.
    if not 2.3 <= S.BLT_GAP <= 4.3:
        problems.append(f"the BLTouch bottom is {S.BLT_GAP} mm over the nozzle tip (antclabs: 2.3 to 4.3)")
    # 5. At X 293 the X end passes behind the mount and the BLTouch.
    back = max(part.bounding_box().max.Y, blt.bounding_box().max.Y)
    if S.X_END_Y0 - back < 1.5:
        problems.append(f"only {S.X_END_Y0 - back:.1f} mm in front of the X end")
    # 6. The install path: the plate goes onto the motor face from the right, with the BLTouch on it.
    for step in range(0, 31, 2):
        for name, shape in [("mount", part), ("BLTouch", blt)]:
            moved = Pos(step, 0, 0) * shape
            for cname, c, _ in ctx:
                if cname.startswith("X end"):
                    continue          # the head is away from the X end when you fit the mount
                if overlap(moved, c) > 0.01:
                    problems.append(f"install path: {name} hits {cname} {step} mm from its place")
    clear = {"x_end": round(S.X_END_Y0 - back, 1), "plate_to_bltouch": S.BLT_CLEAR,
             "bltouch_bottom_over_nozzle": S.BLT_GAP, "head_wider_by": round(S.SHELF_X1 - S.MOTOR_X1, 1)}
    return part, clear


def print_pose(shape):
    """The print orientation: the back face (y = PART_Y1) on the bed, the front up."""
    s = Rot(-90, 0, 0) * shape      # y goes to -z: the back face is the lowest
    bb = s.bounding_box()
    return Pos(-bb.min.X, -bb.min.Y, -bb.min.Z) * s


def main():
    problems = []
    part, clear = check(problems)
    for p in problems:
        print("  CLASH: " + p)
    cov = coverage()
    print("  space: " + ", ".join(f"{k.replace('_', ' ')} {v} mm" for k, v in clear.items()))
    print(f"  probe: x_offset {cov['x_offset']}, y_offset {cov['y_offset']}; the pin reaches X {cov['pin_x'][0]} to "
          f"{cov['pin_x'][1]}, Y {cov['pin_y'][0]} to {cov['pin_y'][1]}")
    if not problems:
        print("  fit check: the mount, the BLTouch and the screws are clear of the motor, the blower, the X end and "
              "the hot end, in place and on the install path.")

    mesh_dir = os.path.join(BUILD, "mesh")
    for d in ("cad", "stl", mesh_dir):
        os.makedirs(os.path.join(HERE, d), exist_ok=True)
    for d in (mesh_dir, os.path.join(HERE, "cad"), os.path.join(HERE, "stl")):
        for f in os.listdir(d):
            os.remove(os.path.join(d, f))
    manifest, summary = [], {"parts": {}, "space": clear, "probe": cov}

    def mesh(name, shape, mat, group, tol=0.02):
        export_stl(shape, os.path.join(mesh_dir, name + ".stl"), tolerance=tol, angular_tolerance=0.1)
        manifest.append({"name": name, "file": name + ".stl", "material": mat, "group": group})

    export_step(part, os.path.join(HERE, "cad", "bltouch_mount.step"))
    pose = print_pose(part)
    export_stl(pose, os.path.join(HERE, "stl", "bltouch_mount.stl"), tolerance=0.01, angular_tolerance=0.05)
    bb = pose.bounding_box()
    summary["parts"]["bltouch_mount"] = {"size": [round(bb.size.X, 1), round(bb.size.Y, 1), round(bb.size.Z, 1)],
                                         "volume_cm3": round(part.volume / 1000, 1)}
    print(f"  bltouch_mount print size {bb.size.X:5.1f} x {bb.size.Y:5.1f} x {bb.size.Z:4.1f} mm, "
          f"volume {part.volume / 1000:4.1f} cm3, valid {pose.is_valid}, {len(pose.solids())} solid")

    mesh("mount", part, "petg", "mount")
    mesh("bltouch", P.bltouch(), "bltouch", "bltouch")
    for i, h in enumerate(P.screws()):
        mesh(f"screw_{i}", h, "steel", "screws")
    for name, shape, mat in P.context():
        mesh(name.replace(" ", "_"), shape, mat, name.split(" ")[0])
    assembly = [part, P.bltouch()] + P.screws() + [s for _, s, _ in P.context()]
    export_step(Compound(children=assembly), os.path.join(HERE, "cad", "bltouch-mount-assembly.step"))

    with open(os.path.join(BUILD, "parts.json"), "w") as fh:
        json.dump(summary, fh, indent=1)
    with open(os.path.join(BUILD, "manifest.json"), "w") as fh:
        json.dump({"parts": manifest}, fh, indent=1)
    print("RESULT: " + ("CLASHES" if problems else "OK"))


if __name__ == "__main__":
    main()
