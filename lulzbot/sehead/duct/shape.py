# SE tool head duct, blower bracket and BLTouch bracket for the TAZ Workhorse: all dimensions.
# Units are millimetres.
#
# The parts replace the stock duct of the LulzBot SE tool head and the BLTouch mount on the right
# end of the motor (lulzbot/bltouch/mount):
#   - the blower bracket: a plate on the end cap of the extruder motor, under the two front motor
#     screws (countersunk screws). The 50 x 15 mm blower lies flat on it, the outlet down at the back.
#   - the duct: a U around the hot end, open at the front. The air comes from the blower outlet,
#     goes under the motor, and leaves through outlets on the right, the back and the left. Two
#     screws of the stock duct hold it: the front bottom motor screw and the screw at the back
#     left of the Aero.
#   - the BLTouch bracket: on two screws of the heat sink fan (the left side). It holds the
#     BLTouch behind the fan, behind and to the left of the nozzle.
#
# Frame ("head frame"): the origin is the nozzle tip.
#   x: to the right of the printer.  y: to the back.  z: up.
# This is the frame of the SE assembly CAD moved by -SE_TIP.
#
# Sources:
#   - The LulzBot SE tool head CAD (lulzbot/ref/lulzbot/sl_toolhead/Achemon_sphynx_assem.fcstd,
#     read by cad.py): the Aero, the motor, the mount, the fans, the stock duct and its screws.
#   - The X axis of lulzbot/ref/lulzbot/final_assembly.fcstd (cad.py): the X carriage and the X
#     ends. The stock head of that file hangs from the same two inserts as the SE head.
#   - Measurements on the printer (2026-11-12 and 2026-11-13, lulzbot/bltouch/mount/shape.py): the
#     motor to the X end at X max. Values marked ESTIMATE are not in the CAD and not measured.
#     MEASURE.md lists the checks.

SE_TIP = (-37.125, -35.4, -112.757)  # the nozzle tip in the SE assembly CAD

# --- The extruder motor (CAD)
MOTOR_X0 = 19.62                  # the front face of the motor (on the Titan)
MOTOR_X1 = 53.375                 # the end cap
MOTOR_Y = (-31.6, 9.4)
MOTOR_Z = (28.13, 69.13)
MOTOR_YC = (MOTOR_Y[0] + MOTOR_Y[1]) / 2
MOTOR_ZC = (MOTOR_Z[0] + MOTOR_Z[1]) / 2
BOLT_PITCH = 31.0                 # NEMA 17
FRONT_BOLTS = [(MOTOR_YC - BOLT_PITCH / 2, MOTOR_ZC - BOLT_PITCH / 2),
               (MOTOR_YC - BOLT_PITCH / 2, MOTOR_ZC + BOLT_PITCH / 2)]   # (y, z), on the end cap
# The motor connector under the motor, at the end cap (CAD), and room for the plug and its cable.
CONNECTOR = ((43.9, MOTOR_X1), (-19.2, -3.0), (21.6, 28.13))
PLUG_ROOM = 4.0                   # ESTIMATE: the plug and the cable bend under the connector

# --- The X ends. The head moves by -129.3 in x from the CAD place to X home (X min): there the
# carriage touches the X endstop of the left X end. At X max the end cap is X_END_GAP from the
# right X end (measured).
X_HOME_SHIFT = -129.3 + 1.0       # 1 mm less: the lever of the switch
X_END_GAP = 7.35                  # measured
XEND_LEFT_FACE = 261.26           # the left face of the right X end at the CAD place
XEND_SHIFT = MOTOR_X1 + X_END_GAP - XEND_LEFT_FACE
XEND_FRONT_Y = -6.6               # the front of the right X end. The CAD gives -1.6; the measurement
                                  # ("the front 25 mm of the motor side is open") gives -6.6. Use -6.6.

# --- The blower (5015, the CAD of the stock head). The CAD place of the blower (head frame):
# body X 11.15..62.95, Y -51.49..-36.49, Z 19.73..70.73. The outlet is the face X 11.15; its inside is
# Y -50.5..-38.3 and Z 20.73..43.8 (ESTIMATE: 1 mm walls). The closed side is Y -36.49; the intake
# is on the other side. Ears with 4 mm holes at (X, Z) (58.35, 26.53) and (15.75, 64.93).
# The new place turns the CAD: X -> z, Y -> -x, Z -> -y. Then the outlet points down, at the back.
BLOWER_T = 15.0
BRACKET_T = 3.0                   # the plate of the blower bracket on the end cap
BLOWER_X0 = MOTOR_X1 + BRACKET_T  # the closed side of the blower on the plate
BLOWER_Y1 = XEND_FRONT_Y - 2.0    # the back of the blower: 2 mm in front of the X end
BLOWER_Z0 = 17.8                  # the outlet face: 0.3 mm over the top of the duct (DUCT_Z1)
BLOWER_MOVE = (BLOWER_X0 - 36.49, BLOWER_Y1 + 19.73, BLOWER_Z0 - 11.15)   # added after the turn


def blower_point(X, Y, Z):
    """A point of the CAD blower at its new place."""
    return (-Y + BLOWER_MOVE[0], -Z + BLOWER_MOVE[1], X + BLOWER_MOVE[2])


BLOWER_BOX = ((BLOWER_X0, BLOWER_X0 + BLOWER_T), (BLOWER_Y1 - 51.0, BLOWER_Y1), (BLOWER_Z0, BLOWER_Z0 + 51.8))
OUTLET_IN_X = (blower_point(0, -38.3, 0)[0], blower_point(0, -50.5, 0)[0])
OUTLET_IN_Y = (blower_point(0, 0, 43.8)[1], blower_point(0, 0, 20.73)[1])
OUTLET_OUT_Y = (blower_point(0, 0, 44.8)[1], BLOWER_Y1)
EAR_TOP = blower_point(58.35, 0, 26.53)[1:]      # (y, z): on the end cap
EAR_LOW = blower_point(15.75, 0, 64.93)[1:]      # (y, z): in front of the motor and under it

# --- The blower bracket (prints on its outer face, x = BLOWER_X0)
BRK_Y = (BLOWER_BOX[1][0] - 1.0, BLOWER_Y1)
BRK_Z = (15.0, 70.5)              # the top: 1.5 mm under the beam of the gantry camera arm (z 72)
BRK_OUTLET_Z0 = BLOWER_Z0 + 0.5   # the plate stops over the duct inlet (y of the outlet)
CSK_D = 6.9                       # M3 countersunk head (ISO 10642: 6.72 max)
SQ_NUT = 5.7                      # M3 square nut (DIN 562: 5.5 x 1.8), plus play
SQ_NUT_T = 1.9
EAR_SCREW = 18                    # M3 x 18 through the top ear: it ends at the back of the plate
BOSS_X0 = MOTOR_X1 - 6.5          # the boss of the low ear, on the back of the plate (off the motor)
BOSS_D = 10.0
NUT_AF = 5.8                      # M3 hex nut, plus play
NUT_DEPTH = 2.6
LOW_SCREW = 25                    # M3 x 25 through the low ear and the boss into the hex nut
FLANGE_X0 = 40.0                  # a stiff flange along the front edge, in front of the motor
FLANGE_T = 3.0
HOLE = 3.4                        # M3 clearance

# --- The hot end (CAD): the heater block, and the stock duct opening around it
BLOCK = ((-7.5, 15.5), (-8.0, 8.0), (6.5, 18.5))
SOCK = 1.5                        # the silicone sock on the block. ESTIMATE
OPEN_X = (-12.5, 22.5)            # the opening of the stock duct around the hot end (CAD)
OPEN_Y = (-16.2, 16.3)

# --- The duct (prints on its bottom face)
DUCT_Z0 = 5.0                     # the bottom, over the nozzle tip (the stock duct: 5.93)
WALL = 1.5
CH_W = 8.0                        # the channel of the ring: width
CH_Z0 = DUCT_Z0 + WALL            # the floor of the channels
CH_WALL_TOP = 12.0                # the side walls of the channel; then a 45 degree roof
DUCT_Z1 = CH_WALL_TOP + CH_W / 2 + WALL   # the top of the ring
RING_R = OPEN_X[1] + WALL + CH_W / 2      # the centre line of the right arm
RING_L = OPEN_X[0] - WALL - CH_W / 2      # the left arm
RING_B = OPEN_Y[1] + WALL + CH_W / 2      # the back arm
RING_FRONT = OPEN_Y[0]                    # the front ends of the side arms
CORNER_GAP = 0.5                  # the back left corner turns at 45 degrees round the BLTouch
# The link under the motor: two channels side by side, from the right arm to the outlet. It passes
# under the motor connector. In front of the connector it is in the view of the cameras.
LINK_Y = (-24.0, -3.0)
INLET_LIP = 3.0                   # the lip that goes up into the blower outlet
LIP_PLAY = 0.3
LIP_T = 1.0
TARGET = (0.0, 0.0, -0.5)         # the outlets point at this point, under the nozzle tip
OUTLET_W = 3.0                    # each outlet: width, and the height of its walls (then a 45 degree roof)
OUTLET_H = 2.0

# The two tabs of the stock duct (CAD): the front tab between the Aero and the motor, on the front
# bottom motor screw; the post at the back left, under the head of the long screw of the Aero.
FRONT_TAB_X = (13.65, MOTOR_X0)
FRONT_TAB_Y = (-31.5, -21.5)
FRONT_TAB_Z = (DUCT_Z1, 38.0)
FRONT_TAB_LOW = (-25.3, 34.2)     # behind y -25.3 the mount comes down to z 34.4: the tab stops at 34.2
POST_X = (-16.0, -12.6)           # the stock post: x -16..-12
POST_Y = (5.0, 16.5)
POST_Z = (DUCT_Z1, 38.5)
POST_SCREW = (10.75, 33.65)       # (y, z) of the long screw (M3 x 35, from the left)
STRUT_Z1 = 27.0                   # the strut from the front tab to the link: under the motor (28.13)

# --- The BLTouch Smart (measured on the printer, lulzbot/bltouch/mount)
BLT_FLANGE_L = 26.21              # across the two ears
BLT_EAR_W = 8.0
BLT_EAR_T = 2.0                   # ESTIMATE
BLT_BODY_D = 10.88                # across the short side of the ears
BLT_BODY_W = 13.0                 # along the ears. ESTIMATE
BLT_HOLE_PITCH = 18.0
BLT_LEN = 36.15                   # from the top of the ears to the bottom of the body, pin up
BLT_GAP = 3.3                     # the bottom of the body over the nozzle tip (antclabs: 2.3 to 4.3)
EARS_Z = BLT_GAP + BLT_LEN        # the top face of the ears = the bottom face of the shelf

# --- The heat sink fan (CAD): its outer face, and two of its four screws (M3 x 12 button heads)
FAN_X0 = -29.5
FAN_BACK = (14.51, 52.23)         # (y, z)
FAN_BOTTOM = (-8.47, 29.25)
FAN_Y1 = 18.21                    # the back corner of the fan, at z 52. The fan is turned 45 degrees:
                                  # at the height of the BLTouch (z 37 and lower) it reaches only y 4.

# --- The BLTouch place: behind the fan, the ears along x
PIN_X = -25.0
PIN_Y = 23.0                      # the ears end at y 27, in front of the mount (y 27.4 at x -16)
BLT_X = (PIN_X - BLT_BODY_W / 2, PIN_X + BLT_BODY_W / 2)
BLT_Y = (PIN_Y - BLT_BODY_D / 2, PIN_Y + BLT_BODY_D / 2)
BLT_HOLES = [PIN_X - BLT_HOLE_PITCH / 2, PIN_X + BLT_HOLE_PITCH / 2]

# --- The BLTouch bracket (prints on its back face, y = SHELF_Y1)
PAD_T = 3.0                       # the pads under the fan screws: x FAN_X0 - PAD_T .. FAN_X0
PAD_D = 9.0
FAN_SCREW = 16                    # M3 x 16 button heads in place of the M3 x 12
STRIP_W = 5.0                     # the strip between the pads, outside the edge of the fan
SHELF_Y = (17.5, 27.2)            # behind the fan and the Aero, in front of the mount (y 27.4)
SHELF_Z1 = EARS_Z + 5.5
SHELF_X = (PIN_X - BLT_FLANGE_L / 2 - 0.5, -12.0)
BLOCK_X0 = FAN_X0 - PAD_T - 2.5   # the block from the back pad to the shelf. The X motor of the left
                                  # X end is 1.5 mm to the left of x -37.5 at X home.
BLT_SCREW = 10                    # M3 x 10 from under the ears into nuts in the shelf
CABLE_DIAG = 9.0                  # the diamond hole for the BLTouch wires

# --- The printer limits
NOZZLE_X = (-50.0, 293.0)
NOZZLE_Y = (-17.0, 308.0)
BED = (0.0, 280.0)
