# Writes the meshes of Klipper's ZMesh for the cases of the bed_mesh tests: the interpolated
# matrix, calc_z at some points, the average and the range.
#
#   python3 dumpmesh.py OUT.json
import sys, json, collections
sys.path.insert(0, '/klipper/klippy')
from extras import bed_mesh
pts4 = [[0.1,0.05,-0.02,0.01],[0.08,0.0,-0.05,0.03],[0.12,0.06,0.01,0.09],[0.15,0.11,0.04,0.12]]
pts3 = [r[:3] for r in pts4[:3]]
cases = [("bicubic", pts4, 4, 4, 2, 3), ("lagrange", pts3, 3, 3, 2, 2), ("direct", pts3, 3, 3, 0, 0),
         ("bicubic", [[0.1,0.2,0.3,0.25,0.1],[0.0,-0.1,0.05,0.2,0.3],[0.12,0.06,0.01,0.09,-0.2],[0.3,0.2,0.1,0.0,-0.1],[0.05,0.15,0.25,0.35,0.45]], 5, 5, 3, 1)]
out = []
for algo, pts, xc, yc, xp, yp in cases:
    p = collections.OrderedDict([('min_x', 50.0), ('max_x', 250.0), ('min_y', 20.0), ('max_y', 250.0),
        ('x_count', xc), ('y_count', yc), ('mesh_x_pps', xp), ('mesh_y_pps', yp), ('algo', algo), ('tension', 0.2)])
    m = bed_mesh.ZMesh(p, "t")
    m.build_mesh(pts)
    zs = [m.calc_z(x, y) for x, y in [(50, 20), (77.7, 33.3), (150, 135), (249.9, 249.9), (10, 300), (123.456, 201.2)]]
    out.append({"algo": algo, "pts": pts, "xc": xc, "yc": yc, "xp": xp, "yp": yp,
                "matrix": m.get_mesh_matrix(), "calc": zs, "avg": m.get_z_average(), "range": list(m.get_z_range())})
with open(sys.argv[1], "w") as f:
    json.dump(out, f)
