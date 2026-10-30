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

window.pwfView3d = { show, hide, modelFile };
