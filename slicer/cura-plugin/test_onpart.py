# Tests of the print on a part: python3 -m unittest test_onpart
import unittest

from Printdeck import onpart


class TestOnPart(unittest.TestCase):
    def test_pocket(self):
        boxes = onpart.pocket_boxes(0, 0, 60, 40)
        self.assertEqual(len(boxes), 4)
        front = boxes[0]
        self.assertAlmostEqual(front[1], -0.3 - 2.0)  # outside the clearance
        self.assertAlmostEqual(front[0], -0.3 + 8.0)  # the corner is open
        verts, tris = onpart.box_mesh(front)
        self.assertEqual((len(verts), len(tris)), (24, 12))

    def test_top_point(self):
        # A box 0..60 x 0..40, 20 high: its top in two triangles, and a side.
        top = [((0, 20, 0), (60, 20, 0), (60, 20, 40)), ((0, 20, 0), (60, 20, 40), (0, 20, 40))]
        side = [((0, 0, 0), (60, 0, 0), (60, 20, 0))]
        x, z, y = onpart.top_point(top + side, [])
        self.assertEqual(y, 20)
        self.assertTrue(0 < x < 60 and 0 < z < 40)
        # A model covers the first triangle: the point is on the other one.
        p1 = onpart.top_point(top + side, [(30, 0, 60, 20)])
        self.assertLess(p1[0], 30)

    def test_to_gcode(self):
        self.assertEqual(onpart.to_gcode(10, 5, -20, 280, 280), (150, 160, 5))

    def test_insert_pause(self):
        # As Cura: the move to the Z of a layer is at the end of the layer before it.
        g = [";FLAVOR:Marlin\n", "PRINT_START BED=60 EXTRUDER=220\n", ";LAYER:0\nG0 X1 Y1 Z0.3\nG1 X2 E1\nG0 Z0.6\n",
             ";LAYER:1\nG1 X3 Y3 E1.5\nG0 Z20.3\nG0 X5 Y5\n", ";LAYER:2\nG1 F900 E2\nG1 X6 Y6 E3\n", "PRINT_END\n"]
        self.assertEqual(onpart.insert_pause(g, 20.0, 'The "panel"', (100, 120, 20), 10), 4)
        self.assertTrue(g[4].startswith(";LAYER:2\n;PRINTDECK"))
        self.assertIn("PRINTDECK_PAUSE MESSAGE=\"The 'panel'\" CHECK_X=100.000 CHECK_Y=120.000 CHECK_Z=20.000", g[4])
        lift, back = g[4].index("G1 Z30.000"), g[4].index("G1 Z20.300")
        self.assertLess(lift, g[4].index("PRINTDECK_PAUSE"))
        self.assertGreater(back, g[4].index("PRINTDECK_PAUSE"))  # back down before the first line
        self.assertLess(back, g[4].index("G1 F900 E2"))
        # The travel to the first point of the layer moved from layer 1 to after the pause.
        self.assertNotIn("G0 X5 Y5", g[3])
        self.assertNotIn("G0 Z20.3", g[3])
        travel = g[4].index("G0 F6000 X5.000 Y5.000")
        self.assertLess(g[4].index("PRINTDECK_PAUSE"), travel)
        self.assertLess(travel, back)

    def test_on_part_start(self):
        g = [";FLAVOR:Marlin\n", ";Generated\nPRINT_START BED=60 EXTRUDER=220 WIPE=160\nM117 x\n"]
        self.assertTrue(onpart.on_part_start(g))
        self.assertNotIn("PRINT_START", g[1])
        self.assertIn("G28 Z\nG28 X Y", g[1])
        self.assertIn("M109 S220", g[1])
        self.assertIn("PRINTDECK_PURGE", g[1])


if __name__ == "__main__":
    unittest.main()
