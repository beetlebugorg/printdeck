"""The tall case: one case for the display, the Pi 5 and the boot drive, in portrait, for two
printers. The TAZ Workhorse has the 7 inch display, and the Mini 2 the 5 inch display. The face
leans back 15 degrees. This file is the first layout: the envelope of the case and the places of
the parts in it, for renders and fit checks. The printed parts come later.

Mount coordinates are the coordinates of shape.py: x to the front of the printer (the front face
of the chassis is at X_FRONT), y up (0 is Y_BASE above the bottom of the chassis), z out of the
cover (the cover face is at 0, and the chassis face goes to about -102.6).

The case stands on the flanges of the two rail clips, in front of the rail. A nub wraps around the
front edge of the cover and holds the cover screw that is 240 mm up. The hole in the nub is a slot
front to back: the clips set the depth, and the screw only holds the case against the cover.
"""

import math
from dataclasses import dataclass

import shape as S

TILT = S.TILT
SIN, COS, TAN = math.sin(TILT), math.cos(TILT), math.tan(TILT)


@dataclass
class Display:
    name: str
    lcm: tuple          # panel outline (across, along), in portrait
    lcm_t: float        # panel thickness with the touch panel
    aa: tuple           # active area (across, along)
    board: tuple        # board outline (across, along), over the ears
    holes: tuple        # mounting holes (across, along) from the board center, one quarter
    board_front: float  # from the front of the glass to the front of the board
    socket_band: float  # past the bottom edge of the board: the HDMI plug and its adapter board


# The 7 inch: Adafruit 2406 (panel 2354, 800 x 480, backpack with AR1100 touch). Values from
# shape.py (datasheet and the backpack .brd).
SEVEN = Display("7in", lcm=(100.0, 164.9), lcm_t=4.9, aa=(85.92, 154.08), board=(116.2, 165.1),
                holes=(54.1, 78.55), board_front=7.35, socket_band=18.0)
# The 5 inch: Adafruit 2260 (panel 1680, 800 x 480, backpack with touch). From the STEP files in
# ref/adafruit/: the board is 91.8 x 120.7 with 3.0 mm holes at 84.2 x 112.7, and the panel is
# 1.6 mm in front of the board. The active area 64.8 x 108.0 is the usual one for this panel (to
# be checked).
FIVE = Display("5in", lcm=(75.8, 120.7), lcm_t=3.0, aa=(64.8, 108.0), board=(91.8, 120.7),
               holes=(42.1, 56.35), board_front=4.6, socket_band=18.0)

# The Pi 5 (RP-008347 drawing; the STEP in ref/raspberrypi/): the board is 85 x 56 x 1.6, and the
# USB and Ethernet stack is 16 mm over the board. The Active Cooler (RP-008187) is 63.5 x 42.5,
# about 12 mm over the board.
PI = (56.0, 85.0, 1.6)            # across, along (vertical), thickness
PI_STANDOFF = 3.5
PI_TALL = 16.0                    # the highest part over the board: the USB and Ethernet stack
PORT_OVER = 2.5                   # the ports go past the top edge of the board
USB_LOWER = 4.3                   # the center of the lower USB port over the board
# The boot drive: SanDisk Extreme PRO USB 3.2 (SDCZ880), 70.9 x 21.3 x 11.4. About 12 mm of it is
# the plug, in the port.
STICK = (21.3, 70.9, 11.4)
STICK_IN = 12.0

T_BACK = 2.5                      # the back wall
BOARD_BACK_PARTS = 3.0            # parts on the back of the display board, away from the sockets
GAP = 1.0                         # air between two parts


@dataclass
class Layout:
    d: Display
    width: float      # outer width of the case (z)
    z_mid: float      # z of the middle of the case
    y_top: float      # the top edge of the face
    y_bot: float      # the bottom edge of the face
    x_back: float     # the outer face of the back wall
    depth_top: float  # from the back wall to the face, at the top
    pi_y: tuple       # bottom and top of the Pi board
    stick_y: tuple    # bottom and top of the stick

    def face_x(self, y):
        """x of the face at the height y."""
        return self.x_back + self.depth_top + (self.y_top - y) * TAN

    def depth_bot(self):
        return self.face_x(self.y_bot) - self.x_back


def layout(d, y_top, x_back, z_mid):
    """The case for the display d: the face from y_top down, its length from the display, and the
    least depth at the top that leaves room for the Pi and the stick behind the display board."""
    along = S.WALL + 1.0 + d.board[1] + d.socket_band + S.WALL     # the face, along its slope
    y_bot = y_top - along * COS
    width = d.board[0] + 2 * S.CLR + 2 * S.WALL
    xi = x_back + T_BACK                                           # the inner face of the back wall
    pi_bot = y_bot + S.WALL + 6.0
    pi_top = pi_bot + PI[1]
    stick_bot = pi_top + PORT_OVER
    stick_top = stick_bot + STICK[1] - STICK_IN
    # The back of the display board, at the face depth: LIP, board_front, the board 1.6, and the
    # parts on its back, across the face (divide by cos for x at one height).
    stack = (S.LIP + d.board_front + 1.6 + BOARD_BACK_PARTS + GAP) / COS
    # In front of the Pi: the USB stack. In front of the stick: the stick in the lower port.
    need_pi = PI_STANDOFF + PI[2] + PI_TALL + GAP
    need_stick = PI_STANDOFF + PI[2] + USB_LOWER + STICK[2] / 2 + GAP
    # face_x(y) - stack >= xi + need at each height. face_x falls with y, so the top of each part
    # is the tightest place.
    dt = 0.0
    for y, need in ((pi_top + PORT_OVER, need_pi), (stick_top, need_stick)):
        dt = max(dt, T_BACK + need + stack - (y_top - y) * TAN)
    dt = max(dt, T_BACK + stack + 2.0)
    return Layout(d, width, z_mid, y_top, y_bot, x_back, math.ceil(dt), (pi_bot, pi_top), (stick_bot, stick_top))


# The Workhorse: the top of the face 2 mm under the logo (264 mm over the bottom of the chassis),
# the back on the clip flanges, and the side toward the bed 10 mm past the slot of the rail, so
# that the clips are under the case.
WORKHORSE_TOP = 262.0 - S.Y_BASE


def workhorse():
    w = SEVEN.board[0] + 2 * S.CLR + 2 * S.WALL
    z_right = S.RAIL_SLOT_Z - 10.0
    return layout(SEVEN, WORKHORSE_TOP, S.BACK_X[0], z_right + w / 2)


if __name__ == "__main__":
    for name, L in (("Workhorse 7in", workhorse()),):
        print(f"{name}: {L.width:.1f} wide, face {L.y_bot + S.Y_BASE:.1f} to {L.y_top + S.Y_BASE:.1f} mm over the "
              f"chassis bottom, depth {L.depth_top:.0f} at the top and {L.depth_bot():.1f} at the bottom, "
              f"z {L.z_mid - L.width / 2:.1f} to {L.z_mid + L.width / 2:.1f}")
