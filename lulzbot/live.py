"""The live model window: it shows a model of build/viewer (see viewer.py) and loads it again each
time a model script publishes a new version. The camera stays where it is.

    uv run --no-project --with pyvista --with trimesh python lulzbot/live.py [NAME]

The model is in the frame of the pod, z up, the front toward -y. The camera turns about the
vertical, so the model stays upright: drag to turn, the wheel zooms, shift and drag pans.

Keys: e explodes the pod in the order it comes apart (e again puts it together), n the next model
(the TAZ, the Mini 2), i the isometric view from the front right, k the view of the camera (from its
lens, with its field of view), l the next camera and lens (camera_options.py), t the next camera
position (the webcam mount, or over the bed), p shows or hides the printer, c shows or hides the
webcam, its mount, and the view of the camera on the bed, x makes the parts see-through, f fits the
view, 1 to 9 hide or show the parts in the list.

In the view of the camera, the yellow frame is the edge of the picture, and the text at the bottom
left gives the camera, the lens, the field of view, and the pixels per mm at the middle of the bed.
Outside the camera view, yellow lines show the view of the camera on the bed.

The window runs on the event loop of VTK: a timer checks for a new version and moves the parts of
the exploded view, so the window answers at all times.
"""

import glob
import json
import os
import sys
import time

import numpy as np
import pyvista as pv
from vtkmodules.vtkCommonCore import vtkPoints
from vtkmodules.vtkCommonDataModel import vtkCellArray, vtkPolyData
from vtkmodules.vtkRenderingCore import vtkActor2D, vtkCoordinate, vtkPolyDataMapper2D

import camera_options as CO
from viewer import OUT

TICK_MS = 40              # the timer: 25 frames a second while the parts move
CHECK_S = 0.5             # how often to look for a new version
EXPLODE_S = 1.2           # the time of one stage of the exploded view
TEXT = "#E6E8EE"
FRUSTUM = "#FFD23F"
PLACES = (("camera", "the webcam mount"), ("camera_top", "over the bed"))


def models():
    return sorted(os.path.basename(f)[:-5] for f in glob.glob(os.path.join(OUT, "*.json")))


class Live:
    def __init__(self, name):
        self.name = name
        self.pl = pv.Plotter(title="Printdeck model", window_size=(1400, 1000))
        self.pl.set_background("#2A2E35", top="#454B55")
        self.pl.enable_anti_aliasing("ssaa")
        self.pl.add_axes(color=TEXT)
        self.pl.enable_terrain_style(mouse_wheel_zooms=True, shift_pans=True)
        self.stamp = None
        self.actors = {}          # part -> actor
        self.info = {}            # part -> its entry in the json
        self.parts = []           # the parts of the list (not the printer)
        self.stages = []
        self.hidden = set()
        self.xray = False
        self.printer = True
        self.camera = True
        self.views = {}
        self.note = ""
        self.need_view = True
        self.place_i = 0          # the camera position (PLACES)
        self.option = 0           # the camera and lens (camera_options.CAMERAS)
        self.in_camera = False    # the window shows the view of the camera
        self.frame2d = None       # the edge of the picture in the camera view
        self.t = 0.0              # the exploded view: 0 assembled, len(stages) apart
        self.goal = 0.0
        self.last_check = 0.0
        self.last_tick = time.time()
        keys = (("e", self.toggle_explode), ("n", self.next_model), ("i", self.iso), ("p", self.toggle_printer),
                ("x", self.toggle_xray), ("f", self.fit), ("c", self.toggle_camera), ("k", self.camera_view),
                ("l", self.next_option), ("t", self.next_place))
        for k, f in keys:
            self.pl.add_key_event(k, f)
        for i in range(1, 10):
            self.pl.add_key_event(str(i), lambda i=i: self.toggle_part(i - 1))

    # ------------------------------------------------------------ loading
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
            self.actors[n] = self.pl.add_mesh(mesh, color=p["color"], opacity=0.4 if see else 1.0, render=False)
            self.info[n] = p
        for n in [k for k in self.actors if k not in names]:
            self.pl.remove_actor(self.actors.pop(n), render=False)
            self.info.pop(n, None)
        self.parts = [p["name"] for p in info["parts"] if not p["name"].startswith("printer")]
        self.stages = info.get("stages", [])
        self.views = info.get("views", {})
        self.note = info.get("note", "")
        self.place()
        self.status()
        self.lens_overlay()
        return True

    # ------------------------------------------------------------ the exploded view
    def offset(self, n):
        steps = self.info.get(n, {}).get("explode")
        if not steps:
            return (0.0, 0.0, 0.0)
        off = np.zeros(3)
        for j, d in enumerate(steps):
            off += np.array(d) * min(1.0, max(0.0, self.t - j))
        return tuple(off)

    def visible(self, n):
        if n.startswith("printer"):
            return self.printer
        if n.startswith("camera") and not self.camera:
            return False
        if self.t > 0.01 and self.info.get(n, {}).get("hide_exploded"):
            return False
        after = self.info.get(n, {}).get("hide_after")
        if after is not None and self.t > after + 0.01:
            return False
        return n not in self.hidden

    def place(self):
        for n, a in self.actors.items():
            a.SetPosition(*self.offset(n))
            a.SetVisibility(self.visible(n))

    def toggle_explode(self):
        self.goal = float(len(self.stages)) if self.goal == 0 else 0.0
        self.status()

    # ------------------------------------------------------------ the timer
    def tick(self, *_):
        now = time.time()
        dt, self.last_tick = now - self.last_tick, now
        changed = False
        if now - self.last_check > CHECK_S:
            self.last_check = now
            if self.load():
                changed = True
                if self.need_view and self.actors:
                    self.iso(render=False)
                    self.need_view = False
        if self.t != self.goal:
            step = dt / EXPLODE_S
            self.t = min(self.goal, self.t + step) if self.goal > self.t else max(self.goal, self.t - step)
            self.place()
            self.status()
            changed = True
        if changed:
            self.pl.render()

    # ------------------------------------------------------------ the text, the views, the keys
    def status(self):
        lines = [f"{self.name}   {self.note}".strip()]
        if self.stages:
            k = min(int(self.t), len(self.stages) - 1)
            lines.append("assembled" if self.t <= 0 else f"stage {k + 1} of {len(self.stages)}: {self.stages[k]}")
        lines.append("")
        for i, n in enumerate(self.parts[:9]):
            lines.append(f"{i + 1} {'-' if n in self.hidden else '+'} {n}")
        lines += ["", "e explode   n next model   i iso   k camera view", "l next lens   t next camera position",
                  "p printer   c webcam   x see-through   f fit"]
        self.pl.add_text("\n".join(lines), position="upper_left", font_size=9, color=TEXT, name="status")

    def pod_bounds(self):
        acts = [a for n, a in self.actors.items()
                if not n.startswith("printer") and "cable" not in n and "clips" not in n and a.GetVisibility()]
        if not acts:
            return None
        bs = [a.GetBounds() for a in acts]
        return [min(b[i] for b in bs) if i % 2 == 0 else max(b[i] for b in bs) for i in range(6)]

    # ------------------------------------------------------------ the camera and its lens
    def lens(self):
        """(the view of the camera position, the camera option, its footprint on the bed), or None."""
        v = self.views.get(PLACES[self.place_i][0])
        bed = self.views.get("bed")
        if not v or not bed:
            return None
        o = CO.CAMERAS[self.option]
        return v, o, CO.footprint(v, o, bed)

    def lens_overlay(self):
        """The text of the camera option, and its view on the bed: lines from the lens to the corners of
        the picture on the bed plane, and the outline there."""
        self.pl.remove_actor("lens_text", render=False)
        self.pl.remove_actor("lens_frustum", render=False)
        got = self.lens()
        if not got:
            if self.views:
                self.pl.add_text("no camera positions in this model: run pod.py live again", position="lower_left",
                                 font_size=10, color=FRUSTUM, name="lens_text")
            return
        v, o, fp = got
        lines = [f"camera position: {PLACES[self.place_i][1]}   (option {self.option + 1} of {len(CO.CAMERAS)})",
                 CO.label(o),
                 f"bed center: {fp['px_mm']:.1f} px/mm across ({fp['width']:.0f} mm wide picture), "
                 f"{fp['px_mm_depth']:.1f} px/mm along the bed",
                 f"print area in the picture: {fp['in_frame'] * 100:.0f}%   (pinhole model, no lens distortion)"]
        self.pl.add_text("\n".join(lines), position="lower_left", font_size=10, color=FRUSTUM, name="lens_text")
        if not self.camera:
            return
        eye = np.asarray(v["eye"], float)
        far = 4.0 * self.views["bed"]["size"]      # a corner past the horizon, or very far: a line this long
        _, f, r, u = CO.frame(v)
        th, tv = (np.tan(np.radians(a / 2)) for a in CO.fov(o))
        pts = [eye]
        for (sx, sy), c in zip(((-1, 1), (1, 1), (1, -1), (-1, -1)), fp["corners"]):
            if c is None or np.linalg.norm(c - eye) > far:
                ray = f + sx * th * r + sy * tv * u
                c = eye + ray / np.linalg.norm(ray) * far
            pts.append(c)
        cells = [2, 0, 1, 2, 0, 2, 2, 0, 3, 2, 0, 4, 5, 1, 2, 3, 4, 1]
        mesh = pv.PolyData(np.array(pts), lines=cells)
        self.pl.add_mesh(mesh, color=FRUSTUM, line_width=2, name="lens_frustum", render=False)

    def frame_2d(self, half=None):
        """The edge of the picture in the camera view: half is (across, up) in parts of the window, or
        None to remove it."""
        if self.frame2d is not None:
            self.pl.renderer.RemoveViewProp(self.frame2d)
            self.frame2d = None
        if half is None:
            return
        hx, hy = half
        p = vtkPoints()
        for x, y in ((-hx, -hy), (hx, -hy), (hx, hy), (-hx, hy)):
            p.InsertNextPoint(0.5 + x, 0.5 + y, 0.0)
        cells = vtkCellArray()
        cells.InsertNextCell(5)
        for i in (0, 1, 2, 3, 0):
            cells.InsertCellPoint(i)
        poly = vtkPolyData()
        poly.SetPoints(p)
        poly.SetLines(cells)
        coord = vtkCoordinate()
        coord.SetCoordinateSystemToNormalizedViewport()
        mapper = vtkPolyDataMapper2D()
        mapper.SetInputData(poly)
        mapper.SetTransformCoordinate(coord)
        a = vtkActor2D()
        a.SetMapper(mapper)
        a.GetProperty().SetColor(*[int(FRUSTUM[i:i + 2], 16) / 255 for i in (1, 3, 5)])
        a.GetProperty().SetLineWidth(2)
        self.pl.renderer.AddViewProp(a)
        self.frame2d = a

    def camera_view(self):
        """The view of the camera: from its lens, with the field of view of the camera option. The
        window shows a little more than the picture, and the yellow frame is the edge of the picture."""
        got = self.lens()
        if not got:
            return
        v, o, _ = got
        th, tv = (np.tan(np.radians(a / 2)) for a in CO.fov(o))
        w, h = self.pl.window_size
        wa = w / h
        tw = max(tv, th / wa) * 1.04
        cam = self.pl.camera
        cam.position, cam.focal_point, cam.up = v["eye"], v["focal"], v["up"]
        cam.view_angle = float(2 * np.degrees(np.arctan(tw)))
        self.pl.reset_camera_clipping_range()
        self.in_camera = True
        self.frame_2d((th / (tw * wa) / 2, tv / tw / 2))
        self.pl.render()

    def leave_camera(self, *_):
        if self.in_camera:
            self.in_camera = False
            self.frame_2d(None)

    def next_option(self):
        self.option = (self.option + 1) % len(CO.CAMERAS)
        self.lens_changed()

    def next_place(self):
        self.place_i = (self.place_i + 1) % len(PLACES)
        self.lens_changed()

    def lens_changed(self):
        self.lens_overlay()
        if self.in_camera:
            self.camera_view()
        else:
            self.pl.render()

    def toggle_camera(self):
        self.camera = not self.camera
        self.place()
        self.lens_overlay()
        self.pl.render()

    def iso(self, render=True):
        """The isometric view of the pod from the front, right and above, z up."""
        self.leave_camera()
        self.pl.camera_position = [(1.0, -1.0, 0.8), (0.0, 0.0, 0.0), (0.0, 0.0, 1.0)]
        self.pl.reset_camera(bounds=self.pod_bounds())
        if render:
            self.pl.render()

    def fit(self):
        self.leave_camera()
        self.pl.reset_camera()
        self.pl.render()

    def next_model(self):
        ms = models()
        if not ms:
            return
        self.name = ms[(ms.index(self.name) + 1) % len(ms)] if self.name in ms else ms[0]
        for a in self.actors.values():
            self.pl.remove_actor(a, render=False)
        self.actors, self.info, self.stamp, self.need_view = {}, {}, None, True
        self.leave_camera()
        self.last_check = 0.0

    def toggle_printer(self):
        self.printer = not self.printer
        self.place()
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
        self.place()
        self.status()
        self.pl.render()

    def run(self):
        self.load()
        self.pl.show(auto_close=False, interactive_update=True)
        self.iso()
        iren = self.pl.iren
        # VTK's own keys (e and q quit, w wireframe, f fly to, p pick, ...) come with the char event of
        # the interactor. Our keys come with the key press event, so drop the char event.
        iren.interactor.RemoveObservers("CharEvent")
        # A drag or the wheel moves the view away from the camera: remove the edge of the picture.
        for ev in ("LeftButtonPressEvent", "MiddleButtonPressEvent", "RightButtonPressEvent",
                   "MouseWheelForwardEvent", "MouseWheelBackwardEvent"):
            iren.add_observer(ev, self.leave_camera)
        iren.add_observer("TimerEvent", self.tick)
        iren.create_timer(TICK_MS, repeating=True)
        iren.start()


if __name__ == "__main__":
    Live(sys.argv[1] if len(sys.argv) > 1 else "taz").run()
