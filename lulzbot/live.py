"""The live model window: it shows a model of build/viewer (see viewer.py) and loads it again each
time a model script publishes a new version. The camera stays where it is.

    uv run --no-project --with pyvista --with trimesh python lulzbot/live.py [NAME]

The model is in its own frame, z up, the front toward -y. The camera turns about the vertical, so
the model stays upright: drag to turn, the wheel zooms, shift and drag pans. Keys: i is the
isometric view from the front right, p shows the printer, x makes the parts see-through, f fits the
view, 1 to 9 hide or show the parts in the list (the list is in the window).
"""

import json
import os
import sys

import pyvista as pv

from viewer import OUT

NAME = sys.argv[1] if len(sys.argv) > 1 else "pod"


class Live:
    def __init__(self):
        self.pl = pv.Plotter(title=f"Printdeck: {NAME}", window_size=(1400, 1000))
        self.pl.set_background("#E9EBEF", top="#FFFFFF")
        self.pl.enable_anti_aliasing("ssaa")
        self.seen = {}           # model name -> stamp
        self.actors = {}         # (model, part) -> actor
        self.hidden = set()
        self.xray = False
        self.printer = False
        self.first = True
        self.parts = []
        for k, f in (("p", self.toggle_printer), ("x", self.toggle_xray), ("f", self.fit), ("i", self.iso)):
            self.pl.add_key_event(k, f)
        for i in range(1, 10):
            self.pl.add_key_event(str(i), lambda i=i: self.toggle_part(i - 1))
        self.pl.add_axes()
        self.pl.enable_terrain_style(mouse_wheel_zooms=True, shift_pans=True)

    def load(self, model):
        path = os.path.join(OUT, f"{model}.json")
        try:
            with open(path) as fh:
                info = json.load(fh)
        except (OSError, ValueError):
            return False
        if self.seen.get(model) == info["t"]:
            return False
        self.seen[model] = info["t"]
        names = set()
        for p in info["parts"]:
            key = (model, p["name"])
            names.add(key)
            try:
                mesh = pv.read(os.path.join(OUT, model, p["file"]))
            except Exception:
                continue
            if key in self.actors:
                self.pl.remove_actor(self.actors[key], render=False)
            a = self.pl.add_mesh(mesh, color=p["color"], smooth_shading=False, show_edges=False,
                                 opacity=0.4 if self.xray and model != "printer" else 1.0, render=False)
            a.SetVisibility(p["name"] not in self.hidden and (model != "printer" or self.printer))
            self.actors[key] = a
        for key in [k for k in self.actors if k[0] == model and k not in names]:
            self.pl.remove_actor(self.actors.pop(key), render=False)
        if model == NAME:
            self.parts = [p["name"] for p in info["parts"]]
            self.status(info.get("note", ""))
        return True

    def status(self, note=""):
        lines = [f"{NAME}  {note}".strip(), ""]
        for i, n in enumerate(self.parts[:9]):
            lines.append(f"{i + 1} {'-' if n in self.hidden else '+'} {n}")
        lines += ["", "i iso   p printer   x see-through   f fit"]
        self.pl.add_text("\n".join(lines), position="upper_left", font_size=9, color="#1B1E26", name="status")

    def tick(self, *_):
        changed = self.load(NAME)
        if self.printer:
            changed = self.load("printer") or changed
        if changed:
            if self.first and any(k[0] == NAME for k in self.actors):
                self.iso()
                self.first = False
            self.pl.render()

    def iso(self):
        """The isometric view of the model from the front, right and above, z up."""
        self.pl.camera_position = [(1.0, -1.0, 0.8), (0.0, 0.0, 0.0), (0.0, 0.0, 1.0)]
        self.pl.reset_camera(bounds=self.bounds())
        self.pl.render()

    def bounds(self):
        acts = [a for (m, _), a in self.actors.items() if m == NAME and a.GetVisibility()]
        b = [min(a.GetBounds()[i] for a in acts) if i % 2 == 0 else max(a.GetBounds()[i] for a in acts) for i in range(6)] if acts else None
        return b

    def fit(self):
        self.pl.reset_camera()
        self.pl.render()

    def toggle_printer(self):
        self.printer = not self.printer
        for (m, _), a in self.actors.items():
            if m == "printer":
                a.SetVisibility(self.printer)
        self.tick()
        self.pl.render()

    def toggle_xray(self):
        self.xray = not self.xray
        for (m, _), a in self.actors.items():
            if m != "printer":
                a.GetProperty().SetOpacity(0.4 if self.xray else 1.0)
        self.pl.render()

    def toggle_part(self, i):
        if i >= len(self.parts):
            return
        n = self.parts[i]
        self.hidden ^= {n}
        a = self.actors.get((NAME, n))
        if a:
            a.SetVisibility(n not in self.hidden)
        self.status()
        self.pl.render()

    def run(self):
        self.tick()
        self.pl.show(interactive_update=True, auto_close=False)
        while not self.pl._closed:
            self.tick()
            self.pl.update(500, force_redraw=False)


if __name__ == "__main__":
    Live().run()
