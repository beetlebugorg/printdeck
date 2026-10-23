# Tests of Play in the Preview, which follows the printer: python3 -m unittest test_playsync
import unittest

from Printdeck import playsync

GCODE = b"""\
;FLAVOR:Marlin
M104 S200
G28
G1 Z5 F3000
G1 X10 Y10
;LAYER_COUNT:2
;LAYER:0
M117 Going
G0 X20 Y20
G1 X30 Y20 E1
G1 X30 Y30 E2
;LAYER:1
G1 Z0.4
G1 X40 Y30 E3
T0
G1 X50 Y30 E4
M107
"""


def end_of(text):
    """The byte after the line that starts with text."""
    i = GCODE.index(text)
    return GCODE.index(b"\n", i) + 1


class TestGcodeMap(unittest.TestCase):
    def test_moves(self):
        m = playsync.GcodeMap.parse(GCODE)
        # G28 and M commands add no point, and "M117 Going" is no move. The two moves before
        # ;LAYER:0 make a path of layer 0 with 1 line and 1 more entry, so the moves of ;LAYER:0
        # start at entry 2. The tool change ends a path of 2 lines: 3 entries.
        self.assertEqual(m.layers, [0, 0, 0, 0, 0, 1, 1, 1])
        self.assertEqual(m.starts, [0, 0, 2, 3, 4, 0, 1, 4])
        self.assertEqual(m.size, len(GCODE))

    def test_locate(self):
        m = playsync.GcodeMap.parse(GCODE)
        self.assertIsNone(m.locate(0))
        self.assertEqual(m.locate(end_of(b"G1 X30 Y20")), (0, 3))
        self.assertEqual(m.locate(end_of(b"G1 X30 Y20") + 3), (0, 3))  # in the next line
        # After the tool change, the new path starts with 2 points: its first line is entry 4.
        self.assertEqual(m.locate(len(GCODE)), (1, 4))

    def test_raft(self):
        data = b";LAYER:-2\nG1 X1\nG1 X2\n;LAYER:-1\nG1 X3\n;LAYER:0\nG1 X4\n"
        m = playsync.GcodeMap.parse(data)
        self.assertEqual(m.layers, [0, 0, 1, 2])  # the raft layers come first

    def test_no_layers(self):
        m = playsync.GcodeMap.parse(b"G1 X1\nG1 X2\nG1 X3\n")
        self.assertEqual((m.layers, m.starts), ([0, 0, 0], [0, 0, 1]))


class TestFollow(unittest.TestCase):
    def test_file_pos(self):
        self.assertEqual(playsync.file_pos({"pos": 120, "progress": 0.5}, 1000), 120)
        self.assertEqual(playsync.file_pos({"progress": 0.25}, 1000), 250)  # an older Printdeck

    def test_follow_time(self):
        durations = [1.0, 2.0, 4.0, 5.0]
        self.assertEqual(playsync.follow_time(durations, 2, 0, None), 2.0)
        # The printer moves at half the speed of the Preview.
        self.assertEqual(playsync.follow_time(durations, 2, 2, None), 3.0)
        # The time stops at the end of the layer, and after EXTRAPOLATE seconds.
        self.assertEqual(playsync.follow_time(durations, 3, 100, None), 5.0)
        self.assertEqual(playsync.follow_time([1.0, 100.0], 1, 100, None), 1.0 + playsync.EXTRAPOLATE / 2)
        # A little ahead: the Preview waits. Far ahead: it steps back.
        self.assertEqual(playsync.follow_time(durations, 1, 0, 2.5), 2.5)
        self.assertEqual(playsync.follow_time(durations, 0, 0, 4.5), 0.0)
        self.assertEqual(playsync.follow_time([], 0, 0, None), 0.0)


if __name__ == "__main__":
    unittest.main()
