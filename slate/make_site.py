#!/usr/bin/env python3
"""Printdeck: write the project website (docs/index.html and docs/img/) in the style of beetlebug.org.
Run after lineart/render.mjs. The text comes from make_pdf.py, so the site and the PDF match.
The Pages workflow adds the STL files and the PDF under files/."""

import html
import shutil
import subprocess
from pathlib import Path

import make_pdf as P

HERE = Path(__file__).resolve().parent
DOCS = HERE.parent / "docs"
IMG = DOCS / "img"

SHOTS = ["hero", "rear", "exploded", "connect"] + [s[0] for s in P.STEPS] + [f"part_{n}" for n, _, _ in P.PRINTS]

CSS = """
:root {
  --bg: #fbfbf9; --raised: #ffffff; --ink: #16181a; --ink-2: #4a4f55; --ink-3: #80868d;
  --line: #e2e2dc; --accent: #0a7f63; --accent-soft: #e3f3ee; --grid: rgba(22, 24, 26, 0.045);
  --sans: "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --mono: "JetBrains Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f1112; --raised: #161a1c; --ink: #eceeed; --ink-2: #b2b8b6; --ink-3: #7c8482;
    --line: #262b2d; --accent: #3fd1a6; --accent-soft: #10302a; --grid: rgba(236, 238, 237, 0.05);
    color-scheme: dark;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--ink); font: 16px/1.6 var(--sans); }
a { color: var(--accent); }
main { max-width: 1080px; margin: 0 auto; padding: 0 16px 80px; }
header { max-width: 1080px; margin: 0 auto; padding: 48px 16px 8px; }
.eyebrow { font: 600 0.75rem var(--mono); text-transform: uppercase; letter-spacing: 0.08em; color: var(--accent); margin: 0 0 8px; }
h1 { font-size: clamp(2.2rem, 5vw, 3.4rem); line-height: 1.05; letter-spacing: -0.03em; margin: 0; }
h2 { font-size: 1.7rem; letter-spacing: -0.02em; margin: 64px 0 8px; }
h3 { margin: 0 0 6px; font-size: 1.1rem; }
.lede { font-size: 1.15rem; color: var(--ink-2); max-width: 46rem; }
.grid-bg { background-color: var(--bg); border: 1px solid var(--line); border-radius: 10px;
  background-image: linear-gradient(var(--grid) 1px, transparent 1px), linear-gradient(90deg, var(--grid) 1px, transparent 1px);
  background-size: 24px 24px; }
picture img { display: block; width: 100%; height: auto; }
.hero { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin-top: 24px; }
.hero picture { padding: 24px; }
.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 24px 0 0; padding: 16px 0; border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
.stats b { display: block; font-size: 1.3rem; letter-spacing: -0.02em; }
.stats span { color: var(--ink-3); font: 500 0.75rem var(--mono); text-transform: uppercase; letter-spacing: 0.06em; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
.chip { font: 600 0.75rem var(--mono); color: var(--ink-2); border: 1px solid var(--line); border-radius: 999px; padding: 4px 10px; background: var(--raised); }
.btn { display: inline-block; font: 600 0.8rem var(--mono); color: var(--accent); border: 1px solid var(--accent); border-radius: 999px; padding: 6px 14px; text-decoration: none; background: var(--raised); margin: 0 8px 8px 0; }
.btn:hover { background: var(--accent-soft); }
.cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; }
.card { background: var(--raised); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; font-size: 0.9rem; }
.card picture { padding: 12px; border: 0; border-radius: 0; border-bottom: 1px solid var(--line); }
.card picture img { height: 120px; object-fit: contain; }
.card div { padding: 12px; color: var(--ink-2); }
.card b { color: var(--ink); }
.step { display: grid; grid-template-columns: 1.1fr 1fr; gap: 24px; align-items: start; padding: 32px 0; border-top: 1px solid var(--line); }
.step picture { padding: 16px; }
.step .num { font: 800 3rem/1 var(--mono); color: var(--accent); }
.step ol { padding-left: 20px; color: var(--ink-2); }
.step li { margin-bottom: 6px; }
.note { border: 1px solid var(--accent); background: var(--accent-soft); border-radius: 10px; padding: 10px 14px; font-size: 0.9rem; }
table { border-collapse: collapse; width: 100%; font-size: 0.9rem; }
td { border-bottom: 1px solid var(--line); padding: 6px 8px 6px 0; vertical-align: top; }
td.q { font: 700 0.85rem var(--mono); white-space: nowrap; width: 3.5rem; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 32px; }
code { font: 0.85em var(--mono); }
.shots { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 24px 0; }
.shots img { width: 100%; height: auto; border: 1px solid var(--line); border-radius: 10px; }
img.plain { width: 100%; height: auto; background: #ffffff; border: 1px solid var(--line); border-radius: 10px; padding: 16px; box-sizing: border-box; }
footer { max-width: 1080px; margin: 0 auto; padding: 24px 16px 48px; color: var(--ink-3); font: 0.8rem var(--mono); border-top: 1px solid var(--line); }
@media (max-width: 760px) {
  .hero, .step, .two { grid-template-columns: 1fr; }
  .stats, .cards, .shots { grid-template-columns: 1fr 1fr; }
}
"""


def copy_images():
    IMG.mkdir(parents=True, exist_ok=True)
    for name in SHOTS:
        for theme in ("light", "dark"):
            src = P.IMG / f"{name}-{theme}.png"
            subprocess.run(["magick", str(src), "-resize", "1200x1200>", "-strip", str(IMG / src.name)], check=True)
    # The mount for the LulzBot TAZ Workhorse, on the printer.
    src = HERE.parent / "lulzbot" / "renders" / "printer-front-left.png"
    subprocess.run(["magick", str(src), "-resize", "1200x1200>", "-strip", str(IMG / "lulzbot-printer.png")], check=True)


def pic(name, alt):
    return (f'<picture class="grid-bg"><source media="(prefers-color-scheme: dark)" srcset="img/{name}-dark.png">'
            f'<img src="img/{name}-light.png" alt="{html.escape(alt)}"></picture>')


def page():
    total = sum(P.FILAMENT[n]["grams"] for n, _, _ in P.PRINTS)
    size = "132 × 221 × 35 mm"
    cards = "".join(
        f'<div class="card">{pic("part_" + n, n)}<div><b>{n.capitalize()}</b> · {c} PLA · {P.FILAMENT[n]["grams"]:.0f} g<br>{t}</div></div>'
        for n, c, t in P.PRINTS)
    steps = []
    for i, (shot, title, need, items, note) in enumerate(P.STEPS, 1):
        chips = "".join(f'<span class="chip">{html.escape(x)}</span>' for x in need)
        lis = "".join(f"<li>{t}</li>" for t in items)
        nt = f'<p class="note"><b>Note.</b> {note}</p>' if note else ""
        steps.append(f'<section class="step">{pic(shot, f"Step {i}: {title}")}<div><div class="num">{i}</div>'
                     f'<h3>{title}</h3><div class="chips">{chips}</div><ol>{lis}</ol>{nt}</div></section>')
    hw = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in P.HARDWARE)
    el = "".join(f'<tr><td class="q">{q}×</td><td>{t}</td></tr>' for q, t in P.ELECTRONICS)
    st = "".join(f"<tr><td><b>{k}</b></td><td>{v}</td></tr>" for k, v in P.SETTINGS)
    files = "".join(f'<a class="btn" href="files/{n}.stl">{n}.stl</a>' for n, _, _ in P.PRINTS)
    repo = "https://github.com/beetlebugorg/printdeck"
    shots = "".join(f'<img src="manual/img/{n}.png" alt="{html.escape(a)}">' for n, a in (
        ("printing", "A print with the model up to the layer that prints now"),
        ("first-layer", "The first layer with the Z offset"),
        ("file", "A file with its print time, filament, roll and cost"),
        ("filament", "The Filament page with the roll in the printer")))
    lulzbot = '<img class="plain" src="img/lulzbot-printer.png" alt="The Printdeck mount on the front left of a LulzBot TAZ Workhorse">'
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Printdeck</title>
<meta name="description" content="A Raspberry Pi 5 desk slab that runs Klipper for the 3D printer. Four printed parts, no supports.">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700;800&family=JetBrains+Mono:wght@500;600;800&display=swap" rel="stylesheet">
<style>{CSS}</style>
</head>
<body>
<header>
  <p class="eyebrow">Cyberdeck · Raspberry Pi 5</p>
  <h1>Printdeck</h1>
  <p class="lede">A desk slab next to the 3D printer. It runs Klipper, and shows and controls the print on a 7" touch
    screen. The Pi 5 is on the rear, so every port is open.</p>
  <a class="btn" href="{repo}/releases/latest">Download Printdeck OS</a>
  <a class="btn" href="manual/printdeck-manual.pdf">Manual (PDF)</a>
  <a class="btn" href="files/printdeck-slate-build.pdf">Build instructions (PDF)</a>
  <a class="btn" href="{repo}">Source on GitHub</a>
</header>
<main>
  <div class="hero">{pic("hero", "The Printdeck from the front")}{pic("rear", "The Printdeck from the rear, with the Pi and the cables")}</div>
  <div class="stats">
    <div><b>{size}</b><span>Size</span></div>
    <div><b>{len(P.PRINTS)} parts · {total:.0f} g</b><span>Printed</span></div>
    <div><b>${P.cost(total):.2f}</b><span>Filament at ${P.PRICE_PER_KG}/kg</span></div>
    <div><b>No supports</b><span>All parts</span></div>
  </div>

  <h2>Print</h2>
  <p>The STL files are in their print orientation. Load them, do not turn them.</p>
  <div class="cards">{cards}</div>
  <p style="margin-top:16px">{files}</p>
  <table>{st}</table>

  <h2>Parts</h2>
  <div class="two"><div><h3>Hardware</h3><table>{hw}</table></div><div><h3>Electronics</h3><table>{el}</table></div></div>

  <h2>Build</h2>
  <p>Left and right are as seen from the front of the case.</p>
  {"".join(steps)}

  <h2>Connect</h2>
  <div class="step">{pic("connect", "The plugs for the printer and the power")}<div><ol>
    <li>Connect the USB-C power supply to the Pi. The port is on the left edge of the Pi.</li>
    <li>Connect the 3D printer to the black USB 2 port next to the board, from above.</li></ol></div></div>

  <h2>Software</h2>
  <p>The Printdeck runs Printdeck OS: one Go program that is the Klipper host, the touch screen, the display of
    the printer, and the web pages. The touch screen shows a 3D view of the print, the temperatures, and the printer
    controls. The deck keeps a list of your rolls of filament, and shows the cost of each print.</p>
  <div class="shots">{shots}</div>
  <p>Download the image from the <a href="{repo}/releases/latest">latest release</a>, and write it to a USB SSD or an
    SD card with Raspberry Pi Imager. The deck updates itself from the releases. The steps to build the OS are in the
    <a href="{repo}/tree/main/os">os/README.md</a>.</p>
  <p>The manual shows each screen: read it <a href="manual/">on the web</a> or as a
    <a href="manual/printdeck-manual.pdf">PDF</a>. For a display that is mounted wide, read the
    <a href="manual/wide/">wide manual</a> or its <a href="manual/wide/printdeck-manual.pdf">PDF</a>.</p>

  <h2>Cura plugin</h2>
  <div class="step"><img class="plain" src="cura/img/cura-tab.png" alt="The touch screen of the deck in the Printdeck tab of Cura"><div>
    <p>The Printdeck plugin for Cura shows the deck in a Printdeck tab, live. Click the screen in Cura as you tap the
      touch screen. Cura follows the roll in the deck, warns when the roll is too short, and shows the layer that
      prints in the Preview.</p>
    <p>The Print on a part tool prints new models on a part that is printed already, for example a logo in another
      color on a lid. The deck prints a pocket for the part, and checks its height with a paper test.</p>
    <p>Download Printdeck.curapackage from the <a href="{repo}/releases">releases</a>, and drag it onto the Cura
      window.</p>
    <p><a class="btn" href="cura/printdeck-cura.pdf">Cura plugin booklet (PDF)</a>
      <a class="btn" href="cura/">Booklet on the web</a>
      <a class="btn" href="{repo}/releases">Download the plugin</a></p></div></div>

  <h2>On the LulzBot TAZ Workhorse</h2>
  <div class="step">{lulzbot}<div>
    <p>A second mount puts the Printdeck on the front left of a LulzBot TAZ Workhorse, with the display wide. You
      print its nine parts on the Workhorse itself, and drill no hole in the printer.</p>
    <p><a class="btn" href="lulzbot/printdeck-manual.pdf">Upgrade booklet (PDF)</a>
      <a class="btn" href="{repo}/tree/main/lulzbot">STL and STEP files</a></p></div></div>
</main>
<footer>Printdeck · the CAD, the images, and this page come from <a href="{repo}/tree/main/slate">slate/</a></footer>
</body>
</html>
"""


def main():
    copy_images()
    (DOCS / "index.html").write_text(page())
    print("wrote", DOCS / "index.html")


if __name__ == "__main__":
    main()
