// Printdeck Slate: the line-art images for the build instructions and the site, in the style
// of the beetlebug.org explode viewer. Uses three.js in headless Chromium.
// Run from slate/ after model.py: node lineart/render.mjs [shot ...]
// Output: build/img/raw/<shot>-light.png and <shot>-dark.png, with a transparent background.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const HERE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const RAW = path.join(HERE, "build", "img", "raw");
const MANIFEST = JSON.parse(fs.readFileSync(path.join(HERE, "build", "manifest.json"), "utf8"));
// The plugs that you connect last, with their cable stubs, get groups of their own.
const LAST = {
  hw_plug_printer_usb: "printer", hw_stub_printer: "printer",
  hw_plug_pi_power: "power", hw_stub_power: "power", hw_plug_amp_power: "power", hw_stub_amp_power: "power",
};
for (const p of MANIFEST.parts) p.group = LAST[p.name] ?? (p.name.startsWith("hw_stub_speaker") ? "speakers" : p.group);

// The slab leans back 15 degrees. N points out of the face, U up the face. Z is up, the front is -Y.
const T = (15 * Math.PI) / 180;
const N = [0, -Math.cos(T), Math.sin(T)];
const U = [0, Math.sin(T), Math.cos(T)];
const mul = (v, k) => v.map((c) => +(c * k).toFixed(2));
const outBack = (d) => mul(N, -d);
const alongSlab = (d) => mul(U, d);

// Fill type for each material: printed, bought, dark, or clear.
const FINISH = {
  pla_navy: "print", pla_orange: "print", pla_gray: "print",
  black_glass: "dark", black_plastic: "dark", label_black: "dark", alu_dark: "dark", post_black: "dark",
};
const LABEL = {
  slab: "Slab", chin: "Chin", back: "Back plate", foot: "Foot", foot_screws: "4 × M3 × 8", side_screw_r: "M3 × 10", side_screw_l: "M3 × 10", display: "7\" display", ssd: "Drive",
  ssd_cable: "Drive cable", pi: "Pi 5 + Amp2", pi_plugs: "Plugs", printer: "Printer", power: "Power", speakers: "Speakers", fan: "30 mm fan", fan_cable: "Fan cable",
  cables: "Display cables",
};

const PRINTED = ["slab", "chin", "back", "foot"];
const CONNECT = ["printer", "power", "speakers"];
const ALL = [...PRINTED, "foot_screws", "side_screw_r", "side_screw_l", "display", "ssd", "ssd_cable", "pi", "pi_plugs", "fan", "fan_cable", "cables", ...CONNECT];
const NO_CABLES = ALL.filter((g) => !["cables", "pi_plugs", ...CONNECT].includes(g));
const BACK = ["back", "ssd", "ssd_cable", "pi", "fan", "fan_cable"];
const FRONT = [-1.4, -1.6, 0.9];      // front left, the same as the site hero
const REAR = [1.3, 1.6, 0.8];         // most steps work on the rear, as on the site
const PRINT_VIEW = [1, -1.2, 0.9];

const each = (groups, v) => Object.fromEntries(groups.map((g) => [g, v]));

// name: { show, focus, explode {group: offset}, camera, labels {group: label} }.
// The steps are the same as on the site. Groups that move as one unit share a label and a guide line.
const SHOTS = {
  hero: { show: NO_CABLES, focus: [], explode: {}, camera: FRONT },
  rear: { show: ALL, focus: [], explode: {}, camera: REAR },
  exploded: {
    show: NO_CABLES, focus: [], camera: [-1.2, 0.95, 0.55],
    explode: { display: outBack(45), ...each(BACK, outBack(110)), chin: alongSlab(-70), foot: [0, 0, -150], foot_screws: [0, 0, -190],
      side_screw_r: [30, 0, 0], side_screw_l: [-30, 0, 0] },
  },
  step1: { show: ["slab", "display"], focus: ["display"], explode: { display: outBack(80) }, camera: REAR },
  step2: {
    show: ["back", "ssd", "ssd_cable"], focus: ["ssd", "ssd_cable"],
    explode: each(["ssd", "ssd_cable"], outBack(-60)), camera: [1.8, -0.6, 0.9],
  },
  step2_pi: {
    show: ["back", "ssd", "ssd_cable", "fan", "fan_cable", "pi"], focus: ["fan", "fan_cable", "pi"],
    explode: { fan: outBack(35), fan_cable: outBack(35), pi: outBack(90) }, camera: [1.8, 0.9, 0.8],
    labels: { fan_cable: LABEL.fan },
  },
  step3: { show: ["slab", "display", ...BACK], focus: BACK, explode: each(BACK, alongSlab(-150)), camera: REAR,
    labels: each(BACK, "Back plate + Pi") },
  step4: {
    show: NO_CABLES.filter((g) => !["foot", "foot_screws"].includes(g)), focus: ["chin", "side_screw_r", "side_screw_l"],
    explode: { chin: alongSlab(-70), side_screw_r: [40, 0, 0], side_screw_l: [-40, 0, 0] }, camera: REAR,
    labels: { side_screw_l: "" },   // one label for the two screws, at the near one
  },
  // The screws go in from below, so this view is from below.
  step5: { show: NO_CABLES, focus: ["foot", "foot_screws"], explode: { foot: [0, 0, -35], foot_screws: [0, 0, -120] },
    camera: [1.4, -1.5, -0.7] },
  step6: {
    show: ALL.filter((g) => !CONNECT.includes(g)), focus: ["cables", "ssd_cable", "pi_plugs"], explode: {}, camera: REAR,
    labels: { pi_plugs: LABEL.cables },
  },
  connect: {
    show: ALL, focus: CONNECT, camera: [-1.6, 1.3, 0.8],
    explode: { printer: alongSlab(45), power: [-45, 0, 0], speakers: [-45, 0, 0] },
  },
};

const SIZE = 1600;
const LABEL_PX = 34;

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
        await page.evaluate((s, t) => window.renderShot(s, t), { ...shot, size: SIZE, labelPx: LABEL_PX }, theme);
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
