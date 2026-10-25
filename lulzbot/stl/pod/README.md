# Pod print files

`pod.py export` writes these files. Each part is in its print orientation, centered on the bed,
with its lowest point at z = 0. Load a file and print it as it is. Do not turn it.

## FDM (`fdm/`)

All parts fit on the TAZ Workhorse and the Mini 2 (0.5 mm nozzle). The dovetail slot of the wedge,
and the groove of the wedge in each bracket, have 0.35 mm of clearance on each side. The wedge
slides down into the groove of its bracket from the top.

No part needs supports. Turn supports off before you slice these parts. If supports are on, the
slicer fills the screw holes, the insert holes and the groove of the TAZ bracket with supports
that do not come out. In Cura, do one of these:

- In the print settings, clear the **Generate Support** box (Support section) for all parts.
- Or, for one part: select it, click **Per Model Settings** in the left toolbar, click **Select
  settings**, find **Generate Support**, select it, click **Close**, and clear the box.

`pod.py overhangs` checks the files. It finds each face that points down at less than 45 degrees
above the bed. A flat roof is a bridge, and it is good if the opening under it is 6 mm across or
less.

| File | Print | On the bed | Supports |
|---|---|---|---|
| `pod-frame_fdm.stl` | 1 | Its back ring | Off. The tops of the two screw wells are round bridges of 4.8 mm. The insert holes end in a cone, through to the other face. |
| `pod-body_fdm.stl` | 1 | Its back | Off. The tops of the four screw wells are round bridges of 5.8 mm. The back edge is a chamfer with a round above it. The vent slots in the side have pointed tops. |
| `pod-mount_fdm.stl` | 1 | The face that goes on the body | Off. The insert holes end in a cone, through to the other face. |
| `pod-wedge_fdm.stl` | 1 for each bracket | Its level bottom (it stands up) | Off |
| `pod-bracket-taz_fdm.stl` | 1, for the TAZ | The top end of the plate (it stands up, 89 mm high) | Off. The groove for the wedge and the slots for the rail clips go up from the bed. Use a brim of 5 mm: the part is high on a narrow base. |
| `pod-bracket-mini2_fdm.stl` | 1, for the Mini 2 | The side of the plate that goes on the frame | Off. The passages of the zip-tie anchors have pointed tops. |
| `cable-clip-single_fdm.stl` | 5, for the TAZ | The base | Off |
| `cable-clip-double_fdm.stl` | 2, for the Mini 2 | The base | Off |

The seam of each layer goes to a corner. To keep it off the round corners of the frame and the
body, set **Z Seam Alignment** to **Sharpest Corner** and **Seam Corner Preference** to **Hide
Seam**.

Print the clips in PETG. Their arms bend across the layer lines, and PLA can crack there.

## Hardware for the pod

| Part | Count | Where |
|---|---|---|
| M2.5 x 4 screw | 8 | In the box of the display: 4 hold the frame to the display, 4 hold the Pi |
| M3 heat-set insert, 4 mm long | 6 | 4 in the bosses of the frame, 2 in the mount. The holes are 4.0 mm across and 4.0 mm deep, with a small hole through the floor. Press each insert flush with the face. |
| M3 x 6 cap screw | 6 | 4 down the posts of the body into the frame, 2 from inside the body into the mount |
