// The photo viewer of the pages: a photo full size over the page, with the next and the previous
// photo of its grid. A link in a grid with data-photo opens it. Each photo has an address: the
// address of the page with #photo=<id>. That address opens the page with the photo over it, and
// a close shows the page.

(() => {
  let box = null; // the viewer
  let list = []; // the links of the grid of the photo that shows
  let at = 0;
  let fromHash = false; // the viewer opened from the address, and not from a tap

  const want = () => {
    const m = /^#photo=([^&]+)/.exec(location.hash);
    return m ? decodeURIComponent(m[1]) : "";
  };
  const linkOf = (id) => location.origin + location.pathname + location.search + "#photo=" + encodeURIComponent(id);

  function build() {
    box = document.createElement("div");
    box.className = "lightbox";
    box.hidden = true;
    box.setAttribute("role", "dialog");
    box.setAttribute("aria-modal", "true");
    box.setAttribute("aria-label", "Photo");
    box.innerHTML = `
      <figure><img alt=""><figcaption><span class="lb-by"></span><span class="lb-n"></span></figcaption></figure>
      <button type="button" class="lb-prev" aria-label="Previous photo">&#8249;</button>
      <button type="button" class="lb-next" aria-label="Next photo">&#8250;</button>
      <div class="lb-keys">
        <button type="button" class="lb-copy">Copy link</button>
        <a class="lb-full" target="_blank" rel="noopener">Full size</a>
        <button type="button" class="lb-close">Close</button>
      </div>
      <span class="lb-msg" role="status"></span>`;
    document.body.appendChild(box);
    box.querySelector(".lb-prev").onclick = () => step(-1);
    box.querySelector(".lb-next").onclick = () => step(1);
    box.querySelector(".lb-close").onclick = close;
    box.querySelector(".lb-copy").onclick = copy;
    box.addEventListener("click", (e) => { if (e.target === box || e.target.tagName === "FIGURE") close(); });
    let x0 = null;
    box.addEventListener("touchstart", (e) => { x0 = e.touches.length === 1 ? e.touches[0].clientX : null; }, { passive: true });
    box.addEventListener("touchend", (e) => {
      if (x0 === null) return;
      const dx = e.changedTouches[0].clientX - x0;
      x0 = null;
      if (Math.abs(dx) > 50) step(dx < 0 ? 1 : -1);
    });
  }

  function draw() {
    const a = list[at];
    const img = box.querySelector("img");
    const small = a.querySelector("img");
    img.src = small ? small.src : ""; // the small copy shows at once, then the photo
    const full = new Image();
    full.onload = () => { if (list[at] === a) img.src = a.href; };
    full.src = a.href;
    img.alt = small ? small.alt : "A photo";
    box.querySelector(".lb-by").textContent = a.dataset.caption || "";
    box.querySelector(".lb-n").textContent = list.length > 1 ? `${at + 1} of ${list.length}` : "";
    box.querySelector(".lb-full").href = a.href;
    box.querySelector(".lb-prev").hidden = box.querySelector(".lb-next").hidden = list.length < 2;
    box.querySelector(".lb-msg").textContent = "";
  }

  // show opens the photo of the link a.
  function show(a) {
    if (!box) build();
    const grid = a.closest("[data-gallery]") || a.parentElement;
    list = [...grid.querySelectorAll("a[data-photo]")];
    at = Math.max(0, list.indexOf(a));
    draw();
    if (box.hidden) {
      box.hidden = false;
      document.documentElement.classList.add("lb-open");
      box.querySelector(".lb-close").focus({ preventScroll: true });
    }
  }

  function step(d) {
    if (list.length < 2) return;
    at = (at + d + list.length) % list.length;
    draw();
    history.replaceState(history.state, "", "#photo=" + encodeURIComponent(list[at].dataset.photo));
  }

  // close hides the viewer, and takes #photo off the address.
  function close() {
    if (!box || box.hidden) return;
    box.hidden = true;
    document.documentElement.classList.remove("lb-open");
    if (!want()) return;
    if (fromHash) history.replaceState(history.state, "", location.pathname + location.search);
    else history.back(); // the tap added the address of the photo to the history
  }

  async function copy() {
    const msg = box.querySelector(".lb-msg");
    try {
      await navigator.clipboard.writeText(linkOf(list[at].dataset.photo));
      msg.textContent = "Link copied";
    } catch {
      msg.textContent = linkOf(list[at].dataset.photo);
    }
  }

  // sync shows the photo of the address, when its grid is on the page. The pages draw their grids
  // after they load, so it looks again for a while.
  let tries = 0;
  function sync() {
    const id = want();
    if (!id) {
      if (box && !box.hidden) { box.hidden = true; document.documentElement.classList.remove("lb-open"); }
      return;
    }
    const a = [...document.querySelectorAll("a[data-photo]")].find((x) => x.dataset.photo === id && x.offsetParent !== null) ||
      [...document.querySelectorAll("a[data-photo]")].find((x) => x.dataset.photo === id);
    if (a) {
      tries = 0;
      show(a);
    } else if (tries++ < 40) {
      setTimeout(sync, 250);
    }
  }

  document.addEventListener("click", (e) => {
    const a = e.target.closest && e.target.closest("a[data-photo]");
    if (!a || e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    fromHash = false;
    history.pushState(history.state, "", "#photo=" + encodeURIComponent(a.dataset.photo));
    show(a);
  });
  document.addEventListener("keydown", (e) => {
    if (!box || box.hidden) return;
    if (e.key === "Escape") close();
    else if (e.key === "ArrowLeft") step(-1);
    else if (e.key === "ArrowRight") step(1);
  });
  window.addEventListener("popstate", () => { fromHash = true; tries = 0; sync(); });
  window.addEventListener("hashchange", () => { fromHash = true; tries = 0; sync(); });
  const start = () => { fromHash = true; sync(); };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
