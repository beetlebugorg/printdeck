#!/usr/bin/env python3
"""Printdeck: write the documentation site in docs/ (https://beetlebug.org/printdeck/).

The pages come from the files of the repository, so the site follows them:

  README.md, ARCHITECTURE.md, os/README.md, printer/README.md,
  slicer/README.md, slicer/cura-plugin/README.md,
  deck/internal/klipper/PORTING.md, desktop/meshes/README.md   the text of the pages
  slate/guide/guide.json, lulzbot/guide/guide.json             the two mounts
  docs/manual/index.html, docs/manual/wide/index.html          the screens of the deck
  deck/cmd/printdeck/*.go, deck/internal/config/config.go,
  os/board/, .github/workflows/, the build scripts              the reference pages

Run it after docs/manual/build.sh, docs/cura/build.sh, slate/make.sh or lulzbot/make.sh, and after
a change to a README. It needs only Python 3. At the end it checks every link of
the site, and stops with an error for a link that goes nowhere.

  docs/build.py
"""

import html
import json
import posixpath
import re
import sys
from pathlib import Path

DOCS = Path(__file__).resolve().parent
ROOT = DOCS.parent
REPO = "https://github.com/beetlebugorg/printdeck"
SITE = "https://beetlebug.org/printdeck/"
PROJECT = "https://beetlebug.org/projects/printdeck/"

esc = html.escape


def read(path):
    return (ROOT / path).read_text()


# ---------------------------------------------------------------------------------------------
# Markdown. The README files use headings, paragraphs, lists, fenced code, tables, inline code,
# bold, links and a few lines of HTML. This converter does only those.

# The README files that have a page on the site.
PAGE_OF = {
    "README.md": "", "ARCHITECTURE.md": "deck/", "os/README.md": "os/", "printer/README.md": "deck/printer/",
    "slicer/README.md": "slicer/adaptive-speed/", "slicer/cura-plugin/README.md": "slicer/cura-plugin/",
    "deck/internal/klipper/PORTING.md": "deck/klipper/", "slate/README.md": "hardware/slate/",
    "lulzbot/README.md": "hardware/workhorse/",
}


def fix_link(url, src_dir, root):
    """A link of a Markdown file in src_dir, for a page at root. A link into docs/ goes to the site, a
    link to a README with a page goes to the page, and any other link to the file on GitHub."""
    if re.match(r"^(https?:|mailto:|#)", url):
        return url
    anchor = ""
    if "#" in url:
        url, anchor = url.split("#", 1)
        anchor = "#" + anchor
    p = posixpath.normpath(posixpath.join(src_dir, url)) if src_dir else posixpath.normpath(url)
    if p in PAGE_OF:
        return root + PAGE_OF[p] + anchor
    if p.startswith("docs/"):
        return root + p[len("docs/"):] + anchor
    kind = "tree" if url.endswith("/") or "." not in posixpath.basename(p) else "blob"
    return f"{REPO}/{kind}/main/{p}{anchor}"


def slug(s):
    return re.sub(r"[^a-z0-9]+", "-", s.lower()).strip("-")


def inline(text, src_dir, root):
    out = []
    for i, tok in enumerate(re.split(r"(`[^`]+`)", text)):
        if i % 2:
            out.append(f"<code>{esc(tok[1:-1])}</code>")
            continue
        t = esc(tok)
        t = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", t)
        t = re.sub(r"\[([^\]]+)\]\(([^)\s]+)\)",
                   lambda m: f'<a href="{fix_link(html.unescape(m.group(2)), src_dir, root)}">{m.group(1)}</a>', t)
        t = re.sub(r'(?<!["\'>=])(https?://[^\s<)]+?)([.,;:]?)(?=[\s<)]|$)', r'<a href="\1">\1</a>\2', t)
        out.append(t)
    return "".join(out)


BLOCK_START = re.compile(r"^(#{1,6} |```|\||<|\s*(?:[-*]|\d+\.) )")
LIST_ITEM = re.compile(r"^(\s*)([-*]|\d+\.)\s+(.*)$")


class Markdown:
    def __init__(self, src_dir, root, shift=0):
        self.src_dir, self.root, self.shift = src_dir, root, shift

    def inline(self, s):
        return inline(s, self.src_dir, self.root)

    def convert(self, text):
        lines = text.split("\n")
        out, i, n = [], 0, len(lines)
        while i < n:
            line = lines[i]
            if not line.strip():
                i += 1
            elif line.startswith("```"):
                j = i + 1
                while j < n and not lines[j].startswith("```"):
                    j += 1
                out.append("<pre><code>" + esc("\n".join(lines[i + 1:j])) + "</code></pre>")
                i = j + 1
            elif (m := re.match(r"^(#{1,6}) (.*)$", line)):
                level = min(6, len(m.group(1)) + self.shift)
                title = m.group(2).strip()
                out.append(f'<h{level} id="{slug(title)}">{self.inline(title)}</h{level}>')
                i += 1
            elif line.startswith("<"):
                j = i
                while j < n and lines[j].strip():
                    j += 1
                raw = "\n".join(lines[i:j])
                raw = re.sub(r'(src|href|srcset)="([^"]+)"',
                             lambda m: f'{m.group(1)}="{fix_link(m.group(2), self.src_dir, self.root)}"', raw)
                out.append(raw)
                i = j
            elif line.startswith("|"):
                j = i
                while j < n and lines[j].startswith("|"):
                    j += 1
                out.append(self.table(lines[i:j]))
                i = j
            elif LIST_ITEM.match(line):
                j = i + 1
                while j < n and (lines[j].strip() or (j + 1 < n and lines[j + 1].startswith("  "))):
                    if lines[j].strip() and not lines[j].startswith(" ") and not LIST_ITEM.match(lines[j]):
                        break
                    j += 1
                out.append(self.list(lines[i:j]))
                i = j
            else:
                j = i + 1
                while j < n and lines[j].strip() and not BLOCK_START.match(lines[j]):
                    j += 1
                out.append("<p>" + self.inline(" ".join(s.strip() for s in lines[i:j])) + "</p>")
                i = j
        return "\n".join(out)

    def table(self, rows):
        cells = [[c.strip() for c in r.strip().strip("|").split("|")] for r in rows]
        head, body = cells[0], cells[1:]
        if body and all(re.match(r"^:?-{3,}:?$", c) for c in body[0]):
            body = body[1:]
        h = "".join(f"<th>{self.inline(c)}</th>" for c in head)
        b = "".join("<tr>" + "".join(f"<td>{self.inline(c)}</td>" for c in r) + "</tr>" for r in body)
        return f'<div class="table-wrap"><table><thead><tr>{h}</tr></thead><tbody>{b}</tbody></table></div>'

    def list(self, lines):
        first = LIST_ITEM.match(lines[0])
        indent = len(first.group(1))
        ordered = first.group(2)[0].isdigit()
        items, cur = [], None
        for line in lines:
            m = LIST_ITEM.match(line)
            if m and len(m.group(1)) == indent:
                cur = [m.group(3)]
                items.append(cur)
            elif cur is not None:
                cur.append(line)
        out = []
        for it in items:
            text, nested = [it[0]], []
            for line in it[1:]:
                if nested or (LIST_ITEM.match(line) and len(LIST_ITEM.match(line).group(1)) > indent):
                    nested.append(line)
                elif line.strip():
                    text.append(line.strip())
            body = self.inline(" ".join(text))
            if nested:
                body += self.list([l for l in nested if l.strip()])
            out.append(f"<li>{body}</li>")
        tag = "ol" if ordered else "ul"
        return f"<{tag}>" + "".join(out) + f"</{tag}>"


def md_sections(text):
    """The sections of a Markdown file by its level 2 headings: a list of (title, body). The text
    before the first heading has the title ""."""
    out, title, body = [], "", []
    for line in text.split("\n"):
        if line.startswith("## "):
            out.append((title, "\n".join(body)))
            title, body = line[3:].strip(), []
        else:
            body.append(line)
    out.append((title, "\n".join(body)))
    return out


def section_body(path, title):
    for t, body in md_sections(read(path)):
        if t == title:
            return body
    sys.exit(f"{path}: no section {title!r}")


def md(path, root, sections=None, skip=(), shift=0):
    """A Markdown file as HTML, without its level 1 heading. sections picks the level 2 sections to
    show, in order; skip leaves some out."""
    src_dir = posixpath.dirname(path)
    conv = Markdown(src_dir, root, shift)
    parts = []
    for t, body in md_sections(read(path)):
        if sections is not None and t not in sections:
            continue
        if t in skip:
            continue
        body = re.sub(r"^# .*\n", "", body, count=1) if t == "" else body
        if t:
            parts.append(f'<h{2 + shift} id="{slug(t)}">{conv.inline(t)}</h{2 + shift}>')
        parts.append(conv.convert(body))
    return "\n".join(parts)


def md_text(text, src_dir, root, shift=0):
    return Markdown(src_dir, root, shift).convert(text)


def without_html(body):
    """A Markdown body without its lines of HTML (the pictures of a README)."""
    return "\n".join(l for l in body.split("\n") if not l.lstrip().startswith("<"))


def docstring(path):
    """The docstring at the top of a Python file, as HTML, in the form of comment_block."""
    text = re.search(r'^"""(.*?)"""', read(path), re.S | re.M).group(1)
    return comment_text(text.split("\n"))


def comment_block(path, mark="#"):
    """The first block of comment lines of a script, as HTML: the text after the mark of each
    line."""
    lines, started = [], False
    for line in read(path).split("\n"):
        if line.startswith("#!"):
            continue
        if not line.startswith(mark):
            if started:
                break
            continue
        started = True
        lines.append(line[len(mark):])
    return comment_text(lines)


def comment_text(lines):
    """Lines of a comment as HTML. A line with three or more spaces of indent is a usage line, and
    goes into a code block. A blank line ends a paragraph."""
    paras, pre, cur = [], [], []

    def flush():
        nonlocal cur, pre
        if pre:
            paras.append("<pre><code>" + esc("\n".join(pre)) + "</code></pre>")
            pre = []
        if cur:
            paras.append("<p>" + inline(" ".join(cur), "", "") + "</p>")
            cur = []

    for text in lines:
        if text.startswith("   "):
            if cur:
                flush()
            pre.append(text.strip())
        elif not text.strip():
            flush()
        else:
            if pre:
                flush()
            cur.append(text.strip())
    flush()
    return "\n".join(paras)


# ---------------------------------------------------------------------------------------------
# The pages.

CSS = r"""
:root { --paper: #f4f0e6; --ink: #171717; --dim: #4a4742; --edge: #a9a59c; --orange: #ff5a1f; --blue: #1d47b5;
  --glcd: #1f4bd1; --glcdInk: #f2f6ff; --plate: #e6e3dc; --plate2: #ece8dd; --outside: #d3d0c8; --white: #ffffff;
  --sans: "IBM Plex Sans", "Helvetica Neue", Arial, sans-serif; --mono: "IBM Plex Mono", ui-monospace, Menlo, monospace;
  color-scheme: light; }
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) { --paper: #1b1a17; --ink: #ebe6da; --dim: #b3ac9f; --edge: #5c574e; --orange: #ff6a33;
    --blue: #8fb0ff; --plate: #26241f; --plate2: #2d2b25; --outside: #121110; color-scheme: dark; }
}
:root[data-theme="dark"] { --paper: #1b1a17; --ink: #ebe6da; --dim: #b3ac9f; --edge: #5c574e; --orange: #ff6a33;
  --blue: #8fb0ff; --plate: #26241f; --plate2: #2d2b25; --outside: #121110; color-scheme: dark; }
* { box-sizing: border-box; }
html { background: var(--outside); }
body { margin: 0; background: var(--paper); color: var(--ink); font: 16px/1.5 var(--sans); }
a { color: var(--blue); }
code, pre, kbd { font-family: var(--mono); }
code { font-size: .92em; background: var(--plate); padding: 0 .25em; border-radius: 3px; }
pre { background: var(--plate); border-left: 4px solid var(--edge); padding: 12px 14px; overflow-x: auto; font-size: 13px; line-height: 1.45; }
pre code { background: none; padding: 0; font-size: inherit; }
h1, h2, h3, h4 { font-family: var(--mono); letter-spacing: .02em; line-height: 1.2; }
h1 { font-size: clamp(26px, 4vw, 34px); margin: 6px 0 10px; text-transform: uppercase; }
h2 { font-size: 20px; margin: 40px 0 10px; padding-bottom: 6px; border-bottom: 3px solid var(--ink); }
h3 { font-size: 16px; margin: 28px 0 8px; text-transform: uppercase; letter-spacing: .08em; color: var(--dim); }
h4 { font-size: 14px; margin: 20px 0 6px; }
p, li { max-width: 76ch; }
.doc > p, .doc > ul, .doc > ol { max-width: 76ch; }
li { margin-bottom: 4px; }
img { max-width: 100%; height: auto; }
.eyebrow { margin: 0; font: 700 12px var(--mono); text-transform: uppercase; letter-spacing: .14em; color: var(--orange); }
.lede { font-size: 18px; color: var(--dim); max-width: 60ch; margin-top: 0; }
.table-wrap { overflow-x: auto; margin: 12px 0; }
table { border-collapse: collapse; width: 100%; }
td, th { border-top: 2px solid var(--edge); padding: 7px 10px; vertical-align: top; text-align: left; }
th { font: 700 11px var(--mono); text-transform: uppercase; letter-spacing: .08em; color: var(--dim); border-top: 0; border-bottom: 2px solid var(--ink); }
td:first-child { font-family: var(--mono); font-size: 14px; }
td.q { font: 700 14px var(--mono); white-space: nowrap; width: 3.5rem; }
td.n { text-align: right; white-space: nowrap; }
.note { border-left: 6px solid var(--orange); padding: 6px 12px; background: var(--plate); border-radius: 0 8px 8px 0; max-width: 76ch; }

/* The top bar */
#menu { display: none; }
.top { display: flex; align-items: center; gap: 16px; padding: 10px 16px; border-bottom: 3px solid var(--ink); background: var(--paper); }
.brand { display: flex; align-items: center; gap: 10px; text-decoration: none; color: var(--ink); }
.lcd { display: inline-block; background: var(--glcd); border: 3px solid #a9a59c; border-radius: 6px; padding: 2px 10px;
  color: var(--glcdInk); font: 700 16px/1.3 var(--mono); letter-spacing: .12em; box-shadow: inset 0 0 0 2px #173aa6; }
.brand span.docs { font: 700 12px var(--mono); letter-spacing: .3em; }
.top-links { margin-left: auto; display: flex; gap: 14px; font: 600 13px var(--mono); }
.top-links a { color: var(--ink); text-decoration: none; }
.menu-btn { display: none; font: 600 13px var(--mono); border: 2px solid var(--ink); border-radius: 6px; padding: 4px 10px; cursor: pointer; }

/* The sidebar and the page */
.layout { display: grid; grid-template-columns: 250px minmax(0, 1fr); gap: 36px; max-width: 1260px; margin: 0 auto; padding: 0 16px; }
.side { position: sticky; top: 0; align-self: start; max-height: 100vh; overflow-y: auto; padding: 20px 0 32px; font-size: 14px; }
.side .group { font: 700 11px var(--mono); text-transform: uppercase; letter-spacing: .12em; color: var(--dim); margin: 18px 0 4px; padding-left: 10px; }
.side a { display: block; padding: 3px 10px; color: var(--ink); text-decoration: none; border-left: 3px solid transparent; }
.side a:hover { background: var(--plate); }
.side a[aria-current] { border-left-color: var(--orange); background: var(--plate); font-weight: 600; }
.side .sub a { padding-left: 24px; font-size: 13px; color: var(--dim); }
.side .switch { display: flex; gap: 6px; padding: 2px 10px 6px; font: 600 11px var(--mono); }
.side .switch a { display: inline; padding: 1px 8px; border: 1.5px solid var(--edge); border-radius: 99px; }
.side .switch a[aria-current] { background: var(--ink); color: var(--paper); border-color: var(--ink); }
main { padding: 28px 0 72px; min-width: 0; overflow-wrap: break-word; }
pre { overflow-wrap: normal; }
.onpage { margin: 16px 0 28px; padding: 10px 16px; border: 2px solid var(--edge); border-radius: 8px; background: var(--plate2); font-size: 14px; }
.onpage b { font: 700 11px var(--mono); text-transform: uppercase; letter-spacing: .12em; color: var(--dim); }
.onpage ol { margin: 4px 0 0; padding: 0; list-style: none; columns: 2; column-gap: 24px; }
.onpage li { break-inside: avoid; margin: 0; }
.onpage a { color: var(--ink); text-decoration: none; }
.onpage a:hover { text-decoration: underline; }
footer { border-top: 3px solid var(--ink); margin-top: 24px; padding: 18px 16px 40px; color: var(--dim); font-size: 13px; text-align: center; }
footer a { color: inherit; }

/* Buttons, chips, stats */
.btn { display: inline-block; font: 600 13px var(--mono); color: var(--ink); border: 2px solid var(--ink); border-radius: 99px; padding: 6px 14px;
  text-decoration: none; margin: 0 8px 8px 0; background: var(--paper); }
.btn:hover { background: var(--plate); }
.btn.primary { background: var(--orange); border-color: var(--orange); color: #fff; }
.chips { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0; }
.chip { font: 600 12px var(--mono); border: 1.5px solid var(--ink); border-radius: 99px; padding: 3px 10px; }
.stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 20px 0; padding: 14px 0; border-top: 2px solid var(--edge); border-bottom: 2px solid var(--edge); }
.stats b { display: block; font: 700 18px var(--mono); }
.stats span { color: var(--dim); font: 600 11px var(--mono); text-transform: uppercase; letter-spacing: .08em; }
.sw { display: inline-block; width: 12px; height: 12px; border-radius: 3px; border: 1px solid #888; vertical-align: -1px; margin-right: 5px; }

/* Pictures */
.pics { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; margin: 20px 0; }
.pics.four { grid-template-columns: repeat(4, 1fr); }
.frame-img { background: var(--plate); border: 2px solid var(--edge); border-radius: 10px; padding: 16px; }
.frame-img img { display: block; width: 100%; height: auto; }
.frame-img.white { background: #fff; }
.frame-img figcaption, .pics figcaption { font: 600 12px var(--mono); text-transform: uppercase; color: var(--dim); margin-top: 8px; }
.pics figure { margin: 0; }
.screens img { border: 3px solid var(--ink); border-radius: 10px; display: block; width: 100%; height: auto; background: #000; }
.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 16px; margin: 16px 0; }
.card { border: 2px solid var(--edge); border-radius: 10px; background: var(--plate2); overflow: hidden; font-size: 14px; }
.card .frame-img { border: 0; border-radius: 0; border-bottom: 2px solid var(--edge); padding: 12px; }
.card .frame-img img { height: 130px; object-fit: contain; }
.card div.text { padding: 10px 12px; }
.card b { font-family: var(--mono); }
.card p { margin: 4px 0 0; }
.map { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 16px; margin: 16px 0; }
.map a.card { display: block; text-decoration: none; color: var(--ink); padding: 14px 16px; }
.map a.card:hover { background: var(--plate); }
.map .card b { display: block; font-size: 15px; margin-bottom: 4px; }
.map .card span { color: var(--dim); }

/* The steps of a build */
.step { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr); gap: 24px; align-items: start; padding: 28px 0; border-top: 2px solid var(--edge); }
.step .num { font: 800 44px/1 var(--mono); color: var(--orange); }
.step h3 { margin: 4px 0 8px; font-size: 20px; text-transform: none; letter-spacing: 0; color: var(--blue); font-style: italic; }
.step ol { padding-left: 20px; }
ol.steps { margin: 0 0 16px; padding-left: 0; list-style: none; counter-reset: step; max-width: 76ch; }
ol.steps li { counter-increment: step; display: grid; grid-template-columns: 34px 1fr; margin-bottom: 8px; }
ol.steps li::before { content: counter(step); font: 700 15px var(--mono); color: #fff; background: var(--orange);
  width: 24px; height: 24px; border-radius: 4px; display: flex; align-items: center; justify-content: center; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 32px; }

/* The screens of the deck, from the manual */
.manual section { margin: 40px 0; }
.manual h2.title { display: flex; align-items: center; gap: 14px; margin: 0 0 14px; font-size: clamp(20px, 3.5vw, 28px);
  text-transform: uppercase; letter-spacing: .06em; border: 0; padding: 0; }
.manual h2.title .num { background: var(--ink); color: var(--paper); border-radius: 6px; padding: 2px 10px; }
.manual h2.title::after { content: ""; flex: 1; height: 6px; background: var(--orange); border-radius: 3px; }
.manual figure { margin: 22px 0 28px; display: grid; grid-template-columns: minmax(0, 440px) minmax(0, 1fr); gap: 24px; align-items: start; }
.manual figure h3 { grid-column: 1 / -1; margin: 0; font-size: 16px; text-transform: uppercase; letter-spacing: .1em; color: var(--dim); }
.manual .frame { position: relative; padding: 0 calc(100% * 72 / 624); }
.manual.wide figure { grid-template-columns: minmax(0, 1fr); }
.manual.wide .frame { max-width: 820px; padding: 0 calc(100% * 72 / 944); }
.manual.wide ol.parts { columns: 2; column-gap: 32px; }
.manual .shot { position: relative; border: 5px solid var(--ink); border-radius: 14px; overflow: hidden; background: #000; box-shadow: 6px 6px 0 var(--edge); }
.manual .shot img { display: block; width: 100%; height: auto; }
.manual .marks { position: absolute; inset: 5px 0; width: 100%; height: calc(100% - 10px); overflow: visible; pointer-events: none; }
.manual .leader { fill: none; stroke: var(--ink); stroke-width: 2.5; stroke-linejoin: round; }
.manual .dot { fill: var(--orange); stroke: var(--ink); stroke-width: 2; }
.manual .badge { fill: var(--ink); stroke: var(--paper); stroke-width: 2.5; }
.manual .badge-n { fill: var(--paper); font: 700 16px var(--mono); text-anchor: middle; dominant-baseline: central; }
.manual ol.parts { list-style: none; margin: 0; padding: 0; }
.manual ol.parts li { display: grid; grid-template-columns: 30px 1fr; gap: 10px; margin-bottom: 12px; break-inside: avoid; max-width: none; }
.manual ol.parts .badge { width: 26px; height: 26px; border-radius: 50%; background: var(--ink); color: var(--paper); font: 700 13px var(--mono);
  display: flex; align-items: center; justify-content: center; margin-top: 1px; }
.manual ol.parts b { display: block; text-transform: uppercase; letter-spacing: .04em; font-family: var(--mono); }
.manual .note { grid-column: 1 / -1; }
.manual td:first-child { font-weight: 600; width: 45%; }
.manual .glcd-row { display: flex; flex-wrap: wrap; gap: 20px; margin: 14px 0; }
.manual .lcd-shot { display: block; width: 288px; max-width: 100%; height: auto; image-rendering: pixelated; border: 6px solid #a9a59c; border-radius: 8px; }
.manual .glcd-cap { margin-top: 6px; font: 600 13px var(--mono); text-transform: uppercase; color: var(--dim); }
.layout-switch { display: flex; gap: 8px; align-items: center; margin: 0 0 16px; font: 600 13px var(--mono); }
.layout-switch a { padding: 3px 12px; border: 2px solid var(--ink); border-radius: 99px; color: var(--ink); text-decoration: none; }
.layout-switch a[aria-current] { background: var(--ink); color: var(--paper); }

@media (max-width: 899px) {
  .layout { grid-template-columns: 1fr; gap: 0; }
  .side { position: static; max-height: none; padding: 8px 0 0; display: none; border-bottom: 2px solid var(--edge); }
  #menu:checked ~ .layout .side { display: block; }
  .menu-btn { display: inline-block; }
  .top-links { display: none; }
  .onpage ol { columns: 1; }
  .stats, .pics.four { grid-template-columns: 1fr 1fr; }
  .pics, .step, .two { grid-template-columns: 1fr; }
  .manual figure { grid-template-columns: minmax(0, 1fr); }
  .manual .frame { max-width: 440px; }
  .manual.wide ol.parts { columns: 1; }
}
@media (max-width: 520px) { .stats, .pics.four { grid-template-columns: 1fr 1fr; } .stats b { font-size: 15px; } }
"""

# The chapters of the manual, and the folder of each on the site.
CHAPTERS = {
    "Set up": "setup", "Meet your Printdeck": "meet", "Print a part": "print", "Change the filament": "filament",
    "Move and heat": "move", "Level and calibrate": "calibrate", "Look after the printer": "care",
    "When something goes wrong": "problems",
}

PAGES = {}  # path -> (title, body)


def manual_chapters(wide):
    """The chapters of the manual, with the HTML of their sections, from docs/manual/index.html."""
    src = (DOCS / "manual" / ("wide/index.html" if wide else "index.html")).read_text()
    toc = re.search(r'<nav class="toc">(.*?)</nav>', src, re.S).group(1)
    chapters, chapter = [], None
    for m in re.finditer(r'<li class="chapter">(.*?)</li>|<li><a href="#([\w-]+)"><span class="num">\d+</span>(.*?)</a></li>', toc):
        if m.group(1):
            chapter = (m.group(1), [])
            chapters.append(chapter)
        else:
            chapter[1].append((m.group(2), m.group(3)))
    sections = {m.group(1): m.group(0) for m in re.finditer(r'<section id="([\w-]+)">.*?</section>', src, re.S)}
    out = []
    for title, secs in chapters:
        if title not in CHAPTERS:
            sys.exit(f"manual: the chapter {title!r} has no folder in CHAPTERS")
        htmls = []
        for sid, _ in secs:
            htmls.append(sections[sid])
        out.append((title, CHAPTERS[title], secs, "\n".join(htmls)))
    return out


def nav_html(current, root, wide):
    tall = manual_chapters(False)
    ui_base = "ui/wide/" if wide else "ui/"
    groups = [
        (None, [("Overview", "")]),
        ("Hardware", [("The hardware", "hardware/"), ("The Slate case", "hardware/slate/"),
                      ("The TAZ Workhorse mount", "hardware/workhorse/")]),
        ("The deck UI", [("The screens", "ui/")] + [(t, ui_base + f + "/") for t, f, _, _ in tall]),
        ("Software", [("Printdeck OS", "os/"), ("The printdeck program", "deck/"), ("The Klipper host", "deck/klipper/"),
                      ("The reference printer", "deck/printer/"), ("Remote screen and API", "deck/remote/"),
                      ("The Cura plugin", "slicer/cura-plugin/"), ("Adaptive Speed", "slicer/adaptive-speed/"),
                      ("Desktop app and simulator", "desktop/"), ("The film tools", "desktop/film/")]),
        ("Build and release", [("Build and release", "build/")]),
        ("Reference", [("Commands", "reference/commands/"), ("Environment variables", "reference/environment/"),
                       ("Files, paths and services", "reference/files/"), ("Hardware lists", "reference/hardware/")]),
        ("Manuals and booklets", [("Manual, tall display", "manual/"), ("Manual, wide display", "manual/wide/"),
                                  ("Workhorse upgrade booklet", "lulzbot/"), ("Cura plugin booklet", "cura/")]),
    ]
    out = []
    for name, items in groups:
        if name:
            out.append(f'<div class="group">{esc(name)}</div>')
        if name == "The deck UI":
            cur_tall = "" if wide else ' aria-current="page"'
            cur_wide = ' aria-current="page"' if wide else ""
            other = current.replace("ui/wide/", "ui/") if wide else current.replace("ui/", "ui/wide/")
            if not current.startswith("ui/") or current == "ui/":
                other = "ui/wide/" + tall[0][1] + "/" if not wide else "ui/" + tall[0][1] + "/"
            tall_href = root + (other if wide else current if current.startswith("ui/") and current != "ui/" else "ui/" + tall[0][1] + "/")
            wide_href = root + (current if wide else other)
            out.append(f'<div class="switch"><a href="{tall_href}"{cur_tall}>Tall</a><a href="{wide_href}"{cur_wide}>Wide</a></div>')
        for title, path in items:
            cur = ' aria-current="page"' if path == current else ""
            cls = ' class="sub"' if name == "The deck UI" and path != "ui/" else ""
            out.append(f'<span{cls}><a href="{root}{path}"{cur}>{esc(title)}</a></span>' if cls else f'<a href="{root}{path}"{cur}>{esc(title)}</a>')
    return "\n".join(out)


def onpage(body):
    """The list of the level 2 headings of a page."""
    heads = re.findall(r'<h2 id="([^"]+)">(.*?)</h2>', body)
    if len(heads) < 2:
        return ""
    items = "".join(f'<li><a href="#{i}">{re.sub("<[^>]+>", "", t)}</a></li>' for i, t in heads)
    return f'<nav class="onpage"><b>On this page</b><ol>{items}</ol></nav>'


def page(path, section, title, lede, body, description=None, wide=False, toc=True):
    depth = path.count("/")
    root = "../" * depth
    body_html = (onpage(body) if toc else "") + body
    doc = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{esc(title)} · Printdeck</title>
<meta name="description" content="{esc(description or lede)}">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@500;600;700&family=IBM+Plex+Sans:ital,wght@0,400;0,600;0,700;1,700&display=swap" rel="stylesheet">
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='6' fill='%231f4bd1'/%3E%3Crect x='7' y='9' width='18' height='14' rx='2' fill='%23f2f6ff'/%3E%3Crect x='10' y='12' width='8' height='3' fill='%23ff5a1f'/%3E%3C/svg%3E">
<link rel="stylesheet" href="{root}site.css">
</head>
<body>
<input type="checkbox" id="menu">
<header class="top">
  <a class="brand" href="{root}"><span class="lcd">PRINTDECK</span><span class="docs">DOCS</span></a>
  <label class="menu-btn" for="menu">Contents</label>
  <nav class="top-links"><a href="{PROJECT}">Project page</a><a href="{REPO}">GitHub</a><a href="{REPO}/releases/latest">Download</a></nav>
</header>
<div class="layout">
<aside class="side"><nav>{nav_html(path, root, wide)}</nav></aside>
<main>
<p class="eyebrow">{esc(section)}</p>
<h1>{esc(title)}</h1>
{f'<p class="lede">{lede}</p>' if lede else ''}
{body_html}
</main>
</div>
<footer>Printdeck is free software (AGPL-3.0). <a href="{REPO}/blob/main/docs/build.py">docs/build.py</a> writes this site from the files of the repository.</footer>
</body>
</html>
"""
    PAGES[path] = doc


def pic(root, name, alt, cls="frame-img"):
    return (f'<picture class="{cls}"><source media="(prefers-color-scheme: dark)" srcset="{root}img/{name}-dark.png">'
            f'<img src="{root}img/{name}-light.png" alt="{esc(alt)}" loading="lazy"></picture>')


def kv_table(rows, first="q"):
    return '<div class="table-wrap"><table>' + "".join(
        f'<tr><td class="{first}">{esc(str(a))}</td><td>{esc(b)}</td></tr>' for a, b in rows) + "</table></div>"


SLATE = json.loads(read("slate/guide/guide.json"))
LULZ = json.loads(read("lulzbot/guide/guide.json"))

SCREENS = [("printing", "A print with the model up to the layer that prints now"),
           ("first-layer", "The first layer with the Z offset"),
           ("file", "A file with its print time, filament, roll and cost"),
           ("filament", "The Filament page with the roll in the printer")]


def screens(root, wide=False):
    d = "manual/wide/img/" if wide else "manual/img/"
    return '<div class="pics four screens">' + "".join(
        f'<figure><img src="{root}{d}{n}.png" alt="{esc(a)}" loading="lazy"></figure>' for n, a in SCREENS) + "</div>"


# --- The landing page ---------------------------------------------------------------------------

def landing():
    root = ""
    lede = ("A Raspberry Pi 5 with a 7\" touch screen next to the 3D printer. It runs Klipper, and shows and "
            "controls the print on the touch screen, on the display of the printer, and in a browser.")
    t = SLATE["totals"]
    body = f"""
<p>
  <a class="btn primary" href="{REPO}/releases/latest">Download Printdeck OS</a>
  <a class="btn" href="#install">Install</a>
  <a class="btn" href="manual/">The manual</a>
  <a class="btn" href="hardware/">Build the hardware</a>
  <a class="btn" href="{REPO}">Source on GitHub</a>
</p>
<div class="pics">
  <figure class="frame-img">{pic(root, "hero", "The Printdeck Slate on its foot", cls="")}<figcaption>The Slate: a desk slab, {t["parts"]} printed parts</figcaption></figure>
  <figure class="frame-img white"><img src="img/lulzbot-printer.png" alt="The Printdeck mount on the front left of a LulzBot TAZ Workhorse"><figcaption>The mount on the LulzBot TAZ Workhorse</figcaption></figure>
</div>
{screens(root)}
<h2 id="install">Install</h2>
{md_text(section_body("README.md", "Install"), "", root)}
<p>The <a href="os/">Printdeck OS</a> page has the first start, the login, the settings and the update in full.</p>

<h2 id="docs">The documentation</h2>
<div class="map">
  <a class="card" href="hardware/"><b>Hardware</b><span>The parts, the Slate case for the desk, and the mount for the LulzBot TAZ Workhorse: print plans, hardware lists and assembly steps.</span></a>
  <a class="card" href="ui/"><b>The deck UI</b><span>Every screen of the touch screen with numbered callouts, for a display that is mounted tall or wide.</span></a>
  <a class="card" href="os/"><b>Printdeck OS</b><span>The read-only Linux image: install, the first start, the two system slots, the update, the release and the debug image.</span></a>
  <a class="card" href="deck/"><b>The printdeck program</b><span>One Go program: the Klipper host, the touch screen, the remote screen, the API for slicers and the display of the printer.</span></a>
  <a class="card" href="slicer/cura-plugin/"><b>Slicers</b><span>The Printdeck plugin for Cura with the Print on a part tool, and the Adaptive Speed script.</span></a>
  <a class="card" href="desktop/"><b>Desktop app and simulator</b><span>The Printdeck for a computer, the simulated TAZ Workhorse in 3D, and the tools that make the film.</span></a>
  <a class="card" href="build/"><b>Build and release</b><span>The OS image, the CI, the tags of a release, the manuals and this site.</span></a>
  <a class="card" href="reference/commands/"><b>Reference</b><span>The commands and flags of printdeck, the environment variables, the files and paths, and the hardware lists.</span></a>
</div>

<h2 id="goal">Goal</h2>
{md_text(section_body("README.md", "Goal"), "", root)}
<h2 id="license">License</h2>
{md_text(section_body("README.md", "License"), "", root)}
"""
    page("", "Documentation", "Printdeck", lede, body, toc=False,
         description="The documentation of Printdeck: a touch screen and a Klipper print server for a 3D printer on a Raspberry Pi 5.")


# --- Hardware -------------------------------------------------------------------------------------

def hardware():
    root = "../"
    s, l = SLATE["totals"], LULZ["totals"]
    body = f"""
<h2 id="parts">The parts</h2>
{md_text(section_body("README.md", "Hardware"), "", root)}
<p>Both mounts hold the same parts. The lists with the screws, the inserts and the cables are on the page of each mount, and together on the <a href="{root}reference/hardware/">hardware lists</a> page.</p>
<h2 id="mounts">The two mounts</h2>
<div class="pics">
  <div class="card"><a href="slate/">{pic(root, "hero", "The Printdeck Slate on its foot")}</a><div class="text"><b>The Slate</b>
  {md_text(without_html(section_body("README.md", "Case")), "", root)}
  <div class="chips"><span class="chip">{s["size"]}</span><span class="chip">{s["parts"]} printed parts</span><span class="chip">{s["grams"]} g of PLA</span><span class="chip">No supports</span></div>
  <p><a class="btn" href="slate/">The Slate case</a></p></div></div>
  <div class="card"><a href="workhorse/"><picture class="frame-img white"><img src="{root}img/lulzbot-printer.png" alt="The Printdeck mount on the front left of a LulzBot TAZ Workhorse" loading="lazy"></picture></a><div class="text"><b>The TAZ Workhorse mount</b>
  {md_text(without_html(section_body("README.md", "On the LulzBot TAZ Workhorse")), "", root)}
  <div class="chips"><span class="chip">{l["parts"]} printed parts</span><span class="chip">{l["grams"]} g of PLA</span><span class="chip">{l["time"]}</span><span class="chip">No supports</span><span class="chip">No holes in the printer</span></div>
  <p><a class="btn" href="workhorse/">The TAZ Workhorse mount</a></p></div></div>
</div>
"""
    page("hardware/", "Hardware", "The hardware", "A Raspberry Pi 5, a 7 inch touch display, a drive and a fan, in one of two printed mounts.", body)


def slate():
    root = "../../"
    g = SLATE
    t = g["totals"]
    cards = "".join(
        f'<div class="card">{pic(root, p["image"], p["name"])}<div class="text"><b>{p["name"].capitalize()}</b> · '
        f'<span class="sw" style="background:{p["swatch"]}"></span>{p["colour"]} PLA · {p["grams"]} g<p>{esc(p["notes"])}</p></div></div>'
        for p in g["prints"])
    plan = "".join(
        f'<tr><td><code>{p["file"]}</code></td><td><span class="sw" style="background:{p["swatch"]}"></span>{p["colour"]}</td>'
        f'<td class="n">{p["size"]}</td><td class="n">{p["grams"]} g</td><td class="n">{p["metres"]} m</td><td class="n">{p["cost"]}</td></tr>'
        for p in g["prints"])
    extras = "".join(f'<tr><td><code>{e["file"]}</code></td><td colspan="5">{esc(e["notes"])}</td></tr>' for e in g["extras"])
    steps = []
    for i, s in enumerate(g["steps"], 1):
        chips = "".join(f'<span class="chip">{esc(x)}</span>' for x in s["need"])
        lis = "".join(f"<li>{esc(x)}</li>" for x in s["items"])
        note = f'<p class="note"><b>Note.</b> {esc(s["note"])}</p>' if s["note"] else ""
        steps.append(f'<section class="step" id="step-{i}">{pic(root, s["image"], f"Step {i}: {s["title"]}")}'
                     f'<div><div class="num">{i}</div><h3>{esc(s["title"])}</h3><div class="chips">{chips}</div><ol>{lis}</ol>{note}</div></section>')
    files = "".join(f'<a class="btn" href="{root}files/{p["name"]}.stl">{p["name"]}.stl</a>' for p in g["prints"])
    files += "".join(f'<a class="btn" href="{root}files/{posixpath.basename(e["file"])}">{posixpath.basename(e["file"])}</a>' for e in g["extras"])
    body = f"""
{md_text(without_html(section_body("slate/README.md", "")).split("\n", 1)[1], "slate", root)}
<div class="stats">
  <div><b>{t["size"]}</b><span>Size</span></div>
  <div><b>{t["parts"]} parts · {t["grams"]} g</b><span>Printed</span></div>
  <div><b>{t["cost"]}</b><span>Filament at {t["pricePerKg"]}/kg</span></div>
  <div><b>No supports</b><span>All parts</span></div>
</div>
<div class="pics">{pic(root, "hero", "The Printdeck from the front")}{pic(root, "rear", "The Printdeck from the rear, with the Pi and the cables")}</div>
<p><a class="btn primary" href="{root}files/printdeck-slate-build.pdf">Build instructions (PDF)</a> {files}</p>

<h2 id="print">Print the parts</h2>
<p>The STL files are in their print orientation. Load them, do not turn them.</p>
<div class="cards">{cards}</div>
<div class="table-wrap"><table><tr><th>File</th><th>Colour</th><th class="n">Size on the bed (mm)</th><th class="n">Filament</th><th class="n">Length</th><th class="n">Cost</th></tr>{plan}
<tr><td><b>Total</b></td><td></td><td></td><td class="n"><b>{t["grams"]} g</b></td><td class="n"><b>{t["metres"]} m</b></td><td class="n"><b>{t["cost"]}</b></td></tr>{extras}</table></div>
<p>The filament comes from slices with the Cura profiles of this case, with the skirt. The cost is at {t["pricePerKg"]} a kilogram.</p>
<h3>Settings</h3>
{kv_table(g["settings"], first="")}

<h2 id="parts">What you need</h2>
<div class="two"><div><h3>Hardware</h3>{kv_table([(f"{q}×", x) for q, x in g["hardware"]])}</div>
<div><h3>Electronics</h3>{kv_table([(f"{q}×", x) for q, x in g["electronics"]])}</div></div>

<h2 id="build">Build</h2>
<p>Left and right are as seen from the front of the case.</p>
{"".join(steps)}

<h2 id="connect">Connect</h2>
<div class="step">{pic(root, "connect", "The plugs for the printer and the power")}<div><ol class="steps">{"".join(f"<li>{esc(x)}</li>" for x in g["connect"])}</ol>
<p>Then switch on. The <a href="{root}ui/setup/">setup</a> opens on the touch screen.</p></div></div>
<h2 id="open">To open the case</h2>
<ol class="steps">{"".join(f"<li>{esc(x)}</li>" for x in g["open"])}</ol>

<h2 id="design">The design</h2>
<p>The case is a build123d model in <a href="{REPO}/tree/main/slate">slate/</a>. The CAD files, the STL files and the build PDF come from it.</p>
{md("slate/README.md", root, sections=("Parts", "Files", "Needs"), shift=1)}
"""
    page("hardware/slate/", "Hardware", "The Slate case", "A desk slab for the Printdeck, with the Pi 5 on its rear.", body)


def workhorse():
    root = "../../"
    g = LULZ
    t = g["totals"]
    img = f"{root}lulzbot/img/"
    plan = "".join(
        f'<tr><td><img src="{img}{p["image"]}.png" alt="" width="60" loading="lazy"></td><td><code>{p["file"]}</code></td>'
        f'<td><span class="sw" style="background:{p["swatch"]}"></span>{p["colour"]}</td><td class="n">{p["count"]}</td>'
        f'<td class="n">{p["grams"]} g</td><td class="n">{p["time"]}</td><td class="n">{p["cost"]}</td><td>{esc(p["notes"])}</td></tr>'
        for p in g["prints"])
    test = g["test"]
    hw = "".join(f'<tr><td class="q">{x["count"]}×</td><td>{esc(x["item"])}</td><td>{esc(x["where"])}</td></tr>' for x in g["hardware"])
    steps = []
    for i, s in enumerate(g["steps"], 1):
        chips = "".join(f'<span class="chip">{esc(x)}</span>' for x in s["need"])
        lis = "".join(f"<li>{esc(x)}</li>" for x in s["items"])
        note = f'<p class="note"><b>Note.</b> {esc(s["note"])}</p>' if s["note"] else ""
        steps.append(f'<section class="step" id="step-{i}"><figure class="frame-img white"><img src="{img}lulzbot-{s["image"]}.png" alt="Step {i}: {esc(s["title"])}" loading="lazy"></figure>'
                     f'<div><div class="num">{i}</div><h3>{esc(s["title"])}</h3><div class="chips">{chips}</div><ol>{lis}</ol>{note}</div></section>')
    cables = "".join(f"<tr><td>{esc(a)}</td><td class=\"n\">{esc(b)}</td><td>{esc(c)}</td></tr>" for a, b, c in g["cables"])
    names = [posixpath.basename(p["file"]) for p in g["prints"]] + [posixpath.basename(test["file"])]
    files = "".join(f'<a class="btn" href="{root}files/lulzbot/{n}">{n}</a>' for n in names)
    body = f"""
{md_text(without_html(section_body("lulzbot/README.md", "")).split("\n", 1)[1], "lulzbot", root)}
<div class="chips"><span class="chip">{t["parts"]} printed parts</span><span class="chip">{t["grams"]} g of PLA</span><span class="chip">{t["time"]} of printing</span><span class="chip">No supports</span><span class="chip">No holes in the printer</span></div>
<div class="pics"><figure class="frame-img white"><img src="{img}lulzbot-hero.png" alt="The mount from the front" loading="lazy"></figure><figure class="frame-img white"><img src="{img}lulzbot-side.png" alt="The mount from the side" loading="lazy"></figure></div>
<p><a class="btn primary" href="{root}lulzbot/printdeck-manual.pdf">Upgrade booklet (PDF)</a><a class="btn" href="{root}lulzbot/">Upgrade booklet on the web</a><a class="btn" href="{root}files/lulzbot/printdeck-lulzbot-build.pdf">Build instructions (PDF)</a></p>
<p>{files}</p>
<p>The deck screens of the booklet are the wide screens: the display is mounted wide in the head. See <a href="{root}ui/wide/{list(CHAPTERS.values())[1]}/">the deck UI, wide</a>.</p>

<h2 id="print">Print the parts</h2>
<p>The STL files are in <code>lulzbot/stl/</code>, in their print orientation. Load them as they are. The numbers come from slices with the Cura profiles of the mount: LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle, 2.85 mm PLA.</p>
<div class="table-wrap"><table><tr><th></th><th>File</th><th>Colour</th><th class="n">Print</th><th class="n">Filament</th><th class="n">Time</th><th class="n">Cost</th><th>Print orientation</th></tr>{plan}
<tr><td></td><td><b>Total</b></td><td></td><td class="n">{t["parts"]}</td><td class="n"><b>{t["grams"]} g</b></td><td class="n"><b>{t["time"]}</b></td><td class="n"><b>{t["cost"]}</b></td><td></td></tr></table></div>
<p class="note">Total: {t["grams"]} g of PLA ({t["metres"]} m), {t["time"]} of printing, {t["cost"]} at {t["pricePerKg"]} a kilogram. The fit test of the screen opening (<code>{test["file"]}</code>, {test["grams"]} g, {test["time"]}) is optional. Print it first, and check the display in it: the display pocket has 0.3 mm of clearance at each side.</p>
<h3>Settings</h3>
{kv_table(g["settings"], first="")}

<h2 id="need">What you need</h2>
<h3>Hardware</h3>
<div class="table-wrap"><table><tr><th></th><th>Item</th><th>Where</th></tr>{hw}</table></div>
<p>All the screws are M3 socket cap screws, for a 2.5 mm hex key. A shorter or a longer heat-set insert fits, up to the length in brackets.</p>
<h3>Electronics and cables</h3>
{kv_table([(f"{q}×", x) for q, x in g["electronics"]])}
<h3>Tools</h3>
<ul>{"".join(f"<li>{esc(x)}</li>" for x in g["tools"])}</ul>

<h2 id="check">Before you start</h2>
<figure class="frame-img white"><img src="{img}lulzbot-exploded.png" alt="The parts of the mount on the printer, exploded" loading="lazy"></figure>
<h3>Check on your printer</h3>
<ol class="steps">{"".join(f"<li>{esc(x)}</li>" for x in g["checks"])}</ol>
<h3>Prepare the drive</h3>
<p>The Printdeck starts from the 2.5 inch drive. Write the Printdeck image to it before you build the mount: the drive goes into the head in step 3. See <a href="{root}os/#install">Install</a> on the Printdeck OS page.</p>
{md_text(section_body("lulzbot/README.md", "Software"), "lulzbot", root)}

<h2 id="build">Build the mount</h2>
<p>Each step names the parts and the hardware that it needs. The parts from an earlier step have the number of that step.</p>
{"".join(steps)}

<h2 id="connect">Connect and start</h2>
<figure class="frame-img white"><img src="{img}lulzbot-connect.png" alt="The cables of the mount" loading="lazy"></figure>
<div class="table-wrap"><table><tr><th>Cable</th><th class="n">Length</th><th>Route</th></tr>{cables}</table></div>
<p>Connect the power cable to the Raspberry Pi 27 W USB-C power supply behind the printer, and the printer cable to the USB socket of the printer board. Turn on the printer and the power supply. The deck starts from the drive and opens the <a href="{root}ui/wide/setup/">setup</a>.</p>

<h2 id="design">The design</h2>
<p>The mount is a build123d model in <a href="{REPO}/tree/main/lulzbot">lulzbot/</a>, on the CAD of the printer from LulzBot.</p>
{md("lulzbot/README.md", root, sections=("Hardware", "Cover screws", "Display dimensions", "To check on the printer", "Files", "Reference files", "Needs"), shift=1)}
"""
    page("hardware/workhorse/", "Hardware", "The TAZ Workhorse mount",
         "The Printdeck on the front left of a LulzBot TAZ Workhorse, printed on the printer itself.", body)


# --- The deck UI ----------------------------------------------------------------------------------

def ui():
    root = "../"
    tall = manual_chapters(False)
    sw = section_body("README.md", "Software")
    screens_md = sw.split("The screens of the touch screen:", 1)[1]
    chapters = "".join(
        f'<a class="card" href="{f}/"><b>{esc(t)}</b><span>{", ".join(esc(st) for _, st in secs)}</span></a>' for t, f, secs, _ in tall)
    body = f"""
<p>The deck is the application on the 7 inch touch screen. It has two layouts: for a display that is mounted tall, as in the Slate, and for a display that is mounted wide, as on the TAZ Workhorse. The pages of this section show every screen with numbered callouts, in the words of the manual. Each page has a switch between the tall and the wide layout.</p>
{screens(root)}
<h2 id="screens">The screens</h2>
{md_text(screens_md, "", root)}
<h2 id="chapters">The chapters</h2>
<div class="map">{chapters}</div>
<h2 id="manuals">The manuals and the booklets</h2>
{md_text(section_body("README.md", "Manual"), "", root)}
<p>The booklets have the same style as the pictures of this site. The booklet of the Workhorse upgrade has the guides of the mount and then the wide manual.</p>
"""
    page("ui/", "The deck UI", "The screens of the deck",
         "Every screen of the touch screen, for a display that is mounted tall or wide.", body)
    for wide in (False, True):
        for title, folder, secs, html_ in manual_chapters(wide):
            path = f"ui/wide/{folder}/" if wide else f"ui/{folder}/"
            r = "../../../" if wide else "../../"
            other = f"{r}ui/{folder}/" if wide else f"{r}ui/wide/{folder}/"
            switch = (f'<div class="layout-switch"><span>Layout:</span><a href="{"" if not wide else other}"{"" if wide else " aria-current=\"page\""}>Tall</a>'
                      f'<a href="{other if not wide else ""}"{" aria-current=\"page\"" if wide else ""}>Wide</a></div>')
            toc = '<nav class="onpage"><b>In this chapter</b><ol>' + "".join(
                f'<li><a href="#{sid}">{st}</a></li>' for sid, st in secs) + "</ol></nav>"
            kind = "wide" if wide else "tall"
            html_ = html_.replace('src="img/', f'src="{r}manual/{"wide/" if wide else ""}img/')
            body = f"""{switch}{toc}<div class="manual{' wide' if wide else ''}">{html_}</div>
<p>From the <a href="{r}manual/{'wide/' if wide else ''}">manual for a {kind} display</a>. The deck draws every picture.</p>"""
            page(path, f"The deck UI · {kind}", title, "", body, wide=wide, toc=False,
                 description=f"The screens of the Printdeck for {title}, on a display that is mounted {kind}.")


# --- Software -------------------------------------------------------------------------------------

def os_page():
    root = "../"
    body = md("os/README.md", root, skip=("Build",))
    body += f'<h2 id="build">Build</h2><p>The build of the image is on the <a href="{root}build/">Build and release</a> page.</p>'
    page("os/", "Software", "Printdeck OS", "A small, read-only Linux system for the Printdeck, with two system slots.", body)


def deck_page():
    root = "../"
    sw = section_body("README.md", "Software").split("The screens of the touch screen:", 1)[0]
    body = f"""
<h2 id="what">What the program does</h2>
{md_text(sw, "", root)}
<p>The screens are on <a href="{root}ui/">the deck UI</a> pages. The commands of the program are in the <a href="{root}reference/commands/">reference</a>.</p>
{md("ARCHITECTURE.md", root)}
<h2 id="demo">Demo mode</h2>
{md_text(section_body("README.md", "Demo mode"), "", root)}
<h2 id="next">Work that remains</h2>
{md_text(section_body("README.md", "Work that remains"), "", root)}
"""
    page("deck/", "Software", "The printdeck program",
         "One static Go binary: the Klipper host, the touch screen, the remote screen, the API for slicers and the display of the printer.", body)


def klipper_page():
    root = "../../"
    body = md("deck/internal/klipper/PORTING.md", root)
    page("deck/klipper/", "Software", "The Klipper host", "A port of klippy, the host program of Klipper, to Go.", body)


def printer_page():
    root = "../../"
    body = md("printer/README.md", root)
    page("deck/printer/", "Software", "The reference printer", "The printer that the Klipper port is tested against.", body)


def routes(path):
    out = []
    for m in re.finditer(r'mux\.HandleFunc\("(GET|POST|DELETE) ([^"]+)",\s*(?:s\.key\()?([\w.]+)', read(path)):
        out.append((m.group(1), m.group(2), m.group(3)))
    return out


def go_doc(path):
    """The first paragraphs of the comment at the top of a Go file, up to its list of files."""
    paras, cur = [], []
    for line in read(path).split("\n"):
        if not line.startswith("//"):
            break
        text = line[2:]
        if text.startswith("\t"):
            break
        if not text.strip():
            if cur:
                paras.append("<p>" + inline(" ".join(cur), "", "") + "</p>")
                cur = []
        else:
            cur.append(text.strip())
    if cur:
        paras.append("<p>" + inline(" ".join(cur), "", "") + "</p>")
    return "\n".join(paras)


def remote_page():
    root = "../../"

    def table(rs):
        return '<div class="table-wrap"><table><tr><th>Method</th><th>Path</th></tr>' + "".join(
            f"<tr><td>{m}</td><td><code>{esc(p)}</code></td></tr>" for m, p, _ in rs) + "</table></div>"

    body = f"""
<h2 id="remote">The remote screen</h2>
{md_text(section_body("os/README.md", "The web pages"), "os", root)}
{go_doc("deck/internal/remote/remote.go")}
<p>The routes of the remote screen, from <code>deck/internal/remote/remote.go</code>:</p>
{table(routes("deck/internal/remote/remote.go"))}
<p>The system page, from <code>deck/cmd/printdeck/sysweb.go</code>:</p>
{table(routes("deck/cmd/printdeck/sysweb.go"))}
<h2 id="api">The API for slicers</h2>
{md_text(section_body("os/README.md", "Slicers"), "os", root)}
{go_doc("deck/internal/octoapi/octoapi.go")}
<p>The <a href="{root}slicer/cura-plugin/">Printdeck plugin for Cura</a> uses the same connection, and the <code>/api/plugin/printdeck</code> routes.</p>
<p>The routes of the API, from <code>deck/internal/octoapi/octoapi.go</code>. The routes under <code>/plugin/appkeys</code> need no key: they are the App Keys flow, in which the touch screen asks you to allow the slicer. All the other routes need the API key.</p>
{table(routes("deck/internal/octoapi/octoapi.go"))}
<p>The desktop app serves the same screen and the same API on <code>127.0.0.1:7351</code>, with a token in place of the password. See <a href="{root}desktop/">Desktop app and simulator</a>.</p>
"""
    page("deck/remote/", "Software", "The remote screen and the API",
         "The deck in a browser, the system page, and the OctoPrint-compatible API that slicers use.", body)


def cura_page():
    root = "../../"
    body = f"""
<figure class="frame-img white"><img src="{root}cura/img/cura-tab.png" alt="The touch screen of the deck in the Printdeck tab of Cura" loading="lazy"><figcaption>The deck in the Printdeck tab of Cura</figcaption></figure>
{md("slicer/cura-plugin/README.md", root)}
<h2 id="booklet">The booklet</h2>
<p>The booklet shows the setup, the Printdeck tab, the messages and the Print on a part tool with pictures of Cura: <a href="{root}cura/">on the web</a>, or as a <a href="{root}cura/printdeck-cura.pdf">PDF</a>. The print version <a href="{root}cura/printdeck-cura-print.pdf">printdeck-cura-print.pdf</a> has two pages on each side of a letter sheet.</p>
"""
    page("slicer/cura-plugin/", "Software", "The Cura plugin",
         "Cura follows the roll in the deck, shows the deck in a tab, and prints on a part that is printed already.", body)


def adaptive_page():
    root = "../../"
    body = md("slicer/README.md", root)
    page("slicer/adaptive-speed/", "Software", "Adaptive Speed", "A Cura post-processing script that sets the speed of each move from the geometry of its path.", body)


def flags(path, name):
    """The flags of a flag set in a Go file: (name, default, description)."""
    out = []
    src = read(path)
    consts = dict(re.findall(r'const (\w+) = "([^"]*)"', src))
    for m in re.finditer(r'fs\.(String|Bool|Int)\("(\w+)",\s*([^,]+),\s*"((?:[^"\\]|\\.)*)"\)', src):
        kind, flag, default, desc = m.groups()
        default = {"true": "on", "false": "off", '""': ""}.get(default.strip(), default.strip())
        default = consts.get(default, default)
        out.append((flag, default, desc))
    return out


def desktop_page():
    root = "../"
    fl = "".join(f"<tr><td><code>-{f}</code></td><td>{esc(d)}</td><td>{esc(t)}</td></tr>" for f, d, t in flags("deck/cmd/printdeck/app.go", "app"))
    body = f"""
{md_text(section_body("README.md", "Desktop app and simulator"), "", root)}
<h2 id="app">printdeck app</h2>
<p>The window starts <code>printdeck app</code> and shows its remote screen. The app runs the Klipper host in its own process, as the <code>klipper</code> service does on the deck, and serves the screen and the API for slicers on <code>127.0.0.1:7351</code>, so Cura finds it again after each start. Its flags, from <code>deck/cmd/printdeck/app.go</code>:</p>
<div class="table-wrap"><table><tr><th>Flag</th><th>Default</th><th>What it does</th></tr>{fl}</table></div>
<p>The folder of the app has <code>gcodes/</code>, <code>models/</code>, <code>klipper/printer.cfg</code>, <code>klipper/printdeck.cfg</code> (the app writes it at each start), <code>logs/klippy.log</code> and the API socket <code>klippy.sock</code>. A new folder gets a <code>printer.cfg</code> with only the include of the Printdeck sections: put the configuration of your printer above it. With <code>-sim</code>, a new folder gets the simulated printer (<code>deck/cmd/printdeck/simprinter.cfg</code>).</p>
<h2 id="window">The window</h2>
<p>The window program (<code>desktop/</code>) reads these environment variables:</p>
<div class="table-wrap"><table><tr><th>Variable</th><th>What it does</th></tr>
<tr><td><code>PRINTDECK</code></td><td>The path of the printdeck program. Without it, the window starts the program next to the window program.</td></tr>
<tr><td><code>PRINTDECK_ATTACH</code></td><td>A file with the first line of the output of a <code>printdeck app</code> that runs already. The window connects to that app and starts no app.</td></tr>
<tr><td><code>PRINTDECK_FILM</code></td><td>With <code>-sim</code>: the 3D window shows the film of <code>desktop/film/</code>, and records it to this file when the file with <code>.go</code> added exists.</td></tr>
<tr><td><code>PRINTDECK_SHOT</code></td><td>A PNG file: the window writes a picture of the windows after <code>PRINTDECK_SHOT_SECONDS</code> (8), and ends. With <code>PRINTDECK_SHOT_FRAMES=N</code> and <code>-sim</code>, it first writes N pictures of the 3D window, 10 a second.</td></tr>
</table></div>
<p>The deck UI in the window is drawn by the QML files of the Cura plugin (<code>slicer/cura-plugin/Printdeck/qml</code>), so the two stay the same.</p>
<h2 id="meshes">The meshes of the 3D window</h2>
{md("desktop/meshes/README.md", root)}
"""
    page("desktop/", "Software", "Desktop app and simulator", "The Printdeck for a computer, and a simulated TAZ Workhorse in 3D.", body)


def film_page():
    root = "../../"
    files = []
    for f in sorted((ROOT / "desktop" / "film").iterdir()):
        if f.suffix == ".py":
            first = []
            for line in f.read_text().split("\n"):
                if not line.startswith("#") or not line[1:].strip() or line.startswith("#   "):
                    break
                first.append(line[1:].strip())
            files.append((f.name, " ".join(first)))
    table = "".join(f"<tr><td><code>{n}</code></td><td>{esc(d)}</td></tr>" for n, d in files)
    body = f"""
<figure class="frame-img white"><img src="{root}img/sim.gif" alt="The 3D window of the simulator: a TAZ Workhorse with the Printdeck prints a plaque" loading="lazy"><figcaption>The 3D window of the simulator</figcaption></figure>
<p><code>desktop/film/</code> makes a short film of the Printdeck with the 3D window of the simulator. Two scripts run it.</p>
<h2 id="film">film.sh</h2>
{comment_block("desktop/film/film.sh")}
<h2 id="take">take.sh</h2>
{comment_block("desktop/film/take.sh")}
<h2 id="files">The files</h2>
<div class="table-wrap"><table><tr><th>File</th><th>What it does</th></tr>{table}
<tr><td><code>cards.json</code></td><td>The title cards of the film, with their times.</td></tr>
<tr><td><code>cura-header.png</code></td><td>A picture for the shot of the Cura plugin.</td></tr></table></div>
<p>The window plays the film with <code>PRINTDECK_FILM</code>: see <a href="{root}desktop/#window">the window</a>.</p>
"""
    page("desktop/film/", "Software", "The film tools", "The scripts that make the film of the Printdeck with the simulator.", body)


# --- Build and release ----------------------------------------------------------------------------

def yaml_comment(path):
    """The header comment of a workflow: the paragraphs, and the indented lines as a table of
    (trigger, what it does)."""
    paras, rows, cur, usage = [], [], [], []
    for line in read(path).split("\n"):
        if not line.startswith("#"):
            if line.startswith("on:"):
                break
            continue
        text = line[1:]
        if text.startswith("   "):
            usage.append(text[3:])
        elif text.strip():
            cur.append(text.strip())
        elif cur:
            paras.append("<p>" + inline(" ".join(cur), "", "") + "</p>")
            cur = []
    if cur:
        paras.append("<p>" + inline(" ".join(cur), "", "") + "</p>")
    # The usage lines are two columns: the event, and what runs, in the column of the first line.
    # A line that starts in that column continues the row before it.
    col = min((len(l) - len(l.lstrip()) for l in usage if l.startswith(" ")), default=0)
    for l in usage:
        if l.startswith(" "):
            rows[-1][1] += " " + l.strip()
        else:
            m = re.match(r"^(\S.*?)\s{2,}(\S.*)$", l)
            rows.append([m.group(1), m.group(2)] if m else [l[:col].strip(), l[col:].strip()])
    table = ""
    if rows:
        table = '<div class="table-wrap"><table><tr><th>Event</th><th>What runs</th></tr>' + "".join(
            f"<tr><td>{esc(a)}</td><td>{esc(b)}</td></tr>" for a, b in rows) + "</table></div>"
    return "\n".join(paras) + table


def build_page():
    root = "../"
    body = f"""
<h2 id="image">The OS image</h2>
{md_text(section_body("os/README.md", "Build"), "os", root)}
{comment_block("os/build.sh")}
<h2 id="ci">Continuous integration</h2>
<p>The workflow <code>.github/workflows/os.yml</code> builds the image on GitHub:</p>
{yaml_comment(".github/workflows/os.yml")}
<p>A release has the files <code>printdeck.img.xz</code>, <code>printdeck-update.tar.xz</code> and <code>Printdeck.curapackage</code>, the notes that GitHub generates, and the change log of <code>scripts/changelog.sh</code>. A tag with a hyphen, such as <code>v0.3.0-beta.1</code>, is a pre-release. The deck gets the betas when <a href="{root}ui/care/#update">Beta updates</a> is on.</p>
<h3>The change log</h3>
{comment_block("scripts/changelog.sh")}
<h3>The Cura plugin package</h3>
{comment_block("slicer/cura-plugin/package.sh")}
<h2 id="site">The documentation</h2>
<p>The workflow <code>.github/workflows/pages.yml</code> publishes <code>docs/</code> to GitHub Pages at <a href="{SITE}">{SITE}</a>, with the STL files and the build PDFs of the two mounts under <code>files/</code>. It runs on a push to <code>main</code> that changes those files. The pages are in the repository: these scripts write them, and a commit publishes them.</p>
<h3>The manuals</h3>
{comment_block("docs/manual/build.sh")}
<h3>The Cura plugin booklet</h3>
{comment_block("docs/cura/build.sh")}
<h3>The Slate</h3>
{comment_block("slate/make.sh")}
<p><code>slate/make_site.py</code>, the last step, writes <code>slate/guide/guide.json</code> and the images of this site in <code>docs/img/</code>.</p>
<h3>The TAZ Workhorse mount</h3>
{comment_block("lulzbot/make.sh")}
<p><code>lulzbot/make_pdf.py</code>, the last step, also writes <code>lulzbot/guide/guide.json</code> and the images in <code>lulzbot/guide/img/</code>. The upgrade booklet (<code>docs/manual/build.sh</code>) and this site read them.</p>
<h3>This site</h3>
{docstring("docs/build.py")}
"""
    page("build/", "Build and release", "Build and release", "The OS image, the CI, the releases, the manuals and this site.", body)


# --- Reference ------------------------------------------------------------------------------------

def command_table(path, prefix):
    """The table of commands in the comment at the top of a Go file: (usage, description)."""
    lines = [l[3:] for l in read(path).split("\n") if l.startswith("//\t" + prefix)]
    rows = []
    cols = []
    for l in lines:
        m = re.match(r"^(\S.*?)\s{2,}(\S.*)$", l)
        if m:
            cols.append(len(l) - len(m.group(2)))
    col = max(set(cols), key=cols.count) if cols else 0
    for l in lines:
        m = re.match(r"^(\S.*?)\s{2,}(\S.*)$", l)
        if m:
            rows.append((m.group(1), m.group(2)))
        elif len(l) > col and l[col - 1] == " ":
            rows.append((l[:col].rstrip(), l[col:].strip()))
        else:
            rows.append((l.strip(), ""))
    return rows


def usage_blocks():
    """The usage comments of the commands in the other files of deck/cmd/printdeck: command ->
    the lines of its comment."""
    out = {}
    for f in sorted((ROOT / "deck" / "cmd" / "printdeck").glob("*.go")):
        if f.name == "main.go" or f.name.endswith("_test.go"):
            continue
        lines = f.read_text().split("\n")
        for i, l in enumerate(lines):
            m = re.match(r"^//\t(printdeck (\S+).*)$", l)
            if not m:
                continue
            block = [m.group(1)]
            j = i + 1
            while j < len(lines) and re.match(r"^//\t\s+\S", lines[j]):
                block.append(lines[j][3:].rstrip())
                j += 1
            out.setdefault(m.group(2), []).append((f.name, block))
    return out


def commands_page():
    root = "../../"
    rows = command_table("deck/cmd/printdeck/main.go", "printdeck")
    table = "".join(f"<tr><td><code>{esc(u)}</code></td><td>{esc(d)}</td></tr>" for u, d in rows)
    details = []
    for cmd, blocks in sorted(usage_blocks().items()):
        for fname, block in blocks:
            details.append(f'<h4 id="{cmd}"><code>printdeck {esc(cmd)}</code> <span class="eyebrow">{fname}</span></h4><pre><code>{esc(chr(10).join(block))}</code></pre>')
    sysrows = command_table("deck/cmd/printdeck/system.go", "printdeck-")
    systable = "".join(f"<tr><td><code>{esc(u)}</code></td><td>{esc(d)}</td></tr>" for u, d in sysrows)
    usage = re.search(r'const usage = `(.*?)`', read("deck/internal/klipper/cli/cli.go"), re.S).group(1)
    kv = re.search(r'KlipperVersion = "([^"]+)"', read("deck/internal/klipper/cli/cli.go")).group(1)
    body = f"""
<p>The program is <code>printdeck</code>, in <code>deck/cmd/printdeck</code>. Without a command, it runs the application on the touch screen; systemd starts it that way on the deck. The commands, from the comment at the top of <code>main.go</code>:</p>
<div class="table-wrap"><table><tr><th>Command</th><th>What it does</th></tr>{table}</table></div>
<h2 id="details">The options of the commands</h2>
<p>The usage lines from the files of the commands.</p>
{"".join(details)}
<h2 id="klippy">printdeck klippy</h2>
<p>The Klipper host has the options of <code>klippy.py</code>. The G-code pseudo-terminal (<code>-I</code>) is not ported: the deck sends G-code through the API socket. The Klipper version of the port is <code>{esc(kv)}</code>, and the firmware of the board must be this version.</p>
<pre><code>{esc(usage)}</code></pre>
<h2 id="system">The system programs</h2>
<p>On Printdeck OS, these names are links to the printdeck program in <code>/usr/bin</code>. The program runs the program of the name it was started under.</p>
<div class="table-wrap"><table><tr><th>Program</th><th>What it does</th></tr>{systable}</table></div>
<p>The deck runs as the user <code>deck</code>. The actions that need root go through the system helper (<code>printdeck helper</code>) on a socket. See <a href="{root}os/#how-it-works">How it works</a>.</p>
"""
    page("reference/commands/", "Reference", "Commands", "The commands and the flags of the printdeck program.", body)


def env_rows():
    """The settings of the deck: (variable, default, description), from config.go and deck.env."""
    src = read("deck/internal/config/config.go")
    # The comment of each field of Config.
    field_doc, pending = {}, []
    struct = re.search(r"type Config struct \{(.*?)\n\}", src, re.S).group(1)
    for line in struct.split("\n"):
        s = line.strip()
        if s.startswith("//"):
            pending.append(s[2:].strip())
        elif s:
            names, _, trailing = s.partition("//")
            doc = " ".join(pending + ([trailing.strip()] if trailing else []))
            for n in re.split(r"[,\s]+", names.split()[0] if " " not in names.strip() else names.strip().rsplit(" ", 1)[0]):
                if n:
                    field_doc[n] = doc
            pending = []
    # The comment before each key in deck.env.
    env_doc, pending, after_key = {}, [], False
    for line in read("os/board/rootfs/etc/printdeck/deck.env").split("\n"):
        if line.startswith("#"):
            if after_key:
                pending = []
            pending.append(line[1:].strip())
            after_key = False
        elif "=" in line:
            env_doc[line.split("=")[0]] = " ".join(pending)
            after_key = True
        else:
            pending, after_key = [], False
    rows = []
    for m in re.finditer(r'(\w+):\s+(str|num)\("([A-Z_0-9]+)",\s*([^)]*)\)', src):
        field, kind, name, default = m.groups()
        default = default.strip().strip('"')
        desc = env_doc.get(name) or field_doc.get(field, "")
        rows.append((name, default, desc))
    return rows


def environment_page():
    root = "../../"
    rows = "".join(f"<tr><td><code>{n}</code></td><td><code>{esc(d) if d else '(empty)'}</code></td><td>{esc(t)}</td></tr>" for n, d, t in env_rows())
    other = [
        ("FIRMWARE_DIR", "printdeck flash", "The folder of the Klipper firmware files (default /usr/share/printdeck/firmware)."),
        ("DEMO_PREVIEW", "printdeck demo, manual", "A PNG file for the model view of the demo print."),
        ("DEMO_HEAT", "printdeck bench", "Set: the bench shows a heater that heats."),
        ("LCD_FONT", "printdeck bench", "A font for the display of the printer on the bench."),
        ("PRINTDECK_SIM_DEBUG", "printdeck app -sim", "Set: the simulated board logs its homing."),
        ("PRINTDECK_OS_CACHE", "os/build.sh", "The folder of the Buildroot tree, the downloads and the build files (default ~/.cache/printdeck-os)."),
        ("PRINTDECK_SIGNING_KEY", "os/build.sh, CI", "The ed25519 key in PEM that signs the update bundle. With no key, the bundle has no signature, and only a debug image installs it."),
        ("PRINTDECK_SIGNING_KEY_FILE", "os/build.sh", "The file of the key (default ~/.config/printdeck/update-signing-key.pem)."),
        ("GCODE_FILES", "slicer tests", "G-code files that test_adaptive_speed also checks: only the speeds may change."),
    ]
    other_rows = "".join(f"<tr><td><code>{n}</code></td><td>{esc(w)}</td><td>{esc(t)}</td></tr>" for n, w, t in other)
    body = f"""
<h2 id="deck">The settings of the deck</h2>
<p>The deck reads its settings from the environment (<code>deck/internal/config/config.go</code>). On Printdeck OS, the service <code>printdeck-deck</code> loads them from <code>/etc/printdeck/deck.env</code>, then the API key from <code>/var/lib/printdeck/api.env</code>, then <code>/var/lib/printdeck/settings.env</code>, which <code>printdeck-settings</code> writes from <code>printdeck.txt</code> on the <code>PRINTDECK</code> partition. To change a setting on a release image, put it in <code>printdeck.txt</code> in lower case, for example <code>display_rotate=90</code>. See <a href="{root}reference/files/">Files, paths and services</a>.</p>
<div class="table-wrap"><table><tr><th>Variable</th><th>Default</th><th>What it does</th></tr>{rows}</table></div>
<h3>deck.env of the image</h3>
<pre><code>{esc(read("os/board/rootfs/etc/printdeck/deck.env"))}</code></pre>
<h2 id="other">Other variables</h2>
<p>These variables are for the tools and the build. The window of the desktop app has its own: see <a href="{root}desktop/#window">the window</a>.</p>
<div class="table-wrap"><table><tr><th>Variable</th><th>Where</th><th>What it does</th></tr>{other_rows}</table></div>
"""
    page("reference/environment/", "Reference", "Environment variables", "The settings of the deck, and the variables of the tools and the build.", body)


def services():
    out = []
    for f in sorted((ROOT / "os/board/rootfs/usr/lib/systemd/system").iterdir()):
        m = re.search(r"^Description=(.*)$", f.read_text(), re.M)
        out.append((f.name, m.group(1) if m else ""))
    return out


def files_page():
    root = "../../"
    svc = "".join(f"<tr><td><code>{n}</code></td><td>{esc(d)}</td></tr>" for n, d in services())
    paths = [
        ("/etc/printdeck/deck.env", "The settings of the deck, in the image. See the environment variables."),
        ("/var/lib/printdeck/", "The state folder of the deck (STATE_DIRECTORY). Only the deck can read it."),
        ("/var/lib/printdeck/password", "The password of the web pages, from printdeck-credentials at the first start."),
        ("/var/lib/printdeck/api.env", "The API key for slicers (OCTOPRINT_API_KEY), from printdeck-credentials."),
        ("/var/lib/printdeck/settings.env", "The settings from printdeck.txt, from printdeck-settings at each start."),
        ("/var/lib/printdeck/apikeys.json", "The keys that the App Keys flow gave to slicers."),
        ("/var/lib/printdeck/spools.json", "The rolls of filament."),
        ("/var/lib/printdeck/history.json", "The history of the prints."),
        ("/var/lib/printdeck/theme, clock, material", "The theme (day, night, auto), the clock (12 or 24) and the loaded material."),
        ("/var/lib/printdeck/update-channel", "The update channel: beta, or stable."),
        ("/var/lib/printdeck/models/", "STL files for the model view (MODEL_DIR). An STL with the name of the G-code file gives a shaded model."),
        ("/var/lib/klipper/config/printer.cfg", "The Klipper configuration (KLIPPER_CONFIG)."),
        ("/var/lib/klipper/gcodes/", "The G-code files: the uploads of the slicers, the folder of [virtual_sdcard] (GCODE_DIR)."),
        ("/var/lib/klipper/logs/klippy.log", "The log of the Klipper host."),
        ("/run/klipper/klippy.sock", "The API socket of the Klipper host (KLIPPY_SOCKET)."),
        ("/usr/share/printdeck/klipper/printdeck.cfg", "The Klipper sections that Printdeck needs. printer.cfg includes it as its last line."),
        ("/usr/share/printdeck/firmware/", "The Klipper firmware files for the boards, and their version."),
        ("/var/log/journal/", "The journal, on the data partition."),
        ("/data/", "The data partition, with the writable layer over /etc, /var, /home and /root."),
    ]
    prows = "".join(f"<tr><td><code>{esc(p)}</code></td><td>{esc(d)}</td></tr>" for p, d in paths)
    cfg = [
        ("printdeck.txt", "The settings that you can change before the first start, and the settings of the touch screen. The file below."),
        ("autoboot.txt", "The A/B switch of the Pi firmware. printdeck-update and printdeck-commit change it."),
        ("printer.cfg", "A Klipper configuration that you put there. The next start copies it to Klipper and renames it to printer.cfg.imported."),
        ("printdeck-boot.log", "The kernel log, when printdeck-init cannot continue."),
    ]
    crows = "".join(f"<tr><td><code>{p}</code></td><td>{esc(d)}</td></tr>" for p, d in cfg)
    body = f"""
<h2 id="partition">The PRINTDECK partition</h2>
<p>The first partition of the disk is FAT, with the label <code>PRINTDECK</code>. Any computer can open it. See <a href="{root}os/#how-it-works">How it works</a> for the other partitions.</p>
<div class="table-wrap"><table><tr><th>File</th><th>What it is</th></tr>{crows}</table></div>
<h3>printdeck.txt</h3>
<pre><code>{esc(read("os/board/printdeck.txt"))}</code></pre>
<h2 id="paths">Paths on the deck</h2>
<div class="table-wrap"><table><tr><th>Path</th><th>What it is</th></tr>{prows}</table></div>
<h3>printdeck.cfg</h3>
<pre><code>{esc(read("os/board/rootfs/usr/share/printdeck/klipper/printdeck.cfg"))}</code></pre>
<h2 id="services">Services</h2>
<p>The units of the image, in <code>os/board/rootfs/usr/lib/systemd/system/</code>:</p>
<div class="table-wrap"><table><tr><th>Unit</th><th>Description</th></tr>{svc}</table></div>
<p>The deck serves the web pages on port 80. The address on the network is <code>printdeck.local</code> (mDNS), or the hostname of <code>printdeck.txt</code>.</p>
<h2 id="desktop">The desktop app</h2>
<p>The app keeps its files in the configuration folder of the user, in <code>Printdeck</code>, or <code>Printdeck Sim</code> with <code>-sim</code>. On Linux that is <code>~/.config/Printdeck</code>. The <code>-dir</code> flag sets another folder. The web server is on <code>127.0.0.1:7351</code>. See <a href="{root}desktop/#app">printdeck app</a>.</p>
"""
    page("reference/files/", "Reference", "Files, paths and services", "The config partition, the files of the deck, and the services of the image.", body)


def hardware_ref_page():
    root = "../../"
    lhw = "".join(f'<tr><td class="q">{x["count"]}×</td><td>{esc(x["item"])}</td><td>{esc(x["where"])}</td></tr>' for x in LULZ["hardware"])
    body = f"""
<h2 id="connections">The connections</h2>
{md_text(section_body("README.md", "Hardware"), "", root)}
<h2 id="slate">The Slate</h2>
<p>From <code>slate/guide/guide.json</code>. The page of the <a href="{root}hardware/slate/">Slate</a> has the steps.</p>
<div class="two"><div><h3>Hardware</h3>{kv_table([(f"{q}×", x) for q, x in SLATE["hardware"]])}</div>
<div><h3>Electronics</h3>{kv_table([(f"{q}×", x) for q, x in SLATE["electronics"]])}</div></div>
<h2 id="workhorse">The TAZ Workhorse mount</h2>
<p>From <code>lulzbot/guide/guide.json</code>. The page of the <a href="{root}hardware/workhorse/">mount</a> has the steps.</p>
<h3>Hardware</h3>
<div class="table-wrap"><table><tr><th></th><th>Item</th><th>Where</th></tr>{lhw}</table></div>
<h3>Electronics and cables</h3>
{kv_table([(f"{q}×", x) for q, x in LULZ["electronics"]])}
<h3>Tools</h3>
<ul>{"".join(f"<li>{esc(x)}</li>" for x in LULZ["tools"])}</ul>
"""
    page("reference/hardware/", "Reference", "Hardware lists", "The parts, the screws, the inserts and the cables of the two mounts.", body)


# ---------------------------------------------------------------------------------------------
# Write and check.

def check_links():
    """Checks that every link and picture of the site goes to a file of the site, or to a file
    that the Pages workflow adds under files/."""
    published = {}
    for f in (ROOT / "slate" / "stl").glob("*.stl"):
        published["files/" + f.name] = f
    published["files/printdeck-slate-build.pdf"] = ROOT / "slate" / "printdeck-slate-build.pdf"
    for f in (ROOT / "lulzbot" / "stl").glob("*.stl"):
        published["files/lulzbot/" + f.name] = f
    published["files/lulzbot/printdeck-lulzbot-build.pdf"] = ROOT / "lulzbot" / "printdeck-lulzbot-build.pdf"
    bad = []
    pages = sorted(DOCS.rglob("*.html"))
    ids = {p: set(re.findall(r'\bid="([^"]+)"', p.read_text())) for p in pages}
    for p in pages:
        rel = p.relative_to(DOCS).as_posix()
        for m in re.finditer(r'(?:href|src)="([^"]+)"', p.read_text()):
            url = html.unescape(m.group(1))
            if re.match(r"^(https?:|mailto:|data:|javascript:)", url):
                continue
            url, _, anchor = url.partition("#")
            if url == "":
                target = p
            else:
                target_rel = posixpath.normpath(posixpath.join(posixpath.dirname(rel), url))
                if url.endswith("/"):
                    target_rel += "/index.html"
                target = DOCS / target_rel
                if target.is_dir():
                    target = target / "index.html"
                if not target.exists() and target_rel in published and published[target_rel].exists():
                    continue
                if not target.exists():
                    bad.append(f"{rel}: {url}")
                    continue
            if anchor and target.suffix == ".html" and target in ids and anchor not in ids[target]:
                bad.append(f"{rel}: #{anchor} is not in {target.relative_to(DOCS)}")
    if bad:
        print("Links that go nowhere:")
        print("\n".join("  " + b for b in bad))
        sys.exit(1)
    print(f"checked the links of {len(pages)} pages")


def main():
    landing()
    hardware()
    slate()
    workhorse()
    ui()
    os_page()
    deck_page()
    klipper_page()
    printer_page()
    remote_page()
    cura_page()
    adaptive_page()
    desktop_page()
    film_page()
    build_page()
    commands_page()
    environment_page()
    files_page()
    hardware_ref_page()
    (DOCS / "site.css").write_text(CSS.strip() + "\n")
    for path, doc in PAGES.items():
        out = DOCS / path / "index.html"
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(doc)
    print(f"wrote {len(PAGES)} pages")
    check_links()


if __name__ == "__main__":
    main()
