"""The Z tower and the X end from the LulzBot CAD of the TAZ Workhorse v1.0.2, in the gantry frame
of shape.py. lulzbot/workhorse.py reads the solids from lulzbot/ref/lulzbot/final_assembly.fcstd.

The CAD axes are X to the left, Y up, Z to the back. Rot(0, 0, 180) * Rot(90, 0, 0) turns them
to x = -X, y = Z, z = Y. The offsets of shape.py then put the CAD on the measured places."""

import functools
import os
import sys

from build123d import Pos, Rot

import shape as S

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.append(os.path.abspath(os.path.join(HERE, "..", "..")))  # after this folder: lulzbot/ has its own shape.py and parts.py
import workhorse as W  # noqa: E402

# The solids of final_frame_01 near the arm: the front right upright and the parts on it, and the
# top of the right Z tower.
FRAME = {
    "upright": 1,
    "top front corner plate": 2,
    "bottom front corner plate": 28,
    "top inner corner bracket": 35,
    "bottom inner corner bracket": 33,
    "top plate": 12,
    "top cover": 18,
    "top block front": 11,
    "top block back": 4,
    "front Z rod": 17,
    "back Z rod": 20,
    "Z tower bar": 16,
}
# The solids of x_axis_01 that make the right X end: the body, the screws, and the bushings.
XEND = [6, 8, 20, 21, 22, 23, 24, 25]


def _place(shape, dx, dy, dz):
    return Pos(dx, dy, dz) * Rot(0, 0, 180) * Rot(90, 0, 0) * shape


@functools.lru_cache(maxsize=None)
def frame_base():
    """(name, shape) of the frame parts at Z 0."""
    solids = list(W.load("final_frame_01"))
    return [(n, _place(solids[i], S.CAD_DX, S.CAD_DY, S.CAD_DZ)) for n, i in FRAME.items()]


def frame(z=0.0):
    """The frame parts with the gantry at Z = z: they move by -z in the gantry frame."""
    return [(n, Pos(0, 0, -z) * s) for n, s in frame_base()]


@functools.lru_cache(maxsize=None)
def x_end():
    """The solids of the right X end. They move with the gantry."""
    solids = list(W.load("x_axis_01"))
    return [_place(solids[i], S.XEND_DX, S.XEND_DY, S.XEND_DZ) for i in XEND]
