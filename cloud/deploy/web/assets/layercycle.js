// The cycle of one layer of a resin print, from the front, cut through the vat: the light under
// the film, the plate with the part, its lift out of the resin and its return. A bar under it has
// the steps with their seconds. The share page calls pwfLayerCycle.show with the numbers of the
// file and of the print.

let run = null; // the animation now: { stop }

// steps returns the steps of a layer: [name, seconds, color].
function steps(p) {
  const light = p.exposure || 0, off = p.lightOff || 0;
  const lift = p.liftSpeed > 0 ? (p.lift / p.liftSpeed) * 60 : 0;
  const ret = p.retractSpeed > 0 ? (p.lift / p.retractSpeed) * 60 : 0;
  const out = [["Light", light, "#8b5cf6"]];
  if (off > 0) out.push(["Light off", off, "#5b5f66"]);
  out.push(["Lift", lift, "#e8762b"], ["Return", ret, "#f2a65a"]);
  const rest = p.measured > 0 ? p.measured - (light + off + lift + ret) : 0;
  if (rest > 0.5) out.push(["Rest", rest, "#3a3d42"]); // the rest after the return, and the time of the board
  return out;
}

// show draws the cycle in the canvas, and plays it at speed times the real time. In real time with
// start (the time of the start of the layer now, in ms of the clock of the page), it plays in step
// with the printer: the step now of the printer.
function show(canvas, p, speed, start) {
  if (run) run.stop();
  const ctx = canvas.getContext("2d");
  const st = steps(p);
  const total = st.reduce((a, s) => a + s[1], 0);
  if (total <= 0) return;
  let t0 = performance.now(), frame = 0;
  const draw = (now) => {
    frame = requestAnimationFrame(draw);
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    const w = canvas.clientWidth, h = canvas.clientHeight;
    if (canvas.width !== Math.round(w * dpr)) { canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr); }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    const sync = speed === 1 && start > 0;
    const t = sync ? (((Date.now() - start) / 1000) % total + total) % total : (((now - t0) / 1000) * speed) % total;
    // The step now, and the time in it.
    let at = 0, i = 0;
    while (i < st.length - 1 && t >= at + st[i][1]) { at += st[i][1]; i++; }
    const [name, secs] = st[i], into = t - at;
    // The height of the plate over the film, from 0 to 1 of the lift.
    let up = 0;
    if (name === "Lift") up = into / secs;
    else if (name === "Return") up = 1 - into / secs;
    const scene = h - 64;
    // The screen and the light.
    const vatL = w * 0.18, vatR = w * 0.82, film = scene - 22;
    ctx.fillStyle = "#15161a";
    ctx.fillRect(vatL - 10, film + 4, vatR - vatL + 20, 18);
    if (name === "Light") {
      const g = ctx.createLinearGradient(0, film + 4, 0, film - 40);
      g.addColorStop(0, "rgba(139,92,246,0.85)");
      g.addColorStop(1, "rgba(139,92,246,0)");
      ctx.fillStyle = g;
      ctx.fillRect(w * 0.35, film - 40, w * 0.3, 44);
    }
    // The vat: the walls, the film and the resin.
    const resinTop = film - 46;
    ctx.fillStyle = "rgba(232,118,43,0.28)";
    ctx.fillRect(vatL, resinTop, vatR - vatL, film - resinTop);
    ctx.strokeStyle = "#8e959c";
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(vatL, resinTop - 30); ctx.lineTo(vatL, film); ctx.lineTo(vatR, film); ctx.lineTo(vatR, resinTop - 30);
    ctx.stroke();
    ctx.strokeStyle = "#c9ccd1";
    ctx.lineWidth = 1;
    ctx.beginPath(); ctx.moveTo(vatL, film); ctx.lineTo(vatR, film); ctx.stroke();
    // The plate and the part: the layers so far under the plate, the new layer at the bottom.
    const lift = (scene - 40) * 0.62 * up;
    const partH = 34, partBottom = film - lift;
    const plateY = partBottom - partH - 12;
    ctx.fillStyle = "#b8bcc2";
    ctx.fillRect(w * 0.3, plateY, w * 0.4, 12);
    ctx.fillRect(w * 0.47, 8, w * 0.06, plateY - 8);
    for (let k = 0; k < 6; k++) {
      ctx.fillStyle = k === 5 && name !== "Light" ? "#f08a3e" : "#d9682a";
      ctx.fillRect(w * 0.38 + k, plateY + 12 + k * (partH / 6), w * 0.24 - 2 * k, partH / 6 - 1);
    }
    if (name === "Light") {
      ctx.fillStyle = `rgba(139,92,246,${0.4 + 0.4 * Math.sin(now / 120) ** 2})`;
      ctx.fillRect(w * 0.38 + 5, partBottom - partH / 6, w * 0.24 - 10, partH / 6);
    }
    // The text of the step now.
    ctx.fillStyle = "#f3f1ec";
    ctx.font = "600 15px Archivo, sans-serif";
    ctx.fillText(`${name} · ${into.toFixed(0)} of ${secs.toFixed(0)} s`, 12, 22);
    ctx.font = "12px 'IBM Plex Mono', monospace";
    ctx.fillStyle = "#c2bcb0";
    const note = p.measured > 0 ? `a layer takes ${p.measured.toFixed(0)} s (measured)` : `a layer takes about ${total.toFixed(0)} s`;
    ctx.fillText(note + (sync ? " · in step with the printer" : speed > 1 ? ` · played ${speed}x` : ""), 12, 40);
    // The bar of the steps, with the cursor.
    const barY = h - 44, barH = 14;
    let x = 12;
    const bw = w - 24;
    ctx.font = "11px 'IBM Plex Mono', monospace";
    st.forEach(([n, s, c], k) => {
      const sw = (s / total) * bw;
      ctx.fillStyle = c;
      ctx.fillRect(x, barY, Math.max(1, sw - 1), barH);
      if (sw > 54) {
        ctx.fillStyle = "#f3f1ec";
        ctx.fillText(`${n} ${s.toFixed(0)}s`, x + 3, barY + barH + 14);
      }
      x += sw;
    });
    const cx = 12 + (t / total) * bw;
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(cx - 1, barY - 4, 2, barH + 8);
  };
  frame = requestAnimationFrame(draw);
  run = { stop() { cancelAnimationFrame(frame); run = null; } };
}

window.pwfLayerCycle = { show, steps };
