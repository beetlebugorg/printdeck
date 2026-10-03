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

- go-librespot (in Docker) makes the Pi a Spotify Connect device. This needs Spotify Premium.
- OctoPrint runs as a `systemd` service on port 5000.
- `inky/nowplaying.py` shows the album art, the title, and the artist on the Inky wHAT.
- `deck/` is the `speakerdeck` binary (Go). It starts on boot and draws a full screen application on the 7" display, without a desktop or a browser:
  - The Music tab follows and controls Spotify with the Spotify Web API (cover, seek, previous, play, next, shuffle, repeat). go-librespot is the Connect device.
  - The Print tab shows a 3D view of the model that prints (the printed height in colour), the progress, the time left, and the temperatures. It can pause, resume, cancel, preheat, and start a file. The view is a shaded STL if OctoPrint or `MODEL_DIR` (default `/var/lib/speakerdeck/models`) has an STL with a matching name. Otherwise it is the toolpath from the G-code.
  - The heart adds the track to Liked Songs or removes it. The plus button adds it to one of your playlists.
  - The volume slider sets the `Digital` control of the Amp2.
  - The status bar shows the other tab: "Printing 42%" on the Music tab, and the current track on the Print tab.

## Installation

1. Copy this directory to the Pi.
2. Set `HIFIBERRY_OVERLAY` in `speakerdeck.env`.
3. Run `setup/10-boot-config.sh`. Reboot the Pi.
4. Run `aplay -l`. Make sure that the list shows the HiFiBerry card.
5. Run `setup/30-octoprint.sh`, and make an API key in OctoPrint.
6. Create an app in the Spotify developer dashboard. Add `http://127.0.0.1:8888/callback` as the redirect URI.
7. Run `setup/50-deck.sh`. It makes `.env` from `.env.example`. Fill it in (the Spotify client ID, the OctoPrint address and key, `DISPLAY_ROTATE`). Run `setup/50-deck.sh` again. It disables the desktop and starts the Docker stack.
8. Run `docker compose run --rm deck auth`. Open the address in a browser. Paste the address of the error page that follows. Run `docker compose restart deck`.
9. In the Spotify app, choose "Speakerdeck" one time. After that it is in the list of devices, and the touch screen can start the music.

To update: `git pull`, then `docker compose up -d --build`.

Settings are in `.env`: `DISPLAY_ROTATE` (90 or 270), `TOUCH_INVERT_X`, `TOUCH_INVERT_Y`, `TOUCH_SWAP_XY` (0 or 1), `MAX_VOLUME`, `PREHEAT_TOOL`, `PREHEAT_BED`.

To look at the screens without the hardware, run `go run ./cmd/speakerdeck demo /tmp/shots` in `deck/`. It writes PNG files.

The Inky wHAT is not part of the Docker stack yet. `setup/40-inky.sh` still installs its Python service.

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

- Test with the AR1100 touch controller. Set `TOUCH_*` if the axes are wrong. Run the calibration if the corners are not exact.
- Test the Spotify and OctoPrint screens with the real services. They are built from the API documentation only.
- Print progress on the Inky wHAT.
- Webcam picture of the print on the Print tab.
- Put the Inky wHAT service in the Docker stack. `inky/librespot-event.sh` and the raspotify setup are no longer used.
