# Adaptive Speed: a post-processing script for Cura.
#
# It changes the speed of each extrusion move, from the geometry of the path it is part of:
#   - straight runs (from corner to corner, over all the short segments of a slicer) go faster, the longer
#     the faster, up to a boost for each kind of move (outer wall, infill, ...); short paths go slower;
#   - curves go only as fast as v = sqrt(a * R): tight curves slow, gentle curves fast;
#   - small features (wall loops shorter than a few cm: pins, small holes) go slower, the smaller the slower;
#   - overhangs (walls that stick out past the layer below) and bridges (nothing below) go slower;
#   - the speed stays below the flow that the hot end can melt, for the width and height of each line,
#     so thick layers have a lower limit than thin ones.
# Each speed stays between a minimum and a maximum.
#
# Quality comes first. A change of speed changes the pressure in the nozzle, and the line gets wider or
# narrower for a moment; on a visible surface that shows as bands. So the outer wall is not boosted, and it
# keeps one steady speed along each loop (it only slows where it must: tight curves, overhangs, bridges).
# The speed gains come from the inside of the part, and never above the flow that Cura already uses.
#
# No layer gets faster than Cura's Minimum Layer Time, so each layer still cools enough.
#
# Only the speeds (F) change. X, Y, Z, and E stay as Cura made them.
#
# Install: copy this file into the "scripts" folder of Cura's configuration folder
# (Help > Show Configuration Folder > scripts), restart Cura, then
# Extensions > Post Processing > Modify G-Code > Add a script > Adaptive Speed.
#
# Test without Cura:  python3 AdaptiveSpeed.py in.gcode out.gcode [key=value ...]

import json
import math
import re
import sys

try:
    from ..Script import Script
except (ImportError, ValueError):  # not inside Cura: for tests and the command line

    class Script:
        def __init__(self):
            self._values = {}

        def getSettingValueByKey(self, key):
            return self._values[key]


# key: (label, description, type, default, unit, extra)
SETTINGS = {
    "adaptive": ("Adaptive speed", "Change the speed of each move from the geometry of its path and the layer below.", "bool", True, "", {}),
    "max_speed": ("Maximum speed", "No print move goes faster.", "float", 150.0, "mm/s", {"minimum_value": "1"}),
    "min_speed": ("Minimum speed", "No print move goes slower (except the first layers, which stay as sliced).", "float", 8.0, "mm/s", {"minimum_value": "1"}),
    "max_flow": ("Maximum flow", "The most plastic the hot end can melt. 0: the most that Cura already uses in this file, so no move melts more than the profile does now.", "float", 0.0, "mm³/s", {"minimum_value": "0"}),
    "filament_diameter": ("Filament diameter", "0: from the material in Cura.", "float", 0.0, "mm", {"minimum_value": "0"}),
    "boost_outer": ("Boost outer wall", "Speed factor for long outer wall moves. The outer wall is the surface: 1 keeps the speed of Cura.", "float", 1.0, "×", {"minimum_value": "1", "enabled": "adaptive"}),
    "boost_inner": ("Boost inner wall", "Speed factor for long inner wall moves.", "float", 1.6, "×", {"minimum_value": "1", "enabled": "adaptive"}),
    "boost_infill": ("Boost infill", "Speed factor for long infill moves.", "float", 2.5, "×", {"minimum_value": "1", "enabled": "adaptive"}),
    "boost_skin": ("Boost top/bottom", "Speed factor for long top and bottom moves. The top is visible: keep this small.", "float", 1.3, "×", {"minimum_value": "1", "enabled": "adaptive"}),
    "boost_other": ("Boost other", "Speed factor for long moves of other kinds (support, skirt, ...).", "float", 1.5, "×", {"minimum_value": "1", "enabled": "adaptive"}),
    "long_run": ("Long straight run", "Straight runs (corner to corner) at least this long get the full boost.", "float", 30.0, "mm", {"minimum_value": "1", "enabled": "adaptive"}),
    "corner_angle": ("Corner angle", "A turn sharper than this ends a straight run. Smaller turns are part of a curve.", "float", 30.0, "°", {"minimum_value": "1", "maximum_value": "179", "enabled": "adaptive"}),
    "curve_accel": ("Curve acceleration", "Limits the speed in curves to the square root of this times the curve radius. Lower is slower in curves.", "float", 1000.0, "mm/s²", {"minimum_value": "10", "enabled": "adaptive"}),
    "short_path": ("Short path", "Paths shorter than this (small islands, gap fill) go slower.", "float", 3.0, "mm", {"minimum_value": "0", "enabled": "adaptive"}),
    "short_factor": ("Speed of the shortest paths", "Speed factor for a path of almost no length.", "float", 0.5, "×", {"minimum_value": "0.1", "maximum_value": "1", "enabled": "adaptive"}),
    "overhang_factor": ("Speed of overhangs", "Speed factor for a wall that sticks out a full line width past the layer below. A 45° overhang sticks out about one layer height.", "float", 0.5, "×", {"minimum_value": "0.1", "maximum_value": "1", "enabled": "adaptive"}),
    "bridge_factor": ("Speed of bridges", "Speed factor for moves with nothing below for most of their length.", "float", 0.5, "×", {"minimum_value": "0.1", "maximum_value": "1", "enabled": "adaptive"}),
    "steady_outer": ("Steady outer wall", "Each loop of the outer wall keeps one speed, so the surface has no bands from speed changes.", "bool", True, "", {"enabled": "adaptive"}),
    "small_feature": ("Small feature", "Wall loops shorter than this (pins, small holes) go slower, the smaller the slower.", "float", 20.0, "mm", {"minimum_value": "0", "enabled": "adaptive"}),
    "small_feature_factor": ("Speed of the smallest features", "Speed factor for a wall loop of almost no length.", "float", 0.5, "×", {"minimum_value": "0.1", "maximum_value": "1", "enabled": "adaptive"}),
    "min_layer_time": ("Minimum layer time", "No layer is faster than this, so each layer cools enough. -1: Cura's Minimum Layer Time.", "float", -1.0, "s", {"minimum_value": "-1"}),
    "first_layers": ("Layers left as sliced", "The first layers keep the speeds of Cura, for the adhesion to the bed.", "int", 1, "", {"minimum_value": "0"}),
    "accel": ("Acceleration", "Used to estimate times where the G-code sets none (M204).", "float", 1500.0, "mm/s²", {"minimum_value": "1"}),
    "corner_velocity": ("Square corner velocity", "Klipper's square_corner_velocity, to estimate the speed in corners.", "float", 5.0, "mm/s", {"minimum_value": "0"}),
}

OUTER, INNER, INFILL, SKIN, OTHER = "outer", "inner", "infill", "skin", "other"


def kind_of(t):
    """The kind of a move from a ;TYPE: comment of Cura (or PrusaSlicer, OrcaSlicer)."""
    t = t.strip().lower()
    if t in ("wall-outer", "outer wall", "external perimeter"):
        return OUTER
    if t in ("wall-inner", "inner wall", "perimeter"):
        return INNER
    if t in ("fill", "sparse infill", "internal infill"):
        return INFILL
    if t == "skin" or "solid infill" in t or "surface" in t:
        return SKIN
    return OTHER


class Move:
    __slots__ = ("chunk", "line", "x0", "y0", "z0", "x1", "y1", "z1", "de", "feed", "kind", "accel", "extrude", "speed", "length", "has_f",
                 "over", "bridge", "reason")

    def __init__(self, chunk, line, x0, y0, z0, x1, y1, z1, de, feed, kind, accel, has_f):
        self.chunk, self.line = chunk, line
        self.x0, self.y0, self.z0, self.x1, self.y1, self.z1 = x0, y0, z0, x1, y1, z1
        self.de, self.feed, self.kind, self.accel, self.has_f = de, feed, kind, accel, has_f
        self.length = math.hypot(x1 - x0, y1 - y0)
        self.extrude = de > 0 and self.length > 1e-6
        self.speed = feed  # mm/s; the new speed of an extrusion move
        self.over = 0.0  # 0..1: how much the move sticks out past the layer below
        self.bridge = False  # nothing below for most of the move
        self.reason = ""  # what decided the new speed, for the report in the header


WORD = re.compile(r"([A-Za-z])\s*([-+]?[0-9]*\.?[0-9]+)")


def words(code):
    return {m.group(1).upper(): float(m.group(2)) for m in WORD.finditer(code)}


def parse(chunks, default_accel):
    """Reads the moves of all chunks. Returns the moves, and for each chunk whether it is a layer and its moves."""
    x = y = z = e = 0.0
    feed = 50.0  # mm/s
    rel_e = rel_xyz = False
    kind = OTHER
    accel = default_accel
    moves = []
    layers = []  # (chunk index, layer number, [moves])
    for ci, chunk in enumerate(chunks):
        lines = chunk.split("\n")
        current = None
        if lines and lines[0].startswith(";LAYER:"):
            try:
                current = (ci, int(lines[0][7:].strip()), [])
            except ValueError:
                current = None
        for li, raw in enumerate(lines):
            code, _, comment = raw.partition(";")
            if not code.strip():
                if raw.startswith(";TYPE:"):
                    kind = kind_of(raw[6:])
                continue
            parts = code.split()
            cmd = parts[0].upper()
            if cmd in ("G0", "G1", "G00", "G01"):
                w = words(" ".join(parts[1:]))
                has_f = "F" in w
                if has_f:
                    feed = w["F"] / 60.0
                nx = x + w["X"] if rel_xyz and "X" in w else w.get("X", x)
                ny = y + w["Y"] if rel_xyz and "Y" in w else w.get("Y", y)
                nz = z + w["Z"] if rel_xyz and "Z" in w else w.get("Z", z)
                ne = e
                if "E" in w:
                    ne = e + w["E"] if rel_e else w["E"]
                m = Move(ci, li, x, y, z, nx, ny, nz, ne - e, feed, kind, accel, has_f)
                moves.append(m)
                if current is not None:
                    current[2].append(m)
                x, y, z, e = nx, ny, nz, ne
            elif cmd == "G92":
                w = words(" ".join(parts[1:]))
                if "E" in w:
                    e = w["E"]
            elif cmd == "M82":
                rel_e = False
            elif cmd == "M83":
                rel_e = True
            elif cmd == "G90":
                rel_xyz = rel_e = False
            elif cmd == "G91":
                rel_xyz = rel_e = True
            elif cmd == "M204":
                w = words(" ".join(parts[1:]))
                accel = w.get("S", w.get("P", accel))
        if current is not None:
            layers.append(current)
    return moves, layers


def move_time(moves, speeds, corner_velocity):
    """The time of moves, with acceleration and the corner speed of Klipper. speeds[i] is the speed of moves[i]."""
    n = len(moves)
    if n == 0:
        return 0.0
    junction = [0.0] * (n + 1)  # the speed between move i-1 and move i
    for i in range(1, n):
        a, b = moves[i - 1], moves[i]
        if a.length < 1e-9 or b.length < 1e-9 or abs(a.z1 - a.z0) > 1e-9 or abs(b.z1 - b.z0) > 1e-9:
            continue
        ux, uy = (a.x1 - a.x0) / a.length, (a.y1 - a.y0) / a.length
        vx, vy = (b.x1 - b.x0) / b.length, (b.y1 - b.y0) / b.length
        cos_theta = -(ux * vx + uy * vy)
        if cos_theta > 0.999999:
            continue  # turning back: stop
        acc = min(a.accel, b.accel)
        limit = min(speeds[i - 1], speeds[i])
        if cos_theta < -0.999999:
            junction[i] = limit  # straight on
            continue
        jd = corner_velocity ** 2 * (math.sqrt(2.0) - 1.0) / acc
        s = math.sqrt(0.5 * (1.0 - cos_theta))
        r = jd * s / (1.0 - s)
        junction[i] = min(limit, math.sqrt(acc * r))
    # Forward: how fast each move can end. Backward: how fast it can start and still stop in time.
    v_end = [0.0] * n
    v_in = 0.0
    for i in range(n):
        m = moves[i]
        d = m.length if m.length > 1e-9 else 0.0
        v_end[i] = min(junction[i + 1], speeds[i], math.sqrt(v_in * v_in + 2 * m.accel * d) if d else 0.0)
        v_in = v_end[i]
    v_start = [0.0] * n
    v_out = 0.0
    for i in range(n - 1, -1, -1):
        m = moves[i]
        d = m.length
        start = min(junction[i], speeds[i], math.sqrt(v_out * v_out + 2 * m.accel * d) if d > 1e-9 else 0.0)
        v_start[i] = start
        v_out = start
    t = 0.0
    for i in range(n):
        m = moves[i]
        v = speeds[i]
        if m.length <= 1e-9:  # Z moves, retractions
            dz = abs(m.z1 - m.z0)
            de = abs(m.de)
            t += max(dz, de) / v if v > 0 else 0.0
            continue
        vi = min(v_start[i], v_end[i - 1] if i else 0.0) if i else v_start[i]
        vo = v_end[i]
        a = m.accel
        d = m.length
        # Trapezoid: speed up from vi, cruise at v, slow down to vo; a triangle if the move is too short.
        d_up = max(0.0, (v * v - vi * vi) / (2 * a))
        d_down = max(0.0, (v * v - vo * vo) / (2 * a))
        if d_up + d_down <= d:
            t += (v - vi) / a + (v - vo) / a + (d - d_up - d_down) / v
        else:
            peak = math.sqrt(max(vi * vi, vo * vo, (2 * a * d + vi * vi + vo * vo) / 2))
            t += max(0.0, (peak - vi) / a) + max(0.0, (peak - vo) / a)
    return t


def path_geometry(ms, corner_angle):
    """For each extrusion move: the length of its straight run (corner to corner), the length of its whole path
    (until the next travel or retraction), and the radius of the curve it is in (infinite if straight)."""
    runs, paths, radius = {}, {}, {}
    chains = []
    corner = math.radians(corner_angle)
    chain = []

    def turn(a, b):
        ux, uy = (a.x1 - a.x0) / a.length, (a.y1 - a.y0) / a.length
        vx, vy = (b.x1 - b.x0) / b.length, (b.y1 - b.y0) / b.length
        return math.acos(max(-1.0, min(1.0, ux * vx + uy * vy)))

    def finish(chain):
        if not chain:
            return
        chains.append(chain)
        total = sum(m.length for m in chain)
        turns = [turn(a, b) for a, b in zip(chain, chain[1:])]
        # Joint radius: a curve drawn with chords of length c that turn by t has the radius c / (2 sin(t / 2)).
        r_joint = []
        for (a, b), t in zip(zip(chain, chain[1:]), turns):
            if t < 1e-3 or t > corner:
                r_joint.append(float("inf"))  # straight on, or a corner (not a curve)
            else:
                r_joint.append(((a.length + b.length) / 2) / (2 * math.sin(t / 2)))
        run, start = 0.0, 0
        for i, m in enumerate(chain):
            run += m.length
            last = i == len(chain) - 1 or turns[i] > corner
            if last:
                for k in range(start, i + 1):
                    runs[chain[k]] = run
                run, start = 0.0, i + 1
        for i, m in enumerate(chain):
            paths[m] = total
            rs = []
            if i > 0:
                rs.append(r_joint[i - 1])
            if i < len(chain) - 1:
                rs.append(r_joint[i])
            radius[m] = min(rs) if rs else float("inf")

    for m in ms:
        if m.extrude and abs(m.z1 - m.z0) < 1e-9:
            chain.append(m)
        else:
            finish(chain)
            chain = []
    finish(chain)
    return runs, paths, radius, chains


def percentile(pairs, q):
    """The value below which the fraction q of the weight lies. pairs: (value, weight)."""
    pairs = sorted(pairs)
    total = sum(w for _, w in pairs)
    acc = 0.0
    for v, w in pairs:
        acc += w
        if acc >= q * total:
            return v
    return pairs[-1][0]


def small_features(chains):
    """The wall loops that are closed (they end where they start): the length of the loop of each of their moves.
    A short loop is a small feature: a pin, a small hole, a thin detail."""
    out = {}
    for chain in chains:
        if chain[0].kind not in (OUTER, INNER):
            continue
        a, b = chain[0], chain[-1]
        if math.hypot(b.x1 - a.x0, b.y1 - a.y0) <= 1.0:
            total = sum(m.length for m in chain)
            for m in chain:
                out[m] = total
    return out


def overhangs(ext, below, area, cell=1.0):
    """Sets m.over and m.bridge: which parts of each move have a line of the layer below under them.
    Infill is left out: it always crosses the gaps of the sparse infill below."""
    # The lines below in a grid of cells, each as (x0, y0, dx, dy, length²), for the test "a line within r".
    grid = {}
    for b in below:
        if not b.extrude:
            continue
        dx, dy = b.x1 - b.x0, b.y1 - b.y0
        seg = (b.x0, b.y0, dx, dy, dx * dx + dy * dy)
        # The cells that the line passes through: points every half cell along it.
        steps = int(b.length / (0.5 * cell)) + 1
        cells = {(int((b.x0 + dx * k / steps) // cell), int((b.y0 + dy * k / steps) // cell)) for k in range(steps + 1)}
        for c in cells:
            grid.setdefault(c, []).append(seg)
    if not grid:
        return
    h = max(1e-3, ext[0].z1 - max(b.z1 for b in below if b.extrude))

    def near(px, py, r):
        """True if a line below passes within r of the point. Stops at the first one it finds."""
        r2 = r * r
        for gx in range(int((px - r) // cell), int((px + r) // cell) + 1):
            for gy in range(int((py - r) // cell), int((py + r) // cell) + 1):
                for x0, y0, dx, dy, l2 in grid.get((gx, gy), ()):
                    t = 0.0 if l2 == 0 else ((px - x0) * dx + (py - y0) * dy) / l2
                    t = 0.0 if t < 0 else 1.0 if t > 1 else t
                    ex, ey = px - x0 - t * dx, py - y0 - t * dy
                    if ex * ex + ey * ey <= r2:
                        return True
        return False

    for m in ext:
        if m.kind == INFILL:
            continue
        w = m.de * area / (m.length * h)  # the width of the line, from its plastic and the layer height
        if not 0.05 < w < 5:
            continue
        # Across the width of the line, five points: each is supported if a line below covers it (a line
        # below covers half a width each side of its middle). The part that is not supported is the overhang.
        nx, ny = -(m.y1 - m.y0) / m.length, (m.x1 - m.x0) / m.length
        cover = 0.5 * w + 0.05
        n = min(16, max(1, int(m.length)))  # every mm along the move, at most 16 points
        out_parts, far = [], 0
        for k in range(n):
            t = (k + 0.5) / n
            px, py = m.x0 + t * (m.x1 - m.x0), m.y0 + t * (m.y1 - m.y0)
            # The middle and the two edges first; the points between them only if one of those is free.
            free = sum(1 for o in (-0.5 * w, 0.0, 0.5 * w) if not near(px + o * nx, py + o * ny, cover))
            if free:
                free += sum(1 for o in (-0.25 * w, 0.25 * w) if not near(px + o * nx, py + o * ny, cover))
            out_parts.append(free / 5)
            if free and not near(px, py, 1.5 * w):
                far += 1
        # Up to a fifth out is normal; more than two thirds out is a full overhang.
        m.over = sum(max(0.0, min(1.0, (f - 0.2) / 0.47)) for f in out_parts) / n
        m.bridge = m.length >= 3 and far >= n / 2


class AdaptiveSpeed(Script):
    def getSettingDataString(self):
        settings = {}
        for key, (label, desc, typ, default, unit, extra) in SETTINGS.items():
            s = {"label": label, "description": desc, "type": typ, "default_value": default}
            if unit:
                s["unit"] = unit
            s.update(extra)
            settings[key] = s
        return json.dumps({"name": "Adaptive Speed", "key": "AdaptiveSpeed", "metadata": {}, "version": 2, "settings": settings})

    def _get(self, key):
        v = self.getSettingValueByKey(key)
        typ = SETTINGS[key][2]
        if typ == "float":
            return float(v)
        if typ == "int":
            return int(v)
        if typ == "bool":
            return v if isinstance(v, bool) else str(v).lower() in ("true", "1")
        return v

    def execute(self, data):
        p = {k: self._get(k) for k in SETTINGS}
        if p["filament_diameter"] <= 0:
            p["filament_diameter"] = cura_setting(data, "material_diameter", 2.85)
        if p["min_layer_time"] < 0:
            p["min_layer_time"] = cura_setting(data, "cool_min_layer_time", 10.0)
        moves, layers = parse(data, p["accel"])
        area = math.pi * (p["filament_diameter"] / 2) ** 2
        boost = {OUTER: p["boost_outer"], INNER: p["boost_inner"], INFILL: p["boost_infill"], SKIN: p["boost_skin"], OTHER: p["boost_other"]}

        tuned = [(ci, n, ms) for ci, n, ms in layers if n >= p["first_layers"]]

        # The flow limit: set, or the most that Cura uses in this file (the 98th percentile of the length).
        max_flow = p["max_flow"]
        if max_flow <= 0:
            flows = [(m.de * area / m.length * m.feed, m.length) for _, _, ms in tuned for m in ms if m.extrude]
            max_flow = percentile(flows, 0.98) if flows else 1e9
        self.flow_used = max_flow

        # Speed limits of each extrusion move: the maximum, and the flow the hot end can melt.
        cap = {}
        for _, _, ms in tuned:
            for m in ms:
                if m.extrude:
                    flow_per_mm = m.de * area / m.length  # mm³ of plastic per mm of move
                    cap[m] = min(p["max_speed"], max_flow / flow_per_mm if flow_per_mm > 0 else p["max_speed"])

        before = sum(move_time(ms, [m.feed for m in ms], p["corner_velocity"]) for _, _, ms in layers)

        # 1. Adaptive speed from the geometry. Overhangs need the layer below.
        below = {}
        for (ci, _, ms), prev in zip(layers, [None] + layers[:-1]):
            below[ci] = prev[2] if prev else None
        for ci, _, ms in tuned:
            if p["adaptive"]:
                self._adapt(ms, below[ci], p, boost, cap, area)
            else:
                for m in ms:
                    if m.extrude:
                        m.speed = max(min(m.feed, cap[m]), min(p["min_speed"], m.feed))

        # 2. Cooling: no layer faster than the minimum layer time.
        slowed = self._cooling(tuned, p, cap)

        # 3. Write the speeds.
        out = self._write(data, moves, layers, p)
        after = sum(move_time(ms, [m.speed if m.extrude else m.feed for m in ms], p["corner_velocity"]) for _, _, ms in layers)
        report = ["%d layers slowed for the minimum layer time" % slowed] if slowed else []
        report += breakdown([m for _, _, ms in tuned for m in ms if m.extrude])
        return self._header(out, before, after, report, p)

    def _adapt(self, ms, below, p, boost, cap, area):
        """The speed of each extrusion move from the path it is part of, the detail around it, and the layer below."""
        ext = [m for m in ms if m.extrude]
        if not ext:
            return
        runs, paths, radius, chains = path_geometry(ms, p["corner_angle"])
        small = small_features(chains)
        if below:
            overhangs(ext, below, area)

        long_ = p["long_run"]
        v_of = {}
        must = {}  # the moves that must be slower than the rest of their loop: curves, overhangs, bridges
        for m in ext:
            # Straight runs: the boost of the kind, more for longer runs (never over air). Short paths: slower.
            air = m.bridge or m.over > 0.05
            f = 1.0 if air else 1 + (boost[m.kind] - 1) * min(1.0, runs[m] / long_)
            slow = {}  # each factor that slows the move, as the speed it alone would allow
            if paths[m] < p["short_path"]:
                g = p["short_factor"] + (1 - p["short_factor"]) * paths[m] / p["short_path"]
                f *= g
                slow["short paths"] = m.feed * g
            if m in small and p["small_feature"] > 0:
                g = p["small_feature_factor"] + (1 - p["small_feature_factor"]) * min(1.0, small[m] / p["small_feature"])
                f *= g
                slow["small features"] = m.feed * g
            if m.bridge:
                f *= p["bridge_factor"]
                slow["bridges"] = m.feed * p["bridge_factor"]
            elif m.over > 0:
                g = 1 - (1 - p["overhang_factor"]) * m.over
                f *= g
                slow["overhangs"] = m.feed * g
            v = m.feed * f
            if radius[m] < float("inf"):  # in a curve: v = sqrt(a * R)
                v = min(v, math.sqrt(p["curve_accel"] * radius[m]))
                slow["curves"] = math.sqrt(p["curve_accel"] * radius[m])
            if m.bridge or m.over > 0:
                v = min(v, m.feed)  # never faster than sliced over air
            if cap[m] < v:
                slow["flow limit"] = cap[m]
            v = min(v, cap[m])
            v_of[m] = v
            must[m] = m.bridge or m.over > 0.05 or v < m.feed * f - 1e-6
            if v > m.feed + 1e-6:
                m.reason = "boost"
            elif v < m.feed - 1e-6:
                m.reason = min(slow, key=slow.get) if slow else "other"

        if p["steady_outer"]:
            # One speed for each outer loop: the speed that the slowest fifth of its length allows, leaving out
            # the moves that must be slower anyway.
            for chain in chains:
                outer = [m for m in chain if m.kind == OUTER and not must[m]]
                if not outer:
                    continue
                steady = percentile([(v_of[m], m.length) for m in outer], 0.2)
                for m in chain:
                    if m.kind == OUTER:
                        # Ordinary moves all take the speed of the loop; the ones that must be slower stay slower.
                        new = min(v_of[m], steady) if must[m] else min(steady, cap[m])
                        if not must[m] and new < m.feed - 1e-6 and new < v_of[m] - 1e-6:
                            m.reason = "steady outer wall"
                        elif new > m.feed + 1e-6:
                            m.reason = "boost"
                        v_of[m] = new

        for m in ext:
            m.speed = max(v_of[m], min(p["min_speed"], cap[m]))

    def _cooling(self, tuned, p, cap):
        """Slows down each layer that got faster than the minimum layer time, back to it. Returns how many."""
        cv = p["corner_velocity"]
        slowed = 0
        for _, _, ms in tuned:
            ext = [m for m in ms if m.extrude]
            if not ext:
                continue
            base = {m: m.speed for m in ext}

            def time_at(s):
                for m in ext:
                    m.speed = max(base[m] * s, min(p["min_speed"], base[m]))
                return move_time(ms, [m.speed if m.extrude else m.feed for m in ms], cv)

            if time_at(1.0) >= p["min_layer_time"]:
                continue
            slowed += 1
            for m in ext:
                m.reason = "minimum layer time"
            lo, hi = 0.0, 1.0  # bisection on the speed factor of the layer
            for _ in range(30):
                mid = (lo + hi) / 2
                if time_at(mid) < p["min_layer_time"]:
                    hi = mid
                else:
                    lo = mid
            time_at(lo)
        return slowed

    def _write(self, data, moves, layers, p):
        """Writes the new speeds into the lines. A line that does not say F but needs another one than the
        line before gets an F, so the speeds never carry over to the wrong move."""
        chunks = [c.split("\n") for c in data]
        by_line = {(m.chunk, m.line): m for m in moves}
        tuned_chunks = {ci for ci, n, ms in layers if n >= p["first_layers"]}
        out_feed = None
        result = []
        for ci, lines in enumerate(chunks):
            new = []
            for li, raw in enumerate(lines):
                m = by_line.get((ci, li))
                if m is None:
                    new.append(raw)
                    continue
                want = m.speed if (m.extrude and ci in tuned_chunks) else m.feed
                code, sep, comment = raw.partition(";")
                if m.has_f:
                    code = re.sub(r"F\s*[-+]?[0-9]*\.?[0-9]+", "F%s" % fmt(want * 60), code, count=1)
                elif out_feed is None or abs(out_feed - want) > 1e-6:
                    parts = code.split(None, 1)
                    code = parts[0] + " F%s" % fmt(want * 60) + (" " + parts[1] if len(parts) > 1 else "")
                out_feed = want
                new.append(code.rstrip() + ((" " if code.strip() else "") + sep + comment if sep else ""))
            result.append("\n".join(new))
        return result

    def _header(self, out, before, after, report, p):
        lines = out[0].split("\n")
        for i, l in enumerate(lines):
            if l.startswith(";TIME:"):
                try:
                    old = float(l[6:])
                    lines[i] = ";TIME:%d" % round(old * after / before) if before > 0 else l
                except ValueError:
                    pass
        info = [";ADAPTIVE_SPEED estimated time %.0f s, was %.0f s" % (after, before),
                ";ADAPTIVE_SPEED minimum layer time %.1f s" % p["min_layer_time"],
                ";ADAPTIVE_SPEED flow limit %.1f mm3/s" % getattr(self, "flow_used", 0)]
        info += [";ADAPTIVE_SPEED " + r for r in report]
        at = next((i + 1 for i, l in enumerate(lines) if l.startswith(";Generated with")), len(lines))
        lines[at:at] = info
        out[0] = "\n".join(lines)
        return out


def breakdown(ext):
    """Lines for the header: for each reason, how much of the extruded length it decided, and how many
    seconds it saved (negative) or added, without acceleration."""
    total = sum(m.length for m in ext) or 1.0
    by = {}
    for m in ext:
        if not m.reason:
            continue
        length, dt = by.get(m.reason, (0.0, 0.0))
        by[m.reason] = (length + m.length, dt + m.length / m.speed - m.length / m.feed)
    lines = []
    for reason, (length, dt) in sorted(by.items(), key=lambda kv: kv[1][1]):
        lines.append("%-20s %5.1f%% of the length, %+.0f s" % (reason, 100 * length / total, dt))
    return lines


def cura_setting(data, key, default):
    """A setting of the first extruder in Cura. Without Cura (the command line), from the ;SETTING_3 lines that
    Cura writes at the end of a G-code file."""
    try:
        from cura.CuraApplication import CuraApplication

        stack = CuraApplication.getInstance().getGlobalContainerStack()
        v = stack.extruderList[0].getProperty(key, "value")
        if v is not None:
            return float(v)
    except Exception:
        pass
    text = "".join(l[11:] for c in data for l in c.split("\n") if l.startswith(";SETTING_3 "))
    m = re.search(re.escape(key) + r" = ([0-9.]+)", text)
    return float(m.group(1)) if m else default


def fmt(f):
    return ("%.1f" % f).rstrip("0").rstrip(".")


def split_gcode(text):
    """Splits a G-code file like Cura does for its scripts: the header, one chunk per layer, the end."""
    lines = text.split("\n")
    starts = [i for i, l in enumerate(lines) if l.startswith(";LAYER:")]
    if not starts:
        return [text]
    chunks = ["\n".join(lines[: starts[0]])]
    for a, b in zip(starts, starts[1:] + [None]):
        part = lines[a:b]
        if b is None:  # the last layer ends at its ;TIME_ELAPSED; the rest is the end G-code
            ends = [i for i, l in enumerate(part) if l.startswith(";TIME_ELAPSED")]
            if ends:
                chunks.append("\n".join(part[: ends[-1] + 1]))
                chunks.append("\n".join(part[ends[-1] + 1:]))
                continue
        chunks.append("\n".join(part))
    return chunks


def main(argv):
    if len(argv) < 3:
        print("usage: python3 AdaptiveSpeed.py in.gcode out.gcode [key=value ...]")
        return 2
    s = AdaptiveSpeed()
    s._values = {k: v[3] for k, v in SETTINGS.items()}
    for kv in argv[3:]:
        k, _, v = kv.partition("=")
        s._values[k] = v
    with open(argv[1]) as f:
        chunks = split_gcode(f.read())
    out = s.execute(chunks)
    with open(argv[2], "w") as f:
        f.write("\n".join(out))
    for l in out[0].split("\n"):
        if l.startswith(";ADAPTIVE_SPEED") or l.startswith(";TIME:"):
            print(l)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
