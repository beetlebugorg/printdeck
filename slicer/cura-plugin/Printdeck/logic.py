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


def material_lines(material, name, color=""):
    """The lines that tell the Printdeck the material of a file, for the header of the G-code:
    the type, the name and the color ("#rrggbb") of the material."""
    if not material:
        return ""
    out = ";PRINTDECK_MATERIAL: %s\n" % material.strip()
    if name and name.strip():
        out += ";PRINTDECK_MATERIAL_NAME: %s\n" % name.strip()
    if re.fullmatch(r"#[0-9a-fA-F]{6}", (color or "").strip()):
        out += ";PRINTDECK_COLOR: %s\n" % color.strip().lower()
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
           ("silver", "#c0c0c0"), ("gold", "#d4a531"), ("natural", "#efe6cf"), ("clear", "#e8f1f5"),
           ("navy", "#1f2a5a"), ("teal", "#16867d")]


def color_from_name(name):
    """The color of a roll from a color word in its name, for example "#ff7a1a" for "Orange eSun",
    or ""."""
    words = re.findall(r"[a-z]+", name.lower())
    for word, color in _COLORS:
        if word in words:
            return color
    return ""


def roll_card(status, needed, cura=None):
    """The texts of the roll card next to the slice button: a dict with the keys name, detail,
    color, fraction (the filament left on the roll, from 0 to 1), job (the line of the sliced
    model, or ""), short (True when the roll has too little filament for the model) and warn (the
    line when Cura has another material than the printer, or ""). None when the Printdeck has no
    roll and no material. needed is the grams of the sliced model, or 0. cura is the material of
    Cura: a dict with the keys base (the base file), name and material (PLA, PETG, ...), or None."""
    if not status:
        return None
    spool = status.get("spool")
    if not spool:
        material = status.get("material") or ""
        if not material:
            return None
        return {"name": material, "detail": "No roll on the Printdeck", "color": "", "fraction": 0.0,
                "job": "", "short": False, "warn": material_warning(cura, material, None)}
    left = float(spool.get("left") or 0)
    full = float(spool.get("full") or 0) or max(left, 1.0)
    card = {"name": spool_name(spool), "detail": "%s · %.1f g left" % (spool.get("material") or "?", left),
            "color": spool.get("color") or color_from_name(spool_name(spool)),
            "fraction": max(0.0, min(1.0, left / full)), "job": "", "short": False,
            "warn": material_warning(cura, spool.get("material") or status.get("material") or "", spool)}
    if needed > 0:
        if needed > left:
            card["job"] = "This print: %.1f g. %.1f g short." % (needed, needed - left)
            card["short"] = True
        else:
            card["job"] = "This print: %.1f g. %.1f g left after." % (needed, left - needed)
    return card


def material_warning(cura, material, spool):
    """The warning of the roll card when the material of Cura is not the filament in the printer,
    or "". material is the material type in the printer, and spool the roll, or None. When the
    roll has a material of Cura, the two materials must be the same. Otherwise the types must be
    the same."""
    if not cura:
        return ""
    name = cura.get("name") or cura.get("base") or "?"
    if spool and spool.get("cura_material") and cura.get("base"):
        if cura["base"] == spool["cura_material"]:
            return ""
        return "You have %s selected, the printer has %s installed." % (name, spool_name(spool))
    if material and cura.get("material") and cura["material"].strip().lower() != material.strip().lower():
        return "You have %s selected, the printer has %s installed." % (name, spool_name(spool) if spool else material)
    return ""


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
    """What changes to keep a material that the plugin made in step with its roll. Returns a pair:
    the metadata of the material that must change (a dict with name, color_code, color_name and
    printdeck_color, or an empty dict), and the color for the roll in the Printdeck, or None.

    meta is the metadata of the material. Its printdeck_color is the color of the last sync. A new
    color on the Printdeck goes to Cura, and a new color in Cura goes to the Printdeck. When both
    are new, the Printdeck wins. A roll with no color keeps the color of the material."""
    name = spool_name(spool)
    changes, to_deck = {}, None
    if meta.get("name") != name:
        changes["name"] = name
    deck = (spool.get("color") or "").lower()
    cura = (meta.get("color_code") or "").lower()
    last = (meta.get("printdeck_color") if "printdeck_color" in meta else meta.get("color_code") or "").lower()
    if deck != last:
        if deck and deck != cura:
            changes["color_code"] = spool["color"]
        changes["printdeck_color"] = deck
    elif cura != last:
        if cura:
            to_deck = meta["color_code"]
        changes["printdeck_color"] = cura
    if (changes.get("color_code") or meta.get("color_code")) and meta.get("color_name") != name:
        changes["color_name"] = name
    return changes, to_deck


def parse_estimate(data):
    """Returns the answer of POST /api/plugin/printdeck/estimate as a dict with the keys moves,
    prep and total, in seconds, or None when the answer has no positive total."""
    if not isinstance(data, dict):
        return None
    try:
        out = {k: float(data.get(k) or 0) for k in ("moves", "prep", "total")}
    except (TypeError, ValueError):
        return None
    if not out["total"] > 0 or out["moves"] < 0 or out["prep"] < 0:
        return None
    return out


def deck_feature_times(times, moves, prep):
    """The print times of the features of Cura for the time of the Printdeck. times is a dict from
    the feature (such as "infill") to its seconds in Cura. Each feature gets the factor that makes
    the sum the seconds of the moves. The feature "none" (Other in Cura) also gets prep, the
    seconds of the start macro, and the rest of the rounding. The sum of the result is the rounded
    total of the Printdeck."""
    total = int(round(moves + prep))
    cura = sum(max(0, t) for t in times.values())
    factor = moves / cura if cura > 0 else 0.0
    out = {k: int(round(max(0, t) * factor)) for k, t in times.items()}
    out["none"] = out.get("none", 0) + total - sum(out.values())
    if out["none"] < 0:
        # The rounding went over the total: the largest feature gets the difference.
        rest, out["none"] = out["none"], 0
        top = max(out, key=out.get)
        out[top] += rest
    return out


def duration_text(seconds):
    """A time as the Printdeck shows it: "1h 44m", or "25 min" below an hour."""
    m = int(seconds // 60)
    if m >= 60:
        return "%dh %02dm" % (m // 60, m % 60)
    return "%d min" % max(m, 1)


def deck_time_line(total, cura_total):
    """The line of the roll card with the time of the Printdeck for the sliced model, and the time
    of Cura before the change, in seconds."""
    line = "Printdeck time: %s" % duration_text(total)
    if cura_total > 0:
        line += " (Cura %s)" % duration_text(cura_total)
    return line


# The words in capitals that start a file name more often than a printer abbreviation does. The
# Printdeck keeps the same list (gcodemeta.CleanName).
_NOT_MACHINE = {"PLA", "PETG", "PET", "ABS", "ASA", "TPU", "PC", "PA", "PP", "PVA", "HIPS", "CF", "NYLON",
                "LED", "USB", "LCD", "PCB", "DIY", "RC"}


def clean_name(name, abbr=""):
    """A readable name from a Cura job name or a model file name, with the rules of the Printdeck
    for a file name: no folder, no extension, no machine prefix ("TAZWH_" or abbr + "_"), no
    version or copy at the end ("_v3", " (1)"), spaces for "_" and "-", and a capital first
    letter. It returns "" for an empty name."""
    name = (name or "").replace("\\", "/").split("/")[-1].strip()
    stem = re.sub(r"\.(gcode|gco|g|ufp|stl|3mf|obj|ply|amf|step|stp)$", "", name, flags=re.IGNORECASE)
    s = stem
    if abbr and s.startswith(abbr + "_") and s[len(abbr) + 1:].strip("_- "):
        s = s[len(abbr) + 1:]
    else:
        m = re.fullmatch(r"([A-Z0-9]{2,12})_(.*)", s, flags=re.DOTALL)
        if m and m.group(1) not in _NOT_MACHINE and re.search(r"[A-Z]", m.group(1)) and m.group(2).strip("_- "):
            s = m.group(2)
    while True:
        t = re.sub(r"(?:[_ -][vV]\d+|\s*\(\d+\))$", "", s)
        if t == s or not t.strip("_- "):
            break
        s = t
    s = re.sub(r"_|(?<!\d)-|-(?!\d)", " ", s)
    s = " ".join(s.split())
    if not s:
        return stem or name
    return s[0].upper() + s[1:]


def title_line(job_name, base_name="", abbr="", models=(), user_named=False):
    """The line that tells the Printdeck the name of the print, for the header of the G-code:
    ";Printdeck title: Hex planter". The name is the job name that the user typed, else the
    name of the one model on the build plate, else the job name without the machine prefix.
    It returns "" when there is no name."""
    if user_named:
        names = [base_name, job_name]
    elif len(models) == 1:
        names = [models[0], base_name, job_name]
    else:
        names = [base_name, job_name]
    for n in names:
        t = clean_name(n, abbr)
        # The Printdeck reads one line: no line ends in the title.
        t = " ".join("".join(c for c in t if c.isprintable() or c.isspace()).split())
        if t:
            return ";Printdeck title: %s\n" % t
    return ""
