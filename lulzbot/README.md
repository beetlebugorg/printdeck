# Printdeck LulzBot

<img src="renders/printer-front-left.png" alt="The Printdeck LulzBot mount on the TAZ Workhorse" width="480">

A mount for the Printdeck hardware on the LulzBot TAZ Workhorse: the Adafruit 7 inch HDMI
display with the AR1100 touch controller, a Raspberry Pi 5 with a 30 mm fan and stick-on
heatsinks, and the 2.5 inch boot drive. It is at the front left of the printer, below the TAZ Workhorse logo on the
electronics chassis, and it wraps around the front left edge of the chassis.

`printdeck-lulzbot-build.pdf` has the print plan, the hardware list, and the assembly steps.

- The head holds the display in landscape (wide view), turned 180 degrees from its native
  position: the HDMI and USB sockets are at the left, so normal straight plugs fit. The face
  leans back 15 degrees, the same as the Slate, and points to the front of the printer. The
  screen opening is at the centre of the face, so the border is the same at the left and the right.
- The head is 252 mm wide. Its left end is 7 mm past the outer face of the pod (45 mm past the
  left side of the chassis). Its right end is 207 mm to the right of the left side of the chassis,
  in front of the front left rail.
- The drive is in the head, flat on the back panel, behind the display.
- The pod is on the left side of the chassis (the aluminium cover), at the same height as the
  head. It holds the Pi, the fan, and the cables. The white grille snaps into a pocket of the
  pod, over a fan plate that holds the fan over the SoC of the Pi (see Fan).
- There is no hole to drill. Two printed clips hold the right end of the head on the front slot
  of the rail. A metal M3 roll-in T-nut for 2020 extrusion rolls into the slot behind the
  lips, and an M3 x 8 screw from the clip pulls it up. Two screws through
  a flange of the plate hold up the head. The printer USB cable and the power cable leave at the rear face of the chassis.

## Parts

| File | Colour | Print orientation | Filament | Time |
|---|---|---|---|---|
| `stl/head.stl` | Navy | Face down | 87 g | 4 h 28 min |
| `stl/pod.stl` | Navy | Outer face down | 95 g | 4 h 42 min |
| `stl/back.stl` | Orange | Rear face down, 0.4 mm layers | 69 g | 3 h 16 min |
| `stl/plate.stl` | Orange | Cover face down, 0.4 mm layers | 61 g | 2 h 18 min |
| `stl/clip.stl` (print 2) | Orange | On its end, with a brim | 2 x 4 g | 2 x 18 min |
| `stl/grille.stl` | White | Back down (the flat grille of the first pod) | 12 g | 42 min |
| `stl/fan_plate.stl` | White | Grille side down | 12 g | 40 min |

In total: 344 g of PLA (navy 182 g, orange 138 g, white 24 g) and 16 h 42 min of printing. Print
the display fit test first (`stl/fit_display.stl`, 22 g, 1 h 10 min): the display pocket has
0.3 mm of clearance at each side. Until you have the metal T-nuts, also print two roll-in nuts
(`stl/roll_nut.stl`, see Hardware). The numbers come from slices with the Cura profiles in `cura/`
(CuraEngine, TAZ Workhorse SE tool head, 0.5 mm nozzle, 2.85 mm PLA). The profiles keep the flow
at 12 mm3/s or less, with slower outer walls. `make.sh` checks each part: one closed mesh, no
flat ceiling, no face flatter than 40 degrees, and 0 g of support in a slice with supports on.
CuraEngine crashes on the head with supports everywhere, so the head is sliced with supports
from the build plate; the overhang check on the STL covers the rest.

## Hardware

`build/hardware.json` and page 4 of the PDF list the hardware. The list comes from the model:
each screw, washer, and heat-set insert is in the model at its place, and `make_pdf.py` stops if
the step chips and the list do not agree. All screws are M3 socket cap head screws.

- M3 x 6 socket cap head: 12 (display 4, Pi 4, drive 4). The display and Pi screws cut their
  own thread in a 2.5 mm hole in the boss: turn them until snug, no more.
- M3 x 8 socket cap head: 4 (pod onto the plate 2, rail clips into the T-nuts 2)
- M3 x 10 socket cap head: 6 (back panel into the head 4; plate on the cover 2, upper pair of PP-FP0164)
- M3 x 12 socket cap head: 2 (plate flange into the back panel)
- M3 washer: 2 (under the cover screws)
- M3 x 7 heat-set insert: 6 (head 4, back panel 2; 3 to 7 mm fits)
- M3 x 5 heat-set insert: 2 (pod blocks; 3 to 5 mm fits)
- M3 roll-in T-nut with spring ball, for 2020 extrusion, 6 mm slot: 2 (rail clips). Buy the uxcell
  M3 roll-in spring T-nut with ball, 2020 series (Amazon B09MFT1PKF). It rolls into the slot at
  any point on the rail, and the spring ball holds it in place. The LulzBot BOM uses Misumi
  HNTAP5-5 drop-in T-nuts (the M5 size) in this frame.

The rail is 2020 extrusion. The model has a typical M3 T-nut: 10 mm across the slot, 6 mm
along it, 4 mm thick, with 45 degree sides at the bottom. Its top is 1.8 mm behind the face of
the rail, so the M3 x 8 screw goes 3.9 mm into its thread. An M3 x 10 screw goes 7.7 mm behind
the face, past the bottom of the slot (about 6.5 mm), so do not use one. A printed twist nut
failed: it must be less than 5.1 mm wide to go through the lips, and the plastic around an M3
insert or an M3 thread was too thin to hold.

### Printed roll-in nut (option)

Until you have the metal T-nuts, you can print two roll-in nuts (`stl/roll_nut.stl`, 1 g and
3 min each, orange, on its end with a brim, 100 % infill). Each one holds a standard M3 hex nut
(5.5 mm across the flats, 2.4 mm thick): press the hex nut into the pocket from the bottom with
pliers. The printed nut rolls into the slot at any point on the rail, the same as the metal
T-nut. You do not remove the ends of the rail.

- Its section across the slot is 7.1 mm wide and 3.6 mm thick, with 1.4 mm rounds, and it is
  11 mm long. You put one long edge into the slot, with the hex nut away from you, and roll the
  nut in behind the lips.
- `roll_nut.py` checks the roll in the section of the rail of the LulzBot model, with 0.25 mm of
  clearance. A wider section does not roll in, and a narrower one holds less of the lips.
- The M3 x 8 screw pulls the nut up until its round shoulders are on the round backs of the
  lips: its top face is then 1.3 mm behind the face of the rail. The screw goes through the full
  2.4 mm thread of the hex nut and 0.8 mm past it. There is 1.2 mm of plastic between the hex nut
  and the lips, and the bottom of the nut is 1.5 mm above the bottom of the slot.
- The hex nut pocket has a 45 degree point up in the print, so the nut prints with no supports.
- The printed nut is not as strong as a metal T-nut. Tighten the clip screw gently.

The idea comes from the "T-NUT for 2020 profile" by nisi1t
(https://www.thingiverse.com/thing:5155562, CC BY): a printed nut with an M3 hex nut in it. That
nut slides in from an end of the rail. This one is a new design that rolls in.

The drive sits on four 0.5 mm pads, so its M3 x 6 screws go 2.5 mm into the drive.

Drill the four mounting holes of the Pi 5 from 2.7 mm to 3.2 mm for M3 (step 1 of the PDF).

## Fan

The Pi has a 30 x 30 x 10 mm 5 V fan with the Pi 5 fan plug (the same as the Slate) and stick-on
heatsinks. The fan is inside the pod, over the centre of the SoC, and blows onto the Pi. The air
comes in through the grille.

- The grille is the flat grille of the first pod, with no change. If you printed it before the
  fan bump, use it. Its pocket moved 15 mm to the rear with the Pi, so its vent slots are over
  the fan.
- The pod is 39 mm out of the cover, 13 mm deeper than the first pod. Under the grille, the pocket
  goes 2 mm deeper for the fan plate, to a 45 degree ledge. The head does not move: two notches in
  the front edge of the pod take the heads of the two screws into the left wall of the head.
- The fan plate is a separate part: a 2 mm plate with the outline of the grille, a 28 mm hole for
  the air, and four posts for the fan. The grille holds the fan plate on the ledge. A part
  of the pod cannot hold the fan: the fan is under the middle of the grille window, and the pod
  prints outer face down, so a part there has nothing under it in the print.
- The fan goes onto four posts of the fan plate, one in each fan hole (the holes are on a 24 mm
  square, 3.2 to 3.4 mm across, through the 10 mm frame). The posts are 3.0 mm across and go 4 mm
  into the holes. Two diagonal posts are solid and hold the fan in place. The other two are hollow,
  with a 2.0 mm pilot hole: an M2.5 self-tapping round head screw goes into each from the far face
  of the fan, and cuts its thread in the post and then in the fan plate. The fan hole holds the
  thin wall of the post from outside. A screw of 8 to 11 mm fits; a 10 mm screw has 4 mm of thread.
- The fan plate prints grille side down, so the posts stand on it with their layers across them.
  They are short, and the screws hold the fan on the plate, so they carry little load.
- The fan is 1.3 mm above a 13 mm heatsink on the SoC. The model has a 20 x 20 x 13 mm heatsink
  on the SoC and a 12 x 12 x 13 mm heatsink on the RP1.
- The fan cable leaves the frame from a side, beside a corner hole, and does not touch the posts
  or the screw heads. It goes down past the end of the GPIO header to the fan socket of the Pi. It must be 8 cm or more, so
  that the fan plate with the fan comes out of the pod on its cable.
- To get to the fan, lift the grille out with a small screwdriver in a vent slot, then pull the
  fan plate out at its air hole. The pocket has no fingernail notch: with the deeper rim, a notch
  in its wall crashes CuraEngine in the support check of the pod.

## Cover screws

The printer has cover PP-FP0164 (10 screws). The plate uses its upper pair of screws.
`shape.py` has their positions in one table, `COVER_SCREWS_MM`: (mm above the bottom of the
chassis, mm from the front face of the chassis).

- Front: (239.8, 6.1). Measured on the printer: 237.3 mm from the bottom edge of the cover, and
  3.6 mm from the front edge of the cover (about +-0.5 mm). The bottom of the cover is 2.5 mm above
  the bottom of the chassis, and the cover is 2.5 mm in from the front face (LulzBot model). The
  drawing gives (240.0, 6.0): the measurement agrees.
- Rear: (239.8, 163.9). From the drawing, at the same height, mirrored to 3.6 mm from the rear
  edge of the cover. To be confirmed.

The pads have slots 10 mm long (up and down) and 4.4 mm wide (+-0.5 mm front to back). Type new
values in the table and run `make.sh`.

## Software

The display is turned 180 degrees from its native position. Set `display_rotate=180` in
`printdeck.txt` on the PRINTDECK partition of the drive; the deck then uses its wide layout. If
the picture is upside down, set `display_rotate=0`.

## Display dimensions

The display dimensions come from the Adafruit CAD (in `ref/adafruit/`): the YX700WV03-T panel
datasheet, the Eagle board file of the 7 inch backpack, and the STEP model of the panel. The
Slate code has values that differ:

| Item | Adafruit CAD | Slate | Difference |
|---|---|---|---|
| Panel outline (LCM), long side | 164.9 (datasheet), 165.0 (STEP) | 166.0 | Slate pocket is 1.7 mm too long |
| LCD active area | 154.08 x 85.92 | 157 x 89 | Slate is 2.9 x 3.1 mm larger |
| Opening (active area + 0.5 each side) | 155.08 x 86.92 | 158 x 90 | Slate shows 1.5 mm of black border per side |
| Active area offset (portrait, a, b) | +2.26, +1.49 | +2.29, +1.10 (measured) | b differs by 0.39 mm |
| Board hole pitch, short side | 108.2 (brd) | 108.1 | 0.1 mm |
| HDMI socket centre | 0.9 mm from the board centre (brd) | 0 | 0.9 mm |
| Panel with touch panel, thickness | 4.9 | 7.35 (glass to board) | 2.45 mm gap |

The STEP model of the panel is an approximation: its active area is 156 x 88.9 with an offset of
about 1 mm from the datasheet. This mount uses the datasheet. The 2.45 mm between the panel and
the board is the space for the FPC fold; put foam tape there, so the glass stays on the lip.

## To check on the printer

- The position of the rear cover screw (see above).
- The height of the logo. The LulzBot drawing gives 264 mm above the bottom of the chassis, and
  the drawing is not to scale. The top of the head is at 260 mm.
- The front slot of the rail: free from 150 to 230 mm above the bottom of the chassis.
- With the bed at the front and the X axis at the bottom, the head is clear of the bed, the
  print, and the tool head.

## Files

- `shape.py` contains all dimensions. Change a value here, then run `make.sh`.
- `parts.py` makes the printed parts, the hardware, and the cables in build123d.
- `workhorse.py` reads the LulzBot model (`ref/lulzbot/final_assembly.fcstd`), the chassis and
  cover positions, and the cover screw holes of both covers.
- `model.py` builds the parts and checks the fit with a true boolean test of every pair:
  - no hardware part or cable touches a printed part, and no two printed parts overlap,
  - no part touches the LulzBot model (the chassis, the cover, the frame, the X axis, the bed),
  - each rail clip is on the centre of the rail slot, and each cover screw is within its slot over
    a hole of cover PP-FP0164,
  - every screw, washer, and insert is in the hardware list, and the counts are in stock,
  - the printer USB and the power cable go out of the rear notches, and 25 mm behind the chassis
    they touch no part of the printer,
  - the head drops onto the clip keys, and the pod slides onto its hooks, without a touch,
  - the fan sits on the fan plate with a post in each of its holes, the screws reach the hollow
    posts, there is 1 mm or more of air over the highest heatsink, and the fan plate with the fan, then
    the grille, go into the pod without a touch,
  - the printed roll-in nut is clear of the rail and the clip, it is on the lips, and the clip
    screw goes through the full thread of its hex nut.
  It writes `cad/*.step` (each part and the assembly, on the printer) and `stl/*.stl`.
- `roll_nut.py` checks that the printed roll-in nut rolls into the slot of the rail: in the
  section of the rail, it finds a path of turns and moves from outside the rail to the place of
  the nut, with 0.25 mm of clearance.
- `fit_display.py` writes `stl/fit_display.stl`.
- `cura/make_profiles.py` writes the Cura profiles, slices each part for the filament and the
  print time, and slices each part again with supports on (from the Slate).
- `render.py` writes the colour images in `renders/` (headless OpenGL with EGL).
- `camera_options.py` has the camera and lens options for the camera of the printer, and their
  fields of view (pinhole model, no lens distortion). In `live.py`, `l` selects the next option,
  `t` the next camera position (the C270 mount, or over the middle of the bed), and `k` shows the
  view of the camera. Each position starts with the camera to buy, the Arducam B0561.
  `camera_preview.py` writes the view of each option from each position to
  `renders/camera-preview/`, with a contact sheet for each printer, `chosen.png`, `mini2-places.png`
  (other places for the camera on the Mini 2 frame), and `table.csv`.
- `camera_models.py` has placeholder models of the B0561 (all sizes are guesses until measured), a
  link plate for it on the arm of the C270 mount, a plate for it over the bed on the top of the
  frame, and the cable routes to the Pi. pod.py puts them in the live window.
- `lineart/render.mjs` writes the line-art images of the PDF (three.js in headless Chromium, from
  the Slate). `make_pdf.py` writes `printdeck-lulzbot-build.pdf` (same layout as the Slate guide).
- `make.sh` runs all steps (about 2 to 3 minutes). It stops if a check fails.

## Reference files

`ref/` has the files that the model uses, with their sources:

- `ref/lulzbot/`: LulzBot TAZ Workhorse v1.0.2 production files, from
  https://download.lulzbot.com/TAZ/TAZ_WE/v1.0.2/ (`production_parts/assemblies/sub_assemblies/`,
  `production_parts/machined_parts/electronics_chassis*/`, `production_docs/bom/`). License:
  GPLv3 or later and CC BY-SA 4.0 (`COPYING`, `LICENSE-CC`, `LICENSE-GPL`). The lulzbot3d
  organisation on GitHub has the firmware and the slicer, but no hardware files of the Workhorse.
- `ref/adafruit/2354_7in_TFT_Touchscreen_v3.step` and `.png`: from
  https://github.com/adafruit/Adafruit_CAD_Parts (folder `2354 7in TFT Touchscreen`). License: MIT
  (`Adafruit_CAD_Parts_LICENSE.txt`).
- `ref/adafruit/Adafruit_7in_HDMI_Backpack.brd` and `.sch`: from
  https://github.com/adafruit/Adafruit-7-HDMI-Backpack-PCB. License: CC BY-SA
  (`backpack_README.md`, `backpack_license.txt`).
- `ref/adafruit/7in_display_datasheet_c3003.pdf`: the 7 inch panel datasheet, from
  https://cdn-shop.adafruit.com/product-files/2407/c3003.pdf (linked on the downloads page of
  https://learn.adafruit.com/adafruit-5-800x480-tft-hdmi-monitor-touchscreen-backpack). No license
  is given.
- `ref/adafruit/adafruit_7inch_backpack_dimensions.png`: the 7 inch backpack drawing from the same
  downloads page. No license is given.

## Needs

`uv` (build123d, trimesh, pyrender), an OpenGL driver with EGL, Node.js, Chromium, ImageMagick 7,
and CuraEngine from the Cura AppImage in `/opt/ultimaker-cura` (the same as the Slate).
