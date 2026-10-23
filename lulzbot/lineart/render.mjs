// Printdeck LulzBot: the line-art images for the build instructions, in the style of the
// beetlebug.org explode viewer. Uses three.js in headless Chromium. From the Slate
// (slate/lineart/render.mjs), with the shots of this mount.
// Run from lulzbot/ after model.py: node lineart/render.mjs [shot ...]
// Output: build/img/raw/<shot>-light.png and <shot>-dark.png, with a transparent background.
//
// The printer model has Y up, the front of the printer toward -Z, and its left side toward +X.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const HERE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const RAW = path.join(HERE, "build", "img", "raw");
const MANIFEST = JSON.parse(fs.readFileSync(path.join(HERE, "build", "manifest.json"), "utf8"));
// The cables, the plugs, and the inserts get groups of their own.
const REGROUP = {
  hw_plug_printer_usb: "printer_cable", hw_cable_printer: "printer_cable",
  hw_plug_pi_power: "power", hw_cable_power: "power",
  hw_plug_pi_hdmi: "pi_plugs", hw_plug_touch_usb: "pi_plugs", hw_plug_drive_usb: "pi_plugs",
  hw_cable_hdmi: "display_cables", hw_cable_touch: "display_cables", hw_cable_drive: "drive_cable",
};
const PREFIX = [
  ["hw_insert_head_back", "ins_head"], ["hw_insert_back_flange", "ins_back"],
  ["hw_insert_pod_block", "ins_block"], ["hw_pi_screw", "pi_screws"], ["hw_display_screw", "display_screws"],
  ["hw_clip_screw", "clip_screws"], ["hw_hammer_nut", "hammer_nuts"], ["clip_", "clips"],
];
for (const p of MANIFEST.parts) {
  p.group = REGROUP[p.name] ?? PREFIX.find(([k]) => p.name.startsWith(k))?.[1] ?? p.group;
}

// The head leans back 15 degrees. R points out of the face.
const T = (15 * Math.PI) / 180;
const R = [0, Math.sin(T), -Math.cos(T)];
const mul = (v, k) => v.map((c) => +(c * k).toFixed(2));
const UP = [0, 1, 0];

// Fill type for each material: printed, bought, dark, clear, or printer.
const FINISH = {
  pla_navy: "print", pla_orange: "print", pla_white: "print", pla_gray: "print",
  black_glass: "dark", black_plastic: "dark", alu_dark: "dark", black_steel: "dark", printer: "printer",
};
const LABEL = {
  head: "Head", back: "Back panel", pod: "Pod", plate: "Plate", grille: "Grille", display: "7\" display",
  drive: "Drive", pi: "Pi 5", pi_plugs: "Plugs", mount_screws: "2 × M3 × 10 + washer", bottom_screws: "2 × M3 × 8",
  head_screws: "4 × M3 × 10", flange_screws: "2 × M3 × 12", drive_screws: "4 × M3 × 6", display_screws: "4 × M3 × 6",
  pi_screws: "4 × M3 × 6", clips: "2 × rail clip", clip_screws: "2 × M3 × 8",
  hammer_nuts: "2 × M3 hammer nut",
  display_cables: "Display cables", drive_cable: "Drive cable", printer_cable: "Printer USB", power: "Power",
  ins_head: "4 × insert M3 × 7", ins_back: "2 × insert M3 × 7", ins_block: "2 × insert M3 × 5",
};

const INS = ["ins_head", "ins_back", "ins_block"];
const HEAD = ["head", "display", "display_screws", "back", "drive", "drive_screws", "head_screws", "ins_head",
  "ins_back"];
const PI = ["pi", "pi_screws"];
const CABLES = ["pi_plugs", "display_cables", "drive_cable", "printer_cable", "power"];
const ALL = ["printer", "plate", "mount_screws", "ins_block", ...PI, "clips", "clip_screws", "hammer_nuts", ...HEAD, "flange_screws",
  ...CABLES, "pod", "grille", "bottom_screws"];
const NO_CABLES = ALL.filter((g) => !CABLES.includes(g));
const FRONT_LEFT = [1.1, 0.75, -1.4];
const LEFT = [1.4, 0.5, -0.55];
const REAR_LEFT = [1.2, 0.6, 1.3];
const PRINT_VIEW = [1, -1.2, 0.9];

const each = (groups, v) => Object.fromEntries(groups.map((g) => [g, v]));

// name: { show, focus, explode {group: offset}, camera, labels {group: label} }.
// The pictures show only the hardware that each step uses.
const SHOTS = {
  hero: { show: NO_CABLES, focus: [], explode: {}, camera: FRONT_LEFT },
  side: { show: NO_CABLES, focus: [], explode: {}, camera: LEFT },
  exploded: {
    show: NO_CABLES.filter((g) => g !== "printer"), focus: [], camera: FRONT_LEFT,
    explode: { ...each(HEAD, mul(R, 70)), display: mul(R, 110), display_screws: mul(R, 70), head_screws: [0, 0, 110],
      back: [0, 0, 35], drive: [0, 0, 35], drive_screws: [0, 0, 70], ins_head: mul(R, 70), ins_back: [0, 0, 35],
      pod: [70, -20, 0], grille: [110, -20, 0], bottom_screws: [70, -70, 0], ...each(PI, [35, 0, 0]),
      mount_screws: [25, 0, 0], clips: [0, 0, -30], flange_screws: [0, 0, 30] },
  },
  step_drill: { show: ["pi"], focus: ["pi"], explode: {}, camera: [0.9, 1.3, -0.6] },
  step_display: {
    show: ["head", "display", "display_screws", "ins_head"],
    focus: ["display", "display_screws", "ins_head"], camera: [0.7, 0.9, 1.3],
    explode: { display: mul(R, -60), display_screws: mul(R, -110), ins_head: [0, 0, 30] },
  },
  step_drive: {
    show: ["back", "drive", "drive_screws", "ins_back"], focus: ["drive", "drive_screws", "ins_back"],
    explode: { drive: [0, 0, -40], drive_screws: [0, 0, 35], ins_back: [0, 0, 25] }, camera: [0.9, 0.6, -1.3],
  },
  step_back: {
    show: HEAD, focus: ["back", "drive", "drive_screws", "ins_back", "head_screws"], camera: REAR_LEFT,
    explode: { ...each(["back", "drive", "drive_screws", "ins_back"], [0, 0, 60]), head_screws: [0, 0, 100] },
    labels: each(["back", "drive", "drive_screws", "ins_back"], "Back panel + drive"),
  },
  step_plate: {
    show: ["printer", "plate", "mount_screws", "ins_block"], focus: ["plate", "mount_screws", "ins_block"],
    camera: LEFT, explode: { plate: [45, 0, 0], ins_block: [45, -25, 0], mount_screws: [90, 0, 0] },
  },
  step_pi: {
    show: ["printer", "plate", "mount_screws", "ins_block", ...PI], focus: PI, camera: LEFT,
    explode: { pi: [40, 0, 0], pi_screws: [70, 0, 0] },
  },
  step_clips: {
    show: ["printer", "plate", "mount_screws", ...PI, "clips", "clip_screws", "hammer_nuts"],
    focus: ["clips", "clip_screws", "hammer_nuts"],
    explode: { hammer_nuts: [0, 0, -15], clips: [0, 0, -40], clip_screws: [0, 0, -75] }, camera: [0.7, 0.6, -1.4],
  },
  step_hang: {
    show: ["printer", "plate", "mount_screws", ...PI, "clips", "clip_screws", ...HEAD, "flange_screws"], focus: [...HEAD, "flange_screws"],
    camera: FRONT_LEFT, explode: { ...each(HEAD, [0, 30, -25]), flange_screws: [0, 0, 30] },
    labels: each(HEAD, "Head"),
  },
  step_cables: {
    show: ["printer", "plate", "mount_screws", ...PI, "clips", "clip_screws", "display", "drive", "back", "flange_screws", ...CABLES],
    focus: CABLES.filter((g) => g !== "pi_plugs"), explode: {}, camera: [1.3, 0.55, -0.9], labels: { pi_plugs: "" },
  },
  step_pod: {
    show: NO_CABLES, focus: ["pod", "grille", "bottom_screws"], camera: LEFT,
    explode: { pod: [55, -25, 0], grille: [85, -25, 0], bottom_screws: [0, -70, 0] },
  },
  connect: {
    show: ALL, focus: ["printer_cable", "power"], camera: REAR_LEFT,
    explode: { printer_cable: [0, 0, 25], power: [0, 0, 25] },
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
  const out = Object.entries(SHOTS).map(([name, spec]) => [name, { parts: shotParts(spec), camera: spec.camera, up: UP }]);
  // Each printed part in its print orientation, from its STL file (Z up on the bed).
  for (const p of ["head", "pod", "back", "plate", "grille", "clip", "fit_display"]) {
    out.push([`part_${p}`, { parts: [{ url: `/stl/${p}.stl`, finish: "print" }], camera: PRINT_VIEW, up: [0, 0, 1] }]);
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
