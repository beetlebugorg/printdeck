# Speakerdeck Slate: the images for the build instructions, rendered in FreeCAD (Coin3D).
# Run with: xvfb-run -a -s "-screen 0 1920x1080x24" freecad render.py
# make.sh sets FREECAD_USER_HOME, so that the script does not change your FreeCAD settings.
#
# The script loads the meshes of model.py (build/mesh, build/manifest.json). Each shot shows
# a set of groups, with an offset for some groups (for an exploded view). The groups of the
# step are in full colour; the groups from the steps before are faded.
# Output: build/img/raw/<shot>.png (transparent). make.sh puts them on white.

import json
import math
import os
import sys
import traceback

import FreeCAD as App
import FreeCADGui as Gui
import Mesh
from pivy import coin

HERE = os.path.dirname(os.path.abspath(__file__)) if "__file__" in dir() else os.getcwd()
sys.path.insert(0, HERE)
import shape as S  # noqa: E402

BUILD = os.path.join(HERE, "build")
RAW = os.path.join(BUILD, "img", "raw")
LOG = os.path.join(BUILD, "render.log")
MANIFEST = json.load(open(os.path.join(BUILD, "manifest.json")))

COLOURS = {
    "pla_navy": "#1F2B4D", "pla_orange": "#E8742A", "pla_gray": "#80868E",
    "black_glass": "#050505", "lcd_metal": "#8C9096", "pcb_green": "#1F6B45", "pcb_blue": "#2C5F9E",
    "terminal_green": "#2E8B57", "steel": "#B8BCC2", "alu_dark": "#3A3D42", "label_black": "#1A1A1A",
    "black_plastic": "#161616", "gold": "#D9B248", "brass": "#D4AC52", "post_red": "#B71C1C",
    "post_black": "#121212", "nickel": "#CFCFCF",
}
GLOSS = {"black_glass": (0.6, 0.8), "steel": (0.5, 0.5), "gold": (0.6, 0.5), "brass": (0.5, 0.5),
         "nickel": (0.5, 0.5), "lcd_metal": (0.4, 0.4), "post_red": (0.4, 0.6), "post_black": (0.4, 0.6)}

PRINTED = ["slab", "chin", "back", "foot"]
HW = ["display", "ssd", "ssd_cable", "pi", "pi_plugs", "fan", "fan_cable", "cables"]
ALL = PRINTED + HW
NO_CABLES = [g for g in ALL if g not in ("cables", "pi_plugs")]


def vec(v, k):
    return [c * k for c in v]


N = S.normal()                      # out of the face
U = S.up()                          # up the face
REAR = vec(N, -1)                   # to the rear of the slab
DOWN = vec(U, -1)                   # down along the face
BACK = ["back", "ssd", "ssd_cable", "pi", "pi_plugs", "fan", "fan_cable"]


def add(*vs):
    return [sum(c) for c in zip(*vs)]


# name: (groups, focus groups, offsets {group: vector}, azimuth, elevation, show the screen)
SHOTS = {
    "hero": (ALL, ALL, {}, 34, 16, True),
    "right": (ALL, ALL, {}, 40, 14, True),
    "rear": (ALL, ALL, {}, 150, 18, True),
    "rear_left": (ALL, ALL, {}, -140, 22, True),
    "exploded": (NO_CABLES, NO_CABLES,
                 {"display": vec(REAR, 45), **{g: vec(REAR, 110) for g in BACK},
                  "chin": add(vec(DOWN, 70), [0, 0, -10]), "foot": [0, 0, -150]}, -128, 18, False),
    "step1": (["slab", "display"], ["slab", "display"], {"display": vec(REAR, 70)}, -150, 30, False),
    "step2": (["back", "ssd"], ["back", "ssd"], {"ssd": vec(N, 25)}, -12, 10, False),
    "step2_pi": (["back", "ssd", "fan", "pi"], ["back", "fan", "pi"],
                 {"fan": vec(REAR, 40), "pi": vec(REAR, 105)}, 118, 22, False),
    "step3": (["slab", "display"] + BACK, BACK, {g: vec(DOWN, 150) for g in BACK}, -145, 22, False),
    "step4": (["slab", "display", "chin"] + BACK, ["chin"], {"chin": vec(DOWN, 60)}, -30, 8, True),
    "step5": (NO_CABLES, ["foot"], {"foot": [0, 0, -60]}, -35, 6, True),
    "step6": (ALL, ["cables", "ssd_cable", "pi_plugs"], {}, 150, 14, True),
}

PRINT_ORIENT = {"slab": (-30, 40), "chin": (-30, 34), "back": (-30, 40), "foot": (-30, 34)}


def rgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i:i + 2], 16) / 255 for i in (0, 2, 4))


def log(msg):
    with open(LOG, "a") as fh:
        fh.write(msg + "\n")


def material(vo, mat, ghost):
    c = rgb(COLOURS.get(mat, "#888888"))
    spec, shin = GLOSS.get(mat, (0.12, 0.25))
    m = App.Material()
    m.DiffuseColor = c
    m.AmbientColor = tuple(x * 0.2 for x in c)
    m.SpecularColor = (spec, spec, spec)
    m.Shininess = shin
    m.Transparency = 0.72 if ghost else 0.0
    vo.ShapeAppearance = [m]
    vo.Transparency = 72 if ghost else 0


def style(o, mat, ghost):
    vo = o.ViewObject
    vo.Visibility = True
    if hasattr(vo, "CreaseAngle"):
        vo.CreaseAngle = 50
    if hasattr(vo, "DisplayMode") and "Shaded" in vo.getEnumerationsOfProperty("DisplayMode"):
        vo.DisplayMode = "Shaded"
    material(vo, mat, ghost)


def screen_node(corners):
    sep = coin.SoSeparator()
    lm = coin.SoLightModel()
    lm.model = coin.SoLightModel.BASE_COLOR
    mat = coin.SoMaterial()
    mat.diffuseColor.setValue(1, 1, 1)
    tex = coin.SoTexture2()
    tex.filename = os.path.join(HERE, "assets", "screen.png")
    coords = coin.SoCoordinate3()
    coords.point.setValues(0, 4, [tuple(p) for p in corners])
    tc = coin.SoTextureCoordinate2()
    tc.point.setValues(0, 4, [(0, 0), (1, 0), (1, 1), (0, 1)])
    fs = coin.SoFaceSet()
    fs.numVertices.setValue(4)
    for n in (lm, mat, tex, coords, tc, fs):
        sep.addChild(n)
    return sep


def view_dir(az, el):
    a, e = math.radians(az), math.radians(el)
    return (math.sin(a) * math.cos(e), -math.cos(a) * math.cos(e), math.sin(e))


def light(direction, intensity, colour=(1, 1, 1)):
    lt = coin.SoDirectionalLight()
    lt.direction.setValue(*direction)
    lt.intensity.setValue(intensity)
    lt.color.setValue(*colour)
    return lt


def camera(visible, az, el, size, out, screen=None):
    v = Gui.ActiveDocument.ActiveView
    v.setCameraType("Perspective")
    root = v.getSceneGraph()
    d = view_dir(az, el)
    k = view_dir(az - 45, el + 35)
    f = view_dir(az + 60, 5)
    r = view_dir(az + 160, 30)
    for i, lt in enumerate([light(tuple(-x for x in k), 0.5), light(tuple(-x for x in f), 0.18),
                            light(tuple(-x for x in r), 0.3, (1.0, 0.96, 0.9))]):
        root.insertChild(lt, i)
    if screen is not None:
        root.addChild(screen_node(screen))
    bb = App.BoundBox()
    for o in visible:
        m = o.Mesh.copy()
        m.Placement = o.Placement
        bb.add(m.BoundBox)
    cam = v.getCameraNode()
    cam.heightAngle.setValue(math.radians(24))
    target = coin.SbVec3f(bb.Center.x, bb.Center.y, bb.Center.z)
    radius = math.sqrt(bb.XLength ** 2 + bb.YLength ** 2 + bb.ZLength ** 2) / 2
    cam.position.setValue(target + coin.SbVec3f(*d) * (radius / math.sin(math.radians(11.5))))
    cam.pointAt(target, coin.SbVec3f(0, 0, 1))
    cam.nearDistance.setValue(10)
    cam.farDistance.setValue(20000)
    Gui.updateGui()
    v.saveImage(out, size, size, "Transparent")
    log(f"wrote {out}")


def shot(name, spec):
    groups, focus, offsets, az, el, show_screen = spec
    doc = App.newDocument("shot_" + name)
    visible = []
    for p in MANIFEST["parts"]:
        if p["group"] not in groups:
            continue
        o = doc.addObject("Mesh::Feature", p["name"])
        o.Mesh = Mesh.Mesh(os.path.join(BUILD, "mesh", p["file"]))
        off = offsets.get(p["group"])
        if off:
            o.Placement = App.Placement(App.Vector(*off), App.Rotation())
        visible.append((o, p))
    doc.recompute()
    Gui.updateGui()
    for o, p in visible:
        style(o, p["material"], p["group"] not in focus)
    screen = None
    if show_screen:
        off = offsets.get("display", [0, 0, 0])
        screen = [[c[i] + off[i] for i in range(3)] for c in MANIFEST["screen"]]
    camera([o for o, _ in visible], az, el, 2600, os.path.join(RAW, name + ".png"), screen)
    App.closeDocument(doc.Name)


def part_shot(name):
    """One printed part in its print orientation, from its STL file."""
    doc = App.newDocument("part_" + name)
    o = doc.addObject("Mesh::Feature", name)
    o.Mesh = Mesh.Mesh(os.path.join(HERE, "stl", name + ".stl"))
    mat = next(p["material"] for p in MANIFEST["parts"] if p["name"] == name)
    doc.recompute()
    Gui.updateGui()
    style(o, mat, False)
    az, el = PRINT_ORIENT[name]
    camera([o], az, el, 1400, os.path.join(RAW, "part_" + name + ".png"))
    App.closeDocument(doc.Name)


def main():
    view = App.ParamGet("User parameter:BaseApp/Preferences/View")
    view.SetBool("EnableHeadlight", True)
    view.SetInt("HeadlightIntensity", 30)
    view.SetBool("EnableBacklight", False)
    view.SetBool("ShowNaviCube", False)
    os.makedirs(RAW, exist_ok=True)
    open(LOG, "w").close()
    only = os.environ.get("SHOTS", "").split()
    try:
        for name, spec in SHOTS.items():
            if not only or name in only:
                shot(name, spec)
        for p in PRINTED:
            if not only or "part_" + p in only:
                part_shot(p)
    except Exception:
        log(traceback.format_exc())
    os._exit(0)


main()
