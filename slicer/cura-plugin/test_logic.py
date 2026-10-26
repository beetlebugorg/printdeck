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
        self.assertEqual(logic.short_roll(120.4, spool), "The model needs 120.4 g of filament. Galaxy Black has 80.0 g left.")
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
        self.assertEqual(logic.material_lines("PLA", "Navy", "#1F2A5A"),
                         ";PRINTDECK_MATERIAL: PLA\n;PRINTDECK_MATERIAL_NAME: Navy\n;PRINTDECK_COLOR: #1f2a5a\n")
        self.assertEqual(logic.material_lines("PLA", "", "none"), ";PRINTDECK_MATERIAL: PLA\n")

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

    def test_roll_card(self):
        status = {"material": "PLA", "spool": {"id": "1", "name": "White", "material": "PLA", "left": 918, "full": 1000}}
        card = logic.roll_card(status, 0)
        self.assertEqual((card["name"], card["detail"], card["job"]), ("White", "PLA · 918.0 g left", ""))
        self.assertAlmostEqual(card["fraction"], 0.918)
        self.assertEqual(card["color"], "#f5f5f5")  # from the color word of the name
        card = logic.roll_card(status, 45.4)
        self.assertEqual(card["job"], "This print: 45.4 g. 872.6 g left after.")
        self.assertFalse(card["short"])
        card = logic.roll_card(status, 1020)
        self.assertEqual(card["job"], "This print: 1020.0 g. 102.0 g short.")
        self.assertTrue(card["short"])
        self.assertEqual(logic.roll_card({"material": "PETG", "spool": None}, 0)["detail"], "No roll on the Printdeck")
        self.assertIsNone(logic.roll_card({"material": "", "spool": None}, 0))
        self.assertIsNone(logic.roll_card(None, 0))
        self.assertEqual(card["warn"], "")

    def test_material_warning(self):
        roll = {"id": "1", "name": "White", "material": "PLA", "left": 918, "full": 1000,
                "cura_material": "printdeck_white"}
        status = {"material": "PLA", "spool": roll}
        white = {"base": "printdeck_white", "name": "White", "material": "PLA"}
        navy = {"base": "printdeck_navy", "name": "Navy", "material": "PLA"}
        self.assertEqual(logic.roll_card(status, 0, white)["warn"], "")
        self.assertEqual(logic.roll_card(status, 0, navy)["warn"], "You have Navy selected, the printer has White installed.")
        # A roll with no material of Cura: the types must be the same.
        status = {"material": "PLA", "spool": dict(roll, cura_material="")}
        self.assertEqual(logic.roll_card(status, 0, navy)["warn"], "")
        petg = {"base": "generic_petg", "name": "Generic PETG", "material": "PETG"}
        self.assertEqual(logic.roll_card(status, 0, petg)["warn"], "You have Generic PETG selected, the printer has White installed.")
        # No roll: the material of the printer.
        card = logic.roll_card({"material": "PLA", "spool": None}, 0, petg)
        self.assertEqual(card["warn"], "You have Generic PETG selected, the printer has PLA installed.")


    def test_roll_plan(self):
        rolls = [{"id": "1", "cura_material": "generic_pla"}, {"id": "2", "cura_material": "gone"},
                 {"id": "3"}, {"id": "4"}]
        plan = logic.roll_plan(rolls, {"generic_pla", "printdeck_red"}, {"2": "printdeck_red", "4": "printdeck_old"})
        self.assertEqual([(sp["id"], action, base) for sp, action, base in plan],
                         [("1", "keep", "generic_pla"), ("2", "link", "printdeck_red"), ("3", "make", ""), ("4", "make", "")])

    def test_roll_favorites(self):
        # The user has generic_abs. The plugin added printdeck_old before; its roll is gone.
        favs, added = logic.roll_favorites(["generic_abs", "printdeck_old", ""], ["printdeck_old"],
                                           ["printdeck_white", "generic_abs", ""])
        self.assertEqual(favs, ["generic_abs", "printdeck_white"])
        self.assertEqual(added, ["printdeck_white"])  # generic_abs was a favorite of the user
        # The next time, nothing changes.
        self.assertEqual(logic.roll_favorites(favs, added, ["printdeck_white", "generic_abs"]), (favs, added))
        # The roll goes: its material leaves the favorites, the favorite of the user stays.
        self.assertEqual(logic.roll_favorites(favs, added, []), (["generic_abs"], []))

    def test_roll_material_changes(self):
        # A material from before the sync: its color is the color of the last sync.
        meta = {"name": "White", "color_code": "#f5f5f5", "color_name": "White"}
        self.assertEqual(logic.roll_material_changes(meta, {"name": "White"}), ({"printdeck_color": ""}, None))
        self.assertEqual(logic.roll_material_changes(meta, {"name": "Snow", "color": "#FFFFFF"}),
                         ({"name": "Snow", "color_code": "#FFFFFF", "printdeck_color": "#ffffff", "color_name": "Snow"}, None))
        self.assertEqual(logic.roll_material_changes(meta, {"name": "White", "color": "#F5F5F5"}), ({}, None))
        # A new color in Cura goes to the Printdeck.
        meta = {"name": "White", "color_code": "#1f2a5a", "color_name": "White", "printdeck_color": "#f5f5f5"}
        self.assertEqual(logic.roll_material_changes(meta, {"name": "White", "color": "#f5f5f5"}),
                         ({"printdeck_color": "#1f2a5a"}, "#1f2a5a"))
        # A new color on the Printdeck goes to Cura, also when Cura has a new color.
        self.assertEqual(logic.roll_material_changes(meta, {"name": "White", "color": "#d22b2b"}),
                         ({"color_code": "#d22b2b", "printdeck_color": "#d22b2b"}, None))
        # In step.
        meta["printdeck_color"] = "#1f2a5a"
        self.assertEqual(logic.roll_material_changes(meta, {"name": "White", "color": "#1F2A5A"}), ({}, None))

    def test_parse_rolls(self):
        s = logic.parse_status({"material": "PLA", "spools": [{"id": "1"}, {"name": "no id"}, 3]})
        self.assertEqual(s["spools"], [{"id": "1"}])
        self.assertIsNone(logic.parse_status({"material": "PLA"})["spools"])

    def test_parse_estimate(self):
        e = logic.parse_estimate({"moves": 5850, "prep": 690, "total": 6540, "planned": 5625})
        self.assertEqual(e, {"moves": 5850.0, "prep": 690.0, "total": 6540.0})
        self.assertIsNone(logic.parse_estimate({"moves": 0, "prep": 0, "total": 0}))
        self.assertIsNone(logic.parse_estimate({"moves": "x", "total": 10}))
        self.assertIsNone(logic.parse_estimate({"moves": -1, "total": 10}))
        self.assertIsNone(logic.parse_estimate([]))

    def test_deck_feature_times(self):
        # Cura: 1h 41m of moves. The Printdeck: 1h 33m of moves and 11 min of start macro.
        cura = {"inset_0": 1800, "inset_x": 1200, "infill": 2400, "travel": 600, "none": 60}
        out = logic.deck_feature_times(cura, 5580, 660)
        self.assertEqual(sum(out.values()), 6240)
        self.assertEqual(set(out), set(cura))
        f = 5580 / 6060
        self.assertEqual(out["infill"], round(2400 * f))
        self.assertEqual(out["inset_0"], round(1800 * f))
        self.assertAlmostEqual(out["none"], 60 * f + 660, delta=2)
        # A Cura time of 0: all the time is "none".
        self.assertEqual(logic.deck_feature_times({"infill": 0}, 100.4, 20), {"infill": 0, "none": 120})
        self.assertEqual(logic.deck_feature_times({}, 100, 0), {"none": 100})
        # The rounding goes over the total with no start macro: "none" stays at 0.
        out = logic.deck_feature_times({"a": 1, "b": 1, "c": 1, "none": 0}, 4.5, 0)
        self.assertEqual(sum(out.values()), 4)
        self.assertTrue(all(v >= 0 for v in out.values()))

    def test_deck_time_line(self):
        self.assertEqual(logic.duration_text(6240), "1h 44m")
        self.assertEqual(logic.duration_text(1500), "25 min")
        self.assertEqual(logic.duration_text(20), "1 min")
        self.assertEqual(logic.deck_time_line(6240, 6060), "Printdeck time: 1h 44m (Cura 1h 41m)")
        self.assertEqual(logic.deck_time_line(6240, 0), "Printdeck time: 1h 44m")

if __name__ == "__main__":
    unittest.main()
