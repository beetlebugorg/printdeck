# Tests of the parts of the Printdeck plugin that do not need Cura: python3 -m unittest test_logic
import json
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

    def test_connected_text(self):
        self.assertEqual(logic.connected_text({"material": "PLA", "spool": {"name": "Galaxy Black", "material": "PLA", "left": 642.4}}),
                         "Connected to the Printdeck. Galaxy Black (PLA) is in the printer, with 642 g left.")
        self.assertIn("PLA is in the printer", logic.connected_text({"material": "PLA", "spool": None}))

    def test_preview_layer(self):
        self.assertEqual(logic.preview_layer({"layer": 12}), 11)
        self.assertEqual(logic.preview_layer({"layer": 0}), 0)



class TestCuraMaterialList(unittest.TestCase):
    def test_order_and_filter(self):
        mats = [
            {"id": "generic_pla", "name": "Generic PLA", "brand": "Generic", "material": "PLA", "color": "#fff", "readonly": True},
            {"id": "acme_pla", "name": "Acme PLA", "brand": "Acme", "material": "PLA", "color": "", "readonly": True},
            {"id": "mc_white", "name": "Microcenter White PLA", "brand": "Microcenter", "material": "PLA",
             "color": "#ffffff", "readonly": False, "density": "1.25"},
            {"id": "fav_petg", "name": "Fav PETG", "brand": "Fav", "material": "PETG", "color": "", "readonly": True},
            {"id": "cur_abs", "name": "Current ABS", "brand": "Cur", "material": "ABS", "color": "", "readonly": True},
        ]
        got = logic.cura_material_list(mats, {"fav_petg"}, "cur_abs")
        self.assertEqual([m["id"] for m in got], ["mc_white", "fav_petg", "cur_abs", "generic_pla"])
        self.assertNotIn("readonly", got[0])
        self.assertEqual(got[0]["color"], "#ffffff")
        # The density of Cura goes to the deck as a number, and 0 when Cura has none.
        self.assertEqual(got[0]["density"], 1.25)
        self.assertEqual(got[1]["density"], 0.0)


class TestRollMaterial(unittest.TestCase):
    nodes = [("generic_pla", "PLA", "Generic"), ("generic_petg", "PETG", "Generic"), ("acme_pla", "PLA", "Acme")]

    def test_base(self):
        # The material of Cura now, when it has the type of the roll: its settings carry over.
        self.assertEqual(logic.choose_base("PLA", "mc_white", "PLA", self.nodes), "mc_white")
        # Another type: the generic material of the type of the roll.
        self.assertEqual(logic.choose_base("PETG", "mc_white", "PLA", self.nodes), "generic_petg")

    def test_names(self):
        self.assertEqual(logic.material_slug("Galaxy Black (eSun)"), "galaxy_black_esun")
        self.assertEqual(logic.material_slug("!!"), "roll")
        self.assertEqual(logic.roll_brand("Generic"), "Printdeck")
        self.assertEqual(logic.roll_brand("Microcenter"), "Microcenter")

    def test_spool_settings(self):
        s = logic.spool_settings('{"other": {"spool_cost": 5}}', "g1", {"price": 22, "full": 1000})
        v = json.loads(s)
        self.assertEqual(v["g1"], {"spool_cost": 22.0, "spool_weight": 1000.0})
        self.assertEqual(v["other"], {"spool_cost": 5})
        self.assertEqual(json.loads(logic.spool_settings("bad", "g1", {})), {})

    def test_color_from_name(self):
        self.assertEqual(logic.color_from_name("Orange eSun"), "#ff7a1a")
        self.assertEqual(logic.color_from_name("Galaxy Black"), "#1a1a1a")
        self.assertEqual(logic.color_from_name("Redline"), "")


if __name__ == "__main__":
    unittest.main()
