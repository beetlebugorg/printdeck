# Pod print files

`pod.py export` writes these files. Each part is in its print orientation and centered on the bed.
FDM and resin-flat parts have their lowest point at z = 0. Resin parts are 6 mm over z = 0, for
the supports.
Load a file and print it as it is. Do not turn it.

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
| `pod-frame_fdm.stl` | 1 | Its back ring | Off. The four display screws go through the back ring, so the frame has no bridges. The insert holes end in a cone, through to the other face. |
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

## Resin (`resin/`)

These files are for the Peopoly Phenom (build area 276 x 155 mm, 4K screen of 12.5 inches: about
72 um per pixel) and standard resin. The body, the frame, the mount, the wedge and the TAZ bracket
fit on one plate. Each part stands on
supports, 6 mm over the plate. The faces that show on the pod face away from the plate, so the
supports touch only faces that do not show. The orientations come from a search of the area that
needs support, the largest layer, and the cups. No layer is larger than 600 mm2, and no large face is
parallel to the screen. The largest pocket of resin that a layer closes off is 6 mm3, in the body.

The resin parts differ from the FDM parts:

- Captive M3 hex nuts replace the heat-set inserts. Heat-set inserts do not hold in cured resin.
  The frame has a nut pocket at the display end of each boss: put the 4 nuts in before the display.
  The display keeps them in. The mount has a nut pocket with a slot to each side edge: slide the 2
  nuts in before the mount goes on the body.
- The dovetail slot of the wedge and the groove of the key in the TAZ bracket have 0.2 mm of
  clearance on each side. Print the mount and the wedge first, and try the fit.
- The clip slots of the bracket keep the FDM clearance (0.35 mm), because the rail clips are FDM.

| File | Print | Toward the plate (supports) | Keep clean |
|---|---|---|---|
| `pod-body_resin.stl` | 1 | The open front: the rim on the frame and the inside. It turns 45 degrees about its long axis and tilts 15 degrees. | The back, the side walls, the posts and the ports |
| `pod-frame_resin.stl` | 1 | The bottom end, and the inside of the top end. It stands on its bottom end and leans 20 degrees, the back ring up. | The back ring, the side walls, and the front rim on the glass |
| `pod-mount_resin.stl` | 1 | The lower end of the plate and of the dovetail. It stands, the dovetail upright. | The dovetail and the face on the body |
| `pod-wedge_resin.stl` | 1 | The level bottom. It stands as on the printer, tilted 15 and 10 degrees. | The slot and the key |
| `pod-bracket-taz_resin.stl` | 1 | The top end of the plate, and the back of the plate (it goes on the printer). It leans 20 degrees, the front up. | The front of the plate and its edges, the groove, and the clip slots |

Slicer settings for the Phenom:

- Layer height 0.05 mm. Solid parts: do not hollow them. The walls are 2 to 3 mm.
- Supports: medium density, about 3 to 4 mm apart. Tip contact 0.3 to 0.4 mm across on the
  bottom end of a part, and 0.25 to 0.3 mm on the rim of the body. Use heavy supports and a raft
  on the plate.
- Put no support tip on the faces in the "Keep clean" column. Remove each tip that the
  automatic supports put there. Supports may go through the openings of the body and the frame.
- Do not put supports in the groove of the bracket, the clip slots, the dovetail slot of the
  wedge, or the nut pockets.
- After the cure, sand the rim of the body flat on a sheet of sandpaper on glass. Then the body
  sits flat on the frame.

## Resin, flat on the plate (`resin-flat/`)

These are the resin parts in the FDM positions: each part is on the build plate, with no supports.
The face on the plate gets the surface of the plate: the back of the body and the back ring of the
frame come out flat and even, with no support marks. The parts are the same as in `resin/`
(captive nuts, 0.2 mm on the dovetail slot and the key). The bed edge of the frame has a chamfer of
0.8 mm (0.5 mm in FDM).

| File | On the plate |
|---|---|
| `pod-body_resin-flat.stl` | Its back |
| `pod-frame_resin-flat.stl` | Its back ring |
| `pod-mount_resin-flat.stl` | The face that goes on the body |
| `pod-wedge_resin-flat.stl` | Its level bottom (it stands up) |
| `pod-bracket-taz_resin-flat.stl` | The top end of the plate (it stands up, 89 mm high) |
| `pod-bracket-mini2_resin-flat.stl` | The side of the plate that goes on the frame |

Slicer settings for the Phenom:

- Supports and raft: none. Put each part directly on the plate.
- Base layers: as few as hold the part (about 4 to 6), with the lowest base exposure that holds.
  The base layers get a long exposure, so they grow wider. The chamfer at the bed edge of the
  frame and of the body keeps that lip out of sight.
- Lift: slow for the base layers and the first 2 mm. The backs of the body and the frame are
  solid layers of about 50 cm2, and each layer pulls hard on the film.
- Take the parts off the plate with a thin scraper. Do not push on the chamfered edge.
- The parts do not close off a pocket of resin: the open side of each part faces the film.

The folder also has the webcam parts and the cable clips in their FDM positions. The tilted files in
`resin/` stay for a comparison.

Print the cable clips in PETG on an FDM printer (`fdm/`). By an estimate, their arms stretch about 3 percent as the
cable goes in, and standard resin cracks there. The resin clip files are for tough resin only.

## Hardware for the pod

| Part | Count | Where |
|---|---|---|
| M2.5 x 4 screw | 8 | In the box of the display: 4 hold the frame to the display, 4 hold the Pi |
| M3 heat-set insert, 4 mm long | 6 | FDM parts only: 4 in the bosses of the frame, 2 in the mount. The holes are 4.0 mm across and 4.0 mm deep, with a small hole through the floor. Press each insert flush with the face. |
| M3 x 6 cap screw | 6 | 4 down the posts of the body into the frame, 2 from inside the body into the mount |
| M3 hex nut (5.5 mm across the flats, 2.4 mm thick) | 6 | Resin parts only, in place of the 6 heat-set inserts: 4 in the bosses of the frame, 2 in the mount |

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
