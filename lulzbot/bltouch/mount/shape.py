# BLTouch mount for the TAZ Workhorse: all dimensions. Units are millimetres.
#
# The mount holds a genuine BLTouch Smart on the right side of the SE tool head, beside the front
# part of the LDO pancake motor (LDO-42STH34-1504AC). It replaces the Creality CR-10 V3 L bracket
# on the ear of the blower fan. A plate goes under the heads of the two front motor screws. A shelf
# sticks out from the plate, and the BLTouch hangs under the shelf on two M3 screws with nuts.
#
# Frame ("head frame"): the origin is the tip of the nozzle.
#   x: to the right of the printer (+X of the printer).
#   y: to the back of the printer (+Y of the printer).
#   z: up.
# The part prints on its back face (y = PART_Y1) with the front up. Then every face is vertical, or
# a hole with a pointed top: no supports. The plate and the shelf are one profile in the x-z plane,
# so the bending of the shelf is in the plane of the layers.
#
# Sources: caliper and ruler measurements on the printer (2026-11-12), the LDO data sheet, the
# antclabs BLTouch data. Values marked ESTIMATE are not measured. MEASURE.md lists the checks.

# --- The motor (LDO-42STH34, NEMA 17, 34 mm long)
MOTOR_X0 = 8.29                  # the face of the Titan extruder, right of the nozzle centre. Measured.
MOTOR_LEN = 34.0                 # LDO-42STH34 body length (data sheet)
MOTOR_X1 = MOTOR_X0 + MOTOR_LEN  # the right face of the motor (the end cap)
MOTOR_SIDE = 42.3                # NEMA 17 square
MOTOR_Y0 = -27.0                 # the front face of the motor, in front of the nozzle. Measured.
MOTOR_Z0 = 20.94                 # the bottom of the motor, above the nozzle tip. Measured.
MOTOR_YC = MOTOR_Y0 + MOTOR_SIDE / 2
MOTOR_ZC = MOTOR_Z0 + MOTOR_SIDE / 2
HOLE_PITCH = 31.0                # NEMA 17 screw square
FRONT_SCREWS = [(MOTOR_YC - HOLE_PITCH / 2, MOTOR_ZC - HOLE_PITCH / 2),
                (MOTOR_YC - HOLE_PITCH / 2, MOTOR_ZC + HOLE_PITCH / 2)]   # (y, z)
REAR_SCREWS = [(MOTOR_YC + HOLE_PITCH / 2, MOTOR_ZC - HOLE_PITCH / 2),
               (MOTOR_YC + HOLE_PITCH / 2, MOTOR_ZC + HOLE_PITCH / 2)]
SCREW_HEAD_D = 5.6               # M3 pan head. ESTIMATE
SCREW_HEAD_H = 2.4               # ESTIMATE

# --- The X end at X 293 (the right end of X)
X_END_GAP = 7.35                 # from the motor end cap to the X end. Measured.
X_END_Y0 = MOTOR_Y0 + 25.0       # the X end is behind this y: the front 25 mm of the motor side is open. Measured (about).
X_END_MARGIN = 2.0               # the mount and the BLTouch stay this far in front of X_END_Y0

# --- The blower fan in front of the motor. Only its place matters: it must stay clear.
BLOWER = ((MOTOR_X1 - 16.0, MOTOR_X1), (MOTOR_Y0 - 52.0, MOTOR_Y0), (2.0, 54.0))  # ESTIMATE (from photos)

# --- The BLTouch Smart
BLT_FLANGE_L = 26.21             # across the two ears. Measured.
BLT_EAR_W = 8.0                  # the ears at the holes. Measured.
BLT_EAR_T = 2.0                  # ear thickness. ESTIMATE
BLT_BODY_D = 10.88               # body, across the short side of the ears. Measured.
BLT_BODY_W = 13.0                # body, along the ears. ESTIMATE
BLT_HOLE_PITCH = 18.0            # antclabs data
BLT_LEN = 36.15                  # from the top face of the ears to the bottom of the body, pin up. Measured.
BLT_GAP = 3.3                    # the bottom of the body over the nozzle tip. antclabs: 2.3 to 4.3.
EARS_Z = BLT_GAP + BLT_LEN       # the top face of the ears = the bottom face of the shelf

# --- The plate on the motor face
PLATE_T = 6.5                    # x thickness
CB_D = 6.6                       # counterbore for the screw head
CB_DEPTH = 2.5                   # the head sinks in: 4.0 mm of plate under the head
SCREW_HOLE = 3.4                 # M3 clearance
SCREW_LONGER = PLATE_T - CB_DEPTH  # each front motor screw gets this much longer: same thread engagement
PLATE_X1 = MOTOR_X1 + PLATE_T
PART_Y1 = X_END_Y0 - X_END_MARGIN  # the back face of the part (it prints on this face)
PLATE_Y0 = MOTOR_Y0              # the plate stops at the front of the motor (the blower is in front)
PLATE_Z0 = MOTOR_Z0 + 0.5
PLATE_Z1 = MOTOR_Z0 + MOTOR_SIDE - 0.5

# --- The BLTouch place and the shelf
BLT_CLEAR = 0.5                  # between the plate and the body
PIN_X = PLATE_X1 + BLT_CLEAR + BLT_BODY_D / 2
BLT_Y1 = PART_Y1                 # the back end of the ears
BLT_Y0 = BLT_Y1 - BLT_FLANGE_L
PIN_Y = (BLT_Y0 + BLT_Y1) / 2
BLT_HOLES = [PIN_Y - BLT_HOLE_PITCH / 2, PIN_Y + BLT_HOLE_PITCH / 2]   # y, at x = PIN_X
SHELF_T = 7.0
SHELF_X1 = PIN_X + BLT_EAR_W / 2 + 2.0  # 2 mm of shelf past the ears
SHELF_Y0 = BLT_Y0
NUT_AF = 5.8                     # M3 nut 5.5 across flats, plus play
NUT_DEPTH = 3.0                  # the pocket from the top of the shelf
BLT_SCREW = 10                   # M3 x 10 from under the ears, through the shelf into the nut
CABLE_DIAG = 9.0                 # the diamond hole for the wires of the BLTouch (they leave the top of the body)
GUSSET = 2.0                     # 45 degree fillet above the shelf at the plate

# --- The printer limits (printer/printer.cfg) and the bed
NOZZLE_X = (-50.0, 293.0)
NOZZLE_Y = (-17.0, 308.0)
BED = (0.0, 280.0)
