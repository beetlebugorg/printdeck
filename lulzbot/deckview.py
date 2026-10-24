"""The parts inside the deck case (deckcase.py), the check that no two overlap, and the renders.

Run: uv run --no-project --with build123d --with numpy --with scipy --with trimesh --with networkx
--with pyrender --with PyOpenGL python deckview.py OUT_DIR
"""

import os
import sys

import numpy as np
import trimesh
from build123d import Box, Pos, export_stl

import deckcase as D
import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))


def box(x0, x1, y0, y1, z0, z1):
    return D.box(x0, x1, y0, y1, z0, z1)


def face_box(u0, u1, v0, v1, w0, w1):
    return D.FACE_LOC * Pos((u0 + u1) / 2, (v0 + v1) / 2, (w0 + w1) / 2) * Box(abs(u1 - u0), abs(v1 - v0), abs(w1 - w0))


def pi_box(x0, x1, y0, y1, z0, z1):
    """A box in Pi STEP coordinates, in mount coordinates."""
    a, b = D.pi_to_mount(x0, y0, z0), D.pi_to_mount(x1, y1, z1)
    return box(min(a[0], b[0]), max(a[0], b[0]), min(a[1], b[1]), max(a[1], b[1]), min(a[2], b[2]), max(a[2], b[2]))


# Pi 5 parts in STEP coordinates (board 85 x 56, from the corner; z up from the board bottom). From
# the Slate model, moved from its centred coordinates (+42.5, +28).
def pi_parts():
    c = lambda x0, x1, y0, y1, z0, z1: (x0 + 42.5, x1 + 42.5, y0 + 28, y1 + 28, z0, z1)
    b = 1.6
    return {
        "pi_board": c(-42.5, 42.5, -28, 28, 0, b),
        "pi_ethernet": c(23.5, 44.5, -25.8, -9.8, b, b + 13.5),
        "pi_usb3": c(27, 44.5, -5.5, 7.7, b, b + 15.6),
        "pi_usb2": c(27, 44.5, 12.4, 25.6, b, b + 15.6),
        "pi_usbc": c(-35.8, -26.8, -29.2, -21.8, b, b + 3.2),
        "pi_hdmi0": c(-20.3, -13.1, -29.2, -21.6, b, b + 3),
        "pi_hdmi1": c(-6.9, 0.3, -29.2, -21.6, b, b + 3),
        "cooler": c(-38.0, 25.5, -21.5, 21.0, b, b + 12.0),
    }


def hardware():
    """(name, shape, colour) in mount coordinates."""
    out = []
    # The display: the glass and the panel, the board, and the sockets at its bottom end.
    la, lb = D.LCM
    out.append(("display_panel", face_box(-la / 2, la / 2, D.DISP_V - lb / 2, D.DISP_V + lb / 2, -S.LIP - S.LCM_T, -S.LIP), "#0B0C10"))
    bf = D.BOARD_FRONT_W
    out.append(("display_board", face_box(-S.BOARD_A, S.BOARD_A, D.DISP_V - D.BOARD_HALF, D.DISP_V + D.BOARD_HALF, bf - D.BOARD_T, bf), "#1F6B45"))
    bot = D.DISP_V - D.BOARD_HALF
    out.append(("display_sockets", face_box(-32, 12.9, bot, bot + 16.5, bf - D.BOARD_T - 6, bf - D.BOARD_T), "#C9CDD2"))
    # The plug ends of the ribbons in the sockets, from below: HDMI (3549, right angle) and USB-C (4108).
    out.append(("plug_hdmi", face_box(S.HDMI_A - 9.5, S.HDMI_A + 9.5, bot - 19.0, bot + 2.0, bf - D.BOARD_T - 8.5, bf - D.BOARD_T - 0.5), "#2A2A2E"))
    out.append(("plug_usbc", face_box(S.USB_A - 8.5, S.USB_A + 8.5, bot - 17.0, bot + 2.0, bf - D.BOARD_T - 5.0, bf - D.BOARD_T - 1.0), "#2A2A2E"))
    # The Pi and its plugs.
    for n, b in pi_parts().items():
        out.append((n, pi_box(*b), "#30343A" if n == "cooler" else ("#1F6B45" if n == "pi_board" else "#C9CDD2")))
    f = 85.0 + 2.5                                   # the faces of the USB ports (STEP x)
    out.append(("drive", pi_box(f - 12, f + 30, 28 - 5.5 + 0.5, 28 + 7.7 - 0.5, 2.6, 9.1), "#E9E9E9"))
    out.append(("touch_usb_a", pi_box(f - 12, f + 18, 28 + 19 - 8.5, 28 + 19 + 8.5, 10.2, 15.2), "#2A2A2E"))
    out.append(("printer_plug", pi_box(f - 12, f + 26, 28 + 12.9, 28 + 25.1, 2.4, 9.4), "#151515"))
    out.append(("hdmi_end", pi_box(39.2 - 9, 39.2 + 9, -1.6 - 14.5, 0.0, 0.6, 15.6), "#2A2A2E"))
    # The power plug from below into the USB-C port, and the printer socket in the floor.
    px, py, pz = D.pi_port("usbc")
    out.append(("power_plug", box(px - D.PLUG[1] / 2, px + D.PLUG[1] / 2, py - 6.5 - 24.0, py - 6.5 + 0.0, pz - D.PLUG[0] / 2, pz + D.PLUG[0] / 2), "#F2F2F2"))
    sx, sz = D.socket_xz()
    out.append(("socket_body", box(sx - D.SOCKET_BODY[1] / 2, sx + D.SOCKET_BODY[1] / 2, D.Y_BOT + D.WALL, D.Y_BOT + 31.0,
                                   sz - D.SOCKET_BODY[0] / 2, sz + D.SOCKET_BODY[0] / 2), "#151515"))
    out.append(("socket_flange", box(sx - D.SOCKET_FLANGE[1] / 2, sx + D.SOCKET_FLANGE[1] / 2, D.Y_BOT - 2.0, D.Y_BOT,
                                     sz - D.SOCKET_FLANGE[0] / 2, sz + D.SOCKET_FLANGE[0] / 2), "#151515"))
    return out


# A plug may sit in its own port, a part may sit on its boss, and the sockets are on the board.
ALLOWED = {frozenset(p) for p in [("drive", "pi_usb3"), ("touch_usb_a", "pi_usb2"), ("printer_plug", "pi_usb2"),
                                  ("hdmi_end", "pi_hdmi1"), ("power_plug", "pi_usbc"), ("plug_hdmi", "display_sockets"),
                                  ("plug_usbc", "display_sockets"), ("power_plug", "body"), ("socket_body", "body"),
                                  ("socket_flange", "body")]}


def check(printed, hw):
    bad = []
    pi = set(pi_parts())
    items = [(n, s) for n, s, _ in printed] + [(n, s) for n, s, _ in hw]
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            (a, sa), (c, sc) = items[i], items[j]
            if frozenset((a, c)) in ALLOWED or (a in pi and c in pi) or {a, c} <= {"display_panel", "display_board", "display_sockets"}:
                continue
            ba, bc = sa.bounding_box(), sc.bounding_box()
            if (ba.max.X < bc.min.X or bc.max.X < ba.min.X or ba.max.Y < bc.min.Y or bc.max.Y < ba.min.Y
                    or ba.max.Z < bc.min.Z or bc.max.Z < ba.min.Z):
                continue
            v = (sa & sc).volume
            if v > 0.5:
                bad.append((a, c, round(v, 1)))
    return bad


def mount_matrix():
    import kitview as K
    return K.mount_matrix()


def main(out):
    import render as R
    os.makedirs(out, exist_ok=True)
    printed = [(n, f(), c) for n, f, c in D.PARTS]
    hw = hardware()
    bad = check(printed, hw)
    for a, c, v in bad:
        print(f"overlap: {a} and {c}: {v} mm3")
    print("no overlaps" if not bad else f"{len(bad)} overlaps")
    M = mount_matrix()
    meshes = {}
    tmp = os.path.join(out, "mesh")
    os.makedirs(tmp, exist_ok=True)
    for n, s, c in printed + hw:
        p = os.path.join(tmp, n + ".stl")
        export_stl(s, p, tolerance=0.05, angular_tolerance=0.15)
        m = trimesh.load(p, force="mesh")
        m.apply_transform(M)
        meshes[n] = (m, c)
    # The Pi from its STEP model, for the looks.
    pth = os.path.join(HERE, "build", "ref", "pi5.stl")
    if os.path.exists(pth):
        m = trimesh.load(pth, force="mesh")
        T = np.eye(4)
        T[:3, :3] = np.array([[0, 0, 1], [0, 1, 0], [-1, 0, 0]], float)
        T[:3, 3] = (D.PI_X0, D.PI_Y0, D.PI_LEFT_Z)
        m.apply_transform(T)
        m.apply_transform(M)
        for n in list(meshes):
            if n.startswith("pi_"):
                del meshes[n]
        meshes["pi5"] = (m, "#1F6B45")
    allm = list(meshes.values())
    printer = R.printer_items() + [(R.logo_mark(), "#DDDDDD")]
    R.render(printer + allm, (-0.45, -0.25, 0.85), (0, 1, 0), os.path.join(out, "deck-printer.png"), size=(1400, 1400))
    no_back = [v for k, v in meshes.items() if k != "back"]
    R.render(no_back, (-0.35, -0.3, -0.9), (0, 1, 0), os.path.join(out, "deck-inside.png"), size=(1300, 1400))
    R.render(allm, (-0.55, -0.3, 0.75), (0, 1, 0), os.path.join(out, "deck-front.png"), size=(1200, 1400))
    R.render([meshes["back"]], (-0.35, -0.3, -0.9), (0, 1, 0), os.path.join(out, "deck-back-plate.png"), size=(1100, 1300))
    return bad


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "build", "deckview"))
