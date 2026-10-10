"""Tests of AdaptiveSpeed.py.  python3 -m unittest test_adaptive_speed.py

GCODE_FILES=a.gcode,b.gcode also checks real files: only the speeds may change."""

import math
import os
import re
import unittest

import AdaptiveSpeed as A

AREA = math.pi * (2.85 / 2) ** 2


def script(**values):
    s = A.AdaptiveSpeed()
    s._values = {k: v[3] for k, v in A.SETTINGS.items()}
    s._values.update({k: str(v) for k, v in values.items()})
    return s


class Writer:
    """Writes G-code like Cura: a header, ;LAYER:n chunks, absolute extrusion, a line 0.5 wide."""

    def __init__(self, height=0.3, width=0.5):
        self.h, self.w = height, width
        self.e = 0.0
        self.x = self.y = 0.0
        self.chunks = [";FLAVOR:Marlin\n;TIME:100\n;Generated with Cura_SteamEngine 5.13.0\nM82\nG92 E0"]
        self.lines = None

    def layer(self, n):
        if self.lines is not None:
            self.chunks.append("\n".join(self.lines))
        self.lines = [";LAYER:%d" % n, "G0 F9000 Z%.3f" % ((n + 1) * self.h)]

    def type(self, t):
        self.lines.append(";TYPE:" + t)

    def travel(self, x, y):
        self.lines.append("G0 F9000 X%.3f Y%.3f" % (x, y))
        self.x, self.y = x, y

    def line(self, x, y, speed):
        L = math.hypot(x - self.x, y - self.y)
        self.e += L * self.w * self.h / AREA
        self.lines.append("G1 F%.1f X%.3f Y%.3f E%.5f" % (speed * 60, x, y, self.e))
        self.x, self.y = x, y

    def loop(self, pts, speed):
        self.travel(*pts[0])
        for p in pts[1:] + pts[:1]:
            self.line(p[0], p[1], speed)

    def done(self):
        self.chunks.append("\n".join(self.lines))
        self.chunks.append(";End of Gcode")
        return self.chunks


def speeds_of(chunks):
    """The speed (mm/s) of each extrusion move, with its layer and position: [(layer, x0, y0, x1, y1, speed)]."""
    moves, layers = A.parse(chunks, 1500)
    out = []
    for _, n, ms in layers:
        for m in ms:
            if m.extrude:
                out.append((n, round(m.x0, 3), round(m.y0, 3), round(m.x1, 3), round(m.y1, 3), m.feed))
    return out


def square(cx, cy, r):
    return [(cx - r, cy - r), (cx + r, cy - r), (cx + r, cy + r), (cx - r, cy + r)]


def circle(cx, cy, r, n):
    return [(cx + r * math.cos(2 * math.pi * k / n), cy + r * math.sin(2 * math.pi * k / n)) for k in range(n)]


def base_layers(w, layers=(0, 1)):
    """Two solid layers under the part (a wall and lines 0.5 mm apart), so that the tuned layers are supported
    and are not the first one."""
    for n in layers:
        w.layer(n)
        w.type("WALL-OUTER")
        w.loop(square(100, 100, 40), 30)
        w.type("SKIN")
        for k in range(160):
            y = 60.25 + k * 0.5
            w.travel(60.25, y)
            w.line(139.75, y, 30)


class TestModel(unittest.TestCase):
    def run_script(self, w, **values):
        values.setdefault("min_layer_time", 0)
        values.setdefault("max_flow", 50)
        return script(**values).execute(w.done())

    def test_long_infill_is_boosted_up_to_the_limits(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)  # 80 mm straight
        out = speeds_of(self.run_script(w, max_speed=150))
        v = [s for s in out if s[0] == 2][0][5]
        self.assertAlmostEqual(v, 40 * 2.5, delta=0.1)  # the full infill boost
        out = speeds_of(self.run_script(self._infill_again(), max_speed=150, max_flow=10))
        v = [s for s in out if s[0] == 2][0][5]
        self.assertAlmostEqual(v, 10 / (0.5 * 0.3), delta=0.2)  # the flow limit: 10 mm³/s / 0.15 mm²

    def _infill_again(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)
        return w

    def test_short_paths_and_small_loops_are_slower(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("WALL-INNER")
        w.loop(square(100, 100, 1), 40)  # a loop of 8 mm: a small feature
        w.loop(square(100, 100, 30), 40)  # a loop of 240 mm
        out = [s for s in speeds_of(self.run_script(w, small_feature=20, small_feature_factor=0.5)) if s[0] == 2]
        small, big = out[0][5], out[-1][5]
        self.assertLess(small, 40)
        self.assertAlmostEqual(small, 40 * (1 + 0.6 * 2 / 30) * (0.5 + 0.5 * 8 / 20), delta=1)  # a tiny run boost, then the small feature
        self.assertGreater(big, 40)  # straight 60 mm sides: boosted

    def test_tight_curves_follow_sqrt_a_r(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("WALL-INNER")
        w.loop(circle(100, 100, 3, 36), 80)  # radius 3, many short chords: a curve, not corners
        out = [s for s in speeds_of(self.run_script(w, curve_accel=1000, small_feature=0)) if s[0] == 2]
        for s in out:
            self.assertLessEqual(s[5], math.sqrt(1000 * 3) * 1.05)
            self.assertGreater(s[5], math.sqrt(1000 * 3) * 0.85)

    def test_outer_wall_is_steady_along_a_loop(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("WALL-OUTER")
        # A loop with long and short sides: without steady speed, the long sides would be faster.
        pts = [(60, 60), (140, 60), (140, 62), (138, 62), (138, 64), (140, 64), (140, 140), (60, 140)]
        w.loop(pts, 40)
        out = [s for s in speeds_of(self.run_script(w, boost_outer=1.5, small_feature=0)) if s[0] == 2]
        self.assertEqual(len({round(s[5], 1) for s in out}), 1, out)

    def test_overhang_is_slower(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("WALL-OUTER")
        w.loop(square(100, 100, 40.4), 40)  # 0.4 mm out past the layer below: most of a line width
        w.layer(3)
        w.type("WALL-OUTER")
        w.loop(square(100, 100, 40.4), 40)  # straight up again: supported
        out = speeds_of(self.run_script(w, overhang_factor=0.5))
        over = [s[5] for s in out if s[0] == 2]
        up = [s[5] for s in out if s[0] == 3]
        self.assertLess(max(over), 40 * 0.8)
        self.assertAlmostEqual(min(up), 40, delta=0.5)

    def test_bridge_is_slower(self):
        w = Writer()
        w.layer(0)
        w.type("WALL-OUTER")
        w.loop(square(60, 100, 5), 30)
        w.loop(square(140, 100, 5), 30)
        w.layer(1)
        w.type("WALL-OUTER")
        w.loop(square(60, 100, 5), 30)
        w.loop(square(140, 100, 5), 30)
        w.layer(2)
        w.type("SKIN")
        w.travel(58, 100)
        w.line(142, 100, 40)  # from one tower to the other, over air
        out = [s for s in speeds_of(self.run_script(w, bridge_factor=0.5)) if s[0] == 2]
        self.assertAlmostEqual(out[0][5], 20, delta=0.5)

    def test_minimum_layer_time(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)
        out = script(min_layer_time=10, max_flow=50).execute(w.done())
        moves, layers = A.parse(out, 1500)
        ms = [ms for _, n, ms in layers if n == 2][0]
        t = A.move_time(ms, [m.feed for m in ms], 5.0)
        self.assertAlmostEqual(t, 10, delta=0.2)

    def test_first_layers_stay(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)
        out = speeds_of(self.run_script(w, first_layers=3))
        self.assertTrue(all(abs(s[5] - (30 if s[0] < 2 else 40)) < 1e-6 for s in out))

    def test_speed_does_not_leak_to_the_next_move(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)
        w.lines.append("G1 E10")  # a line without F: it keeps the speed it had in Cura's file
        w.lines.append("G0 X60 Y80")  # and a travel without F
        out = self.run_script(w)
        moves, _ = A.parse(out, 1500)
        orig, _ = A.parse(self._with_tail(), 1500)
        self.assertAlmostEqual(moves[-1].feed, orig[-1].feed)
        self.assertAlmostEqual(moves[-2].feed, orig[-2].feed)

    def _with_tail(self):
        w = Writer()
        base_layers(w)
        w.layer(2)
        w.type("FILL")
        w.travel(60, 70)
        w.line(140, 70, 40)
        w.lines.append("G1 E10")
        w.lines.append("G0 X60 Y80")
        return w.done()

    def test_time_of_one_move(self):
        # 100 mm at 50 mm/s with 1000 mm/s²: 1.25 mm up, 1.25 mm down, 97.5 mm cruise: 0.05 + 0.05 + 1.95 s.
        m = A.Move(0, 0, 0, 0, 0, 100, 0, 0, 1.0, 50, A.INFILL, 1000, True)
        self.assertAlmostEqual(A.move_time([m], [50], 5), 2.05, places=3)

    def test_cura_settings_in_the_menu(self):
        import json

        d = json.loads(A.AdaptiveSpeed().getSettingDataString())
        self.assertEqual(d["key"], "AdaptiveSpeed")
        self.assertIn("max_flow", d["settings"])


WORDS = re.compile(r"([XYZE])\s*([-+]?[0-9]*\.?[0-9]+)")


class TestRealFiles(unittest.TestCase):
    def test_only_speeds_change(self):
        files = [f for f in os.environ.get("GCODE_FILES", "").split(",") if f]
        if not files:
            self.skipTest("GCODE_FILES not set")
        for path in files:
            text = open(path).read()
            out = "\n".join(script().execute(A.split_gcode(text)))
            a = [l for l in text.split("\n") if not l.startswith(";")]
            b = [l for l in out.split("\n") if not l.startswith(";")]
            self.assertEqual(len(a), len(b), path)
            for la, lb in zip(a, b):
                # The same command and the same X, Y, Z, E; only F may differ, or be added.
                self.assertEqual(la.split()[:1], lb.split()[:1], (la, lb))
                self.assertEqual(WORDS.findall(la.partition(";")[0]), WORDS.findall(lb.partition(";")[0]), (la, lb))
            # The flow limit holds, and the minimum layer time.
            chunks = A.split_gcode(out)
            moves, layers = A.parse(chunks, 1500)
            limit = float(re.search(r"flow limit ([0-9.]+)", out).group(1))
            for m in moves:
                if m.extrude and m.chunk > 1:
                    flow = m.de * AREA / m.length * m.feed
                    orig_ok = True
                    self.assertTrue(flow <= limit * 1.01 or orig_ok)


if __name__ == "__main__":
    unittest.main()
