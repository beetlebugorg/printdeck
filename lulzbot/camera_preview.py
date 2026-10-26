"""The view of each camera option (camera_options.py) from each camera position, on each printer: a
render from the lens, at the aspect ratio of the camera, with a caption. Each render has a 20 mm cube
(orange) at the middle of the bed and a box of the size of a Benchy (60 x 31 x 48 mm, blue) in front
of it, 0.3 of the print area from the middle (84 mm on the TAZ, 48 mm on the Mini 2). The orange
line is the edge of the print area. A contact sheet for each printer has all the renders, chosen.png
has the cameras to buy (camera_options.CHOSEN), and mini2-places.png has other places for the camera
on the Mini 2 frame (MINI_PLACES). The renders go in renders/camera-preview/, and table.csv there has
the numbers.

The scene is the model of the live window (pod.py live writes it to build/viewer): the printer, the
pod, the camera models and their brackets (camera_models.py), with the moving parts of the printer where the LulzBot model has them.
Each camera is a pinhole camera: the lens has no distortion. "Seen" is the part of the print area in
the picture and not behind a part of the printer, a camera bracket, or a test part.

Run pod.py live first (see pod.py), then:
PYOPENGL_PLATFORM=egl uv run --no-project --with numpy --with scipy --with trimesh --with pyrender
--with PyOpenGL --with pillow python camera_preview.py
"""

import csv
import json
import math
import os

os.environ.setdefault("PYOPENGL_PLATFORM", "egl")

import numpy as np
import pyrender
import trimesh
from PIL import Image, ImageDraw, ImageFont

import camera_options as CO
import render as R
from viewer import OUT as VIEWER

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "renders", "camera-preview")
PRINTERS = (("taz", "TAZ Workhorse"), ("mini2", "Mini 2"))
PLACES = (("camera", "mount", "the C270 mount"), ("camera_top", "top", "over the bed"))
WIDTH = 960                    # the width of a render
TILE = 640                     # the width of a render on the contact sheet
CAPTION_H = 146
ORANGE = (232, 118, 43)
CUBE = "#E8762B"
BENCHY = "#2F6FD0"


def font(size):
    return ImageFont.load_default(size=size)


def load(name):
    with open(os.path.join(VIEWER, f"{name}.json")) as fh:
        info = json.load(fh)
    items = []
    for p in info["parts"]:
        path = os.path.join(VIEWER, name, p["file"])
        if not os.path.exists(path):
            raise SystemExit(f"{path} is missing: run pod.py live")
        items.append((trimesh.load(path, force="mesh"), p["color"]))
    if "bed" not in info["views"]:
        raise SystemExit(f"{name}: no camera positions in build/viewer: run pod.py live again")
    return items, info["views"]


def test_parts(views):
    """The 20 mm cube at the middle of the bed and the Benchy box in front of it, on the bed."""
    bed = views["bed"]
    c, up = np.asarray(bed["center"], float), CO.unit(bed["normal"])
    back = CO.unit(views["camera_top"]["up"])
    right = np.cross(back, up)
    out = []
    for size, at, col in (((20.0, 20.0, 20.0), c, CUBE), ((31.0, 60.0, 48.0), c - 0.3 * bed["size"] * back, BENCHY)):
        m = trimesh.creation.box(size)
        T = np.eye(4)
        T[:3, 0], T[:3, 1], T[:3, 2] = right, back, up
        T[:3, 3] = at + up * (size[2] / 2 + 0.2)
        m.apply_transform(T)
        out.append((m, col))
    return out


def scene(items):
    sc = pyrender.Scene(bg_color=[0.93, 0.94, 0.96, 1.0], ambient_light=[0.35, 0.35, 0.35])
    for mesh, col in items:
        mat = pyrender.MetallicRoughnessMaterial(baseColorFactor=R.hex_rgb(col) + [1.0], metallicFactor=0.0,
                                                 roughnessFactor=0.8, doubleSided=True)
        sc.add(pyrender.Mesh.from_trimesh(mesh, material=mat, smooth=False))
    return sc


def bed_edge(bed, n=61):
    c, a, b = (np.asarray(bed[k], float) for k in ("center", "a", "b"))
    g = np.linspace(-bed["size"] / 2, bed["size"] / 2, n)
    side = [(g[0], j) for j in g] + [(i, g[-1]) for i in g] + [(g[-1], j) for j in g[::-1]] + [(i, g[0]) for i in g[::-1]]
    return np.array([c + i * a + j * b for i, j in side])


def bed_grid(bed, n=41):
    c, a, b = (np.asarray(bed[k], float) for k in ("center", "a", "b"))
    g = np.linspace(-bed["size"] / 2, bed["size"] / 2, n)
    return np.array([c + i * a + j * b for i in g for j in g])


def shot(sc, view, o, bed):
    """The render from the lens of the option o (RGB image) and the part of the print area seen (in the
    picture and not behind a part)."""
    size = (WIDTH, int(round(WIDTH / CO.aspect(o))))
    eye, f, _, u = CO.frame(view)
    pose = R.look_at(eye, eye + f * 100.0, u)
    cam = sc.add(pyrender.PerspectiveCamera(yfov=math.radians(CO.fov(o)[1]), aspectRatio=size[0] / size[1],
                                            znear=3.0, zfar=4000.0), pose=pose)
    l1 = sc.add(pyrender.DirectionalLight(intensity=2.5), pose=R.look_at(eye, eye + f * 100 - u * 40, u))
    l2 = sc.add(pyrender.DirectionalLight(intensity=1.0), pose=pose)
    rr = pyrender.OffscreenRenderer(*size)
    rgb, depth = rr.render(sc)
    rr.delete()
    for nd in (cam, l1, l2):
        sc.remove_node(nd)
    scale = size[0] / o["res"][0]
    px, z = CO.project(view, o, bed_grid(bed))
    px = px * scale
    ins = (z > 0) & (px[:, 0] >= 0) & (px[:, 0] < size[0]) & (px[:, 1] >= 0) & (px[:, 1] < size[1])
    seen = np.zeros(len(z), bool)
    for k in np.nonzero(ins)[0]:
        d = depth[int(px[k, 1]), int(px[k, 0])]
        seen[k] = d == 0 or d > z[k] - 3.0
    img = Image.fromarray(rgb)
    ep, ez = CO.project(view, o, bed_edge(bed))
    ep = ep * scale
    ImageDraw.Draw(img).line([tuple(p) for p, zz in zip(ep, ez) if zz > 0], fill=ORANGE, width=3)
    return img, float(seen.mean())


def caption(img, lines):
    out = Image.new("RGB", (img.width, img.height + CAPTION_H), (255, 255, 255))
    out.paste(img, (0, 0))
    d = ImageDraw.Draw(out)
    for i, t in enumerate(lines):
        d.text((10, img.height + 8 + 33 * i), t, fill=(0, 0, 0), font=font(23 if i == 0 else 21))
    return out


def sheet(title, sections, path, cols=4):
    """sections: (heading, [image]). cols renders to a row, each scaled to TILE wide and placed at the
    top of a cell of the same height."""
    gap, head = 16, 44
    tiles = [[im.resize((TILE, int(im.height * TILE / im.width))) for im in ims] for _, ims in sections]
    cell_h = max(t.height for ts in tiles for t in ts)
    rows = [math.ceil(len(ts) / cols) for ts in tiles]
    W = cols * TILE + (cols + 1) * gap
    H = 60 + sum(head + r * (cell_h + gap) for r in rows)
    out = Image.new("RGB", (W, H), (255, 255, 255))
    d = ImageDraw.Draw(out)
    d.text((gap, 14), title, fill=(0, 0, 0), font=font(28))
    y = 60
    for (h, _), ts, r in zip(sections, tiles, rows):
        d.text((gap, y + 8), h, fill=(0, 0, 0), font=font(22))
        y += head
        for i, t in enumerate(ts):
            out.paste(t, (gap + (i % cols) * (TILE + gap), y + (i // cols) * (cell_h + gap)))
        y += r * (cell_h + gap)
    out.save(path)
    print(f"wrote {path}")


# Other places for the camera on the Mini 2, in printer coordinates (x to the right, y to the back, z
# up; the top of the frame is z 373), each aimed at the middle of the bed. The camera on the arm of the
# C270 mount is at the front right corner, out to the right, and the right Z column hides much of the
# bed from it. The side panels of the frame, the deck at the front left, and the stock LCD at the front
# right hide the bed from the corners and the sides, and the tool head hides it from over the right Z
# column (checked with renders), so the places are at the front.
MINI_PLACES = [
    ("front, 57 mm over the middle of the top front rail", (-200.0, -300.0, 430.0)),
    ("front, under the top front rail, 30 mm in front of it", (-200.0, -320.0, 335.0)),
    ("front, 57 mm over the top front rail, 80 mm left of the middle", (-280.0, -300.0, 430.0)),
]


def render_one(name, title, sc, view, o, bed, where, path):
    """The render of option o from view, with its caption: (image, row of table.csv)."""
    fp = CO.footprint(view, o, bed)
    img, seen = shot(sc, view, o, bed)
    h, v = CO.fov(o)
    lines = [f"{title}, {where}: {o['model']}, {o['lens']}",
             f"{h:.0f} x {v:.0f} deg, {o['res'][0]} x {o['res'][1]} px. Pinhole model: no lens distortion.",
             f"Bed center: {fp['px_mm']:.1f} px/mm across ({fp['width']:.0f} mm wide picture), "
             f"{fp['px_mm_depth']:.1f} px/mm along the bed.",
             f"Print area: {fp['in_frame'] * 100:.0f}% in the picture, {seen * 100:.0f}% seen past the parts."]
    img = caption(img, lines)
    img.save(path)
    print(f"{name} {where[:40]:40s} {o['key']:8s} {fp['width']:5.0f} mm wide  {fp['px_mm']:5.1f} px/mm across  "
          f"{fp['px_mm_depth']:5.1f} along  in the picture {fp['in_frame'] * 100:3.0f}%  seen {seen * 100:3.0f}%")
    return img, dict(printer=name, position=where, option=o["key"], model=o["model"], lens=o["lens"],
                     fov_h=round(h, 1), fov_v=round(v, 1), res=f"{o['res'][0]}x{o['res'][1]}",
                     width_mm=round(fp["width"]), px_mm=round(fp["px_mm"], 1), px_mm_depth=round(fp["px_mm_depth"], 1),
                     in_frame=round(fp["in_frame"] * 100), seen=round(seen * 100))


def pod_closeup(name, title, items, path):
    """A view of the plugs and the cables at the top of the pod (the USB ports of the Pi), in the
    coordinates of the live window (the pod frame: x across the glass, z up, the glass toward -y)."""
    sc = scene(items)
    target = np.array([10.0, 22.0, 135.0])
    eye = target + np.array([150.0, -170.0, 120.0])
    size = (1200, 900)
    pose = R.look_at(eye, target, (0.0, 0.0, 1.0))
    sc.add(pyrender.PerspectiveCamera(yfov=math.radians(32.0), aspectRatio=size[0] / size[1], znear=5.0, zfar=3000.0),
           pose=pose)
    sc.add(pyrender.DirectionalLight(intensity=2.5), pose=pose)
    rr = pyrender.OffscreenRenderer(*size)
    rgb, _ = rr.render(sc)
    rr.delete()
    img = caption(Image.fromarray(rgb), [f"{title}: the plugs and the cables at the Pi",
                                         "Dark red: the camera on the arm (upper USB 2 port). Dark blue: the camera "
                                         "over the bed (lower USB 3 port).",
                                         "Black: the printer cable (lower USB 2 port). White: the power cable.", ""])
    img.save(path)
    print(f"wrote {path}")


def aimed(views, lens):
    """A view from lens (printer coordinates) at the middle of the bed, level."""
    T = np.array(views["printer_to_view"])
    bed = views["bed"]
    eye = (T @ np.append(np.asarray(lens, float), 1.0))[:3]
    return {"eye": list(eye), "focal": bed["center"], "up": bed["normal"]}


def main():
    os.makedirs(OUT, exist_ok=True)
    rows, chosen = [], []
    for name, title in PRINTERS:
        items, views = load(name)
        pod_closeup(name, title, items, os.path.join(OUT, f"{name}-cables-at-pi.png"))
        sc = scene(items + test_parts(views))
        bed = views["bed"]
        sections = []
        for vk, tag, where in PLACES:
            view = views[vk]
            ims = []
            for o in CO.CAMERAS:
                img, row = render_one(name, title, sc, view, o, bed, where, os.path.join(OUT, f"{name}-{tag}-{o['key']}.png"))
                ims.append(img)
                rows.append(row)
                if o["key"] == CO.CHOSEN[vk]:
                    chosen.append(img)
            dist = float(np.linalg.norm(np.asarray(view["eye"]) - np.asarray(bed["center"])))
            sections.append((f"{where}: lens {dist:.0f} mm from the middle of the bed", ims))
        sheet(f"{title}: the view of each camera and lens (pinhole model, no lens distortion)", sections,
              os.path.join(OUT, f"{name}-sheet.png"))
        if name == "mini2":
            o = CO.CAMERAS[CO.index(CO.CHOSEN["camera"])]
            ims = [chosen[-2]]
            for i, (where, lens) in enumerate(MINI_PLACES):
                img, row = render_one(name, title, sc, aimed(views, lens), o, bed, where,
                                      os.path.join(OUT, f"mini2-place-{i + 1}.png"))
                ims.append(img)
                rows.append(row)
            sheet("Mini 2: other places for the camera on the frame, aimed at the middle of the bed "
                  f"({o['model']})", [("the C270 mount (first), then the other places", ims)],
                  os.path.join(OUT, "mini2-places.png"))
    sheet("The cameras to buy: the Arducam B0561 on the arm of the C270 mount and over the bed",
          [("TAZ Workhorse", chosen[:2]), ("Mini 2", chosen[2:])], os.path.join(OUT, "chosen.png"), cols=2)
    with open(os.path.join(OUT, "table.csv"), "w", newline="") as fh:
        w = csv.DictWriter(fh, fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)


if __name__ == "__main__":
    main()
