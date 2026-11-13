// BLTouch mount: the line-art images for the build instructions, in the style of the
// beetlebug.org explode viewer. Uses three.js in headless Chromium.
// Run from the project folder after model.py: node lineart/render.mjs [shot ...]
// Output: build/img/raw/<shot>-light.png and <shot>-dark.png, with a transparent background.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const HERE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const RAW = path.join(HERE, "build", "img", "raw");
const MANIFEST = JSON.parse(fs.readFileSync(path.join(HERE, "build", "manifest.json"), "utf8"));

// Fill type for each material: printed, bought, dark, or clear.
const FINISH = { petg: "print", steel: "buy", bltouch: "buy", dark: "dark", frame: "dark", brass: "buy" };
const LABEL = { mount: "Mount", bltouch: "BLTouch", motor: "Motor", blower: "Blower fan", X: "X end at X 293", hot: "Nozzle" };

const PRINTED = ["bltouch_mount"];
// The frame of the meshes: x to the right of the printer, y to the back, z up. A camera is the
// direction from the part to the eye.
const FRONT_RIGHT = [1, -1.1, 0.55];  // from the front right, a little above
const RIGHT = [1, -0.04, 0.03];       // straight at the right side of the head
const FRONT = [0.03, -1, 0.04];       // straight at the front of the head
const PRINT_VIEW = [1, -1.2, 0.9];
const HEAD = ["motor", "rear", "blower", "hot"];

// name: { show, focus, explode {group: offset}, camera, labels {group: label} }.
const SHOTS = {
  hero: { show: [...HEAD, "mount", "screws", "bltouch"], focus: ["mount", "bltouch"], explode: {}, camera: FRONT_RIGHT },
  mount: { show: [...HEAD, "mount", "screws"], focus: ["mount"], explode: { mount: [30, 0, 0], screws: [55, 0, 0] }, camera: FRONT_RIGHT, labels: { screws: "Front motor screws" } },
  bltouch: { show: [...HEAD, "mount", "screws", "bltouch"], focus: ["bltouch"], explode: { bltouch: [0, 0, -30] }, camera: FRONT_RIGHT },
  side: { show: [...HEAD, "X", "mount", "screws", "bltouch"], focus: ["mount", "bltouch"], explode: {}, camera: RIGHT },
  front: { show: [...HEAD, "mount", "screws", "bltouch"], focus: ["mount", "bltouch"], explode: {}, camera: FRONT },
};

const SIZE = 2800;                    // pixels; the images are about 170 mm wide in the PDF (400 dpi)
const LABEL_PX = 60;                  // the label text, in pixels
const LINE_PX = 2.8;                  // the edge lines, in pixels

function shotParts(spec) {
  return MANIFEST.parts.filter((p) => spec.show.includes(p.group)).map((p) => ({
    url: `/build/mesh/${p.file}`,
    finish: FINISH[p.material] ?? "buy",
    label: spec.labels?.[p.group] ?? LABEL[p.group],
    focus: spec.focus.includes(p.group),
    offset: spec.explode[p.group],
  }));
}

function jobs() {
  const out = Object.entries(SHOTS).map(([name, spec]) => [name, { parts: shotParts(spec), camera: spec.camera }]);
  // Each printed part in its print orientation, from its STL file.
  for (const p of PRINTED) {
    out.push([`part_${p}`, { parts: [{ url: `/stl/${p}.stl`, finish: "print" }], camera: PRINT_VIEW }]);
  }
  return out;
}

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".mjs": "text/javascript", ".stl": "model/stl" };

function serve() {
  const server = http.createServer((req, res) => {
    const file = path.join(HERE, decodeURIComponent(new URL(req.url, "http://x").pathname));
    if (!file.startsWith(HERE) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { "content-type": TYPES[path.extname(file)] ?? "application/octet-stream" });
    fs.createReadStream(file).pipe(res);
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

async function main() {
  const only = process.argv.slice(2);
  fs.mkdirSync(RAW, { recursive: true });
  const server = await serve();
  const browser = await puppeteer.launch({
    executablePath: process.env.CHROMIUM ?? "/usr/bin/chromium",
    args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--no-sandbox"],
  });
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: SIZE, height: SIZE, deviceScaleFactor: 1 });
    page.on("pageerror", (e) => console.error("page error:", e.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/lineart/page.html`);
    await page.waitForFunction("window.pageReady === true");
    for (const [name, shot] of jobs()) {
      if (only.length && !only.includes(name)) continue;
      for (const theme of ["light", "dark"]) {
        await page.evaluate((s, t) => window.renderShot(s, t), { ...shot, size: SIZE, labelPx: LABEL_PX, linePx: LINE_PX }, theme);
        const file = path.join(RAW, `${name}-${theme}.png`);
        await (await page.$("#view")).screenshot({ path: file, omitBackground: true });
        console.log("wrote", path.relative(HERE, file));
      }
    }
  } finally {
    await browser.close();
    server.close();
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
