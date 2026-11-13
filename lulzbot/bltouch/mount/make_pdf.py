#!/usr/bin/env python3
"""BLTouch mount: write the build instructions PDF (bltouch-mount-build.pdf).
Run after model.py, cura/make_profiles.py, and lineart/render.mjs. Needs Chromium.
The page style is the style of the BLTouch cable guide (../make_pdf.py)."""

import base64
import importlib.util
import json
import subprocess
import tempfile
from pathlib import Path

import shape as S

HERE = Path(__file__).resolve().parent
BUILD = HERE / "build"
IMG = BUILD / "img"
OUT = HERE / "bltouch-mount-build.pdf"

_spec = importlib.util.spec_from_file_location("bltouch_pdf", HERE.parents[1] / "bltouch" / "make_pdf.py")
B = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(B)

SUMMARY = json.loads((BUILD / "parts.json").read_text())
FILAMENT = json.loads((BUILD / "filament.json").read_text())
SPACE = SUMMARY["space"]
PRICE_PER_KG = 22.99                 # USD, PETG


def img(name, cls="shot"):
    data = base64.b64encode((IMG / f"{name}-light.png").read_bytes()).decode()
    return f'<img class="{cls}" src="data:image/png;base64,{data}">'


def grams(name):
    return FILAMENT.get(name, {}).get("grams", 0)


PROBE = SUMMARY["probe"]

PARTS = [
    ("bltouch_mount", 1, "Back face on the bed, the front up. Solid. No supports. It is also the fit test."),
]

HARDWARE = [
    ("2", f"M3 screws, {S.SCREW_LONGER:.0f} mm longer than the front motor screws",
     "They replace the two front screws of the motor. The plate is 4 mm thick under each head."),
    ("2", "M3 × 10 screws", "From under the BLTouch ears, up through the shelf."),
    ("2", "M3 nuts", "In the pockets on top of the shelf."),
]


P1 = f"""
<header class="cover">
  <div class="kicker">LulzBot TAZ Workhorse · SE tool head · BLTouch</div>
  <h1>BLTouch mount</h1>
  <p class="lede">A printed mount that holds the BLTouch beside the front of the extruder motor. It screws on with the
  two front motor screws, so it stays put when the head gets warm. The pin is close to the nozzle in Y, so the probe
  reaches the full bed.</p>
</header>
{img("hero", "hero")}
<div class="two">
  <div>
    <h3>The probe offsets</h3>
    <table class="parts">
      <tr><td></td><td>x_offset {PROBE['x_offset']} mm, y_offset {PROBE['y_offset']} mm (the old bracket: 12, -76)</td></tr>
      <tr><td></td><td>The pin reaches X {PROBE['pin_x'][0]} to {PROBE['pin_x'][1]} and
      Y {PROBE['pin_y'][0]} to {PROBE['pin_y'][1]}. The bed is 0 to 280.</td></tr>
      <tr><td></td><td>The bottom of the BLTouch is {SPACE['bltouch_bottom_over_nozzle']} mm over the nozzle tip
      (antclabs: 2.3 to 4.3 mm).</td></tr>
      <tr><td></td><td>At X 293 the X end passes {SPACE['x_end']} mm behind the mount.</td></tr>
    </table>
  </div>
  <div>
    <h3>Estimates</h3>
    <p>The motor, the X end and the BLTouch values are measured. The place of the blower fan, the ear thickness and
    the screw heads are estimates. MEASURE.md lists them. The mount itself is the fit test: it is small.</p>
  </div>
</div>
"""


def parts_page():
    rows = ""
    total = 0
    for name, qty, note in PARTS:
        g = grams(name)
        total += g * qty
        size = SUMMARY["parts"][name]["size"]
        rows += (f"<tr><td>{qty}</td><td><b>{name.replace('_', ' ')}</b><div class='why'>{note}</div></td>"
                 f"<td>{size[0]:.0f} × {size[1]:.0f} × {size[2]:.0f} mm</td><td>{g} g</td></tr>")
    hw = "".join(f"<tr><td>{q}</td><td><b>{n}</b><div class='why'>{w}</div></td><td></td><td></td></tr>"
                 for q, n, w in HARDWARE)
    return f"""
<h2>Parts and settings</h2>
<table class="order"><thead><tr><th>Qty</th><th>Part</th><th>Print size</th><th>PETG</th></tr></thead>
<tbody>{rows}{hw}</tbody></table>
<p class="note">Filament: about {total} g of PETG, ${total / 1000 * PRICE_PER_KG:.2f} at ${PRICE_PER_KG}/kg.</p>
<div class="two">
  <div>
    <h3>Print</h3>
    <ul class="tight">
      <li>PETG, 2.85 mm, 0.5 mm nozzle. PLA gets soft next to a warm motor.</li>
      <li>240 °C nozzle, 75 °C bed. These are starting values for this printer.</li>
      <li>0.2 mm layers, 2 mm walls, 100 % infill, skirt. No supports.</li>
      <li>Profiles: <code>python3 cura/make_profiles.py --install</code> with Cura closed, then
      pick “BLTouch mount - …”.</li>
    </ul>
  </div>
  <div>
    <h3>Tools</h3>
    <ul class="tight">
      <li>Phillips screwdriver for the motor screws.</li>
      <li>2.5 mm hex key or screwdriver for the BLTouch screws, small pliers for the nuts.</li>
    </ul>
  </div>
</div>
{img("part_bltouch_mount", "part")}
"""


STEPS = [
    ("bltouch", "Put the BLTouch on the mount", [
        "Turn off the printer. Remove the BLTouch and the old L bracket from the ear of the blower fan. Keep the "
        "BLTouch plugged in.",
        "Put an M3 nut in each pocket on top of the shelf.",
        "Feed the BLTouch wires up through the diamond hole in the shelf.",
        "Hold the ears against the bottom of the shelf. Put in the two M3 × 10 screws from below. Tighten them.",
    ]),
    ("mount", "Change the front motor screws", [
        "Remove only the upper front motor screw. Measure its length. The new screw is "
        f"{S.SCREW_LONGER:.0f} mm longer.",
        "Hold the mount on the motor face, the BLTouch down and to the front. Put the new screw through the upper "
        "hole and tighten it a little.",
        "Do the same with the lower front screw. Then tighten both. Change one screw at a time, so the motor stays "
        "in place.",
    ]),
    ("side", "Check the clearance", [
        "Move the head by hand to the right end of X. The X end must pass behind the BLTouch.",
        "Look from the front: the bottom of the BLTouch must be about 3.3 mm above the nozzle tip.",
        "Route the wires along the old path. Tie them clear of the X end.",
    ]),
    ("front", "Set the printer", [
        f"In printer.cfg, set x_offset: {PROBE['x_offset']} and y_offset: {PROBE['y_offset']} in [bltouch].",
        "Set the mesh to the full bed: mesh_min 15, 15 and mesh_max 265, 265.",
        "Set the z_tilt points so the pin is at X 15 and X 265.",
        "Do the paper test for z_offset, and the axis twist calibration again. Then probe the full bed hot.",
    ]),
]


def step_pages():
    out = []
    for i, (shot, title, lines) in enumerate(STEPS):
        lis = "".join(f"<li>{t}</li>" for t in lines)
        out.append(f"<h2>Step {i + 1}: {title}</h2>{img(shot)}<ol class='steps big'>{lis}</ol>")
    return out


ADJUST = """
<h2>Adjust the fit</h2>
<p>All the values are in <code>shape.py</code>. Change a value, then run <code>./make.sh</code>. The fit check stops
the build if a part touches the motor, the blower, the X end or the hot end.</p>
<table class="parts">
  <tr><td></td><td><b>The BLTouch is too high or too low.</b> Measure from the nozzle tip to the bottom of the
  BLTouch body, pin up. Set <code>BLT_GAP</code> (2.3 to 4.3).</td></tr>
  <tr><td></td><td><b>The X end touches the BLTouch.</b> Measure how far the X end starts behind the front of the
  motor. Set <code>X_END_Y0</code>.</td></tr>
  <tr><td></td><td><b>The screw holes do not line up.</b> Measure from the bottom of the motor and from the front of
  the motor to the centre of each front screw. Set <code>FRONT_SCREWS</code>.</td></tr>
  <tr><td></td><td><b>The plate touches the blower fan.</b> Set <code>BLOWER</code> from a measurement.</td></tr>
  <tr><td></td><td><b>The nuts are loose in the pockets.</b> Set <code>NUT_AF</code> 0.2 mm smaller.</td></tr>
</table>
"""


def main():
    bodies = [P1, parts_page()] + step_pages() + [ADJUST]
    n = len(bodies)
    pages = "".join(f'<section class="page">{b}<footer><span>TAZ Workhorse · BLTouch mount</span>'
                    f"<span>{i + 1} / {n}</span></footer></section>" for i, b in enumerate(bodies))
    css = B.CSS + f"""
.hero {{ width: 100%; max-height: 4.2in; object-fit: contain; display: block; margin: 0 auto 8px; }}
.shot {{ width: 100%; max-height: 5.6in; object-fit: contain; display: block; margin: 6px auto 10px; }}
.part {{ width: 70%; max-height: 3.2in; object-fit: contain; display: block; margin: 10px auto; }}
.order td {{ padding: 5px 6px; border-bottom: 1px solid #e3e3e3; vertical-align: top; }}
.order th {{ text-align: left; font-size: 8.5pt; color: #fff; background: {B.NAVY}; padding: 6px; }}
.why {{ font-size: 8.6pt; color: #555; }}
"""
    html = (f'<!doctype html><html><head><meta charset="utf-8"><title>BLTouch mount</title>'
            f"<style>{css}</style></head><body>{pages}</body></html>")
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "guide.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--no-pdf-header-footer", f"--print-to-pdf={OUT}", src.as_uri()],
                       check=True, capture_output=True)
    print(OUT)


if __name__ == "__main__":
    main()
