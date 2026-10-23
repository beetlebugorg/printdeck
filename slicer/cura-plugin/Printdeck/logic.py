# The parts of the Printdeck plugin that do not need Cura: the answer of the Printdeck, the choice
# of the material, the warning for a short roll, and the match of a Cura job with the print.

import json
import re


def parse_status(data):
    """Returns the answer of GET /api/plugin/printdeck as a dict with the keys material, diameter,
    spool, spools and job. spool and job are None when the Printdeck has none. spools is None
    when the Printdeck does not send the list of its rolls."""
    if not isinstance(data, dict):
        return None
    spool = data.get("spool") if isinstance(data.get("spool"), dict) else None
    job = data.get("job") if isinstance(data.get("job"), dict) else None
    # spools is the list of all rolls, or None from an older Printdeck.
    spools = data.get("spools")
    if isinstance(spools, list):
        spools = [sp for sp in spools if isinstance(sp, dict) and sp.get("id")]
    else:
        spools = None
    return {
        "material": str(data.get("material") or ""),
        "diameter": float(data.get("diameter") or 0),
        "spool": spool,
        "spools": spools,
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
    return "The model needs %.1f g of filament. %s has %.1f g left." % (needed, spool_name(spool), left)


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
    card = {"name": spool_name(spool), "detail": "%s · %.1f g left" % (spool.get("material") or "?", left),
            "color": spool.get("color") or color_from_name(spool_name(spool)),
            "fraction": max(0.0, min(1.0, left / full)), "job": "", "short": False}
    if needed > 0:
        if needed > left:
            card["job"] = "This print: %.1f g. %.1f g short." % (needed, needed - left)
            card["short"] = True
        else:
            card["job"] = "This print: %.1f g. %.1f g left after." % (needed, left - needed)
    return card


def roll_plan(spools, existing, made):
    """What each roll of the Printdeck needs in Cura. spools is the list of the rolls, existing the
    set of the base files of the materials of Cura, and made maps the ID of a roll to the base file
    of the material that the plugin made for it. Returns a list of (roll, action, base file):

    - "keep": the material of the roll is in Cura.
    - "link": the Printdeck has no material of Cura for the roll, or a material that Cura has no
      more, but the plugin made one before. The plugin tells the Printdeck that material.
    - "make": the roll has no material in Cura. The base file is "".
    """
    plan = []
    for sp in spools:
        cura = sp.get("cura_material") or ""
        if cura and cura in existing:
            plan.append((sp, "keep", cura))
        elif made.get(sp["id"]) in existing:
            plan.append((sp, "link", made[sp["id"]]))
        else:
            plan.append((sp, "make", ""))
    return plan


def roll_favorites(favorites, added, wanted):
    """The favorite materials of Cura with the materials of the rolls. favorites is the list of
    the favorites now (the preference cura/favorite_materials), added the materials that the plugin
    added before, and wanted the materials of the rolls now. A favorite that the plugin added and
    that no roll has now goes. The favorites of the user stay. Returns the new favorites and the
    new list of the materials that the plugin added."""
    added, wanted = set(added), [w for w in wanted if w]
    keep = [f for f in favorites if f and (f not in added or f in wanted)]
    mine = [w for w in wanted if w in added or w not in favorites]
    for w in wanted:
        if w not in keep:
            keep.append(w)
    return keep, sorted(set(mine))


def roll_material_changes(meta, spool):
    """The metadata of a material that the plugin made, which must change to follow its roll: a dict
    with name, color_code and color_name, or an empty dict. meta is the metadata of the material.
    A roll with no color keeps the color of the material."""
    name = spool_name(spool)
    changes = {}
    if meta.get("name") != name:
        changes["name"] = name
    color = spool.get("color") or ""
    if color and meta.get("color_code", "").lower() != color.lower():
        changes["color_code"] = color
    if (changes.get("color_code") or meta.get("color_code")) and meta.get("color_name") != name:
        changes["color_name"] = name
    return changes
