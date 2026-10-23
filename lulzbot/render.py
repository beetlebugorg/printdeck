"""Printdeck LulzBot: the PNG renders. Headless OpenGL (pyrender with EGL), an orthographic
view, flat colours with soft light, and dark edge lines from the jumps in depth and normal.
Run: PYOPENGL_PLATFORM=egl uv run --no-project --with pyrender --with trimesh --with pillow
     --with numpy --with scipy python render.py   (make.sh runs it)
"""

import json
import os

os.environ.setdefault("PYOPENGL_PLATFORM", "egl")

import numpy as np
import pyrender
import trimesh
from PIL import Image
from scipy import ndimage

HERE = os.path.dirname(os.path.abspath(__file__))
BUILD = os.path.join(HERE, "build")
OUT = os.path.join(HERE, "renders")


def hex_rgb(h):
    h = h.lstrip("#")
    return [int(h[i:i + 2], 16) / 255 for i in (0, 2, 4)]


def look_at(eye, target, up):
    eye, target, up = (np.asarray(v, float) for v in (eye, target, up))
    f = target - eye
    f /= np.linalg.norm(f)
    r = np.cross(f, up)
    r /= np.linalg.norm(r)
    u = np.cross(r, f)
    m = np.eye(4)
    m[:3, 0], m[:3, 1], m[:3, 2], m[:3, 3] = r, u, -f, eye
    return m


def render(items, view_dir, up, path, size=(1600, 1200), margin=1.08, edges=True, light=None):
    """items: list of (trimesh, '#rrggbb'). view_dir: the direction the camera looks in."""
    scene = pyrender.Scene(bg_color=[1, 1, 1, 0], ambient_light=[0.30, 0.30, 0.30])
    allv = []
    for mesh, col in items:
        mat = pyrender.MetallicRoughnessMaterial(baseColorFactor=hex_rgb(col) + [1.0], metallicFactor=0.0,
                                                 roughnessFactor=0.8, doubleSided=True)
        scene.add(pyrender.Mesh.from_trimesh(mesh, material=mat, smooth=False))
        allv.append(mesh.bounds)
    b = np.array(allv)
    lo, hi = b[:, 0].min(axis=0), b[:, 1].max(axis=0)
    centre = (lo + hi) / 2
    d = np.asarray(view_dir, float)
    d /= np.linalg.norm(d)
    pose = look_at(centre - d * 3000, centre, up)
    # The size of the scene in the image plane.
    corners = np.array([[x, y, z] for x in (lo[0], hi[0]) for y in (lo[1], hi[1]) for z in (lo[2], hi[2])])
    inv = np.linalg.inv(pose)
    pc = (inv[:3, :3] @ corners.T).T + inv[:3, 3]
    w = (pc[:, 0].max() - pc[:, 0].min()) / 2 * margin
    h = (pc[:, 1].max() - pc[:, 1].min()) / 2 * margin
    aspect = size[0] / size[1]
    xmag = max(w, h * aspect)
    cam = pyrender.OrthographicCamera(xmag=xmag, ymag=xmag / aspect, znear=1.0, zfar=8000)
    scene.add(cam, pose=pose)
    key = look_at(centre - np.asarray(light if light is not None else d + np.array(up) * -0.6 + np.cross(d, up) * 0.5) * 3000,
                  centre, up)
    scene.add(pyrender.DirectionalLight(intensity=2.0), pose=key)
    scene.add(pyrender.DirectionalLight(intensity=0.7), pose=pose)
    r = pyrender.OffscreenRenderer(*size)
    rgba, depth = r.render(scene, flags=pyrender.RenderFlags.RGBA)
    r.delete()
    # pyrender applies the perspective formula to the depth buffer. Undo it, and convert the
    # orthographic depth buffer to a distance in mm.
    zn, zf = 1.0, 8000.0
    with np.errstate(divide="ignore", invalid="ignore"):
        ndc = (zf + zn - 2 * zn * zf / depth) / (zf - zn)
    depth = np.where(depth > 0, (ndc + 1) / 2 * (zf - zn) + zn, 0.0)
    img = rgba.astype(float) / 255
    if edges:
        mask = depth > 0
        dz = np.where(mask, depth, depth[mask].max() + 50 if mask.any() else 0)
        px = 2 * xmag / size[0]          # mm per pixel
        jump = np.abs(ndimage.laplace(dz))     # zero on a flat or sloped face, large at a step
        # Depth jumps (silhouettes and steps) and colour jumps (edges between faces).
        lum = img[..., :3].mean(axis=2)
        cj = np.hypot(ndimage.sobel(lum, 1), ndimage.sobel(lum, 0))
        line = (jump > max(1.5, 3.0 * px)) | (cj > 0.35)
        line |= mask ^ ndimage.binary_erosion(mask)
        line = ndimage.binary_dilation(line, iterations=1) & ndimage.binary_dilation(mask, iterations=2)
        img[line, :3] *= 0.25
        img[line, 3] = 1.0
    bg = np.ones_like(img[..., :3])
    out = img[..., :3] * img[..., 3:4] + bg * (1 - img[..., 3:4])
    os.makedirs(os.path.dirname(path), exist_ok=True)
    Image.fromarray((out * 255).clip(0, 255).astype(np.uint8)).save(path)
    print("  wrote", os.path.relpath(path, HERE))


def load(path):
    m = trimesh.load(path, force="mesh")
    return m


PRINTER_COLOURS = {
    "cb_chassis": "#2B2B2D", "cb_cover": "#323235", "final_frame_01": "#45474B", "y_bed_01": "#8E959C",
    "x_axis_01": "#55585D", "cb_lcd": "#2E5E3A", "cb_lcd_bezel": "#1A1A1A", "cb_lcd_cover": "#3B4A55",
}


def printer_items(skip=()):
    out = []
    wdir = os.path.join(BUILD, "workhorse")
    for f in sorted(os.listdir(wdir)):
        n = f[:-4]
        if not f.endswith(".stl") or n in ("control_box_01", "cb_cover") or n in skip:
            continue
        out.append((load(os.path.join(wdir, f)), PRINTER_COLOURS.get(n, "#3A3B3E")))
    # The cover of the printer (PP-FP0164), from model.py.
    out.append((load(os.path.join(BUILD, "mesh", "printer_cover_full.stl")), PRINTER_COLOURS["cb_cover"]))
    return out


def case_items(manifest, colours, groups=None, skip_names=(), offsets=None):
    out = []
    for p in manifest["parts"]:
        if groups is not None and p["group"] not in groups and p["name"] not in groups:
            continue
        if p["name"] in skip_names or p["group"] == "printer":
            continue
        m = load(os.path.join(BUILD, "mesh", p["file"]))
        if offsets:
            off = next((o for k, o in offsets.items() if p["name"].startswith(k) or p["group"] == k), None)
            if off is not None:
                m = m.copy()
                m.apply_translation(off)
        out.append((m, colours.get(p["material"], "#888888")))
    return out


def logo_mark():
    """The TAZ Workhorse logo area on the front of the chassis (silk screen, PP-FP0172 sheet 7:
    264 to 306 mm above the bottom of the chassis). For the renders only."""
    m = trimesh.creation.box((60.0, 42.0, 0.2))
    m.apply_translation((460.0, -625.235 + 285.0, -9.1))
    return m


def main():
    import model as M
    colours = M.COLOURS
    with open(os.path.join(BUILD, "manifest.json")) as fh:
        manifest = json.load(fh)
    allcase = case_items(manifest, colours)
    printer = printer_items() + [(logo_mark(), "#C8CCD0")]
    # The printer: from the front left, a little above. The printer front is -Z, the left is +X.
    render(printer + allcase, (-0.62, -0.38, 0.75), (0, 1, 0), os.path.join(OUT, "printer-front-left.png"),
           size=(1800, 1500))
    render(printer + allcase, (0, -0.05, 1), (0, 1, 0), os.path.join(OUT, "printer-front.png"), size=(1800, 1400))
    # A close view of the front left corner of the chassis.
    near = [(load(os.path.join(BUILD, "workhorse", f"cb_{n}.stl")), PRINTER_COLOURS.get(f"cb_{n}", "#3A3B3E"))
            for n in ("chassis", "lcd", "lcd_bezel", "lcd_cover", "lcd_knob", "power_switch")]
    near += [(load(os.path.join(BUILD, "mesh", "printer_cover_full.stl")), PRINTER_COLOURS["cb_cover"])]
    near += [(logo_mark(), "#C8CCD0")]
    render(near + allcase, (-0.55, -0.3, 0.8), (0, 1, 0), os.path.join(OUT, "mount-front-left.png"),
           size=(1600, 1500), margin=1.04)
    render(allcase, (0, 0, 1), (0, 1, 0), os.path.join(OUT, "mount-front.png"), size=(1700, 1100))
    render(allcase, (-1, 0, 0), (0, 1, 0), os.path.join(OUT, "mount-left.png"), size=(1500, 1400))
    # Inside: the head, the back panel, the pod, and the grille are off. The cables show.
    inside = case_items(manifest, colours, skip_names=("head", "back", "pod", "grille", "hw_display_glass",
                                                       "hw_display_lcd"))
    render(inside, (-0.55, -0.3, 0.8), (0, 1, 0), os.path.join(OUT, "mount-inside.png"), size=(1600, 1400))
    render(inside, (-1, 0, 0), (0, 1, 0), os.path.join(OUT, "mount-inside-left.png"), size=(1500, 1400))
    # Exploded: the head with the display forward (-Z), the pod and the grille out (+X).
    off = {"head": (0, 0, -90), "display": (0, 0, -60), "head_screws": (0, 0, 25), "back": (0, 0, -25),
           "pod": (70, 0, 0), "grille": (110, 0, 0), "fan": (110, 0, 0), "bottom_screws": (70, -30, 0)}
    exploded = case_items(manifest, colours, offsets=off)
    render([near[0], near[-2]] + exploded, (-0.62, -0.3, 0.75), (0, 1, 0), os.path.join(OUT, "exploded.png"), size=(1800, 1500))
    # Each printed part in its print orientation and colour.
    for name, (mat, _, stl) in M.PRINTED.items():
        if not stl:
            continue
        m = load(os.path.join(BUILD, "mesh", "print_" + stl + ".stl"))
        render([(m, colours[mat])], (-0.55, 0.75, -0.6), (0, 0, 1), os.path.join(OUT, f"part-{stl}.png"),
               size=(1400, 1100))


if __name__ == "__main__":
    main()
