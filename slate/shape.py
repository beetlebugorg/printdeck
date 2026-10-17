# Speakerdeck Slate: all dimensions. Units are millimetres.
#
# The case is a thin tall slab that leans back 15 degrees on a gray foot. The 7 inch screen is
# in portrait. The Pi 5 and the Amp2 sit on the outside of the back plate, as a backpack, so
# every port has free space for a straight plug and the bend of its cable. The parts:
#   slab        navy    the face and the walls, open at the bottom and at the rear
#   chin        orange  the bottom of the face: vent slots, two snap tongues
#   back        orange  the rear wall. It slides up from the bottom into grooves in the slab
#                       and the chin. The drive is on its front, the fan and the Pi on its rear.
#   foot        gray    the plate under the chin
#
# No cable shows on the front or the sides. The display cables run inside the chin, out
# through two notches in the bottom edge of the back plate, and up the rear: the HDMI ribbon
# at the left, the touch USB at the right. The drive cable goes through a notch in the top
# edge of the back plate, and down into the USB 3 port. Every plug ends inside the outline
# of the slab, so it does not show from the front.
#
# Slab coordinates (a, b, c): a to the right, b up the face, c out of the face (toward the
# viewer). c = 0 is the front face, b = 0 is the front bottom edge. The world has X = a,
# Z up, and the front toward -Y. The table is at Z = 0.

import math

TILT = math.radians(15)          # the face leans back
FOOT_H = 8.0                     # the slab stands on the foot
CLR = 0.3                        # clearance between two printed parts

# Slab and chin: the outer box.
W = 132.0                        # width (a from -W/2 to W/2)
H = 221.2                        # height along the face (b)
D = 35.0                         # depth (c from -D to 0)
WALL = 3.0                       # side walls, top wall, front skin, chin floor
R_TOP = 10.0                     # radius of the two top corners
EDGE = 1.0                       # chamfer of the front and rear edges
CHIN_B = 44.0                    # the chin is below this line

# The walls are 2 mm thicker in front of STEP_C, for the grooves of the chin tongues.
STEP_C = -24.0
STEP_T = 2.0

# Display: Adafruit 7 inch HDMI backpack, in portrait, sockets at the bottom.
DISP_C = (0.0, 131.6, -1.0)      # centre of the glass front; 1 mm behind the face
SCREEN_WH = (89.0, 157.0)        # visible area
OPENING_WH = (90.0, 158.0)
LCD_WH = (100.6, 166.6)          # pocket for the LCD, from the skin to the glass
EARS = [(x * 54.05, y * 78.55) for x in (-1, 1) for y in (-1, 1)]
BOSS_R = 3.8                     # bosses for M3 heat-set inserts
INSERT_D = 4.0
INSERT_DEPTH = 4.5               # 4 mm inserts: the hole stays 3.85 mm behind the face

# Back plate: the rear wall. Its edges sit in grooves in the slab and the chin.
PLATE_C = (-31.5, -29.0)         # c range of the plate
GROOVE = 1.2                     # depth of the grooves; the plate edge goes in GROOVE - CLR
GROOVE_C = (PLATE_C[0] - CLR, PLATE_C[1] + CLR)

# SSD: on the front of the plate, adapter up, its bottom (with the M3 holes) on the plate.
# The drive top (c = -22.2) stays behind the display plugs (c = -21.35).
SSD_O = (0.0, 94.0, PLATE_C[1])
SSD_HOLES = [(x * 30.86, SSD_O[1] + y) for x in (-1, 1) for y in (36.2, -40.4)]
SSD_USB_A = -22.7                # centre of the micro-B socket (case/hardware.scad, not measured)

# Pi 5 with the Amp2: on four bosses on the rear of the plate, parts to the rear. The USB and
# Ethernet edge points up, so the USB plugs are easy to connect. The USB-C, micro HDMI, Amp2
# power and speaker edge points left. The USB plugs and their cable bends stay below the top.
PI_A, PI_B = -2.0, 128.0         # centre of the Pi board
PI_BOSS_H = 19.0
PI_BOSS_R = 4.0                  # 8 mm bosses: 2 mm of wall around the heat-set insert
PI_HOLES = [(PI_A + y, PI_B + x) for x, y in ((-39, -24.5), (19, -24.5), (-39, 24.5), (19, 24.5))]

# 30 mm fan under the Pi, on four 5 mm bosses with M3 screws, blowing onto the Pi. A heatsink cools the SoC.
FAN_A, FAN_B = PI_A, PI_B
FAN_FEET = 5.0                   # height of the bosses under the fan: the air comes in here
FAN_HOLE = 12.0                  # the fan holes are on a 24 mm square
FAN_PILOT_R = 1.25               # M3 x 16 screw: 10 mm fan, 5 mm boss, 1 mm into the plate

# Chin tongues: they go up into the slab, with a ridge that snaps into a groove. A pin through
# the release hole in each side wall pushes the ridge out of its groove.
TONGUE_B = (38.0, 56.0)
RIDGE_B = 51.0

# Cable route.
VENTS_B = (24.0, 30.0, 36.0)
CABLE_NOTCHES = ((-25.0, -5.0), (-1.0, 21.0))   # a ranges of the notches for the touch USB
CABLE_NOTCH_B = 22.0                            # and the HDMI cable, up to this b
SSD_NOTCH = (SSD_USB_A - 4.0, SSD_USB_A + 4.0, 208.0)   # a0, a1, bottom b; in the plate top
TIES = ((-55.0, 70.0, 5.5), (52.0, 80.0, 5.5), (52.0, 150.0, 5.5))   # a, b, half width
TIE_HOLE_R = 1.6                                # for a 3 mm twist tie

# Foot. It reaches far to the rear: the backpack moves the centre of mass back.
FOOT_IN = 6.0                    # the foot is this much in from the front and the sides
FOOT_REAR = 60.0                 # and this much out at the rear
FOOT_HOOKS_Y = (12.0, 22.0)
FOOT_HOOK_X = 55.0               # inner face of the hook arm


def up():
    return (0.0, math.sin(TILT), math.cos(TILT))


def normal():
    return (0.0, -math.cos(TILT), math.sin(TILT))


def to_world(p):
    a, b, c = p
    u, n = up(), normal()
    return (a, b * u[1] + c * n[1], FOOT_H + b * u[2] + c * n[2])


def floor_b(c, extra=0.0):
    """The lowest b at depth c that is extra above the table plane of the slab bottom."""
    return (extra - c * math.sin(TILT)) / math.cos(TILT)
