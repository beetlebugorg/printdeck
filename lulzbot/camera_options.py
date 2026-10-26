"""Camera and lens options for the camera of the printer, and the math of their views. live.py (keys k,
l, and t) and camera_preview.py use it. It needs only numpy, so the live window can import it.

Each option is a pinhole camera: the lens has no distortion. A real wide lens (2.7 mm) bends the
lines near the edges, and the edges of its picture show more of the bed than a pinhole camera.

FOV = 2 atan(size / 2 / f) for the sensor size across and up. When an option has no sensor size, the
FOV is the figure of the vendor.
"""

import math

import numpy as np

# The sensors: the active area (mm across, up) and the pixels.
IMX577 = dict(sensor="Sony IMX577, 1/2.3 in", size=(6.29, 4.71), res=(4056, 3040))
# 3840 x 2160 pixels of 2.0 um.
IMX678 = dict(sensor="Sony IMX678, 1/1.8 in", size=(3840 * 0.002, 2160 * 0.002), res=(3840, 2160))


def diag_to_fov(diag, aspect):
    """(across, up) FOV in degrees from the diagonal FOV and the aspect ratio of the picture."""
    t = math.tan(math.radians(diag / 2)) / math.hypot(aspect, 1.0)
    return 2 * math.degrees(math.atan(t * aspect)), 2 * math.degrees(math.atan(t))


# key, the name of the camera, the lens, and the data. The C270 FOV is the same as camera.FOV_DIAG
# (55 degrees diagonal) at camera.ASPECT (16:9); camera_preview.py checks this.
CAMERAS = [
    dict(key="c270", model="Logitech C270", lens="stock lens", sensor="1280 x 720 webcam", res=(1280, 720),
         fov=diag_to_fov(55.0, 16 / 9), note="the current webcam: 55 deg diagonal (Logitech)"),
    dict(key="elp-2.7", model="ELP-USB12MP01-L281", lens="2.7 mm M12 (LN069)", f=2.7, **IMX577),
    dict(key="elp-4", model="ELP-USB12MP01-L281", lens="4 mm M12 low distortion (LN011)", f=4.0, **IMX577),
    dict(key="elp-6", model="ELP-USB12MP01-L281", lens="6 mm M12 (LN024)", f=6.0, **IMX577),
    dict(key="elp-8", model="ELP-USB12MP01-L281", lens="8 mm M12 (LN002)", f=8.0, **IMX577),
    dict(key="b0561", model="Arducam B0561 16 MP autofocus", lens="3.95 mm fixed", sensor="1/2.8 in",
         res=(4656, 3496), f=3.95, fov=(65.0, 52.0), note="FOV from the vendor"),
    dict(key="b0587", model="Arducam B0587", lens="4.9 mm", f=4.9, note="the vendor gives 88 x 72 deg: at 16:9 a "
         "72 deg height needs a 7.1 mm high sensor, and this one is 4.32 mm", **IMX678),
]


# The cameras to buy: the default option of each camera position in live.py (keys l and t).
CHOSEN = {"camera": "b0561", "camera_top": "b0561"}


def index(key):
    return next(i for i, o in enumerate(CAMERAS) if o["key"] == key)


def fov(o):
    """(across, up) FOV of an option in degrees."""
    if "size" in o and "fov" not in o:
        return tuple(2 * math.degrees(math.atan(s / 2 / o["f"])) for s in o["size"])
    return tuple(o["fov"])


def aspect(o):
    """The aspect ratio of the picture (across / up) of a pinhole camera with the FOV of o."""
    h, v = (math.tan(math.radians(a / 2)) for a in fov(o))
    return h / v


def label(o):
    h, v = fov(o)
    return f"{o['model']}, {o['lens']}: {h:.0f} x {v:.0f} deg, {o['res'][0]} x {o['res'][1]}"


def unit(v):
    v = np.asarray(v, float)
    return v / np.linalg.norm(v)


def frame(view):
    """The eye, and the forward, right, and up directions of a view (a dict with eye, focal, up)."""
    eye = np.asarray(view["eye"], float)
    f = unit(np.asarray(view["focal"], float) - eye)
    r = unit(np.cross(f, view["up"]))
    return eye, f, r, np.cross(r, f)


def project(view, o, pts):
    """The pixels (x to the right, y down, 0 to the resolution) of the points pts, and their depth
    along the view."""
    eye, f, r, u = frame(view)
    th, tv = (math.tan(math.radians(a / 2)) for a in fov(o))
    W, H = o["res"]
    q = np.atleast_2d(pts) - eye
    z = q @ f
    return np.stack([W / 2 * (1 + (q @ r) / (z * th)), H / 2 * (1 - (q @ u) / (z * tv))], axis=1), z


def footprint(view, o, bed):
    """The view of an option on the bed plane. bed: a dict with center, normal, a, b (the directions of
    the sides), and size (the print area). Returns a dict:
    width: the width of the picture (mm) through the bed center, on the bed plane, along the rows;
    px_mm: pixels per mm there (the resolution across / width);
    px_mm_depth: pixels per mm at the bed center along the bed, square to the rows (less on a slant);
    corners: the corners of the picture on the bed plane (None for a corner that looks over the horizon);
    in_frame: the part of the print area in the picture."""
    eye, f, r, u = frame(view)
    c, n = np.asarray(bed["center"], float), unit(bed["normal"])
    th, tv = (math.tan(math.radians(a / 2)) for a in fov(o))
    W, H = o["res"]
    z = float((c - eye) @ f)
    # A level camera: r is on the bed plane, so a move along r on the bed is a move along the rows.
    width = 2 * z * th * float(np.linalg.norm(r - (r @ n) * n))
    d = unit(np.cross(n, r))
    p, _ = project(view, o, np.array([c - d * 0.5, c + d * 0.5]))
    depth = float(np.linalg.norm(p[1] - p[0]))
    corners = []
    for sx, sy in ((-1, 1), (1, 1), (1, -1), (-1, -1)):
        ray = f + sx * th * r + sy * tv * u
        den = ray @ n
        t = ((c - eye) @ n) / den if abs(den) > 1e-9 else -1
        corners.append(eye + t * ray if t > 0 else None)
    g = np.linspace(-bed["size"] / 2, bed["size"] / 2, 41)
    a, b = np.asarray(bed["a"], float), np.asarray(bed["b"], float)
    pts = np.array([c + i * a + j * b for i in g for j in g])
    px, zz = project(view, o, pts)
    inside = (zz > 0) & (px[:, 0] >= 0) & (px[:, 0] <= W) & (px[:, 1] >= 0) & (px[:, 1] <= H)
    return dict(width=width, px_mm=W / width, px_mm_depth=depth, corners=corners, in_frame=float(inside.mean()))
