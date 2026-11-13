# BLTouch mount

A printed mount for the BLTouch Smart on the LulzBot TAZ Workhorse with the SE tool head. It
replaces the Creality CR-10 V3 L bracket on the ear of the blower fan.

The mount is a plate under the heads of the two front screws of the extruder motor (LDO
pancake). A shelf sticks out from the plate. The BLTouch hangs under the shelf on two M3 × 10
screws with nuts. The BLTouch sits beside the front 25 mm of the motor. At the right end of X
(X 293), the X end passes behind it.

- The pin is about 54.7 mm right of the nozzle and 17.1 mm in front of it. The old bracket put
  it 12 mm right and 76 mm in front, so the pin could not go behind Y 232.
- The pin now reaches X 4.7 to 347.7 and Y −34.1 to 290.9: the full bed.
- The mount holds on two metal screws into the motor, away from the heater block.

## Files

| File | Content |
|---|---|
| `shape.py` | All the dimensions, with the measured values and the estimates. |
| `parts.py` | The mount and the context (motor, blower, X end, BLTouch) in build123d. |
| `model.py` | The fit check, then `cad/*.step`, `stl/bltouch_mount.stl` (print orientation) and the meshes. |
| `cura/make_profiles.py` | The Cura profile, the filament use and the support check. |
| `lineart/render.mjs` | The line-art images. |
| `make_pdf.py` | The build PDF, `bltouch-mount-build.pdf`. |
| `make.sh` | Runs all the steps. |
| `MEASURE.md` | The values that are estimates, and how to measure them. |

## Build

```
./make.sh
```

## After the install

Change `[bltouch]` in `printer/printer.cfg`: `x_offset`, `y_offset`, then the paper test for
`z_offset`. Set `[bed_mesh]` to the full bed, move the `[z_tilt]` points, and do the axis twist
calibration again.
