# Printdeck LulzBot: all dimensions. Units are millimetres.
#
# The mount is at the front left of the LulzBot TAZ Workhorse, below the TAZ Workhorse logo on
# the electronics chassis (the tall black box on the left of the printer). It wraps around the
# front left edge of the chassis:
#   - The head holds the 7 inch display in landscape, 15 degrees back from vertical, the same
#     as the Slate. Its face points to the front of the printer, the same as the LulzBot LCD.
#     It goes to the right past the chassis, in front of the front left rail (the 20 x 20
#     T-slot upright of the frame). The bed and the print do not go there.
#   - The pod is flat on the left side of the chassis (the aluminium cover). It holds the Pi 5,
#     and the printer USB and the power cables go through it to the back of the printer.
# The parts:
#   plate     orange  flat on the cover, under the pod. Screws hold it on the cover; the Pi is on it.
#   back      orange  the back of the head. Its dovetail slots hang on the keys of two clips in
#                     the front left rail, and two screws from the pod hold its left end.
#   head      navy    the face and the walls around the display. Four screws from the back of
#                     the back panel hold it.
#   pod       navy    the cover over the Pi. It slides up and in onto two hooks on the plate.
#   grille    white   the vent panel over the Pi. It snaps into a pocket of the pod.
#   clip      orange  two clips on the front left rail, each held by a hammer nut in the slot.
#   roll_nut  orange  an option: a printed roll-in nut with an M3 hex nut, until you have the
#                     metal T-nuts. It is not in the assembly; model.py checks it on its own.
# The orange edges of the plate and the back panel show as thin lines between the navy parts
# and the black printer.
#
# Mount coordinates (x, y, z): x to the front of the printer, y up, z out of the chassis cover
# (to the left of the printer). x = 0 is the middle of the chassis depth, x = 85 is the front
# face of the chassis, y = 0 is 14 mm above the bottom of the chassis, z = 0 is the outer face
# of the cover. workhorse.py has the printer coordinates.

import math

import workhorse as WH

CLR = 0.3                        # clearance between two printed parts
TILT = math.radians(15)          # the head leans back, the same as the Slate

W = WH.CHASSIS["z"][1] - WH.CHASSIS["z"][0]      # 170.0: the depth of the chassis
X_FRONT = W / 2                  # the front face of the chassis
Y_BASE = 14.0                    # y = 0 is this far above the bottom of the chassis
H = 246.0                        # the top of the plate and the pod: 4 mm below the logo
POD_Y0 = 111.0                   # the bottom of the plate and the pod: level with the head
T_PLATE = 2.5
WALL = 1.5                       # 3 lines of the 0.5 mm nozzle (Workhorse SE tool head)
SKIN = 1.5                       # the face of the head and the outer face of the pod
LIP = 1.0                        # the glass is 1 mm behind the face (a 1 mm lip, 0.6 mm pocket)
R_EDGE = 6.0                     # radius of the outer corners
R_IN = R_EDGE - WALL             # radius of the inner corners: the corner wall is as thick as the walls
EDGE = 1.0                       # chamfer of the outer edges of a face

# The front of the printer: the frame upright is 20 x 20 T-slot (HD-EX0062).
RAIL_FRONT_X = X_FRONT + 1.0     # the front face of the rail is 1 mm in front of the chassis
RAIL_SLOT_Z = 399.6 - WH.COVER_FACE_X    # the centre of the front slot of the rail

# Display: Adafruit 7 inch 800 x 480 40-pin TFT (product 2354) with the HDMI backpack and the
# AR1100 touch controller (product 2407). Portrait display coordinates (a, b, c): the panel
# outline centre, a across the short side, b along the long side toward the HDMI end is -b,
# c out of the glass. In the head the display is in landscape, in the native position of the
# panel (FPC at the bottom): the HDMI and USB sockets are at the right. Landscape coordinates
# (h, v): h = -b to the right, v = a up.
#
# Panel (YX700WV03-T datasheet c3003.pdf, ref/adafruit/): LCM outline 164.9 x 100.0, LCM with
# the touch panel 4.9 thick, touch panel outline 164.4 x 99.0, LCD active area 154.08 x 85.92.
# With the FPC at the bottom, the active area is 3.92 from the left edge and 4.78 from the
# top edge, so it is 1.49 left and 2.26 up from the centre of the outline.
LCM_AB = (100.0, 164.9)
LCM_T = 4.9
TP_AB = (99.0, 164.4)
AA_HV = (154.08, 85.92)
AA_OFF_HV = (1.49, -2.26)        # in the head (turned 180 degrees: the sockets at the left)
OPEN_MARGIN = 0.5                # the opening is 0.5 mm larger than the active area on each side
POCKET_HV = (LCM_AB[1] + 2 * CLR, LCM_AB[0] + 2 * CLR)
# Backpack board (Adafruit-7-HDMI-Backpack-PCB, ref/adafruit/*.brd): 165.1 x 116.2 with the four
# corner ears, 165.1 x 100.2 between the ears. Holes: 3.0 mm at 157.1 x 108.2.
BOARD_B = 165.1 / 2              # half length
BOARD_A = 100.2 / 2              # half width between the ears
EAR_A = 116.2 / 2                # half width over the ears
EAR_B = 8.0                      # the ears are 8 mm long
EARS = [(x * 54.1, y * 78.55) for x in (-1, 1) for y in (-1, 1)]   # (a, b)
# The stack from the Slate (it fits on the printed Slate): the board front is 7.35 behind the
# glass front. The panel with the touch panel is 4.9, so 2.45 is left for the FPC fold and tape.
BOARD_C = (-9.35, -7.35)
HDMI_A = 0.9                     # HDMI socket X1 (brd): a = 0.9
USB_A = -20.5                    # micro USB socket X2 (brd), for touch and power
BOSS_R = 3.8                     # bosses for M3 heat-set inserts
INSERT_D = 4.0                   # the pocket of every M3 heat-set insert (the same as the Slate)
# Every insert pocket is deep enough for the longest insert that fits there, plus 0.6 mm at the
# blind end for the plastic that the insert pushes ahead. A shorter insert is fine.
INSERT_BLIND = 0.6
INSERTS = {                       # place: (count, shortest, longest insert in mm)
    "head_back": (4, 3.0, 7.0),
    "back_flange": (2, 3.0, 7.0),
    "pod_block": (2, 3.0, 5.0),
}
INSERT_USE = {"head_back": 7, "back_flange": 7, "pod_block": 5}   # the length in the model
# The display board and the Pi have little load: their M3 x 6 screws cut their own thread in a
# 2.5 mm hole in the boss, with no insert. Do not tighten them more than snug.
TAP_D = 2.5
DISPLAY_TAP_DEPTH = 5.6          # the screw goes 4.4 mm into the display boss
PI_TAP_DEPTH = 4.6               # the screw goes 4.4 mm into the Pi boss; 1.4 mm of floor stays
# Socket cap head screws (ISO 4762), the only screws in this design.
CAP = {3: (5.5, 3.0), 5: (8.5, 5.0)}   # size: (head diameter, head height)
CBORE = {3: (6.0, 3.2)}               # counterbore: diameter, depth

# Head coordinates (p, q, r): p to the right, q up the face, r out of the face; the origin is
# the centre of the face. The head is 15 degrees back from vertical. The display is turned 180
# degrees from its native position: the HDMI and USB sockets are at the left (toward the pod),
# and the FPC edge of the panel is at the top. Set display_rotate=180 in printdeck.txt.
# The plugs are normal straight plugs; the band at the left is for them and the turn of their
# cables to the back. The screen opening is at the centre of the face, so the border is the
# same on the left and the right: the band at the right is the same width.
DISP_OFF = (-AA_OFF_HV[0], -AA_OFF_HV[1])   # the panel centre, from the head centre (p, q)
PLUG_BAND = 40.0                 # a straight plug (30 mm past the board edge) and the cable turn
_P_HALF = abs(DISP_OFF[0]) + BOARD_B + PLUG_BAND + CLR + WALL
_Q_HALF = abs(DISP_OFF[1]) + EAR_A + CLR + 1.0 + WALL
HEAD_P = (-_P_HALF, _P_HALF)     # outer faces of the side walls
HEAD_Q = (-_Q_HALF, _Q_HALF)
HEAD_LEFT_OUT = 20.0             # the left face of the head is this far past HEAD_POD_Z
BACK_X = (RAIL_FRONT_X + 2.5, RAIL_FRONT_X + 5.5)   # the back panel: on the flanges of the rail clips
HEAD_TOP_DEPTH = 16.0            # from the back panel to the top edge of the face
# The head is where it was with the first pod, 26 mm out of the cover. The pod is now deeper for
# the fan (POD_Z, below), and the head does not move.
HEAD_POD_Z = 26.0
HEAD_TOP = (BACK_X[1] + HEAD_TOP_DEPTH, H)    # (x, y) of the top edge of the face
HEAD_ZC = HEAD_POD_Z + HEAD_LEFT_OUT + HEAD_P[0]   # z of p = 0 (p points to -z)


def head_dirs():
    s, c = math.sin(TILT), math.cos(TILT)
    return (0.0, 0.0, -1.0), (-s, c, 0.0), (c, s, 0.0)   # p, q, r in mount coordinates


def head_origin():
    """The mount position of the head origin (the display centre on the face)."""
    s, c = math.sin(TILT), math.cos(TILT)
    qt = HEAD_Q[1]
    return (HEAD_TOP[0] + qt * s, HEAD_TOP[1] - qt * c, HEAD_ZC)


def head_pt(p, q, r):
    o = head_origin()
    P, Q, R = head_dirs()
    return tuple(o[i] + p * P[i] + q * Q[i] + r * R[i] for i in range(3))


def disp_hv(h, v, c=0.0):
    """A landscape display point (from the panel centre) in head coordinates."""
    return (DISP_OFF[0] + h, DISP_OFF[1] + v, -LIP + c)


def disp_ab(a, b, c=0.0):
    """A portrait display point in head coordinates (the display is turned 180 degrees)."""
    return disp_hv(b, -a, c)


# Head to back panel: four M3 x 10 socket cap screws from the back of the back panel (you fit
# them on the bench, before the head goes on the printer). Two go into columns in the right
# band, above and below the plugs; two go into blocks on the left wall. Head points (p, q).
HEAD_COLS = [(_P_HALF - WALL - CLR - 4.0, 48.0), (_P_HALF - WALL - CLR - 4.0, -48.0)]
HEAD_BLOCKS = [46.0, -46.0]      # q of the two blocks on the left wall, above and below the plugs
COL_R = 3.5

# Back panel. Two M3 x 12 socket cap screws from inside the pod, through a flange of the plate,
# into heat-set inserts in two bosses of the back panel, hold up the head. The two screws are
# 87 mm apart in height, so the head does not turn. At the right end, two printed clips in the
# front slot of the rail hold the back panel against the rail: each clip snaps into the slot,
# and its dovetail key goes into a slot in the back panel. You need no nut in the rail.
WINDOWS = [(158.0, 192.0, 2.8, 22.8), (116.0, 140.0, 2.8, 22.8)]   # y0, y1, z0, z1: display cables; drive cable
FLANGE_X = (82.3, None)          # the flange of the plate; its front is the back panel (CLR)
FLANGE_SCREWS = [(149.0, 19.0), (232.0, 19.0)]   # (y, z)
FLANGE_HALF = 6.0                # half height of each flange block
FLANGE_Z = (12.5, 23.2)          # the flange block (its underside slopes 45 degrees to a web in front of the cables)
# A heat-set insert often stands a little proud of the rear face of the back panel, with a ring of
# melted plastic around it. The gap between the flange and the back panel is only CLR, so a relief
# around each flange screw hole takes the insert and the ring: radius, depth into the flange.
FLANGE_RELIEF = (3.6, 1.0)

# Rail clip: from the slot of the rail (front slot of the front left upright, in the LulzBot
# model): the opening is 6.15 mm wide at the face and 5.1 mm at its narrowest (the lips are
# round), the lip is 1.7 mm thick, and behind the lip the slot is 12.4 mm wide (16.6 mm deeper
# in) and 6.55 mm deep; measured in the model, the cavity behind the lips is a 45 degree V:
# 12.9 mm wide at 0.5 mm, 4.9 mm wide at 4.5 mm. The rail is 2020 extrusion with a 6 mm slot.
# The LulzBot BOM puts Misumi HNTAP5-5 drop-in T-nuts (M5) in this frame. Each clip has a metal
# M3 roll-in T-nut for a 2020 6 mm slot (the uxcell nut with a spring ball): it rolls in through
# the opening, behind the lips. An M3 x 8 screw from a counterbore in the key of the clip
# pulls the nut against the lips. A printed nut failed: it must be less than 5.1 mm wide to go
# in, so the plastic around the M3 thread was too thin to hold. Clip coordinates: the slot
# centre and the rail face. The clip prints on its end, so its section is on the bed and it
# needs no supports.
CLIP_T = 2.5                     # the flange of the clip on the rail face
CLIP_L = 24.0                    # the length along the slot
CLIP_FLANGE_W = 16.0
CLIP_KEY = (8.0, 11.0)           # width of the dovetail key at the rear and at the front of the back panel
RAIL_LIP = 1.7                   # the lip of the slot
# The hammer nut, a typical M3 nut for a 2020 6 mm slot: across the slot, along the slot, thick.
# Its lower part has 45 degree sides like the V of the slot, below the straight upper part.
HAMMER_NUT = (10.0, 6.0, 4.0)
HAMMER_STRAIGHT = 1.2            # the straight upper part of its sides
NUT_GAP = 0.1                    # from the back of the lips to the nut (the screw pulls it up)
CLIP_SCREW = 8.0                 # M3 x 8 from the counterbore into the nut: 3.9 mm of thread.

# Printed roll-in nut: an option until you have the metal T-nuts. A printed nut holds a standard
# M3 hex nut (after the "T-NUT for 2020 profile" of nisi1t, Thingiverse 5155562, CC BY, which slides
# in from an end of the rail). Its section across the slot is 7.1 mm wide and 3.6 mm thick, with
# 1.4 mm rounds. It goes edge first into the opening and rolls in behind the lips at any point
# of the rail: roll_nut.py checks the path in the section of the rail of the LulzBot model, with
# 0.25 mm of clearance. A wider section does not roll in. The hex nut lies flat in a pocket from
# the bottom, with its flats across the slot. The M3 x 8 screw pulls the nut up until its round
# shoulders are on the round backs of the lips.
ROLL_NUT_HALF = 4.0              # half the width of the top face, before the rounds
ROLL_NUT_STRAIGHT = 0.6          # the straight part of the sides; then 45 degrees to the bottom
ROLL_NUT_T = 3.6                 # thick
ROLL_NUT_BOTTOM = 2.75           # half the width of the bottom, before the rounds
ROLL_NUT_ROUND = 1.4             # the round of the corners
ROLL_NUT_L = 11.0                # along the slot
ROLL_NUT_TOP = 1.3               # the top face behind the rail face, with the nut pulled up on the lips
HEX_NUT = (5.5, 2.4)             # the M3 hex nut: across the flats, thick
HEX_TOP = 1.2                    # the hex nut is this far under the top face of the printed nut
HEX_POCKET = 5.6                 # the pocket across the flats: a press fit
ROLL_CLEAR = 0.25                # the clearance of the roll-in check


def _clip_half_plane(poly, a, b, c):
    """The part of the convex polygon poly where a * w + b * u <= c."""
    out = []
    for i, p in enumerate(poly):
        q = poly[(i + 1) % len(poly)]
        fp, fq = a * p[0] + b * p[1] - c, a * q[0] + b * q[1] - c
        if fp <= 0:
            out.append(p)
        if fp * fq < 0:
            t = fp / (fp - fq)
            out.append((p[0] + t * (q[0] - p[0]), p[1] + t * (q[1] - p[1])))
    return out


def rounded(poly, r, n=10):
    """The convex polygon poly with its corners rounded with radius r: the polygon moved in by r,
    then out by r. Where an edge is too short for two rounds, the rounds meet."""
    area = sum(p[0] * q[1] - q[0] * p[1] for p, q in zip(poly, poly[1:] + poly[:1]))
    if area < 0:
        poly = poly[::-1]
    # Each edge as an outward normal (nx, nu) and an offset: nx * w + nu * u <= c.
    lines = []
    for p, q in zip(poly, poly[1:] + poly[:1]):
        dw, du = q[0] - p[0], q[1] - p[1]
        L = math.hypot(dw, du)
        nx, nu = du / L, -dw / L
        lines.append((nx, nu, nx * p[0] + nu * p[1]))
    inner = [(-1e3, -1e3), (1e3, -1e3), (1e3, 1e3), (-1e3, 1e3)]
    for nx, nu, c in lines:
        inner = _clip_half_plane(inner, nx, nu, c - r)
    out = []
    for i, p in enumerate(inner):
        a0 = inner[i - 1]
        a1 = inner[(i + 1) % len(inner)]
        # the outward normals of the two edges at p
        n0 = math.atan2(-(p[0] - a0[0]), p[1] - a0[1])
        n1 = math.atan2(-(a1[0] - p[0]), a1[1] - p[1])
        if n1 < n0:
            n1 += 2 * math.pi
        k = max(1, int(n * (n1 - n0) / (math.pi / 2)))
        for j in range(k + 1):
            t = n0 + (n1 - n0) * j / k
            out.append((p[0] + r * math.cos(t), p[1] + r * math.sin(t)))
    return out


def roll_nut_section():
    """The section of the printed roll-in nut across the slot, as (w, u) points: w across the
    slot from its middle, u down from the top face."""
    a, k, t, b = ROLL_NUT_HALF, ROLL_NUT_STRAIGHT, ROLL_NUT_T, ROLL_NUT_BOTTOM
    return rounded([(-a, 0.0), (a, 0.0), (a, k), (b, t), (-b, t), (-a, k)], ROLL_NUT_ROUND)
# An M3 x 10 reaches 7.7 mm behind the rail face, past the bottom of the slot (6.5 mm).
CLIPS_Y = (158.0, 214.0)         # y of the clip centres (the back panel sets their height)
CLIP_SLACK = 3.0                 # the dovetail slot is longer by this on each end
CLIP_DROP = CLIP_L + 2 * CLIP_SLACK   # the head drops this far from the windows into the dovetail slots

# Pod: on the cover. The Raspberry Pi 5 is on four bosses on the plate; its parts point out of
# the cover. The USB edge points to the front (to the cable window), the micro HDMI and USB-C
# edge points down. Pi coordinates (from the Slate): x toward the USB edge, y toward the GPIO
# header, z up from the bottom of the board.
POD_X = (-X_FRONT, BACK_X[0] - CLR)
X_IN = X_FRONT - WALL            # the inner face of the rear wall is at -X_IN
# The plugs need room. The USB plugs at the front edge are about 40 mm long, so the Pi is 15 mm
# farther from the back panel than at first. The micro HDMI plug at the bottom edge is about 35 mm
# with its strain relief, and its cable then turns to the front with a bend of 20 mm radius above
# the bottom wall, so the Pi is 22 mm higher. The plug must sit straight in the Pi: a sharp bend
# at the plug loosens it, and the display signal drops.
PI_X, PI_Y = -10.0, 201.0        # centre of the Pi board
PLUG_USB_A = 40.0                # a USB-A plug and the stiff part of its cable, from the port face
PLUG_MICRO_HDMI = 35.0           # a micro HDMI plug with its strain relief, from the port face
PLUG_USB_C = 30.0                # the USB-C power plug with its strain relief
HDMI_BEND = 20.0                 # the least bend radius of the HDMI cable
PI_BOSS_H = 3.5
PI_BOSS_R = 3.5
PI_Z = T_PLATE + PI_BOSS_H       # bottom of the Pi board
PI_HOLES_PI = [(-39, -24.5), (19, -24.5), (-39, 24.5), (19, 24.5)]


def pi_pt(x, y, z):
    """A point in Pi coordinates, in mount coordinates."""
    return (PI_X + x, PI_Y + y, PI_Z + z)


PI_HOLES = [pi_pt(x, y, 0)[:2] for x, y in PI_HOLES_PI]
PI_HOLE_D = 3.2                  # drill the four 2.7 mm holes of the Pi to 3.2 mm for M3

# Mounting on the cover. The printer has cover PP-FP0164 (10 screws). The plate uses its upper
# pair of screws: the front one and the rear one, 3.5 mm in from the edges of the cover. Each
# entry: (mm above the bottom of the electronics chassis, mm from the front face of the chassis).
# Height: measured on the printer, 237.3 mm from the bottom of the cover to the middle of the
# front screw. The bottom of the cover is 2.5 mm above the bottom of the chassis (LulzBot model),
# so the screw is 239.8 mm above the bottom of the chassis (the drawing gives 240.0).
# Distance from the front face: measured on the printer, 3.6 mm from the front edge of the cover
# to the middle of the front screw (calipers, about +-0.5 mm). The cover is 2.5 mm in from the
# front face of the chassis, so the screw is 6.1 mm from the front face. The drawing gives
# 3.5 mm on the cover (6.0 mm): the two agree within the measurement. The rear screw is from the
# drawing, at the same height, mirrored: 3.6 mm from the rear edge of the cover (163.9 mm from the
# front face); it is to be confirmed. Type new values here and run make.sh. Each cover screw
# under the plate changes to a longer M3 socket cap screw with a washer: the plate pad is 4 mm
# thick, so an M3 x 10 goes 3.5 mm into the PEM nut of the chassis (the old screw: 4.0 mm). The holes in
# the plate are slots, MOUNT_SLOT mm long up and down and MOUNT_SLOT_W mm wide (+-0.5 mm front to
# back for the measurement), so a few mm of difference fits.
COVER_SCREWS_MM = [(239.8, 6.1), (239.8, 163.9)]
COVER_SCREW_NOTES = ["front: measured (237.3 mm up, 3.6 mm from the cover edge)",
                     "rear: from the drawing, mirrored; to be confirmed"]
MOUNT_SLOT_W = 4.4
COVER_SCREWS_CONFIRMED = False
MOUNT_SLOT = 10.0
PAD_T = 4.0
PAD_R = 3.9                      # half width of each pad (a washer is 7 mm)
MOUNT_SCREWS = [(X_FRONT - f, b - Y_BASE) for b, f in COVER_SCREWS_MM]   # (x, y) in mount coordinates

# Cable exits: two notches in the rear wall of the pod, at the rear face of the chassis. The
# printer USB cable goes out at the top, the power cable at the bottom. Behind the printer
# they go to the RAMBo USB socket and to the 5 V supply. (y0, y1, z1); z0 is the plate.
EXITS = [(113.5, 122.5, 11.0), (124.5, 134.5, 11.0)]

# Pod to plate. The pod slides up and in (45 degrees) onto two hooks on the plate. Two M3 x 10
# screws go up through the bottom wall of the pod into heat-set inserts in two blocks on the
# plate. They pull the pod up, and the 45 degree hooks pull it onto the plate.
HOOKS = [
    # (x0, x1) of the fin (inner end first), y of the top of the fin base, the block hangs on
    ((-73.0, -58.0), 166.0, "skin"),     # behind the rear end of the Pi: the printer cable goes down there
    ((55.0, 72.0), 150.0, "skin"),
]
FIN_T = 3.0
FIN_L = 7.0
HOOK_FRONT = 20.0
BOTTOM_SCREWS_X = (40.0, 70.0)
BOTTOM_SCREW_Z = 7.5
BOTTOM_BOSS_TOP = 6.0
BOTTOM_GAP = 0.6
BLOCK_Y = (BOTTOM_BOSS_TOP + BOTTOM_GAP, 14.0)  # above POD_Y0; the HDMI cable is above it

# Windows in the plate and the back panel (less filament): rounded rectangles away from the
# screws, the bosses, and the hooks. Plate: (x0, x1, y0, y1). Back panel: (z0, z1, y0, y1).
PLATE_WINDOWS = [(-72.0, -52.0, 120.0, 152.0), (-72.0, -56.0, 172.0, 238.0), (-40.0, 2.0, 182.0, 220.0),
                 (30.0, 72.0, 160.0, 220.0), (-38.0, 28.0, 126.0, 148.0)]

# The 2.5 inch drive (the boot drive) with its USB adapter, in the head: flat on the inner face of
# the back panel, behind the display, adapter toward the pod. Four M3 x 6 socket cap screws from
# the rear of the back panel hold it. The drive is on four pads 0.5 mm high, so the screws go
# 2.5 mm into the drive (a 2.5 inch drive allows 3.0 mm, some only 2.5 mm). Its micro-B plug points to the pod; the cable goes through
# the lower window of the back panel to the lower USB 3 port. From the Slate model.
DRIVE_PAD = 0.5                  # the pads under the drive, at its four holes
DRIVE_Y = 150.5                  # centre of the drive (across its width)
DRIVE_Z_ADAPTER = -13.0          # the end of the adapter board
DRIVE_LEN, DRIVE_ADAPTER = 50.2, 86.3   # half length of the drive; the adapter end, from the centre
DRIVE_USB = 22.7                 # the micro-B socket, from the centre line (from the Slate)
DRIVE_HOLES = [(36.2, 30.86), (36.2, -30.86), (-40.4, 30.86), (-40.4, -30.86)]   # (along, across)
MICRO_B_LEN = 22.0
# Windows in the back panel (less filament), clear of the screws, the clip slots, and the cable windows.
BACK_WINDOWS = [(118.0, 236.0, -194.0, -146.0), (190.0, 236.0, -100.0, -30.0), (118.0, 186.0, -100.0, -70.0),
                (125.0, 176.0, -56.0, -10.0)]   # y0, y1, z0, z1

# Grille: a window in the pod over the Pi, with a white panel in it. Two posts on the plate
# hold its flange against the skin.
GRILLE_WIN = (-47.0, 10.0, 156.0, 240.0)   # x0, x1, y0, y1 (left of the USB port bodies)
# The grille snaps into a pocket in the pod. The pocket is the window in the skin and a rim on
# the inner face of the skin; its floor is a 45 degree ledge (no flat ceiling in the print).
# Each long side of the grille has a flexible tab (a 14 mm cantilever in the plane of the
# grille, printed flat, so it bends along the layers) with a ridge that clicks into a groove in
# the pocket wall. A small screwdriver in a vent slot lifts the grille out. The pocket has no
# fingernail notch: with the deeper rim, a notch in its wall crashes CuraEngine in the support check.
GRILLE_SLOT_W = 3.2              # vent slots, the same width as the slots in the chassis
GRILLE_SLOT_PITCH = 6.4
GRILLE_CLR = 0.2                 # clearance around the grille in the pocket
GRILLE_T = 3.0                   # the grille thickness = the pocket depth (flush with the skin)
GRILLE_RIM = (3.5, 4.5)          # the rim on the inner face of the skin: width, height (past the fan plate)
GRILLE_LEDGE = 1.2               # the 45 degree ledge under the grille
GRILLE_TAB = (14.0, 2.0, 1.4)    # tab length, tab width, the slot between the tab and the grille
GRILLE_RIDGE = 0.6               # the ridge on the tab (the snap); the groove is 0.2 mm deeper

# Fan: a 30 x 30 x 10 mm 5 V fan with the Pi 5 fan plug (4-pin JST-SH), the same as the Slate.
# It is inside the pod, over the SoC of the Pi, and it blows onto the Pi. The air comes in through
# the grille. The grille is the flat grille of the first pod: the user has it printed, so its
# shape stays the same, and its pocket moved with the Pi so that its vent slots are over the fan.
# The fan is on a fan plate (a separate part): a flat plate under the grille, in a deeper part of
# the grille pocket, with a round hole for the air. The grille holds the fan plate on the 45 degree
# ledge of the pocket. A part of the pod cannot hold the fan: the fan is under the middle of the
# grille window, and a part there has nothing under it in the print of the pod (outer face down).
# The fan plate goes in from the outside, so the fan comes out with it for service, on its cable.
# The fan holes of a 30 mm fan are on a 24 mm square, 3.2 to 3.4 mm across, through the 10 mm frame.
# Four posts stand from the fan plate, one into each hole. Two diagonal posts are solid: they
# locate the fan. The other two are hollow, with a pilot hole for an M2.5 self-tapping screw. The
# screw goes into the hole from the far face of the fan (the face to the Pi), and cuts its thread
# in the post and then in the fan plate. The fan hole holds the thin wall of the post from outside.
# A screw of 8 to 11 mm fits: an 8 mm screw has 2 mm of thread in the post, a 10 mm screw 4 mm.
# The fan plate prints flat, grille side down, so the posts stand up from the bed with their layers
# across them. They are short, and the screws hold the fan on the plate, so they carry little load.
# The cable leaves the fan frame from a side, beside a corner hole: it does not touch the posts or
# the screw heads, at any corner.
FAN = (30.0, 10.0)               # size, thickness
FAN_XY = (PI_X - 12.0, PI_Y + 2.5)   # the centre of the fan: over the centre of the SoC
FAN_AIR = 1.0                    # the least air between the fan and the highest heatsink
HEATSINK_H = 13.0                # the highest heatsink in the model
FAN_PLATE_T = 2.0                # the fan plate, under the grille
FAN_HOLE = 28.0                  # the hole for the air in the fan plate
FAN_PLATE_EDGE = 0.8             # the chamfer of the fan plate on the 45 degree ledge
FAN_HOLES = 12.0                 # the fan holes, from the centre of the fan (a 24 mm square)
FAN_HOLE_D = 3.3                 # a fan hole in the model
POST = (3.0, 4.0, 0.5)           # the posts: diameter (a slip fit in the fan hole), length into the hole, tip chamfer
PILOT = (2.0, 1.5)               # the pilot hole for the M2.5 screw: diameter, depth into the fan plate
FAN_SCREW = (2.5, 4.5, 1.8, 10.0)   # M2.5 round head: diameter, head diameter, head height, length
# The outer face of the pod: the grille, the fan plate, the fan, the air, and the highest heatsink
# on the SoC of the Pi, in 0.5 mm steps.
POD_Z = math.ceil(2 * (PI_Z + 1.5 + 2.2 + HEATSINK_H + FAN_AIR + FAN[1] + FAN_PLATE_T + GRILLE_T)) / 2
