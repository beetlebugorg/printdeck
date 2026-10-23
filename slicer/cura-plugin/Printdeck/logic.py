# The parts of the Printdeck plugin that do not need Cura: the answer of the Printdeck, the choice
# of the material, the warning for a short roll, and the match of a Cura job with the print.

import json
import re


def parse_status(data):
    """Returns the answer of GET /api/plugin/printdeck as a dict with the keys material, diameter,
    spool and job. spool and job are None when the Printdeck has none."""
    if not isinstance(data, dict):
        return None
    spool = data.get("spool") if isinstance(data.get("spool"), dict) else None
    job = data.get("job") if isinstance(data.get("job"), dict) else None
    return {
        "material": str(data.get("material") or ""),
        "diameter": float(data.get("diameter") or 0),
        "spool": spool,
        "job": job,
    }


def same_material(a, b):
    """Reports that two material types are the same, for example "PLA" and "pla"."""
    return a.strip().upper() == b.strip().upper()


def choose_material(material, nodes):
    """Returns the base file of the Cura material for a material type, or None.

    nodes is a list of (base_file, material_type, brand) of the materials that fit the printer and
    the nozzle. A generic material comes first, then the first one of the type."""
    if not material:
        return None
    matches = [n for n in nodes if same_material(n[1] or "", material)]
    if not matches:
        return None
    for base_file, _, brand in matches:
        if (brand or "").lower() == "generic" or base_file.startswith("generic_"):
            return base_file
    return sorted(matches)[0][0]


def short_roll(needed, spool):
    """Returns the text of the warning when the model needs more grams than the roll has, or None.
    needed is the grams of the sliced model."""
    if not spool or needed <= 0:
        return None
    left = float(spool.get("left") or 0)
    if needed <= left:
        return None
    return "The model needs %d g of filament. %s has %d g left." % (round(needed), spool_name(spool), round(left))


def spool_name(spool):
    """The name of a roll, or its material."""
    return spool.get("name") or spool.get("material") or "The roll"


def _stem(name):
    name = re.sub(r"\.(gcode|g|gco|ufp)$", "", name.strip().lower())
    return re.sub(r"[ _\-]", "", name.split("/")[-1])


def job_matches(cura_job, printing_file):
    """Reports that the Cura job is the file that prints, for example "TAZ6_benchy" and
    "benchy.gcode". The names match when one contains the other."""
    a, b = _stem(cura_job or ""), _stem(printing_file or "")
    if len(a) < 4 or len(b) < 4:
        return False
    return a in b or b in a


def preview_layer(job):
    """The layer of the Preview of Cura for the print: Cura counts from 0."""
    return max(0, int(job.get("layer") or 1) - 1)


def material_lines(material, name):
    """The lines that tell the Printdeck the material of a file, for the header of the G-code."""
    if not material:
        return ""
    out = ";PRINTDECK_MATERIAL: %s\n" % material.strip()
    if name and name.strip():
        out += ";PRINTDECK_MATERIAL_NAME: %s\n" % name.strip()
    return out


def connected_text(status):
    """The message when Cura connects to the Printdeck: the filament in the printer."""
    spool = status.get("spool")
    if spool:
        return "Connected to the Printdeck. %s (%s) is in the printer, with %d g left." % (
            spool_name(spool), spool.get("material") or "?", round(float(spool.get("left") or 0)))
    if status.get("material"):
        return ("Connected to the Printdeck. %s is in the printer. To follow the grams of a roll, add the roll on "
                "the Printdeck: Filament > Rolls." % status["material"])
    return "Connected to the Printdeck. No filament is set on the Printdeck: Filament on the touch screen."



def cura_material_list(materials, favorites, current):
    """The materials of Cura for the rolls of the Printdeck: the materials of the user, the
    favorites, the material that Cura has now, and the generic ones, in that order, then by name.

    materials are dicts with the keys id (the base file), name, brand, material (the type), color,
    readonly (False for a material of the user) and density (g/cm³, a number or a text). favorites
    is a set of ids. Each material of the answer has the keys id, name, brand, material and color
    (texts), and density (a number, or 0 when it is unknown)."""
    out = []
    for m in materials:
        if not m.get("readonly", True):
            group = 0
        elif m["id"] in favorites:
            group = 1
        elif m["id"] == current:
            group = 2
        elif (m.get("brand") or "").lower() == "generic":
            group = 3
        else:
            continue
        out.append((group, (m.get("name") or "").lower(), m))
    out.sort(key=lambda g: g[:2])
    seen, answer = set(), []
    for _, _, m in out:
        if m["id"] in seen:
            continue
        seen.add(m["id"])
        entry = {k: str(m.get(k) or "") for k in ("id", "name", "brand", "material", "color")}
        entry["density"] = _number(m.get("density"))
        answer.append(entry)
    return answer


def _number(value):
    """value as a positive float, or 0."""
    try:
        v = float(value)
    except (TypeError, ValueError):
        return 0.0
    return v if v > 0 else 0.0


def material_slug(name):
    """A part of a material ID from a name: lower-case letters, digits and "_"."""
    slug = re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")
    return slug or "roll"


def choose_base(material, current_base, current_type, nodes):
    """The base file of the material that a new material for a roll copies: the material of Cura
    now when it has the type of the roll, so its settings carry over, else the material that
    choose_material gives. nodes are as for choose_material."""
    if current_base and same_material(current_type, material):
        return current_base
    return choose_material(material, nodes)


def roll_brand(base_brand):
    """The brand of a material that the plugin makes for a roll: the brand of the material that it
    copies, or Printdeck for a generic one."""
    if not base_brand or base_brand.lower() == "generic":
        return "Printdeck"
    return base_brand


def spool_settings(settings, guid, spool):
    """The preference cura/material_settings (JSON) with the price and the grams of a roll for the
    material with the GUID, so that Cura shows the cost of a print as the Printdeck does."""
    try:
        values = json.loads(settings or "{}")
    except ValueError:
        values = {}
    if not isinstance(values, dict):
        values = {}
    entry = dict(values.get(guid) or {})
    if spool.get("price"):
        entry["spool_cost"] = float(spool["price"])
    if spool.get("full"):
        entry["spool_weight"] = float(spool["full"])
    if entry:
        values[guid] = entry
    return json.dumps(values)


# The colors of the usual color words in the names of rolls.
_COLORS = [("black", "#1a1a1a"), ("white", "#f5f5f5"), ("orange", "#ff7a1a"), ("red", "#d22b2b"),
           ("blue", "#2a62d0"), ("green", "#2e9d4a"), ("yellow", "#f4d03f"), ("purple", "#7d3fb5"),
           ("pink", "#f28dc0"), ("brown", "#7a4b2a"), ("grey", "#8a8a8a"), ("gray", "#8a8a8a"),
           ("silver", "#c0c0c0"), ("gold", "#d4a531"), ("natural", "#efe6cf"), ("clear", "#e8f1f5")]


def color_from_name(name):
    """The color of a roll from a color word in its name, for example "#ff7a1a" for "Orange eSun",
    or ""."""
    words = re.findall(r"[a-z]+", name.lower())
    for word, color in _COLORS:
        if word in words:
            return color
    return ""


def roll_card(status, needed):
    """The texts of the roll card next to the slice button: a dict with the keys name, detail,
    color, fraction (the filament left on the roll, from 0 to 1), job (the line of the sliced
    model, or "") and short (True when the roll has too little filament for the model). None when
    the Printdeck has no roll and no material. needed is the grams of the sliced model, or 0."""
    if not status:
        return None
    spool = status.get("spool")
    if not spool:
        material = status.get("material") or ""
        if not material:
            return None
        return {"name": material, "detail": "No roll on the Printdeck", "color": "", "fraction": 0.0,
                "job": "", "short": False}
    left = float(spool.get("left") or 0)
    full = float(spool.get("full") or 0) or max(left, 1.0)
    card = {"name": spool_name(spool), "detail": "%s · %d g left" % (spool.get("material") or "?", round(left)),
            "color": spool.get("color") or color_from_name(spool_name(spool)),
            "fraction": max(0.0, min(1.0, left / full)), "job": "", "short": False}
    if needed > 0:
        if needed > left:
            card["job"] = "This print: %d g. %d g short." % (round(needed), round(needed - left))
            card["short"] = True
        else:
            card["job"] = "This print: %d g. %d g left after." % (round(needed), round(left - needed))
    return card
