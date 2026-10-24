"""Detailed renders of the new kit on the TAZ Workhorse: the display slab, the Pi box, the plug
ends of the two flat cables, and the routes of the ribbons, with their lengths.

The plug ends have no CAD from Adafruit. They are modeled from the vendor sizes (marked [V] in the
parts list) and the photos: a small board, the plug, and the clip for the ribbon. The Pi 5 is the
Raspberry Pi STEP model (build/ref/pi5.stl, made from ref/raspberrypi/). The Active Cooler is from
its drawing.

Run it with: uv run --no-project --with build123d --with numpy --with scipy --with trimesh
--with networkx --with pyrender --with PyOpenGL python kitview.py OUT_DIR
"""

import math
import os
import sys

import numpy as np
import trimesh

import pibox as PB
import shape as S
import tall as T

RIBBON = (10.0, 0.3)          # the Adafruit ribbon: width, thickness
SLAB_T = 20.0                 # the slab, face to back
SKIN = 1.5
SOCKET_BAND = 21.0            # over the top end of the display board: the plug ends


def unit(v):
    v = np.asarray(v, float)
    return v / np.linalg.norm(v)


def box_mesh(c, size, axes=None):
    """A box with centre c and size (sx, sy, sz) along the three axes (columns of axes)."""
    m = trimesh.creation.box(extents=size)
    M = np.eye(4)
    if axes is not None:
        M[:3, :3] = np.column_stack(axes)
    M[:3, 3] = c
    m.apply_transform(M)
    return m


def ribbon_mesh(points, widths, w=RIBBON[0], t=RIBBON[1]):
    """A flat ribbon along points: at each point, widths gives the direction across the ribbon."""
    P = [np.asarray(p, float) for p in points]
    W = [unit(v) for v in widths]
    verts, faces = [], []
    for i, p in enumerate(P):
        d = P[min(i + 1, len(P) - 1)] - P[max(i - 1, 0)]
        n = unit(np.cross(unit(d), W[i]))
        for sw in (-1, 1):
            for st in (-1, 1):
                verts.append(p + sw * W[i] * w / 2 + st * n * t / 2)
    for i in range(len(P) - 1):
        a, b = 4 * i, 4 * (i + 1)
        quad = [(0, 1, 3, 2), (0, 2, 6, 4), (1, 5, 7, 3), (0, 4, 5, 1), (2, 3, 7, 6)]
        for q in quad:
            idx = [(a if k < 4 else b) + (k % 4) for k in q]
            faces += [(idx[0], idx[1], idx[2]), (idx[0], idx[2], idx[3])]
    return trimesh.Trimesh(np.array(verts), np.array(faces), process=False)


def length(points):
    return sum(np.linalg.norm(np.subtract(points[i + 1], points[i])) for i in range(len(points) - 1))


# ---------------------------------------------------------------- the slab


class Slab:
    """The display slab in mount coordinates. Face frame: u across (to the right, seen from the
    front), v up the face, w out of the face; the origin is the middle of the top edge of the face."""

    def __init__(self, L):
        self.L = L
        s, c = T.SIN, T.COS
        self.v = np.array([-s, c, 0.0])
        self.w = np.array([c, s, 0.0])
        self.u = np.array([0.0, 0.0, -1.0])
        self.o = np.array([L.face_x(L.y_top), L.y_top, L.z_mid])
        self.width = L.width
        d = L.d
        # Down the face: the skin, the socket band, the display board, 1 mm, the skin.
        self.board_top = -(SKIN + SOCKET_BAND)
        self.length = SKIN + SOCKET_BAND + d.board[1] + 1.0 + SKIN

    def pt(self, u, v, w):
        return self.o + u * self.u + v * self.v + w * self.w

    def axes(self):
        return [self.u, self.v, self.w]

    def disp(self, a, b, c):
        """A point in portrait display coordinates (a across, b along with the HDMI end at -b, c out
        of the glass). The HDMI end is at the top of the slab."""
        bc = self.board_top - self.L.d.board[1] / 2      # v of the display centre
        return self.pt(a, bc - b, -S.LIP + c)

    def meshes(self):
        """The slab, open at the back, and the display: (mesh, colour)."""
        out = []
        W, Ln = self.width, self.length
        # The walls and the face as thin boxes (the face has the window of the active area).
        aa = self.L.d.aa
        bc = self.board_top - self.L.d.board[1] / 2
        face_parts = [
            (0, -SKIN / 2, W, SKIN),                                 # top band of the face is drawn by the walls
        ]
        frame_top = -Ln
        # Face around the window: four strips.
        wt, wb = bc + aa[1] / 2, bc - aa[1] / 2
        strips = [((0, (0 + wt) / 2), (W, -wt)), ((0, (wb - Ln) / 2), (W, wb + Ln)),
                  ((-(W + aa[0]) / 4, bc), ((W - aa[0]) / 2, aa[1])), (((W + aa[0]) / 4, bc), ((W - aa[0]) / 2, aa[1]))]
        for (uc, vc), (su, sv) in strips:
            out.append((box_mesh(self.pt(uc, vc, -SKIN / 2), (su, sv, SKIN), self.axes()), "#2C3E73"))
        out.append((box_mesh(self.pt(0, bc, -S.LIP - 0.75), (aa[0], aa[1], 1.5), self.axes()), "#0B0C10"))
        # Side walls, top and bottom walls.
        for su in (-1, 1):
            out.append((box_mesh(self.pt(su * (W / 2 - 0.75), -Ln / 2, -SLAB_T / 2), (1.5, Ln, SLAB_T), self.axes()), "#2C3E73"))
        for vv in (-0.75, -Ln + 0.75):
            out.append((box_mesh(self.pt(0, vv, -SLAB_T / 2), (W, 1.5, SLAB_T), self.axes()), "#2C3E73"))
        # The display: the panel and the board with its sockets.
        d = self.L.d
        la, lb = d.lcm
        out.append((box_mesh(self.disp(0, 0, -d.lcm_t / 2), (la, lb, d.lcm_t), self.axes()), "#8C9096"))
        c0, c1 = -d.board_front - 2.0, -d.board_front
        out.append((box_mesh(self.disp(0, 0, (c0 + c1) / 2), (d.board[0] - 16, d.board[1], 2.0), self.axes()), "#1F6B45"))
        for sa in (-1, 1):
            for sb in (-1, 1):
                out.append((box_mesh(self.disp(sa * (d.board[0] / 2 - 4), sb * (d.board[1] / 2 - 4), (c0 + c1) / 2),
                                     (8, 8, 2.0), self.axes()), "#1F6B45"))
        return out

    def sockets(self):
        """The HDMI and USB-C sockets on the back of the board, at its top end: (name, a, mesh)."""
        d = self.L.d
        bt = -d.board[1] / 2
        c0 = -d.board_front - 2.0
        out = []
        for name, a, wa, hc in (("hdmi_socket", S.HDMI_A, 15.0, 6.0), ("usbc_socket", S.USB_A, 9.0, 3.3)):
            out.append((name, a, box_mesh(self.disp(a, bt + 7.0, c0 - hc / 2), (wa, 14.0, hc), self.axes())))
        return out


# ---------------------------------------------------------------- plug ends


def plug_end(origin, axis, side, up, plug, board, clip, right_angle):
    """A plug end of the Adafruit ribbon family: the plug (in the socket along axis), the small
    board behind it, and the ribbon clip. axis points into the socket; side is across the board;
    up is out of the board. For a right-angle end, the clip is on the side of the board, and the
    ribbon leaves along side. It returns the meshes and the point and direction where the ribbon
    leaves."""
    ax, sd, upv = unit(axis), unit(side), unit(up)
    o = np.asarray(origin, float)          # the face of the socket, at the middle of the plug
    pl, pw, ph = plug                      # plug: into the socket, across, thick
    bl, bw, bt = board                     # board: along -axis, across, thick
    out = [(box_mesh(o + ax * (pl / 2 - 1.0), (pw, ph, pl), [sd, upv, ax]), "#C9CDD2")]
    bcen = o - ax * (bl / 2 + 1.0) - upv * (ph / 2 - bt / 2)
    out.append((box_mesh(bcen, (bw, bt, bl), [sd, upv, ax]), "#1B1B1F"))
    if right_angle:
        cc = bcen + sd * (bw / 2 - clip[2] / 2) + upv * (bt / 2 + clip[1] / 2)
        out.append((box_mesh(cc, (clip[2], clip[1], clip[0]), [sd, upv, ax]), "#F0F0F0"))
        exit_p, exit_d = bcen + sd * (bw / 2) + upv * (bt / 2 + clip[1] / 2), sd
    else:
        cc = bcen - ax * (bl / 2 - clip[2] / 2) + upv * (bt / 2 + clip[1] / 2)
        out.append((box_mesh(cc, (clip[0], clip[1], clip[2]), [sd, upv, ax]), "#F0F0F0"))
        exit_p, exit_d = bcen - ax * (bl / 2) + upv * (bt / 2 + clip[1] / 2), -ax
    return out, exit_p, exit_d


CLIP = (12.0, 2.5, 4.0)        # the ribbon clip: across the ribbon, thick, along the ribbon [E]


# ---------------------------------------------------------------- the Pi box


def mount_matrix():
    import parts as P
    t = P.LOC.wrapped.Transformation()
    M = np.eye(4)
    for i in range(3):
        for j in range(4):
            M[i, j] = t.Value(i + 1, j + 1)
    return M


def pi_meshes(b):
    """The Pi 5 from its STEP model (build/ref/pi5.stl), and the Active Cooler from its drawing."""
    out = []
    path = os.path.join(os.path.dirname(__file__), "build", "ref", "pi5.stl")
    if os.path.exists(path):
        m = trimesh.load(path, force="mesh")
        # The STEP has its origin at a corner of the board: board 85 x 56, USB-C edge at y = 0.
        m.apply_translation((b.pi[0] - 42.5, b.pi[1] - 28.0, b.pi[2]))
        out.append((m, "#1F6B45"))
    else:
        for n, (x0, x1, y0, y1, z0, z1) in PB.PI_PARTS.items():
            if n != "cooler":
                a, c = b.to_mount(x0, y0, z0), b.to_mount(x1, y1, z1)
                out.append((box_mesh(np.add(a, c) / 2, np.subtract(c, a)), "#1F6B45" if n == "pi_board" else "#C9CDD2"))
    x0, x1, y0, y1, z0, z1 = PB.PI_PARTS["cooler"]
    a, c = b.to_mount(x0, y0, z0 + 1.2), b.to_mount(x1, y1, z1)
    # The base and the fins, and the blower in one corner, with its round intake.
    out.append((box_mesh((np.add(a, c) / 2) - [0, 0, (c[2] - a[2]) / 2 - 1.0], (c[0] - a[0], c[1] - a[1], 2.0)), "#30343A"))
    for i in range(12):
        fx = a[0] + 3 + i * 2.8
        out.append((box_mesh((fx, (a[1] + c[1]) / 2 - 6, (a[2] + c[2]) / 2 + 1), (0.8, c[1] - a[1] - 14, c[2] - a[2] - 2)), "#30343A"))
    fan_c = (c[0] - 16.0, c[1] - 16.0, (a[2] + c[2]) / 2 + 1)
    out.append((box_mesh(fan_c, (30, 30, c[2] - a[2] - 2)), "#1B1B1F"))
    intake = trimesh.creation.cylinder(radius=11.5, height=0.6)
    intake.apply_translation((fan_c[0], fan_c[1], c[2] + 0.2))
    out.append((intake, "#55595F"))
    return out


def box_shell(b, lid=False):
    """The Pi box shell without its lid (for the view inside), or with it."""
    x0, x1, y0, y1, z0, z1 = b.outer
    w = b.wall
    out = []
    col = "#2C3E73"
    out.append((box_mesh(((x0 + x1) / 2, (y0 + y1) / 2, z0 + w / 2), (x1 - x0, y1 - y0, w)), col))
    out.append((box_mesh(((x0 + x1) / 2, y0 + w / 2, (z0 + z1) / 2), (x1 - x0, w, z1 - z0)), col))
    out.append((box_mesh(((x0 + x1) / 2, y1 - w / 2, (z0 + z1) / 2), (x1 - x0, w, z1 - z0)), col))
    out.append((box_mesh((x0 + w / 2, (y0 + y1) / 2, (z0 + z1) / 2), (w, y1 - y0, z1 - z0)), col))
    out.append((box_mesh((x1 - w / 2, (y0 + y1) / 2, (z0 + z1) / 2), (w, y1 - y0, z1 - z0)), col))
    if lid:
        out.append((box_mesh(((x0 + x1) / 2, (y0 + y1) / 2, z1 - w / 2), (x1 - x0, y1 - y0, w)), col))
    return out


# ---------------------------------------------------------------- the assembly


def build():
    L = T.workhorse()
    L.y_top = T.WORKHORSE_TOP
    slab = Slab(L)
    b = PB.layout(top=L.y_top)        # the top of the box is the top of the slab
    parts = {"slab": slab.meshes(), "pi": pi_meshes(b), "box": box_shell(b), "lid": box_shell(b, lid=True)[-1:]}
    # Drive, printer plug and socket, power plug: the boxes of pibox.
    for n, sh in PB.solids(b):
        if n in ("drive", "printer_plug", "socket_flange", "socket_body", "power_plug", "cover_screw"):
            bb = sh.bounding_box()
            col = {"drive": "#E9E9E9", "power_plug": "#F2F2F2", "cover_screw": "#B8BCC2"}.get(n, "#151515")
            parts.setdefault("pibox_parts", []).append(
                (box_mesh(((bb.min.X + bb.max.X) / 2, (bb.min.Y + bb.max.Y) / 2, (bb.min.Z + bb.max.Z) / 2),
                          (bb.max.X - bb.min.X, bb.max.Y - bb.min.Y, bb.max.Z - bb.min.Z)), col))
    # The neck: it joins the front end of the box to the cover-side wall of the slab, at the top,
    # around the corner of the chassis. The ribbons go through it.
    x0, x1, y0, y1, z0, z1 = b.outer
    cw = slab.pt(-slab.width / 2, 0, 0)[2]
    neck_h = 34.0
    ny0, ny1 = y1 - neck_h, y1
    back_at = lambda y: (slab.pt(0, 0, -SLAB_T)[0] + (L.y_top - y) * T.TAN)
    nx0 = x1 - b.wall
    prof = []
    for yy in (ny0, ny1):
        prof += [(nx0, yy), (back_at(yy) + 4.0, yy)]
    pts3 = [(x, y, z) for x, y in prof for z in (cw, z1)]
    neck = trimesh.convex.convex_hull(np.array(pts3))
    parts["neck"] = [(neck, "#2C3E73")]
    socks = {n: (a, m) for n, a, m in slab.sockets()}
    parts["slab"] += [(m, "#C9CDD2") for _, m in socks.values()]
    routes = {}
    ends = []
    d = L.d
    bt = -d.board[1] / 2
    c_sock = -d.board_front - 2.0 - 3.0
    # Display HDMI end (3549, right angle, R): plug down into the socket, the board above it,
    # the ribbon leaves toward the cover side (+z, -u).
    o = slab.disp(S.HDMI_A, bt, c_sock)
    m, hp, hd = plug_end(o, slab.v * -1, -slab.u, -slab.w, (9.0, 14.0, 4.5), (17.0, 19.0, 1.6), CLIP, True)
    ends += m
    # Display USB-C end (4108, straight): the ribbon leaves upward, then folds toward the cover side.
    o = slab.disp(S.USB_A, bt, -d.board_front - 2.0 - 1.65)
    m, up_, ud = plug_end(o, slab.v * -1, -slab.u, -slab.w, (6.5, 8.4, 2.6), (16.0, 17.0, 1.6), CLIP, False)
    ends += m
    # Pi end of the touch cable (4109, straight) in the upper USB 2 port, and the HDMI end (3557,
    # right angle) in HDMI1 from below.
    px = b.to_mount(PB.PORT_FACE, 19.0, PB.USB2_HIGH[0] + 3.0)
    m, tp, td = plug_end(px, [-1, 0, 0], [0, 1, 0], [0, 0, 1], (12.0, 12.0, 4.5), (18.0, 17.0, 1.6), CLIP, False)
    ends += m
    hx = b.to_mount(-3.3, PB.EDGE_FACE, PB.BOARD_T + 1.5)
    m, pp, pd = plug_end(hx, [0, 1, 0], [1, 0, 0], [0, 0, 1], (6.5, 6.4, 2.8), (12.0, 17.5, 1.6), CLIP, True)
    ends += m
    parts["ends"] = ends
    # Routes: (point, the direction across the ribbon). A flat ribbon bends only across its width,
    # so a turn in its own plane is a 45 degree fold; the renders show a fold as a short twist.
    # The ribbons go up the inside of the front wall, through the neck and into the top band of
    # the slab.
    fx = x1 - b.wall - 2.0
    Y, Z = np.array([0, 1.0, 0]), np.array([0, 0, 1.0])
    hv = (hp - slab.o) @ slab.v
    hw = (hp - slab.o) @ slab.w
    tw = hw - 3.0
    ry = slab.pt(0, hv, hw)[1]                                   # the height of the ribbons in the neck
    def sp(u, v, w):
        return slab.pt(u, v, w)
    floor_z = b.wall + RIBBON[1] / 2 + 0.2
    hdmi = [(pp, Y), (pp + pd * 6, Y), ((pp[0] + 10, pp[1], floor_z), Y), ((fx, pp[1], floor_z), Y),
            ((fx, pp[1] + 6, floor_z + 5.0), Z), ((fx, ry - 6, 9.0), Z), ((fx + 4, ry, 9.0), Y),
            (sp(-slab.width / 2 - 2, hv, hw), Y), (sp(-slab.width / 2 + 4, hv, hw), slab.v), (hp, slab.v)]
    touch = [(tp, Y), (tp + td * 6, Y), ((fx, tp[1], tp[2]), Y), ((fx, tp[1] + 3, tp[2]), Z),
             ((fx, ry - 4, 16.0), Z), ((fx + 4, ry - 1, 16.0), Y), (sp(-slab.width / 2 - 2, hv, tw), Y),
             (sp(-slab.width / 2 + 4, hv, tw), slab.v), (sp((up_ - slab.o) @ slab.u, hv, tw), slab.v),
             (up_ + ud * 6, -slab.u), (up_, -slab.u)]
    routes["hdmi"] = hdmi
    routes["touch"] = touch
    rib = []
    for name, pts in routes.items():
        P_ = [np.asarray(p, float) for p, _ in pts]
        W_ = [np.asarray(w_, float) for _, w_ in pts]
        rib.append((ribbon_mesh(P_, W_), "#D8A24A" if name == "hdmi" else "#C8C8C8"))
    parts["ribbons"] = rib
    routes = {n: [p for p, _ in pts] for n, pts in routes.items()}
    lens = {n: length([np.asarray(p, float) for p in pts]) for n, pts in routes.items()}
    return parts, lens, slab, b


def main(out):
    import render as R
    os.makedirs(out, exist_ok=True)
    parts, lens, slab, b = build()
    for n, l_ in lens.items():
        print(f"{n} ribbon route: {l_:.0f} mm")
    M = mount_matrix()

    def world(lst):
        res = []
        for m, c in lst:
            m = m.copy()
            m.apply_transform(M)
            res.append((m, c))
        return res

    allp = {k: world(v) for k, v in parts.items()}
    every = sum(allp.values(), [])
    printer = R.printer_items() + [(R.logo_mark(), "#DDDDDD")]
    R.render(printer + [x for k, v in allp.items() if k != "lid" or True for x in v],
             (-0.45, -0.25, 0.85), (0, 1, 0), os.path.join(out, "kit-front-left.png"), size=(1400, 1400))
    inside = [x for k, v in allp.items() if k != "lid" for x in v]
    near = []
    for f in ("cb_chassis", "cb_cover"):
        pth = os.path.join("build", "workhorse", f + ".stl")
        if os.path.exists(pth):
            m = trimesh.load(pth, force="mesh")
            c0, c1 = np.array([395.0, -470.0, -40.0]), np.array([540.0, -350.0, 40.0])
            try:
                for o_, n_ in ((c0, (1, 0, 0)), (c1, (-1, 0, 0)), (c0, (0, 1, 0)), (c1, (0, -1, 0)), (c0, (0, 0, 1)), (c1, (0, 0, -1))):
                    m = m.slice_plane(o_, n_, cap=True)
            except Exception:
                pass
            near.append((m, "#9A9DA2"))
    R.render(near + inside, (-0.55, 0.55, 0.65), (0, 1, 0), os.path.join(out, "kit-corner-top.png"), size=(1600, 1300))
    # Close-up of the plug ends on the display board, from behind and above.
    sock = slab.disp(0, -slab.L.d.board[1] / 2, 0) @ M[:3, :3].T + M[:3, 3]
    close = [(m, c) for m, c in inside if np.linalg.norm(m.bounds.mean(axis=0) - sock) < 75
             and (np.ptp(m.bounds, axis=0).max() < 90 or c != "#2C3E73")]
    R.render(close, (0.3, -0.25, -0.92), (0, 1, 0), os.path.join(out, "kit-display-ends.png"), size=(1500, 1100))
    R.render(inside, (-0.8, -0.35, 0.5), (0, 1, 0), os.path.join(out, "kit-corner.png"), size=(1600, 1300))
    R.render(inside, (-1.0, -0.15, -0.15), (0, 1, 0), os.path.join(out, "kit-pibox-open.png"), size=(1600, 1200))
    R.render(inside, (0.35, -0.3, -1.0), (0, 1, 0), os.path.join(out, "kit-slab-back.png"), size=(1400, 1400))


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "build/kitview")
