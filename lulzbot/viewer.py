"""A live 3D window for the models while they change.

A model script calls publish() with its meshes. publish() writes each part as an STL file and a list
of the parts (build/viewer/NAME.json). live.py shows them in a window, and loads the parts again
when the list changes. The camera stays where it is.

    uv run --no-project --with pyvista --with trimesh python lulzbot/live.py [NAME]
"""

import json
import os
import time

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "build", "viewer")


def publish(meshes, name="model", note="", stages=None, views=None):
    """meshes: (part name, trimesh, "#RRGGBB"[, extra]), in the view coordinates. extra is a dict for
    the window: "explode" (the move of the part in each stage of the exploded view) or
    "hide_exploded". views: named camera views for the window, each a dict with "eye", "focal",
    "up", and "fov" (the vertical field of view in degrees)."""
    d = os.path.join(OUT, name)
    os.makedirs(d, exist_ok=True)
    stamp = f"{time.time():.3f}"
    parts = []
    for n, m, c, *extra in meshes:
        f = f"{n}.stl"
        m.export(os.path.join(d, f".{f}"))
        os.replace(os.path.join(d, f".{f}"), os.path.join(d, f))
        parts.append({"name": n, "color": c, "file": f, **(extra[0] if extra else {})})
    tmp = os.path.join(OUT, f".{name}.json")
    with open(tmp, "w") as fh:
        json.dump({"t": stamp, "note": note, "stages": stages or [], "parts": parts, "views": views or {}}, fh)
    os.replace(tmp, os.path.join(OUT, f"{name}.json"))
