// The 3D view of the G-code of a plate on the build pages, with the zslice viewer
// (assets/zslice, docs/embedding/viewer.md of zslice). The controls follow the view of
// viewer3d.js: the layer slider at the side of the view, and under it the facts of the layer,
// the moves of the layer, the layer keys, "This layer only", and the colors.
//
// The page calls window.pwfZslice.showGcode(box, opts) for each plate, and hide() when it shows
// another page. opts: url (the G-code), bed ([x, y] in mm), color ("#rrggbb" of the filament).

import { ZsliceView, compileWasm } from "./zslice/zslice-view.js";

// The wasm module, compiled once for all the views of the page.
let module = null;
function wasmModule() {
  module ??= compileWasm(new URL("./zslice/zslice.wasm", import.meta.url));
  return module;
}

// The views of the page, by their box.
const views = new Map();

const el = (tag, cls, txt) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (txt) e.textContent = txt;
  return e;
};

// clock is a time of the print: "1 h 05 min", "45 min 37 s".
function clock(s) {
  s = Math.max(0, Math.round(s));
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
  if (h > 0) return `${h} h ${String(m).padStart(2, "0")} min`;
  if (m > 0) return `${m} min ${sec} s`;
  return `${sec} s`;
}

// rgb is the color "#rrggbb" as 0xRRGGBB, or null.
function rgb(hex) {
  return /^#[0-9a-f]{6}$/i.test(hex || "") ? parseInt(hex.slice(1), 16) : null;
}

async function showGcode(box, opts) {
  views.get(box)?.destroy();
  views.delete(box);
  box.textContent = "";
  box.classList.remove("gv-split", "gv-compact");
  const note = el("p", "part-wait", "Reading the G-code…");
  box.appendChild(note);

  const stage = el("div", "gv-stage");
  const canvas = el("canvas");
  canvas.tabIndex = 0;
  canvas.setAttribute("aria-label", "The 3D view of the plate");
  stage.appendChild(canvas);
  const main = el("div", "gv-main");
  const side = el("div", "gv-side");
  const controls = el("div", "gv-controls");
  main.append(stage, side);
  main.hidden = controls.hidden = true;
  box.append(main, controls);
  box.classList.add("gv-split");

  let v;
  try {
    v = await ZsliceView.create(canvas, { module: await wasmModule(), dark: true });
    views.set(box, v);
    main.hidden = false; // the canvas needs its size for the first frames
    const res = await fetch(opts.url);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const [bedX, bedY] = opts.bed || [];
    await v.loadGcode(res, {
      bedX, bedY,
      onProgress: (b) => { note.textContent = `Reading the G-code… ${(b / 1e6).toFixed(1)} MB`; },
    });
  } catch (e) {
    if (views.get(box) === v) {
      v?.destroy();
      views.delete(box);
    }
    main.hidden = true;
    note.textContent = "The G-code did not load: " + (e?.message || e);
    return;
  }
  if (views.get(box) !== v) return; // a newer call or hide() took the box
  const info = v.info();
  if (!info.layers) {
    v.destroy();
    views.delete(box);
    main.hidden = true;
    note.textContent = "This file has no moves to show.";
    return;
  }
  note.remove();
  controls.hidden = false;

  const n = info.layers;
  const layer = el("input");
  layer.type = "range"; layer.min = 1; layer.max = n; layer.value = n;
  layer.setAttribute("aria-label", "Layer");
  const layerNum = el("span", "gv-layer-n");
  side.append(layerNum, layer, el("span", "gv-layer-cap", "Layer"));

  const facts = el("div", "gv-info");
  const moveRow = el("label", "gv-row", "Moves ");
  const move = el("input");
  move.type = "range"; move.min = 0; move.max = 1000; move.value = 1000;
  move.setAttribute("aria-label", "The moves of the layer");
  moveRow.appendChild(move);
  const keys = el("div", "gv-keys");
  const step = (d) => {
    layer.value = Math.max(1, Math.min(n, +layer.value + d));
    move.value = 1000;
    update();
  };
  const down = el("button", "", "‹ Layer"); down.type = "button"; down.onclick = () => step(-1);
  const up = el("button", "", "Layer ›"); up.type = "button"; up.onclick = () => step(1);
  const only = el("label", "gv-check");
  const onlyBox = el("input"); onlyBox.type = "checkbox";
  only.append(onlyBox, " This layer only");
  const mode = el("select");
  mode.setAttribute("aria-label", "The colors of the lines");
  const filament = rgb(opts.color);
  for (const [val, t] of [["kind", "Color by line type"], ["filament", "Color of the filament"]]) {
    if (val === "filament" && filament == null) continue;
    const o = el("option", "", t); o.value = val; mode.appendChild(o);
  }
  const reset = el("button", "", "Start view"); reset.type = "button"; reset.onclick = () => v.reset();
  keys.append(down, up, only, mode, reset);
  controls.append(facts, moveRow, keys);

  // update shows the layers up to the layer slider (or that layer only), and in the top layer
  // the moves up to the move slider.
  const update = () => {
    const L = +layer.value - 1; // from 0
    const lay = v.layer(L);
    const m = +move.value;
    if (m < 1000 && lay && lay.end > lay.start) {
      v.setPosition(Math.round(lay.start + ((lay.end - lay.start) * m) / 1000));
    } else if (onlyBox.checked) {
      v.setLayerRange(L, L);
    } else {
      v.setLayer(L);
    }
    layerNum.textContent = String(L + 1);
    let text = `Layer ${L + 1} of ${n}`;
    if (lay) {
      text += ` · Z ${lay.z.toFixed(2)} mm`;
      if (lay.endSeconds > 0) text += ` · ${clock(lay.endSeconds)} into the print`;
    }
    facts.textContent = text;
  };
  layer.oninput = () => { move.value = 1000; update(); };
  move.oninput = update;
  onlyBox.onchange = update;
  mode.onchange = () => v.setColors(mode.value === "kind" ? { byFeature: true } : { byFeature: false, color: filament });
  // Click the view, then the arrow keys move the layer.
  canvas.addEventListener("keydown", (e) => {
    const d = { ArrowUp: 1, ArrowRight: 1, ArrowDown: -1, ArrowLeft: -1 }[e.key];
    if (d) {
      e.preventDefault();
      step(d);
    }
  });
  update();
}

// hide frees the views of the page: the page calls it when it shows another page.
function hide() {
  for (const v of views.values()) v.destroy();
  views.clear();
}

window.pwfZslice = { showGcode, hide };
