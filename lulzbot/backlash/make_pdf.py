#!/usr/bin/env python3
"""TAZ Workhorse: write the guide for the cube wire on Y-MAX (taz-cube-ymax.pdf). Needs Chromium.

Sources:
  - The UltiMachine RAMBo 1.4a design files (github.com/ultimachine/RAMBo-1.4): sheet 3
    (Endstops). rambo-endstops.png is a crop of "PCB 3D Print (top)_RAMBo_1.4a.PDF", as in the
    BLTouch cable guide.
  - LulzBot Marlin (gitlab.com/lulzbot3d/marlin), Configuration_adv.h: G425 and the cube of the
    Workhorse (CALIBRATION_OBJECT_CENTER 266, -16, -2 and DIMENSIONS 10, 4, 10).
  - The Workhorse bill of materials (../ref/lulzbot/TAZ_Workhorse_NA_BOM.csv): PP-MP0227, XY
    Calibration Cube.
  - ../../printer/printer.cfg: [printdeck_cube].

The page style is a copy of the style of the BLTouch cable guide (lulzbot/bltouch/make_pdf.py).

  python3 make_pdf.py
"""

import base64
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "taz-cube-ymax.pdf"
PAGES = 3

NAVY, ORANGE = "#1F2B4D", "#E8742A"
WIRE = {"yellow": "#E8C21A", "white": "#FFFFFF", "black": "#1A1A1A"}


def img(name):
    return "data:image/png;base64," + base64.b64encode((HERE / name).read_bytes()).decode()


def board(crop, marks, label):
    """A crop of the ENDSTOPS block of the RAMBo, with pins marked. crop is (x, y, w, h) in the
    picture. marks are (x, y, wire color or None for an empty pin, text)."""
    x0, y0, w, h = crop
    s = f'<svg viewBox="{x0} {y0} {w} {h}" xmlns="http://www.w3.org/2000/svg" class="board">'
    s += f'<image href="{img("rambo-endstops.png")}" x="0" y="0" width="1250" height="650"/>'
    for x, y, color, text in marks:
        if color:
            s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="#333" stroke-width="13"/>'
            s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="{WIRE[color]}" stroke-width="9"/>'
        else:
            s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="{ORANGE}" stroke-width="5" stroke-dasharray="8 6"/>'
        s += f'<text x="{x}" y="{y + 78}" class="bmark">{text}</text>'
    s += f'<rect x="{x0}" y="{y0 + h - 44}" width="{w}" height="44" fill="{NAVY}" opacity="0.9"/>'
    s += f'<text x="{x0 + 16}" y="{y0 + h - 14}" class="blabel">{label}</text>'
    return s + "</svg>"


# The pins in the picture: the top row is X-MIN, Y-MIN, Z-MIN with + − S from the left. The bottom
# row is X-MAX, Y-MAX, Z-MAX with S − + from the left.
ZMIN = board((20, 110, 760, 330), [(574, 288, None, "+"), (632, 288, None, "−"), (690, 288, "yellow", "S")],
             "Before: Z-MIN · X21 · top row")
YMAX = board((20, 250, 760, 330), [(334, 438, "yellow", "S"), (394, 438, None, "−"), (454, 438, None, "+")],
             "After: Y-MAX · X25 · bottom row")


def circuit():
    """The circuit: the RAMBo pulls S up to 5 V. The cube and the four washers are on the wire. The
    nozzle is on ground. A touch pulls S to ground."""
    s = '<svg viewBox="0 0 900 330" xmlns="http://www.w3.org/2000/svg" class="diagram">'
    # RAMBo
    s += f'<rect x="20" y="60" width="210" height="200" rx="10" fill="#2E6B3A" stroke="#1d4a27" stroke-width="3"/>'
    s += '<text x="125" y="95" class="pcb" style="text-anchor:middle;font-size:16px">RAMBo 1.4</text>'
    s += '<text x="125" y="125" class="pcb" style="text-anchor:middle;font-size:13px">Y-MAX (PA1)</text>'
    s += '<rect x="150" y="150" width="60" height="26" rx="3" fill="#E9E4D2" stroke="#555"/>'
    s += '<text x="180" y="168" class="mark">S</text>'
    s += '<text x="125" y="215" class="pcb" style="text-anchor:middle;font-size:12px">pull-up to 5 V</text>'
    s += '<text x="125" y="240" class="pcb" style="text-anchor:middle;font-size:12px">GND: − pin, frame</text>'
    # wire
    s += f'<path d="M210 163 H420" stroke="#333" stroke-width="10" fill="none"/>'
    s += f'<path d="M210 163 H420" stroke="{WIRE["yellow"]}" stroke-width="6" fill="none"/>'
    s += '<text x="315" y="150" class="small">the washer wire</text>'
    # cube and washers
    s += '<rect x="420" y="120" width="86" height="86" fill="#B8BCC4" stroke="#444" stroke-width="3"/>'
    s += '<text x="463" y="228" class="htitle" style="text-anchor:middle">cube</text>'
    for i, x in enumerate((540, 590, 640, 690)):
        s += f'<ellipse cx="{x}" cy="190" rx="20" ry="8" fill="#C9CCD2" stroke="#444" stroke-width="2"/>'
    s += '<path d="M506 163 H690 V182" stroke="#333" stroke-width="3" fill="none"/>'
    for x in (540, 590, 640):
        s += f'<path d="M{x} 163 V182" stroke="#333" stroke-width="3"/>'
    s += '<text x="615" y="228" class="htitle" style="text-anchor:middle">4 bed washers</text>'
    # nozzle
    s += '<path d="M440 20 h46 v36 l-14 26 h-18 l-14 -26 z" fill="#C9A227" stroke="#5a4a10" stroke-width="3"/>'
    s += '<text x="300" y="45" class="smallL">nozzle on ground</text>'
    s += '<path d="M486 34 H800 V290 H125 V262" stroke="#1A1A1A" stroke-width="4" fill="none" stroke-dasharray="10 6"/>'
    s += '<text x="460" y="310" class="smallL">the hot end ground wire, back to the RAMBo</text>'
    s += f'<text x="463" y="105" class="callout">touch: S goes to ground</text>'
    return s + "</svg>"


P1 = f"""
<header class="cover">
  <div class="kicker">LulzBot TAZ Workhorse · RAMBo 1.4 · Klipper</div>
  <h1>The cube wire on Y-MAX</h1>
  <p class="lede">The steel cube at the front right of the bed measures the backlash of X, Y and Z. The nozzle
  touches it, as G425 did in the LulzBot firmware. Its wire was on Z-MIN, and the BLTouch has Z-MIN now. The wire
  goes to the free Y-MAX header.</p>
</header>
{circuit()}
<div class="two">
  <div>
    <h3>How it works</h3>
    <ul class="tight">
      <li>The cube and the four bed washers are on one wire, the washer wire. The RAMBo holds the wire at 5 V.</li>
      <li>The nozzle is on ground. When it touches the cube, the wire goes to ground, and the deck sees a trigger.</li>
      <li><code>CUBE_MEASURE</code> touches the top, the right, the left and the back of the cube. After each touch
      the nozzle backs away in 0.01 mm steps. The distance to the open contact is the play of the axis.</li>
    </ul>
  </div>
  <div>
    <h3>What you need</h3>
    <ul class="tight">
      <li>The washer plug. It came out of Z-MIN when the BLTouch cable went in.</li>
      <li>A multimeter with a continuity beep.</li>
      <li>A brass brush for the nozzle tip.</li>
      <li>If the plug is lost: a Molex SL 3-pin housing (50-57-9403) and one SL socket (16-02-0086), as on the
      other endstop plugs.</li>
    </ul>
    <h3>Why</h3>
    <p>The LulzBot firmware had backlash correction: <code>M425 X0.25 Y0.25 Z0.04</code>. Klipper has none. The
    measure shows if the play is still there after the new belts. Fix play in the belts, the pulleys and the idlers
    first.</p>
  </div>
</div>
"""

P2 = f"""
<h2>Step 1: check the wire</h2>
<ol class="steps">
  <li><b>Turn off the printer</b> and unplug it. Open the control box.</li>
  <li><b>Find the washer plug.</b> It hangs near the ENDSTOPS block of the RAMBo, with one wire on the S position.</li>
  <li><b>Cube.</b> Put one probe of the meter in the S socket of the plug, and the other on the cube. The meter must
  beep. If it does not, the cube has its own wire: follow it from the cube, and tell Claude where it goes.</li>
  <li><b>Washers.</b> Do the same with a bed washer. The meter must beep.</li>
  <li><b>Nozzle.</b> Put one probe on the nozzle, and the other on the − pin of any endstop header. The meter must
  beep. Clean the nozzle tip with the brass brush first: plastic on the tip stops the contact.</li>
</ol>
<h2>Step 2: the plug on Y-MAX</h2>
<div class="two">
  {ZMIN}
  {YMAX}
</div>
<ul>
  <li>ENDSTOPS has two rows of three headers. The top row is X-MIN, Y-MIN, Z-MIN, with <b>+ − S</b> from the left.
  The BLTouch is on Z-MIN.</li>
  <li>The bottom row is X-MAX, <b>Y-MAX</b>, Z-MAX, with <b>S − +</b> from the left. The right Z switch is on X-MAX
  and the left one on Z-MAX.</li>
  <li>The headers of the bottom row turn the other way. Put the latch of the plug on the same side of the header
  as on Z-MIN. Then the wire stays on <b>S</b>.</li>
</ul>
<ol class="steps">
  <li><b>Push the washer plug onto Y-MAX</b>, the middle header of the bottom row, with the wire on S (the left pin).</li>
  <li><b>Check</b> with the meter: S of Y-MAX to the cube beeps. S of Y-MAX to the + pin does not beep.</li>
  <li><b>Close the control box.</b></li>
</ol>
<p class="note">Picture: the top of the RAMBo 1.4a, from the UltiMachine design files (github.com/ultimachine/RAMBo-1.4).</p>
"""

CFG = """[printdeck_cube]
pin: ^!PA1     # Y-MAX
left: 261      # X of the left face
right: 271     # X of the right face
back: -14      # Y of the back face
top: 3         # Z of the top face"""

P3 = f"""
<h2>Step 3: the config and the test</h2>
<div class="row">
  <pre>{CFG}</pre>
  <div>
    <h3>The section</h3>
    <p>The deck update of the backlash PR has the section in <code>printer.cfg</code>. The places come from the
    LulzBot firmware. They are estimates: <code>CUBE_MEASURE</code> reports the places that it finds.</p>
  </div>
</div>
<ol class="steps">
  <li><b>Turn on the printer.</b> Leave the nozzle cold.</li>
  <li>In the Console of the deck, send <code>CUBE_QUERY</code>. It must say <b>cube: open</b>.</li>
  <li><b>Touch the cube to the nozzle</b> with a short wire: one end on the cube, the other end on the cold
  nozzle. Send <code>CUBE_QUERY</code> again. It must say <b>cube: TRIGGERED</b>.</li>
  <li>If the two answers are the other way round, remove the <code>!</code> from the pin, and test again.</li>
</ol>
<h2>Step 4: measure</h2>
<div class="warn"><div class="warn-t">Clean nozzle, BLTouch pin up</div>
<p>A plastic film on the tip stops the contact, and the nozzle then presses into the cube. Each touch stops 1 mm
past the place in the config (0.5 mm on the top), so a failed contact cannot press farther.</p></div>
<ol class="steps">
  <li><b>Clean the nozzle tip</b> with the brass brush, warm (150 °C for PLA). Then wait for 100 °C or less.</li>
  <li><b>Home:</b> <code>G28</code>. Check that the BLTouch pin is up.</li>
  <li><b>Measure:</b> <code>CUBE_MEASURE</code>. The nozzle goes to the cube at the front right, touches the top,
  then the right, the left and the back faces. It takes a few minutes.</li>
  <li><b>Read the result</b> in the Console, for example:<br>
  <code>Backlash: X 0.240 mm (right 0.250, left 0.230), Y 0.180 mm, Z 0.040 mm</code></li>
</ol>
<div class="next"><div class="next-t">What the numbers mean</div>
<p>Up to about 0.05 mm is good. More than 0.1 mm on X or Y shows play: check the belt tension, the set screws of the
motor pulleys, and the idlers, and measure again. Tell Claude the result. The deck can then correct the rest of the
play, as M425 did in the LulzBot firmware.</p></div>
"""

CSS = f"""
@page {{ size: letter; margin: 0; }}
* {{ box-sizing: border-box; }}
body {{ margin: 0; font-family: "Noto Sans", "Liberation Sans", sans-serif; color: #1b1b1b; font-size: 10.5pt;
  line-height: 1.42; -webkit-print-color-adjust: exact; print-color-adjust: exact; }}
.page {{ width: 8.5in; height: 11in; padding: 0.55in 0.6in 0.7in; position: relative; page-break-after: always;
  overflow: hidden; }}
.page:last-child {{ page-break-after: auto; }}
footer {{ position: absolute; left: 0.6in; right: 0.6in; bottom: 0.35in; display: flex; justify-content: space-between;
  font: 8pt "JetBrainsMono Nerd Font", monospace; color: #888; border-top: 1px solid #ddd; padding-top: 6px; }}
.cover {{ background: {NAVY}; color: #fff; margin: -0.55in -0.6in 18px; padding: 0.55in 0.6in 22px; }}
.kicker {{ font: 600 9pt "JetBrainsMono Nerd Font", monospace; letter-spacing: .12em; color: {ORANGE};
  text-transform: uppercase; }}
h1 {{ font-size: 34pt; margin: 6px 0 6px; letter-spacing: -.01em; }}
.lede {{ font-size: 12pt; margin: 0; color: #DDE2EE; max-width: 6.6in; }}
h2 {{ font-size: 18pt; color: {NAVY}; margin: 0 0 10px; border-bottom: 3px solid {ORANGE}; padding-bottom: 6px; }}
h2 + * {{ margin-top: 0; }}
ol + h2, ul + h2, .warn + h2 {{ margin-top: 18px; }}
h3 {{ font-size: 11.5pt; color: {NAVY}; margin: 12px 0 4px; }}
.warn {{ border-left: 6px solid {ORANGE}; background: #FFF3EA; padding: 10px 14px; margin-bottom: 8px; }}
.warn-t {{ font-weight: 700; color: #B4501A; font-size: 12pt; }}
.warn p {{ margin: 4px 0 0; }}
.diagram {{ width: 100%; height: auto; display: block; margin: 4px 0 0; }}
.diagram text {{ font-family: "JetBrainsMono Nerd Font", monospace; }}
.diagram .mark {{ fill: #333; font-size: 17px; font-weight: 700; text-anchor: middle; }}
.diagram .htitle {{ fill: {NAVY}; font-size: 16px; font-weight: 700; }}
.diagram .callout {{ fill: {ORANGE}; font-size: 15px; font-weight: 700; text-anchor: middle; }}
.diagram .small {{ fill: #666; font-size: 13px; text-anchor: middle; }}
.diagram .smallL {{ fill: #555; font-size: 14px; }}
.diagram .pcb {{ fill: #fff; font-weight: 700; }}
code, pre {{ font-family: "JetBrainsMono Nerd Font", monospace; font-size: 9pt; }}
.two {{ display: grid; grid-template-columns: 1fr 1fr; gap: 22px; }}
.board {{ width: 100%; height: auto; display: block; border-radius: 6px; }}
.row {{ display: grid; grid-template-columns: 1fr 1.2fr; gap: 20px; align-items: start; margin: 4px 0 12px; }}
.row h3 {{ margin-top: 0; }}
.board .bmark {{ font: 700 36px "JetBrainsMono Nerd Font", monospace; fill: #fff; text-anchor: middle;
  paint-order: stroke; stroke: {NAVY}; stroke-width: 9px; }}
.board .blabel {{ font: 700 20px "JetBrainsMono Nerd Font", monospace; fill: #fff; }}
ul, ol {{ margin: 4px 0; padding-left: 18px; }}
li {{ margin: 3px 0; }}
.tight li {{ margin: 1px 0; }}
.note {{ font-size: 8.5pt; color: #777; margin-top: 14px; }}
ol.steps {{ counter-reset: s; list-style: none; padding-left: 0; }}
ol.steps > li {{ counter-increment: s; position: relative; padding-left: 34px; margin: 7px 0; }}
ol.steps > li::before {{ content: counter(s); position: absolute; left: 0; top: 0; width: 24px; height: 24px;
  border-radius: 50%; background: {NAVY}; color: #fff; font: 700 10pt "JetBrainsMono Nerd Font", monospace;
  display: flex; align-items: center; justify-content: center; }}
.next {{ background: #EEF1F7; border-radius: 8px; padding: 12px 16px; margin-top: 16px; }}
.next-t {{ font-weight: 700; color: {NAVY}; font-size: 12pt; }}
.next p {{ margin: 4px 0 8px; }}
pre {{ background: {NAVY}; color: #E9ECF4; padding: 10px 14px; border-radius: 6px; margin: 0; }}
"""


def page(body, n):
    return (f'<section class="page">{body}<footer><span>TAZ Workhorse · The cube wire on Y-MAX</span>'
            f"<span>{n} / {PAGES}</span></footer></section>")


def main():
    html = (f'<!doctype html><html><head><meta charset="utf-8"><title>TAZ Workhorse cube wire</title>'
            f"<style>{CSS}</style></head><body>" + page(P1, 1) + page(P2, 2) + page(P3, 3) + "</body></html>")
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "guide.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--no-pdf-header-footer", f"--print-to-pdf={OUT}", src.as_uri()],
                       check=True, capture_output=True)
    print(OUT)


if __name__ == "__main__":
    main()
