"""The Pi box: a small box on the cover of the TAZ Workhorse for the Pi 5, the Active Cooler and
the boot drive. It shows two ports on its bottom: the Pi's own USB-C port (the power supply plugs
straight into the Pi, in a recess), and a USB-A socket for the printer (an Adafruit 908 panel-mount
extension). Two flat cables go from its front end to the display slab: HDMI and the touch USB.

This is the first layout: the box envelope, the Pi with its ports and plugs, and a check that no
two parts overlap. The printed parts (the base, the lid, the vents and the screws) come later.

Mount coordinates are those of shape.py: x to the front of the printer (the front face of the
chassis is at X_FRONT = 85), y up (0 is 14 mm over the bottom of the chassis), z out of the cover
(the cover face is at 0).

Pi coordinates (the same as the Slate model): x toward the USB and Ethernet ports, y toward the
GPIO header, z up from the bottom of the board. In the box, Pi x is mount x (the USB ports face the
front), Pi y is mount y (the USB-C edge is down), and Pi z is mount z (the parts face out).
"""

from dataclasses import dataclass

from build123d import Align, Axis, Box, Pos, fillet

import shape as S


def box(x0, x1, y0, y1, z0, z1):
    return Pos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2) * Box(abs(x1 - x0), abs(y1 - y0), abs(z1 - z0))


# The Pi 5 board and its parts, in Pi coordinates (from the Slate model, after the Pi 5 drawing
# RP-008347: the board is 85 x 56, its bottom at z = 0, 1.6 thick).
BOARD_T = 1.6
PI_PARTS = {
    "pi_board": (-42.5, 42.5, -28, 28, 0, BOARD_T),
    "pi_ethernet": (23.5, 44.5, -25.8, -9.8, BOARD_T, BOARD_T + 13.5),
    "pi_usb3": (27, 44.5, -5.5, 7.7, BOARD_T, BOARD_T + 15.6),
    "pi_usb2": (27, 44.5, 12.4, 25.6, BOARD_T, BOARD_T + 15.6),
    "pi_header": (-35.4, 15.4, 21.9, 27, BOARD_T, BOARD_T + 8.5),
    "pi_usbc": (-35.8, -26.8, -29.2, -21.8, BOARD_T, BOARD_T + 3.2),
    "pi_hdmi0": (-20.3, -13.1, -29.2, -21.6, BOARD_T, BOARD_T + 3),
    "pi_hdmi1": (-6.9, 0.3, -29.2, -21.6, BOARD_T, BOARD_T + 3),
    # The Active Cooler (RP-008187): 63.5 x 42.5, about 12 mm over the board, over the SoC. Its
    # place on the board is from the drawing (to check with the STEP of the Pi).
    "cooler": (-38.0, 25.5, -21.5, 21.0, BOARD_T, BOARD_T + 12.0),
}
PORT_FACE = 44.5        # Pi x of the faces of the USB ports
EDGE_FACE = -29.2       # Pi y of the faces of the USB-C and micro HDMI ports
STANDOFF = 3.0          # under the board: the parts under a Pi 5 are lower than this

# Plugs, in Pi coordinates. Each is the plug and its body, with the room for the cable bend.
# - The flash drive (RPi Flash Drive, drawing): 42 long with the plug, 12.2 x 6.5; 12 in the port.
# - The touch cable USB-A end (Adafruit 4109): a board 30 x 17 x 5 with the plug; about 12 in the port.
# - The printer cable (Adafruit 908): the male end into the Pi, about 38 long [E]; its cable bends down.
# - The HDMI end (Adafruit 3557, right angle): 17.5 x 15 x 14.5; it plugs into HDMI1 from below.
USB2_LOW = (BOARD_T + 0.8, BOARD_T + 7.8)       # the lower port of a stack, in z
USB2_HIGH = (BOARD_T + 8.6, BOARD_T + 15.6)


def plugs():
    f = PORT_FACE
    return {
        "drive": (f - 12, f + 30, -5.5 + 0.5, 7.7 - 0.5, BOARD_T + 1.0, BOARD_T + 7.5),
        "touch_usb_a": (f - 12, f + 18, 19.0 - 8.5, 19.0 + 8.5, USB2_HIGH[0], USB2_HIGH[0] + 5.0),
        "printer_plug": (f - 12, f + 26, 12.9, 25.1, USB2_LOW[0], USB2_LOW[1]),
        "hdmi_end": (-6.9 - 5.5, 0.3 + 5.5, EDGE_FACE - 14.5, -28.0, BOARD_T - 1.0, BOARD_T + 14.0),
    }


# A plug may sit in its own port.
PLUGGED = {("drive", "pi_usb3"), ("touch_usb_a", "pi_usb2"), ("printer_plug", "pi_usb2"), ("hdmi_end", "pi_hdmi1")}

# The printer socket (Adafruit 908, drawing): a flange 38.3 x 14.65 on the outside of the bottom,
# M3 screws 30 mm apart, and a body that goes 31 mm into the box. The body section is about
# 20 x 11 [E]. It is in front of the Pi ports, under the plugs.
SOCKET = (38.3, 14.65, 2.0)       # flange: along x, along z, thickness
SOCKET_BODY = (20.0, 11.0, 31.0)  # along x, along z, into the box (up)
# The power plug of the official supply (drawing): 24 long, 12 x 7. It goes up into a recess in
# the bottom, into the Pi's own USB-C port.
POWER_PLUG = (12.0, 7.0, 24.0)


@dataclass
class PiBox:
    pi: tuple          # mount position of the Pi origin (the centre of the board bottom)
    outer: tuple       # x0, x1, y0, y1, z0, z1 of the box, mount coordinates
    wall: float = 2.0

    def to_mount(self, x, y, z):
        return (self.pi[0] + x, self.pi[1] + y, self.pi[2] + z)


COVER_SCREW_Y = 240.0 - S.Y_BASE    # the cover screw that holds the box (mount y)
FRONT_GAP = 0.5                     # the front wall stops this far behind the chassis face


def layout(top=None):
    """The box. top is the mount y of its top; by default the board top edge is 8 mm over the
    cover screw."""
    wall = 2.0
    # Front: the plugs end inside the front wall, which is just behind the chassis face.
    x1 = S.X_FRONT - FRONT_GAP
    plug_front = max(p[1] for p in plugs().values())
    px = x1 - wall - 2.0 - plug_front
    # Bottom: the HDMI end is the lowest part. Top: the cover screw is inside, under the board.
    plug_low = min(p[2] for p in plugs().values())
    y0 = COVER_SCREW_Y + 8.0 - 28.0 - 3.0 - wall - (56.0)        # first guess, set below
    py = COVER_SCREW_Y + 8.0 - 28.0                               # the board top edge 8 mm over the screw
    if top is not None:
        py = top - wall - 3.0 - 28.0
    y0 = py + plug_low - 2.0 - wall
    y1 = py + 28.0 + 3.0 + wall
    x0 = px - 42.5 - 3.0 - wall
    z0 = 0.0
    pz = z0 + wall + STANDOFF
    z1 = pz + max(p[5] for p in PI_PARTS.values()) + 3.0 + wall    # 3 mm of air over the cooler and the ports
    return PiBox(pi=(px, py, pz), outer=(x0, x1, y0, y1, z0, z1), wall=wall)


def socket_x(b):
    """Mount x of the middle of the printer socket: in front of the Pi ports."""
    return b.pi[0] + PORT_FACE + 2.0 + SOCKET_BODY[0] / 2


def solids(b):
    """The box envelope and the parts in it, in mount coordinates: (name, shape)."""
    out = []
    x0b, x1b, y0b, y1b, z0b, z1b = b.outer
    zc = (z0b + z1b) / 2
    sx = socket_x(b)
    out.append(("socket_flange", box(sx - SOCKET[0] / 2, sx + SOCKET[0] / 2, y0b - SOCKET[2], y0b, zc - SOCKET[1] / 2, zc + SOCKET[1] / 2)))
    out.append(("socket_body", box(sx - SOCKET_BODY[0] / 2, sx + SOCKET_BODY[0] / 2, y0b + b.wall, y0b + b.wall + SOCKET_BODY[2] - b.wall,
                                   zc - SOCKET_BODY[1] / 2, zc + SOCKET_BODY[1] / 2)))
    # The power plug, in the Pi's USB-C port from below. Its tip is in the port.
    cx, _, cz = b.to_mount((PI_PARTS["pi_usbc"][0] + PI_PARTS["pi_usbc"][1]) / 2, 0, BOARD_T + 1.6)
    yt = b.pi[1] + EDGE_FACE + 1.0
    out.append(("power_plug", box(cx - POWER_PLUG[0] / 2, cx + POWER_PLUG[0] / 2, yt - 6.5 - POWER_PLUG[2], yt - 6.5,
                                  cz - POWER_PLUG[1] / 2, cz + POWER_PLUG[1] / 2)))
    # The cover screw: an M3 socket cap head on the floor of the box, at the front screw of the cover.
    sx2 = S.X_FRONT - 6.1
    out.append(("cover_screw", Pos(sx2, COVER_SCREW_Y, b.wall) * Box(5.5, 5.5, 3.0, align=(Align.CENTER, Align.CENTER, Align.MIN))))
    for name, (x0, x1, y0, y1, z0, z1) in list(PI_PARTS.items()) + list(plugs().items()):
        a, c = b.to_mount(x0, y0, z0), b.to_mount(x1, y1, z1)
        out.append((name, box(a[0], c[0], a[1], c[1], a[2], c[2])))
    x0, x1, y0, y1, z0, z1 = b.outer
    shell = box(x0, x1, y0, y1, z0, z1)
    shell = fillet(shell.edges().filter_by(Axis.Z), 6.0)
    inner = box(x0 + b.wall, x1 - b.wall, y0 + b.wall, y1 - b.wall, z0 + b.wall, z1 - b.wall)
    return [("box", shell - inner)] + out


def check(parts):
    """Pairs of parts that overlap (the box shell against every part, and the plugs against each
    other and the Pi). Volumes under 0.5 mm3 are touching faces."""
    bad = []
    names = [n for n, _ in parts]
    for i in range(len(parts)):
        for j in range(i + 1, len(parts)):
            (a, sa), (c, sc) = parts[i], parts[j]
            if {a, c} <= set(PI_PARTS) or (a, c) in PLUGGED or (c, a) in PLUGGED:
                continue
            # The power plug goes through the bottom wall into its recess, and the socket flange
            # is on the outside of the bottom, and its body through it.
            if {a, c} in ({"box", "power_plug"}, {"box", "socket_body"}, {"box", "socket_flange"}):
                continue
            v = (sa & sc).volume
            if v > 0.5:
                bad.append((a, c, round(v, 1)))
    return bad


if __name__ == "__main__":
    b = layout()
    x0, x1, y0, y1, z0, z1 = b.outer
    print(f"box {x1 - x0:.1f} long x {y1 - y0:.1f} high x {z1 - z0:.1f} out of the cover; "
          f"{y0 + S.Y_BASE:.1f} to {y1 + S.Y_BASE:.1f} mm over the chassis bottom")
    parts = solids(b)
    for a, c, v in check(parts):
        print(f"  overlap: {a} and {c}: {v} mm3")
