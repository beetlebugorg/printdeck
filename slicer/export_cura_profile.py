#!/usr/bin/env python3
"""Copy the Cura profiles of the TAZ Workhorse from Cura into profiles/taz-workhorse-se/.

    python3 slicer/export_cura_profile.py [--cura ~/.local/share/cura/5.13] [--resources DIR]

The folder then has each container of the Cura stack of the printer, so a slice in GitHub
Actions (curaslice.py of printdeck-action) gets the same settings as Cura on this computer:

    definitions/         fdmprinter, fdmextruder (from the Cura install), and the Workhorse
    definition_changes/  the machine settings: the start and end G-code, the acceleration
    quality/             the generic quality "draft" of Cura
    quality_changes/     the profiles PLA 0.10 Fine, PLA 0.20 Standard and PLA 0.30 Fast
    materials/           the materials of the Printdeck rolls
    cura.json            the stack of each profile, and the version of Cura

The script copies no machine instance file: it has the API key of the deck. It also skips the
user layer of Cura (the settings that a person changed for one slice). A part sets those in its
printdeck.yml instead. Run it again after a change of a profile in Cura, and commit the result.
"""

import argparse
import json
import os
import re
import shutil
import sys
import urllib.parse

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "profiles", "taz-workhorse-se")
MACHINE = "lulzbot_taz_workhorse_se"
EXTRUDER = "lulzbot_taz_workhorse_se_extruder"
MACHINE_CHANGES = "LulzBot+TAZ+Workhorse+%7C+SE+%7C+0.5+mm_settings.inst.cfg"
EXTRUDER_CHANGES = "lulzbot_taz_workhorse_se_extruder+%232_settings.inst.cfg"
# The profiles: the name in printdeck.yml, and the name of the quality changes in Cura.
PROFILES = {
    "pla-0.10-fine": "PLA_0.10_Fine",
    "pla-0.20-standard": "PLA_0.20_Standard",
    "pla-0.30-fast": "PLA_0.30_Fast",
}
SECRET = re.compile(r"api_key|password|token", re.I)


def copy(src, dst):
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    text = open(src, encoding="utf-8").read()
    for line in text.splitlines():
        if SECRET.search(line.split("=", 1)[0]):
            sys.exit(f"{src} has a secret ({line.split('=', 1)[0].strip()}): it does not go into the repository")
    shutil.copyfile(src, dst)


def quality_type(path):
    m = re.search(r"^quality_type\s*=\s*(\S+)", open(path, encoding="utf-8").read(), re.M)
    return m.group(1) if m else "normal"


def material_info(path):
    text = open(path, encoding="utf-8").read()
    get = lambda tag: (re.search(rf"<{tag}>([^<]*)</{tag}>", text) or [None, ""])[1].strip()
    return {"brand": get("brand"), "material": get("material"), "color": get("color"),
            "color_code": get("color_code"), "roll": get("printdeck_roll")}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cura", default=os.path.expanduser("~/.local/share/cura/5.13"))
    ap.add_argument("--resources", default="/opt/ultimaker-cura/share/cura/resources")
    ap.add_argument("--version", default="5.13.0", help="the version of Cura and CuraEngine")
    a = ap.parse_args()
    cura, res = a.cura, a.resources
    if os.path.exists(OUT):
        shutil.rmtree(OUT)
    copy(os.path.join(res, "definitions", "fdmprinter.def.json"), os.path.join(OUT, "definitions", "fdmprinter.def.json"))
    copy(os.path.join(res, "definitions", "fdmextruder.def.json"), os.path.join(OUT, "definitions", "fdmextruder.def.json"))
    copy(os.path.join(cura, "definitions", MACHINE + ".def.json"), os.path.join(OUT, "definitions", MACHINE + ".def.json"))
    copy(os.path.join(cura, "extruders", EXTRUDER + ".def.json"), os.path.join(OUT, "definitions", EXTRUDER + ".def.json"))
    copy(os.path.join(cura, "definition_changes", MACHINE_CHANGES), os.path.join(OUT, "definition_changes", "machine.inst.cfg"))
    copy(os.path.join(cura, "definition_changes", EXTRUDER_CHANGES), os.path.join(OUT, "definition_changes", "extruder.inst.cfg"))
    stacks = {}
    for name, qc in PROFILES.items():
        g, e = qc + ".inst.cfg", qc + "_e0.inst.cfg"
        copy(os.path.join(cura, "quality_changes", g), os.path.join(OUT, "quality_changes", g))
        copy(os.path.join(cura, "quality_changes", e), os.path.join(OUT, "quality_changes", e))
        qt = quality_type(os.path.join(cura, "quality_changes", g))
        q = os.path.join(res, "quality", qt + ".inst.cfg")
        if not os.path.exists(os.path.join(OUT, "quality", qt + ".inst.cfg")):
            copy(q, os.path.join(OUT, "quality", qt + ".inst.cfg"))
        stacks[name] = {"quality_changes": "quality_changes/" + g, "quality_changes_extruder": "quality_changes/" + e,
                        "quality": "quality/" + qt + ".inst.cfg"}
    materials = {}
    for f in sorted(os.listdir(os.path.join(cura, "materials"))):
        if not f.startswith("printdeck_") or not f.endswith(".xml.fdm_material"):
            continue
        dst = urllib.parse.unquote(f).replace("#", "").replace(" ", "").replace("+", "-")
        copy(os.path.join(cura, "materials", f), os.path.join(OUT, "materials", dst))
        info = material_info(os.path.join(cura, "materials", f))
        key = info["roll"] and "roll-" + info["roll"] or dst.split(".")[0]
        materials[key] = {"file": "materials/" + dst, **info}
    json.dump({"cura": a.version, "machine": MACHINE, "extruder": EXTRUDER,
               "definition_changes": "definition_changes/machine.inst.cfg",
               "definition_changes_extruder": "definition_changes/extruder.inst.cfg",
               "profiles": stacks, "materials": materials}, open(os.path.join(OUT, "cura.json"), "w"), indent=1)
    print(f"wrote {OUT}: {len(stacks)} profiles, {len(materials)} materials")


if __name__ == "__main__":
    main()
