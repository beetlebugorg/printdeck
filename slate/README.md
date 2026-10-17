# Speakerdeck Slate

A compact slab for the Speakerdeck hardware: Raspberry Pi 5 with the HiFiBerry Amp2, the
Adafruit 7 inch touch display in portrait, the 2.5 inch SSD with its USB adapter, and the 30 mm
fan. The slab is 132 mm wide, 221 mm high, and 35 mm deep. It leans back 15 degrees on a gray
foot.

- The slab holds only the display and the drive. The Pi and the Amp2 sit on the outside of the
  orange back plate, as a backpack. Every port has free space for a straight plug and the bend
  of its cable, and every plug ends inside the outline of the slab.
- The USB ports point up, so you connect them from the top of the case. The USB-C, micro
  HDMI, Amp2 power, and speaker terminals point to the left. The speaker wires go straight into the Amp2 terminals.
- No cable shows on the front or the sides. The display cables run inside the chin, out through
  two notches in the bottom edge of the back plate, and up the rear: the HDMI ribbon at the
  left, the touch USB at the right. The HDMI cable is a flat
  ribbon: a round HDMI cable cannot bend in the 10 mm under the display.
- The drive cable (micro-B SuperSpeed to USB-A) goes through a notch in the top edge of the
  back plate and down into the USB 3 port. It is short: 15 to 20 cm.
- The fan sits under the Pi, on four 5 mm bosses, with four M3 screws. A heatsink cools the
  SoC.
- The back plate slides up into grooves in the slab and the chin. The chin snaps on with two
  tongues and holds the back plate. Screws hold only the display, the drive, and the Pi, into
  heat-set inserts. No part needs supports.

`speakerdeck-slate-build.pdf` has the parts list, the print settings, and the assembly steps.

## Parts

| File | Colour | Print |
|---|---|---|
| `stl/slab.stl` | Navy | Front face down |
| `stl/chin.stl` | Orange | Front face down |
| `stl/back.stl` | Orange | Front face (drive side) down |
| `stl/foot.stl` | Gray | Flat, hooks up |

The STL files are in their print orientation. A slice of each part with supports on (support
angle 60 degrees, supports everywhere) adds 0 g of support. `make.sh` checks this.

Use the Cura profiles in `cura/profiles/`: run `python3 cura/make_profiles.py --install` with
Cura closed.

## Files

- `shape.py` contains all dimensions. Change a value here, then run `make.sh`.
- `parts.py` makes the printed parts, the hardware, the plug spaces, and the cables in build123d.
- `model.py` checks the fit with a true boolean test of every pair (no hardware, plug space,
  or cable touches a printed part; no two printed parts overlap; no two hardware parts or
  cables overlap, except the pairs that mate) and writes `cad/*.step` (each part and the assembly with colours),
  `stl/*.stl`, and the meshes for the renders.
- `cura/make_profiles.py` writes a Cura profile for each part, slices each part with CuraEngine
  for the filament values in the PDF, and slices each part again with supports on.
- `render.py` renders the images in FreeCAD. `make_pdf.py` writes the PDF.
- `make.sh` runs all steps (about 2 minutes). It stops if the fit check, the mesh check, or
  the support check fails.
- `assets/screen.png` is the screen image of the renders.

## Needs

`uv` (build123d, trimesh), FreeCAD 1.x (`freecad`), `xvfb-run`, ImageMagick 7, Chromium, and
CuraEngine from the Cura AppImage in `/opt/ultimaker-cura`.
