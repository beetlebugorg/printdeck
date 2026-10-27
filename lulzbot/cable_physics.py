"""The shape of the cables from a simple physical model: each cable is an elastic rod that does not
stretch, held at its ends and at its clips, with its own weight. pod.py solves the cables of the live
window with it (the power cable, the printer cable, and the two camera cables).

The rod is a polyline of nodes about SEG mm apart. The ends of the rod and the clips are anchors: each
fixes a point and the direction of the cable there (the axis of the plug, or the direction of the
clip). Between two anchors, a span has a fixed length: the length of the drawn route plus slack.
The solver finds the nodes with the least energy:
- stretch: a stiff spring on each segment, so each span keeps its length;
- bending: EI times the squared curvature;
- weight: the weight of the cable times the height of each node;
- bend radius: a penalty where the radius goes under MIN_BEND diameters;
- contact: a penalty where a node is nearer than its radius to a solid (points sampled on the parts
  near the cable) or to a cable solved before it.
It solves all the cables of a printer together, from their drawn routes (pod.rounded), with L-BFGS-B:
first with long segments, then finer. The result goes in a cache
(build/cables), keyed on the inputs, so the next run does not solve again.

Values for a round USB 2.0 or power cable of 3 to 4.5 mm with a PVC jacket (typical values, not
measured): EI = 2e-5 N m^2 (20 N mm^2), 30 g/m. Units here: mm and N.
"""

import hashlib
import json
import math
import os
import time

import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
CACHE = os.path.join(HERE, "build", "cables")

VERSION = 3                   # goes in the cache key: change it when the solver changes
SEG = 3.0                     # mm, the target length of a segment
EI = 20.0                     # N mm^2 (2e-5 N m^2)
WEIGHT = 0.030 * 9.81 / 1000  # N per mm of cable (30 g/m)
MIN_BEND = 5.0                # the smallest bend radius, in cable diameters
CLEAR = 0.5                   # mm, the gap the contact penalty keeps to a solid
K_STRETCH = 300.0             # N/mm
K_BEND_MIN = 200000.0
K_CONTACT = 500.0             # N/mm
SAMPLE = 0.5                  # mm, the spacing of the points on the surfaces of the solids
REACH = 40.0                  # mm: the solids this near the drawn routes
NEAR = 2.5                    # mm: a clip within this distance of the drawn route holds the cable
# The slack of a span: a share of its length plus a fixed length. A span between two clips is pulled
# snug; a free span (from a plug to its first clip, or from the last clip to a plug) hangs a little.
SLACK = {"clip": (0.0, 0.3), "free": (0.03, 10.0)}


# ---------------------------------------------------------------- the distance field of the solids
class Field:
    """The distance (mm) from a point to the nearest surface of the solids, and its direction: the
    nearest of many points sampled on the surfaces (about SAMPLE mm apart), within REACH mm of the
    drawn routes. It has no inside: a cable that starts outside a part stays outside."""

    def __init__(self, meshes, paths):
        import trimesh
        from scipy.spatial import cKDTree
        near = cKDTree(np.vstack([_resample(p, int(np.ceil(_len(p) / 5.0))) for p in paths]))
        lo = np.min([p.min(axis=0) for p in paths], axis=0) - REACH
        hi = np.max([p.max(axis=0) for p in paths], axis=0) + REACH
        pts = []
        for m in meshes:
            b = m.bounds
            if np.any(b[1] < lo) or np.any(b[0] > hi):
                continue
            tri = m.triangles
            keep = np.all((tri.max(axis=1) >= lo) & (tri.min(axis=1) <= hi), axis=1)
            if not keep.any():
                continue
            sub = m.submesh([np.nonzero(keep)[0]], append=True)
            n = int(min(1.2e7, max(500, 2.0 * sub.area / SAMPLE ** 2)))
            q = np.vstack([trimesh.sample.sample_surface(sub, n)[0], sub.vertices])
            d, _ = near.query(q, distance_upper_bound=REACH)
            pts.append(q[np.isfinite(d)])
        self.pts = np.vstack(pts) if pts else np.zeros((1, 3)) + 1e6
        self.tree = cKDTree(self.pts)

    def __call__(self, x, reach=10.0):
        """The distance and its direction; a point farther than reach gets reach and no direction."""
        d, j = self.tree.query(x, distance_upper_bound=reach)
        far = ~np.isfinite(d)
        j = np.where(far, 0, j)
        d = np.where(far, reach, d)
        g = (x - self.pts[j]) / np.maximum(d, 1e-9)[:, None]
        g[far] = 0.0
        return d, g


def _len(p):
    return float(np.linalg.norm(np.diff(p, axis=0), axis=1).sum())


# ---------------------------------------------------------------- the rod
def _resample(path, n):
    """n + 1 points at equal arc length along the polyline path."""
    seg = np.linalg.norm(np.diff(path, axis=0), axis=1)
    s = np.concatenate([[0.0], np.cumsum(seg)])
    t = np.linspace(0.0, s[-1], n + 1)
    return np.stack([np.interp(t, s, path[:, k]) for k in range(3)], axis=1)


def _param(path, p):
    """The arc length along path of the point of path nearest to p, and the distance."""
    a, b = path[:-1], path[1:]
    ab = b - a
    t = np.clip(np.einsum("ij,ij->i", p - a, ab) / np.maximum(np.einsum("ij,ij->i", ab, ab), 1e-12), 0, 1)
    q = a + ab * t[:, None]
    dist = np.linalg.norm(q - p, axis=1)
    i = int(np.argmin(dist))
    seg = np.linalg.norm(ab, axis=1)
    return float(seg[:i].sum() + t[i] * seg[i]), float(dist[i])


def _cut(path, s0, s1):
    """The part of the polyline path from arc length s0 to s1."""
    seg = np.linalg.norm(np.diff(path, axis=0), axis=1)
    s = np.concatenate([[0.0], np.cumsum(seg)])
    inner = [path[i] for i in range(len(path)) if s0 < s[i] < s1]
    at = lambda v: np.array([np.interp(v, s, path[:, k]) for k in range(3)])
    return np.array([at(s0)] + inner + [at(s1)])


def make_spec(name, points, d, clips=(), start_dir=None, end_dir=None, ties=(), coil=None, total=None,
              remainder=None):
    """A cable spec from a drawn route (points) and the cable centers of the clips it may pass through
    (each within NEAR mm of the rounded route is an anchor, in the direction of the route there).
    start_dir: the direction out of the plug at the start; end_dir: the direction into the end (both
    the route direction when None). ties: more anchors (point, direction). coil: the polyline of a
    coil on the route (its ends are anchors and its shape is fixed). total: the length of the cable;
    remainder: "last" to give the last span the rest of the total."""
    import pod as P
    path = P.rounded(points)
    t0 = np.asarray(start_dir if start_dir is not None else path[1] - path[0], float)
    t1 = np.asarray(end_dir if end_dir is not None else path[-1] - path[-2], float)
    found = []
    total_len = _len(path)
    for c in clips:
        c = np.asarray(c, float)
        s, dist = _param(path, c)
        if dist > NEAR or s < 10.0 or s > total_len - 10.0:
            continue
        a = _cut(path, max(0.0, s - 2.0), min(total_len, s + 2.0))
        found.append((s, (c.tolist(), (a[-1] - a[0]).tolist(), "clip")))
    for p, t in ties:
        found.append((_param(path, np.asarray(p, float))[0], (list(p), list(t), "tie")))
    if coil is not None:
        coil = np.asarray(coil, float)
        found.append((_param(path, coil[0])[0], (coil[0].tolist(), (coil[1] - coil[0]).tolist(), "coil")))
        found.append((_param(path, coil[-1])[0] + 1e-6, (coil[-1].tolist(), (coil[-1] - coil[-2]).tolist(), "coil")))
    found.sort(key=lambda f: f[0])
    anchors = [(path[0].tolist(), t0.tolist(), "plug")] + [f[1] for f in found] + [(path[-1].tolist(), t1.tolist(), "plug")]
    spec = dict(name=name, path=path, anchors=anchors, d=d)
    if coil is not None:
        k = next(i for i, a in enumerate(anchors) if a[2] == "coil")
        spec["fixed"] = {k: coil}
    if total is not None:
        spec["total"] = total
    if remainder == "last":
        spec["remainder"] = len(anchors) - 2
    return spec


def moved(spec, T):
    """The spec moved by the 4 x 4 matrix T."""
    R = T[:3, :3]
    mv = lambda p: (T @ np.append(np.asarray(p, float), 1.0))[:3].tolist()
    return dict(spec, path=np.array([mv(p) for p in spec["path"]]),
                anchors=[(mv(p), (R @ np.asarray(t, float)).tolist(), k) for p, t, k in spec["anchors"]],
                fixed={k: np.array([mv(p) for p in fp]) for k, fp in spec.get("fixed", {}).items()})


HELD = ("clip", "tie")    # anchors that hold the cable snug


def lengths(spec):
    """For each span of spec: (kind, the drawn length, the length of cable). A span in spec["fixed"]
    (a coil) has the length of its fixed shape. When spec has "total" and "remainder" (a span index),
    that span has the rest of the total length."""
    path = np.asarray(spec["path"], float)
    anchors = spec["anchors"]
    s_at = [_param(path, np.asarray(p, float))[0] for p, _, _ in anchors]
    out = []
    for k in range(len(anchors) - 1):
        if k in spec.get("fixed", {}):
            fp = np.asarray(spec["fixed"][k], float)
            out.append(("fixed", _len(fp), _len(fp)))
            continue
        piece = _cut(path, s_at[k], s_at[k + 1])
        piece[0], piece[-1] = anchors[k][0], anchors[k + 1][0]
        drawn = _len(piece)
        kind = "clip" if (anchors[k][2] in HELD and anchors[k + 1][2] in HELD) else "free"
        a, b = SLACK[kind]
        L = drawn * (1 + a) + b
        if k in spec.get("exact", {}):
            L = drawn + spec["exact"][k]          # the sag sheet: the drawn length plus a given length
        out.append((kind, drawn, L))
    r = spec.get("remainder")
    if r is not None and r not in spec.get("exact", {}):
        kind, drawn, _ = out[r]
        out[r] = (kind, drawn, spec["total"] - sum(L for i, (_, _, L) in enumerate(out) if i != r))
    return out


def build(spec, seg=SEG):
    """The rod of a cable spec, with segments of about seg mm: the start nodes, the rest length of
    each segment, the fixed nodes, and the spans [(first node, last node, kind, rest length, drawn
    length)]. The nodes of a fixed span (a coil) are all fixed."""
    path = np.asarray(spec["path"], float)
    anchors = spec["anchors"]
    s_at = [_param(path, np.asarray(p, float))[0] for p, _, _ in anchors]
    init = spec.get("init")
    if init is not None:
        init = np.asarray(init, float)
        i_at = [_param(init, np.asarray(p, float))[0] for p, _, _ in anchors]
    nodes, rest, fixed, spans = [], [], set(), []
    for k, (kind, drawn, L) in enumerate(lengths(spec)):
        if L < drawn * 0.98 - 1.0:
            raise ValueError(f"{spec['name']}: span {k} is {drawn:.0f} mm on the route and has {L:.0f} mm of cable")
        n = max(6, int(math.ceil(L / seg)))
        if kind == "fixed":
            piece = np.asarray(spec["fixed"][k], float)
        elif init is not None:
            # Start from a solved shape (the drawn route only sets the lengths).
            piece = _cut(init, i_at[k], i_at[k + 1])
        else:
            piece = _cut(path, s_at[k], s_at[k + 1])
        pts = _resample(piece, n)
        i0 = len(nodes) - 1 if nodes else 0
        if nodes:
            pts = pts[1:]
        nodes += list(pts)
        h = L / n
        rest += [h] * n
        i1 = i0 + n
        (pa, ta, _), (pb, tb, _) = anchors[k], anchors[k + 1]
        if kind == "fixed":
            rest[-n:] = list(np.linalg.norm(np.diff(_resample(piece, n), axis=0), axis=1))
            fixed |= set(range(i0, i1 + 1))
        else:
            ta, tb = np.asarray(ta, float), np.asarray(tb, float)
            nodes[i0], nodes[i1] = np.asarray(pa, float), np.asarray(pb, float)
            nodes[i0 + 1] = nodes[i0] + h * ta / np.linalg.norm(ta)
            nodes[i1 - 1] = nodes[i1] - h * tb / np.linalg.norm(tb)
            fixed |= {i0, i0 + 1, i1 - 1, i1}
        spans.append((i0, i1, kind, L, drawn))
    return np.array(nodes), np.array(rest), sorted(fixed), spans


def energy(x, rest, up, r, field, others, kmin):
    """The energy of the rod at nodes x and its gradient. others: fixed cables (tree, points, radius)."""
    g = np.zeros_like(x)
    e = x[1:] - x[:-1]
    l = np.linalg.norm(e, axis=1)
    dl = l - rest
    E = K_STRETCH * float(np.sum(dl ** 2))
    ge = (2 * K_STRETCH * dl / np.maximum(l, 1e-9))[:, None] * e
    # Bending: the change of the tangent (e / rest) at each inner node.
    t = e / rest[:, None]
    s = t[1:] - t[:-1]
    hm = (rest[1:] + rest[:-1]) / 2
    E += EI * float(np.sum(np.einsum("ij,ij->i", s, s) / hm))
    gs = 2 * EI * s / hm[:, None]
    k = np.linalg.norm(s, axis=1) / hm
    over = np.maximum(0.0, k - kmin)
    E += K_BEND_MIN * float(np.sum(over ** 2))
    gs += (2 * K_BEND_MIN * over / np.maximum(k, 1e-12) / hm ** 2)[:, None] * s
    gt = np.zeros_like(t)
    gt[1:] += gs
    gt[:-1] -= gs
    ge += gt / rest[:, None]
    g[1:] += ge
    g[:-1] -= ge
    # Weight.
    share = np.zeros(len(x))
    share[1:] += rest / 2
    share[:-1] += rest / 2
    E += WEIGHT * float(np.sum(share * (x @ up)))
    g += WEIGHT * share[:, None] * up[None, :]
    # Contact with the solids.
    if field is not None:
        d, dg = field(x)
        pen = np.maximum(0.0, r + CLEAR - d)
        E += K_CONTACT * float(np.sum(pen ** 2))
        g -= (2 * K_CONTACT * pen)[:, None] * dg
    # Contact with the cables solved before.
    for tree, pts, ro in others:
        dist, j = tree.query(x)
        lim = r + ro + CLEAR
        pen = np.maximum(0.0, lim - dist)
        if pen.any():
            v = (x - pts[j]) / np.maximum(dist, 1e-9)[:, None]
            E += K_CONTACT * float(np.sum(pen ** 2))
            g -= (2 * K_CONTACT * pen)[:, None] * v
    return E, g


LEVELS = (4 * SEG, 2 * SEG, SEG)


def _minimize(f, v0):
    from scipy.optimize import minimize
    res = minimize(f, v0, jac=True, method="L-BFGS-B",
                   options=dict(maxiter=6000, maxfun=12000, maxcor=30, ftol=1e-10, gtol=1e-3))
    if os.environ.get("CABLE_DEBUG"):
        print("  iterations", res.nit, "energy", round(float(res.fun), 3), "largest force",
              round(float(np.abs(res.jac).max()), 4), res.message)
    return res.x


def _start(spec, seg, prev):
    """The rod of spec at segments of seg mm, started from the rod prev (x, spans) of the level
    before."""
    x0, rest, fixed, spans = build(spec, seg)
    free = np.ones(len(x0), bool)
    free[fixed] = False
    if prev is not None:
        px, pspans = prev
        for (i0, i1, *_), (j0, j1, *_) in zip(spans, pspans):
            fine = _resample(px[j0:j1 + 1], i1 - i0)
            m = free[i0:i1 + 1]
            x0[i0:i1 + 1][m] = fine[m]
    return x0, rest, free, spans


def solve_one(spec, up, field, others):
    """The nodes of the rod of spec at the least energy, clear of the cables others (fixed). It
    solves with long segments first and starts each finer rod from the one before (the long, slow
    sag modes of a fine rod converge slowly)."""
    r = spec["d"] / 2
    kmin = 1.0 / (MIN_BEND * spec["d"])
    up = np.asarray(up, float) / np.linalg.norm(up)
    prev = None
    for seg in LEVELS:
        x0, rest, free, spans = _start(spec, seg, prev)

        def f(v, x0=x0, free=free, rest=rest):
            x = x0.copy()
            x[free] = v.reshape(-1, 3)
            E, g = energy(x, rest, up, r, field, others, kmin)
            return E, g[free].ravel()

        x = x0.copy()
        x[free] = _minimize(f, x0[free].ravel()).reshape(-1, 3)
        prev = (x, spans)
    return x, rest, spans


def solve_joint(specs, up, field):
    """All the cables of specs at once, each clear of the others: [(nodes, rest, spans)]."""
    from scipy.spatial import cKDTree
    up = np.asarray(up, float) / np.linalg.norm(up)
    prev = [None] * len(specs)
    for seg in LEVELS:
        rods = [_start(s, seg, p) for s, p in zip(specs, prev)]
        offs = np.cumsum([0] + [len(r[0]) for r in rods])
        X0 = np.vstack([r[0] for r in rods])
        free = np.concatenate([r[2] for r in rods])
        cid = np.concatenate([np.full(len(r[0]), i) for i, r in enumerate(rods)])
        rad = np.array([s["d"] / 2 for s in specs])[cid]
        lim_max = 2 * rad.max() + CLEAR

        def f(v, X0=X0, free=free, rods=rods, offs=offs, cid=cid, rad=rad):
            X = X0.copy()
            X[free] = v.reshape(-1, 3)
            E, G = 0.0, np.zeros_like(X)
            for i, (s, r) in enumerate(zip(specs, rods)):
                a, b = offs[i], offs[i + 1]
                e, g = energy(X[a:b], r[1], up, s["d"] / 2, field, [], 1.0 / (MIN_BEND * s["d"]))
                E += e
                G[a:b] = g
            pairs = cKDTree(X).query_pairs(lim_max, output_type="ndarray")
            if len(pairs):
                pairs = pairs[cid[pairs[:, 0]] != cid[pairs[:, 1]]]
                i, j = pairs[:, 0], pairs[:, 1]
                dv = X[i] - X[j]
                dist = np.linalg.norm(dv, axis=1)
                pen = np.maximum(0.0, rad[i] + rad[j] + CLEAR - dist)
                E += K_CONTACT * float(np.sum(pen ** 2))
                w = (2 * K_CONTACT * pen / np.maximum(dist, 1e-9))[:, None] * dv
                np.add.at(G, i, -w)
                np.add.at(G, j, w)
            return E, G[free].ravel()

        X = X0.copy()
        X[free] = _minimize(f, X0[free].ravel()).reshape(-1, 3)
        prev = [(X[offs[i]:offs[i + 1]], rods[i][3]) for i in range(len(specs))]
    return [(X[offs[i]:offs[i + 1]], rods[i][1], rods[i][3]) for i in range(len(specs))]


def key(specs, up, meshes):
    h = hashlib.sha256()
    h.update(json.dumps([VERSION, LEVELS, SEG, EI, WEIGHT, MIN_BEND, CLEAR, K_STRETCH, K_BEND_MIN, K_CONTACT, SAMPLE, REACH, SLACK]).encode())
    h.update(np.round(np.asarray(up, float), 6).tobytes())
    for s in specs:
        h.update(s["name"].encode())
        h.update(np.round(np.asarray(s["path"], float), 3).tobytes())
        for p, t, k in s["anchors"]:
            h.update(np.round(np.asarray(p, float), 3).tobytes() + np.round(np.asarray(t, float), 4).tobytes() + k.encode())
        h.update(json.dumps([s["d"], sorted(s.get("exact", {}).items()), s.get("total"), s.get("remainder")]).encode())
        for k, fp in sorted(s.get("fixed", {}).items()):
            h.update(str(k).encode() + np.round(np.asarray(fp, float), 3).tobytes())
    for m in meshes:
        h.update(np.round(m.bounds, 2).tobytes())
        h.update(str(len(m.faces)).encode())
    return h.hexdigest()[:20]


def solve_all(tag, specs, up, meshes, study=None):
    """Solve the cables of specs together (each keeps clear of the others), with the solids meshes,
    gravity against up. Returns {name: dict(nodes, rest, spans, d, path)} and the time
    of the solve (0 from the cache). study: spans to solve again with other lengths for the sag sheet,
    each (cable name, span index, [cable length over the drawn length, mm]); each starts from the solved
    shape, with the other cables fixed."""
    from scipy.spatial import cKDTree
    os.makedirs(CACHE, exist_ok=True)
    k = key(specs, up, meshes)
    path = os.path.join(CACHE, f"{tag}-{k}.json")
    if os.path.exists(path):
        with open(path) as fh:
            data = json.load(fh)
        return {n: {kk: (np.array(v) if kk in ("nodes", "rest", "path") else v) for kk, v in c.items()}
                for n, c in data["cables"].items()}, data.get("study", []), 0.0
    t0 = time.time()
    field = Field(meshes, [np.asarray(s["path"], float) for s in specs])
    out, others = {}, {}
    for s, (x, rest, spans) in zip(specs, solve_joint(specs, up, field)):
        dense = _resample(x, int(np.ceil(_len(x))))
        others[s["name"]] = (cKDTree(dense), dense, s["d"] / 2)
        out[s["name"]] = dict(nodes=x, rest=rest, spans=spans, d=s["d"], path=np.asarray(s["path"], float))
    res_study = []
    for name, si, extras in study or []:
        s = next(c for c in specs if c["name"] == name)
        rest_of = [o for n, o in others.items() if n != name]
        rows = []
        for ex in extras:
            x, rest, spans = solve_one(dict(s, exact={si: ex}, init=out[name]["nodes"]), up, field, rest_of)
            i0, i1 = spans[si][0], spans[si][1]
            rows.append(dict(extra=ex, nodes=x[i0:i1 + 1].tolist(), length=spans[si][3], drawn=spans[si][4]))
        res_study.append(dict(cable=name, span=si, rows=rows))
    dt = time.time() - t0
    with open(path, "w") as fh:
        json.dump({"cables": {n: {kk: (v.tolist() if isinstance(v, np.ndarray) else v) for kk, v in c.items()}
                              for n, c in out.items()}, "study": res_study, "time": dt}, fh)
    return out, res_study, dt


# ---------------------------------------------------------------- the results
def stats(c, up):
    """For a solved cable: the length, the smallest bend radius (mm), and for each span (kind, the
    straight distance between its anchors, the length, the largest sag under the straight line)."""
    x, rest = c["nodes"], c["rest"]
    up = np.asarray(up, float) / np.linalg.norm(up)
    e = x[1:] - x[:-1]
    t = e / np.linalg.norm(e, axis=1)[:, None]
    ang = np.arccos(np.clip(np.einsum("ij,ij->i", t[1:], t[:-1]), -1, 1))
    hm = (rest[1:] + rest[:-1]) / 2
    rad = hm / np.maximum(ang, 1e-9)
    spans = []
    for i0, i1, kind, L, drawn in c["spans"]:
        spans.append(dict(kind=kind, chord=float(np.linalg.norm(x[i1] - x[i0])), length=L, drawn=drawn,
                          sag=sag(x[i0:i1 + 1], up)))
    return dict(length=float(np.linalg.norm(e, axis=1).sum()), min_radius=float(rad.min()), spans=spans)


def sag(seg, up):
    """The largest distance of the nodes under the straight line between the ends, along -up."""
    a, b = seg[0], seg[-1]
    ab = b - a
    t = np.clip((seg - a) @ ab / max(ab @ ab, 1e-9), 0, 1)
    q = a + t[:, None] * ab
    return float(np.max((q - seg) @ up))


def penetration(c, meshes):
    """The deepest a node of the cable goes into the solids (mm, 0 when it does not), and the nearest
    gap to them: trimesh distances to the surfaces, signed by a ray test where a mesh is closed."""
    import trimesh
    x = c["nodes"]
    r = c["d"] / 2
    best = float("inf")
    for m in meshes:
        b = m.bounds
        if np.any(b[1] < x.min(axis=0) - 20) or np.any(b[0] > x.max(axis=0) + 20):
            continue
        q = trimesh.proximity.ProximityQuery(m)
        # In chunks: one query of all the nodes against a large mesh uses more than 4 GB.
        for i in range(0, len(x), 10):
            _, d, _ = q.on_surface(x[i:i + 10])
            best = min(best, float(d.min()) - r)
    return best


def tube(path, d, sides=16):
    """A tube of diameter d along the polyline path, swept with a frame that does not twist."""
    import pod as P
    return P.tube(np.asarray(path, float), d, sides)
