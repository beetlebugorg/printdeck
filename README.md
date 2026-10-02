# Speakerdeck

A small Spotify player and OctoPrint host on a Raspberry Pi 5.

## Hardware

| Part | Connection |
|---|---|
| Raspberry Pi 5 | Power from the Amp HAT. Do not connect USB-C at the same time. |
| HiFiBerry Amp HAT | 40-pin header, through the stacking adapter. Its 12 to 24 V supply also powers the Pi. |
| Inky wHAT, black and white | 40-pin ribbon cable to the header of the stack. Flush in the top of the case. |
| Denon SC-N10 speakers | Binding posts on the rear panel |
| 30 mm fan | Fan connector of the Pi |
| Adafruit 7" HDMI backpack display | Micro HDMI port. Portrait position in the front. |
| AR1100 touch controller | USB |
| 2.5" SSD | USB 3 port, boot device |

## Software

- `raspotify` makes the Pi a Spotify Connect device. This needs Spotify Premium.
- OctoPrint runs as a `systemd` service on port 5000.
- `inky/nowplaying.py` shows the album art, the title, and the artist on the Inky wHAT.

## Installation

1. Copy this directory to the Pi.
2. Set `HIFIBERRY_OVERLAY` in `speakerdeck.env`.
3. Run `setup/10-boot-config.sh`. Reboot the Pi.
4. Run `aplay -l`. Make sure that the list shows the HiFiBerry card.
5. Run `setup/40-inky.sh`.
6. Run `inky/venv/bin/python inky/nowplaying.py --test`. Make sure that the Inky wHAT shows the test image.
7. Run `setup/20-spotify.sh`.
8. Run `setup/30-octoprint.sh`.

## Case

The case is a cabinet with the size of one SC-N10 speaker: 153 x 233 x 196 mm.

### Files to print

The STL files are in `case/stl/`. Each file has the correct position for the print bed.

| File | Part | Cura profile |
|---|---|---|
| `case/stl/body.stl` | Body, with the rear wall | Speakerdeck - Body 0.3 |
| `case/stl/base.stl` | Base plate for the drive and the Pi stack | Speakerdeck - Base 0.3 |
| `case/stl/frame.stl` | Display frame | Speakerdeck - Display Frame 0.2 |
| `case/stl/inky_frame.stl` | Inky frame | Speakerdeck - Inky Frame 0.1 slow |
| `case/stl/cap.stl` | 8 caps on one plate | Speakerdeck - Caps 0.2 |
| `case/stl/pad.stl` | Pad behind the Inky wHAT | Speakerdeck - Pad 0.2 |

The profiles are for the LulzBot TAZ Workhorse with the 0.5 mm tool head and 2.85 mm PLA.
Close Cura, then run `case/cura/make_profiles.py --install`.

`case/speakerdeck-build.pdf` contains the build instructions and the filament estimate.

### Source

- `case/common.scad` contains all parameters.
- `case/body.scad`, `base.scad`, `frame.scad`, `inky_frame.scad`, `cap.scad`, and `pad.scad` are the printed parts, one file for each part.
- `case/speakerdeck.scad` is the assembly and the exploded view.
- `case/hardware.scad` contains the models of the purchased parts, for the images only.
- `case/vendor/` contains models from the manufacturers.
- `case/build/make.sh` makes the STL files, the images, and the PDF again.

If you change a parameter, run `case/build/make.sh` and commit the new STL files and the new PDF.

## Work that remains

- Portrait rotation of the 7" display in the OS.

- Touch UI for the Spotify controls on the 7" display.
- OctoDash on the 7" display.
- Calibration of the AR1100 touch controller.
- Print progress on the Inky wHAT.
