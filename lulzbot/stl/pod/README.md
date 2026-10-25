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
| `camera-arm-taz_fdm.stl` | 1, for the C270 webcam on the TAZ (option) | Its back face (as the file is) | Off |
| `camera-clamp-taz_fdm.stl` | 1, for the C270 webcam on the TAZ (option) | Its bottom: it stands as it does on the printer | Off. Use a brim: it is 126 mm tall. |
| `camera-arm-mini2_fdm.stl` | 1, for the C270 webcam on the Mini 2 (option) | Its back face (as the file is) | Off |
| `camera-clamp-mini2_fdm.stl` | 1, for the C270 webcam on the Mini 2 (option) | Its bottom: it stands as it does on the printer | Off |
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

## Webcam (option)

`camera.py` has a mount for a Logitech C270 webcam on the frame of each printer. The deck does not change. The camera plugs into the lower USB 3 port of the Pi. Each mount is two printed parts: the clamp on the frame, and the arm with the hinge of the camera. Every part prints with supports off. `camera.py` checks each STL: no face under 45 degrees, and no flat ceiling.

| Part | Count | Where |
|---|---|---|
| M3 x 16 cap screw | 1 | The hinge of the camera: through the thin lug and the eyelet. It cuts its thread in the thick lug. |
| M3 heat-set insert, 4 mm long | 2 | In the tab of the clamp, from its face on the arm. |
| M3 x 18 cap screw | 2 | Through the post of the arm into the inserts. |
| M3 x 8 cap screw and M3 roll-in T-nut (2020, 6 mm slot) | 2 each | TAZ: the saddle on the front right upright. The same T-nut as the rail clips. |
| M3 frame screw, 5 mm longer | 2 | Mini 2: the plate on the right face of the front right corner, in place of the two frame screws of the corner. |
| `cable-clip-single` | 4 (TAZ) or 3 (Mini 2) | Along the frame to the deck, on 12 x 12 mm foam tape. |

1. Press the two inserts into the tab of the clamp.
2. Take the clip off the camera. Take out the small screw at the end of the clip pin, and push the pin out of the eyelet on the back of the camera.
3. Put the eyelet between the two lugs of the arm. Put the M3 x 16 screw through the thin lug and the eyelet, and turn it into the thick lug.
4. Put the post of the arm in the channel of the tab, and put in the two M3 x 18 screws.
5. TAZ Workhorse: roll the two T-nuts into the front slot and the right slot of the front right upright, under the corner plate. Put the keys of the saddle into the slots, and tighten the two M3 x 8 screws. Mini 2: take out the two frame screws on the right face of the front right corner. Put the lip of the plate on the top of the frame, and put in the two longer screws.
6. Tilt the camera: about 52 degrees down on the TAZ and 46 degrees down on the Mini 2. Tighten the hinge screw until the camera stays at its tilt.
7. Run the cable along the frame in the clips to the deck. On the TAZ it goes along the top front rail and down the front left upright (about 1.1 m). On the Mini 2 it goes along the top front edge of the frame (about 0.6 m). Coil the rest of the 1.5 m cable and tie it.

To take the camera off, unplug it, and take out the two screws of the arm. The clamp can stay on the frame.
