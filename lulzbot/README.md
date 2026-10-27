# Printdeck pod

<img src="../docs/img/pod-taz.png" alt="The Printdeck pod on the front left of a LulzBot TAZ Workhorse" width="480">

The pod is the hardware of Printdeck: a Raspberry Pi Touch Display 2 (5 inch), mounted tall, with a
Raspberry Pi 5 on its back, in a printed case. A printed bracket holds the pod on the printer. I
build it and test it on a LulzBot TAZ Workhorse. This folder also has a bracket for the LulzBot
Mini 2. The pod is the same on the two brackets.

[stl/pod/README.md](stl/pod/README.md) has the print files, the print settings, the hardware of
the pod and the webcam option. `pod.py` is the model of the pod and the two brackets.

## The pod

<img src="../docs/img/pod.png" alt="The pod on its bracket" width="320">

The glass of the display is the front of the pod. The glass is wider than the case, so the case is
a smaller body behind the glass. The display hangs with its ribbon end down. The Pi 5 with its
Active Cooler is on the four standoffs of the display, with the USB ports up.

The case has three printed parts. Each part prints flat with no supports.

- The frame is the thin part around the display. Four M2.5 x 4 screws from the box of the display
  hold it to the four raised tabs of the display.
- The body is the deep part around the Pi. Four M3 x 6 screws go from the back of the body into
  heat-set inserts in the frame.
- The mount is a plate with a dovetail. Two M3 x 6 screws from inside the body hold it to the back
  of the body.

The ports of the Pi are flush with the walls of the body. The USB-C power port and the two micro
HDMI ports are in the left wall. The USB-A and Ethernet ports are in the top wall. The printer cable
plugs into a USB port of the Pi from above.

The bracket of each printer has a wedge. The pod leans back 15 degrees on the wedge. A key on the
back of the wedge slides down into a groove in the bracket. The dovetail of the mount drops into a
slot in the wedge from above. To remove the pod, lift it off the dovetail and pull it forward.

## On the TAZ Workhorse

The pod is at the front left of the printer, on the front of the electronics chassis, below the TAZ
Workhorse logo. You drill no hole in the printer.

- The TAZ bracket stands on two printed rail clips (`stl/clip.stl`) in the front slot of the rail.
  Print each clip on its end, with a brim.
- An M3 x 8 screw holds each rail clip to a metal M3 roll-in T-nut for 2020 extrusion in the slot.
- A nub at the top of the bracket goes over the edge of the side cover. The upper front screw of the
  cover holds the nub.
- The printer cable goes to the USB-B port on the rear face of the chassis. The power cable goes
  along the lower edge of the side cover. Five single cable clips hold the two cables on the side
  cover, on squares of foam tape.

## On the Mini 2

<img src="../docs/img/pod-mini2.png" alt="The Printdeck pod on the top left of a LulzBot Mini 2" width="360">

The pod is at the top of the left upright, over the electronics case. The stock LCD of the Mini 2
stays on the right upright.

- The Mini 2 bracket is a plate on the front of the left upright. The two frame screws of the top
  corner hold it. Take out each screw, and put a longer screw in the same hole (M3 x 10 in the
  model).
- Two zip-tie anchors on the plate, at the right of the pod, hold the printer cable.
- The printer cable goes to the USB-B port in the front of the electronics case. Two double cable
  clips hold the power and printer cables side by side on the front of the electronics case.

## What you need

- A Raspberry Pi Touch Display 2, 5 inch. Its box has the DSI ribbon, the power wire and the M2.5
  screws.
- A Raspberry Pi 5 with the Raspberry Pi Active Cooler.
- An SD card for Printdeck OS. See [Install](../README.md#install).
- The Raspberry Pi 27 W USB-C power supply.
- A USB cable from USB-A to USB-B, from the Pi to the printer.
- The printed parts and the hardware in [stl/pod/README.md](stl/pod/README.md). For the TAZ Workhorse, also two rail
  clips (`stl/clip.stl`), two M3 x 8 screws and two M3 roll-in T-nuts.

## Software

The display hangs with its ribbon end down, so the deck turns the picture 180 degrees. Set the turn
on the Settings page of the deck, or with `display_rotate=180` in `printdeck.txt`. The deck uses its
tall layout on the pod. The [manual](../docs/manual/printdeck-manual.pdf) shows the tall screens.

## Files

- `pod.py` is the model of the pod, the two brackets and the wedge, in build123d. It checks that no
  two parts overlap. `pod.py export` writes the print files. `pod.py overhangs` checks the print
  files for faces that need supports.
- `fit_holes.py` writes a fit test for the display holes of the frame
  (`stl/pod/fdm/fit_holes_fdm.stl`).
- `clips.py` has the cable clips. `camera.py` has the webcam mounts. `camera_models.py` has
  placeholder models of other cameras, for the layout only.
- `cable_physics.py` gives the shapes of the cables in the 3D window of the model.
- `live.py` and `viewer.py` show the model in a 3D window while it changes.
- `workhorse.py` reads the CAD of the TAZ Workhorse. `parts.py` and `shape.py` have the rail clip
  and the sizes of the rail.

The other scripts in this folder are for an earlier mount with a 7 inch display.

## Reference files

`ref/` has the files that the models use, with their sources:

- `ref/lulzbot/`: LulzBot TAZ Workhorse v1.0.2 production files, from
  https://download.lulzbot.com/TAZ/TAZ_WE/v1.0.2/. License: GPLv3 or later and CC BY-SA 4.0
  (`COPYING`, `LICENSE-CC`, `LICENSE-GPL`).
- `ref/raspberrypi/`: the product brief of the Raspberry Pi 27 W USB-C power supply.

## Needs

`uv` (build123d, trimesh, pyrender), and an OpenGL driver with EGL for the renders.
