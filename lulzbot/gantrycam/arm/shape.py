# Gantry camera arm for the TAZ Workhorse: all dimensions. Units are millimetres.
#
# The arm holds an Arducam B0561 camera in front of the front right upright, at the height of
# the nozzle. It hangs from the right X end, so it goes up and down with the gantry: the nozzle
# and the layer stay at the same height in the picture. Three printed parts:
#   - the clamp plate: in the pocket of the X end face, under the two M3 screws of that face.
#   - the upper arm: a beam above the tool head motor from the plate to the upright, and a drop
#     down along the inner face of the upright. The upper screw goes through the beam and the plate.
#   - the camera bracket: around the front of the upright, low. Its front wall has a vertical slot
#     for the 1/4"-20 screw of the camera bracket base. Two M3 bolts hold it to the drop.
#
# Frame ("gantry frame"): the origin is the tip of the nozzle with the head at the right end of
# X (X 293).
#   x: to the right of the printer.  y: to the back.  z: up.
# The gantry frame goes up and down with Z. The frame parts (the upright and its corner plates)
# do not: the fit check moves them by -Z for Z from 0 to 297.
#
# Sources: caliper and ruler measurements on the printer (2026-11-15, docs/measure/measured.json),
# the BLTouch offsets of the pad search (x 65.34, y -22.78), the BLTouch mount
# (lulzbot/bltouch/mount), and the B0561 sizes (lulzbot/camera_models.py). Values marked ESTIMATE
# are not measured. MEASURE.md lists the checks.

# --- The tool head at the right end of X (the parts that come near the arm)
PIN_X, PIN_Y = 65.34, -22.78     # the BLTouch pin (printer.cfg, from the pad search)
TAB_X1 = PIN_X + 4.0 + 2.0       # the right face of the BLTouch mount tab (half an ear and 2 mm of shelf)
TAB_Y0 = PIN_Y - 26.21 / 2       # the front of the tab (the ears are 26.21 long)
TAB_Z = (39.45, 46.45)           # the shelf of the mount, over the nozzle tip
MOTOR = ((18.9, 52.9), (-35.0, 9.6), (23.2, 65.5))  # ESTIMATE: x from the pin offset; the front from the
                                 # 9 mm in front of the lower screw (measured); the top from the mount project
MOUNT_PLATE = ((52.9, 59.4), (-35.0, -9.7), (23.7, 65.0))  # the plate of the BLTouch mount on the motor
BLTOUCH = ((PIN_X - 5.44, PIN_X + 5.44), (PIN_Y - 6.5, PIN_Y + 6.5), (3.3, TAB_Z[0]))
BLOWER = ((-25.0, 25.0), (-62.0, -35.0), (2.0, 56.0))    # ESTIMATE from the photos: the 50 mm radial fan
DUCT = ((-30.0, 25.0), (-45.0, 10.0), (3.0, 18.0))      # ESTIMATE: the black printed duct under it

# --- The front right upright (2020) and its corner plates
UP_X0 = TAB_X1 + 11.0            # the inner face. Measured: 11 mm from the tab, head at the right end.
UP_Y0 = TAB_Y0 - 8.0             # the front face. Measured: the tab is 8 mm behind it.
UP_W = 20.0
PLATE_PROUD = 6.5                # a corner plate (3 mm) and its button heads (about 3 mm) on the front face. ESTIMATE
TOP_PLATE_Z = 345.0              # the bottom of the top corner plate over the bed. Measured.
BOTTOM_PLATE_Z = -10.0           # the top of the bottom corner plate, under the bed surface. ESTIMATE (hard to reach)
PLATE_REACH = 80.0               # how far left of the upright a corner plate can reach. ESTIMATE (photos)
Z_MAX = 297.0                    # printer.cfg position_max of Z

# --- The X end face with the two screws
SCREW_X = UP_X0 - 50.18          # Measured (twice: 50.96, 50.18): the screws to the inner face of the upright
FACE_Y = UP_Y0 + 16.87           # Measured: the screw face is 16.87 mm behind the front face of the upright
SCREW_Z = (53.0, 53.0 + 35.44)   # Measured: the lower screw 53 mm over the tip; 35.44 mm apart
FACE_W = 13.63                   # Measured: the face between the raised belt strip and the step
STRIP_W = 14.0                   # ESTIMATE: the raised belt strip left of the face
STRIP_PROUD = 4.0                # ESTIMATE: how far the strip stands in front of the face
FACE_Z = (31.0, 113.0)           # ESTIMATE from the photo scale: the bottom and the top of the X end front
M3_HEAD_D = 5.26                 # Measured: the socket head
M3_HEAD_H = 3.0                  # M3 socket head (ISO 4762)
LOWER_CLEAR = 9.0                # Measured: from the lower screw to the motor, head at the right end
UPPER_CLEAR = 25.0               # Measured: 25 mm or more in front of the upper screw
HEAD_SPLIT_Z = 66.0              # ESTIMATE: the top of the motor and of the BLTouch mount plate
NOZZLE_Y_EST = -20.0             # ESTIMATE: the nozzle, for the aim of the camera in the images only

# --- The clamp plate
PLATE_T = 6.0                    # y thickness. With the head sunk in, 3 mm stays to the motor.
PLATE_W = FACE_W - 0.4           # 0.2 mm play on each side in the pocket
PLATE_Z = (48.0, 104.0)              # the bottom over the BLTouch mount tab (46.45), which passes in front of the face
CB_D = 6.0                       # counterbore for an M3 socket head (5.5 nominal, 5.26 measured)
CB_DEPTH = M3_HEAD_H             # the lower head is flush with the plate
HOLE = 3.4                       # M3 clearance

# --- The upper arm (prints upside down: the top face z = ARM_TOP on the bed)
ARM_TOP = 104.0
BEAM_Z0 = 72.0                   # the bottom of the beam: over the motor top (65.5) and the mount plate (65)
BEAM_Y1 = FACE_Y - STRIP_PROUD - 0.5   # the back of the beam: 0.5 mm in front of the belt strip
BEAM_Y0 = UP_Y0 + 0.5            # the front of the beam: 0.5 mm behind the front of the upright.
                                 # The corner plates are on the front face, so the beam never meets them.
POCKET = FACE_Y - PLATE_T        # the plate front: the beam has a pocket for the plate down to this y
BEAM_X0 = SCREW_X - FACE_W / 2 - STRIP_W
DROP_X = (TAB_X1 + 2.0, UP_X0 - 2.0)   # the drop: 2 mm right of the tab, 2 mm from the upright
DROP_Z0 = 10.0                   # the bottom of the drop
CB_ARM = 6.2                     # the counterbore of the upper screw in the beam
ARM_UNDER_HEAD = 3.0             # beam under the upper screw head

# --- The camera bracket (prints on its bottom face z = CAM_Z0)
CAM_Z = (-4.0, 40.0)             # all of it under 40 mm: at Z max the top corner plate starts at
                                 # 345 - 297 = 48 mm over the tip
FRONT_GAP = 10.0                 # between the front face of the upright and the bracket (plates: 6.5)
WALL = 8.0
FRONT_Y = (UP_Y0 - FRONT_GAP - WALL, UP_Y0 - FRONT_GAP)
FRONT_X = (DROP_X[0], UP_X0 + UP_W + 8.0)
TONGUE_Y = (FRONT_Y[0], BEAM_Y0)                  # from the front wall back to the drop
RIB_X = (FRONT_X[1] - WALL, FRONT_X[1])           # a rib at the right end of the front wall, back to the
RIB_Y = (FRONT_Y[1], FRONT_Y[1] + 4.0)            # gap: the wall is an L, stiff in y
JOINT_Z = (16.0, 32.0)           # the two M3 bolts from the bracket into the drop (nuts at the back)
JOINT_HOLE = 3.4
NUT_AF = 5.8                     # M3 nut 5.5 across flats, plus play
NUT_DEPTH = 3.0

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
