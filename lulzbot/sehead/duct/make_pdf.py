#!/usr/bin/env python3
"""SE tool head duct: write the build instructions PDF (se-duct-build.pdf).
Run after model.py, cura/make_profiles.py, and lineart/render.mjs. Needs Chromium.
The page style is the style of the BLTouch cable guide (../../bltouch/make_pdf.py)."""

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
OUT = HERE / "se-duct-build.pdf"

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


PIN = SUMMARY["pin"]

PARTS = [
    ("fit_lip", 1, "Fit test. The lip that goes into the blower outlet. Print it first."),
    ("duct", 1, "Bottom face on the bed. Solid. No supports."),
    ("blower_bracket", 1, "Blower side on the bed. Solid. No supports."),
    ("bltouch_bracket", 1, "Back face on the bed. Solid. No supports."),
]

HARDWARE = [
    ("2", "M3 countersunk screw (ISO 10642), 1 mm shorter than the front motor screws of the BLTouch mount",
     "The two front motor screws. Round down to a stock length; 2 mm shorter is also good. The heads sink into "
     "the blower bracket, under the blower."),
    ("1", "M3 × 18 socket screw and 1 M3 square nut (DIN 562, 1.8 mm)",
     "The top ear of the blower. The square nut goes into the pocket in the back of the bracket. Not longer than "
     "18 mm: the end of the screw stops at the motor."),
    ("1", "M3 × 25 socket screw and 1 M3 nut", "The low ear of the blower, through the boss of the bracket."),
    ("2", "M3 × 16 button head screws", "Two screws of the heat sink fan (now M3 × 12). They go through the pads "
     "of the BLTouch bracket."),
    ("2", "M3 × 12 socket screws and 2 M3 nuts", "The BLTouch, from the top of the shelf. The nuts go under the ears."),
]


P1 = f"""
<header class="cover">
  <div class="kicker">LulzBot TAZ Workhorse · SE tool head · BLTouch Smart</div>
  <h1>SE tool head duct</h1>
  <p class="lede">A new part cooling duct for the SE tool head, a bracket that puts the blower on the right end
  of the motor, and a bracket that puts the BLTouch behind and to the left of the nozzle. The duct goes round
  the hot end on the right, the back and the left. The front is open, so you and the cameras see the nozzle.</p>
</header>
{img("hero", "hero")}
<div class="two">
  <div>
    <h3>The space</h3>
    <table class="parts">
      <tr><td></td><td>The BLTouch pin is {-PIN[0]:.0f} mm left of the nozzle and {PIN[1]:.0f} mm behind it. Its body is
      {SPACE['BLTouch body to the duct']} mm from the duct and {SPACE['BLTouch to the mount']} mm from the extruder mount.</td></tr>
      <tr><td></td><td>At X home the BLTouch bracket is {SPACE['BLTouch bracket to the left X end at X home']} mm from
      the X motor. At X max the blower is {SPACE['blower to the right X end at X max']} mm from the right X end.</td></tr>
      <tr><td></td><td>The duct is {SPACE['duct bottom over the nozzle tip']} mm over the nozzle tip (the stock duct:
      5.9 mm). The blower stays inside the space of the old BLTouch mount on the right.</td></tr>
    </table>
  </div>
  <div>
    <h3>Sources</h3>
    <p>The head, the stock duct and the blower come from the LulzBot CAD of the SE tool head. The X carriage and
    the X ends come from the LulzBot CAD of the TAZ Workhorse. The front of the right X end, the motor connector
    and the blower outlet are estimates. Print the lip test first, and do the checks of MEASURE.md.</p>
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
      <li>PETG, 2.85 mm, 0.5 mm nozzle. PLA gets soft near the hot end.</li>
      <li>240 °C nozzle, 75 °C bed. These are starting values for this printer.</li>
      <li>0.2 mm layers, 100 % infill. Skirt. No supports.</li>
      <li>Profiles: <code>python3 cura/make_profiles.py --install</code> with Cura closed, then
      pick “SE duct - …”.</li>
    </ul>
  </div>
  <div>
    <h3>Tools</h3>
    <ul class="tight">
      <li>2.5 mm hex key, 2 mm hex key for the button heads and the countersunk screws.</li>
      <li>Small pliers for the nuts. Cable ties.</li>
    </ul>
  </div>
</div>
"""


STEPS = [
    ("fitlip", "Test the lip", [
        "Print the lip test first. Print the other parts after it fits.",
        "Push the lip into the outlet of the blower. It must go in by hand and stay in.",
        "If it is loose or does not go in, see LIP_PLAY on the last page.",
    ]),
    ("stock", "Remove the stock duct and the BLTouch mount", [
        "Turn off the printer and let the hot end cool.",
        "Remove the BLTouch and its mount from the right end of the motor. Keep the two front motor screws: the "
        "new countersunk screws must be 1 mm shorter.",
        "Remove the blower from the stock duct. Keep its cable connected.",
        "Remove the two screws that hold the stock duct: the front bottom motor screw, and the long screw at the "
        "back left of the Aero (from the left, behind the heat sink fan). The motor stays on its other screws.",
        "Take the stock duct off from below.",
    ]),
    ("duct", "Fit the duct", [
        "Put the duct up from below. The front tab goes between the Aero and the motor, at the front bottom motor "
        "screw. The post goes against the back left of the Aero.",
        "Put in the long screw at the back left through the post. Tighten it.",
        "Look under the head: the duct must not touch the heater block or the sock.",
    ]),
    ("bracket", "Fit the blower bracket", [
        "Put the M3 square nut into the pocket in the back of the bracket.",
        "Put the bracket on the end cap of the motor. The flange goes to the front of the motor, and the boss is "
        "under the front of the motor.",
        "Put in the two countersunk screws through the bracket into the front motor holes. The bottom screw also "
        "holds the front tab of the duct. Tighten them. The heads must be flush or under the face.",
    ]),
    ("blower", "Fit the blower", [
        "Put the blower on the bracket, the intake out to the right. The outlet goes down onto the lip of the duct.",
        "Put the M3 × 18 screw through the top ear into the square nut. Put the M3 × 25 screw through the low ear "
        "and the boss, with the nut on the back of the boss. Tighten both.",
        "Route the blower cable up along the motor. Tie it clear of the X end.",
    ]),
    ("bltbracket", "Fit the BLTouch bracket", [
        "Remove the back screw and the bottom screw of the heat sink fan (the two at the back bottom edge).",
        "Put the bracket on the fan: the pads on the screw holes, the shelf behind the fan.",
        "Put in the two M3 × 16 button head screws. Tighten them until the fan is firm. Do not crush the fan frame.",
    ]),
    ("bltouch", "Fit the BLTouch", [
        "Put the BLTouch under the shelf, the ears left and right, the wires up through the diamond hole.",
        "Put the two M3 × 12 screws down through the shelf and the ears, into the nuts under the ears. Tighten them.",
        "Route the BLTouch cable up behind the fan to the head cables. Tie it clear of the carriage.",
    ]),
    ("front", "Check, then set the printer", [
        "Move the head by hand to the left end and to the right end of X. Nothing touches the X ends.",
        "Turn on the printer. Test the blower at 100 %: air comes out of the right, back and left outlets.",
        f"In printer.cfg set [bltouch] x_offset: {PIN[0]:.2f} and y_offset: {PIN[1]:.2f}. Then run the Probe offset "
        "row of the Calibration page to measure the real offsets.",
        "Run PROBE_CALIBRATE for the new z_offset, and run the axis twist calibration again.",
        "Watch the first nozzle wipe: the BLTouch is now on the left, near the wiper pad.",
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
the build if a part touches the head, the X carriage, the motor connector, the right X end at X max, or the left
X end at X home, in the LulzBot CAD.</p>
<table class="parts">
  <tr><td></td><td><b>The lip is loose or tight in the blower outlet.</b> Set <code>LIP_PLAY</code> (now 0.3 mm on
  each side). The outlet size is an estimate from the CAD.</td></tr>
  <tr><td></td><td><b>The blower touches the right X end at X max.</b> Measure from the front face of the motor to
  the front of the X end. Set <code>XEND_FRONT_Y</code>.</td></tr>
  <tr><td></td><td><b>The motor plug touches the duct.</b> Measure from the bottom of the motor to the bottom of the
  plug and its cable. Set <code>PLUG_ROOM</code>.</td></tr>
  <tr><td></td><td><b>The duct touches a print, or the air misses the nozzle.</b> Set <code>DUCT_Z0</code> (the
  bottom of the duct over the nozzle tip) and <code>TARGET</code> (the point the outlets aim at).</td></tr>
  <tr><td></td><td><b>The BLTouch is too high or too low.</b> The body bottom must be 2.3 to 4.3 mm over the nozzle
  tip. Set <code>BLT_GAP</code>, or put thin washers under the ears.</td></tr>
</table>
"""


def main():
    bodies = [P1, parts_page()] + step_pages() + [ADJUST]
    n = len(bodies)
    pages = "".join(f'<section class="page">{b}<footer><span>TAZ Workhorse · SE tool head duct</span>'
                    f"<span>{i + 1} / {n}</span></footer></section>" for i, b in enumerate(bodies))
    css = B.CSS + f"""
.hero {{ width: 100%; max-height: 4.2in; object-fit: contain; display: block; margin: 0 auto 8px; }}
.shot {{ width: 100%; max-height: 5.6in; object-fit: contain; display: block; margin: 6px auto 10px; }}
.part {{ width: 70%; max-height: 3.2in; object-fit: contain; display: block; margin: 10px auto; }}
.order td {{ padding: 5px 6px; border-bottom: 1px solid #e3e3e3; vertical-align: top; }}
.order th {{ text-align: left; font-size: 8.5pt; color: #fff; background: {B.NAVY}; padding: 6px; }}
.why {{ font-size: 8.6pt; color: #555; }}
"""
    html = (f'<!doctype html><html><head><meta charset="utf-8"><title>SE tool head duct</title>'
            f"<style>{css}</style></head><body>{pages}</body></html>")
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "guide.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--no-pdf-header-footer", f"--print-to-pdf={OUT}", src.as_uri()],
                       check=True, capture_output=True)
    print(OUT)


if __name__ == "__main__":
    main()
