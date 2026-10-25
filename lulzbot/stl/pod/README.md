# Pod print files

`pod.py export` writes these files. Each part is in its print orientation, centered on the bed,
with its lowest point at z = 0. Load a file and print it as it is. Do not turn it.

## FDM (`fdm/`)

All parts fit on the TAZ Workhorse and the Mini 2 (0.5 mm nozzle). The dovetail slot of the wedge,
and the groove of the wedge in each bracket, have 0.35 mm of clearance on each side. The wedge
slides down into the groove of its bracket from the top.

| File | Print | On the bed | Supports |
|---|---|---|---|
| `pod-frame_fdm.stl` | 1 | Its back | Off. The roofs of the screw wells and insert holes are round bridges of 4 to 8 mm. |
| `pod-body_fdm.stl` | 1 | Its back | Off. The roofs of the four screw wells are round bridges of 8 mm. The vent slots in the side have pointed tops. |
| `pod-mount_fdm.stl` | 1 | The face that goes on the body | Off |
| `pod-wedge_fdm.stl` | 1 for each bracket | Its level bottom (it stands up) | Off |
| `pod-bracket-taz_fdm.stl` | 1, for the TAZ | The front of the plate | Tree supports. The groove for the wedge opens onto the bed, and its roof is 12.5 mm wide. Normal supports leave the roof empty. Tree supports fill it, and the nub for the cover screw. |
| `pod-bracket-mini2_fdm.stl` | 1, for the Mini 2 | The side of the plate that goes on the frame | None |
| `cable-clip-single_fdm.stl` | 5, for the TAZ | The base | None |
| `cable-clip-double_fdm.stl` | 2, for the Mini 2 | The base | None |

Print the clips in PETG. Their arms bend across the layer lines, and PLA can crack there.

## Hardware for the pod

| Part | Count | Where |
|---|---|---|
| M2.5 x 4 screw | 8 | In the box of the display: 4 hold the frame to the display, 4 hold the Pi |
| M3 heat-set insert | 6 | 4 in the back ring of the frame, 2 in the mount |
| M3 x 6 cap screw | 6 | 4 down the posts of the body into the frame, 2 from inside the body into the mount |
