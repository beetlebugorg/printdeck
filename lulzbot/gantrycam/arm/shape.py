# Gantry camera arm for the TAZ Workhorse: all dimensions. Units are millimetres.
#
# The arm holds an Arducam B0561 camera in front of the front right upright, at the height of
# the nozzle. It hangs from the right X end, so it goes up and down with the gantry: the nozzle
# and the layer stay at the same height in the picture. Three printed parts:
#   - the clamp plate: on the left face of the X end (the face toward the bed), under the two M3
#     screws of that face.
#   - the upper arm: a beam from the plate forward to the back of the upright, and a drop down
#     beside the inner face of the upright. The upper screw goes through the arm and the plate.
#   - the camera bracket: around the front of the upright, low. Its front wall has a vertical slot
#     for the 1/4"-20 screw of the camera bracket base. Two M3 bolts hold it to the drop.
#
# Frame ("gantry frame"): the origin is the tip of the nozzle with the head at the right end of
# X (X 293).
#   x: to the right of the printer.  y: to the back.  z: up.
# The gantry frame goes up and down with Z. The frame parts (the upright, its corner plates and
# brackets, the top of the Z tower) do not: the fit check moves them by -Z for Z from 0 to 297.
#
# Sources:
#   - The LulzBot CAD of the TAZ Workhorse v1.0.2 (lulzbot/ref/lulzbot/final_assembly.fcstd,
#     read by lulzbot/workhorse.py). It gives the Z tower: the uprights, the corner plates and
#     brackets, the Z rods, the top plates, and the X end. The fit check uses these solids.
#   - Caliper and ruler measurements on the printer (2026-11-15, docs/measure/measured.json).
#     They place the CAD on the tool head and agree with it to 0.2 mm, except FACE_X (2.4 mm).
#   - The BLTouch offsets of the pad search (x 65.34, y -22.78), the BLTouch mount
#     (lulzbot/bltouch/mount), and the B0561 sizes (lulzbot/camera_models.py).
# Values marked ESTIMATE are not measured and not in the CAD. MEASURE.md lists the checks.

# --- The tool head at the right end of X (the parts that come near the arm)
PIN_X, PIN_Y = 65.34, -22.78     # the BLTouch pin (printer.cfg, from the pad search)
TAB_X1 = PIN_X + 4.0 + 2.0       # the right face of the BLTouch mount tab (half an ear and 2 mm of shelf)
TAB_Y0 = PIN_Y - 26.21 / 2       # the front of the tab (the ears are 26.21 long)
TAB_Z = (39.45, 46.45)           # the shelf of the mount, over the nozzle tip
MOUNT_PLATE = ((52.9, 59.4), (-35.0, -9.7), (23.7, 65.0))  # the plate of the BLTouch mount on the motor
BLTOUCH = ((PIN_X - 5.44, PIN_X + 5.44), (PIN_Y - 6.5, PIN_Y + 6.5), (3.3, TAB_Z[0]))

# --- The front right upright (2020)
UP_X0 = TAB_X1 + 11.0            # the inner face. Measured: 11 mm from the tab, head at the right end.
UP_Y0 = TAB_Y0 - 8.0             # the front face. Measured: the tab is 8 mm behind it.
UP_W = 20.0
PLATE_PROUD = 6.5                # a front corner plate (3.2 mm in the CAD) and its screw heads
TOP_PLATE_Z = 345.0              # the bottom of the top front corner plate over the bed. Measured.
Z_MAX = 297.0                    # printer.cfg position_max of Z

# --- The place of the LulzBot CAD in the gantry frame. The CAD axes: X to the left, Y up, Z to
# the back. x = -X + CAD_DX, y = Z + CAD_DY, z = Y + CAD_DZ - (Z position) for the frame.
CAD_DX = UP_X0 - 140.4           # the inner face of the front right upright is at X -140.4
CAD_DY = UP_Y0 + 10.0            # its front face is at Z -10
CAD_DZ = 540.2                   # the bed surface: the top front corner plate starts at Y -195.2,
                                 # 345 mm (measured) over the bed
# The X end in the CAD: the screws at Y -462.2 and -427.2 (35 mm apart; measured 35.44), the
# left face at X -125.9, the screws at Z 60. It moves with the gantry: these offsets put the CAD
# screws on the measured screws.
XEND_DX = CAD_DX - 2.37          # the left face: measured 16.87 mm from the upright, CAD 14.5
XEND_DY = CAD_DY + 0.18          # the screws: measured 50.18 mm behind the upright, CAD 50.0
XEND_DZ = 53.0 + 462.2           # the lower screw: 53 mm over the tip (measured)

# --- The left face of the X end with the two screws (the screws go left to right)
FACE_X = UP_X0 - 16.87           # Measured: from the screw face to the inner face of the upright
SCREW_Y = UP_Y0 + UP_W + 50.18   # Measured: the screws are 50.18 mm behind the back face of the upright
FACE_Y = (SCREW_Y - 7.0, SCREW_Y - 7.0 + 13.63)  # Measured width 13.63; the front edge 7 mm in
                                 # front of the screws (CAD). Behind the face the X end stands out.
SCREW_Z = (53.0, 53.0 + 35.44)   # Measured: the lower screw 53 mm over the tip; 35.44 mm apart
XEND_TOP = 103.5                 # CAD: the top of the X end over the tip
M3_HEAD_D = 5.26                 # Measured: the socket head
M3_HEAD_H = 3.0                  # M3 socket head (ISO 4762)
LOWER_CLEAR = 9.0                # Measured: from the lower screw head to the motor, head at the right end
UPPER_CLEAR = 25.0               # Measured: 25 mm or more from the upper screw head
HEAD_SPLIT_Z = 66.0              # ESTIMATE: the top of the motor and of the BLTouch mount plate
NOZZLE_Y_EST = -20.0             # ESTIMATE: the nozzle, for the aim of the camera in the images only

# --- The clamp plate (prints on its right face, x = FACE_X)
PLATE_T = 6.0                    # x thickness. With the lower head sunk in, 6 mm stays to the motor.
PLATE_Y = (FACE_Y[0] + 0.2, FACE_Y[1] - 0.2)
PLATE_Z = (48.0, 98.0)
CB_D = 6.0                       # counterbore for an M3 socket head (5.5 nominal, 5.26 measured)
CB_DEPTH = M3_HEAD_H             # the lower head is flush with the plate
HOLE = 3.4                       # M3 clearance

# --- The upper arm (prints upside down: the top face z = ARM_TOP on the bed)
ARM_TOP = XEND_TOP - 5.5         # under the top of the X end: if the X end reaches Z max, the arm does
BEAM_Z0 = 72.0                   # the bottom of the beam: over the motor top (HEAD_SPLIT_Z)
ARM_UNDER_HEAD = 3.0             # arm under the upper screw head
CB_ARM = 6.2                     # the counterbore of the upper screw in the arm
ARM_X0 = FACE_X - PLATE_T - 0.2 - ARM_UNDER_HEAD - M3_HEAD_H   # the left face of the arm over the plate
CORNER_Y1 = UP_Y0 + 18.5         # CAD: the back of the inner corner bracket at the top of the upright
BEAM_Y0 = CORNER_Y1 + 2.0        # the beam starts 2 mm behind that bracket
BEAM_Y1 = FACE_Y[0] - 1.0        # and ends 1 mm in front of the X end, right of the face
DROP_X = (TAB_X1 + 2.0, UP_X0 - 2.0)   # the drop: 2 mm right of the tab, 2 mm from the upright
DROP_Y = (UP_Y0 + 0.5, BEAM_Y0 + 12.0)  # from just behind the upright front back into the beam
DROP_Z0 = 10.0                   # the bottom of the drop
DROP_Z1 = 73.0                   # the top of the drop in front of the beam: 5 mm under the inner
                                 # corner bracket (78.2 mm over the tip at Z max, CAD)

# --- The camera bracket (prints on its bottom face z = CAM_Z0)
CAM_Z = (-4.0, 40.0)             # all of it under 40 mm: at Z max the top corner plate starts at
                                 # 345 - 297 = 48 mm over the tip
FRONT_GAP = 10.0                 # between the front face of the upright and the bracket (plates: 6.5)
WALL = 8.0
FRONT_Y = (UP_Y0 - FRONT_GAP - WALL, UP_Y0 - FRONT_GAP)
FRONT_X = (DROP_X[0], UP_X0 + UP_W + 8.0)
TONGUE_Y = (FRONT_Y[0], DROP_Y[0])                # from the front wall back to the drop
RIB_X = (FRONT_X[1] - WALL, FRONT_X[1])           # a rib at the right end of the front wall, back to the
RIB_Y = (FRONT_Y[1], FRONT_Y[1] + 4.0)            # gap: the wall is an L, stiff in y
JOINT_Z = (16.0, 32.0)           # the two M3 x 30 bolts from the bracket into the drop
JOINT_HOLE = 3.4
NUT_Y = DROP_Y[0] + 8.0          # the nut slots, open on the left face of the drop
NUT_AF = 5.8                     # M3 nut 5.5 across flats, plus play
NUT_T = 2.8                      # M3 nut 2.4 thick, plus play

# --- The camera (lulzbot/camera_models.py B0561) on its U bracket, with the 1/4"-20 screw
CAM_CASE = (35.84, 35.84, 11.15) # width, height, depth
CAM_BRACKET = 44.53              # the top of the case to the bottom of the base
CAM_PIVOT_H = 26.11              # the pivot axis to the bottom of the base
CAM_BASE_D = 25.75
SLOT_X = UP_X0 + UP_W / 2        # the slot: in front of the middle of the upright
SLOT_W = 6.8                     # for the 1/4"-20 screw (6.35)
SLOT_Z = (6.0, 32.0)             # the screw (about the lens height) over the tip
SLOT_CB_W = 11.5                 # the counterbore of the screw head on the back face
SLOT_CB_DEPTH = 4.0              # 4 mm of wall stays under the head, as on the corner camera link
LENS_Z = 14.0                    # the lens height in the pictures, over the tip

# --- The printer limits
NOZZLE_X = (-50.0, 293.0)
