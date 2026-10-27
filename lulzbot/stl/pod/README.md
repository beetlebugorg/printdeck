# Pod print files

Each file is in its print orientation. Load a file and print it as it is.

## FDM (`fdm/`)

1. Turn supports off. No part needs supports. Supports fill the screw holes, the insert holes and
   the groove of the TAZ bracket, and stay in the part.
2. Set **Z Seam Alignment** to **Sharpest Corner** and **Seam Corner Preference** to **Hide
   Seam**. This keeps the seam off the round corners.
3. Print the parts in the table. All parts fit on the TAZ Workhorse and the Mini 2 (0.5 mm
   nozzle).
4. Print the cable clips in PETG. PLA can crack where their arms bend.

To turn supports off in Cura, clear **Generate Support** (Support section) for all parts. For
one part: select it, click **Per Model Settings**, click **Select settings**, select **Generate
Support**, click **Close**, and clear the box.

| File | Print | On the bed |
|---|---|---|
| `pod-frame_fdm.stl` | 1 | Its back ring |
| `pod-body_fdm.stl` | 1 | Its back |
| `pod-mount_fdm.stl` | 1 | The face that goes on the body |
| `pod-wedge_fdm.stl` | 1 for each bracket | Its level bottom (it stands up) |
| `pod-bracket-taz_fdm.stl` | 1, for the TAZ | The top end of the plate (it stands up, 89 mm high). Use a 5 mm brim. |
| `pod-bracket-mini2_fdm.stl` | 1, for the Mini 2 | The side of the plate that goes on the frame |
| `pod-shim-0.5mm_fdm.stl`, `pod-shim-1.0mm_fdm.stl`, `pod-shim-2.0mm_fdm.stl` | 1 each, for the TAZ | Flat |
| `camera-arm-taz_fdm.stl` | 1, for the C270 webcam on the TAZ (option) | Its back face |
| `camera-clamp-taz_fdm.stl` | 1, for the C270 webcam on the TAZ (option) | Its bottom. Use a brim: it is 126 mm tall. |
| `camera-arm-mini2_fdm.stl` | 1, for the C270 webcam on the Mini 2 (option) | Its back face |
| `camera-clamp-mini2_fdm.stl` | 1, for the C270 webcam on the Mini 2 (option) | Its bottom |
| `camera-top-taz_fdm.stl` | 1, for the camera over the bed on the TAZ | Its end (it stands up, 124 mm high). Use an 8 mm brim. |
| `cable-clip-single_fdm.stl` | 5, for the TAZ | The base |
| `cable-clip-double_fdm.stl` | 2, for the Mini 2 | The base |

### The fit

The dovetail of the pod and the key of the wedge taper. Push each part down by hand until it is
tight. To remove it, pull it up.

On the TAZ, the nub of the bracket is 2.5 mm off the cover. Fill the gap with shims:

1. Put the bracket on the two rail clips. Loosen the cover screw.
2. Put shims between the nub and the cover until they fill the gap with no force. The shims are
   0.5, 1.0 and 2.0 mm thick.
3. Tighten the cover screw, an M3 x 10 with a washer.

## Resin (`resin/`)

These files are for the Peopoly Phenom and standard resin. Each part stands on supports, 6 mm
over the plate. The body, the frame, the mount, the wedge and the TAZ bracket fit on one plate.

The resin parts use captive M3 hex nuts in place of the heat-set inserts:

- Frame: put the 4 nuts in the pockets before the display goes on.
- Mount: slide the 2 nuts into the pockets before the mount goes on the body.

Print the mount and the wedge first, and try the fit.

| File | Supports on | Keep clean |
|---|---|---|
| `pod-body_resin.stl` | The open front: the rim and the inside | The back, the side walls, the posts and the ports |
| `pod-frame_resin.stl` | The bottom end, and the inside of the top end | The back ring, the side walls, and the front rim |
| `pod-mount_resin.stl` | The lower end of the plate and of the dovetail | The dovetail and the face on the body |
| `pod-wedge_resin.stl` | The level bottom | The slot and the key |
| `pod-bracket-taz_resin.stl` | The top end and the back of the plate | The front of the plate and its edges, the groove, and the clip slots |

Slicer settings:

- Layer height 0.05 mm. Print the parts solid.
- Supports: medium density, about 3 to 4 mm apart. Tip contact 0.3 to 0.4 mm, and 0.25 to 0.3 mm
  on the rim of the body. Use heavy supports and a raft.
- Remove each support tip on a "Keep clean" face.
- Put no supports in the groove of the bracket, the clip slots, the slot of the wedge, or the nut
  pockets.
- After the cure, sand the rim of the body flat on sandpaper on glass.

## Resin, flat on the plate (`resin-flat/`)

The same resin parts, flat on the plate with no supports. The back of the body and the frame
come out flat, with no support marks.

| File | On the plate |
|---|---|
| `pod-body_resin-flat.stl` | Its back |
| `pod-frame_resin-flat.stl` | Its back ring |
| `pod-mount_resin-flat.stl` | The face that goes on the body |
| `pod-wedge_resin-flat.stl` | Its level bottom (it stands up) |
| `pod-bracket-taz_resin-flat.stl` | The top end of the plate (it stands up, 89 mm high) |
| `pod-bracket-mini2_resin-flat.stl` | The side of the plate that goes on the frame |
| `pod-shim-0.5mm_resin-flat.stl`, `pod-shim-1.0mm_resin-flat.stl`, `pod-shim-2.0mm_resin-flat.stl` | Flat |

Slicer settings:

- No supports and no raft.
- Base layers: about 4 to 6, with the lowest base exposure that holds.
- Lift: slow for the base layers and the first 2 mm.
- Print the body and the frame on one plate, and the other parts on a second plate. Print the TAZ
  bracket on its own plate.
- Remove the parts with a thin scraper. Keep the scraper off the chamfered edge.

Print the cable clips in PETG on an FDM printer. Standard resin cracks where the arms bend. The resin clip
files are for tough resin only.

## Hardware for the pod

| Part | Count | Where |
|---|---|---|
| M2.5 x 4 screw | 8 | In the box of the display: 4 hold the frame to the display, 4 hold the Pi |
| M3 heat-set insert, 4 mm long | 6 | FDM parts only: 4 in the bosses of the frame, 2 in the mount. Press each insert flush with the face. |
| M3 x 6 cap screw | 6 | 4 down the posts of the body into the frame, 2 from inside the body into the mount |
| M3 hex nut (5.5 mm across the flats, 2.4 mm thick) | 6 | Resin parts only, in place of the 6 heat-set inserts |

## Webcam (option)

`camera.py` has a mount for a Logitech C270 webcam on each printer. The camera plugs into the
lower USB 3 port of the Pi. Each mount is a clamp on the frame and an arm with the hinge.

| Part | Count | Where |
|---|---|---|
| M3 x 16 cap screw | 1 | The hinge of the camera |
| M3 heat-set insert, 4 mm long | 2 | In the tab of the clamp |
| M3 x 18 cap screw | 2 | Through the post of the arm into the inserts |
| M3 x 8 cap screw and M3 roll-in T-nut (2020, 6 mm slot) | 2 each | TAZ: the saddle on the front right upright |
| M3 frame screw, 5 mm longer | 2 | Mini 2: in place of the two frame screws of the front right corner |
| `cable-clip-single` | 4 (TAZ) or 3 (Mini 2) | Along the frame to the deck, on 12 x 12 mm foam tape |

1. Press the two inserts into the tab of the clamp.
2. Remove the clip from the camera. Remove the small screw at the end of the clip pin, and push
   the pin out of the eyelet.
3. Put the eyelet between the two lugs of the arm. Put the M3 x 16 screw through the thin lug and
   the eyelet, into the thick lug.
4. Put the post of the arm in the channel of the tab. Put in the two M3 x 18 screws.
5. TAZ Workhorse: roll the two T-nuts into the front slot and the right slot of the front right
   upright, under the corner plate. Put the saddle keys into the slots, and tighten the two M3 x 8
   screws.
6. Mini 2: remove the two frame screws on the right face of the front right corner. Put the lip of
   the plate on the top of the frame, and put in the two longer screws.
7. Tilt the camera: about 52 degrees down on the TAZ, 46 degrees on the Mini 2. Tighten the hinge
   screw until the camera stays.
8. Run the cable in the clips to the deck. Coil the rest of the cable and tie it.

To remove the camera, unplug it and remove the two screws of the arm.

## Camera over the bed (TAZ)

`camera-top-taz_fdm.stl` holds an Arducam B0561 camera over the bed. The camera looks straight
down. The part grips the top front rail on its top face and its inside face. The camera hangs
behind the rail, and the back of the space over the bed stays free for the filament tube.

Print it in PETG or PLA+:

- Put it on the bed as it is in the file: it stands on its end, 124 mm high. The layers are then
  in the plane of the load of the camera.
- 4 walls, 40% infill or more, 5 top and 5 bottom layers, 0.2 mm layers.
- No supports. Use an 8 mm brim.

| Part | Count | Where |
|---|---|---|
| M3 x 8 cap screw | 6 | 2 down through the camera wall, 4 through the inside plate |
| M3 roll-in T-nut (2020, 6 mm slot) | 6 | 2 in the top slot and 4 in the inside slot of the top front rail |
| 1/4"-20 stud (set screw), 30 mm | 1 | In the round base of the camera, through the camera wall |
| 1/4"-20 hex nut (7/16") | 1 | On the stud, on the front face of the camera wall |
| USB-A to JST-ZR 4-pin cable, 1 m | 1 | The cable that comes with the camera |

In place of the stud and the nut, you can use a 1/4"-20 x 7/8" cap screw from the front face of
the camera wall into the base.

1. Roll 2 T-nuts into the top slot and 4 T-nuts into the inside slot of the top front rail, at the
   middle of the bed.
2. Put the part on the rail: the top plate on the top face, the inside plate on the inside face.
   Put the middle of the part at the middle of the bed.
3. Put in the 2 M3 x 8 screws down through the camera wall and the 4 through the inside plate.
   Tighten them.
4. Turn the stud into the round base of the camera from below, until its top is flush with the top
   of the base inside the U. Do not turn it in more: it touches the camera.
5. Put the base in the round seat on the back of the camera wall, with the stud through the hole.
   Put the nut on the stud on the front face of the wall.
6. Hold the camera square, with its sides parallel to the ends of the part. Tighten the nut with a
   7/16" wrench.
7. Turn the camera on its pivot until it looks straight down, with the cable socket up and near
   the wall. Tighten the pivot screws.
8. Plug the cable into the camera. Put it through the notch over the camera wall, down the front
   face of the wall beside the nut, and over the front edge of the part. Run it down the front face
   of the rail and along the clips to the deck. Plug it into the lower USB 3 port of the Pi.
9. Coil the rest of the cable and tie it.
