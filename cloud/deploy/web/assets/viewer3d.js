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

// readPart reads part.bin (gzip): the smooth mesh of a resin part (see deck/internal/chitu/smooth.go).
// It returns the geometry in mm in the frame of the node "bed" (X to the right, Y the height from
// the plate, Z to the front), and the heights of the vertices for the colors.
async function readPart(T, url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error("part.bin: " + res.status);
  const buf = await new Response(res.body.pipeThrough(new DecompressionStream("gzip"))).arrayBuffer();
  const dv = new DataView(buf);
  const magic = String.fromCharCode(...new Uint8Array(buf, 0, 4));
  if (magic !== "PDM2") throw new Error("part.bin: " + magic);
  const cell = dv.getFloat32(4, true), slice = dv.getFloat32(8, true);
  const W = dv.getUint16(12, true), H = dv.getUint16(14, true);
  const nv = dv.getUint32(20, true), nt = dv.getUint32(24, true);
  const pos = new Float32Array(nv * 3), height = new Float32Array(nv);
  let o = 28;
  for (let i = 0; i < nv; i++, o += 6) {
    const x = dv.getUint16(o, true) / 64 - 1, y = dv.getUint16(o + 2, true) / 64 - 1, z = dv.getUint16(o + 4, true) / 64 - 1;
    pos[i * 3] = (x + 0.5 - W / 2) * cell;
    pos[i * 3 + 1] = (z + 0.5) * slice;
    pos[i * 3 + 2] = (y + 0.5 - H / 2) * cell;
    height[i] = (z + 0.5) * slice;
  }
  const idx = new Uint32Array(buf, o, nt * 3);
  const g = new T.BufferGeometry();
  g.setAttribute("position", new T.BufferAttribute(pos, 3));
  g.setIndex(new T.BufferAttribute(new Uint32Array(idx), 1));
  g.computeVertexNormals();
  g.setAttribute("color", new T.BufferAttribute(new Float32Array(nv * 3), 3));
  return { g, height };
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

  let printer;
  const file = modelFile(opts.model);
  if (file) {
    try { printer = (await new GLTFLoader().loadAsync(file)).scene; } catch (e) { printer = null; }
  }
  if (!printer) printer = plain(T, "resin");
  scene.add(printer);
  const part = await readPart(T, opts.url);
  paint(T, part, opts.done || 0);
  const mat = new T.MeshPhysicalMaterial({ vertexColors: true, roughness: 0.38, metalness: 0, clearcoat: 0.6,
    clearcoatRoughness: 0.25, sheen: 0.2 });
  const mesh = new T.Mesh(part.g, mat);
  const bed = printer.getObjectByName("bed") || printer;
  bed.add(mesh);
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
