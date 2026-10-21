"""LulzBot TAZ Workhorse reference geometry, from the LulzBot source files in ref/lulzbot/.

final_assembly.fcstd is a FreeCAD file: a zip with one BREP file for each sub-assembly. The
BREP shapes are already in their assembly position, so this module reads them as they are.
The assembly coordinates (mm): X to the left of the printer (seen from the front), Y up, and
Z to the rear. The front of the printer faces -Z. The electronics chassis is the tall box on
the left side. Its cover (2 mm aluminium) is the outer face at X = 510.22 to 512.22.

The assembly has the older cover PP-FP0164 (10 screw holes). The BOM of the printer in
ref/lulzbot/ lists the newer cover PP-FP0173 (14 screw holes). Both covers are 165 x 475 x 2 mm
and have the same outline, so the hole positions of each cover come from its STEP file.
Run: uv run --no-project --with build123d python workhorse.py  (writes build/workhorse/*.stl)
"""

import os
import zipfile
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
REF = os.path.join(HERE, "ref", "lulzbot")
FCSTD = os.path.join(REF, "final_assembly.fcstd")

# The chassis and the cover, in assembly coordinates (from the BREP of the control box).
CHASSIS = {"x": (409.62, 510.22), "y": (-625.235, -145.235), "z": (-9.0, 161.0)}
COVER = {"x": (510.22, 512.22), "y": (-622.735, -147.735), "z": (-6.5, 158.5)}
COVER_FACE_X = COVER["x"][1]            # the outer face of the cover
CHASSIS_TOP_Y = CHASSIS["y"][1]
CHASSIS_FRONT_Z = CHASSIS["z"][0]

# Screw holes of the two covers, in cover coordinates (x across 0..165, y along 0..475), from
# the STEP files. All holes are 4.0 mm. An M3 x 6 button head screw goes through each hole
# into an S-M3-2 PEM nut in the flange of the chassis (BOM: HD-BT0140, 14 pieces).
COVER_HOLES = {
    "PP-FP0164": [(3.5, y) for y in (27.5, 237.5, 447.5)] + [(161.5, y) for y in (27.5, 237.5, 447.5)]
    + [(x, y) for x in (27.5, 137.5) for y in (3.5, 471.5)],
    "PP-FP0173": [(3.5, y) for y in (27.5, 167.5, 307.5, 447.5)] + [(161.5, y) for y in (27.5, 167.5, 307.5, 447.5)]
    + [(x, y) for x in (27.5, 82.5, 137.5) for y in (3.5, 471.5)],
}


def cover_hole_world(x, y):
    """A cover hole axis point on the outer face of the cover: (X, Y, Z) in the assembly."""
    return (COVER_FACE_X, COVER["y"][0] + y, COVER["z"][0] + x)


def parts():
    """name -> BREP file name, for each sub-assembly in final_assembly.fcstd."""
    with zipfile.ZipFile(FCSTD) as z:
        doc = ET.fromstring(z.read("Document.xml"))
    out = {}
    for o in doc.find("ObjectData"):
        props = {p.get("name"): list(p)[0] for p in o.find("Properties") if len(list(p))}
        if "Shape" in props and "sourceFile" in props:
            out[o.get("name")] = props["Shape"].get("file")
    return out


def load(name):
    from build123d import import_brep
    f = parts()[name]
    tmp = os.path.join(HERE, "build", "workhorse")
    os.makedirs(tmp, exist_ok=True)
    path = os.path.join(tmp, f)
    with zipfile.ZipFile(FCSTD) as z, open(path, "wb") as fh:
        fh.write(z.read(f))
    return import_brep(path)


def control_box_parts():
    """The shells of the control box: chassis, fan, switch, power inlet, housings, RAMBo, LCD,
    and the cover. The order is the order in control_box.fcstd."""
    names = ["chassis", "fan_80mm", "power_switch", "power_inlet", "interconnect_housing", "rambo",
             "interconnect_cover", "lcd_cover", "lcd_bezel", "lcd", "lcd_knob", "cover", "dual_connector",
             "grounding_bracket"]
    return dict(zip(names, list(load("control_box_01"))))


def main():
    from build123d import export_stl
    out = os.path.join(HERE, "build", "workhorse")
    os.makedirs(out, exist_ok=True)
    for name in parts():
        s = load(name)
        export_stl(s, os.path.join(out, name + ".stl"), tolerance=0.3, angular_tolerance=0.4)
        b = s.bounding_box()
        print(f"  {name:40s} X {b.min.X:7.1f} {b.max.X:7.1f}  Y {b.min.Y:7.1f} {b.max.Y:7.1f}  Z {b.min.Z:7.1f} {b.max.Z:7.1f}")
    for name, s in control_box_parts().items():
        export_stl(s, os.path.join(out, "cb_" + name + ".stl"), tolerance=0.3, angular_tolerance=0.4)


if __name__ == "__main__":
    main()


def step_cover_holes(name):
    """The hole centres of a cover STEP file (cover coordinates), from its circle edges."""
    from build123d import import_step, GeomType
    s = import_step(os.path.join(REF, f"{name}_electronics_chassis_cover.step"))
    out = set()
    for e in s.edges().filter_by(GeomType.CIRCLE):
        if abs(e.radius - 2.0) < 0.05:
            c = e.arc_center
            out.add((round(c.X, 2), round(c.Y, 2)))
    return sorted(out)


def world_cover_holes():
    """The hole centres of the cover in final_assembly.fcstd, in assembly coordinates (Y, Z)."""
    from build123d import GeomType
    cover = control_box_parts()["cover"]
    out = set()
    for e in cover.edges().filter_by(GeomType.CIRCLE):
        if abs(e.radius - 2.0) < 0.05:
            c = e.arc_center
            out.add((round(c.Y, 2), round(c.Z, 2)))
    return sorted(out)


def cover_world(name):
    """A cover STEP in the assembly position of the cover (its hole pattern is symmetric, so the
    two ends are the same)."""
    from build123d import import_step, Location, Plane
    s = import_step(os.path.join(REF, f"{name}_electronics_chassis_cover.step"))
    loc = Location(Plane(origin=(COVER["x"][0], COVER["y"][1], COVER["z"][0]), x_dir=(0, 0, 1), z_dir=(1, 0, 0)))
    return loc * s
