# Print on a part: the parts of the tool that do not need Cura. A part that is already printed (the
# base) goes into a pocket on the bed, and Cura prints new models on it. The pocket is a low wall
# around the outline of the base. Before the first layer of the new models, the G-code lifts the
# nozzle over the base and pauses with PRINTDECK_PAUSE: the user puts the base in the pocket, and
# the deck checks the height of the base with a paper test at a point of its top face.
#
# Cura has Y up. Here a point of the scene is (x, y, z) with y up, and a point of the bed is (x, z).

import math
import re

# The defaults of the pocket, in mm.
CLEARANCE = 0.3  # between the base and the wall
WALL = 2.0  # the thickness of the wall
HEIGHT = 3.0  # the height of the wall
GAP = 8.0  # the open corners, for the fingers

# The lift over the base before the pause, in mm.
LIFT = 10.0


def pocket_boxes(x0, z0, x1, z1, clearance=CLEARANCE, wall=WALL, height=HEIGHT, gap=GAP):
    """Returns the walls of the pocket around the outline x0..x1, z0..z1 of the base, as boxes
    (xa, za, xb, zb, height). The corners are open, so the base comes out easily."""
    ax0, az0, ax1, az1 = x0 - clearance, z0 - clearance, x1 + clearance, z1 + clearance
    boxes = []
    # The walls along x, at the front and the back, and along z, at the sides.
    if ax1 - ax0 > 2 * gap:
        boxes.append((ax0 + gap, az0 - wall, ax1 - gap, az0, height))
        boxes.append((ax0 + gap, az1, ax1 - gap, az1 + wall, height))
    if az1 - az0 > 2 * gap:
        boxes.append((ax0 - wall, az0 + gap, ax0, az1 - gap, height))
        boxes.append((ax1, az0 + gap, ax1 + wall, az1 - gap, height))
    return boxes


def box_mesh(box):
    """The vertices and the triangles of a box (xa, za, xb, zb, height) on the bed, with 4 vertices
    for each face so that each face has its own normal."""
    xa, za, xb, zb, h = box
    c = [(xa, 0, za), (xb, 0, za), (xb, 0, zb), (xa, 0, zb), (xa, h, za), (xb, h, za), (xb, h, zb), (xa, h, zb)]
    faces = [(0, 3, 2, 1), (4, 5, 6, 7), (0, 1, 5, 4), (1, 2, 6, 5), (2, 3, 7, 6), (3, 0, 4, 7)]
    verts, tris = [], []
    for f in faces:
        i = len(verts)
        verts += [c[k] for k in f]
        tris += [(i, i + 1, i + 2), (i, i + 2, i + 3)]
    return verts, tris


def top_point(triangles, avoid, margin=3.0):
    """Returns the point (x, z) of the top face of the base for the height check, and its height y.

    triangles are the triangles of the base in the scene, as three (x, y, z) points. The top face is
    the triangles that face up at the highest y. The point is the center of the largest of them that
    is at least margin from the outlines in avoid, (x0, z0, x1, z1) of the new models; the new
    models cover the face there. With no such triangle, the point is the center of the largest one."""
    top = max((max(p[1] for p in t) for t in triangles), default=None)
    if top is None:
        return None
    faces = []
    for a, b, c in triangles:
        if min(a[1], b[1], c[1]) < top - 0.05:
            continue
        ux, uy, uz = b[0] - a[0], b[1] - a[1], b[2] - a[2]
        vx, vy, vz = c[0] - a[0], c[1] - a[1], c[2] - a[2]
        nx, ny, nz = uy * vz - uz * vy, uz * vx - ux * vz, ux * vy - uy * vx
        area = math.sqrt(nx * nx + ny * ny + nz * nz) / 2
        if area <= 0 or abs(ny) / (2 * area) < 0.98:
            continue
        faces.append((area, ((a[0] + b[0] + c[0]) / 3, (a[2] + b[2] + c[2]) / 3)))
    if not faces:
        return None
    faces.sort(key=lambda f: -f[0])

    def clear(p):
        return all(not (r[0] - margin <= p[0] <= r[2] + margin and r[1] - margin <= p[1] <= r[3] + margin) for r in avoid)

    for _, p in faces:
        if clear(p):
            return p[0], p[1], top
    return faces[0][1][0], faces[0][1][1], top


def to_gcode(x, y, z, width, depth, center_is_zero=False):
    """A point of the scene as the X, Y and Z of the G-code. Cura has Y up, and the front of the bed
    at +z."""
    if center_is_zero:
        return x, -z, y
    return x + width / 2, depth / 2 - z, y


_move = re.compile(r"^G[01] .*$", re.M)
_zword = re.compile(r"\bZ(-?[0-9.]+)")
_eword = re.compile(r"\bE-?[0-9.]")


def layer_start_z(chunk, z):
    """The Z of the nozzle when the layer prints its first line. Cura moves to the Z of a layer at
    the end of the layer before it, so this is z, the last Z before the layer, unless the layer moves
    Z before its first extrusion."""
    for m in _move.finditer(chunk):
        line = m.group(0)
        zm = _zword.search(line)
        if zm:
            return float(zm.group(1))
        if _eword.search(line):
            break
    return z


def last_z(chunk, z):
    """The last Z of a chunk of G-code, or z when it has none."""
    for m in _move.finditer(chunk):
        zm = _zword.search(m.group(0))
        if zm:
            z = float(zm.group(1))
    return z


_xy = re.compile(r"\b([XY])(-?[0-9.]+)")


def take_travel(chunk):
    """Removes the moves at the end of a layer, after its last extrusion: Cura travels there to the
    first point of the next layer, at the Z of the next layer. It returns the chunk and the X and Y
    of that point, or None."""
    lines = chunk.split("\n")
    xy = {}
    keep = []
    i = len(lines) - 1
    while i >= 0:
        line = lines[i]
        if _move.match(line) and _eword.search(line):
            break
        if _move.match(line):
            for axis, v in _xy.findall(line):
                xy.setdefault(axis, float(v))  # the last value of each axis
        else:
            keep.append(line)
        i -= 1
    if "X" not in xy or "Y" not in xy:
        return chunk, None
    return "\n".join(lines[:i + 1] + keep[::-1]), (xy["X"], xy["Y"])


def insert_pause(gcode_list, z_start, message, check, lift):
    """Inserts the pause at the start of the first layer at z_start or higher, after its ;LAYER:
    line. The travel to the first point of the layer moves from the end of the layer before it to
    after the pause, so that it is over the base:

    - a lift of lift mm over the check height (or the layer),
    - PRINTDECK_PAUSE: the resume goes back to the lifted position,
    - the travel to the first point of the layer, still lifted,
    - the move down to the Z of the layer.

    It returns the index of the layer, or -1."""
    z = 0.0
    for i, chunk in enumerate(gcode_list):
        if chunk.startswith(";LAYER:"):
            zl = layer_start_z(chunk, z)
            if zl >= z_start - 0.01:
                xy = None
                if i > 0:
                    gcode_list[i - 1], xy = take_travel(gcode_list[i - 1])
                top = check[2] if check else zl
                head, _, rest = chunk.partition("\n")
                gcode_list[i] = head + "\n" + pause_lines(message, check, top + lift, zl, xy) + rest
                return i
        z = last_z(chunk, z)
    return -1


def pause_lines(message, check, lift_z, layer_z, xy=None):
    """The G-code of the pause: a lift over the base, PRINTDECK_PAUSE with the message and the check
    point (x, y, z) of the G-code (or None for no check), the travel to xy (or None), and the move
    down to the Z of the layer."""
    message = message.replace('"', "'")
    out = ";PRINTDECK: the base goes in its pocket\nG1 Z%.3f F600\n" % lift_z
    out += 'PRINTDECK_PAUSE MESSAGE="%s"' % message
    if check:
        out += " CHECK_X=%.3f CHECK_Y=%.3f CHECK_Z=%.3f" % check
    out += "\n"
    if xy:
        out += "G0 F6000 X%.3f Y%.3f\n" % xy
    return out + "G1 Z%.3f F600\n" % layer_z


_start = re.compile(r"^PRINT_START\b.*$", re.M)


def on_part_start(gcode_list):
    """Replaces the PRINT_START line of the start G-code with a start for a print on a part that is
    in its pocket already: Z homes up first, away from the part, there is no probe of the bed and no
    prime line on it, and the nozzle purges at the park position. The height check of the pause
    sets the Z. It returns True when it found PRINT_START."""
    for i, chunk in enumerate(gcode_list):
        m = _start.search(chunk)
        if not m:
            continue
        params = dict(re.findall(r"(\w+)=([0-9.]+)", m.group(0)))
        bed, ext = params.get("BED", "60"), params.get("EXTRUDER", "210")
        start = "\n".join([
            "; Printdeck: a print on a part in its pocket. Z homes up first, and the height check sets the Z.",
            "M140 S%s" % bed,
            "M104 S%s" % ext,
            "G28 Z",
            "G28 X Y",
            "G90",
            "M190 S%s" % bed,
            "M109 S%s" % ext,
            "PRINTDECK_PURGE",
            "G92 E0",
        ])
        gcode_list[i] = chunk[:m.start()] + start + chunk[m.end():]
        return True
    return False
