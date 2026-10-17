#!/usr/bin/env python3
"""Speakerdeck Slate: write the build instructions PDF (speakerdeck-slate-build.pdf).
Run after model.py, cura/make_profiles.py, and render.py. Needs ImageMagick and Chromium."""

import json
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
BUILD = HERE / "build"
IMG = BUILD / "img"
WEB = BUILD / "web"
OUT = HERE / "speakerdeck-slate-build.pdf"

SUMMARY = json.loads((BUILD / "parts.json").read_text())
FILAMENT = json.loads((BUILD / "filament.json").read_text())
SUPPORTS = json.loads((BUILD / "supports.json").read_text())
HDMI_MM = SUMMARY["cables"]["hdmi"]
USB_MM = SUMMARY["cables"]["usb"]
SSD_MM = SUMMARY["cables"]["ssd"]
PRICE_PER_KG = 22.99                 # USD, PLA


def cost(grams):
    return grams / 1000 * PRICE_PER_KG

# Name, colour, print notes. The order is the order of the parts list.
PRINTS = [
    ("slab", "Navy", "Front face down. The screen opening is in the first layers: use a smooth sheet for a clean face."),
    ("chin", "Orange", "Front face down. The tongues stand up at the two sides."),
    ("back", "Orange", "Front face (the drive side) down. The bosses and the fan posts point up."),
    ("foot", "Gray", "Flat, the four hooks up."),
]

SETTINGS = [
    ("Profiles", "One Cura profile for each part: run <code>python3 cura/make_profiles.py --install</code> with Cura "
                 "closed, then pick “Speakerdeck - Slate …”."),
    ("Material", "PLA, 2.85 mm, 0.5 mm nozzle"),
    ("Slab, chin", "0.3 mm layers, 1.5 mm walls, 1.2 mm top and bottom, 20 % grid infill, skirt"),
    ("Chassis", "0.3 mm layers, 30 % grid infill, skirt"),
    ("Back plate", "0.3 mm layers, 40 % grid infill, skirt"),
    ("Foot", "0.3 mm layers, 20 % grid infill, skirt"),
    ("Supports", "Off for all parts. Every hook, pocket, and slot has 45° faces. A slice of each part with supports "
                 "on (60°, everywhere) adds 0 g of support."),
]

HARDWARE = [
    (4, "M3 heat-set insert, for a 4.0 mm hole, 4 mm long (display)"),
    (4, "M3 heat-set insert, for a 4.0 mm hole, max. 5.5 mm long (Pi)"),
    (4, "M3 × 6 screw (display)"),
    (4, "M3 × 5 screw (drive)"),
    (4, "M3 × 16 screw, pan head (fan). It cuts its thread in the printed boss."),
    (3, "Twist tie, 15 cm, max. 3 mm wide (orange looks best)"),
]

ELECTRONICS = [
    (1, "Raspberry Pi 5 + HiFiBerry Amp2, as one stack on four M3 × 16 male-female standoffs"),
    (1, "Heatsink for the Pi 5 SoC, max. 16 × 16 × 8 mm, with thermal tape"),
    (1, "Adafruit 7 inch HDMI display with touch"),
    (1, "2.5 inch SATA drive with a USB adapter board"),
    (1, "30 mm fan, 10 mm thick, with a Pi 5 fan plug"),
    (1, f"HDMI ribbon cable kit: HDMI A plug, micro HDMI plug, flat ribbon 40 to 50 cm (the route is "
        f"{HDMI_MM / 10:.0f} cm). A round HDMI cable cannot bend in the 10 mm under the display."),
    (1, f"USB-A to micro USB cable, thin (max. 3.5 mm), 50 to 60 cm (the route is {USB_MM / 10:.0f} cm). "
        "Straight plugs, max. 30 mm long."),
    (1, f"USB 3 cable for the drive: micro-B SuperSpeed to USB-A, 15 to 20 cm (the route is {SSD_MM / 10:.0f} cm). "
        "Straight micro-B plug, max. 22 mm long from the socket. Cable max. 4.5 mm diameter."),
    (1, "Raspberry Pi 27 W USB-C power supply"),
    (1, "12 to 20 V power supply for the Amp2, with a right-angle 5.5 × 2.1 mm plug"),
]

STEPS = [
    ("step1", "Display", ["slab", "display", "4 × insert (4 mm)", "4 × M3 × 6"], [
        "Press four 4 mm M3 heat-set inserts into the four bosses in the slab. Use a soldering iron at 220 °C. "
        "Stop when the insert is flush: the face is only 3.85 mm away.",
        "Put the slab face down on a soft cloth.",
        "Connect the HDMI ribbon cable and the USB cable to the display.",
        "Put the display into the slab: glass to the front, sockets at the bottom. The cables come out of the open bottom of the slab.",
        "Attach the display with four M3 × 6 screws.",
    ], None),
    ("step2", "Drive", ["back plate", "drive", "4 × M3 × 5"], [
        "Put the drive on the flat front of the back plate: adapter up, the M3 holes against the plate. Attach it with four M3 × 5 screws from the rear.",
        "Connect the drive cable to the micro-B socket of the drive adapter. Bend the cable to the rear through the notch in the top edge of the plate.",
    ], None),
    ("step2_pi", "Fan and Pi", ["back plate (step 2)", "fan", "Pi 5 + Amp2", "heatsink", "4 × insert", "4 × M3 × 16"], [
        "Press four M3 heat-set inserts into the four tall bosses on the rear of the back plate.",
        "Put the fan in between the four corner guides on the rear, with the cable toward the right of the case. The label side goes away from the plate: the fan blows onto the Pi.",
        "Attach the fan with four M3 × 16 screws through its corner holes into the four bosses. Turn each screw until the fan touches the boss. Do not tighten more: the thread is in plastic.",
        "Put the heatsink on the SoC of the Pi.",
        "Put the Pi stack on the four bosses: the USB ports up, the USB-C and HDMI ports toward the left of the case. Turn the male ends of the standoffs through the Pi into the inserts.",
        "Lead the fan cable around the right edge of the Pi, above the end of the GPIO header. Connect it to the fan connector of the Pi.",
    ], "Left and right are as seen from the front of the case. The picture shows the rear, so the left of the case is at the right of the picture."),
    ("step3", "Back plate into the slab", ["slab (step 1)", "back plate (step 3)"], [
        "Keep the slab face down. Lead the two display cables through the two notches in the bottom edge of the back plate: the USB cable at the left, the ribbon at the right.",
        "Put the edges of the back plate into the grooves of the slab, at the open bottom. Slide the plate up until it stops at the top wall.",
    ], "The drive is behind the display plugs. If the plate does not slide, look at the plugs first."),
    ("step4", "Chin", ["chin"], [
        "Put the extra length of the display cables into the chin.",
        "Push the chin up onto the slab: its grooves take the bottom of the back plate. Push until the two tongues click.",
    ], "The chin holds the back plate. The case is now closed."),
    ("step5", "Foot", ["foot"], [
        "Put the case on the foot. The foot is 6 mm in from the front and the sides, and it reaches far to the rear.",
        "Push down until the four hooks click into the floor of the chin.",
    ], None),
    ("step6", "Cables on the rear", ["3 × twist tie"], [
        "Lead the drive cable over the top of the Pi. Connect it to the blue USB 3 port next to the board, from above.",
        "Lead the HDMI ribbon to the left, then up the left side of the back plate. Tie it at the pair of holes at the left. Connect it to the micro HDMI port next to the USB-C port.",
        "Lead the USB cable to the right, then up the right side of the back plate. Tie it at the two pairs of holes at the right. Bend it over the top and connect it to the black USB 2 port away from the board, from above.",
        "Connect the printer to the black USB 2 port next to the board. The USB ports point up, so you connect them from the top of the case.",
        "For each tie: put it in through one hole and out through the other, around the cable. Twist it closed on the rear.",
    ], "No cable shows on the front or the sides. On the rear the cables are part of the look: keep them tight, with no loops."),
]

CSS = """
  @page { size: A4; margin: 0; }
  * { box-sizing: border-box; }
  body { margin: 0; font-family: "Helvetica Neue", Helvetica, Arial, "Liberation Sans", sans-serif; color: #111; }
  .page { width: 210mm; height: 297mm; padding: 16mm 16mm 14mm; page-break-after: always; position: relative; overflow: hidden; }
  .page:last-child { page-break-after: auto; }
  h1 { font-size: 50pt; letter-spacing: 2pt; margin: 0; font-weight: 800; }
  h2 { font-size: 20pt; margin: 0 0 5mm; font-weight: 700; }
  .sub { font-size: 24pt; font-weight: 700; margin-top: 2mm; color: #1F2B4D; }
  .num { font-size: 84pt; font-weight: 800; line-height: 1; color: #1F2B4D; }
  .head { display: flex; align-items: flex-end; gap: 8mm; border-bottom: 1.2mm solid #1F2B4D; padding-bottom: 4mm; }
  .head .title { font-size: 22pt; font-weight: 700; padding-bottom: 3mm; }
  .fig { width: 100%; margin: 4mm 0; text-align: center; }
  .fig img { max-width: 100%; max-height: 118mm; }
  .need { display: flex; flex-wrap: wrap; gap: 3mm; margin: 4mm 0; }
  .chip { border: 0.6mm solid #111; border-radius: 3mm; padding: 2mm 4mm; font-size: 11pt; font-weight: 700; }
  ol, ul { font-size: 12pt; line-height: 1.5; padding-left: 6mm; margin: 2mm 0; }
  p { font-size: 12pt; line-height: 1.5; margin: 2mm 0; }
  .note { border: 0.8mm solid #E8742A; border-radius: 3mm; padding: 3mm 5mm; margin-top: 4mm; font-size: 11.5pt; }
  .note b { color: #E8742A; }
  table { border-collapse: collapse; width: 100%; font-size: 10.5pt; }
  td, th { border-bottom: 0.2mm solid #999; padding: 1.4mm 2mm; vertical-align: middle; text-align: left; }
  th { border-bottom: 0.6mm solid #111; }
  td.q { font-weight: 800; font-size: 13pt; width: 12mm; }
  td.n, th.n { text-align: right; white-space: nowrap; }
  tr.total td { font-weight: 800; border-top: 0.6mm solid #111; border-bottom: none; }
  .thumb { width: 22mm; height: 16mm; object-fit: contain; display: block; }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 4mm; }
  .card { border: 0.4mm solid #111; border-radius: 3mm; padding: 2.5mm; font-size: 8.8pt; line-height: 1.3; }
  .card img { width: 100%; height: 28mm; object-fit: contain; }
  .card b { display: block; font-size: 11pt; margin: 1mm 0; }
  .sw { display: inline-block; width: 3.5mm; height: 3.5mm; border-radius: 1mm; vertical-align: -0.5mm; margin-right: 1.5mm; }
  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 8mm; }
  code { font-size: 0.92em; }
  .foot { position: absolute; bottom: 8mm; left: 16mm; right: 16mm; font-size: 9.5pt; color: #555; display: flex; justify-content: space-between; }
"""

SWATCH = {"Gray": "#80868E", "Orange": "#E8742A", "Navy": "#1F2B4D"}


def jpg(name, width=1600):
    WEB.mkdir(parents=True, exist_ok=True)
    src, dst = IMG / f"{name}.png", WEB / f"{name}.jpg"
    subprocess.run(["magick", str(src), "-resize", f"{width}x{width}>", "-quality", "88", str(dst)], check=True)
    return dst.as_uri()


def foot(n):
    return f'<div class="foot"><span>Speakerdeck Slate · build instructions</span><span>{n}</span></div>'


def cover():
    total = sum(FILAMENT[n]["grams"] for n, _, _ in PRINTS)
    return f"""<section class="page">
  <h1>SPEAKERDECK</h1><div class="sub">Slate</div>
  <p style="margin-top:4mm;max-width:160mm">A compact slab for the Speakerdeck: Raspberry Pi 5, HiFiBerry Amp2, the 7 inch
  touch display in portrait, and the drive. The slab leans back 15° on a gray foot. The Pi is open on the orange back plate at the
  rear, as a backpack: every port has free space, so you plug the power, the printer, and the speakers straight
  into it. The front and the sides stay clean. All the cables show only on the rear. The case parts snap together.</p>
  <div class="fig"><img src="{jpg('hero')}" style="max-height:125mm;max-width:49%"><img src="{jpg('rear')}" style="max-height:125mm;max-width:49%"></div>
  <div class="need"><div class="chip"><span class="sw" style="background:#1F2B4D"></span>Navy (blue) PLA</div>
    <div class="chip"><span class="sw" style="background:#E8742A"></span>Orange PLA</div>
    <div class="chip"><span class="sw" style="background:#80868E"></span>Gray PLA</div>
    <div class="chip">4 printed parts · {total} g</div><div class="chip">No supports</div></div>
  {foot(1)}
</section>"""


def parts_page():
    rows = []
    tg = tm = 0
    colours = {}
    for name, colour, _ in PRINTS:
        p = SUMMARY["parts"][name]
        g, m = FILAMENT[name]["grams"], FILAMENT[name]["metres"]
        tg += g
        tm += m
        colours[colour] = colours.get(colour, 0) + g
        sx, sy, sz = p["size"]
        rows.append(f'<tr><td><img class="thumb" src="{jpg("part_" + name, 500)}"></td><td><code>stl/{name}.stl</code></td>'
                    f'<td><span class="sw" style="background:{SWATCH[colour]}"></span>{colour}</td>'
                    f'<td class="n">{sx:.0f} × {sy:.0f} × {sz:.0f}</td><td class="n">{g:.0f} g</td><td class="n">{m:.1f} m</td><td class="n">${cost(g):.2f}</td></tr>')
    rows.append(f'<tr class="total"><td></td><td>Total</td><td></td><td></td><td class="n">{tg:.0f} g</td><td class="n">{tm:.0f} m</td>'
                f'<td class="n">${cost(tg):.2f}</td></tr>')
    by_colour = " · ".join(f"{c} {g:.0f} g (${cost(g):.2f})" for c, g in colours.items())
    hw = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in HARDWARE)
    el = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in ELECTRONICS)
    return f"""<section class="page">
  <h2>Printed parts</h2>
  <table><tr><th></th><th>File</th><th>Colour</th><th class="n">Size on the bed (mm)</th><th class="n">Filament</th><th class="n">Length</th><th class="n">Cost</th></tr>
  {''.join(rows)}</table>
  <p style="font-size:9.5pt;color:#444">Filament from slices with the Cura profiles of this case (CuraEngine 5.13), with the skirt.
  Cost at ${PRICE_PER_KG:.2f}/kg. By colour: {by_colour}.</p>
  <div class="cols" style="margin-top:6mm">
    <div><h2>Hardware</h2><table>{hw}</table>
      <p style="font-size:10pt">The case parts hold with snap hooks. The screws hold only the display and the drive;
      the Pi stack goes on its own standoffs.</p></div>
    <div><h2>Electronics</h2><table style="font-size:9.5pt">{el}</table></div>
  </div>
  {foot(2)}
</section>"""


def print_page():
    cards = "".join(f'<div class="card"><img src="{jpg("part_" + n, 700)}"><b>{n.replace("_", " ")}</b>'
                    f'<span class="sw" style="background:{SWATCH[c]}"></span>{c} · support check {SUPPORTS[n]:.0f} g<br>{t}</div>'
                    for n, c, t in PRINTS)
    st = "".join(f"<tr><td>{k}</td><td>{v}</td></tr>" for k, v in SETTINGS)
    return f"""<section class="page">
  <h2>Print</h2>
  <p>The STL files are in their print orientation. Load them, do not turn them.</p>
  <div class="grid3">{cards}</div>
  <h2 style="margin-top:5mm">Settings (LulzBot TAZ Workhorse)</h2>
  <table style="font-size:9.5pt">{st}</table>
  <div class="note"><b>Fit.</b> The snap joints have 0.3 mm of clearance. Print the chin first and push it onto the slab.
  If it is too tight, set the horizontal expansion in your slicer to −0.1 mm for the chin and the back plate.</div>
  {foot(3)}
</section>"""


def step_page(i, step, n):
    img, title, need, items, note = step
    chips = "".join(f'<div class="chip">{x}</div>' for x in need)
    lis = "".join(f"<li>{t}</li>" for t in items)
    nt = f'<div class="note"><b>Note.</b> {note}</div>' if note else ""
    return f"""<section class="page">
  <div class="head"><div class="num">{i}</div><div class="title">{title}</div></div>
  <div class="need">{chips}</div>
  <div class="fig"><img src="{jpg(img)}"></div>
  <ol>{lis}</ol>{nt}
  {foot(n)}
</section>"""


def end_page(n):
    return f"""<section class="page">
  <h2>Connect</h2>
  <div class="cols"><div class="fig"><img src="{jpg('rear_left')}" style="max-height:95mm"></div>
    <div class="fig"><img src="{jpg('exploded')}" style="max-height:95mm"></div></div>
  <ol><li>Connect the USB-C power supply to the Pi. The port is on the left edge of the Pi. Turn the cable to the rear.</li>
    <li>Connect the 3D printer to the black USB 2 port next to the board, from above.</li>
    <li>Put the right-angle plug of the Amp2 power supply into the jack on the left edge of the Amp2. Turn the cable to the rear.</li>
    <li>Connect the speaker wires to the screw terminals on the right edge of the Amp2.</li></ol>
  <h2 style="margin-top:6mm">To open the case</h2>
  <ol><li>Disconnect the cables on the rear. Pull the foot off. Its hooks let go when you pull hard.</li>
    <li>Push a pin or a paper clip into the small hole in each side wall, just above the chin. Each pin pushes
      the ridge of one chin tongue out of its groove. Pull the chin down.</li>
    <li>Slide the back plate down and out of the grooves. The drive and the Pi come out with it.</li></ol>
  {foot(n)}
</section>"""


def main():
    pages = [cover(), parts_page(), print_page()]
    for i, s in enumerate(STEPS):
        pages.append(step_page(i + 1, s, len(pages) + 1))
    pages.append(end_page(len(pages) + 1))
    html = (f'<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Speakerdeck Slate build</title>'
            f'<style>{CSS}</style></head><body>{"".join(pages)}</body></html>')
    src = BUILD / "build.html"
    src.write_text(html)
    subprocess.run(["chromium", "--headless", "--disable-gpu", "--no-pdf-header-footer", "--allow-file-access-from-files",
                    f"--print-to-pdf={OUT}", src.as_uri()], check=True, capture_output=True)
    print("wrote", OUT)


if __name__ == "__main__":
    main()
