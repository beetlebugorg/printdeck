// zslice-view.js: the embeddable G-code viewer of zslice. It shows a
// G-code file of any slicer in the 3D view of the zslice app, on a canvas
// of the host page. It has no tools and no panel: the host draws its own
// controls (a layer slider, the facts of the print).
//
// The parts:
//
// 1. GcodeFile reads a G-code file in pieces (zs_gcode_new, zs_gcode_feed,
//    zs_gcode_finish). It waits for the next task of the page after each
//    piece, so a large file does not stop the page. It needs no DOM.
// 2. ZsliceView owns one wasm instance, the view of the core (view.js,
//    zs_view_load_gcode), the WebGL2 drawing of the app (gl.js), and the
//    mouse, touchpad, and touch input of the app (input.js).
//
// The module uses zslice.wasm, the module with no threads. It needs no
// worker and no cross-origin isolation. See docs/embedding/viewer.md.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

import { Wasm, statusText } from "./wasm.js";
import { View3D } from "./view.js";
import { GlRenderer } from "./gl.js";
import { attachViewInput } from "./input.js";

// The size of one piece of a file for zs_gcode_feed: about 10 ms of work
// on a desktop CPU.
const PIECE = 1 << 20;

// The next task of the page: the browser can draw and take input first.
const nextTask = () => new Promise((resolve) => setTimeout(resolve, 0));

function check(rc, what) {
  if (rc !== 0) throw new Error(`zslice: ${what} failed (${statusText(rc)})`);
}

// Compiles zslice.wasm. A server that does not send application/wasm
// makes compileStreaming fail: the bytes then compile in one piece.
export async function compileWasm(url) {
  if (WebAssembly.compileStreaming) {
    try {
      return await WebAssembly.compileStreaming(fetch(url));
    } catch {
      // Not application/wasm, or no streaming: compile the bytes below.
    }
  }
  const res = await fetch(url);
  if (!res.ok) throw new Error(`zslice: ${url}: HTTP ${res.status}`);
  return WebAssembly.compile(await res.arrayBuffer());
}

// The bytes of a source, in pieces: a Uint8Array, an ArrayBuffer, a Blob
// (a dropped file), a Response (of fetch), or a ReadableStream.
async function* pieces(source) {
  if (source instanceof ArrayBuffer) source = new Uint8Array(source);
  if (ArrayBuffer.isView(source)) {
    const bytes = new Uint8Array(source.buffer, source.byteOffset, source.byteLength);
    for (let at = 0; at < bytes.length; at += PIECE) yield bytes.subarray(at, Math.min(bytes.length, at + PIECE));
    return;
  }
  if (typeof Response !== "undefined" && source instanceof Response) {
    if (!source.ok) throw new Error(`zslice: HTTP ${source.status}`);
    source = source.body;
  } else if (typeof Blob !== "undefined" && source instanceof Blob) {
    source = source.stream();
  }
  if (!source?.getReader) throw new Error("zslice: give the G-code as bytes, a Blob, a Response, or a stream");
  const reader = source.getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      for (let at = 0; at < value.length; at += PIECE) yield value.subarray(at, Math.min(value.length, at + PIECE));
    }
  } finally {
    reader.releaseLock();
  }
}

// A G-code file in wasm memory (a zs_gcode).
export class GcodeFile {
  constructor(wasm) {
    this.w = wasm;
    this.x = wasm.x;
    check(this.x.zs_gcode_new(wasm.cell), "zs_gcode_new");
    this.g = wasm.u32(wasm.cell);
  }

  // Reads all of `source` (see `pieces`). opts: onProgress(bytes) after
  // each piece; signal (an AbortSignal) stops the read.
  async read(source, { onProgress, signal } = {}) {
    const w = this.w;
    const buf = w.alloc(PIECE);
    let bytes = 0;
    let last = performance.now();
    try {
      for await (const piece of pieces(source)) {
        signal?.throwIfAborted();
        w.u8.set(piece, buf);
        check(this.x.zs_gcode_feed(this.g, buf, piece.length), "zs_gcode_feed");
        bytes += piece.length;
        onProgress?.(bytes);
        // Give the page a turn after about 30 ms of work.
        if (performance.now() - last > 30) {
          await nextTask();
          last = performance.now();
        }
      }
    } finally {
      w.free(buf, PIECE);
    }
    signal?.throwIfAborted();
    check(this.x.zs_gcode_finish(this.g), "zs_gcode_finish");
    return this;
  }

  // The facts of the file: from zs_gcode_get_info, zs_gcode_get_meta,
  // zs_gcode_get_layer, and the notes of the slicer.
  info() {
    const w = this.w;
    const x = this.x;
    // zs_gcode_info on wasm32: size, moves, beads (u64), layers (u32 and
    // 4 bytes of padding), time_s, filament_mm, box[6] (f64): 96 bytes.
    const p = w.alloc(96);
    let info;
    try {
      check(x.zs_gcode_get_info(this.g, p), "zs_gcode_get_info");
      const dv = w.dv;
      const f = (o) => dv.getFloat64(p + o, true);
      info = {
        size: Number(dv.getBigUint64(p, true)),
        moves: Number(dv.getBigUint64(p + 8, true)),
        beads: Number(dv.getBigUint64(p + 16, true)),
        layers: dv.getUint32(p + 24, true),
        seconds: f(32),
        filamentDiameter: f(40),
        box: [f(48), f(56), f(64), f(72), f(80), f(88)],
      };
    } finally {
      w.free(p, 96);
    }
    // zs_gcode_meta on wasm32: slicer (a pointer and 4 bytes of padding),
    // filament_m, filament_g (f64), weight_known (int), notes (u32).
    const m = w.alloc(32);
    try {
      check(x.zs_gcode_get_meta(this.g, m), "zs_gcode_get_meta");
      const dv = w.dv;
      info.slicer = w.cstr(dv.getUint32(m, true));
      info.meters = dv.getFloat64(m + 8, true);
      info.grams = dv.getFloat64(m + 16, true);
      info.gramsEstimated = dv.getInt32(m + 24, true) === 0;
      const n = dv.getUint32(m + 28, true);
      const settings = {};
      const c = w.cell;
      for (let i = 0; i < n; i++) {
        if (x.zs_gcode_get_note(this.g, i, c, c + 4) !== 0) continue;
        settings[w.cstr(w.u32(c))] = w.cstr(w.u32(c + 4));
      }
      info.settings = settings;
    } finally {
      w.free(m, 32);
    }
    info.layerHeights = [];
    info.layerZ = [];
    for (let i = 0; i < info.layers; i++) {
      const l = this.layer(i);
      // The core keeps these as f32: 0.30000001 becomes 0.3.
      info.layerHeights.push(Math.round(l.height * 1e4) / 1e4);
      info.layerZ.push(Math.round(l.z * 1e4) / 1e4);
    }
    return info;
  }

  // Layer i (from 0): its z and height (mm), the print time at its start
  // and end (s), and its bytes in the file.
  layer(i) {
    const w = this.w;
    // zs_gcode_layer: start, end (u64), z, height, start_s, end_s (f64).
    const p = w.alloc(48);
    try {
      if (this.x.zs_gcode_get_layer(this.g, i, p) !== 0) return null;
      const dv = w.dv;
      const f = (o) => dv.getFloat64(p + o, true);
      return { start: Number(dv.getBigUint64(p, true)), end: Number(dv.getBigUint64(p + 8, true)), z: f(16), height: f(24), startSeconds: f(32), endSeconds: f(40) };
    } finally {
      w.free(p, 48);
    }
  }

  free() {
    if (this.g) this.x.zs_gcode_free(this.g);
    this.g = 0;
  }
}

// What input.js uses of the view: the camera moves only. A destroyed
// viewer drops the view, so a listener that input.js leaves on the window
// keeps no wasm memory.
// `moved` is true after a move of the user: a resize then keeps the camera.
class Camera {
  constructor(view) {
    this.view = view;
    this.moved = false;
    this.hit = new Int32Array(2);
  }
  get loaded() { return !!this.view?.loaded; }
  orbit(dx, dy) { this.moved = true; this.view?.orbit(dx, dy); }
  pan(dx, dy) { this.moved = true; this.view?.pan(dx, dy); }
  zoomAt(f, px, py) { this.moved = true; this.view?.zoomAt(f, px, py); }
  resetSmooth() { this.moved = false; this.view?.resetSmooth(); }
  signAt() { return -1; }
  plateAt() { return -1; }
}

// A canvas for input.js: each listener gets the signal of the viewer, so
// destroy() removes them all.
function canvasWithSignal(canvas, signal) {
  return new Proxy(canvas, {
    get(t, p) {
      if (p === "addEventListener") return (type, fn, opts) => t.addEventListener(type, fn, typeof opts === "object" ? { ...opts, signal } : { capture: !!opts, signal });
      const v = t[p];
      return typeof v === "function" ? v.bind(t) : v;
    },
    set(t, p, v) {
      t[p] = v;
      return true;
    },
  });
}

export class ZsliceView {
  // Makes a viewer on a canvas. opts: wasm (the URL of zslice.wasm; the
  // default is next to this file), module (a compiled
  // WebAssembly.Module, in place of the URL), dark (true, false, or
  // "auto": the dark mode of the system; the default is "auto"),
  // byFeature (a color for each feature; the default is true), color (the
  // filament color 0xRRGGBB when byFeature is false).
  static async create(canvas, opts = {}) {
    const module = opts.module ?? (await compileWasm(opts.wasm ?? new URL("./zslice.wasm", import.meta.url)));
    if (WebAssembly.Module.imports(module).some((i) => i.kind === "memory")) {
      throw new Error("zslice: zslice-mt.wasm needs threads; use zslice.wasm");
    }
    const wasm = await Wasm.create(module);
    return new ZsliceView(canvas, wasm, opts);
  }

  constructor(canvas, wasm, opts = {}) {
    this.canvas = canvas;
    this.w = wasm;
    this.x = wasm.x;
    this.gl = new GlRenderer(canvas);
    this.view = new View3D(wasm, Math.max(1, canvas.width), Math.max(1, canvas.height));
    // The reduced view: the look of the printdeck screen, with no tools,
    // and the camera that frames the part.
    this.x.zs_view_set_reduced(this.view.v, 1);
    this.file = null;
    this.loadGen = 0;
    this.byFeature = opts.byFeature ?? true;
    this.color = opts.color ?? 0xd0d0cc;
    this.queued = 0;
    this.lastTime = 0;
    this.abort = new AbortController();
    this.camera = new Camera(this.view);
    this.setDark(opts.dark ?? "auto");

    // The mouse, the touchpad, and touch, as in the app (input.js). The
    // app state is the 3D page with no tools and no pick.
    canvas.style.touchAction = "none";
    const none = () => {};
    attachViewInput(canvasWithSignal(canvas, this.abort.signal), {
      app: { page: "3d", plates: [], activePlate: 0, state: "empty" },
      view: this.camera,
      tools: { tool: "view", pts: [] },
      requestFrame: () => this.requestFrame(),
      pickAt: none,
      pickRun: none,
      hitAt: () => false,
      select: none,
      showContextMenu: none,
      activatePlate: none,
    });

    if (typeof ResizeObserver !== "undefined") {
      this.observer = new ResizeObserver(() => this.resize());
      this.observer.observe(canvas);
    }
    this.resize();
  }

  // Reads a G-code file and shows it. source: a Uint8Array, an
  // ArrayBuffer, a Blob (a dropped file), a Response (of fetch), or a
  // ReadableStream. opts: bedX, bedY (the bed size in mm; 0 or none: a
  // bed around the part), onProgress(bytes), signal (an AbortSignal). A
  // second call stops the first. Returns info().
  async loadGcode(source, opts = {}) {
    if (!this.view) throw new Error("zslice: the viewer is destroyed");
    const gen = ++this.loadGen;
    const file = new GcodeFile(this.w);
    try {
      await file.read(source, opts);
      if (gen !== this.loadGen || !this.view) throw new DOMException("A newer load replaced this one", "AbortError");
    } catch (e) {
      file.free();
      throw e;
    }
    check(this.x.zs_view_load_gcode(this.view.v, file.g, opts.bedX ?? 0, opts.bedY ?? 0), "zs_view_load_gcode");
    this.view.loaded = true;
    // The view now points to the new file: free the old one.
    this.file?.free();
    this.file = file;
    this.facts = file.info();
    this.x.zs_view_gcode_colors(this.view.v, this.byFeature ? 1 : 0, this.color);
    this.camera.moved = false;
    this.x.zs_view_frame_part(this.view.v, 0.8);
    this.requestFrame();
    return this.info();
  }

  // The facts of the file: layers, layerHeights, layerZ, seconds, meters,
  // grams (gramsEstimated: true when the file gives no weight), slicer,
  // settings ({ key: value }), filamentDiameter, box, size, moves, beads.
  // Null before a load.
  info() {
    return this.facts ? structuredClone(this.facts) : null;
  }

  // Layer n (from 0): z, height, startSeconds, endSeconds, start, end.
  layer(n) {
    return this.file?.layer(n) ?? null;
  }

  // Shows the layers 0 to n (from 0).
  setLayer(n) {
    if (!this.file) return;
    this.x.zs_view_set_layer(this.view.v, this.clampLayer(n));
    this.requestFrame();
  }

  // Shows the layers lo to hi (from 0): the layers under lo hide, so the
  // inside of the part at lo shows.
  setLayerRange(lo, hi) {
    if (!this.file) return;
    this.view.setLayerRange(this.clampLayer(lo), this.clampLayer(hi));
    this.requestFrame();
  }

  // Shows the print at a byte offset of the file (the file position of a
  // printer): the moves before it, and its layer lit.
  setPosition(offset) {
    if (!this.file) return;
    this.x.zs_view_set_position(this.view.v, BigInt(Math.max(0, Math.floor(offset))));
    this.requestFrame();
  }

  clampLayer(n) {
    const top = Math.max(0, (this.facts?.layers ?? 1) - 1);
    return Math.max(0, Math.min(top, Math.floor(n)));
  }

  // The colors: byFeature, or one filament color (0xRRGGBB).
  setColors({ byFeature = this.byFeature, color = this.color } = {}) {
    this.byFeature = byFeature;
    this.color = color;
    if (this.view) this.x.zs_view_gcode_colors(this.view.v, byFeature ? 1 : 0, color);
    this.requestFrame();
  }

  // The dark look: true, false, or "auto" (the dark mode of the system).
  setDark(mode) {
    this.darkQuery?.removeEventListener?.("change", this.darkChange);
    this.darkQuery = null;
    const apply = (on) => {
      if (!this.view) return;
      this.x.zs_view_set_dark(this.view.v, on ? 1 : 0);
      this.gl.dark = on;
      if (this.file) this.x.zs_view_gcode_colors(this.view.v, this.byFeature ? 1 : 0, this.color);
      this.requestFrame();
    };
    if (mode === "auto" && typeof matchMedia !== "undefined") {
      this.darkQuery = matchMedia("(prefers-color-scheme: dark)");
      this.darkChange = () => apply(this.darkQuery.matches);
      this.darkQuery.addEventListener?.("change", this.darkChange);
      apply(this.darkQuery.matches);
    } else {
      apply(!!mode && mode !== "auto");
    }
  }

  // Moves the camera back to the start view around the part.
  reset() {
    if (!this.view) return;
    this.camera.moved = false;
    this.x.zs_view_frame_part(this.view.v, 0.8);
    this.requestFrame();
  }

  // Sets the size of the drawing to the size of the canvas on the page.
  // A ResizeObserver calls it; call it after a change that the observer
  // does not see.
  resize() {
    if (!this.view) return;
    const dpr = globalThis.devicePixelRatio || 1;
    const r = this.canvas.getBoundingClientRect();
    const w = Math.max(1, Math.round(r.width * dpr));
    const h = Math.max(1, Math.round(r.height * dpr));
    const changed = this.canvas.width !== w || this.canvas.height !== h;
    if (changed) {
      this.canvas.width = w;
      this.canvas.height = h;
    }
    this.view.resize(w, h);
    // The start view fills the new size, until the user moves the camera.
    if (this.file && !this.camera.moved) this.x.zs_view_frame_part(this.view.v, 0.8);
    // A new size clears the canvas: draw at once.
    if (changed) this.draw(performance.now());
    else this.requestFrame();
  }

  requestFrame() {
    if (this.queued || !this.view) return;
    this.queued = requestAnimationFrame((t) => this.draw(t));
  }

  draw(now) {
    if (this.queued) cancelAnimationFrame(this.queued);
    this.queued = 0;
    if (!this.view) return;
    const dt = this.lastTime ? Math.min(0.1, (now - this.lastTime) / 1000) : 1 / 60;
    this.lastTime = now;
    const more = this.view.animate(dt);
    this.gl.draw(this.view.frame());
    if (more) this.requestFrame();
    else this.lastTime = 0;
  }

  // Frees the view, the file, and the GPU data, and removes the
  // listeners. The canvas stays on the page.
  destroy() {
    if (!this.view) return;
    this.loadGen++;
    this.abort.abort();
    this.observer?.disconnect();
    this.darkQuery?.removeEventListener?.("change", this.darkChange);
    if (this.queued) cancelAnimationFrame(this.queued);
    this.queued = 0;
    this.x.zs_view_free(this.view.v);
    this.file?.free();
    this.file = null;
    this.view = null;
    this.camera.view = null;
    this.gl.gl.getExtension("WEBGL_lose_context")?.loseContext();
    this.gl = null;
    this.facts = null;
  }
}

export default ZsliceView;
