# Measurements of the gantry camera arm

The values are in `docs/measure/measured.json`, and the pictures for them are in `docs/measure/`.
Head at the right end means the head at X 293.

## Measured (2026-11-15)

| # | Value | What |
|---|---|---|
| 1 | 35.44 mm | The two screws of the X end face, top edge to top edge of the heads (the centre spacing). |
| 2 | 5.26 mm | The screw head. A 2.5 mm hex key fits: M3 socket screws. |
| 3 | 13.63 mm | The face with the screws, from the raised belt strip to the first step. |
| 4 | 50.18 mm | From the centre of the screws to the inner face of the front right upright (first try 50.96). |
| 5 | 16.87 mm | From the screw face to the front face of the upright. The face is behind it. |
| 6 | 103 mm | At Z 50, from the bed to the centre of the lower screw: the lower screw is 53 mm over the tip. |
| 7 | 345 mm | From the bed to the bottom of the top corner plate. |
| 9 | 11 mm | Head at the right end: from the BLTouch mount tab to the inner face of the upright. |
| 9b | 8 mm | Head at the right end: the tab is 8 mm behind the front face of the upright. |
| 10 | 9 mm | Head at the right end: from the lower screw to the motor in front of it. |
| 10b | 25 mm or more | Head at the right end: in front of the upper screw. |

## Estimates

Check these before the full print. The fit tests and the clamp plate show most of them.

- `BOTTOM_PLATE_Z` (item 8): the top of the bottom corner plate is about 10 mm under the bed
  surface. The bracket bottom is 4 mm under the tip, and 10 mm in front of the upright.
- `STRIP_PROUD`, `STRIP_W`: the raised belt strip left of the face. The beam is 0.5 mm in front
  of it. If the beam touches the strip, measure how far the strip stands out.
- `HEAD_SPLIT_Z`: the top of the motor and of the BLTouch mount plate, about 66 mm over the tip.
  The beam starts 6 mm over it. Check that the head passes under the beam at X 293.
- `PLATE_PROUD`, `PLATE_REACH`: a corner plate and its screw heads stand about 6.5 mm out from
  the front face of the upright. The bracket is 10 mm out.
- `FACE_Z`: the X end front from 31 to 113 mm over the tip (from the photo scale). It only
  matters for the images.
- `NOZZLE_Y_EST`: the nozzle, only for the aim of the camera in the images.
- The top front bar of the frame: the beam top is 104 mm over the tip, under the top of the
  X end (about 113 mm). If the X end reaches Z 297 without a hit, the beam does too.
