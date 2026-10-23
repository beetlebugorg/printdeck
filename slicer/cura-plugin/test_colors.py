# Tests of the color changes: python3 -m unittest test_colors
import unittest

from Printdeck import colors

WHITE = {"roll": "1", "name": "#1 White"}


def gcode(layers):
    """A G-code of Cura in chunks: the start, then a chunk for each layer at its Z, with an
    extrusion and the travel to the next layer."""
    out = [";FLAVOR:Marlin\n;LAYER_COUNT:%d\n" % len(layers)]
    for i, z in enumerate(layers):
        out.append(";LAYER:%d\nG0 F600 Z%.3f\nG1 X10 Y10 E%d\nG0 X20 Y20\n" % (i, z, i + 1))
    return out


class TestColors(unittest.TestCase):
    def test_letters_on_top(self):
        # A part 3 mm high, letters 3 to 3.6 mm: one change, at 3 mm, and no change back.
        groups = [{"roll": "2", "name": "#2 Navy", "bottom": 3.0, "top": 3.6}]
        self.assertEqual(colors.plan(groups, 3.0, WHITE), [(3.0, "2", "#2 Navy")])

    def test_back_to_the_start(self):
        # A band of color in the middle of a part: a change in, and a change back to the start.
        groups = [{"roll": "2", "name": "#2 Navy", "bottom": 3.0, "top": 4.0}]
        self.assertEqual(colors.plan(groups, 10.0, WHITE), [(3.0, "2", "#2 Navy"), (4.0, "1", "#1 White")])

    def test_on_the_bed(self):
        # A colored model from the bed: the print starts with it, so no change at the bed.
        groups = [{"roll": "2", "name": "#2 Navy", "bottom": 0.0, "top": 5.0}]
        self.assertEqual(colors.plan(groups, 2.0, WHITE), [])

    def test_two_colors_stacked(self):
        # Navy 3 to 4 mm, then red 4 to 5 mm on it: the change at 4 mm is to red.
        groups = [{"roll": "2", "name": "#2 Navy", "bottom": 3.0, "top": 4.0},
                  {"roll": "3", "name": "#3 Red", "bottom": 4.0, "top": 5.0}]
        self.assertEqual(colors.plan(groups, 3.0, WHITE), [(3.0, "2", "#2 Navy"), (4.0, "3", "#3 Red")])

    def test_insert(self):
        g = gcode([0.2, 0.4, 0.6, 0.8])
        # The colored models start at 0.4 mm: the change is at the layer at 0.6 mm.
        i = colors.insert_change(g, 0.4, "2", "#2 Navy")
        self.assertEqual(i, 3)
        self.assertIn('PRINTDECK_PAUSE MESSAGE="Load #2 Navy" ROLL=2', g[3])
        self.assertTrue(g[3].startswith(";LAYER:2\n"))
        # The travel to the first point of the layer is after the pause.
        self.assertNotIn("G0 X20 Y20", g[2])
        self.assertLess(g[3].index("PRINTDECK_PAUSE"), g[3].index("G0 F6000 X20.000 Y20.000"))
        self.assertEqual(colors.insert_change(gcode([0.2, 0.4]), 5.0, "2", "#2 Navy"), -1)

    def test_apply_two(self):
        # A band: in at 0.4 mm, back at 0.6 mm. Each change is at its own layer.
        g = gcode([0.2, 0.4, 0.6, 0.8])
        self.assertEqual(colors.apply(g, [(0.4, "2", "#2 Navy"), (0.6, "1", "#1 White")]), [])
        self.assertIn("ROLL=2", g[3])
        self.assertIn("ROLL=1", g[4])


if __name__ == "__main__":
    unittest.main()
