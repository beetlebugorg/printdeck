# The parts of the Printdeck plugin that do not need Cura: the answer of the Printdeck, the choice
# of the material, the warning for a short roll, and the match of a Cura job with the print.

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

