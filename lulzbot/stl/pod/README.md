# Pod print files

`pod.py export` writes these files. Each part is in its print orientation, centered on the bed,
with its lowest point at z = 0. Load a file and print it as it is. Do not turn it.

## FDM (`fdm/`)

All parts fit on the TAZ Workhorse and the Mini 2 (0.5 mm nozzle). The dovetail slot of each
bracket has 0.35 mm of clearance on each side.

| File | Print | On the bed | Supports |
|---|---|---|---|
| `pod-frame_fdm.stl` | 1 | Its back | Off. The roofs of the screw wells and insert holes are round bridges of 4 to 8 mm. |
| `pod-body_fdm.stl` | 1 | Its back | Off. The roofs of the four screw wells are round bridges of 8 mm. |
| `pod-mount_fdm.stl` | 1 | The face that goes on the body | Off |
| `pod-bracket-taz_fdm.stl` | 1, for the TAZ | The top end of the plate | Block the supports in the two slots for the rail clips. Their roofs are 12 mm bridges, and support in a slot spoils the fit of the clip. Use a brim. |
| `pod-bracket-mini2_fdm.stl` | 1, for the Mini 2 | The side of the plate that goes on the frame | None |
| `cable-clip-single_fdm.stl` | 5, for the TAZ | The base | None |
| `cable-clip-double_fdm.stl` | 2, for the Mini 2 | The base | None |

Print the clips in PETG. Their arms bend across the layer lines, and PLA can crack there.

## Hardware for the pod

| Part | Count | Where |
|---|---|---|
| M2.5 x 4 screw | 8 | In the box of the display: 4 hold the frame to the display, 4 hold the Pi |
| M3 heat-set insert | 6 | 4 in the back ring of the frame, 2 in the back of the body |
| M3 x 6 cap screw | 4 | Down the posts of the body into the frame |
| M3 x 6 countersunk screw | 2 | Through the mount into the body |

## Resin (`resin/`)

The dovetail slot of each bracket has 0.15 mm of clearance on each side. The backs of the body, the
frame and the brackets face the build plate, so the open fronts face away from it.
Each part leans 30 degrees. Add the supports in the resin slicer, on the back faces.

| File | Print |
|---|---|
| `pod-frame_resin.stl` | 1 |
| `pod-body_resin.stl` | 1 |
| `pod-mount_resin.stl` | 1 |
| `pod-bracket-taz_resin.stl` | 1, for the TAZ |
| `pod-bracket-mini2_resin.stl` | 1, for the Mini 2 |
| `cable-clip-single_resin.stl` | 5, for the TAZ |
| `cable-clip-double_resin.stl` | 2, for the Mini 2 |
