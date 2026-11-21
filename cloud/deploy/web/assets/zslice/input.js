// input.js: the mouse, the touch screen, and the wheel over the view (the
// canvas of app.js).
//
// The mouse, as in Cura. In 3D: a right drag orbits, a middle drag (or
// Shift and a drag) pans, and the wheel zooms toward the pointer. A left
// drag orbits too, but in the Select tool a left drag that starts on a
// printed line lights the layers from that layer to the layer under the
// pointer (app.js, rangeStart), and the release opens the card of these
// layers (rangecard.js). On a trackpad: two fingers clicked down and
// slid pan (a right drag after a trackpad scroll: the browser gives no
// device for a button), and a light two-finger swipe or a pinch zooms.
// In 2D: a drag pans, and the wheel zooms (preview.c). In both: a click (a
// release within 4 pixels of the press) selects the line under the
// pointer, and a right click opens the context menu there. In 3D, Shift
// and a click add the part under the pointer to the selection of parts
// (or take it out), and Ctrl and a click select that part alone.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// ctx: { app, view (View3D), view2d, tools (tools.js), supportTool
//        (support.js), requestFrame(), pickAt(px, py, picked), hitAt(px,
//        py), select(path, seg, picked), showContextMenu(x, y),
//        activatePlate(i, frame), signMenu(i, x, y), range: { start(px, py) -> drag state
//        or null, move(state, px, py), end(state), clear() } }
export function attachViewInput(canvas, ctx) {
  const { app, view, view2d, tools, supportTool, requestFrame, pickAt, pickRun, hitAt, select, showContextMenu, activatePlate, signMenu, grab, range } = ctx;
  let drag = null;
  // The last scroll came from a touchpad (see the wheel below).
  let touchpad = false;
  const dpr = () => window.devicePixelRatio || 1;

  // Two fingers on a touch screen: a pinch zooms about the point between
  // them, and a move of that point pans. These listeners run first (the
  // capture phase) and stop the one-pointer handlers while two fingers are
  // down. One finger orbits, as a left drag does.
  const touches = new Map();
  let pinch = null;
  const touchPair = () => {
    const [a, b] = [...touches.values()];
    return { dist: Math.hypot(a.x - b.x, a.y - b.y), mx: (a.x + b.x) / 2, my: (a.y + b.y) / 2 };
  };
  canvas.addEventListener("pointerdown", (e) => {
    if (e.pointerType !== "touch") return;
    touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (touches.size === 2) {
      canvas.setPointerCapture(e.pointerId);
      // The first finger's drag ends: no orbit, and no click at the end.
      drag = null;
      pinch = touchPair();
      e.stopImmediatePropagation();
    }
  }, { capture: true });
  canvas.addEventListener("pointermove", (e) => {
    if (e.pointerType !== "touch" || !touches.has(e.pointerId)) return;
    touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (!pinch || touches.size !== 2) return;
    e.stopImmediatePropagation();
    const now = touchPair();
    const r = canvas.getBoundingClientRect();
    const px = (now.mx - r.left) * dpr();
    const py = (now.my - r.top) * dpr();
    const k = pinch.dist > 0 && now.dist > 0 ? now.dist / pinch.dist : 1;
    if (app.page === "2d") {
      view2d.pan((now.mx - pinch.mx) * dpr(), (now.my - pinch.my) * dpr());
      if (k !== 1) view2d.zoomAt(k, px, py, canvas.height);
    } else {
      view.pan((now.mx - pinch.mx) * dpr(), (now.my - pinch.my) * dpr());
      if (k !== 1) view.zoomAt(1 / k, px, py);
    }
    pinch = now;
    requestFrame();
  }, { capture: true });
  const touchUp = (e) => {
    if (e.pointerType !== "touch") return;
    touches.delete(e.pointerId);
    if (pinch) {
      // The end of a pinch is no click, also for the last finger.
      e.stopImmediatePropagation();
      if (touches.size === 0) pinch = null;
      drag = null;
      canvas.style.cursor = "";
    }
  };
  canvas.addEventListener("pointerup", touchUp, { capture: true });
  canvas.addEventListener("pointercancel", touchUp, { capture: true });

  // The hand of the Grab tool is sticky (the user's design): a click on a
  // part picks it up, and the hand keeps it with no button held. A move of
  // the pointer carries it; Alt and a move turn it (Shift snaps to 15°);
  // the wheel lifts it. The next click, or Esc, lets it go.
  let handTurn = null;
  canvas.addEventListener("pointerdown", (e) => {
    if (pinch || !grab || tools.tool !== "grab" || app.page !== "3d" || e.button !== 0) return;
    e.stopImmediatePropagation();
    e.preventDefault();
    if (grab.holding()) {
      grab.end();
      return;
    }
    if (!view.loaded) return;
    const r = canvas.getBoundingClientRect();
    handTurn = null;
    grab.start((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr());
  }, { capture: true });
  canvas.addEventListener("pointermove", (e) => {
    if (!grab?.holding()) return;
    const r = canvas.getBoundingClientRect();
    const px = (e.clientX - r.left) * dpr();
    const py = (e.clientY - r.top) * dpr();
    if (e.altKey) {
      // Turn by 0.5° a pixel of the move; Shift snaps the sum to 15°.
      const t = (handTurn ??= { yaw: 0, tilt: 0, sentYaw: 0, sentTilt: 0, x: e.clientX, y: e.clientY });
      t.yaw += (e.clientX - t.x) * 0.5;
      t.tilt += (e.clientY - t.y) * 0.5;
      t.x = e.clientX;
      t.y = e.clientY;
      const snap = (v) => (e.shiftKey ? Math.round(v / 15) * 15 : v);
      const yaw = snap(t.yaw) - t.sentYaw;
      const tilt = snap(t.tilt) - t.sentTilt;
      if (yaw || tilt) {
        grab.turn(yaw, tilt);
        t.sentYaw += yaw;
        t.sentTilt += tilt;
      }
      return;
    }
    if (handTurn) {
      // After a turn, the hand moves on from here: no jump.
      handTurn = null;
    }
    grab.move(px, py);
  });
  window.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && grab?.holding()) {
      grab.end();
      e.preventDefault();
    }
  });

  canvas.addEventListener("pointerdown", (e) => {
    if (pinch) return;
    canvas.setPointerCapture(e.pointerId);
    drag = { button: e.button, x: e.clientX, y: e.clientY, x0: e.clientX, y0: e.clientY, moved: false, part: null };
    canvas.style.cursor = "grabbing";
    canvas.focus({ preventScroll: true });
    // The measure tape: a left drag on an end of it moves that end.
    if (e.button === 0 && tools.tool === "measure" && tools.pts.length > 0) {
      const r = canvas.getBoundingClientRect();
      const end = tools.endAt((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr(), dpr());
      if (end >= 0) {
        drag.tape = end;
        canvas.style.cursor = "grabbing";
        return;
      }
    }
    // The Select tool in 3D: a left drag that starts on a printed line is
    // for a range of layers.
    if (e.button === 0 && app.page === "3d" && view.loaded && tools.tool === "select" && !e.ctrlKey && !e.shiftKey && !e.altKey && range) {
      const r = canvas.getBoundingClientRect();
      drag.range = range.start((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr());
      if (drag.range) canvas.style.cursor = "ns-resize";
    }
    // The move tool, or Ctrl, in 3D: a left drag on a part moves it.
    if (e.button === 0 && app.page === "3d" && view.loaded && (tools.tool === "move" || e.ctrlKey)) {
      const d = drag;
      d.part = "pending";
      const r = canvas.getBoundingClientRect();
      tools.moveStart((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr()).then((ok) => {
        if (drag === d) d.part = ok ? "moving" : null;
      });
    }
  });
  canvas.addEventListener("pointermove", (e) => {
    if (!drag) return;
    const dx = e.clientX - drag.x;
    const dy = e.clientY - drag.y;
    drag.x = e.clientX;
    drag.y = e.clientY;
    if (dx === 0 && dy === 0) return;
    if (Math.abs(e.clientX - drag.x0) > 4 || Math.abs(e.clientY - drag.y0) > 4) drag.moved = true;
    if (drag.tape !== undefined) {
      const r = canvas.getBoundingClientRect();
      tools.dragEnd(drag.tape, (e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr(), e.shiftKey);
      return;
    }
    if (drag.part === "moving") {
      const r = canvas.getBoundingClientRect();
      tools.moveTo((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr());
      return;
    }

    if (drag.part === "pending") return;
    if (drag.range) {
      if (drag.moved) {
        const r = canvas.getBoundingClientRect();
        range.move(drag.range, (e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr());
      }
      return;
    }
    // A right drag: a mouse orbits (as in Cura); two fingers clicked down
    // on a touchpad pan. The browser gives both as the right button, so
    // the last scroll tells the device (see `touchpad`). A middle drag and
    // Shift with a drag pan; a left drag orbits.
    if (app.page === "2d") view2d.pan(dx * dpr(), dy * dpr());
    else if (drag.button === 1 || (drag.button === 2 && touchpad) || e.shiftKey) view.pan(dx * dpr(), dy * dpr());
    else view.orbit(dx, dy);
    requestFrame();
  });
  canvas.addEventListener("pointerup", (e) => {
    const d = drag;
    endDrag();
    if (d?.part === "moving") {
      tools.moveEnd();
      return;
    }
    // A press on an end of the tape: a drag moved it; a click leaves it.
    if (d?.tape !== undefined) return;
    // The end of a drag over layers: the card of the range.
    if (d?.range && d.moved) {
      range.end(d.range);
      return;
    }
    if (!d || d.moved) return;
    const r = canvas.getBoundingClientRect();
    const px = (e.clientX - r.left) * dpr();
    const py = (e.clientY - r.top) * dpr();
    // A click on the sign of a plate goes to that plate; a right click
    // opens its menu. A sign never covers a part, so it gets only the
    // clicks on itself.
    if (app.page === "3d" && (d.button === 0 || d.button === 2)) {
      const s = view.signAt(px, py);
      if (s >= 0) {
        if (d.button === 0) activatePlate(s, true);
        else signMenu?.(s, e.clientX, e.clientY);
        return;
      }
    }
    if (!view.loaded) return;
    // A click elsewhere ends a range of layers.
    if (d.button === 0) range?.clear();
    // A click on another plate (its bed or its beads) makes it active.
    if (d.button === 0 && app.page === "3d" && app.plates.length > 1) {
      const i = view.plateAt(px, py);
      if (i >= 0 && i !== app.activePlate) {
        activatePlate(i, false);
        return;
      }
    }
    if (d.button === 0 && tools.tool === "support" && app.page === "3d") supportTool.click(px, py, e.shiftKey, e.altKey);
    else if (d.button === 0 && tools.tool === "measure") tools.measureClick(px, py, e.shiftKey);
    // The Select tool: Shift with a click selects the run of the line there
    // (from one retraction to the next), and Ctrl+Shift adds the part to
    // the selection. Ctrl with a click selects that part alone. In the
    // other tools, Shift with a click adds the part, or takes it out.
    else if (d.button === 0 && e.shiftKey && !e.ctrlKey && tools.tool === "select") pickRun(px, py);
    else if (d.button === 0 && app.page === "3d" && e.shiftKey && tools.tool !== "grab") tools.addPart(px, py);
    else if (d.button === 0 && app.page === "3d" && (tools.tool === "move" || tools.tool === "turn" || e.ctrlKey)) tools.pickPart(px, py);
    else if (d.button === 0) pickAt(px, py, true);
    else if (d.button === 2) {
      // A right click on a handle of a picked support tree: its menu.
      if (app.page === "3d" && supportTool?.menuAt(px, py, e.clientX, e.clientY)) return;
      // A right click: the line there, then the menu.
      if (hitAt(px, py)) select(view.hit[0], view.hit[1], false);
      showContextMenu(e.clientX, e.clientY);
    }
  });
  const endDrag = () => {
    drag = null;
    canvas.style.cursor = "";
  };
  canvas.addEventListener("pointercancel", endDrag);
  canvas.addEventListener("contextmenu", (e) => e.preventDefault());
  // A double click: on another plate (with more than one), the camera goes
  // to it and it becomes the active plate; on a side of a part (3D), the view
  // turns to face that side and zooms to it; elsewhere, the view resets.
  canvas.addEventListener("dblclick", async (e) => {
    if (app.page === "2d") {
      view2d.fit(canvas.width, canvas.height);
      requestFrame();
      return;
    }
    if (app.plates.length > 1) {
      const r = canvas.getBoundingClientRect();
      const i = view.plateAt((e.clientX - r.left) * dpr(), (e.clientY - r.top) * dpr());
      if (i >= 0 && i !== app.activePlate) {
        activatePlate(i, true);
        return;
      }
    }
    const px = e.offsetX * dpr();
    const py = e.offsetY * dpr();
    // The side of the print under the pointer (zs_view_look_at_side), from
    // the size of its part.
    const part = app.state !== "empty" ? await tools.partAt(px, py) : null;
    const size = part ? Math.max(part.sizeX, part.sizeY, part.sizeZ) : 0;
    if (app.state !== "empty" && view.x.zs_view_look_at_side(view.v, px, py, size)) {
      // The first click of the double click picked a line: not wanted.
      select(-1, -1, false);
      requestFrame();
      return;
    }
    view.resetSmooth();
    requestFrame();
  });
  // A wheel event from a trackpad: two fingers that move (pixels, small
  // steps, or a sideways part) pan the view, as in Fusion and Tinkercad. A
  // mouse wheel gives steps of 100 pixels or more, or lines, and zooms. A
  // pinch has Ctrl, and zooms. Shift and a mouse wheel: zoom too.
  let trackpadUntil = 0;
  function fromTrackpad(e) {
    if (e.ctrlKey || e.deltaMode !== 0) return false;
    const now = performance.now();
    // A sideways part, or a small step: a trackpad. Its next events in the
    // same gesture count as the trackpad too (a fast swipe has big steps).
    if (e.deltaX !== 0 || Math.abs(e.deltaY) < 50 || !Number.isInteger(e.deltaY)) {
      trackpadUntil = now + 300;
      return true;
    }
    return now < trackpadUntil;
  }
  // The last scroll: from a touchpad (true) or a mouse wheel (false).
  // Before any scroll: a mouse.
  canvas.addEventListener("wheel", (e) => {
    if (e.ctrlKey) return;
    touchpad = fromTrackpad(e);
  }, { capture: true, passive: true });

  canvas.addEventListener("wheel", (e) => {
    e.preventDefault();
    const r = canvas.getBoundingClientRect();
    // The hand holds a part: the wheel lifts it or lowers it.
    if (grab?.holding()) {
      grab.lift(e.deltaMode === 1 ? e.deltaY * 33 : e.deltaY);
      return;
    }
    // The move tool and a mouse wheel: turn the selected part 15° a step.
    if (tools.tool === "move" && tools.part && !e.ctrlKey && !fromTrackpad(e)) {
      tools.turn(e.deltaY);
      return;
    }
    if (fromTrackpad(e)) {
      // A two-finger swipe zooms toward the pointer, as a pinch does: 40
      // pixels of the swipe are one wheel step.
      const steps = e.deltaY / 40;
      const px = (e.clientX - r.left) * dpr();
      const py = (e.clientY - r.top) * dpr();
      if (app.page === "2d") view2d.zoomAt(Math.pow(1.2, -steps), px, py, canvas.height);
      else view.zoomAt(Math.pow(1.15, steps), px, py);
      requestFrame();
      return;
    }
    // One wheel step is about 100 pixels (deltaMode 0) or 3 lines.
    // A pinch on a trackpad comes as a wheel event with Ctrl and small
    // steps, so it counts more for each pixel.
    const per = e.ctrlKey ? 25 : 100;
    const steps = e.deltaMode === 1 ? e.deltaY / 3 : e.deltaMode === 2 ? e.deltaY : e.deltaY / per;
    const px = (e.clientX - r.left) * dpr();
    const py = (e.clientY - r.top) * dpr();
    if (app.page === "2d") view2d.zoomAt(Math.pow(1.2, -steps), px, py, canvas.height);
    else view.zoomAt(Math.pow(1.15, steps), px, py);
    requestFrame();
  }, { passive: false });
}
