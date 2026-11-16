# Measurements of the gantry camera arm

The values are in `docs/measure/measured.json`. Head at the right end means the head at X 293.

The Z tower and the X end come from the LulzBot CAD of the TAZ Workhorse v1.0.2
(`lulzbot/ref/lulzbot/final_assembly.fcstd`). The measurements put the CAD on the tool head, and
they check it. `shape.py` has the offsets.

## Measured (2026-11-15)

| # | Value | What | CAD |
|---|---|---|---|
| 1 | 35.44 mm | The two screws of the X end, top edge to top edge of the heads (the centre spacing). | 35.0 |
| 2 | 5.26 mm | The screw head. A 2.5 mm hex key fits: M3 socket screws. | |
| 3 | 13.63 mm | The X end face with the screws (the face toward the bed), front to back: from the front edge of the X end to the raised part. | |
| 4 | 50.18 mm | Front to back, from the screws to the back face of the front right upright. | 50.0 |
| 5 | 16.87 mm | Left to right, from the screw face to the inner face of the front right upright. | 14.5 |
| 6 | 103 mm | At Z 50, from the bed to the centre of the lower screw: the lower screw is 53 mm over the tip. | 52.2 |
| 7 | 345 mm | From the bed to the bottom of the top corner plate. | |
| 9 | 11 mm | Head at the right end: from the BLTouch mount tab to the inner face of the upright. | |
| 9b | 8 mm | Head at the right end: the tab is 8 mm behind the front face of the upright. | |
| 10 | 9 mm | Head at the right end: from the lower screw head to the motor left of it. | |
| 10b | 25 mm or more | Head at the right end: left of the upper screw head. | |

Items 4 and 5 were first read as front to back and left to right the other way around. The CAD
shows that the screws go left to right, and that the X end is inside the Z tower, behind the
upright. 2026-11-16.

## Estimates

- `HEAD_SPLIT_Z`: the top of the motor and of the BLTouch mount plate, about 66 mm over the tip.
  The beam starts 6 mm over it. Check that the head passes under the beam at X 293.
- `PLATE_PROUD`: a corner plate and its screw heads stand about 6.5 mm out from the front face of
  the upright. The bracket is 10 mm out.
- `NOZZLE_Y_EST`: the nozzle, only for the aim of the camera in the images.
- Item 5 and the CAD differ by 2.4 mm. The arm slice shows which one is right: if the drop is not
  2 mm from the upright, set `FACE_X`.
