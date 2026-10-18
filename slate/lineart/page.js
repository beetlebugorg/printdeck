// Draws one shot in the line-art style of the beetlebug.org explode viewer:
// orthographic camera, Z up, flat matte fill, dark edge lines on top.
// render.mjs calls window.renderShot(shot, theme) and takes a screenshot of #view.

import * as THREE from "three";
import { STLLoader } from "three/addons/loaders/STLLoader.js";

// The values are the same as in the site viewer (src/scripts/explode-viewer.ts).
const THEMES = {
  light: {
    fill: { print: "#f3f3ef", buy: "#c3c7ca", dark: "#4a4f55", clear: "#d9eef2" },
    ink: "#16181a", inkOpacity: 0.55, accent: "#0a7f63", mix: 0.22, bg: "#fbfbf9",
  },
  dark: {
    fill: { print: "#3a4144", buy: "#5c6468", dark: "#22272a", clear: "#2c4a52" },
    ink: "#eceeed", inkOpacity: 0.5, accent: "#3fd1a6", mix: 0.5, bg: "#0f1112",
  },
};
const EDGE_ANGLE = 28;

const loader = new STLLoader();
const cache = new Map();

function geometry(url) {
  if (!cache.has(url)) {
    cache.set(url, loader.loadAsync(url).then((g) => {
      g.computeVertexNormals();
      return { mesh: g, edges: new THREE.EdgesGeometry(g, EDGE_ANGLE) };
    }));
  }
  return cache.get(url);
}

const view = document.getElementById("view");
const labels = document.getElementById("labels");
const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, preserveDrawingBuffer: true });
renderer.setPixelRatio(1);
renderer.setClearColor(0x000000, 0);
view.prepend(renderer.domElement);

// shot: { size, parts: [{ url, finish, label, focus, offset }], camera: [x, y, z], labelPx }
window.renderShot = async (shot, themeName) => {
  const T = THEMES[themeName];
  document.documentElement.style.setProperty("--accent", T.accent);
  document.documentElement.style.setProperty("--bg", T.bg);
  const size = shot.size;
  view.style.width = view.style.height = `${size}px`;
  renderer.setSize(size, size);

  const scene = new THREE.Scene();
  scene.add(new THREE.HemisphereLight(0xffffff, 0xb0b0b0, 2.6));
  const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, -10000, 10000);
  camera.up.set(0, 0, 1);
  const key = new THREE.DirectionalLight(0xffffff, 1.8);
  key.position.set(1, -2, 3);
  camera.add(key);
  scene.add(camera);

  const accent = new THREE.Color(T.accent);
  const ink = new THREE.Color(T.ink);
  const nodes = [];
  const geoms = await Promise.all(shot.parts.map((p) => geometry(p.url)));
  shot.parts.forEach((p, i) => {
    const color = new THREE.Color(T.fill[p.finish]);
    if (p.focus) color.lerp(accent, T.mix);
    const clear = p.finish === "clear";
    const mesh = new THREE.Mesh(geoms[i].mesh, new THREE.MeshStandardMaterial({
      color, roughness: 0.75, metalness: p.finish === "buy" ? 0.35 : 0,
      polygonOffset: true, polygonOffsetFactor: 1, polygonOffsetUnits: 1,
      transparent: clear, opacity: clear ? 0.55 : 1,
    }));
    const edges = new THREE.LineSegments(geoms[i].edges, new THREE.LineBasicMaterial({
      color: p.focus ? accent : ink, transparent: true, opacity: p.focus ? 0.95 : T.inkOpacity,
    }));
    const group = new THREE.Group();
    group.add(mesh, edges);
    scene.add(group);
    const home = new THREE.Box3().setFromObject(mesh);
    const off = new THREE.Vector3(...(p.offset || [0, 0, 0]));
    group.position.copy(off);
    group.updateMatrixWorld(true);
    nodes.push({ p, mesh, home, off });
  });

  // A dashed guide line from the home of each moved part to its exploded position.
  // One guide for each label, from the centre of all the parts with that label.
  const byLabel = new Map();
  for (const n of nodes) {
    if (n.off.lengthSq() < 1) continue;
    const k = `${n.p.label}|${n.off.toArray()}`;
    if (!byLabel.has(k)) byLabel.set(k, { box: new THREE.Box3(), off: n.off, focus: n.p.focus });
    byLabel.get(k).box.union(n.home);
  }
  for (const g of byLabel.values()) {
    const a = g.box.getCenter(new THREE.Vector3());
    const line = new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([a, a.clone().add(g.off)]),
      new THREE.LineDashedMaterial({
        color: g.focus ? accent : ink, dashSize: 3, gapSize: 2.5, transparent: true,
        opacity: g.focus ? 1 : T.inkOpacity,
      }),
    );
    line.computeLineDistances();
    scene.add(line);
  }

  // Fit the camera to the bounding sphere of the visible parts, as the site viewer does.
  const box = new THREE.Box3();
  for (const n of nodes) box.expandByObject(n.mesh);
  const sphere = box.getBoundingSphere(new THREE.Sphere());
  const dir = new THREE.Vector3(...shot.camera).normalize();
  const half = (sphere.radius * 2.25) / 2;
  Object.assign(camera, { left: -half, right: half, top: half, bottom: -half });
  camera.position.copy(sphere.center).addScaledVector(dir, sphere.radius * 6);
  camera.lookAt(sphere.center);
  camera.updateProjectionMatrix();
  camera.updateMatrixWorld(true);
  renderer.render(scene, camera);

  // Labels above each focus part, one for each name. A label that overlaps a label moves up.
  labels.replaceChildren();
  const tags = [];
  const seen = new Map();
  for (const n of nodes) {
    if (!n.p.focus || !n.p.label) continue;
    const b = new THREE.Box3().setFromObject(n.mesh);
    if (seen.has(n.p.label)) seen.get(n.p.label).union(b);
    else seen.set(n.p.label, b);
  }
  const px = shot.labelPx;
  for (const [text, b] of seen) {
    const el = document.createElement("span");
    el.className = "tag";
    el.textContent = text;
    el.style.fontSize = `${px}px`;
    el.style.padding = `${px * 0.25}px ${px * 0.65}px`;
    labels.append(el);
    const p = b.getCenter(new THREE.Vector3());
    p.z = b.max.z;
    p.project(camera);
    const w = el.offsetWidth, h = el.offsetHeight;
    tags.push({ el, x: ((p.x + 1) / 2) * size - w / 2, y: ((1 - p.y) / 2) * size - h * 1.3, w, h });
  }
  tags.sort((a, b) => b.y - a.y);
  const placed = [];
  for (const t of tags) {
    for (let k = 0; k < tags.length; k++) {
      const hit = placed.find((o) => t.x < o.x + o.w + 4 && o.x < t.x + t.w + 4 && t.y < o.y + o.h + 3 && o.y < t.y + t.h + 3);
      if (!hit) break;
      t.y = hit.y - t.h - 3;
    }
    placed.push(t);
    t.el.style.transform = `translate(${t.x}px, ${t.y}px)`;
  }
  await document.fonts.ready;
  await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  for (const n of nodes) {
    n.mesh.material.dispose();
  }
  return true;
};
window.pageReady = true;
