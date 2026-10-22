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
#   clip      orange  two clips on the front left rail, each held by a twist nut in the slot.
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
HEAD_LEFT_OUT = 20.0             # the left face of the head is this far past the outer face of the pod
BACK_X = (RAIL_FRONT_X + 2.5, RAIL_FRONT_X + 5.5)   # the back panel: on the flanges of the rail clips
HEAD_TOP_DEPTH = 16.0            # from the back panel to the top edge of the face
POD_Z = 26.0                     # the outer face of the pod and the left face of the head
HEAD_TOP = (BACK_X[1] + HEAD_TOP_DEPTH, H)    # (x, y) of the top edge of the face
HEAD_ZC = POD_Z + HEAD_LEFT_OUT + HEAD_P[0]   # z of p = 0 (p points to -z)


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

# Rail clip: from the slot of the rail (front slot of the front left upright, in the LulzBot
# model): the opening is 6.15 mm wide at the face and 5.1 mm at its narrowest (the lips are
# round), the lip is 1.7 mm thick, and behind the lip the slot is 12.4 mm wide (16.6 mm deeper
# in) and 6.55 mm deep; measured in the model, the cavity behind the lips is a 45 degree V:
# 12.9 mm wide at 0.5 mm, 4.9 mm wide at 4.5 mm. A metal nut cannot go through a 5.1 mm
# opening. A printed twist nut (11.6 x 4.8 mm at the lips, with 45 degree sides like the V)
# goes in lengthwise, and turns a quarter turn behind the lips, until it is across the slot.
# Two opposite corners are round, so it turns easily; the model checks that it turns free of
# the rail. An M3 x 8 screw from a counterbore in the key of the clip cuts its own thread in a
# 2.5 mm hole in the nut and pulls the nut against the lips. The nut is 4.8 mm wide along the
# slot, so a 4 mm heat-set insert leaves 0.4 mm of plastic, and an M3 hex nut (5.5 mm across
# the flats) does not fit. Below the lips a round boss (4.8 mm, the width of the nut) goes
# down the V of the slot, so the 2.5 mm hole has at least 1.15 mm of plastic around it for
# the full 3.9 mm of thread. The boss is round, so it turns free at each angle. Clip
# coordinates: the slot centre and the rail face. The clip prints on its end, so its section
# is on the bed and it needs no supports; the nut prints flat.
CLIP_T = 2.5                     # the flange of the clip on the rail face
CLIP_L = 24.0                    # the length along the slot
CLIP_FLANGE_W = 16.0
CLIP_KEY = (8.0, 11.0)           # width of the dovetail key at the rear and at the front of the back panel
RAIL_LIP = 1.7                   # the lip of the slot
NUT = (11.6, 4.8, 4.0)           # the twist nut: across the slot (at the lips), along the slot, thick
NUT_GAP = 0.1                    # from the back of the lips to the nut (the screw pulls it up)
NUT_ROUND = 2.0                  # the radius of its two round corners
NUT_BOSS = (2.4, 4.4, 0.3)       # the round boss: radius, depth from the wide face, 45 degree chamfer at its end
CLIP_SCREW = 8.0                 # M3 x 8 from the counterbore into the nut
CLIPS_Y = (158.0, 214.0)         # y of the clip centres (the back panel sets their height)
CLIP_SLACK = 3.0                 # the dovetail slot is longer by this on each end
CLIP_DROP = CLIP_L + 2 * CLIP_SLACK   # the head drops this far from the windows into the dovetail slots

# Pod: on the cover. The Raspberry Pi 5 is on four bosses on the plate; its parts point out of
# the cover. The USB edge points to the front (to the cable window), the micro HDMI and USB-C
# edge points down. Pi coordinates (from the Slate): x toward the USB edge, y toward the GPIO
# header, z up from the bottom of the board.
POD_X = (-X_FRONT, BACK_X[0] - CLR)
X_IN = X_FRONT - WALL            # the inner face of the rear wall is at -X_IN
PI_X, PI_Y = 5.0, 179.0          # centre of the Pi board
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
    ((-65.0, -50.0), 166.0, "skin"),
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
PLATE_WINDOWS = [(-72.0, -52.0, 120.0, 152.0), (-72.0, -42.0, 172.0, 238.0), (-28.0, 18.0, 160.0, 198.0),
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
GRILLE_WIN = (-32.0, 25.0, 156.0, 240.0)   # x0, x1, y0, y1 (left of the USB port bodies)
# The grille snaps into a pocket in the pod. The pocket is the window in the skin and a rim on
# the inner face of the skin; its floor is a 45 degree ledge (no flat ceiling in the print).
# Each long side of the grille has a flexible tab (a 14 mm cantilever in the plane of the
# grille, printed flat, so it bends along the layers) with a ridge that clicks into a groove in
# the pocket wall. A notch at the bottom edge of the pocket is for a fingernail or a small
# screwdriver to lift the grille out.
GRILLE_SLOT_W = 3.2              # vent slots, the same width as the slots in the chassis
GRILLE_SLOT_PITCH = 6.4
GRILLE_CLR = 0.2                 # clearance around the grille in the pocket
GRILLE_T = 3.0                   # the grille thickness = the pocket depth (flush with the skin)
GRILLE_RIM = (3.5, 2.5)          # the rim on the inner face of the skin: width, height
GRILLE_LEDGE = 1.2               # the 45 degree ledge under the grille
GRILLE_TAB = (14.0, 2.0, 1.4)    # tab length, tab width, the slot between the tab and the grille
GRILLE_RIDGE = 0.6               # the ridge on the tab (the snap); the groove is 0.2 mm deeper
GRILLE_NOTCH = (10.0, 1.6)       # the fingernail notch: width, depth
