#!/usr/bin/env python3
"""Gantry camera arm: write the build instructions PDF (gantry-camera-arm-build.pdf).
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
OUT = HERE / "gantry-camera-arm-build.pdf"

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


STIFF = SUMMARY["stiffness"]

PARTS = [
    ("fit_arm_slice", 1, "Fit test. The top 8 mm of the upper arm. Top face on the bed. Print it first."),
    ("fit_bracket_slice", 1, "Fit test. The top 10 mm of the camera bracket. Print it first."),
    ("clamp_plate", 1, "Back face on the bed. Solid. It is also a fit test for the two screws."),
    ("upper_arm", 1, "Upside down: the top face on the bed. No supports."),
    ("camera_bracket", 1, "Bottom face on the bed. No supports."),
]

HARDWARE = [
    ("1", "M3 socket screw, 3 mm longer than the lower X end screw",
     "The lower screw. Its head sinks into the plate, so 3 mm of plate is under the head."),
    ("1", "M3 socket screw, 9 mm longer than the upper X end screw",
     "The upper screw. It goes through the arm (3 mm under the head) and the plate (6 mm)."),
    ("2", "M3 × 30 screws and 2 M3 nuts", "They hold the camera bracket to the drop of the arm. The nuts go in the "
     "pockets at the back of the drop."),
    ("1", "1/4\"-20 × 1/4\" screw", "Holds the U bracket of the camera in the slot. The screw of the old corner mount fits."),
]


P1 = f"""
<header class="cover">
  <div class="kicker">LulzBot TAZ Workhorse · SE tool head · Arducam B0561</div>
  <h1>Gantry camera arm</h1>
  <p class="lede">A printed arm that holds the camera in front of the front right upright, at the height of the
  nozzle. It hangs from the right X end, so it goes up and down with the gantry: the nozzle and the layer stay at
  one height in the picture. It replaces the corner camera mount.</p>
</header>
{img("hero", "hero")}
<div class="two">
  <div>
    <h3>The space</h3>
    <table class="parts">
      <tr><td></td><td>The clamp plate is {SPACE['lower plate to motor']} mm from the motor at X 293, and the beam
      {SPACE['beam over the motor top']} mm over it.</td></tr>
      <tr><td></td><td>The drop is {SPACE['drop to the BLTouch tab']} mm from the BLTouch mount tab and
      {SPACE['drop to the upright']} mm from the upright.</td></tr>
      <tr><td></td><td>The bracket is {SPACE['bracket to the upright face']} mm in front of the upright, and its top is
      {SPACE['bracket top under the top plate at Z max']} mm under the top corner plate at Z 297.</td></tr>
      <tr><td></td><td>The camera on the arm: about {STIFF['left_right_hz']} Hz left and right, {STIFF['front_back_hz']} Hz
      front and back. The input shaper peaks are about 40 to 60 Hz.</td></tr>
    </table>
  </div>
  <div>
    <h3>Estimates</h3>
    <p>The X end face, the screws and the upright are measured. The height of the belt strip, the top of the motor,
    and the corner plates are estimates. MEASURE.md lists them. Print the two fit tests and the clamp plate first, and do steps 1 to 3.</p>
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
      <li>PETG, 2.85 mm, 0.5 mm nozzle. PLA creeps under the screws.</li>
      <li>240 °C nozzle, 75 °C bed. These are starting values for this printer.</li>
      <li>0.2 mm layers, 2 mm walls. The plate 100 % infill; the arm and the bracket 40 % gyroid. Skirt. No supports.</li>
      <li>Profiles: <code>python3 cura/make_profiles.py --install</code> with Cura closed, then
      pick “Gantry camera - …”.</li>
    </ul>
  </div>
  <div>
    <h3>Tools</h3>
    <ul class="tight">
      <li>2.5 mm hex key for the M3 screws.</li>
      <li>A 3/16" hex key for the 1/4"-20 screw, small pliers for the nuts.</li>
    </ul>
  </div>
</div>
"""


STEPS = [
    ("plate", "Fit the clamp plate", [
        "Print the clamp plate and the two fit tests first. Print the upper arm and the bracket only after the "
        "tests in steps 2 and 3 pass.",
        "Turn off the printer. Remove the old corner camera mount from the upright. Keep the camera and its screw.",
        "Move the gantry up to a height that is easy to reach. Push the head to the left.",
        "Remove the lower screw of the X end face. Put the clamp plate in the pocket of the face, the counterbore to "
        "the front. It must go in with a little play. If it does not go in, see FACE_W on the last page.",
        "Look through the upper hole of the plate. The upper screw must be in the middle of it. If it is not, see "
        "SCREW_Z on the last page.",
        "Put in the new lower screw (3 mm longer) and tighten it. Its head is flush with the plate. Leave the upper "
        "screw in the X end for now.",
    ]),
    ("fitarm", "Test the arm slice", [
        "The arm slice is the top 8 mm of the upper arm. Its top is level with the top of the clamp plate.",
        "Put the slice on the top of the clamp plate: the plate goes into the pocket at the back of the slice. The "
        "long side goes to the right, toward the upright. Hold the slice level.",
        "Look at the right end of the slice, the top of the drop. It must stop about 2 mm from the inner face of the "
        "upright. If it touches, or the gap is more than 4 mm, see SCREW_X on the last page.",
        "Look at the back of the slice, left of the plate. It must not touch the raised belt strip of the X end. If "
        "it touches, see STRIP_PROUD on the last page.",
        "Look at the front of the slice. It must be a little behind the front face of the upright.",
        "Remove the slice. It does not stay on the printer.",
    ]),
    ("fitbracket", "Test the bracket slice", [
        "The bracket slice is the top 10 mm of the camera bracket. It goes around the front of the upright, 10 mm "
        "in front of it.",
        "Hold the slice around the front of the upright. The front wall is in front of the upright, the short rib "
        "is on the right, and the tongue goes back on the left side of the upright.",
        "Keep about 2 mm between the tongue and the upright. The back end of the tongue is then where the drop of "
        "the arm comes down.",
        "Slide the slice down the upright to the bottom corner plate, then up to the top corner plate. The plates "
        "and their screw heads must pass inside the front wall with a gap.",
        "If a screw head touches the front wall, see the corner plates on the last page.",
        "Remove the slice. If steps 2 and 3 pass, print the upper arm and the camera bracket.",
    ]),
    ("arm", "Fit the upper arm", [
        "Remove the upper screw of the X end face.",
        "Push the arm back onto the plate: the plate goes into the pocket at the back of the beam. The drop goes down "
        "beside the inner face of the upright.",
        "Put in the new upper screw (9 mm longer) through the arm and the plate. Tighten it.",
        "Put an M3 nut in each pocket at the back of the drop.",
    ]),
    ("bracket", "Fit the camera bracket", [
        "Hold the bracket around the front of the upright. Its tongue goes back to the front of the drop.",
        "Put the two M3 × 30 screws from the front, through the bracket and the drop, into the nuts. Tighten them.",
        "The bracket must not touch the upright: about 10 mm in front of it, and 2 mm beside it.",
    ]),
    ("camera", "Fit the camera", [
        "Put the 1/4\"-20 screw from the back of the slot into the base of the U bracket. Tighten it a little.",
        "Turn the camera on its pivot until it looks at the middle of the bed.",
        "Slide it up or down in the slot until you see the nozzle tip under the BLTouch. Tighten the screw.",
        "Route the USB cable along the old cable path, with a loop for the full Z travel.",
    ]),
    ("top", "Check the travel", [
        "With the motors off, move the head by hand to the right end of X. The motor passes in front of the plate, "
        "and the BLTouch tab passes left of the drop.",
        "Home the printer. Move Z slowly to 297, then to 0, and watch the bracket and the corner plates.",
        "On the deck, set the rotation of this camera: the U bracket stands on its side, so the picture turns 90°.",
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
the build if a part touches the X end, the tool head at any X, the upright, or a corner plate at any Z.</p>
<table class="parts">
  <tr><td></td><td><b>The plate does not go into the pocket.</b> Measure the face between the belt strip and the step.
  Set <code>FACE_W</code>.</td></tr>
  <tr><td></td><td><b>The holes do not line up.</b> Measure the screws, top edge to top edge. Set the spacing in
  <code>SCREW_Z</code>.</td></tr>
  <tr><td></td><td><b>The arm touches the upright.</b> Measure from the screws to the inner face of the upright. Set
  <code>UP_X0</code> through <code>SCREW_X</code>.</td></tr>
  <tr><td></td><td><b>The beam touches the belt strip.</b> Measure how far the strip stands in front of the face. Set
  <code>STRIP_PROUD</code>.</td></tr>
  <tr><td></td><td><b>A corner plate touches the bracket.</b> Measure how far the plate and its screw heads stand
  out from the front face of the upright. Set <code>PLATE_PROUD</code>, and set <code>FRONT_GAP</code> 3 mm more
  than it.</td></tr>
  <tr><td></td><td><b>The head touches the beam at X 293.</b> Measure the top of the motor over the nozzle tip. Set
  <code>HEAD_SPLIT_Z</code>; the beam starts 6 mm over it.</td></tr>
  <tr><td></td><td><b>The nuts are loose in the pockets.</b> Set <code>NUT_AF</code> 0.2 mm smaller.</td></tr>
</table>
"""


def main():
    bodies = [P1, parts_page()] + step_pages() + [ADJUST]
    n = len(bodies)
    pages = "".join(f'<section class="page">{b}<footer><span>TAZ Workhorse · Gantry camera arm</span>'
                    f"<span>{i + 1} / {n}</span></footer></section>" for i, b in enumerate(bodies))
    css = B.CSS + f"""
.hero {{ width: 100%; max-height: 4.2in; object-fit: contain; display: block; margin: 0 auto 8px; }}
.shot {{ width: 100%; max-height: 5.6in; object-fit: contain; display: block; margin: 6px auto 10px; }}
.part {{ width: 70%; max-height: 3.2in; object-fit: contain; display: block; margin: 10px auto; }}
.order td {{ padding: 5px 6px; border-bottom: 1px solid #e3e3e3; vertical-align: top; }}
.order th {{ text-align: left; font-size: 8.5pt; color: #fff; background: {B.NAVY}; padding: 6px; }}
.why {{ font-size: 8.6pt; color: #555; }}
"""
    html = (f'<!doctype html><html><head><meta charset="utf-8"><title>Gantry camera arm</title>'
            f"<style>{css}</style></head><body>{pages}</body></html>")
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "guide.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--no-pdf-header-footer", f"--print-to-pdf={OUT}", src.as_uri()],
                       check=True, capture_output=True)
    print(OUT)


if __name__ == "__main__":
    main()
