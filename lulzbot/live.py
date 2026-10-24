"""The live model window: it shows a model of build/viewer (see viewer.py) and loads it again each
time a model script publishes a new version. The camera stays where it is.

    uv run --no-project --with pyvista --with trimesh python lulzbot/live.py [NAME]

The model is in the frame of the pod, z up, the front toward -y. The camera turns about the
vertical, so the model stays upright: drag to turn, the wheel zooms, shift and drag pans.

Keys: n the next model (the TAZ, the Mini 2), i the isometric view from the front right, p shows
or hides the printer, x makes the parts see-through, f fits the view, 1 to 9 hide or show the parts
in the list.
"""

import glob
import json
import os
import sys

import pyvista as pv

from viewer import OUT


def models():
    return sorted(os.path.basename(f)[:-5] for f in glob.glob(os.path.join(OUT, "*.json")))


class Live:
    def __init__(self, name):
        self.name = name
        self.pl = pv.Plotter(title="Printdeck model", window_size=(1400, 1000))
        self.pl.set_background("#E9EBEF", top="#FFFFFF")
        self.pl.enable_anti_aliasing("ssaa")
        self.pl.add_axes()
        self.pl.enable_terrain_style(mouse_wheel_zooms=True, shift_pans=True)
        self.stamp = None
        self.actors = {}         # part -> actor
        self.parts = []          # the parts of the list (not the printer)
        self.hidden = set()
        self.xray = False
        self.printer = True
        self.note = ""
        self.need_view = True
        for k, f in (("n", self.next_model), ("i", self.iso), ("p", self.toggle_printer), ("x", self.toggle_xray), ("f", self.fit)):
            self.pl.add_key_event(k, f)
        for i in range(1, 10):
            self.pl.add_key_event(str(i), lambda i=i: self.toggle_part(i - 1))

    def visible(self, part):
        if part.startswith("printer"):
            return self.printer
        return part not in self.hidden

    def load(self):
        try:
            with open(os.path.join(OUT, f"{self.name}.json")) as fh:
                info = json.load(fh)
        except (OSError, ValueError):
            return False
        if self.stamp == info["t"]:
            return False
        self.stamp = info["t"]
        names = set()
        for p in info["parts"]:
            n = p["name"]
            names.add(n)
            try:
                mesh = pv.read(os.path.join(OUT, self.name, p["file"]))
            except Exception:
                continue
            if n in self.actors:
                self.pl.remove_actor(self.actors[n], render=False)
            see = self.xray and not n.startswith("printer")
            a = self.pl.add_mesh(mesh, color=p["color"], opacity=0.4 if see else 1.0, render=False)
            a.SetVisibility(self.visible(n))
            self.actors[n] = a
        for n in [k for k in self.actors if k not in names]:
            self.pl.remove_actor(self.actors.pop(n), render=False)
        self.parts = [p["name"] for p in info["parts"] if not p["name"].startswith("printer")]
        self.note = info.get("note", "")
        self.status()
        return True

    def status(self):
        lines = [f"{self.name}   {self.note}".strip(), ""]
        for i, n in enumerate(self.parts[:9]):
            lines.append(f"{i + 1} {'-' if n in self.hidden else '+'} {n}")
        lines += ["", "n next model   i iso   p printer", "x see-through   f fit"]
        self.pl.add_text("\n".join(lines), position="upper_left", font_size=9, color="#1B1E26", name="status")

    def tick(self):
        if self.load():
            if self.need_view and self.actors:
                self.iso()
                self.need_view = False
            self.pl.render()

    def pod_bounds(self):
        acts = [a for n, a in self.actors.items() if not n.startswith("printer") and n != "bracket" and a.GetVisibility()]
        if not acts:
            return None
        bs = [a.GetBounds() for a in acts]
        return [min(b[i] for b in bs) if i % 2 == 0 else max(b[i] for b in bs) for i in range(6)]

    def iso(self):
        """The isometric view of the pod from the front, right and above, z up."""
        self.pl.camera_position = [(1.0, -1.0, 0.8), (0.0, 0.0, 0.0), (0.0, 0.0, 1.0)]
        self.pl.reset_camera(bounds=self.pod_bounds())
        self.pl.render()

    def fit(self):
        self.pl.reset_camera()
        self.pl.render()

    def next_model(self):
        ms = models()
        if not ms:
            return
        self.name = ms[(ms.index(self.name) + 1) % len(ms)] if self.name in ms else ms[0]
        for a in self.actors.values():
            self.pl.remove_actor(a, render=False)
        self.actors, self.stamp, self.need_view = {}, None, True
        self.tick()

    def toggle_printer(self):
        self.printer = not self.printer
        for n, a in self.actors.items():
            if n.startswith("printer"):
                a.SetVisibility(self.printer)
        self.pl.render()

    def toggle_xray(self):
        self.xray = not self.xray
        for n, a in self.actors.items():
            if not n.startswith("printer"):
                a.GetProperty().SetOpacity(0.4 if self.xray else 1.0)
        self.pl.render()

    def toggle_part(self, i):
        if i >= len(self.parts):
            return
        n = self.parts[i]
        self.hidden ^= {n}
        if n in self.actors:
            self.actors[n].SetVisibility(self.visible(n))
        self.status()
        self.pl.render()

    def run(self):
        self.tick()
        self.pl.show(interactive_update=True, auto_close=False)
        while not self.pl._closed:
            self.tick()
            self.pl.update(500, force_redraw=False)


if __name__ == "__main__":
    Live(sys.argv[1] if len(sys.argv) > 1 else "taz").run()
