#!/usr/bin/env python3
"""Write build.html, the source of the build instructions PDF."""

import json
from pathlib import Path

FILAMENT = json.loads(Path(__file__).with_name("filament.json").read_text())

CSS = """
  @page { size: A4; margin: 0; }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: "Helvetica Neue", Helvetica, Arial, "Liberation Sans", sans-serif; color: #111; }
  .page { width: 210mm; height: 297mm; padding: 16mm 16mm 14mm; page-break-after: always; position: relative; overflow: hidden; }
  .page:last-child { page-break-after: auto; }
  h1 { font-size: 54pt; letter-spacing: 2pt; margin: 0; font-weight: 800; }
  h2 { font-size: 20pt; margin: 0 0 6mm; font-weight: 700; }
  .sub { font-size: 14pt; margin-top: 2mm; }
  .num { font-size: 84pt; font-weight: 800; line-height: 1; }
  .head { display: flex; align-items: flex-end; gap: 8mm; border-bottom: 1.2mm solid #111; padding-bottom: 4mm; }
  .head .title { font-size: 20pt; font-weight: 700; padding-bottom: 3mm; }
  .fig { width: 100%; margin: 4mm 0; }
  .fig img { max-width: 100%; max-height: 132mm; display: block; margin: 0 auto; }
  .need { display: flex; flex-wrap: wrap; gap: 3mm; margin: 4mm 0; }
  .chip { border: 0.6mm solid #111; border-radius: 3mm; padding: 2mm 4mm; font-size: 12pt; font-weight: 700; }
  .chip span { font-weight: 400; }
  ol, ul { font-size: 12.5pt; line-height: 1.5; padding-left: 6mm; margin: 2mm 0; }
  p { font-size: 12.5pt; line-height: 1.5; margin: 2mm 0; }
  .warn { border: 0.8mm solid #E4572E; border-radius: 3mm; padding: 3mm 5mm; margin-top: 4mm; font-size: 12pt; }
  .warn b { color: #E4572E; }
  table { border-collapse: collapse; width: 100%; font-size: 11.5pt; }
  td { border-bottom: 0.2mm solid #999; padding: 1.5mm 2mm; vertical-align: middle; }
  td.q { font-weight: 800; font-size: 14pt; width: 14mm; }
  .parts { display: grid; grid-template-columns: repeat(7, 1fr); gap: 2mm; margin-bottom: 5mm; }
  .part { border: 0.4mm solid #111; border-radius: 3mm; padding: 2mm; text-align: center; font-size: 9pt; }
  .part img { width: 100%; height: 18mm; object-fit: contain; }
  .part b { display: block; font-size: 14pt; }
  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 8mm; }
  .cols table { font-size: 10pt; }
  .cols td { padding: 0.8mm 2mm; }
  .cols td.q { font-size: 11.5pt; }
  .cols h2 { font-size: 15pt; margin-bottom: 2mm; }
  .stl td, .stl th { font-size: 10pt; padding: 1mm 2mm; }
  .stl th { text-align: left; border-bottom: 0.6mm solid #111; }
  .stl td.n, .stl th.n { text-align: right; }
  .stl img { height: 11mm; width: 14mm; object-fit: contain; display: block; }
  .stl tr.total td { font-weight: 800; border-top: 0.6mm solid #111; border-bottom: none; }
  code { font-size: 0.92em; }
  .files { font-size: 11pt; margin: 0 0 2mm; color: #333; }
  .foot { position: absolute; bottom: 8mm; right: 16mm; font-size: 10pt; color: #555; }
  svg text { font-family: inherit; }
"""

HARDWARE = [
    (16, "M3 heat-set insert, 4 mm long"),
    (12, "M3 × 6 bolt"),
    (4, "M3 × 6 countersunk bolt"),
    (4, "M3 × 8 countersunk bolt"),
    (4, "M3 × 16 bolt and M3 nut"),
    (4, "Speaker binding post (2 red, 2 black)"),
    (1, "DC panel jack, 5.5 × 2.1 mm"),
    (4, "Rubber foot, self-adhesive"),
]

ELECTRONICS = [
    (1, "Raspberry Pi 5 + adapter + HiFiBerry Amp2, as one stack"),
    (1, "Inky wHAT with a 40-pin ribbon cable, 200 mm"),
    (1, "Adafruit 7 inch HDMI display with touch"),
    (1, "2.5 inch SATA drive with a USB adapter board"),
    (1, "30 mm fan"),
    (1, "Micro HDMI to HDMI cable, right-angle HDMI plug"),
    (1, "USB-A to micro USB cable, right-angle micro USB plug"),
    (1, "Short USB cable for the drive"),
]

WIRING = """<div class="fig">
    <svg viewBox="0 0 700 430" xmlns="http://www.w3.org/2000/svg" font-size="13">
      <g fill="#fff" stroke="#111" stroke-width="2">
        <rect x="250" y="40" width="200" height="200" rx="8" fill="#f1f1ee"/>
        <rect x="270" y="56" width="160" height="34" rx="4"/>
        <rect x="270" y="100" width="160" height="40" rx="4"/>
        <rect x="270" y="150" width="160" height="76" rx="4"/>
        <rect x="30" y="92" width="150" height="62" rx="4"/>
        <rect x="30" y="260" width="150" height="44" rx="4"/>
        <rect x="530" y="92" width="150" height="56" rx="4"/>
        <rect x="530" y="190" width="150" height="44" rx="4"/>
        <rect x="530" y="270" width="150" height="44" rx="4"/>
        <rect x="530" y="350" width="150" height="44" rx="4"/>
      </g>
      <g fill="none" stroke="#E4572E" stroke-width="2.5">
        <path d="M180 108 H228 V166 H270"/>
        <path d="M180 138 H210 V186 H270"/>
        <path d="M180 282 H244 V212 H270"/>
        <path d="M430 120 H530"/>
        <path d="M530 212 H485 V132 H430"/>
        <path d="M530 292 H470 V180 H430"/>
        <path d="M530 372 H452 V206 H430"/>
      </g>
      <g font-weight="700" text-anchor="middle">
        <text x="350" y="78">Inky wHAT</text>
        <text x="350" y="125">HiFiBerry Amp</text>
        <text x="350" y="193">Raspberry Pi 5</text>
        <text x="105" y="119">7 inch display</text>
        <text x="105" y="287">Drive</text>
        <text x="605" y="116">Binding posts</text>
        <text x="605" y="217">DC jack</text>
        <text x="605" y="297">Fan</text>
        <text x="605" y="377">3D printer</text>
      </g>
      <g font-size="11" fill="#333">
        <text x="105" y="138" text-anchor="middle">driver and touch on the board</text>
        <text x="605" y="134" text-anchor="middle">L+  L−  R−  R+</text>
        <text x="186" y="103">HDMI</text>
        <text x="100" y="172">USB: power and touch</text>
        <text x="186" y="277">USB 3</text>
        <text x="444" y="114">4 wires</text>
        <text x="490" y="228">power</text>
        <text x="476" y="308">fan plug</text>
        <text x="458" y="388">USB, slot</text>
      </g>
    </svg>
  </div>"""

INS = "M3 insert"
B6 = "M3 × 6"

# (title, STL files for the step, parts for the step, image file or HTML, instructions, note, warning)
STEPS = [
    ("Inserts in the body", ["body"], [(8, INS)], "step1.png", [
        "Put the body upside down. The picture shows the view from below.",
        "Push 4 inserts into the 4 bosses around the display opening.",
        "Push 4 inserts into the 4 corner posts.",
        "Let the body become cool.",
    ], "", None),
    ("Inserts in the frames", ["frame", "inky_frame"], [(8, INS)], "step2.png", [
        "Push 4 inserts into the 4 bosses on the rear face of the display frame.",
        "Push 4 inserts into the 4 bosses on the rear face of the Inky frame.",
    ], "", None),
    ("Parts on the rear wall", ["body"], [(4, "M3 × 16 + nut"), (4, "Binding post"), (1, "DC jack")], "step3.png", [
        "Attach the fan to the inner side, behind the round hole pattern. "
        "The label of the fan points to the wall, so the air goes out of the case.",
        "Install the 4 binding posts. Put red on the inner holes and black on the outer holes.",
        "Install the DC jack in the round hole near the center.",
    ], "", None),
    ("Display frame on the body", ["body", "frame", "cap"], [(4, "Cap"), (4, B6)], "step4.png", [
        "Put the display frame into the indent in the angled front.",
        "The 4 bosses of the frame go through the 4 holes of the body.",
        "Put a cap on each boss from the inner side.",
        "Install a bolt through each cap.",
    ], "", None),
    ("Display into the body", ["body"], [(4, B6)], "step5.png", [
        "Connect the right-angle HDMI plug and the right-angle USB plug to the display.",
        "Put the display into the body from below. The glass points to the front, and the sockets point down.",
        "Put the LCD module into the opening. The board sits on the 4 bosses.",
        "Install 4 bolts through the 4 holes of the display board.",
    ], "The display glass is now 1 mm behind the front of the frame.",
     ("Caution.", "Tighten the bolts lightly. Too much force can bend the board.")),
    ("Inky wHAT in the lower front", ["inky_frame", "pad", "cap"], [(1, "Pad"), (4, "Cap"), (4, B6)], "step6.png", [
        "Put the pad into the cavity in the lower front.",
        "Connect the ribbon cable to the header of the Inky wHAT.",
        "Put the ribbon cable through the opening in the cavity.",
        "Put the Inky wHAT on the pad. The wide border is at the bottom.",
        "Put the Inky frame into the indent. The frame pushes the Inky wHAT on the pad.",
        "Put a cap on each boss from the inner side, and install a bolt through each cap.",
    ], "", ("Caution.", "Do not push on the e-paper. The glass is thin.")),
    ("Drive on the base plate", ["base"], [(4, "M3 × 6 countersunk")], "step7.png", [
        "Put the drive on the 4 pads of the base plate. The label points down, and the adapter board points to the rear.",
        "Install 4 countersunk bolts from below, through the base plate, into the 4 holes in the bottom of the drive.",
    ], "", ("Caution.", "Use M3 × 6 bolts or shorter. A longer bolt can damage the drive.")),
    ("Pi stack on the base plate", ["base"], [], "step8.png", [
        "Put the Pi stack on the 4 pins of the base plate.",
        "The USB ports point to the rear. The HDMI ports point to the drive.",
    ], "The pins and the weight of the stack hold it in position. Keep the case upright.", None),
    ("Cables", [], [], WIRING, [
        "Put the body on its side, adjacent to the base plate.",
        "Connect the HDMI cable of the display to a micro HDMI port of the Pi.",
        "Connect the USB cable of the display to a USB port of the Pi.",
        "Connect the drive to a blue USB port of the Pi.",
        "Connect the ribbon cable of the Inky wHAT to the header of the stack.",
        "Connect the binding posts to the speaker terminals of the Amp2. Keep the polarity correct.",
        "Connect the DC jack to the power input of the Amp2.",
        "Connect the fan to the fan connector of the Pi.",
    ], "", ("Warning.", "The Amp2 supplies power to the Pi. Do not connect a USB-C power supply at the same time.")),
    ("Close the case", ["body", "base"], [(4, "M3 × 8 countersunk")], "step10.png", [
        "Put the USB cable of the printer in the notch at the bottom of the rear wall.",
        "Put the body on the base plate. Make sure that no cable is between the two parts.",
        "Install 4 countersunk bolts from below, into the 4 corner posts.",
    ], "", None),
    ("Complete", [], [(4, "Rubber foot")], "step11.png", [
        "Attach the 4 rubber feet to the bottom.",
        "Put the unit between the speakers.",
        "Connect the speaker cables to the binding posts.",
        "Connect the 12 to 24 V power supply.",
    ], "", None),
]


def rows(items):
    return "".join(f'<tr><td class="q">{q}</td><td>{t}</td></tr>' for q, t in items)


def cover():
    return f"""<section class="page">
  <h1>SPEAKERDECK</h1>
  <div class="sub">Build instructions</div>
  <div class="fig" style="margin-top:30mm"><img src="img/step{len(STEPS)}.png"></div>
  <p style="margin-top:14mm">A Spotify player and OctoPrint host with the size of one Denon SC-N10 speaker.</p>
  <p>153 mm wide. 233 mm high. 196 mm deep.</p>
</section>
"""


def parts_page():
    s = FILAMENT["settings"]
    stl_rows = "".join(
        f'<tr><td><img src="img/part_{p["name"]}.png"></td><td>{p["label"]}</td><td><code>{p["file"]}</code></td>'
        f'<td class="n">{p["quantity"]}</td><td class="n">{p["size"][0]:.0f} × {p["size"][1]:.0f} × {p["size"][2]:.0f}</td>'
        f'<td class="n">{p["grams"]:.0f} g</td><td class="n">{p["metres"]:.1f} m</td></tr>'
        for p in FILAMENT["parts"]
    )
    return f"""<section class="page">
  <h2>Printed parts</h2>
  <table class="stl">
    <tr><th></th><th>Part</th><th>File</th><th class="n">Quantity</th><th class="n">Size on the bed (mm)</th><th class="n">Filament</th><th class="n">Length</th></tr>
    {stl_rows}
    <tr class="total"><td></td><td>Total</td><td></td><td></td><td></td><td class="n">{FILAMENT["total_grams"]} g</td><td class="n">{FILAMENT["total_metres"]:.0f} m</td></tr>
  </table>
  <p style="font-size:10pt">The filament values are estimates for {s["material"]}, {s["filament_d"]} mm filament, {s["shell"]} mm walls, and {s["infill"] * 100:.0f} % infill. The values for a part with a quantity include all pieces. A slicer gives the correct values for your settings.</p>
  <ul style="font-size:11pt">
    <li>The STL files are in the <code>case/stl/</code> directory. Each file has the correct position for the print bed.</li>
    <li>Print the body upside down, with the top on the bed. Use supports for the display opening and the Inky cavity.</li>
    <li>Print the two frames in a dark color.</li>
    <li>Print the pad in TPU, or use the file as a pattern and cut the pad from 2 mm rubber or foam.</li>
  </ul>
  <div class="cols" style="margin-top:4mm">
    <div><h2>Hardware</h2><table>{rows(HARDWARE)}</table></div>
    <div><h2>Electronics</h2><table>{rows(ELECTRONICS)}</table></div>
  </div>
  <p style="margin-top:4mm"><b>Tools:</b> soldering iron with an insert tip, hex keys for M3, small flat screwdriver, calipers.</p>
  <div class="warn"><b>Measure first.</b> Compare your Inky wHAT (thickness and header position) and the height of your Pi stack with the values in <code>speakerdeck.scad</code>. Correct the values, then run <code>build/make.sh</code>.</div>
  <div class="foot">2</div>
</section>
"""


def step_page(number, title, files, need, image, items, note, warning):
    chips = "".join(f'<div class="chip">{q} × <span>{t}</span></div>' for q, t in need)
    figure = f'<div class="fig"><img src="img/{image}"></div>' if image.endswith(".png") else image
    warn = f'<div class="warn"><b>{warning[0]}</b> {warning[1]}</div>' if warning else ""
    return f"""<section class="page">
  <div class="head"><div class="num">{number}</div><div class="title">{title}</div></div>
  {f'<div class="need">{chips}</div>' if need else ''}
  {'<div class="files">Printed parts: ' + ", ".join(f"<code>stl/{name}.stl</code>" for name in files) + "</div>" if files else ''}
  {figure}
  <ol>{''.join(f'<li>{item}</li>' for item in items)}</ol>
  {f'<p>{note}</p>' if note else ''}
  {warn}
  <div class="foot">{number + 2}</div>
</section>
"""


def main():
    pages = cover() + parts_page() + "".join(step_page(i + 1, *step) for i, step in enumerate(STEPS))
    html = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Speakerdeck build instructions</title>
<style>{CSS}</style>
</head>
<body>
{pages}
</body>
</html>
"""
    Path(__file__).with_name("build.html").write_text(html)


if __name__ == "__main__":
    main()
