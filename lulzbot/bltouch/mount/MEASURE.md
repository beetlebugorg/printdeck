# Measurements

All values are in `shape.py`. The origin is the nozzle tip: x to the right, y to the back, z up.

## Measured (2026-11-12)

| Value | In `shape.py` | How |
|---|---|---|
| 8.29 mm | `MOTOR_X0` | Nozzle centre to the face of the Titan extruder, left to right |
| 20.94 mm | `MOTOR_Z0` | Nozzle tip up to the bottom of the motor |
| 27 mm | `MOTOR_Y0` | Nozzle centre forward to the front face of the motor |
| 7.35 mm | `X_END_GAP` | Motor end cap to the X end, at X 293 |
| about 25 mm | `X_END_Y0` | The open part of the motor side, from the front, at X 293 |
| 26.21 mm | `BLT_FLANGE_L` | BLTouch, across the two ears |
| 8.0 mm | `BLT_EAR_W` | BLTouch ears, at the holes |
| 10.88 mm | `BLT_BODY_D` | BLTouch body, across the short side of the ears |
| 36.15 mm | `BLT_LEN` | BLTouch, top of the ears to the bottom of the body, pin up |

## Estimates

| # | Value | Estimate | How to measure |
|---|---|---|---|
| 1 | Front motor screws, centre | NEMA 17 pattern: 31 mm square, centred on the motor | Ruler from the bottom and the front of the motor to each front screw |
| 2 | Screw head | 5.6 mm wide, 2.4 mm high | Calipers on a front motor screw |
| 3 | Blower fan | Stays left of the motor end cap, and in front of the motor | Look from the front: the blower must not stick out to the right of the end cap |
| 4 | BLTouch ear thickness | 2.0 mm | Calipers on an ear |
| 5 | BLTouch body, along the ears | 13 mm | Calipers |
| 6 | Motor length | 34 mm (LDO-42STH34 data sheet) | Ruler, from the Titan face to the end cap |

If the mount does not fit, measure the value of the problem and change it in `shape.py`. The
"Adjust the fit" page of the PDF lists the usual problems.
