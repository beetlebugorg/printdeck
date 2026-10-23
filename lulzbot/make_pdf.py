#!/usr/bin/env python3
"""Printdeck LulzBot: write the build instructions PDF (printdeck-lulzbot-build.pdf).
Run after model.py, cura/make_profiles.py, and lineart/render.mjs. Needs Chromium.
The page layout, the fonts, and the colours are the same as the Slate guide (slate/make_pdf.py).
The images are the line-art renders of lineart/render.mjs, light theme, with a transparent background."""

import json
import math
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
BUILD = HERE / "build"
IMG = BUILD / "img"
OUT = HERE / "printdeck-lulzbot-build.pdf"

SUMMARY = json.loads((BUILD / "parts.json").read_text())
FILAMENT = json.loads((BUILD / "filament.json").read_text())
SUPPORTS = json.loads((BUILD / "supports.json").read_text())
CABLES = SUMMARY["cables"]
HEAD = SUMMARY["head"]
PRICE_PER_KG = 22.99                 # USD, PLA
SPOOL_G = 1000


def cost(grams):
    return grams / 1000 * PRICE_PER_KG


def hours(minutes):
    return f"{minutes // 60} h {minutes % 60:02d} min"


def cm(mm):
    return f"{mm / 10:.0f} cm"


HW = json.loads((BUILD / "hardware.json").read_text())
ITEMS = HW["items"]
COVER_OK = HW["cover_confirmed"]


def chip(key, n):
    """A hardware chip: the count and the short name of an item of the hardware list."""
    item = ITEMS[key]["item"]
    short = item.replace(" socket cap head", "").replace(" heat-set insert", " insert")
    short = short.replace(" (drop-in T-nut) for 2020 extrusion, 6 mm slot", "")
    return f"{n} × {short}"


# Part, STL name, colour, count, print notes. The order is the order of the parts list.
PRINTS = [
    ("head", "head", "Navy", 1, "Face down. The screen opening is in the first layers: use a smooth sheet for a clean face."),
    ("pod", "pod", "Navy", 1, "Outer face down. The grille pocket and the vents are in the first layers."),
    ("back", "back", "Orange", 1, "Rear face down. A flat plate with the clip slots and the windows."),
    ("plate", "plate", "Orange", 1, "The face that goes on the cover down. The bosses and the hooks point up."),
    ("clip_a", "clip", "Orange", 2, "On its end, with a brim: the section of the clip is on the bed. Print 2."),
    ("grille", "grille", "White", 1, "Back down. The front face is the top layer. The snap tabs bend along the layers."),
]
TEST = ("fit_display", "fit_display", "Navy", 1, "Optional fit test of the screen opening. Face down.")

SETTINGS = [
    ("Profiles", "One Cura profile for each part: run <code>python3 cura/make_profiles.py --install</code> with Cura "
                 "closed, then pick “Printdeck - LulzBot …”."),
    ("Printer", "LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle"),
    ("Material", "PLA, 2.85 mm"),
    ("Head, pod, grille", "0.3 mm layers, 1.5 mm walls, 1.2 mm top and bottom, 15 % grid infill, skirt"),
    ("Back panel, plate", "0.4 mm layers (a 0.3 mm first layer), 1.5 mm walls, 15 % grid infill, skirt"),
    ("Clip", "0.3 mm layers, 100 % infill, with a brim."),
    ("Speeds", "Infill 75 mm/s (60 mm/s with 0.4 mm layers), inner walls 60 mm/s, outer walls 35 mm/s (30 mm/s on a face "
               "that shows), travel 200 mm/s. The flow stays at 12 mm³/s or less."),
    ("Supports", "Off for all parts. Every hole, pocket, and hook has 45° faces. A slice of each part with supports "
                 "on (60°, everywhere) adds 0 g of support."),
]

ELECTRONICS = [
    (1, "Raspberry Pi 5 (from the Slate build)"),
    (1, "Raspberry Pi Active Cooler, max. 10.5 mm high (buy, if you do not have one)"),
    (1, "Adafruit 7 inch HDMI display with touch, product 2407 (from the Slate build)"),
    (1, "2.5 inch SATA drive with its USB adapter board, the boot drive (from the Slate build)"),
    (1, f"HDMI cable: micro HDMI to HDMI, straight plugs, a flat ribbon of 30 cm or more (the route is "
        f"{cm(CABLES['hdmi'])}). The ribbon kit of the Slate fits."),
    (1, f"Micro USB to USB-A cable, straight plugs, max. 3.5 mm thick, 15 cm or more (the route is {cm(CABLES['touch'])})."),
    (1, f"USB 3 cable for the drive: micro-B SuperSpeed to USB-A, 15 to 20 cm (the route is {cm(CABLES['drive'])})."),
    (1, f"USB-A to USB-B cable for the printer: {cm(CABLES['printer'])} in the pod, plus the run behind the printer to "
        "the USB socket of the printer board."),
    (1, f"USB-C power cable: {cm(CABLES['power'])} in the pod, plus the run behind the printer to the Raspberry Pi 27 W "
        "USB-C power supply."),
]

TOOLS = [
    "Drill with a 3.2 mm bit (for the four holes of the Pi)",
    "Soldering iron with a heat-set insert tip, at 220 °C",
    "2.5 mm hex key (M3 socket cap screws)",
]


def cover_text():
    pos = "; ".join(f"{b:.1f} mm above the bottom of the chassis and {f:.1f} mm from its front face ({n})"
                    for (b, f), n in zip(HW["cover_screws_mm"], HW["cover_notes"]))
    return pos


CHECKS = [
    "Check the rear screw of the upper pair on the cover: 237.3 mm above the bottom edge of the cover, and about "
    "3.6 mm from the rear edge of the cover. The design uses the value of the LulzBot drawing.",
    "Measure the TAZ Workhorse logo on the front of the chassis. The top of the head is 260 mm above the bottom of "
    "the chassis; the LulzBot drawing gives the logo at 264 mm, and the drawing is not to scale.",
    "Look at the front slot of the front left rail. The clips need a free slot from 150 to 230 mm above the bottom "
    "of the chassis: remove the dust cover there, if the rail has one.",
    "Move the bed to the front and the X axis to the bottom. Check that the head is clear of the bed, the print, and "
    "the tool head: it is 72 to 324 mm to the right of the left face of the chassis, and 125 to 260 mm above the "
    "bottom of the chassis.",
    "Check behind the printer: the printer USB cable and the power cable leave the pod at the rear face of the "
    "chassis, 128 to 149 mm above its bottom.",
]

# Steps: (shot, title, printed and other chips, hardware {key: count}, items, note).
STEPS = [
    ("step_drill", "Drill the Pi", ["Pi 5", "3.2 mm drill"], {}, [
        "The mounting holes of the Pi 5 are 2.7 mm, for M2.5. Drill the four holes to 3.2 mm for M3.",
        "Drill slowly, from the top of the board, with the board flat on a piece of wood.",
        "Keep the drill straight. Stay inside the plated ring of each hole: do not touch the tracks or the parts "
        "around it.",
        "Remove all the dust from the board with a brush or with air before you continue.",
    ], "Skip this step if your Pi already has 3.2 mm holes."),
    ("step_display", "Display into the head", ["head", "display", "foam tape"],
     {"ins_m3x7": 4, "m3x6": 4}, [
        "Press the four M3 × 7 inserts into the two columns and the two blocks at the back of the head. Use a "
        "soldering iron at 220 °C. Stop when the insert is flush.",
        "Put the head face down on a soft cloth.",
        "Put the display glass down into the pocket: the sockets of the board at the left (seen from the front), the "
        "FPC edge of the panel at the top.",
        "Put the foam tape on the back of the panel. Put the board on the four bosses, and attach it with four M3 × 6 "
        "screws. They cut their own thread in the bosses: turn them until they are snug, and no more.",
        "Connect the HDMI cable and the micro USB cable to the board. Both plugs point to the left.",
    ], "The screen opening is at the centre of the face: the border is the same at the left and the right."),
    ("step_drive", "Drive on the back panel", ["back panel", "drive"], {"ins_m3x7": 2, "m3x6": 4}, [
        "Press the two M3 × 7 inserts into the two bosses on the front face of the back panel, from the rear.",
        "Put the drive on the four pads on the front face of the back panel: the adapter toward the left (the pod "
        "side), the M3 holes on the pads.",
        "Attach it with four M3 × 6 screws from the rear face of the panel.",
        "Connect the micro-B plug of the drive cable to the adapter.",
    ], None),
    ("step_back", "Back panel onto the head", ["head (step 2)", "back panel (step 3)"], {"m3x10": 4}, [
        "Put the display cables through the upper window at the left of the back panel, and the drive cable through "
        "the lower window.",
        "Put the back panel on the head. Attach it with four M3 × 10 screws from the rear face, into the inserts.",
    ], "The heads of the screws stay on the rear face. They are behind the head, and they do not show."),
    ("step_plate", "Plate on the cover", ["plate"], {"ins_m3x5": 2, "m3x10": 2, "m3_washer": 2}, [
        "Press the two M3 × 5 inserts into the two blocks at the bottom of the plate (from below).",
        "The cover is PP-FP0164, with 10 screws. Remove its upper pair of screws from the left side of the chassis: "
        "the front one and the rear one, 237.3 mm above the bottom edge of the cover.",
        "Put the plate on the cover, with the slots of the two pads over the two holes. Attach it with two M3 × 10 "
        "screws, each with a washer.",
    ], "The slots in the pads let the plate fit 5 mm up or down and 0.5 mm to the front or the rear. The pad is 4 mm "
       "thick, so the M3 × 10 goes 3.5 mm into the nut of the chassis (the old screw went 4.0 mm)."),
    ("step_pi", "Pi on the plate", ["Pi 5 (step 1)", "Active Cooler"], {"m3x6": 4}, [
        "Put the Active Cooler on the Pi, and connect its cable to the fan connector.",
        "Put the Pi on the four bosses: the USB ports toward the front of the printer, the USB-C and HDMI ports down.",
        "Attach it with four M3 × 6 screws. They cut their own thread in the bosses: turn them until they are "
        "snug, and no more.",
    ], None),
    ("step_clips", "Clips in the rail", ["2 × rail clip"], {"m3_hammer": 2, "m3x8": 2}, [
        "Put an M3 × 8 screw through the counterbore in the key of each clip, and turn it two turns into a hammer "
        "nut. The sloped side of the nut points away from the clip.",
        "Hold the back panel on the front face of the front left rail, with its bottom edge 125 mm above the bottom "
        "of the chassis. Mark the top of the two long slots in the panel on the rail.",
        "Turn a clip a quarter turn, so that the nut is along the slot. Push the nut into the front slot of the rail "
        "at a mark. Turn the clip back until the key is along the rail: the nut turns with it, behind the lips.",
        "Tighten the screw until the clip is tight on the rail. Do the same with the second clip.",
    ], "The hammer nut is wider than the opening of the slot when it is across the slot. If the nut does not turn "
       "with the clip, push it in with a small screwdriver beside the clip and turn it a quarter turn."),
    ("step_hang", "Hang the head", ["head (step 4)"], {"m3x12": 2}, [
        "Hold the head 30 mm above its place, with the two windows of the back panel over the two clip keys. Push the "
        "back panel against the clips.",
        "Let the head drop 30 mm. The keys go into the dovetail slots.",
        "Lead the display cables and the drive cable out of the windows into the pod.",
        "From inside the pod, put the two M3 × 12 screws through the flange of the plate into the back panel.",
    ], "The two screws through the flange hold up the head. The clips hold its right end against the rail."),
    ("step_cables", "Cables", [], {}, [
        "Connect the HDMI cable to the HDMI 0 port (next to the USB-C port).",
        "Connect the touch cable to the upper USB 3 port (blue), and the drive cable to the lower USB 3 port.",
        "Connect the printer cable to the lower USB 2 port. Lead it up the front of the pod, over the Pi, down at the "
        "rear end of the Pi, and out of the upper notch in the rear wall.",
        "Connect the power cable to the USB-C port. Lead it down and to the rear, out of the lower notch.",
        "Behind the printer, connect the printer cable to the USB socket of the printer board, and the power cable to "
        "the power supply.",
    ], "No cable goes through a hole in the printer, and no cable shows on the front or the side."),
    ("step_pod", "Grille and pod", ["grille", "pod"], {"m3x8": 2}, [
        "Push the grille into the pocket of the pod from the outside, with the fingernail notch at the bottom. Push "
        "both sides until the two tabs click. The grille is flush with the pod.",
        "Hold the pod about 6 mm low and 6 mm out from its place, and 4 mm to the rear. Move it 4 mm to the front, "
        "then push it up and in at 45° onto the two hooks of the plate. The rear wall goes over the two cables.",
        "Put the two M3 × 8 screws up through the bottom of the pod into the plate. They pull the pod up, and the "
        "45° hooks pull it onto the plate.",
    ], "To remove the grille, put a fingernail or a small screwdriver into the notch at its bottom edge, and lift."),
]


def check_chips():
    """Every hardware chip of the steps is an item of the hardware list, and the counts agree.
    The build stops when they do not."""
    used = {}
    for _, _, _, hw, _, _ in STEPS:
        for k, n in hw.items():
            if k not in ITEMS:
                raise SystemExit(f"step hardware {k} is not in the hardware list of the model")
            used[k] = used.get(k, 0) + n
    bad = {k: (used.get(k, 0), e["count"]) for k, e in ITEMS.items() if used.get(k, 0) != e["count"]}
    if bad:
        raise SystemExit("step hardware counts do not agree with the model: "
                         + ", ".join(f"{ITEMS[k]['item']}: steps {a}, model {b}" for k, (a, b) in bad.items()))
    print("  step chips: all hardware in the list, counts agree")


CSS = """
  @page { size: A4; margin: 0; }
  * { box-sizing: border-box; }
  :root { --ink: #16181a; --ink-2: #4a4f55; --accent: #0a7f63; --bg: #fbfbf9; --grid: #ececE6; --line: #e2e2dc;
          --mono: "JetBrains Mono", "JetBrainsMono Nerd Font", ui-monospace, monospace; }
  body { margin: 0; font-family: "Inter", "Helvetica Neue", Helvetica, Arial, "Liberation Sans", sans-serif; color: var(--ink); }
  .page { width: 210mm; height: 297mm; padding: 16mm 16mm 14mm; page-break-after: always; position: relative; overflow: hidden; }
  .page:last-child { page-break-after: auto; }
  h1 { font-size: 50pt; letter-spacing: 2pt; margin: 0; font-weight: 800; }
  h2 { font-size: 20pt; margin: 0 0 5mm; font-weight: 700; }
  .sub { font-size: 24pt; font-weight: 700; margin-top: 2mm; color: var(--accent); }
  .num { font: 800 72pt/1 var(--mono); color: var(--accent); }
  .head { display: flex; align-items: flex-end; gap: 8mm; border-bottom: 0.6mm solid var(--ink); padding-bottom: 4mm; }
  .head .title { font-size: 22pt; font-weight: 700; padding-bottom: 3mm; }
  .fig { width: 100%; margin: 4mm 0; padding: 4mm; text-align: center; border: 0.3mm solid var(--line); border-radius: 3mm;
         background-color: var(--bg); background-size: 6mm 6mm;
         background-image: linear-gradient(var(--grid) 0.15mm, transparent 0.15mm), linear-gradient(90deg, var(--grid) 0.15mm, transparent 0.15mm); }
  .fig img { max-width: 100%; max-height: 112mm; vertical-align: middle; }
  .need { display: flex; flex-wrap: wrap; gap: 3mm; margin: 4mm 0; }
  .chip { border: 0.4mm solid var(--ink); border-radius: 999px; padding: 1.5mm 4mm; font: 600 10pt var(--mono); }
  ol, ul { font-size: 12pt; line-height: 1.5; padding-left: 6mm; margin: 2mm 0; }
  p { font-size: 12pt; line-height: 1.5; margin: 2mm 0; }
  .note { border: 0.4mm solid var(--accent); border-radius: 3mm; padding: 3mm 5mm; margin-top: 4mm; font-size: 11.5pt; background: #e3f3ee; }
  .note b { color: var(--accent); }
  table { border-collapse: collapse; width: 100%; font-size: 10.5pt; }
  td, th { border-bottom: 0.2mm solid #999; padding: 1.4mm 2mm; vertical-align: middle; text-align: left; }
  th { border-bottom: 0.6mm solid #111; }
  td.q { font-weight: 800; font-size: 13pt; width: 12mm; }
  td.n, th.n { text-align: right; white-space: nowrap; }
  tr.total td { font-weight: 800; border-top: 0.6mm solid #111; border-bottom: none; }
  tr.opt td { color: #555; font-style: italic; }
  .thumb { width: 17mm; height: 13mm; object-fit: contain; display: block; }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 4mm; }
  .card { border: 0.3mm solid var(--line); background: var(--bg); border-radius: 3mm; padding: 2.5mm; font-size: 8.8pt; line-height: 1.3; }
  .card img { width: 100%; height: 21mm; object-fit: contain; }
  .card b { display: block; font-size: 11pt; margin: 1mm 0; }
  .sw { display: inline-block; width: 3.5mm; height: 3.5mm; border-radius: 1mm; vertical-align: -0.5mm; margin-right: 1.5mm;
        border: 0.2mm solid #888; }
  .cols { display: grid; grid-template-columns: 1fr 1fr; gap: 8mm; }
  code { font-size: 0.92em; }
  .foot { position: absolute; bottom: 8mm; left: 16mm; right: 16mm; font-size: 9.5pt; color: #555; display: flex; justify-content: space-between; }
"""

SWATCH = {"Navy": "#1F2B4D", "Orange": "#E8742A", "White": "#F2F2EE"}


def img(name):
    return (IMG / f"{name}-light.png").as_uri()


def foot(n):
    return f'<div class="foot"><span>Printdeck LulzBot · build instructions</span><span>{n}</span></div>'


def totals():
    g = sum(FILAMENT[s]["grams"] * c for _, s, _, c, _ in PRINTS)
    m = sum(FILAMENT[s]["metres"] * c for _, s, _, c, _ in PRINTS)
    t = sum(FILAMENT[s]["minutes"] * c for _, s, _, c, _ in PRINTS)
    return g, m, t


def cover():
    g, _, t = totals()
    n = sum(c for *_, c, _ in PRINTS)
    return f"""<section class="page">
  <h1>PRINTDECK</h1><div class="sub">LulzBot TAZ Workhorse mount</div>
  <p style="margin-top:4mm;max-width:165mm">A mount for the Printdeck at the front left of the TAZ Workhorse:
  the 7 inch touch display in a wide head below the TAZ Workhorse logo, and the Raspberry Pi 5 in a pod on the
  left side of the electronics chassis. The head leans back 15°, the same as the Slate, and faces the front of the
  printer. The drive is in the head, behind the display. There is no hole to drill:
  two printed clips hold on the front rail with metal hammer nuts, the upper pair of screws of the cover
  (PP-FP0164) holds the plate, and the cables leave at the rear of the chassis.</p>
  <div class="fig"><img src="{img('hero')}" style="max-height:120mm;max-width:49%"><img src="{img('side')}" style="max-height:120mm;max-width:49%"></div>
  <div class="need"><div class="chip"><span class="sw" style="background:#1F2B4D"></span>Navy PLA</div>
    <div class="chip"><span class="sw" style="background:#E8742A"></span>Orange PLA</div>
    <div class="chip"><span class="sw" style="background:#F2F2EE"></span>White PLA</div>
    <div class="chip">{n} printed parts · {g} g · {hours(t)}</div><div class="chip">No supports</div>
    <div class="chip">No drilling in the printer</div></div>
  {foot(1)}
</section>"""


def plan_page():
    rows = []
    colours = {}
    for name, stl, colour, count, _ in PRINTS:
        p = SUMMARY["parts"][name]
        f = FILAMENT[stl]
        c = colours.setdefault(colour, {"g": 0, "m": 0.0, "t": 0, "parts": []})
        c["g"] += f["grams"] * count
        c["m"] += f["metres"] * count
        c["t"] += f["minutes"] * count
        c["parts"].append(stl if count == 1 else f"{count} × {stl}")
        sx, sy, sz = p["size"]
        n = "" if count == 1 else f" ({count}×)"
        rows.append(f'<tr><td><img class="thumb" src="{img("part_" + stl)}"></td><td><code>stl/{stl}.stl</code>{n}</td>'
                    f'<td><span class="sw" style="background:{SWATCH[colour]}"></span>{colour}</td>'
                    f'<td class="n">{sx:.0f} × {sy:.0f} × {sz:.0f}</td><td class="n">{f["grams"] * count} g</td>'
                    f'<td class="n">{f["metres"] * count:.1f} m</td><td class="n">{hours(f["minutes"] * count)}</td>'
                    f'<td class="n">${cost(f["grams"] * count):.2f}</td></tr>')
    g, m, t = totals()
    rows.append(f'<tr class="total"><td></td><td>Total</td><td></td><td></td><td class="n">{g} g</td><td class="n">{m:.0f} m</td>'
                f'<td class="n">{hours(t)}</td><td class="n">${cost(g):.2f}</td></tr>')
    f = FILAMENT[TEST[1]]
    rows.append(f'<tr class="opt"><td><img class="thumb" src="{img("part_fit_display")}"></td><td><code>stl/fit_display.stl</code></td>'
                f'<td><span class="sw" style="background:{SWATCH[TEST[2]]}"></span>{TEST[2]}</td><td class="n">optional</td>'
                f'<td class="n">{f["grams"]} g</td><td class="n">{f["metres"]:.1f} m</td><td class="n">{hours(f["minutes"])}</td>'
                f'<td class="n">${cost(f["grams"]):.2f}</td></tr>')
    crow = "".join(
        f'<tr><td><span class="sw" style="background:{SWATCH[k]}"></span>{k}</td><td>{", ".join(v["parts"])}</td>'
        f'<td class="n">{v["g"]} g</td><td class="n">{v["m"]:.1f} m</td><td class="n">{hours(v["t"])}</td>'
        f'<td class="n">{math.ceil(v["g"] / SPOOL_G)} × 1 kg ({v["g"] / SPOOL_G * 100:.0f} %)</td></tr>'
        for k, v in colours.items())
    hb = SUMMARY["parts"]["head"]["size"]
    return f"""<section class="page">
  <h2>Print plan</h2>
  <p>Print the parts before you start. The numbers come from slices of the STL files with the Cura profiles of
  this mount (CuraEngine, LulzBot TAZ Workhorse SE, 0.5 mm nozzle, 2.85 mm PLA), with the skirt.</p>
  <table style="font-size:9pt"><tr><th></th><th>File</th><th>Colour</th><th class="n">Size on the bed (mm)</th><th class="n">Filament</th>
  <th class="n">Length</th><th class="n">Time</th><th class="n">Cost</th></tr>
  {''.join(rows)}</table>
  <h2 style="margin-top:6mm">By colour</h2>
  <table><tr><th>Colour</th><th>Parts</th><th class="n">Filament</th><th class="n">Length</th><th class="n">Time</th>
  <th class="n">Spools</th></tr>{crow}</table>
  <div class="note"><b>Total.</b> {g} g of PLA ({m:.0f} m), {hours(t)} of printing, ${cost(g):.2f} at
  ${PRICE_PER_KG:.2f}/kg. With the fit test: {g + f["grams"]} g and {hours(t + f["minutes"])}. The largest part
  (the head) needs a bed of {hb[0]:.0f} × {hb[1]:.0f} mm; the Workhorse bed is 280 × 280 mm.</div>
  <div class="note"><b>Order.</b> Print the fit test first ({hours(f["minutes"])}): the display pocket has 0.3 mm
  of clearance at each side, so check it before the head. Then the head and the back panel: steps 2 to 4 need them.
  Then the plate, the clips, the pod, and the grille.</div>
  <div class="note"><b>By colour.</b> You can also print one job for each colour: the head and the pod together
  (navy, with the head profile), the back panel, the plate, the clips together (orange, with the plate
  profile; set the clips to 100 % infill in Cura), and the grille (white). The time is the same, with
  fewer changes of the spool; a failure costs more than one part.</div>
  {foot(2)}
</section>"""


def print_page():
    cards = "".join(f'<div class="card"><img src="{img("part_" + s)}"><b>{s.replace("_", " ")}</b>'
                    f'<span class="sw" style="background:{SWATCH[c]}"></span>{c} · support check {SUPPORTS[s]:.0f} g<br>{t}</div>'
                    for _, s, c, _, t in PRINTS + [TEST])
    st = "".join(f"<tr><td>{k}</td><td>{v}</td></tr>" for k, v in SETTINGS)
    return f"""<section class="page">
  <h2>Print</h2>
  <p>The STL files are in their print orientation. Load them as they are.</p>
  <div class="grid3" style="grid-template-columns: repeat(4, 1fr)">{cards}</div>
  <h2 style="margin-top:5mm">Settings (LulzBot TAZ Workhorse)</h2>
  <table style="font-size:9pt">{st}</table>
  <div class="note"><b>Fit.</b> The grille has 0.2 mm of clearance in its pocket, and the hooks and the clip slots
  have 0.3 mm. If the pod is tight on the hooks, set the horizontal expansion in your slicer to −0.1 mm for the pod.</div>
  {foot(3)}
</section>"""


def parts_page():
    rows = []
    for key in sorted(ITEMS, key=lambda k: (not k.startswith("m3x"), ITEMS[k]["item"])):
        e = ITEMS[key]
        where = "; ".join(f"{n} × {w}" for w, n in e["places"].items())
        rows.append(f'<tr><td class="q">{e["count"]}×</td><td><b>{e["item"]}</b><br>'
                    f'<span style="font-size:8.5pt;color:#444">{where} · {e["box"]}</span></td></tr>')
    el = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in ELECTRONICS)
    tl = "".join(f"<li>{t}</li>" for t in TOOLS)
    return f"""<section class="page">
  <div class="cols">
    <div><h2>Hardware</h2><table style="font-size:9.5pt">{''.join(rows)}</table>
      <p style="font-size:9pt">From the model: each screw, washer, and insert is in the model at its place. All
      screws are socket cap head screws. A shorter or a longer insert is fine, up to the length in brackets.
      All the screws are M3 socket cap screws. The screws of the display and the Pi cut their own thread in the
      bosses. The position of the rear cover screw is to be confirmed (page 5).</p></div>
    <div><h2>Electronics and cables</h2><table style="font-size:8.8pt">{el}</table>
      <h2 style="margin-top:4mm">Tools</h2><ul style="font-size:10pt">{tl}</ul>
      <p style="font-size:9pt">2 mm foam tape, one strip (between the panel and the display board).</p></div>
  </div>
  {foot(4)}
</section>"""


def mount_page():
    ch = "".join(f"<li>{c}</li>" for c in CHECKS)
    pos = cover_text()
    e = ITEMS["m3x10"]
    return f"""<section class="page">
  <h2>On the printer</h2>
  <div class="fig"><img src="{img('exploded')}" style="max-height:72mm"></div>
  <p>The mount uses two screws of the printer and the front rail. You drill no hole.</p>
  <ul style="font-size:10.5pt"><li><b>Cover screws.</b> The cover of the chassis is PP-FP0164, with 10 M3
    screws into nuts in the chassis. The two screws of its upper pair, under the plate, change to M3 × 10 socket cap
    screws with a washer. They are at: {pos}. The pads have slots 10 mm long and 4.4 mm wide, so the plate fits 5 mm
    up or down.</li>
    <li><b>Front rail.</b> Two printed clips hold on the front slot of the front left rail. An M3 hammer nut for
    2020 extrusion goes into the slot along it, and turns a quarter turn behind the lips; an M3 × 8 screw pulls it
    up. You drill no hole in the rail. The dovetail keys of the clips
    hold the right end of the head against the rail.</li>
    <li><b>Flange.</b> Two M3 × 12 socket cap screws go from inside the pod through the flange of the plate into the
    back panel. They hold up the head.</li>
    <li><b>Hooks.</b> The pod slides up onto two 45° hooks of the plate, and two M3 × 8 screws hold it from below.</li></ul>
  <h2 style="margin-top:4mm">Check on your printer</h2>
  <ol style="font-size:10pt">{ch}</ol>
  {foot(5)}
</section>"""


def step_page(i, step, n):
    shot, title, need, hw, items, note = step
    chips = "".join(f'<div class="chip">{x}</div>' for x in need + [chip(k, c) for k, c in hw.items()])
    lis = "".join(f"<li>{t}</li>" for t in items)
    nt = f'<div class="note"><b>Note.</b> {note}</div>' if note else ""
    return f"""<section class="page">
  <div class="head"><div class="num">{i}</div><div class="title">{title}</div></div>
  <div class="need">{chips}</div>
  <div class="fig"><img src="{img(shot)}"></div>
  <ol>{lis}</ol>{nt}
  {foot(n)}
</section>"""


def end_page(n):
    rows = "".join(f"<tr><td>{k}</td><td class='n'>{cm(v)}</td><td>{t}</td></tr>" for k, v, t in [
        ("HDMI", CABLES["hdmi"], "HDMI 0 port to the display, plug to plug"),
        ("Touch USB", CABLES["touch"], "upper USB 3 port to the display, plug to plug"),
        ("Drive", CABLES["drive"], "lower USB 3 port to the drive, plug to plug"),
        ("Printer USB", CABLES["printer"], "lower USB 2 port to the rear face of the chassis"),
        ("Power", CABLES["power"], "USB-C port to the rear face of the chassis"),
    ])
    return f"""<section class="page">
  <h2>Connect</h2>
  <div class="fig"><img src="{img('connect')}" style="max-height:80mm"></div>
  <table style="font-size:10pt"><tr><th>Cable</th><th class="n">Length</th><th>Route</th></tr>{rows}</table>
  <div class="note"><b>Wide display.</b> The display is turned 180° in the head (its sockets are at the left). Before
    you connect the drive, put it in a computer and open <code>printdeck.txt</code> on the PRINTDECK partition. Set
    <code>display_rotate=180</code>: the deck then uses its wide layout. If the picture is upside down at the first
    start, set <code>display_rotate=0</code>.</div>
  <ol style="margin-top:3mm"><li>Connect the power cable to the power supply behind the printer.</li>
    <li>Connect the printer cable to the USB socket of the printer board.</li>
    <li>Turn on the printer. The deck starts from the drive.</li></ol>
  <h2 style="margin-top:4mm">To open the mount</h2>
  <ol><li>Remove the two screws under the pod. Move the pod down and out at 45°, off the hooks, then 4 mm to the rear
      and out. To remove the grille, lift it at the fingernail notch.</li>
    <li>Disconnect the cables from the Pi. Remove the two flange screws from inside the pod.</li>
    <li>Lift the head 30 mm and pull it off the clip keys. The display and the drive come off with it.</li>
    <li>To get to the display or the drive, remove the four screws from the rear of the back panel.</li></ol>
  {foot(n)}
</section>"""


GUIDE = HERE / "guide"

# The corners of the screen in renders/printer-front-left.png, in pixels: top left, top right,
# bottom right, bottom left. The upgrade booklet draws the screen of the deck into them.
# write_guide checks that the render still has the dark screen there.
SCREEN = [[575, 935], [780, 873], [804, 1017], [599, 1078]]
SCREEN_RGB = (43, 43, 43)


def plain(t):
    """A text with no HTML tags, for guide.json."""
    import re
    return re.sub(r"<[^>]+>", "", t)


def write_guide():
    """Write guide/guide.json and the line-art images that it names, for the upgrade booklet of the
    deck (printdeck manual DIR lulzbot). The guide/ folder is in git, so the booklet builds with no
    run of make.sh."""
    import shutil
    (GUIDE / "img").mkdir(parents=True, exist_ok=True)
    names = ["hero", "side", "exploded", "connect"] + ["part_" + s for _, s, *_ in PRINTS + [TEST]] + [s[0] for s in STEPS]
    for n in dict.fromkeys(names):
        shutil.copyfile(IMG / f"{n}-light.png", GUIDE / "img" / f"lulzbot-{n}.png")
    from PIL import Image
    render = Image.open(HERE / "renders" / "printer-front-left.png").convert("RGB")
    cx, cy = (sum(p[0] for p in SCREEN) // 4, sum(p[1] for p in SCREEN) // 4)
    if max(abs(a - b) for a, b in zip(render.getpixel((cx, cy)), SCREEN_RGB)) > 6:
        raise SystemExit("the screen of renders/printer-front-left.png moved: measure SCREEN again")
    shutil.copyfile(HERE / "renders" / "printer-front-left.png", GUIDE / "img" / "lulzbot-printer.png")
    g, m, t = totals()

    def part(name, stl, colour, count, notes):
        f = FILAMENT[stl]
        return {"file": f"stl/{stl}.stl", "name": stl.replace("_", " "), "colour": colour, "swatch": SWATCH[colour],
                "count": count, "grams": f["grams"] * count, "metres": round(f["metres"] * count, 1),
                "time": hours(f["minutes"] * count), "cost": f"${cost(f['grams'] * count):.2f}",
                "size": " × ".join(f"{v:.0f}" for v in SUMMARY["parts"].get(name, {"size": [0, 0, 0]})["size"]),
                "notes": notes, "image": f"lulzbot-part_{stl}"}
    hw = []
    for key in sorted(ITEMS, key=lambda k: (not k.startswith("m3x"), ITEMS[k]["item"])):
        e = ITEMS[key]
        hw.append({"count": e["count"], "item": e["item"],
                   "where": "; ".join(f"{n} × {w}" for w, n in e["places"].items())})
    guide = {
        "totals": {"grams": g, "metres": round(m), "time": hours(t), "cost": f"${cost(g):.2f}",
                   "parts": sum(c for *_, c, _ in PRINTS), "pricePerKg": f"${PRICE_PER_KG:.2f}"},
        "prints": [part(*p) for p in PRINTS],
        "test": part(*TEST),
        "settings": [[k, plain(v)] for k, v in SETTINGS],
        "hardware": hw,
        "electronics": [[n, plain(t)] for n, t in ELECTRONICS],
        "tools": TOOLS,
        "checks": CHECKS,
        "steps": [{"image": shot, "title": title,
                   "need": need + [chip(k, c) for k, c in hwk.items()], "items": items, "note": note or ""}
                  for shot, title, need, hwk, items, note in STEPS],
        "cables": [[k, cm(v), r] for k, v, r in [
            ("HDMI", CABLES["hdmi"], "HDMI 0 port to the display, plug to plug"),
            ("Touch USB", CABLES["touch"], "upper USB 3 port to the display, plug to plug"),
            ("Drive", CABLES["drive"], "lower USB 3 port to the drive, plug to plug"),
            ("Printer USB", CABLES["printer"], "lower USB 2 port to the rear face of the chassis"),
            ("Power", CABLES["power"], "USB-C port to the rear face of the chassis")]],
        "screen": SCREEN,
    }
    (GUIDE / "guide.json").write_text(json.dumps(guide, indent=1, ensure_ascii=False) + "\n")
    print("wrote", GUIDE / "guide.json")


def main():
    check_chips()
    write_guide()
    pages = [cover(), plan_page(), print_page(), parts_page(), mount_page()]
    for i, s in enumerate(STEPS):
        pages.append(step_page(i + 1, s, len(pages) + 1))
    pages.append(end_page(len(pages) + 1))
    html = (f'<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Printdeck LulzBot build</title>'
            f'<style>{CSS}</style></head><body>{"".join(pages)}</body></html>')
    src = BUILD / "build.html"
    src.write_text(html)
    subprocess.run(["chromium", "--headless", "--disable-gpu", "--no-pdf-header-footer", "--allow-file-access-from-files",
                    f"--print-to-pdf={OUT}", src.as_uri()], check=True, capture_output=True)
    print("wrote", OUT, f"({len(pages)} pages)")


if __name__ == "__main__":
    main()
