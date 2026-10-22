# Tests of the parts of the Printdeck plugin that do not need Cura: python3 -m unittest test_logic
import unittest

from Printdeck import logic


class TestLogic(unittest.TestCase):
    def test_parse(self):
        s = logic.parse_status({"material": "PLA", "diameter": 2.85, "spool": {"id": "1", "name": "Black", "left": 640}, "job": None})
        self.assertEqual(s["material"], "PLA")
        self.assertEqual(s["spool"]["left"], 640)
        self.assertIsNone(s["job"])
        self.assertIsNone(logic.parse_status([]))

    def test_choose_material(self):
        nodes = [("ultimaker_pla_175", "PLA", "Ultimaker"), ("generic_pla_175", "PLA", "Generic"), ("generic_petg_175", "PETG", "Generic")]
        self.assertEqual(logic.choose_material("pla", nodes), "generic_pla_175")
        self.assertEqual(logic.choose_material("PETG", nodes), "generic_petg_175")
        self.assertIsNone(logic.choose_material("TPU", nodes))
        self.assertIsNone(logic.choose_material("", nodes))

    def test_short_roll(self):
        spool = {"name": "Galaxy Black", "left": 80}
        self.assertEqual(logic.short_roll(120.4, spool), "The model needs 120 g of filament. Galaxy Black has 80 g left.")
        self.assertIsNone(logic.short_roll(50, spool))
        self.assertIsNone(logic.short_roll(120, None))

    def test_job_matches(self):
        self.assertTrue(logic.job_matches("TAZ6_benchy", "benchy.gcode"))
        self.assertTrue(logic.job_matches("Lower Panel", "LTAZ_lower_panel.gcode"))
        self.assertFalse(logic.job_matches("cap", "cap.gcode"))  # too short to match
        self.assertFalse(logic.job_matches("benchy", "rail_clip.gcode"))

    def test_material_lines(self):
        self.assertEqual(logic.material_lines("ABS", "Generic Black ABS"), ";PRINTDECK_MATERIAL: ABS\n;PRINTDECK_MATERIAL_NAME: Generic Black ABS\n")
        self.assertEqual(logic.material_lines("PLA", ""), ";PRINTDECK_MATERIAL: PLA\n")
        self.assertEqual(logic.material_lines("", "x"), "")

    def test_preview_layer(self):
        self.assertEqual(logic.preview_layer({"layer": 12}), 11)
        self.assertEqual(logic.preview_layer({"layer": 0}), 0)


if __name__ == "__main__":
    unittest.main()
