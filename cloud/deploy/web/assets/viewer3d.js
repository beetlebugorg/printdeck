// The 3D view of a printer page: the model of the printer, which the viewer turns with a finger
// or the mouse, and the picture of a print over the bed. The page (cmd/pwf-web) calls
// pwfView3d.show. three.js loads only when a page shows a view.

const three = "./three-r170/";
let lib; // the three.js modules, after the first show
let view; // the view now: { box, renderer, stop }
// The other views of the page: showGcode with opts.many keeps a view of its own in each box, for
// example one for each plate of a build. hide() stops them too.
const many = new Map(); // box -> { box, stop }

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
  for (const v of many.values()) v.stop();
  many.clear();
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


// The real bed of a printer, under the views of the models and of the G-code, so a person sees
// where a part goes on the bed. printer is the name or the key of the printer, for example "taz"
// or "LulzBot TAZ Workhorse". The bed is in the scene of the views: X = x - bx/2, Y up,
// Z = by/2 - y, with x and y the coordinates of the printer.
//
// The TAZ Workhorse: the meshes of the bed from the CAD of LulzBot (desktop/meshes, cad/export.py;
// GPLv3 or later and/or CC BY-SA 4.0). A vertex is 10 bytes: the position as 3 int16 in steps of
// 1/50 mm, the normal as 3 int8, and a byte of padding, in the scene of the CAD (Y up, Z to the
// front). The place (x, y) of the printer is at X x + 4.03 and Z 128.15 - y of the CAD
// (desktop/Sim3D.qml).
//
// The Peopoly Phenom: the vat and the area of its screen, drawn to their sizes (no CAD): the area
// is 276.5 × 155.5 mm.
function bedKind(printer) {
  const m = (printer || "").toLowerCase();
  if (m.includes("taz") || m.includes("workhorse")) return "taz";
  if (m.includes("phenom")) return "phenom";
  return "";
}

const bedMeshes = new Map();
function tazMesh(T, kind) {
  if (!bedMeshes.has(kind)) {
    bedMeshes.set(kind, (async () => {
      const r = await fetch("/assets/models/taz-bed/" + kind + ".bin");
      if (!r.ok) throw new Error("the bed did not load");
      const dv = new DataView(await r.arrayBuffer());
      const n = dv.byteLength / 10;
      const pos = new Float32Array(n * 3), nor = new Float32Array(n * 3);
      for (let i = 0; i < n; i++) {
        for (let k = 0; k < 3; k++) {
          pos[i * 3 + k] = dv.getInt16(i * 10 + k * 2, true) / 50;
          nor[i * 3 + k] = dv.getInt8(i * 10 + 6 + k) / 127;
        }
      }
      const g = new T.BufferGeometry();
      g.setAttribute("position", new T.BufferAttribute(pos, 3));
      g.setAttribute("normal", new T.BufferAttribute(nor, 3));
      return g;
    })());
    bedMeshes.get(kind).catch(() => bedMeshes.delete(kind));
  }
  return bedMeshes.get(kind);
}

// printerBed returns the group of the real bed of the printer for a bed of bx × by in the scene,
// or null for a printer with no real bed.
async function printerBed(T, printer, bx, by) {
  const kind = bedKind(printer);
  if (kind === "taz") {
    const parts = [["surface", "#b8923f", 0.5, 0], ["metal", "#9aa0a8", 0.35, 0], ["printed", "#2a2b2e", 0.7, 0], ["washer", "#c9cdd2", 0.3, 0.86 - 1.78]];
    const g = new T.Group();
    const geos = await Promise.all(parts.map(([k]) => tazMesh(T, k)));
    parts.forEach(([, color, rough, dy], i) => {
      const m = new T.Mesh(geos[i], new T.MeshStandardMaterial({ color, roughness: rough, metalness: rough < 0.4 ? 0.6 : 0 }));
      m.position.y = dy;
      g.add(m);
    });
    g.position.set(-4.03 - bx / 2, -0.03, by / 2 - 128.15);
    return g;
  }
  if (kind === "phenom") {
    // The vat: a frame with walls around the area of the screen, and the screen area at Y 0.
    const g = new T.Group();
    const aw = 276.5, ad = 155.5, wall = 12, h = 45;
    const frame = new T.MeshStandardMaterial({ color: "#3a3d42", roughness: 0.6, metalness: 0.3 });
    const glass = new T.MeshStandardMaterial({ color: "#d8a520", roughness: 0.2, transparent: true, opacity: 0.18, depthWrite: false });
    const box = (w, hh, d, x, y, z, mat) => {
      const m = new T.Mesh(new T.BoxGeometry(w, hh, d), mat);
      m.position.set(x, y, z);
      g.add(m);
    };
    box(aw + 2 * wall, h, wall, 0, h / 2 - 2, -(ad / 2 + wall / 2), glass);
    box(aw + 2 * wall, h, wall, 0, h / 2 - 2, ad / 2 + wall / 2, glass);
    box(wall, h, ad, -(aw / 2 + wall / 2), h / 2 - 2, 0, glass);
    box(wall, h, ad, aw / 2 + wall / 2, h / 2 - 2, 0, glass);
    box(aw + 2 * wall + 30, 4, ad + 2 * wall + 30, 0, -4, 0, frame);
    const screen = new T.Mesh(new T.PlaneGeometry(aw, ad), new T.MeshStandardMaterial({ color: "#14161a", roughness: 0.3 }));
    screen.rotation.x = -Math.PI / 2;
    screen.position.y = -1.9;
    g.add(screen);
    const edge = new T.LineSegments(new T.EdgesGeometry(new T.PlaneGeometry(aw, ad)), new T.LineBasicMaterial({ color: 0xff7a1a }));
    edge.rotation.x = -Math.PI / 2;
    edge.position.y = -1.8;
    g.add(edge);
    return g;
  }
  return null;
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
  // The bed: see-through, so the bottom of a part shows from under the bed.
  const bed = new T.Mesh(new T.PlaneGeometry(bx, by), new T.MeshStandardMaterial({ color: "#2b2d31", roughness: 0.8, transparent: true,
    opacity: 0.55, depthWrite: false, side: T.DoubleSide }));
  bed.rotation.x = -Math.PI / 2;
  bed.position.y = -0.01;
  scene.add(bed);
  const grid = new T.GridHelper(Math.max(bx, by), Math.round(Math.max(bx, by) / 10), 0x5b5f66, 0x3a3d42);
  grid.position.y = 0.05;
  scene.add(grid);
  // The real bed of the printer, in place of the plain bed, when the page names the printer.
  printerBed(T, opts.printer, bx, by).then((real) => {
    if (!real) return;
    real.traverse((o) => {
      if (!o.isMesh) return;
      o.material = o.material.clone();
      Object.assign(o.material, { transparent: true, opacity: Math.min(o.material.opacity, 0.5), depthWrite: false, side: T.DoubleSide });
    });
    scene.add(real);
    bed.visible = grid.visible = false;
  }).catch(() => {});
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
  let at = new Float64Array(cap); // the byte of the line of each move (the text is ASCII)
  let tn = 0, tcap = 1 << 14, trav = new Float32Array(tcap * 6);
  const layerSeg = [], layerTrav = [], layerZ = [], layerTime = [];
  let x = 0, y = 0, z = 0, e = 0, f = 0, rel = false, cur = KIND.OTHER, layerH = 0.2, lastZ = 0, maxE = 0;
  const grow = () => {
    cap *= 2;
    const s2 = new Float32Array(cap * 7); s2.set(seg); seg = s2;
    const k2 = new Uint8Array(cap); k2.set(kind); kind = k2;
    const v2 = new Float32Array(cap); v2.set(speed); speed = v2;
    const a2 = new Float64Array(cap); a2.set(at); at = a2;
  };
  let pos = 0;
  const len = text.length;
  let lineAt = 0;
  while (pos < len) {
    let end = text.indexOf("\n", pos);
    if (end < 0) end = len;
    const line = text.slice(pos, end);
    lineAt = pos;
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
        kind[n] = cur; speed[n] = f / 60; at[n] = lineAt; n++;
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
  return { n, seg, kind, speed, at, tn, trav, layerSeg, layerTrav, layerZ, layerTime, layers: layerSeg.length - 1 };
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
// the layers that printed, or null for all: the later layers are gray; many: keep the other
// views of the page (one view in each box). The viewer turns all around the plate, also under
// the bed. It returns a handle with setPrinted(n).
async function showGcode(box, opts) {
  if (opts.many) {
    many.get(box)?.stop();
    many.delete(box);
  } else {
    hide();
  }
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

  // The page parts: the view with the layer slider at its side, and the other controls on the
  // right (under the view on a narrow screen).
  const stage = document.createElement("div");
  stage.className = "gv-stage";
  const controls = document.createElement("div");
  controls.className = "gv-controls";
  const main = document.createElement("div");
  main.className = "gv-main";
  const side = document.createElement("div");
  side.className = "gv-side";
  if (!opts.compact) {
    main.append(stage, side);
    box.append(main, controls);
    box.classList.add("gv-split");
  } else {
    box.append(stage);
    box.classList.add("gv-compact");
  }
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
  // The real bed of the printer, when the page names the printer.
  printerBed(T, opts.printer, bx, by).then((real) => {
    if (!real) return;
    scene.add(real);
    bed.visible = grid.visible = false;
    redraw = true;
  }).catch(() => {});

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
    mesh.instanceMatrix.setUsage(T.DynamicDrawUsage);
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
  camera.position.set(mid.x + size * 0.6, mid.y + size * 0.9, mid.z + size * 1.5);
  // The view of a print page fits the whole part in the view, at each size of the view, until
  // the person turns it.
  const radius = 0.5 * Math.hypot(maxx - minx, maxy - miny, maxz);
  const dir = new T.Vector3(0.6, 0.9, 1.5).normalize();
  let turned = false;
  orbit.addEventListener("start", () => { turned = true; });
  const framePart = () => {
    if (turned) return;
    const vf = (camera.fov * Math.PI) / 180;
    const hf = 2 * Math.atan(Math.tan(vf / 2) * camera.aspect);
    const d = (radius / Math.sin(Math.min(vf, hf) / 2)) * (opts.compact ? 1.08 : 1.25);
    if (!Number.isFinite(d) || d <= 0) return; // the view has no size yet
    camera.position.copy(mid).addScaledVector(dir, d);
  };
  orbit.target.copy(mid);
  orbit.maxDistance = Math.max(bx, by) * 4;

  let redraw = true;
  // The controls.
  const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt) e.textContent = txt; return e; };
  const info = el("div", "gv-info");
  const layer = el("input");
  layer.type = "range"; layer.min = 1; layer.max = g.layers; layer.value = g.layers;
  layer.setAttribute("aria-label", "Layer");
  const layerNum = el("span", "gv-layer-n");
  side.append(layerNum, layer, el("span", "gv-layer-cap", "Layer"));
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
  controls.append(info, moveRow, keys, legend);

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
      // A full upload: the animation of a layer can leave a part range.
      mesh.instanceMatrix.clearUpdateRanges();
      mesh.instanceColor.clearUpdateRanges();
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
    layerNum.textContent = L;
  };
  layer.oninput = () => { move.value = 1000; update(); };
  move.oninput = update;
  onlyBox.onchange = update;
  travBox.onchange = update;
  mode.onchange = () => { paint(); update(); };
  update();

  // The view of the time lapse camera (setView): a camera from the matrix of the deck, in a stage
  // with the shape of the frames of the time lapse.
  let matched = null, matchedAspect = 0, useMatched = false;
  const viewKey = el("button", "gv-viewkey", "Turn freely");
  viewKey.type = "button";
  viewKey.hidden = true;
  viewKey.onclick = () => {
    useMatched = !useMatched;
    viewKey.textContent = useMatched ? "Turn freely" : "Camera view";
    orbit.enabled = !useMatched;
    fit();
  };
  if (opts.compact) box.appendChild(viewKey);
  const fit = () => {
    if (opts.compact) {
      // The stage fills the box, or has the shape of the frames in the camera view.
      let sw = box.clientWidth, sh = box.clientHeight;
      if (matched && useMatched && matchedAspect > 0) {
        if (sw / sh > matchedAspect) sw = Math.round(sh * matchedAspect);
        else sh = Math.round(sw / matchedAspect);
      }
      stage.style.width = sw + "px";
      stage.style.height = sh + "px";
    }
    const w = stage.clientWidth, h = stage.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
    framePart();
    redraw = true;
  };
  const resize = new ResizeObserver(fit);
  resize.observe(opts.compact ? box : stage);
  fit();
  // The view draws only after a change: a large plate takes a moment for each picture, and a
  // phone keeps its battery.
  orbit.addEventListener("change", () => { redraw = true; });
  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    if (orbit.update() || redraw) {
      redraw = false;
      renderer.render(scene, matched && useMatched ? matched : camera);
    }
  };
  tick();
  // The arrow keys move the layer while the view has the focus.
  renderer.domElement.tabIndex = 0;
  renderer.domElement.addEventListener("keydown", (ev) => {
    if (ev.key === "ArrowUp" || ev.key === "ArrowRight") { step(1); ev.preventDefault(); }
    if (ev.key === "ArrowDown" || ev.key === "ArrowLeft") { step(-1); ev.preventDefault(); }
  });
  // The page of a print (compact): the moves that printed are solid, in the order of the file,
  // and the layers that did not print yet are a faint ghost. Each mesh of moves has a twin with
  // the same moves in the reverse order: the solid mesh draws the first moves (its count), and
  // the ghost draws the last moves. A step of the print only changes the two counts.
  const lowerBound = (a, v) => { let lo = 0, hi = a.length; while (lo < hi) { const m = (lo + hi) >> 1; if (a[m] < v) lo = m + 1; else hi = m; } return lo; };
  // The nozzle: the tip, the hex of the nozzle and the heater block, in the size of a real hot end,
  // drawn over the part with an orange edge, so it shows also behind a wall.
  const nozzle = new T.Group();
  {
    const body = new T.MeshBasicMaterial({ color: 0x2a2d33, transparent: true, opacity: 0.85, depthTest: false });
    const edge = new T.LineBasicMaterial({ color: 0xff7a1a, depthTest: false });
    const part = (geo, y) => {
      const m = new T.Mesh(geo, body);
      m.position.y = y;
      m.renderOrder = 10;
      const l = new T.LineSegments(new T.EdgesGeometry(geo, 20), edge);
      l.position.y = y;
      l.renderOrder = 11;
      nozzle.add(m, l);
    };
    part(new T.ConeGeometry(2.6, 4, 6).rotateX(Math.PI), 2); // the tip, its point at y 0
    part(new T.CylinderGeometry(4.2, 4.2, 4, 6), 6); // the hex
    part(new T.BoxGeometry(18, 10, 14), 13); // the heater block
    const tip = new T.Mesh(new T.SphereGeometry(0.9, 12, 8), new T.MeshBasicMaterial({ color: 0xffb070, depthTest: false }));
    tip.renderOrder = 12;
    nozzle.add(tip);
  }
  nozzle.visible = false;
  scene.add(nozzle);
  const ghosts = [];
  if (opts.compact) {
    printed = null;
    paint();
    update();
    const ghostMat = new T.MeshBasicMaterial({ color: 0xd3d8e0, transparent: true, opacity: 0.2, depthWrite: false });
    for (const { mesh, idx, mats, geo } of meshes) {
      const N = idx.length;
      const g2 = new T.InstancedMesh(geo, ghostMat, N);
      const rev = g2.instanceMatrix.array;
      for (let j = 0; j < N; j++) rev.set(mats.subarray((N - 1 - j) * 16, (N - j) * 16), j * 16);
      g2.instanceMatrix.needsUpdate = true;
      g2.frustumCulled = false;
      g2.count = 0;
      scene.add(g2);
      ghosts.push(g2);
    }
  }
  // showTo draws the moves before the move cut as solid, and the moves from the move ghostFrom as
  // the ghost; the dot of the nozzle is at the end of the last solid move of a layer that prints.
  const showTo = (cut, ghostFrom, dot) => {
    meshes.forEach(({ mesh, idx }, k) => {
      mesh.count = lowerBound(idx, cut);
      ghosts[k].count = idx.length - lowerBound(idx, ghostFrom);
    });
    if (dot && cut > 0) {
      const o = (cut - 1) * 7;
      nozzle.position.set(g.seg[o + 2] - bx / 2, g.seg[o + 4], by / 2 - g.seg[o + 3]);
      nozzle.visible = true;
    } else {
      nozzle.visible = false;
    }
    redraw = true;
  };
  let lastCut = -1;
  // setView sets the view of the time lapse camera: q is the matrix from the G-code coordinates to
  // the frames (12 numbers, row by row; u = a/c, v = b/c, v down), and aspect the shape of the
  // frames. The scene has X = x - bx/2, Y = z, Z = by/2 - y.
  const setView = (q, aspect) => {
    if (!q || q.length !== 12 || !(aspect > 0)) return;
    const toGcode = new T.Matrix4().set(1, 0, 0, bx / 2, 0, 0, -1, by / 2, 0, 1, 0, 0, 0, 0, 0, 1);
    const Q = new T.Matrix4().set(q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7], q[8], q[9], q[10], q[11], 0, 0, 0, 1).multiply(toGcode);
    const e = Q.elements; // by column
    const row = (i) => [e[i], e[4 + i], e[8 + i], e[12 + i]];
    let r0 = row(0), r1 = row(1), r2 = row(2);
    // The depth of the part: c at the corners of its box. c is above 0 in front of the camera.
    const cs = [];
    for (const x of [minx, maxx]) for (const y of [miny, maxy]) for (const z of [0, maxz]) {
      const X = x - bx / 2, Y = z, Z = by / 2 - y;
      cs.push(r2[0] * X + r2[1] * Y + r2[2] * Z + r2[3]);
    }
    if (cs.reduce((a, b) => a + b, 0) < 0) {
      r0 = r0.map((v) => -v); r1 = r1.map((v) => -v); r2 = r2.map((v) => -v);
      for (let i = 0; i < cs.length; i++) cs[i] = -cs[i];
    }
    const cmin = Math.min(...cs), cmax = Math.max(...cs);
    if (!(cmin > 0)) return; // a part behind the camera: no view
    // A matrix with no direction of Y (a calibration of the nozzle on a printer with a bed that
    // moves in Y) flattens the part: its edge in Y is a few pixels. No view then.
    const uv = (x, y, z) => {
      const X = x - bx / 2, Y = z, Z = by / 2 - y;
      const c = r2[0] * X + r2[1] * Y + r2[2] * Z + r2[3];
      return [(r0[0] * X + r0[1] * Y + r0[2] * Z + r0[3]) / c * aspect, (r1[0] * X + r1[1] * Y + r1[2] * Z + r1[3]) / c];
    };
    const len = (a, b) => Math.hypot(a[0] - b[0], a[1] - b[1]);
    const o = uv(minx, miny, 0);
    const ex = len(o, uv(maxx, miny, 0)), ey = len(o, uv(minx, maxy, 0)), ez = len(o, uv(minx, miny, Math.max(maxz, 1)));
    if (ey < 0.1 * Math.max(ex, ez)) return;
    const n = cmin * 0.3, f = cmax * 3;
    const A = (f + n) / (f - n), B = (-2 * f * n) / (f - n);
    const P = new T.Matrix4().set(
      2 * r0[0] - r2[0], 2 * r0[1] - r2[1], 2 * r0[2] - r2[2], 2 * r0[3] - r2[3],
      r2[0] - 2 * r1[0], r2[1] - 2 * r1[1], r2[2] - 2 * r1[2], r2[3] - 2 * r1[3],
      A * r2[0], A * r2[1], A * r2[2], A * r2[3] + B,
      r2[0], r2[1], r2[2], r2[3]);
    const first = !matched;
    matched = new T.Camera();
    matched.projectionMatrix.copy(P);
    matched.projectionMatrixInverse.copy(P).invert();
    matchedAspect = aspect;
    viewKey.hidden = false;
    if (first && !turned) {
      useMatched = true;
      orbit.enabled = false;
      viewKey.textContent = "Turn freely";
    }
    fit();
  };
  if (opts.view) setView(opts.view, opts.aspect);
  const handle = {
    setView,
    // setPrinted shows the layers up to k as solid, and the later layers as the ghost.
    setPrinted(k) {
      if (!opts.compact) { printed = k; paint(); update(); return; }
      const cut = g.layerSeg[Math.max(0, Math.min(g.layers, k))];
      lastCut = -1;
      showTo(cut, cut, false);
    },
    // setFilePos shows the print at the byte pos of the file, and dt seconds later at the speeds
    // of the G-code (dt can be below 0): the moves before it solid, with the nozzle, the rest of
    // its layer hidden, and the later layers as the ghost.
    setFilePos(pos, dt) {
      if (!opts.compact) return;
      let cut = lowerBound(g.at.subarray(0, g.n), pos);
      dt = Math.max(-60, Math.min(60, dt || 0));
      if (dt > 0) { while (cut < g.n && dt > 0) { dt -= moveTime(cut); cut++; } }
      else { while (cut > 0 && dt < 0) { cut--; dt += moveTime(cut); } }
      if (cut === lastCut) return;
      lastCut = cut;
      // The layer of the move: the last layer that starts at or before it.
      let L = 1;
      while (L < g.layers && g.layerSeg[L] <= Math.max(0, cut - 1)) L++;
      showTo(cut, g.layerSeg[L], true);
    },
    // setProgress shows the layers before L as solid, the moves of layer L for the share f (0 to
    // 1) of the time of the layer, with the nozzle, and the later layers as the ghost.
    setProgress(L, f) {
      if (!opts.compact) return;
      L = Math.max(1, Math.min(g.layers, L | 0));
      const ls = g.layerSeg[L - 1], le = g.layerSeg[L];
      // Each move takes its length at the speed of the G-code.
      let total = 0;
      for (let i = ls; i < le; i++) total += moveTime(i);
      let t = Math.max(0, Math.min(1, f)) * total, cut = ls;
      while (cut < le && t > 0) { t -= moveTime(cut); cut++; }
      if (cut === lastCut) return;
      lastCut = cut;
      showTo(cut, le, true);
    },
  };
  function moveTime(i) {
    const o = i * 7;
    return Math.hypot(g.seg[o + 2] - g.seg[o], g.seg[o + 3] - g.seg[o + 1]) / Math.max(1, g.speed[i]);
  }
  const done = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      orbit.dispose();
      renderer.dispose();
      for (const m of meshes) { m.geo.dispose(); m.mesh.dispose(); }
      for (const m of ghosts) m.dispose();
      mat.dispose();
      travGeo.dispose();
      box.textContent = "";
      box.classList.remove("gv-compact", "gv-split");
    },
  };
  if (opts.many) many.set(box, done);
  else view = done;
  return handle;
}

// The changes of a model between two plans. The Action saves each model in the middle of the bed
// at Z 0, so the old model is placed against the new one at the middle, or at the low or the high
// side of each axis: the place with the least volume that differs. A grid of small cubes (voxels)
// gives the added and the removed volume.

// stlTris reads the triangles of an STL file: 9 numbers for each triangle. The files of one page
// load once (the key is the path, with no signature).
const stlCache = new Map();
function stlTris(url) {
  const key = new URL(url, location.href).pathname;
  if (!stlCache.has(key)) {
    stlCache.set(key, (async () => {
      const resp = await fetch(url, { credentials: "same-origin" });
      if (!resp.ok) throw new Error("the model did not load: " + resp.status);
      const buf = await resp.arrayBuffer();
      const dv = new DataView(buf);
      const n = buf.byteLength >= 84 ? dv.getUint32(80, true) : 0;
      if (buf.byteLength === 84 + 50 * n) {
        const t = new Float32Array(n * 9);
        for (let i = 0; i < n; i++) for (let k = 0; k < 9; k++) t[i * 9 + k] = dv.getFloat32(84 + 50 * i + 12 + k * 4, true);
        return t;
      }
      const text = new TextDecoder().decode(buf);
      return new Float32Array([...text.matchAll(/vertex\s+(\S+)\s+(\S+)\s+(\S+)/g)].flatMap((m) => [+m[1], +m[2], +m[3]]));
    })());
    stlCache.get(key).catch(() => stlCache.delete(key));
  }
  return stlCache.get(key);
}

// boxOf is the box around the triangles: [x0, y0, z0, x1, y1, z1].
function boxOf(t) {
  const b = [Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity];
  for (let i = 0; i < t.length; i += 3) {
    for (let k = 0; k < 3; k++) {
      if (t[i + k] < b[k]) b[k] = t[i + k];
      if (t[i + k] > b[k + 3]) b[k + 3] = t[i + k];
    }
  }
  return b;
}

// volumeOf is the volume of a closed mesh in mm³.
function volumeOf(t) {
  let v = 0;
  for (let i = 0; i < t.length; i += 9) {
    const ax = t[i], ay = t[i + 1], az = t[i + 2], bx = t[i + 3], by = t[i + 4], bz = t[i + 5], cx = t[i + 6], cy = t[i + 7], cz = t[i + 8];
    v += ax * (by * cz - bz * cy) - ay * (bx * cz - bz * cx) + az * (bx * cy - by * cx);
  }
  return Math.abs(v / 6);
}

// voxels fills a grid (origin o, cell size c, nx × ny × nz) with the inside of the mesh t, moved
// by d: for each column of the grid, the crossings of the surface along Z in pairs.
function voxels(t, d, o, c, nx, ny, nz) {
  const cols = Array.from({ length: nx * ny }, () => null);
  for (let i = 0; i < t.length; i += 9) {
    const ax = t[i] + d[0], ay = t[i + 1] + d[1], az = t[i + 2] + d[2];
    const bx = t[i + 3] + d[0], by = t[i + 4] + d[1], bz = t[i + 5] + d[2];
    const cx = t[i + 6] + d[0], cy = t[i + 7] + d[1], cz = t[i + 8] + d[2];
    const den = (by - cy) * (ax - cx) + (cx - bx) * (ay - cy);
    if (Math.abs(den) < 1e-12) continue; // a triangle on its edge from above
    const i0 = Math.max(0, Math.ceil((Math.min(ax, bx, cx) - o[0]) / c - 0.5));
    const i1 = Math.min(nx - 1, Math.floor((Math.max(ax, bx, cx) - o[0]) / c - 0.5));
    const j0 = Math.max(0, Math.ceil((Math.min(ay, by, cy) - o[1]) / c - 0.5));
    const j1 = Math.min(ny - 1, Math.floor((Math.max(ay, by, cy) - o[1]) / c - 0.5));
    for (let x = i0; x <= i1; x++) {
      const px = o[0] + (x + 0.5) * c;
      for (let y = j0; y <= j1; y++) {
        const py = o[1] + (y + 0.5) * c;
        const w0 = ((by - cy) * (px - cx) + (cx - bx) * (py - cy)) / den;
        const w1 = ((cy - ay) * (px - cx) + (ax - cx) * (py - cy)) / den;
        const w2 = 1 - w0 - w1;
        if (w0 < 0 || w1 < 0 || w2 < 0) continue;
        const z = w0 * az + w1 * bz + w2 * cz;
        const k = x * ny + y;
        (cols[k] || (cols[k] = [])).push(z);
      }
    }
  }
  const g = new Uint8Array(nx * ny * nz);
  for (let k = 0; k < cols.length; k++) {
    const zs = cols[k];
    if (!zs) continue;
    zs.sort((p, q) => p - q);
    for (let m = 0; m + 1 < zs.length; m += 2) {
      const z0 = Math.max(0, Math.ceil((zs[m] - o[2]) / c - 0.5));
      const z1 = Math.min(nz - 1, Math.floor((zs[m + 1] - o[2]) / c - 0.5));
      for (let z = z0; z <= z1; z++) g[k * nz + z] = 1;
    }
  }
  return g;
}

// gridFor is a grid around both boxes with about n cells on the longest side.
function gridFor(a, b, n) {
  const lo = [0, 1, 2].map((k) => Math.min(a[k], b[k]));
  const hi = [0, 1, 2].map((k) => Math.max(a[k + 3], b[k + 3]));
  const c = Math.max(0.2, Math.max(hi[0] - lo[0], hi[1] - lo[1], hi[2] - lo[2]) / n);
  const o = lo.map((v) => v - c);
  const [nx, ny, nz] = [0, 1, 2].map((k) => Math.ceil((hi[k] - lo[k]) / c) + 2);
  return { o, c, nx, ny, nz };
}

// modelDiff compares the old and the new model: the sizes, the volumes, and the added and the
// removed volume in mm³. It returns also the grid and the move of the old model, for the view.
async function modelDiff(oldUrl, newUrl) {
  const [a, b] = await Promise.all([stlTris(oldUrl), stlTris(newUrl)]);
  const ba = boxOf(a), bb = boxOf(b);
  const same = a.length === b.length && a.every((v, i) => v === b[i]);
  // The places to try: the middle, the low side and the high side of X and Y, and the bottom
  // and the top of Z.
  const opts = (k) => [0, bb[k] - ba[k], bb[k + 3] - ba[k + 3]];
  let best = [0, 0, 0];
  if (!same) {
    let bestN = Infinity;
    for (const dx of opts(0)) for (const dy of opts(1)) for (const dz of [0, bb[5] - ba[5]]) {
      const d = [dx, dy, dz];
      const moved = [ba[0] + dx, ba[1] + dy, ba[2] + dz, ba[3] + dx, ba[4] + dy, ba[5] + dz];
      const gr = gridFor(moved, bb, 48);
      const ga = voxels(a, d, gr.o, gr.c, gr.nx, gr.ny, gr.nz), gb = voxels(b, [0, 0, 0], gr.o, gr.c, gr.nx, gr.ny, gr.nz);
      let n = 0;
      for (let i = 0; i < ga.length; i++) n += ga[i] !== gb[i];
      if (n < bestN) { bestN = n; best = d; }
    }
  }
  const moved = [ba[0] + best[0], ba[1] + best[1], ba[2] + best[2], ba[3] + best[0], ba[4] + best[1], ba[5] + best[2]];
  const gr = gridFor(moved, bb, 140);
  let added = 0, removed = 0, ga = null, gb = null;
  if (!same) {
    ga = voxels(a, best, gr.o, gr.c, gr.nx, gr.ny, gr.nz);
    gb = voxels(b, [0, 0, 0], gr.o, gr.c, gr.nx, gr.ny, gr.nz);
    for (let i = 0; i < ga.length; i++) { if (gb[i] && !ga[i]) added++; else if (ga[i] && !gb[i]) removed++; }
  }
  const cell = gr.c ** 3;
  return {
    same,
    oldSize: [ba[3] - ba[0], ba[4] - ba[1], ba[5] - ba[2]],
    newSize: [bb[3] - bb[0], bb[4] - bb[1], bb[5] - bb[2]],
    oldVolume: volumeOf(a), newVolume: volumeOf(b),
    added: added * cell, removed: removed * cell,
    grid: gr, ga, gb, move: best, newTris: b,
  };
}

// diffText is a short text of the changes of a model: "2.0 mm taller · +3.1 cm³ (+4%)".
function diffText(d) {
  if (d.same) return "The same shape";
  const out = [];
  const words = [["wider", "narrower"], ["deeper", "shallower"], ["taller", "lower"]];
  for (let k = 0; k < 3; k++) {
    const dd = d.newSize[k] - d.oldSize[k];
    if (Math.abs(dd) >= 0.05) out.push(`${Math.abs(dd).toFixed(1)} mm ${words[k][dd > 0 ? 0 : 1]}`);
  }
  const dv = (d.newVolume - d.oldVolume) / 1000;
  const pct = d.oldVolume > 0 ? (100 * (d.newVolume - d.oldVolume)) / d.oldVolume : 0;
  if (Math.abs(dv) >= 0.01) out.push(`${dv > 0 ? "+" : "−"}${Math.abs(dv).toFixed(2)} cm³ (${pct > 0 ? "+" : "−"}${Math.abs(pct).toFixed(1)}%)`);
  if (!out.length) {
    const moved = (d.added + d.removed) / 1000;
    return moved >= 0.01 ? `Same size and volume, ${moved.toFixed(2)} cm³ moved` : "Small changes of the surface";
  }
  return out.join(" · ");
}

// describeDiff returns the text of the changes of the model newUrl since oldUrl.
async function describeDiff(oldUrl, newUrl) {
  return diffText(await modelDiff(oldUrl, newUrl));
}

// showDiff draws the changes of a model in box: the new model in faint gray, the added volume in
// green and the removed volume in red. It returns the text of the changes.
async function showDiff(box, oldUrl, newUrl) {
  hide();
  box.textContent = "";
  const note = document.createElement("p");
  note.className = "part-wait";
  note.textContent = "Comparing the models…";
  box.appendChild(note);
  const [{ T, OrbitControls, RoomEnvironment }, d] = await Promise.all([load(), modelDiff(oldUrl, newUrl)]);
  note.remove();
  const renderer = new T.WebGLRenderer({ antialias: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.setClearColor(0x15161a);
  box.appendChild(renderer.domElement);
  const scene = new T.Scene();
  const pmrem = new T.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.add(new T.HemisphereLight(0xffffff, 0x303540, 0.9));
  const key = new T.DirectionalLight(0xffffff, 1.4);
  key.position.set(-0.5, 0.9, 1.2);
  scene.add(key);
  // Printer X, Y, Z to the scene X, Y (up), Z (toward the viewer).
  const geo = new T.BufferGeometry();
  const pos = new Float32Array(d.newTris.length);
  for (let i = 0; i < pos.length; i += 3) { pos[i] = d.newTris[i]; pos[i + 1] = d.newTris[i + 2]; pos[i + 2] = -d.newTris[i + 1]; }
  geo.setAttribute("position", new T.BufferAttribute(pos, 3));
  geo.computeVertexNormals();
  const ghost = new T.Mesh(geo, new T.MeshStandardMaterial({ color: 0xb8bcc4, transparent: true, opacity: d.same ? 0.9 : 0.22, depthWrite: false, roughness: 0.6 }));
  scene.add(ghost);
  const meshes = [];
  if (!d.same) {
    const { o, c, nx, ny, nz } = d.grid;
    const cube = new T.BoxGeometry(c, c, c);
    for (const [want, color] of [[1, 0x2fbf5b], [-1, 0xe0413a]]) {
      let n = 0;
      for (let i = 0; i < d.ga.length; i++) if ((want > 0 ? d.gb[i] && !d.ga[i] : d.ga[i] && !d.gb[i])) n++;
      if (!n) continue;
      const mesh = new T.InstancedMesh(cube, new T.MeshStandardMaterial({ color, roughness: 0.5 }), n);
      const m = new T.Matrix4();
      let j = 0;
      for (let x = 0; x < nx; x++) for (let y = 0; y < ny; y++) for (let z = 0; z < nz; z++) {
        const i = (x * ny + y) * nz + z;
        if (!(want > 0 ? d.gb[i] && !d.ga[i] : d.ga[i] && !d.gb[i])) continue;
        m.makeTranslation(o[0] + (x + 0.5) * c, o[2] + (z + 0.5) * c, -(o[1] + (y + 0.5) * c));
        mesh.setMatrixAt(j++, m);
      }
      scene.add(mesh);
      meshes.push(mesh);
    }
  }
  const camera = new T.PerspectiveCamera(35, 1, 0.5, 5000);
  const size = Math.max(20, ...d.newSize);
  const mid = new T.Vector3(0, d.newSize[2] / 2, 0);
  camera.position.set(size * 0.7, size * 1.0, size * 1.6);
  const orbit = new OrbitControls(camera, renderer.domElement);
  orbit.target.copy(mid);
  orbit.enableDamping = true;
  let redraw = true;
  orbit.addEventListener("change", () => { redraw = true; });
  const fit = () => {
    const w = box.clientWidth, h = box.clientHeight;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
    redraw = true;
  };
  const resize = new ResizeObserver(fit);
  resize.observe(box);
  fit();
  let frame = 0;
  const tick = () => {
    frame = requestAnimationFrame(tick);
    if (orbit.update() || redraw) { redraw = false; renderer.render(scene, camera); }
  };
  tick();
  const done = {
    box,
    stop() {
      cancelAnimationFrame(frame);
      resize.disconnect();
      orbit.dispose();
      renderer.dispose();
      geo.dispose();
      for (const m of meshes) m.dispose();
      box.textContent = "";
    },
  };
  return diffText(d);
}


// The views of the Parts tab: a part or an assembly that a person turns, takes apart and cuts
// open, to look at the design.

// inspect draws the items in the element box. opts.items: [{ url, color, name, explode }], with
// the models in their places (mm, Z up). explode is the direction [x, y, z] that the item moves
// in the exploded view, or null: then the item moves away from the middle. opts.onPick(i) runs
// when a person taps item i, or -1 for the empty space. It returns the controls of the view:
// explode(f) and cut(f), with f from 0 to 1, highlight(i), and reset().
async function inspect(box, opts) {
  hide();
  const { T, OrbitControls, RoomEnvironment } = await load();
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.toneMapping = T.ACESFilmicToneMapping;
  renderer.localClippingEnabled = true;
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
  const camera = new T.PerspectiveCamera(30, 1, 1, 50000);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;

  const items = opts.items || [];
  const geos = await Promise.all(items.map((it) => readSTL(T, it.url).catch(() => null)));
  // The cut: a plane across X of the view. The back faces show where the plane cuts a wall.
  const plane = new T.Plane(new T.Vector3(-1, 0, 0), 1e9);
  const group = new T.Group();
  group.rotation.x = -Math.PI / 2; // Z of the files is up
  const meshes = [];
  geos.forEach((g, i) => {
    if (!g) return;
    const it = items[i];
    const front = new T.MeshPhysicalMaterial({ color: it.color || "#e8762b", roughness: 0.5, clearcoat: 0.2, envMapIntensity: 0.8,
      clippingPlanes: [plane], transparent: true, opacity: 1 });
    const back = new T.MeshBasicMaterial({ color: new T.Color(it.color || "#e8762b").multiplyScalar(0.45), side: T.BackSide,
      clippingPlanes: [plane], transparent: true, opacity: 1 });
    const m = new T.Mesh(g, front);
    m.add(new T.Mesh(g, back));
    m.userData.index = i;
    g.computeBoundingBox();
    m.userData.center = g.boundingBox.getCenter(new T.Vector3());
    group.add(m);
    meshes.push(m);
  });
  scene.add(group);
  // The whole stands on the floor, in the middle.
  scene.updateMatrixWorld(true);
  const all = new T.Box3().setFromObject(group);
  const mid = all.getCenter(new T.Vector3());
  group.position.set(-mid.x, -all.min.y, -mid.z);
  scene.updateMatrixWorld(true);
  const size = all.getSize(new T.Vector3());
  const len = Math.max(20, size.length());
  const floor = new T.GridHelper(len * 2, 20, 0x5b5f66, 0x3a3d42);
  floor.material.transparent = true;
  floor.material.opacity = 0.5;
  scene.add(floor);

  // The explode: each item moves along its direction, or away from the middle of all (in the
  // coordinates of the files).
  const middle = new T.Box3().setFromObject(group).getCenter(new T.Vector3());
  group.worldToLocal(middle);
  meshes.forEach((m) => {
    const it = items[m.userData.index];
    let d;
    if (it.explode && it.explode.length === 3) {
      d = new T.Vector3(...it.explode);
    } else {
      d = m.userData.center.clone().sub(middle);
      if (d.length() < len * 0.02) d.set(0, 0, 1);
      d.normalize();
    }
    m.userData.dir = d.multiplyScalar(len * 0.45);
  });
  let exploded = 0, cutAt = 1, picked = -1;
  const place = () => {
    for (const m of meshes) m.position.copy(m.userData.dir).multiplyScalar(exploded);
  };
  const cutPlane = () => {
    // cut 1: no cut; 0: all cut away. The plane moves across X of the view.
    const b = new T.Box3().setFromObject(group);
    plane.constant = cutAt >= 0.999 ? 1e9 : b.min.x + (b.max.x - b.min.x) * cutAt;
  };
  const paint = () => {
    for (const m of meshes) {
      const on = picked < 0 || m.userData.index === picked;
      for (const mat of [m.material, m.children[0].material]) {
        mat.opacity = on ? 1 : 0.15;
        mat.depthWrite = on;
      }
      m.material.emissive = new T.Color(on && picked >= 0 ? 0x332211 : 0x000000);
    }
  };

  // The distance of the camera that shows a ball of the radius r.
  const reach = (r) => (r / Math.sin((camera.fov * Math.PI) / 360)) * 1.15;
  const dirHome = new T.Vector3(0.55, 0.42, 0.72).normalize();
  const home = () => {
    camera.position.copy(dirHome).multiplyScalar(reach(len / 2)).add(new T.Vector3(0, size.y / 2, 0));
    controls.target.set(0, size.y / 2, 0);
    controls.update();
  };
  // After an explode, the camera goes back when the items are out of the view.
  const fitOut = () => {
    const b = new T.Box3().setFromObject(group);
    const ball = b.getBoundingSphere(new T.Sphere());
    const need = reach(ball.radius + ball.center.distanceTo(controls.target) * 0.6);
    const off = camera.position.clone().sub(controls.target);
    if (off.length() < need) camera.position.copy(controls.target).add(off.setLength(need));
  };
  home();
  controls.minDistance = len * 0.15;
  controls.maxDistance = len * 12;

  // A tap (not a turn) picks an item.
  const ray = new T.Raycaster();
  let down = null;
  const el = renderer.domElement;
  const onDown = (e) => { down = [e.clientX, e.clientY]; };
  const onUp = (e) => {
    if (!down || Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 6) return;
    const r = el.getBoundingClientRect();
    ray.setFromCamera(new T.Vector2(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1), camera);
    // A point that the cut took away is not a hit.
    const hits = ray.intersectObjects(meshes, false).filter((h) => plane.distanceToPoint(h.point) >= 0);
    const i = hits.length ? hits[0].object.userData.index : -1;
    if (opts.onPick) opts.onPick(i);
  };
  el.addEventListener("pointerdown", onDown);
  el.addEventListener("pointerup", onUp);

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
      el.removeEventListener("pointerdown", onDown);
      el.removeEventListener("pointerup", onUp);
      controls.dispose();
      renderer.dispose();
      geos.forEach((g) => g && g.dispose());
      box.textContent = "";
    },
  };
  return {
    explode(f) { exploded = Math.max(0, Math.min(1, +f)); place(); cutPlane(); fitOut(); },
    cut(f) { cutAt = Math.max(0, Math.min(1, +f)); cutPlane(); },
    highlight(i) { picked = i; paint(); },
    reset() { home(); },
  };
}

// thumbs draws a picture of each part for the cards of the Parts tab, with one renderer.
// parts: [{ urls, color }]. It returns a promise of the PNG data URLs, "" for a part that did not
// load.
async function thumbs(parts, px) {
  const { T, RoomEnvironment } = await load();
  const size = px || 320;
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true, preserveDrawingBuffer: true });
  renderer.setPixelRatio(1);
  renderer.setSize(size, size, false);
  renderer.toneMapping = T.ACESFilmicToneMapping;
  const scene = new T.Scene();
  const pmrem = new T.PMREMGenerator(renderer);
  scene.environment = pmrem.fromScene(new RoomEnvironment(), 0.04).texture;
  scene.environmentIntensity = 0.35;
  scene.add(new T.HemisphereLight(0xffffff, 0x50555f, 0.7));
  const key = new T.DirectionalLight(0xffffff, 1.5);
  key.position.set(-1, 1.6, 1.4);
  scene.add(key);
  const camera = new T.PerspectiveCamera(30, 1, 1, 50000);
  const out = [];
  for (const p of parts) {
    try {
      const geos = await Promise.all((p.urls || []).map((u) => readSTL(T, u)));
      const group = new T.Group();
      const mat = new T.MeshPhysicalMaterial({ color: p.color || "#e8762b", roughness: 0.5, clearcoat: 0.2, envMapIntensity: 0.8 });
      for (const g of geos) group.add(new T.Mesh(g, mat));
      group.rotation.x = -Math.PI / 2;
      scene.add(group);
      scene.updateMatrixWorld(true);
      const b = new T.Box3().setFromObject(group);
      const mid = b.getCenter(new T.Vector3());
      group.position.sub(mid);
      const r = b.getSize(new T.Vector3()).length() / 2;
      const dist = r / Math.sin((camera.fov * Math.PI) / 360) * 1.05;
      camera.position.set(dist * 0.55, dist * 0.5, dist * 0.67);
      camera.lookAt(0, 0, 0);
      renderer.render(scene, camera);
      out.push(renderer.domElement.toDataURL("image/png"));
      scene.remove(group);
      geos.forEach((g) => g.dispose());
      mat.dispose();
    } catch {
      out.push("");
    }
  }
  renderer.dispose();
  return out;
}

window.pwfView3d = { show, showPart, showModels, showGcode, hide, modelFile, describeDiff, showDiff, inspect, thumbs };
