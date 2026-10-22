# Makes the meshes of the 3D window (Sim3D.qml) from the CAD of the LulzBot TAZ Workhorse
# ("Redgum", https://gitlab.com/lulzbot3d/printers/redgum, GPLv3 or later and/or CC BY-SA 4.0).
#
#   CAD=folder-with-the-fcstd-files OUT=desktop/meshes freecadcmd -c "exec(open('export.py').read())"
#
# The folder has the files of production_parts/assemblies/sub_assemblies/: final_assembly,
# final_frame, x_axis, y_bed, aerostruder_v2_HS0.5 and control_box (.fcstd).
#
# Each part goes to a group by the axis that moves it (frame, bed, gantry, carriage) and to a kind
# by its name (metal, printed, motor, belt, surface). Each group and kind is one file
# GROUP_KIND.bin: triangles of 3 vertices of 6 float32 (the position and the normal), in the
# coordinates of the scene of Sim3D.qml (mm; Y up, X to the right, Z to the front), at the pose of
# the CAD: the tool head at X 137, the bed at Y 132 and the gantry at Z 26.6 of the printer.
import FreeCAD, MeshPart, os, struct, json

CAD = os.environ["CAD"]
OUT = os.environ["OUT"]
os.makedirs(OUT, exist_ok=True)

# The scene from the CAD: a half turn about the vertical axis (the CAD has the back at +Z and the
# printer X toward -X), and the bed top at Y 0, the nozzle in the plane Z 0, and the bed washer at
# the front left at printer X -10, Y -10.
OFFSET = FreeCAD.Vector(294.9, 540.6, 55.1)

def scene(v):
    return FreeCAD.Vector(-v.x, v.y, -v.z) + OFFSET

def normal(v):
    return FreeCAD.Vector(-v.x, v.y, -v.z)

fin = FreeCAD.openDocument(CAD + "/final_assembly.fcstd")
place = {o.Label: o.Placement for o in fin.Objects if o.TypeId == "Part::FeaturePython"}

# The parts of the bed that move with it; the other parts of y_bed are the fixed Y base.
BED = ("bed_mount_plate", "heatbed", "bed_corner", "bed_standoff", "bed_leveling_washer", "wiper_mount",
       "auxilary_bed_mount_cover", "y_cable_cover", "Y_bearing_holder", "_0mm_high_precision_bushing",
       "_0mm_bushing_retaining_ring", "bed_mount_table", "Y_belt_clamp", "y_belt_mount")
# The parts of the X axis that move with the carriage; the others move with the gantry only.
CARRIAGE = ("x_carriage", "x_double_bearing_holder", "x_belt_clamp", "x_belt_tensioner",
            "_2mm_high_precision_bushing", "_2mm_bushing_retaining_ring")
# Parts of the CAD that are not the printer.
SKIP = ("xy_calibration_cube",)

def kind(label):
    l = label.lower()
    if "heatbed" in l:
        return "surface"
    if "nema" in l:
        return "motor"
    if "belt" in l and not any(w in l for w in ("clamp", "mount", "anchor", "tensioner")):
        return "belt"
    if any(w in l for w in ("rod", "t_slot", "extrusion", "plate", "bracket", "bearing", "bushing", "washer",
                            "pully", "pulley", "yz_idler", "standoff", "heatsink", "heater", "nozzle", "block",
                            "chassis", "spring", "screw", "nut", "shaft", "gear")):
        return "metal"
    return "printed"

meshes = {}  # (group, kind) -> list of triangles

def add(group, label, shape, pl):
    if label.startswith(SKIP):
        return
    s = shape.copy()
    s.transformShape(pl.toMatrix())
    m = MeshPart.meshFromShape(Shape=s, LinearDeflection=0.4, AngularDeflection=0.4, Relative=False)
    tris = meshes.setdefault((group, kind(label)), [])
    for f in m.Facets:
        n = normal(f.Normal)
        tris.append([(scene(FreeCAD.Vector(*p)), n) for p in f.Points])

def parts(sub, label, group_of):
    doc = FreeCAD.openDocument(CAD + "/" + sub + ".fcstd")
    pl = place[label]
    for o in doc.Objects:
        if o.TypeId != "Part::FeaturePython" or "Constraint" in o.Label or o.Shape.isNull():
            continue
        add(group_of(o.Label), o.Label, o.Shape, pl)
    FreeCAD.closeDocument(doc.Name)

parts("final_frame", "final_frame_01", lambda l: "frame")
parts("control_box", "control_box_01", lambda l: "frame")
parts("y_bed", "y_bed_01", lambda l: "bed" if l.startswith(BED) else "frame")
parts("x_axis", "x_axis_01", lambda l: "carriage" if l.startswith(CARRIAGE) else "gantry")
parts("aerostruder_v2_HS0.5", "aerostruder_v2_HS0_5_01", lambda l: "carriage")
for o in fin.Objects:
    if o.Label in ("extruder_cap_v2_01",):
        add("carriage", o.Label, o.Shape, FreeCAD.Placement())
    if o.Label.startswith(("electronics_chassis_mount", "interconnect_strain_relief")):
        add("frame", o.Label, o.Shape, FreeCAD.Placement())

manifest = []
for (group, k), tris in sorted(meshes.items()):
    with open("%s/%s_%s.bin" % (OUT, group, k), "wb") as f:
        for tri in tris:
            for p, n in tri:
                f.write(struct.pack("<6f", p.x, p.y, p.z, n.x, n.y, n.z))
    manifest.append({"group": group, "kind": k, "triangles": len(tris)})
with open(OUT + "/meshes.json", "w") as f:
    json.dump(manifest, f, indent=1)
