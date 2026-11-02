#!/usr/bin/env python3
"""Printdeck Slate v4: write the build guide PDF (printdeck-slate-v4-build.pdf).

Run after `slate.py render`, which writes the pictures in renders/. Needs Chromium. The text is the
text of stl/slate/README.md.

  python3 make_slate_pdf.py
"""

import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
RENDERS = HERE / "renders"
OUT = HERE / "printdeck-slate-v4-build.pdf"

NAVY, ORANGE = "#1F2B4D", "#E8742A"

# File, colour, notes. The masses and the times are from stl/slate/README.md.
RESIN = [
    ("slate-case_resin.stl", "Navy", "54 ml before the supports. Supports on the open front: the rim and the inside. "
                                     "Keep the back, the side walls and the ports clean."),
    ("slate-base_resin.stl", "Orange", "53 ml before the supports. Supports on the bottom of the foot. Keep the lip, "
                                       "the cheeks, the tops of the ribs, and the boss and the pad clean."),
]
FDM = [
    ("slate-frame_fdm.stl", "Navy", "On its back ring. 30.1 g, 2 h 24 min."),
    ("slate-body_fdm.stl", "Navy", "On its back. 38.6 g, 3 h 24 min."),
    ("slate-base_fdm.stl", "Orange", "On the bottom of the foot, standing up (113 mm high). 51.5 g, 4 h 38 min."),
]

RESIN_SETTINGS = [
    ("Printer", "Peopoly Phenom, <code>abs-like-0.05</code> profile. Both parts fit on the plate."),
    ("Orientation", "Each file is in its print orientation. The case is tilted 15° two ways, open front toward the "
                    "plate. The base has its back plate vertical. Both stand on supports, 6 mm over the plate."),
    ("Drain holes", "None. The case is open at the front, and the base has no closed space."),
    ("Colour", "Navy resin for the case and orange resin for the base, or any resin and paint."),
]
FDM_SETTINGS = [
    ("Printer", "LulzBot TAZ Workhorse, <code>pla-0.20-standard</code> profile, 0.5 mm nozzle."),
    ("Supports", "Off. No part needs supports."),
    ("Z seam", "Z Seam Alignment: Sharpest Corner. Seam Corner Preference: Hide Seam."),
    ("Brim", "5 mm (10 lines) on each part, so that the corners stay down on the bed."),
]

HARDWARE = [
    (8, "M2.5 × 4 screw", "In the box of the display. 4 hold the Pi on the standoffs, 4 hold the display in the case."),
    (2, "M3 × 8 cap screw", "From behind, through the boss of the base and the back of the body, into the nuts."),
    (2, "M3 hex nut", "5.5 mm across the flats, 2.4 mm thick. Pressed into the two rings in the body."),
    (4, "M3 × 6 cap screw", "FDM only. Down the posts of the body into the frame."),
    (4, "M3 heat-set insert, 4 mm long", "FDM only. In the bosses of the frame, flush with the face."),
    (4, "Rubber foot, 10 mm across or less", "Under the corners of the foot."),
]

ELECTRONICS = [
    (1, "Raspberry Pi Touch Display 2, 5 inch. Its box has the DSI ribbon, the power wire and the M2.5 screws."),
    (1, "Raspberry Pi 5 with the Raspberry Pi Active Cooler."),
    (1, "microSD card for Printdeck OS."),
    (1, "Raspberry Pi 27 W USB-C power supply."),
    (1, "USB-A to USB-B cable, from the Pi to the printer."),
    (2, "Twist ties, to hold the printer cable on the back plate."),
]

TOOLS = [
    "A screwdriver for the M2.5 screws, and a short one for the M3 × 6 screws.",
    "A 2.5 mm hex key for the M3 cap screws.",
    "FDM only: a soldering iron for the heat-set inserts.",
]

# Picture, title, the parts of the step, the actions, a note. "Resin" or "FDM" in the title marks a
# step for one process.
STEPS = [
    ("step-nuts", "Nuts", ["body or resin case", "2 × M3 nut"], [
        "Press the two M3 nuts into the two rings on the inner floor of the body, from the front.",
        "Press each nut flat into its hexagon pocket.",
    ], "On the resin case, the rings are in the same place, inside the case."),
    ("step-inserts", "Inserts (FDM)", ["frame", "4 × M3 insert"], [
        "Press the four heat-set inserts into the four bosses on the back of the frame.",
        "Stop when each insert is flush with the face.",
    ], None),
    ("step-pi", "Pi on the display", ["display", "Pi 5 with Active Cooler", "4 × M2.5 × 4"], [
        "Put the Pi 5 with its Active Cooler on the four standoffs of the display. The USB ports point toward the "
        "top of the display.",
        "Attach the Pi with four M2.5 × 4 screws.",
        "Connect the DSI ribbon and the power wire.",
    ], "Put the microSD card in the Pi before you attach it. In the case, the slot is between the Pi and the display."),
    ("step-resin-case", "Display into the case (resin)", ["resin case (step 1)", "display (step 3)", "4 × M2.5 × 4"], [
        "Slide the display with the Pi into the case from the front.",
        "Put four M2.5 × 4 screws through the back ring into the tabs of the display.",
        "Put the two upper screws in from behind, above the body.",
        "Put the two lower screws down the two wells in the back of the case.",
    ], None),
    ("step-fdm-case", "Frame and body (FDM)", ["frame (step 2)", "body (step 1)", "4 × M2.5 × 4", "4 × M3 × 6"], [
        "Put the frame on the display. Attach it with four M2.5 × 4 screws into the tabs.",
        "Put the body on the frame.",
        "Put the four M3 × 6 screws down the posts of the body into the inserts. Tighten them.",
    ], "Assemble the case before it goes on the base. On the base, the foot is in the way of the two lower posts."),
    ("step-base", "Case on the base", ["case", "base"], [
        "Put the case on the base from above.",
        "Its bottom goes on the ribs, between the cheeks and behind the lip.",
        "The back of the body goes on the boss and the pad.",
    ], None),
    ("step-screws", "Back screws", ["2 × M3 × 8"], [
        "Put the two M3 × 8 screws in from behind, through the boss and the back of the body, into the nuts.",
        "Tighten them. The screws pull the body onto the boss and the pad.",
    ], None),
    ("step-feet", "Feet", ["4 × rubber foot"], [
        "Stick the four rubber feet under the corners of the foot.",
    ], "With rubber feet, a push on the screen slides the Slate before it tips."),
]

CSS = """
  @page { size: A4; margin: 0; }
  * { box-sizing: border-box; }
  :root { --ink: #16181a; --ink-2: #4a4f55; --accent: #E8742A; --bg: #fbfbf9; --grid: #ececE6; --line: #e2e2dc;
          --mono: "JetBrains Mono", "JetBrainsMono Nerd Font", ui-monospace, monospace; }
  body { margin: 0; font-family: "Inter", "Helvetica Neue", Helvetica, Arial, "Liberation Sans", sans-serif; color: var(--ink); }
  .page { width: 210mm; height: 297mm; padding: 16mm 16mm 14mm; page-break-after: always; position: relative; overflow: hidden; }
  .page:last-child { page-break-after: auto; }
  h1 { font-size: 50pt; letter-spacing: 2pt; margin: 0; font-weight: 800; }
  h2 { font-size: 20pt; margin: 0 0 4mm; font-weight: 700; }
  h3 { font-size: 13pt; margin: 4mm 0 2mm; font-weight: 700; }
  .sub { font-size: 24pt; font-weight: 700; margin-top: 2mm; color: #1F2B4D; }
  .num { font: 800 72pt/1 var(--mono); color: var(--accent); }
  .head { display: flex; align-items: flex-end; gap: 8mm; border-bottom: 0.6mm solid var(--ink); padding-bottom: 4mm; }
  .head .title { font-size: 22pt; font-weight: 700; padding-bottom: 3mm; }
  .fig { width: 100%; margin: 4mm 0; padding: 4mm; text-align: center; border: 0.3mm solid var(--line); border-radius: 3mm;
         background-color: var(--bg); background-size: 6mm 6mm;
         background-image: linear-gradient(var(--grid) 0.15mm, transparent 0.15mm), linear-gradient(90deg, var(--grid) 0.15mm, transparent 0.15mm); }
  .fig img { max-width: 100%; max-height: 125mm; vertical-align: middle; }
  .need { display: flex; flex-wrap: wrap; gap: 3mm; margin: 4mm 0; }
  .chip { border: 0.4mm solid var(--ink); border-radius: 999px; padding: 1.5mm 4mm; font: 600 10pt var(--mono); }
  ol, ul { font-size: 12pt; line-height: 1.5; padding-left: 6mm; margin: 2mm 0; }
  p { font-size: 12pt; line-height: 1.5; margin: 2mm 0; }
  .note { border: 0.4mm solid var(--accent); border-radius: 3mm; padding: 3mm 5mm; margin-top: 4mm; font-size: 11.5pt; background: #fdf0e6; }
  .note b { color: var(--accent); }
  table { border-collapse: collapse; width: 100%; font-size: 10pt; }
  td, th { border-bottom: 0.2mm solid #999; padding: 1.4mm 2mm; vertical-align: top; text-align: left; }
  th { border-bottom: 0.6mm solid #111; }
  td.q { font-weight: 800; font-size: 12pt; width: 12mm; }
  td.k { font-weight: 700; width: 30mm; }
  .sw { display: inline-block; width: 3.5mm; height: 3.5mm; border-radius: 1mm; vertical-align: -0.5mm; margin-right: 1.5mm; }
  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 8mm; }
  code { font-size: 0.92em; }
  .foot { position: absolute; bottom: 8mm; left: 16mm; right: 16mm; font-size: 9.5pt; color: #555; display: flex; justify-content: space-between; }
"""

SWATCH = {"Navy": NAVY, "Orange": ORANGE}


def img(name):
    return (RENDERS / f"slate-{name}.png").as_uri()


def foot(n):
    return f'<div class="foot"><span>Printdeck Slate v4 · build guide</span><span>{n}</span></div>'


def cover():
    return f"""<section class="page">
  <h1>PRINTDECK</h1><div class="sub">Slate v4</div>
  <p style="margin-top:4mm;max-width:165mm">The desk case of Printdeck. It holds the Raspberry Pi Touch Display 2
  (5 inch, tall) and the Raspberry Pi 5 with its Active Cooler. It stands on the desk next to the printer, and leans
  back 15°. The navy case is the top, and the orange base is the bottom. Two M3 screws hold the case on the base.</p>
  <div class="fig"><img src="{img('front')}" style="max-height:120mm;max-width:49%"><img src="{img('back')}" style="max-height:120mm;max-width:49%"></div>
  <div class="need"><div class="chip"><span class="sw" style="background:{NAVY}"></span>Navy case</div>
    <div class="chip"><span class="sw" style="background:{ORANGE}"></span>Orange base</div>
    <div class="chip">Resin: 2 parts · 107 ml</div><div class="chip">FDM: 3 parts · 120 g</div>
    <div class="chip">No supports for FDM</div></div>
  {foot(1)}
</section>"""


def parts_page():
    def rows(items):
        return "".join(f'<tr><td><code>{f}</code></td><td><span class="sw" style="background:{SWATCH[c]}"></span>{c}</td>'
                       f'<td>{t}</td></tr>' for f, c, t in items)
    hw = "".join(f'<tr><td class="q">{q}×</td><td><b>{n}</b><br>{t}</td></tr>' for q, n, t in HARDWARE)
    el = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in ELECTRONICS)
    tools = "".join(f"<li>{t}</li>" for t in TOOLS)
    return f"""<section class="page">
  <h2>Printed parts</h2>
  <p>Print in resin first: the case is one part. The FDM case is two parts, the frame and the body, held with four
  M3 screws into heat-set inserts.</p>
  <h3>Resin</h3>
  <table><tr><th>File</th><th>Colour</th><th>Notes</th></tr>{rows(RESIN)}</table>
  <h3>FDM</h3>
  <table><tr><th>File</th><th>Colour</th><th>Notes</th></tr>{rows(FDM)}</table>
  <div class="cols" style="margin-top:6mm">
    <div><h2>Hardware</h2><table>{hw}</table></div>
    <div><h2>Electronics</h2><table>{el}</table>
      <h2 style="margin-top:5mm">Tools</h2><ul style="font-size:10.5pt">{tools}</ul></div>
  </div>
  {foot(2)}
</section>"""


def print_page():
    def table(items):
        return "<table>" + "".join(f'<tr><td class="k">{k}</td><td>{v}</td></tr>' for k, v in items) + "</table>"
    return f"""<section class="page">
  <h2>Print</h2>
  <p>The STL files are in their print orientation. Load them, do not turn them. The files are at
  <code>beetlebug.org/printdeck/hardware/print/</code>.</p>
  <div class="cols">
    <div><h3>Resin</h3>{table(RESIN_SETTINGS)}</div>
    <div><h3>FDM</h3>{table(FDM_SETTINGS)}</div>
  </div>
  <div class="fig"><img src="{img('side')}" style="max-height:110mm"></div>
  <p style="font-size:10.5pt;color:#444">The Slate from the left: the power plug in the left wall, above the cheek.
  The FDM masses and times are from a CuraEngine slice of the TAZ Workhorse SE profile.</p>
  {foot(3)}
</section>"""


def step_page(i, step, n):
    shot, title, need, items, note = step
    chips = "".join(f'<div class="chip">{x}</div>' for x in need)
    lis = "".join(f"<li>{t}</li>" for t in items)
    nt = f'<div class="note"><b>Note.</b> {note}</div>' if note else ""
    return f"""<section class="page">
  <div class="head"><div class="num">{i}</div><div class="title">{title}</div></div>
  <div class="need">{chips}</div>
  <div class="fig"><img src="{img(shot)}"></div>
  <ol>{lis}</ol>{nt}
  {foot(n)}
</section>"""


def pod_body_page(n):
    return f"""<section class="page">
  <h2>A pod body on the base</h2>
  <p>A pod body that is printed already has no nut rings. It also goes on the base.</p>
  <div class="fig"><img src="{img('pod-body')}" style="max-height:105mm"></div>
  <ol>
    <li>Put the body on the base, with no frame and no display.</li>
    <li>Put each nut on the inner floor of the body, at the hole of the old mount. Hold it, and turn the M3 × 8
      screw in from behind. Keep the two screws loose.</li>
    <li>Put the display and the frame on the body. The body moves a little on the loose screws, so the posts line
      up with the frame.</li>
    <li>Put in the four M3 × 6 screws of the frame. Use a short screwdriver for the two lower screws, or put the back
      of the foot over the edge of a table.</li>
    <li>Tighten the two M3 × 8 screws.</li>
  </ol>
  <div class="note"><b>Why loose.</b> The base holds the frame at the glass, and the screws hold the body. With tight
  screws, the frame screws do not line up with the posts.</div>
  {foot(n)}
</section>"""


def end_page(n):
    return f"""<section class="page">
  <h2>Cables</h2>
  <ol>
    <li>Plug the power cable into the left wall, above the cheek. It goes down outside the base.</li>
    <li>Plug the printer cable into a USB port in the top wall. Run it back over the top of the case and down the
      back plate. Hold it with two twist ties through the four holes in the back plate.</li>
  </ol>
  <div class="cols"><div class="fig"><img src="{img('exploded')}" style="max-height:85mm"></div>
    <div class="fig"><img src="{img('section')}" style="max-height:85mm"></div></div>
  <h2 style="margin-top:4mm">The air</h2>
  <p>The Active Cooler gets its air through the vents in the back and the right wall of the body, and through the
  opening around the ports in the top wall. The back plate is 5 mm behind the body. Keep the back open.</p>
  <h2 style="margin-top:4mm">To take the display out</h2>
  <ol>
    <li>Remove the two M3 × 8 screws from behind, and lift the case off the base.</li>
    <li>Do the steps in the opposite order. To change the microSD card, take the display out of the case.</li>
  </ol>
  {foot(n)}
</section>"""


def main():
    pages = [cover(), parts_page(), print_page()]
    for i, s in enumerate(STEPS):
        pages.append(step_page(i + 1, s, len(pages) + 1))
    pages.append(pod_body_page(len(pages) + 1))
    pages.append(end_page(len(pages) + 1))
    html = (f'<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Printdeck Slate v4 build guide</title>'
            f'<style>{CSS}</style></head><body>{"".join(pages)}</body></html>')
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "slate-v4-build.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--disable-gpu", "--no-pdf-header-footer", "--allow-file-access-from-files",
                        f"--print-to-pdf={OUT}", src.as_uri()], check=True, capture_output=True)
    print("wrote", OUT)


if __name__ == "__main__":
    main()
