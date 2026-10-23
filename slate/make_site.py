#!/usr/bin/env python3
"""Printdeck: write the data of the Slate guide (guide/guide.json) and the images of the
documentation site (../docs/img/). Run after lineart/render.mjs. The text comes from make_pdf.py,
so the site and the PDF match. docs/build.py writes the pages of the site from guide.json."""

import json
import re
import subprocess
from pathlib import Path

import make_pdf as P

HERE = Path(__file__).resolve().parent
DOCS = HERE.parent / "docs"
IMG = DOCS / "img"
GUIDE = HERE / "guide"

SHOTS = ["hero", "rear", "exploded", "connect"] + [s[0] for s in P.STEPS] + [f"part_{n}" for n, _, _ in P.PRINTS]

SWATCH = {"Gray": "#80868E", "Orange": "#E8742A", "Navy": "#1F2B4D"}

# The size of the case: the slab is as wide and as deep as the case, and the slab with the chin
# is as high.
SIZE = "132 × 221 × 35 mm"

# The two extra STL files, from README.md.
EXTRAS = [
    {"file": "stl/fit_display.stl", "name": "display fit test",
     "notes": "The front skin around the screen opening. Print it face down with the slab profile, and put the "
              "display glass down into its pocket: the dark LCD border must show the same on each side."},
    {"file": "stl/drill_guide.stl", "name": "drill guide",
     "notes": "For a slab from before the chin screws. Drill the two 3.2 mm holes with it, then drill each hole "
              "out to 6 mm, 3.1 mm deep, for the screw head."},
]

# The last page of the PDF (make_pdf.end_page).
CONNECT = [
    "Connect the USB-C power supply to the Pi. The port is on the left edge of the Pi. Turn the cable to the rear.",
    "Connect the 3D printer to the black USB 2 port next to the board, from above.",
]

OPEN = [
    "Disconnect the cables on the rear. Remove the four screws under the foot, and lift the case off the foot.",
    "Remove the screw from each side wall of the slab.",
    "Push a pin or a paper clip into the small hole in each side wall, just above the chin. Each pin pushes "
    "the ridge of one chin tongue out of its groove. Pull the chin down.",
    "Slide the back plate down and out of the grooves. The drive and the Pi come out with it.",
]


def plain(s):
    return re.sub(r"</?code>", "", s)


def copy_images():
    IMG.mkdir(parents=True, exist_ok=True)
    for name in SHOTS:
        for theme in ("light", "dark"):
            src = P.IMG / f"{name}-{theme}.png"
            subprocess.run(["magick", str(src), "-resize", "1200x1200>", "-strip", str(IMG / src.name)], check=True)
    # The mount for the LulzBot TAZ Workhorse, on the printer.
    src = HERE.parent / "lulzbot" / "renders" / "printer-front-left.png"
    subprocess.run(["magick", str(src), "-resize", "1200x1200>", "-strip", str(IMG / "lulzbot-printer.png")], check=True)


def write_guide():
    GUIDE.mkdir(exist_ok=True)
    prints = []
    for name, colour, notes in P.PRINTS:
        f = P.FILAMENT[name]
        size = P.SUMMARY["parts"][name]["size"]
        prints.append({"file": f"stl/{name}.stl", "name": name, "colour": colour, "swatch": SWATCH[colour],
                       "count": 1, "grams": round(f["grams"]), "metres": f["metres"],
                       "cost": f"${P.cost(f['grams']):.2f}", "size": " × ".join(f"{v:.0f}" for v in size),
                       "notes": notes, "image": f"part_{name}", "supportCheck": P.SUPPORTS[name]})
    total = sum(p["grams"] for p in prints)
    metres = sum(p["metres"] for p in prints)
    guide = {
        "totals": {"grams": total, "metres": round(metres), "parts": len(prints), "cost": f"${P.cost(total):.2f}",
                   "pricePerKg": f"${P.PRICE_PER_KG:.2f}", "size": SIZE},
        "prints": prints,
        "extras": EXTRAS,
        "settings": [[k, plain(v)] for k, v in P.SETTINGS],
        "hardware": [[q, t] for q, t in P.HARDWARE],
        "electronics": [[q, t] for q, t in P.ELECTRONICS],
        "steps": [{"image": shot, "title": title, "need": need, "items": items, "note": note or ""}
                  for shot, title, need, items, note in P.STEPS],
        "connect": CONNECT,
        "open": OPEN,
    }
    (GUIDE / "guide.json").write_text(json.dumps(guide, indent=1, ensure_ascii=False) + "\n")
    print("wrote", GUIDE / "guide.json")


def main():
    copy_images()
    write_guide()


if __name__ == "__main__":
    main()
