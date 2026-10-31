// The 3D view of a printer page: the model of the printer, which the viewer turns with a finger
// or the mouse, and the picture of a print over the bed. The page (cmd/pwf-web) calls
// pwfView3d.show. three.js loads only when a page shows a view.

const three = "./three-r170/";
let lib; // the three.js modules, after the first show
let view; // the view now: { box, renderer, stop }

// The model file of a printer, from its model name, or "" for the plain model of its process.
function modelFile(model) {
  const m = (model || "").toLowerCase();
  if (m.includes("taz") || m.includes("workhorse")) return "/assets/models/taz.glb";
  if (m.includes("phenom")) return "/assets/models/phenom.glb";
  return "";
}

async function load() {
  if (!lib) {
    const [T, G, O, R] = await Promise.all([
      import(three + "three.module.min.js"),
      import(three + "GLTFLoader.js"),
      import(three + "OrbitControls.js"),
      import(three + "RoomEnvironment.js"),
    ]);
    lib = { T, GLTFLoader: G.GLTFLoader, OrbitControls: O.OrbitControls, RoomEnvironment: R.RoomEnvironment };
  }
  return lib;
}

// plain is the model of a printer with no model file: a frame and a bed for "fdm", a box with a
// vat and a plate for "resin". It returns the group and its node "bed".
function plain(T, process) {
  const g = new T.Group();
  const mat = (c) => new T.MeshStandardMaterial({ color: c, roughness: 0.75 });
  const box = (w, h, d, x, y, z, c) => {
    const m = new T.Mesh(new T.BoxGeometry(w, h, d), mat(c));
    m.position.set(x, y, z);
    g.add(m);
  };
  const bed = new T.Group();
  bed.name = "bed";
  if (process === "resin") {
    box(400, 200, 340, 0, 100, 0, "#2b2d31");
    box(300, 40, 200, 0, 220, 0, "#c9cdd2");
    box(60, 500, 30, 0, 450, -150, "#8e959c");
    box(260, 10, 160, 0, 400, 0, "#b9bec6");
    bed.position.set(0, 395, 0);
    bed.rotation.x = Math.PI;
  } else {
    for (const x of [-230, 230]) box(20, 520, 20, x, 260, -150, "#3a3b3e");
    box(480, 20, 20, 0, 510, -150, "#3a3b3e");
    box(480, 20, 360, 0, 10, 0, "#3a3b3e");
    box(300, 6, 300, 0, 60, 0, "#c4c8cf");
    bed.position.set(0, 63, 0);
  }
  g.add(bed);
  return g;
}

// show draws the view of a printer in the element box. opts: model (the model name of the
// printer), process ("fdm" or "resin"), picture (the URL of the picture of a print, or ""), and
// credit (the element for the credit of the model file).
async function show(box, opts) {
  hide();
  const { T, GLTFLoader, OrbitControls } = await load();
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  box.textContent = "";
  box.appendChild(renderer.domElement);
  const scene = new T.Scene();
  scene.add(new T.HemisphereLight(0xffffff, 0x8890a0, 2.4));
  const sun = new T.DirectionalLight(0xffffff, 1.6);
  sun.position.set(0.6, 1.4, 1.2);
  scene.add(sun);
  const camera = new T.PerspectiveCamera(32, 1, 10, 20000);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  controls.autoRotate = true;
  controls.autoRotateSpeed = 0.6;
  controls.addEventListener("start", () => (controls.autoRotate = false));

  let printer;
  const file = modelFile(opts.model);
  if (file) {
    try {
      printer = (await new GLTFLoader().loadAsync(file)).scene;
    } catch (e) {
      printer = null;
    }
  }
  if (opts.credit) opts.credit.hidden = !(printer && file.includes("taz"));
  if (!printer) printer = plain(T, opts.process);
  scene.add(printer);

  // The camera: in front of the printer, to the right and over it.
  const bounds = new T.Box3().setFromObject(printer);
  const mid = bounds.getCenter(new T.Vector3());
  const size = bounds.getSize(new T.Vector3()).length();
  camera.position.set(mid.x + size * 0.5, mid.y + size * 0.3, mid.z + size * 0.95);
  controls.target.copy(mid);
  controls.minDistance = size * 0.3;
  controls.maxDistance = size * 3;

  // The picture of the print, over the middle of the bed.
  const bed = printer.getObjectByName("bed");
  let pic = null;
  if (opts.picture && bed) {
    pic = document.createElement("img");
    pic.className = "view3d-pic";
    pic.alt = "";
    pic.src = opts.picture;
    box.appendChild(pic);
  }
  const anchor = new T.Vector3();

  const fit = () => {
    const w = box.clientWidth, h = box.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
  };
  const resize = new ResizeObserver(fit);
  resize.observe(box);
  fit();

  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    controls.update();
    renderer.render(scene, camera);
    if (pic) {
      // 60 mm over the bed, as the part stands on it.
      bed.localToWorld(anchor.set(0, 60, 0));
      anchor.project(camera);
      pic.style.left = ((anchor.x + 1) / 2) * box.clientWidth + "px";
      pic.style.top = ((1 - anchor.y) / 2) * box.clientHeight + "px";
      pic.hidden = anchor.z > 1;
    }
  };
  tick();
  view = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      controls.dispose();
      renderer.dispose();
      box.textContent = "";
    },
  };
}

// hide stops the view now, if any.
function hide() {
  if (view) {
    view.stop();
    view = null;
  }
}

// readCells reads part.bin (gzip, "PDC1"): the share of resin in each cell of a resin part, from 0
// to 255 (see deck/internal/chitu/coverage.go). slice(z) returns the shares of the slice z.
async function readCells(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error("part.bin: " + res.status);
  const buf = await new Response(res.body.pipeThrough(new DecompressionStream("gzip"))).arrayBuffer();
  const dv = new DataView(buf), bytes = new Uint8Array(buf);
  const magic = String.fromCharCode(...bytes.subarray(0, 4));
  if (magic !== "PDC1") throw new Error("part.bin: " + magic);
  const W = dv.getUint16(12, true), H = dv.getUint16(14, true), D = dv.getUint16(16, true);
  const at = new Uint32Array(D);
  let o = 20 + D * 4;
  for (let z = 0; z < D; z++) {
    at[z] = o;
    o += 4 + dv.getUint32(o, true);
  }
  const slice = (z) => {
    const out = new Uint8Array(W * H);
    let p = at[z], end = p + 4 + dv.getUint32(p, true), i = 0;
    p += 4;
    while (p < end) {
      const v = bytes[p++];
      let run = 0, shift = 0, b;
      do { b = bytes[p++]; run += (b & 0x7f) * 2 ** shift; shift += 7; } while (b & 0x80);
      if (v) out.fill(v, i, i + run);
      i += run;
    }
    return out;
  };
  return { cell: dv.getFloat32(4, true), slice: dv.getFloat32(8, true), W, H, D, read: slice };
}

// surfaceNet builds the surface of the part where the share of resin crosses one half, with f x f
// cells as one in X and Y (f = 1 is all the detail): a vertex in each box of 8 cells that the
// surface crosses, at the middle of the crossings on its edges, and a quad for each edge that it
// crosses. The shares put each crossing at its place in the cell. It returns null with more than
// maxVerts vertices.
function surfaceNet(c, f, maxVerts) {
  const W = Math.ceil(c.W / f), H = Math.ceil(c.H / f), D = c.D;
  const field = (z) => {
    const out = new Float32Array(W * H);
    if (z < 0 || z >= D) return out;
    const s = c.read(z);
    if (f === 1) {
      for (let i = 0; i < out.length; i++) out[i] = s[i] / 255;
      return out;
    }
    for (let y = 0; y < c.H; y++) for (let x = 0; x < c.W; x++) out[((y / f) | 0) * W + ((x / f) | 0)] += s[y * c.W + x];
    for (let i = 0; i < out.length; i++) out[i] /= 255 * f * f;
    return out;
  };
  const iso = 0.5;
  const at = (fl, x, y) => (x < 0 || y < 0 || x >= W || y >= H ? 0 : fl[y * W + x]);
  let verts = new Float32Array(1 << 20), nv = 0;
  let tris = new Uint32Array(1 << 21), nt = 0;
  const cw = W + 1, ch = H + 1;
  let prev = new Int32Array(cw * ch).fill(-1), cur = new Int32Array(cw * ch);
  let f0 = field(-1);
  const E = [[0,1],[2,3],[4,5],[6,7],[0,2],[1,3],[4,6],[5,7],[0,4],[1,5],[2,6],[3,7]];
  const v = new Float32Array(8);
  for (let cz = -1; cz < D; cz++) {
    const f1 = field(cz + 1);
    cur.fill(-1);
    for (let cy = -1; cy < H; cy++) for (let cx = -1; cx < W; cx++) {
      let mask = 0;
      for (let k = 0; k < 8; k++) {
        v[k] = at(k & 4 ? f1 : f0, cx + (k & 1), cy + ((k >> 1) & 1));
        if (v[k] >= iso) mask |= 1 << k;
      }
      if (mask === 0 || mask === 255) continue;
      let sx = 0, sy = 0, sz = 0, n = 0;
      for (const [a, b] of E) {
        if (((mask >> a) & 1) === ((mask >> b) & 1)) continue;
        const t = (iso - v[a]) / (v[b] - v[a]);
        sx += (a & 1) + t * ((b & 1) - (a & 1));
        sy += ((a >> 1) & 1) + t * (((b >> 1) & 1) - ((a >> 1) & 1));
        sz += ((a >> 2) & 1) + t * (((b >> 2) & 1) - ((a >> 2) & 1));
        n++;
      }
      if (nv * 3 + 3 > verts.length) { const g = new Float32Array(verts.length * 2); g.set(verts); verts = g; }
      verts[nv * 3] = cx + sx / n; verts[nv * 3 + 1] = cy + sy / n; verts[nv * 3 + 2] = cz + sz / n;
      const idx = nv++;
      if (nv > maxVerts) return null;
      const ci = (cy + 1) * cw + (cx + 1);
      cur[ci] = idx;
      for (let axis = 0; axis < 3; axis++) {
        if ((mask & 1) === ((mask >> (1 << axis)) & 1)) continue;
        let q;
        if (axis === 0) { if (cy < 0 || cz < 0) continue; q = [cur[ci], cur[ci - cw], prev[ci - cw], prev[ci]]; }
        else if (axis === 1) { if (cx < 0 || cz < 0) continue; q = [cur[ci], prev[ci], prev[ci - 1], cur[ci - 1]]; }
        else { if (cx < 0 || cy < 0) continue; q = [cur[ci], cur[ci - 1], cur[ci - 1 - cw], cur[ci - cw]]; }
        if (q[0] < 0 || q[1] < 0 || q[2] < 0 || q[3] < 0) continue;
        if (!(mask & 1)) { const t = q[1]; q[1] = q[3]; q[3] = t; } // the quad faces out of the part
        if (nt * 3 + 6 > tris.length) { const g = new Uint32Array(tris.length * 2); g.set(tris); tris = g; }
        tris.set([q[0], q[1], q[2], q[0], q[2], q[3]], nt * 3);
        nt += 2;
      }
    }
    const t = prev; prev = cur; cur = t;
    f0 = f1;
  }
  return { verts: verts.subarray(0, nv * 3), tris: tris.subarray(0, nt * 3), nv, W, H, f };
}

// readPart reads part.bin and builds the geometry of the part in mm, hanging under the build plate
// as it prints: Y down from the plate. The cells turn a half turn about Y: the first row of the
// screen is at the front of the printer, and its first column at the right, as a person sees the
// part from the front. It takes as much detail as the device holds.
async function readPart(T, url) {
  const c = await readCells(url);
  // All the detail first; cells twice as big only when the surface has too many vertices for
  // the device.
  const big = !/Mobi|Android|iPhone|iPad/.test(navigator.userAgent);
  const maxVerts = big ? 8000000 : 2500000;
  let f = 1;
  let m = null;
  while (!(m = surfaceNet(c, f, maxVerts))) f++;
  const cell = c.cell * f, pos = new Float32Array(m.nv * 3), height = new Float32Array(m.nv);
  for (let i = 0; i < m.nv; i++) {
    const x = m.verts[i * 3], y = m.verts[i * 3 + 1], z = m.verts[i * 3 + 2];
    pos[i * 3] = -(x + 0.5 - m.W / 2) * cell;
    pos[i * 3 + 1] = -(z + 0.5) * c.slice;
    pos[i * 3 + 2] = -(y + 0.5 - m.H / 2) * cell;
    height[i] = (z + 0.5) * c.slice;
  }
  const g = new T.BufferGeometry();
  g.setAttribute("position", new T.BufferAttribute(pos, 3));
  g.setIndex(new T.BufferAttribute(new Uint32Array(m.tris), 1));
  g.computeVertexNormals();
  g.setAttribute("color", new T.BufferAttribute(new Float32Array(m.nv * 3), 3));
  return { g, height, cellMM: cell, plateW: c.W * c.cell, plateD: c.H * c.cell };
}

// paint colors the part: the height up to done (mm) in the color of the resin, the rest gray.
function paint(T, part, done) {
  const col = part.g.getAttribute("color");
  const cured = new T.Color("#e8762b"), gray = new T.Color("#8d9197");
  for (let i = 0; i < part.height.length; i++) {
    const c = part.height[i] <= done ? cured : gray;
    col.setXYZ(i, c.r, c.g, c.b);
  }
  col.needsUpdate = true;
}

// showPart draws the part of part.bin in box, under the build plate of the printer, as it prints
// on a resin printer. opts: url (part.bin), model (the model name of the printer), done (the
// height that is done, in mm). It returns a function that sets done again.
async function showPart(box, opts) {
  hide();
  const { T, OrbitControls, RoomEnvironment } = await load();
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.toneMapping = T.ACESFilmicToneMapping;
  box.textContent = "";
  box.appendChild(renderer.domElement);
  const scene = new T.Scene();
  // A studio around the part for its reflections, as a real part looks under room light.
  const pmrem = new T.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.35;
  scene.add(new T.HemisphereLight(0xffffff, 0x50555f, 0.5));
  const key = new T.DirectionalLight(0xffffff, 1.4);
  key.position.set(-1, 1.6, 1.4);
  scene.add(key);
  const rim = new T.DirectionalLight(0xffe2c4, 1.2);
  rim.position.set(1.2, 0.4, -1);
  scene.add(rim);
  const camera = new T.PerspectiveCamera(30, 1, 1, 20000);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;

  // The build plate only, with the part under it: the case of the printer is in the way. A full
  // plate takes some seconds to build: a note says so.
  const note = document.createElement("p");
  note.className = "part-wait";
  note.textContent = "Building the 3D part from the layers…";
  box.appendChild(note);
  await new Promise((r) => setTimeout(r, 50));
  const part = await readPart(T, opts.url);
  note.remove();
  paint(T, part, opts.done || 0);
  const plateMat = new T.MeshStandardMaterial({ color: "#b8bcc2", metalness: 0.3, roughness: 0.45 });
  const plate = new T.Mesh(new T.BoxGeometry(part.plateW + 8, 6, part.plateD + 8), plateMat);
  plate.position.y = 3;
  scene.add(plate);
  const arm = new T.Mesh(new T.BoxGeometry(60, 24, 30), plateMat);
  arm.position.set(0, 18, -part.plateD / 2 + 10);
  scene.add(arm);
  const mat = new T.MeshPhysicalMaterial({ vertexColors: true, roughness: 0.32, metalness: 0, clearcoat: 0.5,
    clearcoatRoughness: 0.2, envMapIntensity: 0.9 });
  const mesh = new T.Mesh(part.g, mat);
  scene.add(mesh);
  scene.updateMatrixWorld(true);

  // The camera: in front of the part, a little to the right and under it, as it hangs.
  const bounds = new T.Box3().setFromObject(mesh);
  const mid = bounds.getCenter(new T.Vector3());
  const size = Math.max(40, bounds.getSize(new T.Vector3()).length());
  camera.position.set(mid.x + size * 0.9, mid.y - size * 0.2, mid.z + size * 1.8);
  controls.target.copy(mid);
  controls.minDistance = size * 0.2;
  controls.maxDistance = size * 12;

  const fit = () => {
    const w = box.clientWidth, h = box.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
  };
  const resize = new ResizeObserver(fit);
  resize.observe(box);
  fit();
  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    controls.update();
    renderer.render(scene, camera);
  };
  tick();
  view = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      controls.dispose();
      renderer.dispose();
      part.g.dispose();
      box.textContent = "";
    },
  };
  return (done) => paint(T, part, done);
}

// readSTL reads an STL file (binary or text) into a geometry in mm, with Z up as in the file.
async function readSTL(T, url) {
  const resp = await fetch(url, { credentials: "same-origin" });
  if (!resp.ok) throw new Error("the model did not load: " + resp.status);
  const buf = await resp.arrayBuffer();
  const dv = new DataView(buf);
  let pos;
  const n = buf.byteLength >= 84 ? dv.getUint32(80, true) : 0;
  if (buf.byteLength === 84 + 50 * n) {
    pos = new Float32Array(n * 9);
    for (let i = 0; i < n; i++) {
      for (let k = 0; k < 9; k++) pos[i * 9 + k] = dv.getFloat32(84 + 50 * i + 12 + k * 4, true);
    }
  } else {
    const text = new TextDecoder().decode(buf);
    const vs = [...text.matchAll(/vertex\s+(\S+)\s+(\S+)\s+(\S+)/g)].map((m) => [+m[1], +m[2], +m[3]]);
    pos = new Float32Array(vs.flat());
  }
  const g = new T.BufferGeometry();
  g.setAttribute("position", new T.BufferAttribute(pos, 3));
  g.computeVertexNormals();
  return g;
}

// showModels draws the models of a part in box, on a bed with a grid of 10 mm, as the part
// stands on the printer. opts: urls (STL files, in place to each other), color, bed ([x, y] in
// mm, or nothing), note (the element of a message while it loads).
async function showModels(box, opts) {
  hide();
  const { T, OrbitControls, RoomEnvironment } = await load();
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.toneMapping = T.ACESFilmicToneMapping;
  box.textContent = "";
  box.appendChild(renderer.domElement);
  const scene = new T.Scene();
  const pmrem = new T.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.35;
  scene.add(new T.HemisphereLight(0xffffff, 0x50555f, 0.6));
  const key = new T.DirectionalLight(0xffffff, 1.4);
  key.position.set(-1, 1.6, 1.4);
  scene.add(key);
  const camera = new T.PerspectiveCamera(30, 1, 1, 20000);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;
  const geos = await Promise.all((opts.urls || []).map((u) => readSTL(T, u)));
  // The models stand on the bed: Z of the file is up, and the middle of all of them is at 0.
  const group = new T.Group();
  const mat = new T.MeshPhysicalMaterial({ color: opts.color || "#e8762b", roughness: 0.45, clearcoat: 0.3, envMapIntensity: 0.8 });
  for (const g of geos) group.add(new T.Mesh(g, mat));
  group.rotation.x = -Math.PI / 2;
  scene.add(group);
  scene.updateMatrixWorld(true);
  const b = new T.Box3().setFromObject(group);
  const mid = b.getCenter(new T.Vector3());
  group.position.set(-mid.x, -b.min.y, -mid.z);
  scene.updateMatrixWorld(true);
  const size = b.getSize(new T.Vector3());
  const [bx, by] = opts.bed || [Math.max(60, size.x * 1.6), Math.max(60, size.z * 1.6)];
  const bed = new T.Mesh(new T.BoxGeometry(bx, 2, by), new T.MeshStandardMaterial({ color: "#2b2d31", roughness: 0.8 }));
  bed.position.y = -1;
  scene.add(bed);
  const grid = new T.GridHelper(Math.max(bx, by), Math.round(Math.max(bx, by) / 10), 0x5b5f66, 0x3a3d42);
  grid.position.y = 0.05;
  scene.add(grid);
  const len = Math.max(40, size.length());
  camera.position.set(len * 0.9, len * 0.8, len * 1.4);
  controls.target.set(0, size.y / 2, 0);
  controls.minDistance = len * 0.2;
  controls.maxDistance = len * 12;
  const fit = () => {
    const w = box.clientWidth, h = box.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
  };
  const resize = new ResizeObserver(fit);
  resize.observe(box);
  fit();
  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    controls.update();
    renderer.render(scene, camera);
  };
  tick();
  view = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      controls.dispose();
      renderer.dispose();
      geos.forEach((g) => g.dispose());
      box.textContent = "";
    },
  };
}

// The G-code preview of a plate: the moves of the file as the printer makes them.

// The kinds of lines of CuraEngine, their names on the page, and the colors of the layer view of
// Cura (resources/themes/cura-light/theme.json, LGPL-3.0). Support is half see-through in Cura:
// here it is the same cyan on the dark background.
const KINDS = [
  ["WALL-OUTER", "Outer wall", "#e60000"],
  ["WALL-INNER", "Inner wall", "#00e600"],
  ["SKIN", "Top and bottom", "#e6e600"],
  ["FILL", "Infill", "#e67300"],
  ["SUPPORT", "Support", "#00a8a8"],
  ["SUPPORT-INTERFACE", "Support interface", "#3f7fff"],
  ["SKIRT", "Skirt and brim", "#00e6e6"],
  ["PRIME-TOWER", "Prime tower", "#00ffff"],
  ["OTHER", "Other", "#ffffff"],
];

// beadGeometry is one extrusion of length 1 along X, with an oval section of the height h and
// the width w of the line, as the plastic lies on the layer below. Each layer is a row of
// beads, so the light shows each layer line, as on a real print.
function beadGeometry(T, h, w) {
  const seg = 12;
  const pos = [], nor = [], idx = [];
  const ring = [];
  for (let k = 0; k < seg; k++) {
    const t = (k / seg) * Math.PI * 2;
    const y = (h / 2) * Math.sin(t), z = (w / 2) * Math.cos(t);
    const ny = Math.sin(t) / (h / 2), nz = Math.cos(t) / (w / 2), nl = Math.hypot(ny, nz);
    ring.push([y, z, ny / nl, nz / nl]);
  }
  for (const x of [-0.5, 0.5]) for (const [y, z, ny, nz] of ring) { pos.push(x, y, z); nor.push(0, ny, nz); }
  for (let k = 0; k < seg; k++) {
    const a = k, b = (k + 1) % seg, c = seg + k, d = seg + ((k + 1) % seg);
    idx.push(a, c, b, b, c, d);
  }
  // The ends.
  for (const [x, sx] of [[-0.5, -1], [0.5, 1]]) {
    const base = pos.length / 3;
    pos.push(x, 0, 0); nor.push(sx, 0, 0);
    for (const [y, z] of ring) { pos.push(x, y, z); nor.push(sx, 0, 0); }
    for (let k = 0; k < seg; k++) {
      const a = base + 1 + k, b = base + 1 + ((k + 1) % seg);
      if (sx > 0) idx.push(base, a, b); else idx.push(base, b, a);
    }
  }
  const g = new T.BufferGeometry();
  g.setAttribute("position", new T.Float32BufferAttribute(pos, 3));
  g.setAttribute("normal", new T.Float32BufferAttribute(nor, 3));
  g.setIndex(idx);
  return g;
}
const KIND = Object.fromEntries(KINDS.map(([k], i) => [k, i]));

// parseGcode reads the moves of a G-code. It returns the extrusions (x0, y0, x1, y1, z, h, width
// in mm, the kind, the feed rate in mm/s) and the travels, each in the order of the file, with
// the first move of each layer, and the time at the end of each layer.
function parseGcode(text) {
  let n = 0, cap = 1 << 16;
  let seg = new Float32Array(cap * 7), kind = new Uint8Array(cap), speed = new Float32Array(cap);
  let tn = 0, tcap = 1 << 14, trav = new Float32Array(tcap * 6);
  const layerSeg = [], layerTrav = [], layerZ = [], layerTime = [];
  let x = 0, y = 0, z = 0, e = 0, f = 0, rel = false, cur = KIND.OTHER, layerH = 0.2, lastZ = 0, maxE = 0;
  const grow = () => {
    cap *= 2;
    const s2 = new Float32Array(cap * 7); s2.set(seg); seg = s2;
    const k2 = new Uint8Array(cap); k2.set(kind); kind = k2;
    const v2 = new Float32Array(cap); v2.set(speed); speed = v2;
  };
  let pos = 0;
  const len = text.length;
  while (pos < len) {
    let end = text.indexOf("\n", pos);
    if (end < 0) end = len;
    const line = text.slice(pos, end);
    pos = end + 1;
    const c0 = line.charCodeAt(0);
    if (c0 === 59) { // ";"
      if (line.startsWith(";LAYER:")) {
        layerSeg.push(n); layerTrav.push(tn); layerZ.push(z);
      } else if (line.startsWith(";TYPE:")) {
        const k = KIND[line.slice(6).trim()];
        cur = k === undefined ? KIND.OTHER : k;
      } else if (line.startsWith(";TIME_ELAPSED:")) {
        layerTime[layerSeg.length - 1] = parseFloat(line.slice(14));
      } else if (line.startsWith(";Layer height:")) {
        layerH = parseFloat(line.slice(14)) || layerH;
      }
      continue;
    }
    if (c0 !== 71 && c0 !== 77) continue; // G, M
    const code = line.split(";", 1)[0].trim();
    if (code === "M82") { rel = false; continue; }
    if (code === "M83") { rel = true; continue; }
    const parts = code.split(/\s+/);
    const g = parts[0];
    if (g === "G92") {
      for (const p of parts) if (p[0] === "E") { e = parseFloat(p.slice(1)); maxE = e; }
      continue;
    }
    if (g !== "G0" && g !== "G1") continue;
    let nx = x, ny = y, nz = z, ne = null;
    for (let i = 1; i < parts.length; i++) {
      const p = parts[i], v = parseFloat(p.slice(1));
      switch (p[0]) {
        case "X": nx = v; break;
        case "Y": ny = v; break;
        case "Z": nz = v; break;
        case "E": ne = v; break;
        case "F": f = v; break;
      }
    }
    let extrude = false;
    if (ne !== null) {
      const de = rel ? ne : ne - e;
      // An extrusion adds filament: an E over the highest E so far (an unretract does not count).
      if (rel) extrude = de > 0 && (nx !== x || ny !== y);
      else { extrude = ne > maxE + 1e-5 && (nx !== x || ny !== y); if (ne > maxE) maxE = ne; }
      e = rel ? e + ne : ne;
    }
    if (nz !== z) { if (nz > z) lastZ = z; z = nz; }
    if (nx !== x || ny !== y) {
      if (extrude) {
        if (n >= cap) grow();
        const o = n * 7;
        const h = Math.max(0.05, Math.min(1, z - lastZ || layerH));
        seg[o] = x; seg[o + 1] = y; seg[o + 2] = nx; seg[o + 3] = ny; seg[o + 4] = z; seg[o + 5] = h; seg[o + 6] = 0.5;
        kind[n] = cur; speed[n] = f / 60; n++;
      } else {
        if (tn >= tcap) { tcap *= 2; const t2 = new Float32Array(tcap * 6); t2.set(trav); trav = t2; }
        const o = tn * 6;
        trav[o] = x; trav[o + 1] = y; trav[o + 2] = z; trav[o + 3] = nx; trav[o + 4] = ny; trav[o + 5] = z;
        tn++;
      }
      x = nx; y = ny;
    }
  }
  layerSeg.push(n); layerTrav.push(tn);
  return { n, seg, kind, speed, tn, trav, layerSeg, layerTrav, layerZ, layerTime, layers: layerSeg.length - 1 };
}

// readText reads the text of the file at url. A file with a name that ends in .gz is a G-code with
// gzip: the browser decompresses it.
async function readText(url) {
  const r = await fetch(url, { credentials: "same-origin" });
  if (!r.ok) throw new Error("the G-code did not load: " + r.status);
  if (!new URL(url, location.href).pathname.endsWith(".gz")) return r.text();
  const plain = r.body.pipeThrough(new DecompressionStream("gzip"));
  return new Response(plain).text();
}

// The color of the layers that did not print yet (opts.printed).
const GHOST = "#4a4e57";

// showGcode draws the G-code of a plate in box, with its controls: the layer, the moves of the
// layer, the kinds of lines, the travels, and the colors by kind or by speed. opts: url, bed
// ([x, y] in mm), title; compact: no controls, for the page of a print; printed: the count of
// the layers that printed, or null for all: the later layers are gray. The viewer turns all
// around the plate, also under the bed. It returns a handle with setPrinted(n).
async function showGcode(box, opts) {
  hide();
  box.textContent = "";
  const note = document.createElement("p");
  note.className = "part-wait";
  note.textContent = "Reading the G-code…";
  box.appendChild(note);
  const [{ T, OrbitControls, RoomEnvironment }, text] = await Promise.all([load(), readText(opts.url)]);
  await new Promise((r) => setTimeout(r, 20));
  const g = parseGcode(text);
  note.remove();
  if (!g.n || !g.layers) {
    box.textContent = "This file has no moves to show.";
    return;
  }
  const [bx, by] = opts.bed || [280, 280];

  // The page parts: the view, and the controls under it.
  const stage = document.createElement("div");
  stage.className = "gv-stage";
  const controls = document.createElement("div");
  controls.className = "gv-controls";
  box.append(stage);
  if (!opts.compact) box.append(controls);
  else box.classList.add("gv-compact");
  const renderer = new T.WebGLRenderer({ antialias: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.setClearColor(0x15161a);
  stage.appendChild(renderer.domElement);
  const scene = new T.Scene();
  const pmrem = new T.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.25;
  // The light: from above and from the front, low enough that each bead has a light top and a
  // dark groove under it, as a print under a lamp.
  scene.add(new T.HemisphereLight(0xffffff, 0x303540, 0.9));
  const key = new T.DirectionalLight(0xffffff, 1.6);
  key.position.set(-0.5, 0.9, 1.2);
  scene.add(key);
  const fill = new T.DirectionalLight(0xffffff, 0.45);
  fill.position.set(0.8, -0.3, -0.6);
  scene.add(fill);
  const camera = new T.PerspectiveCamera(35, 1, 0.5, 5000);
  const orbit = new OrbitControls(camera, renderer.domElement);
  orbit.enableDamping = true;
  orbit.minPolarAngle = 0;
  orbit.maxPolarAngle = Math.PI; // also from under the bed

  // The bed: see-through, so the first layer shows from under it.
  const bedMat = new T.MeshStandardMaterial({ color: "#2b2d31", transparent: true, opacity: 0.55, depthWrite: false, side: T.DoubleSide });
  const bed = new T.Mesh(new T.PlaneGeometry(bx, by), bedMat);
  bed.rotation.x = -Math.PI / 2;
  bed.position.y = -0.01;
  scene.add(bed);
  const grid = new T.GridHelper(Math.max(bx, by), Math.round(Math.max(bx, by) / 10), 0x5b5f66, 0x34373d);
  grid.position.y = 0;
  scene.add(grid);

  // Each extrusion is a bead: one geometry for each height of layer (with the height and the
  // width in it, so the light falls true), and an instance for each move, with its length and
  // its direction. Front of the printer (Y 0) toward the viewer.
  const n = g.n;
  const groups = new Map(); // the height in 1/100 mm -> the moves of that height
  for (let i = 0; i < n; i++) {
    const key = Math.round(g.seg[i * 7 + 5] * 100);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(i);
  }
  const mat = new T.MeshStandardMaterial({ roughness: 0.55, metalness: 0 });
  const colors = new Float32Array(n * 3);
  const meshes = [];
  for (const [key, list] of groups) {
    const idx = Uint32Array.from(list);
    const geo = beadGeometry(T, key / 100, g.seg[list[0] * 7 + 6]);
    const mesh = new T.InstancedMesh(geo, mat, idx.length);
    const mats = new Float32Array(idx.length * 16);
    for (let j = 0; j < idx.length; j++) {
      const o = idx[j] * 7;
      const x0 = g.seg[o], y0 = g.seg[o + 1], x1 = g.seg[o + 2], y1 = g.seg[o + 3], z = g.seg[o + 4], h = g.seg[o + 5];
      const dx = x1 - x0, dy = y1 - y0, l = Math.hypot(dx, dy);
      const a = Math.atan2(dy, dx); // the scene direction is (dx, 0, -dy): Z of the scene is -Y of the printer
      const c = Math.cos(a), sn = Math.sin(a), m = j * 16;
      // Scale X only (the length), turn about Y, move to the middle of the move.
      mats[m] = c * l; mats[m + 1] = 0; mats[m + 2] = -sn * l; mats[m + 3] = 0;
      mats[m + 4] = 0; mats[m + 5] = 1; mats[m + 6] = 0; mats[m + 7] = 0;
      mats[m + 8] = sn; mats[m + 9] = 0; mats[m + 10] = c; mats[m + 11] = 0;
      mats[m + 12] = (x0 + x1) / 2 - bx / 2; mats[m + 13] = z - h / 2; mats[m + 14] = by / 2 - (y0 + y1) / 2; mats[m + 15] = 1;
    }
    mesh.instanceMatrix.array.set(mats);
    mesh.instanceMatrix.needsUpdate = true;
    mesh.instanceColor = new T.InstancedBufferAttribute(new Float32Array(idx.length * 3), 3);
    mesh.frustumCulled = false;
    scene.add(mesh);
    meshes.push({ mesh, idx, mats, geo });
  }

  // The travels: thin lines.
  const travGeo = new T.BufferGeometry();
  const tpos = new Float32Array(g.tn * 6);
  for (let i = 0; i < g.tn; i++) {
    const o = i * 6;
    tpos[o] = g.trav[o] - bx / 2; tpos[o + 1] = g.trav[o + 2]; tpos[o + 2] = by / 2 - g.trav[o + 1];
    tpos[o + 3] = g.trav[o + 3] - bx / 2; tpos[o + 4] = g.trav[o + 5]; tpos[o + 5] = by / 2 - g.trav[o + 4];
  }
  travGeo.setAttribute("position", new T.BufferAttribute(tpos, 3));
  // The travels in the color of the moves of Cura (layerview_move_combing).
  const travels = new T.LineSegments(travGeo, new T.LineBasicMaterial({ color: 0x3050ff, transparent: true, opacity: 0.7 }));
  travels.visible = false;
  travels.frustumCulled = false;
  scene.add(travels);

  // The camera: from the front and above, at the plate.
  let minx = Infinity, maxx = -Infinity, miny = Infinity, maxy = -Infinity, maxz = 0;
  for (let i = 0; i < n; i++) {
    const o = i * 7;
    minx = Math.min(minx, g.seg[o], g.seg[o + 2]); maxx = Math.max(maxx, g.seg[o], g.seg[o + 2]);
    miny = Math.min(miny, g.seg[o + 1], g.seg[o + 3]); maxy = Math.max(maxy, g.seg[o + 1], g.seg[o + 3]);
    maxz = Math.max(maxz, g.seg[o + 4]);
  }
  const mid = new T.Vector3((minx + maxx) / 2 - bx / 2, maxz / 2, by / 2 - (miny + maxy) / 2);
  const size = Math.max(30, maxx - minx, maxy - miny, maxz);
  const far = opts.compact ? 0.8 : 1; // the view of a print page is closer
  camera.position.set(mid.x + size * 0.6 * far, mid.y + size * 0.9 * far, mid.z + size * 1.5 * far);
  orbit.target.copy(mid);
  orbit.maxDistance = Math.max(bx, by) * 4;

  let redraw = true;
  // The controls.
  const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt) e.textContent = txt; return e; };
  const info = el("div", "gv-info");
  const layerRow = el("label", "gv-row", "Layer ");
  const layer = el("input");
  layer.type = "range"; layer.min = 1; layer.max = g.layers; layer.value = g.layers;
  layerRow.appendChild(layer);
  const moveRow = el("label", "gv-row", "Moves ");
  const move = el("input");
  move.type = "range"; move.min = 0; move.max = 1000; move.value = 1000;
  moveRow.appendChild(move);
  const keys = el("div", "gv-keys");
  const step = (d) => { layer.value = Math.max(1, Math.min(g.layers, +layer.value + d)); move.value = 1000; update(); };
  const down = el("button", "", "‹ Layer"); down.type = "button"; down.onclick = () => step(-1);
  const up = el("button", "", "Layer ›"); up.type = "button"; up.onclick = () => step(1);
  const only = el("label", "gv-check");
  const onlyBox = el("input"); onlyBox.type = "checkbox";
  only.append(onlyBox, " This layer only");
  const trav = el("label", "gv-check");
  const travBox = el("input"); travBox.type = "checkbox";
  trav.append(travBox, " Travel moves");
  const mode = el("select");
  for (const [v, t] of [["kind", "Color by line type"], ["speed", "Color by speed"]]) {
    const o = el("option", "", t); o.value = v; mode.appendChild(o);
  }
  keys.append(down, up, only, trav, mode);
  const legend = el("div", "gv-legend");
  const present = new Set();
  for (let i = 0; i < n; i++) present.add(g.kind[i]);
  const shown = KINDS.map(() => true);
  KINDS.forEach(([, name, color], k) => {
    if (!present.has(k)) return;
    const l = el("label", "gv-check");
    const b = el("input"); b.type = "checkbox"; b.checked = true;
    b.onchange = () => { shown[k] = b.checked; update(); };
    const sw = el("i", "gv-swatch"); sw.style.background = color;
    l.append(b, sw, " " + name);
    legend.appendChild(l);
  });
  controls.append(info, layerRow, moveRow, keys, legend);

  // The colors: by kind, or by speed from blue (slow) to orange (fast).
  let vmin = Infinity, vmax = 0;
  for (let i = 0; i < n; i++) { vmin = Math.min(vmin, g.speed[i]); vmax = Math.max(vmax, g.speed[i]); }
  const kindColor = KINDS.map(([, , c]) => new T.Color(c));
  const tmp = new T.Color();
  const ghost = new T.Color(GHOST);
  let printed = opts.printed == null ? null : opts.printed;
  // By speed, as the feed rate view of Cura: blue for the slowest moves, through green and
  // yellow, to red for the fastest.
  const paint = () => {
    // The moves of the layers that did not print yet are gray.
    const cut = printed == null ? n : g.layerSeg[Math.max(0, Math.min(g.layers, printed))];
    for (let i = 0; i < n; i++) {
      let c = kindColor[g.kind[i]];
      if (mode.value === "speed") c = tmp.setHSL((2 / 3) * (1 - (vmax > vmin ? (g.speed[i] - vmin) / (vmax - vmin) : 0)), 1, 0.5);
      if (i >= cut) c = ghost;
      colors[i * 3] = c.r; colors[i * 3 + 1] = c.g; colors[i * 3 + 2] = c.b;
    }
  };
  paint();

  // update shows the moves up to the move slider of the layer slider, with the kinds that are on.
  // The moves of each mesh are in the order of the file.
  const update = () => {
    const L = +layer.value; // 1 is the first layer
    const start = onlyBox.checked ? g.layerSeg[L - 1] : 0;
    const ls = g.layerSeg[L - 1], le = g.layerSeg[L];
    const end = ls + Math.round((le - ls) * (+move.value / 1000));
    const all = shown.every((v, k) => v || !present.has(k));
    for (const { mesh, idx, mats } of meshes) {
      const im = mesh.instanceMatrix.array, ic = mesh.instanceColor.array;
      let count = 0;
      for (let j = 0; j < idx.length; j++) {
        const i = idx[j];
        if (i >= end) break;
        if (i < start || (!all && !shown[g.kind[i]])) continue;
        im.set(mats.subarray(j * 16, j * 16 + 16), count * 16);
        ic[count * 3] = colors[i * 3]; ic[count * 3 + 1] = colors[i * 3 + 1]; ic[count * 3 + 2] = colors[i * 3 + 2];
        count++;
      }
      mesh.count = count;
      mesh.instanceMatrix.needsUpdate = true;
      mesh.instanceColor.needsUpdate = true;
    }
    redraw = true;
    // The travels of the same moves.
    travels.visible = travBox.checked;
    const ts = onlyBox.checked ? g.layerTrav[L - 1] : 0;
    const tl = g.layerTrav[L - 1], te0 = g.layerTrav[L];
    const te = tl + Math.round((te0 - tl) * (+move.value / 1000));
    travGeo.setDrawRange(ts * 2, Math.max(0, te - ts) * 2);
    // The facts of the layer.
    const z = g.seg[Math.max(0, Math.min(n - 1, ls)) * 7 + 4];
    const t = g.layerTime[L - 1];
    const clock = (s) => { s = Math.round(s); const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60); return h ? `${h} h ${m} min` : `${m} min ${s % 60} s`; };
    info.textContent = `Layer ${L} of ${g.layers} · Z ${z.toFixed(2)} mm` + (t ? ` · ${clock(t)} into the print` : "") +
      ` · ${le - ls} moves in this layer`;
  };
  layer.oninput = () => { move.value = 1000; update(); };
  move.oninput = update;
  onlyBox.onchange = update;
  travBox.onchange = update;
  mode.onchange = () => { paint(); update(); };
  update();

  const fit = () => {
    const w = stage.clientWidth, h = stage.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
    redraw = true;
  };
  const resize = new ResizeObserver(fit);
  resize.observe(stage);
  fit();
  // The view draws only after a change: a large plate takes a moment for each picture, and a
  // phone keeps its battery.
  orbit.addEventListener("change", () => { redraw = true; });
  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    if (orbit.update() || redraw) {
      redraw = false;
      renderer.render(scene, camera);
    }
  };
  tick();
  // The arrow keys move the layer while the view has the focus.
  renderer.domElement.tabIndex = 0;
  renderer.domElement.addEventListener("keydown", (ev) => {
    if (ev.key === "ArrowUp" || ev.key === "ArrowRight") { step(1); ev.preventDefault(); }
    if (ev.key === "ArrowDown" || ev.key === "ArrowLeft") { step(-1); ev.preventDefault(); }
  });
  const handle = {
    // setPrinted shows the layers up to k in color, and the later layers in gray.
    setPrinted(k) {
      if (k === printed) return;
      printed = k;
      paint();
      update();
    },
  };
  view = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      orbit.dispose();
      renderer.dispose();
      for (const m of meshes) { m.geo.dispose(); m.mesh.dispose(); }
      mat.dispose();
      travGeo.dispose();
      box.textContent = "";
      box.classList.remove("gv-compact");
    },
  };
  return handle;
}

window.pwfView3d = { show, showPart, showModels, showGcode, hide, modelFile };
