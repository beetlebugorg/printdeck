#!/usr/bin/env python3
"""TAZ 6 BLTouch cable: write the wiring guide PDF (taz6-bltouch-cable.pdf). Needs Chromium.

The pin facts are from the UltiMachine RAMBo 1.4a design files (github.com/ultimachine/RAMBo-1.4):
sheet 2 (Motor Extensions, X20 = MX1) and sheet 3 (Endstops, X21 = Z-MIN). The two board pictures
are crops of "PCB 3D Print (top)_RAMBo_1.4a.PDF" from the same files.

  python3 make_pdf.py
"""

import base64
import math
import subprocess
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "taz6-bltouch-cable.pdf"

NAVY, ORANGE = "#1F2B4D", "#E8742A"
WIRE = {"brown": "#7A4B2A", "red": "#D22B2B", "orange": "#FF8C1A", "black": "#1A1A1A", "white": "#FFFFFF"}


def img(name):
    return "data:image/png;base64," + base64.b64encode((HERE / name).read_bytes()).decode()


DARK = {"brown": "#40250F", "red": "#7E1010", "orange": "#A65200", "black": "#000000", "white": "#7A7A7A"}
P = 24    # the 2.54 mm pitch in the drawings
L = 112   # the length of a Dupont housing in the drawings

DEFS = """<defs>
<linearGradient id="gH" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#4A4A4A"/>
<stop offset=".4" stop-color="#2B2B2B"/><stop offset="1" stop-color="#121212"/></linearGradient>
<linearGradient id="gV" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#4A4A4A"/>
<stop offset=".4" stop-color="#2B2B2B"/><stop offset="1" stop-color="#121212"/></linearGradient>
<linearGradient id="gGold" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#F7DC8E"/>
<stop offset=".5" stop-color="#C99A3A"/><stop offset="1" stop-color="#8A6620"/></linearGradient>
<linearGradient id="gGoldV" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#F7DC8E"/>
<stop offset=".5" stop-color="#C99A3A"/><stop offset="1" stop-color="#8A6620"/></linearGradient>
<linearGradient id="gTin" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#FAFAFA"/>
<stop offset=".45" stop-color="#B5B5B5"/><stop offset="1" stop-color="#DADADA"/></linearGradient>
<linearGradient id="gCu" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#F0A060"/>
<stop offset=".5" stop-color="#B8642A"/><stop offset="1" stop-color="#D9864A"/></linearGradient>
<linearGradient id="gPcb" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#2F8550"/>
<stop offset="1" stop-color="#17502C"/></linearGradient>
<linearGradient id="gProbe" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#FFFFFF"/>
<stop offset=".6" stop-color="#ECEEF3"/><stop offset="1" stop-color="#CDD2DC"/></linearGradient>
</defs>"""


_SVG = [0]


def uniq(svg):
    """Give the gradients of one SVG their own ids. Chromium mixes up equal ids in different SVGs."""
    _SVG[0] += 1
    n = _SVG[0]
    return svg.replace('id="g', f'id="s{n}g').replace("url(#g", f"url(#s{n}g")


def tube(color, d, w=7):
    """A wire with its insulation: a dark edge, the color, and a highlight."""
    cap = 'fill="none" stroke-linecap="round" stroke-linejoin="round"'
    return (f'<path d="{d}" {cap} stroke="{DARK[color]}" stroke-width="{w + 2}"/>'
            f'<path d="{d}" {cap} stroke="{WIRE[color]}" stroke-width="{w}"/>'
            f'<path d="{d}" {cap} stroke="#FFF" stroke-opacity=".38" stroke-width="{w * 0.3:.1f}"/>')


def twist(colors, x0, x1, yc, starts, ends, amp, period=42, w=7):
    """Wires twisted together from x0 to x1. Each wire starts at yc + starts[i] and stops at yc + ends[i].
    The parts of a wire at the back of the twist are drawn first."""
    back, front = "", ""
    n = len(colors)
    for i, color in enumerate(colors):
        chunks, pts, side = [], [], None
        for k in range(161):
            t = k / 160
            x = x0 + (x1 - x0) * t
            r = min(1.0, t / 0.2, (1 - t) / 0.2)
            r = r * r * (3 - 2 * r)
            a = 2 * math.pi * (x - x0) / period + 2 * math.pi * i / n
            y = yc + (starts[i] + (ends[i] - starts[i]) * t) * (1 - r) + amp * math.sin(a) * r
            now = math.cos(a) > 0
            if side is not None and now != side:
                chunks.append((side, pts))
                pts = pts[-1:]
            side = now
            pts.append(f"{x:.1f} {y:.1f}")
        chunks.append((side, pts))
        for is_front, pts in chunks:
            d = "M " + " L ".join(pts)
            if is_front:
                front += tube(color, d, w)
            else:
                back += tube(color, d, w)
    return back + front


def latch(x, y, w, h):
    """The window in a Dupont housing, with the tab of the terminal in it."""
    return (f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="1.5" fill="#060606"/>'
            f'<rect x="{x + 2.5}" y="{y + 2.5}" width="{w - 5}" height="{h - 5}" rx="1" fill="#C9C9C9" opacity=".85"/>')


def hplug(x, y, colors, male=False, face="right", plus=None):
    """A Dupont housing on its side, seen from the wide face. The cells go down from y. The mating face
    is on the right or on the left. Male housings have gold pins out of the mating face."""
    n = len(colors)
    h = n * P
    out = ""
    if male:
        for i in range(n):
            cy = y + i * P + P / 2
            px = x + L - 4 if face == "right" else x - 40
            out += f'<rect x="{px}" y="{cy - 3}" width="44" height="6" rx="1" fill="#D2A33E"/>'
    out += f'<rect x="{x}" y="{y}" width="{L}" height="{h}" rx="3" fill="#232323" stroke="#000" stroke-width="1"/>'
    for i in range(n):
        out += f'<rect x="{x + 2}" y="{y + i * P + 2}" width="{L - 4}" height="5" rx="2" fill="#3E3E3E"/>'
    fx = x + L - 7 if face == "right" else x
    out += f'<rect x="{fx}" y="{y}" width="7" height="{h}" fill="#000" opacity=".35"/>'
    for i in range(1, n):
        out += f'<line x1="{x}" y1="{y + i * P}" x2="{x + L}" y2="{y + i * P}" stroke="#000" stroke-width="1.6"/>'
        out += f'<line x1="{x}" y1="{y + i * P + 1.6}" x2="{x + L}" y2="{y + i * P + 1.6}" stroke="#555" stroke-width=".8"/>'
    for i in range(n):
        lx = x + (L * 0.48 if face == "right" else L * 0.32)
        out += latch(lx, y + i * P + 6, 20, P - 12)
    if plus is not None:
        out += (f'<rect x="{x + 10}" y="{y + plus * P + 4}" width="16" height="{P - 8}" rx="2" fill="#fff"/>'
                f'<text x="{x + 18}" y="{y + plus * P + P / 2 + 5}" class="plus">+</text>')
    return out


def vplug(x, ybot, colors, plus=None):
    """A Dupont housing down on a pin header, seen from the side. The wires go in at the top."""
    n = len(colors)
    w = n * P
    y = ybot - L
    out = f'<rect x="{x}" y="{y}" width="{w}" height="{L}" rx="3" fill="#232323" stroke="#000" stroke-width="1"/>'
    out += f'<rect x="{x + 2}" y="{y + 2}" width="6" height="{L - 4}" rx="2" fill="#3E3E3E"/>'
    out += f'<rect x="{x}" y="{ybot - 7}" width="{w}" height="7" fill="#000" opacity=".35"/>'
    for i in range(1, n):
        out += f'<line x1="{x + i * P}" y1="{y}" x2="{x + i * P}" y2="{ybot}" stroke="#000" stroke-width="1.6"/>'
        out += f'<line x1="{x + i * P + 1.6}" y1="{y}" x2="{x + i * P + 1.6}" y2="{ybot}" stroke="#555" stroke-width=".8"/>'
    for i, c in enumerate(colors):
        if c:
            out += latch(x + i * P + 6, y + L * 0.42, P - 12, 22)
    if plus is not None:
        out += (f'<rect x="{x + plus * P + 4}" y="{y + 8}" width="{P - 8}" height="18" rx="2" fill="#fff"/>'
                f'<text x="{x + plus * P + P / 2}" y="{y + 22}" class="plus">+</text>')
    return out


def header(x, ybase, n):
    """A straight pin header on the board, seen from the side: gold pins in a black base."""
    out = ""
    for i in range(n):
        cx = x + i * P + P / 2
        out += f'<rect x="{cx - 3}" y="{ybase - 60}" width="6" height="{60 + 24 + 30}" rx="1.2" fill="#D2A33E"/>'
        out += f'<ellipse cx="{cx}" cy="{ybase + 24 + 20}" rx="8" ry="4" fill="#C9C9C9"/>'
    out += f'<rect x="{x}" y="{ybase}" width="{n * P}" height="24" rx="2" fill="#1B1B1B"/>'
    out += f'<line x1="{x + 1}" y1="{ybase + 1.5}" x2="{x + n * P - 1}" y2="{ybase + 1.5}" stroke="#555" stroke-width="1.5"/>'
    return out


def bltouch(x, y):
    """The BLTouch, seen from the front."""
    return (f'<rect x="{x}" y="{y}" width="140" height="22" rx="11" fill="#F2F3F7" stroke="#8C95A8" stroke-width="1.5"/>'
            f'<circle cx="{x + 16}" cy="{y + 11}" r="6" fill="#fff" stroke="#8C95A8" stroke-width="1.5"/>'
            f'<circle cx="{x + 124}" cy="{y + 11}" r="6" fill="#fff" stroke="#8C95A8" stroke-width="1.5"/>'
            f'<rect x="{x + 32}" y="{y + 22}" width="76" height="118" rx="8" fill="#F2F3F7" stroke="#8C95A8" stroke-width="1.5"/>'
            f'<circle cx="{x + 70}" cy="{y + 44}" r="5" fill="#E8202A"/>'
            f'<circle cx="{x + 70}" cy="{y + 44}" r="10" fill="#E8202A" opacity=".2"/>'
            f'<text x="{x + 70}" y="{y + 92}" class="probe">BL</text>'
            f'<text x="{x + 70}" y="{y + 114}" class="probe">Touch</text>'
            f'<rect x="{x + 46}" y="{y + 140}" width="48" height="30" rx="4" fill="#F2F3F7" stroke="#8C95A8" stroke-width="1.5"/>'
            f'<rect x="{x + 67}" y="{y + 170}" width="6" height="40" rx="2" fill="#C9C9C9" stroke="#888" stroke-width=".8"/>')


def overview():
    """The cable from the BLTouch to Z-MIN and MX1, seen from the side, with the cross of brown and red."""
    s = f'<svg viewBox="0 20 1000 540" xmlns="http://www.w3.org/2000/svg" class="diagram">{DEFS}'
    # The leads of the BLTouch to its two plugs.
    s += tube("brown", "M 62 268 C 62 150, 120 82, 200 82", 5)
    s += tube("red", "M 74 268 C 74 160, 130 106, 200 106", 5)
    s += tube("orange", "M 86 268 C 86 170, 140 130, 200 130", 5)
    s += tube("black", "M 98 268 C 98 215, 150 192, 200 192", 5)
    s += tube("white", "M 110 268 C 110 235, 160 216, 200 216", 5)
    s += bltouch(20, 250)
    # The plugs at the probe end: the BLTouch leads on the left, the cable on the right.
    s += hplug(200, 70, ["brown", "red", "orange"])
    s += hplug(200, 180, ["black", "white"])
    s += hplug(324, 70, ["brown", "red", "orange"], male=True, face="left")
    s += hplug(324, 180, ["black", "white"], male=True, face="left")
    s += '<text x="256" y="56" class="small">BLTouch leads</text><text x="380" y="56" class="small">cable</text>'
    # The two twisted groups.
    s += twist(["brown", "red", "orange"], 436, 600, 106, [-24, 0, 24], [-24, 0, 24], 15)
    s += twist(["black", "white"], 436, 600, 204, [-12, 12], [12, -12], 10)
    s += '<text x="518" y="262" class="small">twisted</text>'
    # The board: Z-MIN on the left, MX1 on the right.
    zx, mx, yb = 640, 784, 446
    s += header(zx, yb, 3) + header(mx, yb, 5)
    s += f'<rect x="620" y="470" width="370" height="18" rx="2" fill="#246B3E"/>'
    s += '<text x="975" y="483" class="pcb">RAMBo 1.4</text>'
    # The wires from the twist to the plugs on the board.
    def drop(color, y, x):
        return tube(color, f"M 600 {y} L {x - 40} {y} Q {x} {y} {x} {y + 40} L {x} {yb - L + 4}")
    s += drop("black", 216, zx + P * 1.5) + drop("white", 192, zx + P * 2.5)
    s += drop("orange", 130, mx + P * 2.5) + drop("brown", 82, mx + P * 3.5) + drop("red", 106, mx + P * 4.5)
    s += vplug(zx, yb, [None, "black", "white"])
    s += vplug(mx + 2 * P, yb, ["orange", "brown", "red"], plus=2)
    # The cross.
    s += f'<circle cx="{mx + P * 4}" cy="113" r="34" fill="none" stroke="{ORANGE}" stroke-width="3" stroke-dasharray="6 4"/>'
    s += f'<text x="{mx + P * 4 - 44}" y="40" class="callout">cross brown and red</text>'
    # The marks of the board under each pin.
    for i, (m, k) in enumerate([("+", ""), ("−", ""), ("S", "PB4")]):
        cx = zx + i * P + P / 2
        s += f'<text x="{cx}" y="512" class="mark">{m}</text><text x="{cx}" y="530" class="pin">{k}</text>'
    for i, (m, k) in enumerate([("5", ""), ("4", ""), ("S", "PA0"), ("−", ""), ("+", "")]):
        cx = mx + i * P + P / 2
        s += f'<text x="{cx}" y="512" class="mark">{m}</text><text x="{cx}" y="530" class="pin">{k}</text>'
    s += f'<text x="{zx + 1.5 * P}" y="554" class="htitle">Z-MIN</text>'
    s += f'<text x="{mx + 2.5 * P}" y="554" class="htitle">MX1</text>'
    s += "</svg>"
    return uniq(s)


def terminals():
    """The two Dupont crimp terminals, larger than real, with a wire in each."""
    s = f'<svg viewBox="0 0 1000 320" xmlns="http://www.w3.org/2000/svg" class="diagram">{DEFS}'
    for row, (y, male, name) in enumerate([(40, False, "Socket (female)"), (180, True, "Pin (male)")]):
        cy = y + 40
        # The contact end.
        if male:
            s += f'<rect x="60" y="{cy - 7}" width="120" height="14" rx="3" fill="#D2A33E" stroke="#7A5A1C" stroke-width="1"/>'
            s += f'<rect x="170" y="{cy - 22}" width="110" height="44" rx="3" fill="#C9C9C9" stroke="#777" stroke-width="1"/>'
        else:
            s += f'<rect x="60" y="{cy - 22}" width="220" height="44" rx="3" fill="#C9C9C9" stroke="#777" stroke-width="1"/>'
            s += f'<rect x="60" y="{cy - 22}" width="10" height="44" fill="#9A9A9A"/>'
        s += f'<path d="M 200 {cy - 22} L 236 {cy - 40} L 244 {cy - 22} Z" fill="#C9C9C9" stroke="#777" stroke-width="1"/>'
        # The wire, the bare copper, and the two crimps.
        s += tube("red", f"M 420 {cy} L 640 {cy}", 30)
        for k in range(5):
            s += f'<line x1="290" y1="{cy - 10 + k * 5}" x2="372" y2="{cy - 10 + k * 5}" stroke="#C06A30" stroke-width="3.2" stroke-linecap="round"/>'
        s += f'<rect x="280" y="{cy - 6}" width="30" height="12" fill="#C9C9C9"/>'
        s += f'<path d="M 310 {cy - 20} C 330 {cy - 26}, 350 {cy - 26}, 368 {cy - 20} L 368 {cy + 20} C 350 {cy + 26}, 330 {cy + 26}, 310 {cy + 20} Z" fill="#C9C9C9" stroke="#777" stroke-width="1"/>'
        s += f'<rect x="368" y="{cy - 4}" width="40" height="8" fill="#C9C9C9"/>'
        s += f'<path d="M 400 {cy - 26} C 425 {cy - 32}, 450 {cy - 32}, 470 {cy - 26} L 470 {cy + 26} C 450 {cy + 32}, 425 {cy + 32}, 400 {cy + 26} Z" fill="#C9C9C9" stroke="#777" stroke-width="1"/>'
        s += f'<text x="60" y="{y - 8}" class="htitle">{name}</text>'
        # The callouts.
        s += f'<line x1="238" y1="{cy - 40}" x2="290" y2="{cy - 52}" stroke="#999" stroke-width="1.2"/>'
        s += f'<text x="296" y="{cy - 48}" class="smallL">latch tab</text>'
        if row == 1:
            s += f'<line x1="339" y1="{cy + 26}" x2="339" y2="{cy + 52}" stroke="#999" stroke-width="1.2"/>'
            s += f'<text x="250" y="{cy + 68}" class="smallL">wire crimp: bare copper</text>'
            s += f'<line x1="440" y1="{cy + 32}" x2="520" y2="{cy + 52}" stroke="#999" stroke-width="1.2"/>'
            s += f'<text x="500" y="{cy + 68}" class="smallL">insulation crimp: insulation</text>'
    s += '<text x="670" y="76" class="smallL">Strip 2.5 mm of insulation.</text>'
    s += '<text x="670" y="96" class="smallL">The copper must not go past</text><text x="670" y="114" class="smallL">the wire crimp.</text>'
    s += "</svg>"
    return uniq(s)


def cable_end(colors, labels, male=False, plus=None):
    """One finished end of the cable, seen from the wide face, with the latch windows up."""
    n = len(colors)
    s = f'<svg viewBox="0 0 470 {n * P + 30}" xmlns="http://www.w3.org/2000/svg" class="end">{DEFS}'
    for i, c in enumerate(colors):
        if c:
            cy = 15 + i * P + P / 2
            s += tube(c, f"M 10 {cy} C 80 {cy}, 120 {cy}, 200 {cy}")
    s += hplug(190, 15, colors, male=male, face="right", plus=plus)
    for i, text in enumerate(labels):
        s += f'<text x="{190 + L + (54 if male else 14)}" y="{15 + i * P + P / 2 + 5}" class="elabel">{text}</text>'
    s += "</svg>"
    return uniq(s)


def board(name, size, crop, marks, label):
    """A crop of a picture of the board, with the pins of the cable marked. crop is (x, y, w, h) in
    the picture. marks are (x, y, wire color or None for an empty pin, text)."""
    x0, y0, w, h = crop
    s = f'<svg viewBox="{x0} {y0} {w} {h}" xmlns="http://www.w3.org/2000/svg" class="board">'
    s += f'<image href="{img(name)}" x="0" y="0" width="{size[0]}" height="{size[1]}"/>'
    for x, y, color, text in marks:
        if color:
            if color == "white":
                s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="#333" stroke-width="13"/>'
            s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="{WIRE[color]}" stroke-width="9"/>'
        else:
            s += f'<circle cx="{x}" cy="{y}" r="27" fill="none" stroke="{ORANGE}" stroke-width="5" stroke-dasharray="8 6"/>'
        s += f'<text x="{x}" y="{y + 78}" class="bmark">{text}</text>'
    s += f'<rect x="{x0}" y="{y0 + h - 44}" width="{w}" height="44" fill="{NAVY}" opacity="0.9"/>'
    s += f'<text x="{x0 + 16}" y="{y0 + h - 14}" class="blabel">{label}</text>'
    s += "</svg>"
    return uniq(s)


PINS = [
    ("brown", "Brown", "GND", "MX1", "2 (−)", ""),
    ("red", "Red", "+5 V", "MX1", "1 (+)", ""),
    ("orange", "Orange", "Control (servo)", "MX1", "3", "PA0"),
    ("black", "Black", "GND", "Z-MIN", "− (middle)", ""),
    ("white", "White", "Probe signal", "Z-MIN", "S", "PB4"),
]


def pin_table():
    rows = ""
    for key, name, fn, header, pin, kpin in PINS:
        border = "#8A8A8A" if key == "white" else WIRE[key]
        rows += (f'<tr><td><span class="sw" style="background:{WIRE[key]};border-color:{border}"></span>{name}</td>'
                 f"<td>{fn}</td><td>{header}</td><td>{pin}</td><td><code>{kpin}</code></td></tr>")
    rows += ('<tr class="empty"><td><span class="sw none"></span>None</td><td></td><td>Z-MIN</td>'
             '<td>+ (5 V)</td><td></td></tr>')
    rows += ('<tr class="empty"><td><span class="sw none"></span>None</td><td></td><td>MX1</td>'
             '<td>4 and 5</td><td><code>PC5, PL5</code></td></tr>')
    return ('<table class="pins"><thead><tr><th>Wire</th><th>BLTouch function</th><th>RAMBo header</th>'
            f"<th>Pin</th><th>Klipper pin</th></tr></thead><tbody>{rows}</tbody></table>")


def page(body, n):
    return (f'<section class="page">{body}<footer><span>TAZ 6 · BLTouch cable</span>'
            f"<span>{n} / 5</span></footer></section>")


ENDSTOPS = board("rambo-endstops.png", (1250, 650), (20, 110, 760, 330),
                 [(574, 288, None, "+"), (632, 288, "black", "−"), (690, 288, "white", "S")],
                 "Z-MIN · X21 · top row of ENDSTOPS")
MOTEX = board("rambo-motex.png", (900, 750), (150, 240, 520, 330),
              [(436, 432, "red", "+"), (376, 432, "brown", "−"), (316, 432, "orange", "S")],
              "MX1 · bottom row of MOTEX")

P1 = f"""
<header class="cover">
  <div class="kicker">LulzBot TAZ 6 · RAMBo 1.4 · Klipper</div>
  <h1>BLTouch cable</h1>
  <p class="lede">One cable from the BLTouch to two headers on the RAMBo. The three control wires go to
  <b>MX1</b>. The two probe wires go to <b>Z-MIN</b>, in the place of the washer wire.</p>
</header>
<div class="warn">
  <div class="warn-t">Cross the brown and red wires</div>
  <p>The plug of the BLTouch has GND, then 5 V. The MX1 header has 5 V, then GND. A straight 3-wire cable
  connects 5 V to GND. The probe before this one drew 1.39 A from the 5 V rail of the RAMBo. Test the cable
  with a meter before you connect the probe (page 5).</p>
</div>
{overview()}
<p class="caption">Side view. The plugs at the RAMBo end are down on their pins. The board marks and the Klipper pins are under each pin.</p>
{pin_table()}
"""

P2 = f"""
<h2>The two headers on the RAMBo</h2>
<p>The marks are from the silkscreen of the RAMBo 1.4a. Use the marks on the board to find each pin, because
the board can be in the control box in any direction.</p>
<div class="row">
  {MOTEX}
  <div>
    <h3>MX1: control wires</h3>
    <ul>
      <li>MOTEX is a block of three rows of five pins, next to the thermistor headers. MX3 is the top row and
      <b>MX1 is the bottom row</b>.</li>
      <li>Pin 1 is at the end with the <b>1</b> and <b>+</b> marks. Pin 2 has the <b>−</b> mark.</li>
      <li>Pin 1 is 5 V. Pin 2 is GND. Pin 3 is PA0, through a 120 Ω filter.</li>
      <li>Pins 4 and 5 (PC5, PL5) stay empty.</li>
      <li>If the holes of MOTEX have no pins, solder a 1×3 straight pin header into pins 1 to 3 of MX1.</li>
    </ul>
  </div>
</div>
<div class="row">
  {ENDSTOPS}
  <div>
    <h3>Z-MIN: probe wires</h3>
    <ul>
      <li>The MIN headers are the top row of ENDSTOPS: X-MIN, Y-MIN, then <b>Z-MIN</b> (X21).</li>
      <li>The marks are <b>+ − S</b>. S is pin 1. The washer wire is in this header now.</li>
      <li>S goes to PB4. The board has a 10 kΩ pull-up to 5 V, a 120 Ω filter and a 100 pF capacitor on it.</li>
      <li>The + pin (5 V) stays empty. The BLTouch gets its power from MX1.</li>
    </ul>
  </div>
</div>
<p class="note">Source: UltiMachine RAMBo 1.4a design files, github.com/ultimachine/RAMBo-1.4. Sheet 2, Motor
Extensions (X20 = MX1). Sheet 3, Endstops (X21 = Z-MIN). The pictures are crops of the 3D print of the top of
the board.</p>
"""

P3 = f"""
<h2>Make the cable</h2>
<div class="two">
  <div>
    <h3>Parts</h3>
    <table class="parts">
      <tr><td>5</td><td>Stranded wires, 24 or 26 AWG, in brown, red, orange, black and white. The length is in step 1.</td></tr>
      <tr><td>1</td><td>Dupont 1×3 housing and 1 Dupont 1×2 housing, for the probe end.</td></tr>
      <tr><td>5</td><td>Dupont crimp pins (male), for the probe end. Look at the BLTouch leads first. If they end in pins, use sockets here.</td></tr>
      <tr><td>1</td><td>Dupont 1×3 housing and 3 crimp sockets (female), for MX1.</td></tr>
      <tr><td>1</td><td>3-position housing and 2 crimp terminals that fit Z-MIN. Use the plug of the washer wire as the sample. I did not check the part number of this housing.</td></tr>
      <tr><td></td><td>Heat-shrink tube, braided sleeve (optional), zip ties, a label for each board end.</td></tr>
    </table>
    <h3>Tools</h3>
    <ul class="tight">
      <li>Dupont crimp tool and a wire stripper.</li>
      <li>Multimeter, with continuity and DC volts.</li>
      <li>Heat gun or lighter for the heat-shrink tube.</li>
    </ul>
  </div>
  <div>
    <h3>Steps</h3>
    <ol class="steps">
      <li><b>Measure the path.</b> Move the toolhead to the end of X and to the top of Z. Lay a string from
      the BLTouch mount along the toolhead cable to the RAMBo. Add 200 mm to that length.</li>
      <li><b>Cut five wires</b> to that length.</li>
      <li><b>Twist the wires in two groups.</b> Twist black and white together, about one turn each 25 mm.
      Twist brown, red and orange together. The twist decreases noise on the probe signal.</li>
      <li><b>Probe end.</b> Crimp the pins. Put brown, red, orange in the 1×3 housing and black, white in the
      1×2 housing, in the same order as the BLTouch plugs. Push each pair together and make sure that each
      color meets the same color.</li>
      <li><b>MX1 end.</b> Crimp the sockets. Put <b>red, brown, orange</b> in the 1×3 housing. Mark the red
      end of the housing <b>+</b>.</li>
      <li><b>Z-MIN end.</b> Crimp the terminals. Put them in the 3-position housing as <b>empty, black,
      white</b>, for <b>+ − S</b>. Compare with the washer plug.</li>
      <li><b>Finish.</b> Put a label on each board end: "MX1 +" and "Z-MIN". Shrink the tube on the ends, and
      put on the sleeve.</li>
    </ol>
  </div>
</div>
<h3>Route the cable</h3>
<ul class="tight">
  <li>Tie the cable to the toolhead cable each 100 mm. Keep a loop at the toolhead.</li>
  <li>Keep the cable away from the heater wires where you can.</li>
  <li>Before the last ties, move X and Z through their full travel by hand. Make sure that the cable does not
  pull or catch.</li>
</ul>
"""

PC = f"""
<h2>The connectors</h2>
<p>All the plugs are Dupont housings with 2.54 mm pitch. Each wire gets a crimp terminal. The terminal locks in
the housing with its latch tab.</p>
{terminals()}
<ul class="tight">
  <li>Crimp the wire crimp first, then the insulation crimp. Pull on the wire: it must not move.</li>
  <li>Push the terminal into the housing with the latch tab on the side of the window, until it clicks.</li>
  <li>To remove a terminal, lift the window tab with a pin, then pull the wire.</li>
</ul>
<h3>The four finished ends</h3>
<p>Each end is shown with the latch windows up and the mating face on the right. Compare your cable with these
before you connect it.</p>
<div class="ends">
  <figure>{cable_end(["brown", "red", "orange"], ["GND", "5 V", "control"], male=True)}
  <figcaption><b>Probe end, 1×3</b> · to the BLTouch control plug</figcaption></figure>
  <figure>{cable_end(["black", "white"], ["GND", "signal"], male=True)}
  <figcaption><b>Probe end, 1×2</b> · to the BLTouch probe plug</figcaption></figure>
  <figure>{cable_end(["red", "brown", "orange"], ["+ (pin 1)", "− (pin 2)", "S (pin 3, PA0)"], plus=0)}
  <figcaption><b>MX1 end, 1×3</b> · red and brown crossed · + mark on the red end</figcaption></figure>
  <figure>{cable_end([None, "black", "white"], ["+ (empty)", "−", "S (PB4)"])}
  <figcaption><b>Z-MIN end, 3-position</b> · the + position stays empty</figcaption></figure>
</div>
<p class="note">The Z-MIN housing must fit the header like the washer plug. If the washer plug is not a Dupont
housing, use a housing of the same type as the washer plug.</p>
"""

P4 = """
<h2>Test before the probe goes on</h2>
<ol class="steps big">
  <li><b>Continuity.</b> With the power off, check each wire from end to end. Then check each pair of
  wires next to each other in each plug: the meter must show an open circuit.</li>
  <li><b>The probe.</b> With the probe off the cable, measure the resistance from red to brown on the
  BLTouch. A reading of a few ohms or less is a short: do not use the probe. I do not have the normal value
  for this probe.</li>
  <li><b>Connect the board end.</b> With the power off, take the washer plug out of Z-MIN. Push the cable
  into Z-MIN and MX1, with the + mark on the + pin.</li>
  <li><b>Measure the voltage at the probe end.</b> Turn on the power, with the probe still off the cable.
  Put the red lead of the meter on the red wire and the black lead on the brown wire of the 1×3 plug.
  <ul>
    <li><b>+5.0 V:</b> correct.</li>
    <li><b>−5.0 V:</b> red and brown are the wrong way round. Turn off the power and correct the MX1 plug.</li>
    <li><b>0 V:</b> the plug is on the wrong pins, or MX1 has no power. Do not continue.</li>
  </ul>
  Then measure from white to black on the 1×2 plug. The pull-up on the board gives about 5 V.</li>
  <li><b>Connect the probe.</b> Turn off the power, connect the BLTouch, then turn on the power.
  <ul>
    <li><b>Correct:</b> the pin goes down and up two times, then the light stays on. The LCD stays bright.</li>
    <li><b>Stop:</b> the LCD goes dim, the probe does not light, or the probe gets warm. Turn off the power
    immediately.</li>
  </ul></li>
</ol>
<div class="next">
  <div class="next-t">Then the config</div>
  <p>Tell Claude that the probe passed. The <code>[bltouch]</code> section replaces the washer
  <code>[probe]</code>. The points of <code>[bed_tilt]</code> and the PRINT_START macro change too. The
  config is saved only when you say go, and the first test is a self-test with no movement.</p>
<pre>[bltouch]
sensor_pin: ^PB4
control_pin: PA0
x_offset: ...     # measure from the nozzle to the pin
y_offset: ...
#z_offset: set with PROBE_CALIBRATE</pre>
</div>
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
.lede {{ font-size: 12pt; margin: 0; color: #DDE2EE; max-width: 6.4in; }}
h2 {{ font-size: 18pt; color: {NAVY}; margin: 0 0 10px; border-bottom: 3px solid {ORANGE}; padding-bottom: 6px; }}
h3 {{ font-size: 11.5pt; color: {NAVY}; margin: 12px 0 4px; }}
.warn {{ border-left: 6px solid {ORANGE}; background: #FFF3EA; padding: 10px 14px; margin-bottom: 8px; }}
.warn-t {{ font-weight: 700; color: #B4501A; font-size: 12pt; }}
.warn p {{ margin: 4px 0 0; }}
.diagram {{ width: 100%; height: auto; display: block; margin: 4px 0 0; }}
.diagram text {{ font-family: "JetBrainsMono Nerd Font", monospace; }}
.diagram .mark {{ fill: #333; font-size: 17px; font-weight: 700; text-anchor: middle; }}
.diagram .htitle {{ fill: {NAVY}; font-size: 16px; font-weight: 700; }}
.diagram .hsub {{ fill: #777; font-size: 13px; }}
.diagram .probe {{ fill: {NAVY}; font-size: 18px; font-weight: 700; text-anchor: middle; }}
.diagram .callout {{ fill: {ORANGE}; font-size: 15px; font-weight: 700; text-anchor: middle; }}
.diagram .small {{ fill: #666; font-size: 13px; text-anchor: middle; }}
.diagram .smallL {{ fill: #555; font-size: 14px; }}
.diagram .pcb {{ fill: #fff; font-size: 11px; font-weight: 700; text-anchor: end; }}
.plus {{ font: 700 15px "JetBrainsMono Nerd Font", monospace; fill: {NAVY}; text-anchor: middle; }}
.ends {{ display: grid; grid-template-columns: 1fr 1fr; gap: 6px 22px; }}
.ends figure {{ margin: 0; }}
.end {{ width: 100%; height: auto; display: block; }}
.end .elabel {{ font: 700 14px "JetBrainsMono Nerd Font", monospace; fill: #333; }}
.ends figcaption {{ font-size: 9pt; color: #555; margin-top: 2px; }}
.diagram .pin {{ fill: {ORANGE}; font-size: 14px; font-weight: 700; text-anchor: middle; }}
.caption {{ font-size: 8.5pt; color: #777; margin: 0 0 10px; text-align: center; }}
table {{ border-collapse: collapse; width: 100%; }}
.pins th {{ text-align: left; font-size: 8.5pt; text-transform: uppercase; letter-spacing: .06em; color: #fff;
  background: {NAVY}; padding: 6px 8px; }}
.pins td {{ padding: 3px 8px; border-bottom: 1px solid #e3e3e3; }}
.pins tr.empty td {{ color: #999; }}
.sw {{ display: inline-block; width: 14px; height: 14px; border-radius: 50%; border: 2px solid; vertical-align: -2px;
  margin-right: 8px; }}
.sw.none {{ border: 2px dashed #aaa; }}
code, pre {{ font-family: "JetBrainsMono Nerd Font", monospace; font-size: 9pt; }}
.two {{ display: grid; grid-template-columns: 1fr 1fr; gap: 22px; }}
.board {{ width: 100%; height: auto; display: block; border-radius: 6px; }}
.row {{ display: grid; grid-template-columns: 1.15fr 1fr; gap: 20px; align-items: start; margin: 14px 0 18px; }}
.row h3 {{ margin-top: 0; }}
.board .bmark {{ font: 700 36px "JetBrainsMono Nerd Font", monospace; fill: #fff; text-anchor: middle;
  paint-order: stroke; stroke: {NAVY}; stroke-width: 9px; }}
.board .blabel {{ font: 700 20px "JetBrainsMono Nerd Font", monospace; fill: #fff; }}
ul, ol {{ margin: 4px 0; padding-left: 18px; }}
li {{ margin: 3px 0; }}
.tight li {{ margin: 1px 0; }}
.note {{ font-size: 8.5pt; color: #777; margin-top: 14px; }}
.parts td {{ padding: 4px 6px; border-bottom: 1px solid #eee; vertical-align: top; }}
.parts td:first-child {{ width: 26px; font-weight: 700; color: {ORANGE}; }}
ol.steps {{ counter-reset: s; list-style: none; padding-left: 0; }}
ol.steps > li {{ counter-increment: s; position: relative; padding-left: 34px; margin: 7px 0; }}
ol.steps > li::before {{ content: counter(s); position: absolute; left: 0; top: 0; width: 24px; height: 24px;
  border-radius: 50%; background: {NAVY}; color: #fff; font: 700 10pt "JetBrainsMono Nerd Font", monospace;
  display: flex; align-items: center; justify-content: center; }}
ol.big > li {{ margin: 12px 0; }}
.next {{ background: #EEF1F7; border-radius: 8px; padding: 12px 16px; margin-top: 16px; }}
.next-t {{ font-weight: 700; color: {NAVY}; font-size: 12pt; }}
.next p {{ margin: 4px 0 8px; }}
pre {{ background: {NAVY}; color: #E9ECF4; padding: 10px 14px; border-radius: 6px; margin: 0; }}
"""


def main():
    html = (f'<!doctype html><html><head><meta charset="utf-8"><title>TAZ 6 BLTouch cable</title>'
            f"<style>{CSS}</style></head><body>"
            + page(P1, 1) + page(P2, 2) + page(P3, 3) + page(PC, 4) + page(P4, 5) + "</body></html>")
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / "guide.html"
        src.write_text(html)
        subprocess.run(["chromium", "--headless", "--no-pdf-header-footer", f"--print-to-pdf={OUT}", src.as_uri()],
                       check=True, capture_output=True)
    print(OUT)


if __name__ == "__main__":
    main()
