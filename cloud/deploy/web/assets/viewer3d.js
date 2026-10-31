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
    const [T, G, O] = await Promise.all([
      import(three + "three.module.min.js"),
      import(three + "GLTFLoader.js"),
      import(three + "OrbitControls.js"),
    ]);
    lib = { T, GLTFLoader: G.GLTFLoader, OrbitControls: O.OrbitControls };
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
// as it prints: Y down from the plate, X to the right, Z to the front. It takes as much detail as
// the device holds.
async function readPart(T, url) {
  const c = await readCells(url);
  const big = (navigator.deviceMemory || 4) >= 8 && !/Mobi/.test(navigator.userAgent);
  const maxVerts = big ? 4000000 : 1500000;
  let f = Math.max(1, Math.ceil(Math.sqrt((c.W * c.H * c.D) / (big ? 120e6 : 40e6))));
  let m = null;
  while (!(m = surfaceNet(c, f, maxVerts))) f++;
  const cell = c.cell * f, pos = new Float32Array(m.nv * 3), height = new Float32Array(m.nv);
  for (let i = 0; i < m.nv; i++) {
    const x = m.verts[i * 3], y = m.verts[i * 3 + 1], z = m.verts[i * 3 + 2];
    pos[i * 3] = (x + 0.5 - m.W / 2) * cell;
    pos[i * 3 + 1] = -(z + 0.5) * c.slice;
    pos[i * 3 + 2] = (y + 0.5 - m.H / 2) * cell;
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
  const cured = new T.Color("#e8762b"), gray = new T.Color("#a8aaae");
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
  const { T, GLTFLoader, OrbitControls } = await load();
  const renderer = new T.WebGLRenderer({ antialias: true, alpha: true });
  renderer.setPixelRatio(Math.min(2, window.devicePixelRatio || 1));
  renderer.toneMapping = T.ACESFilmicToneMapping;
  box.textContent = "";
  box.appendChild(renderer.domElement);
  const scene = new T.Scene();
  scene.add(new T.HemisphereLight(0xffffff, 0x6a7080, 1.6));
  const key = new T.DirectionalLight(0xffffff, 2.4);
  key.position.set(-1, 1.6, 1.4);
  scene.add(key);
  const rim = new T.DirectionalLight(0xffe2c4, 1.2);
  rim.position.set(1.2, 0.4, -1);
  scene.add(rim);
  const camera = new T.PerspectiveCamera(30, 1, 1, 20000);
  const controls = new OrbitControls(camera, renderer.domElement);
  controls.enableDamping = true;

  // The build plate only, with the part under it: the case of the printer is in the way.
  const part = await readPart(T, opts.url);
  paint(T, part, opts.done || 0);
  const plateMat = new T.MeshStandardMaterial({ color: "#b8bcc2", metalness: 0.3, roughness: 0.45 });
  const plate = new T.Mesh(new T.BoxGeometry(part.plateW + 8, 6, part.plateD + 8), plateMat);
  plate.position.y = 3;
  scene.add(plate);
  const arm = new T.Mesh(new T.BoxGeometry(60, 24, 30), plateMat);
  arm.position.set(0, 18, -part.plateD / 2 + 10);
  scene.add(arm);
  const mat = new T.MeshPhysicalMaterial({ vertexColors: true, roughness: 0.38, metalness: 0, clearcoat: 0.6,
    clearcoatRoughness: 0.25, sheen: 0.2 });
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

window.pwfView3d = { show, showPart, hide, modelFile };
