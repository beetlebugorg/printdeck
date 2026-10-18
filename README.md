# Printdeck

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="slate/docs/hero-dark.png">
  <img src="slate/docs/hero-light.png" alt="The Printdeck Slate on its foot" width="320">
</picture>

A Raspberry Pi 5 desk slab next to the 3D printer. It runs OctoPrint, shows the print on a 7" touch
screen, and plays Spotify through passive speakers. The project started as Speakerdeck, so the
software still uses that name.

Website: https://beetlebugorg.github.io/speakerdeck/

## Hardware

| Part | Connection |
|---|---|
| Raspberry Pi 5 | Power from the Amp HAT. Do not connect USB-C at the same time. |
| HiFiBerry Amp HAT | 40-pin header, through the stacking adapter. Its 12 to 24 V supply also powers the Pi. |
| Denon SC-N10 speakers | Binding posts on the rear panel |
| 30 mm fan | Fan connector of the Pi |
| Adafruit 7" HDMI backpack display | Micro HDMI port. Portrait position in the front. |
| AR1100 touch controller | USB |
| 2.5" SSD | USB 3 port, boot device |

## Software

- go-librespot (in Docker) makes the Pi a Spotify Connect device. This needs Spotify Premium.
- CamillaDSP (in Docker) is the equalizer. go-librespot plays into the ALSA loopback (`snd-aloop`), and CamillaDSP plays from there to the Amp2.
- OctoPrint runs as a `systemd` service on port 5000.
- `deck/` is the `speakerdeck` binary (Go). It starts on boot and draws a full screen application on the 7" display, without a desktop or a browser:
  - The Music tab follows and controls Spotify with the Spotify Web API (cover, seek, previous, play, next, shuffle, repeat). go-librespot is the Connect device.
  - The Print tab has a character display (20 x 2, like the display of the printer): what it does, the progress and the time left, and the kind of move now with its speed (from the `;TYPE:` marks of Cura, PrusaSlicer, and OrcaSlicer).
  - The Print tab shows a 3D view of the model that prints (the printed height in colour), the progress, the time left, and the temperatures. It can pause, resume, cancel, preheat, and start a file. The view is a shaded STL if OctoPrint or `MODEL_DIR` (default `/var/lib/speakerdeck/models`) has an STL with a matching name. Otherwise it is the toolpath from the G-code.
  - The equalizer button opens five bands (80 Hz to 10 kHz, ±12 dB) and presets. The setting is saved in `/var/lib/speakerdeck/eq.json`. A boost lowers the overall gain by the same amount, so it cannot clip.
  - The heart adds the track to Liked Songs or removes it. The plus button adds it to one of your playlists.
  - The volume slider and the volume of the Spotify app are the same volume. They set the `Digital` control of the Amp2. The percent covers a window of decibels that fits the speakers (`VOLUME_FLOOR_DB` to `VOLUME_CEIL_DB`, -40 to -10 dB at first).
  - Control (when no print runs): home X, Y, Z; a joystick moves the head (faster further out, stops when you lift the finger); Z in steps of 0.1, 1, 10 mm; extrude and retract; fan; motors off; emergency stop. A filament swap leads through heat, unload, load, and purge.
  - Tune (while a print runs): speed and flow in percent, Z offset in baby steps, fan.
  - Tap a temperature to set it with a keypad.
  - Tap the clock to choose the day theme, the night theme, or auto (night from `NIGHT_FROM` to `NIGHT_TO`, 20 to 7). The choice is saved in `/var/lib/speakerdeck/theme`.
  - The status bar shows the other tab: "Printing 42%" on the Music tab, and the current track on the Print tab.

## Installation

1. Copy this directory to the Pi.
2. Set `HIFIBERRY_OVERLAY` in `speakerdeck.env`.
3. Run `setup/10-boot-config.sh`. Reboot the Pi.
4. Run `aplay -l`. Make sure that the list shows the HiFiBerry card.
5. Run `setup/30-octoprint.sh`, and make an API key in OctoPrint.
   For a printer with Klipper firmware, also run `setup/35-klipper.sh`. Read the note at the top of the script first.
6. Create an app in the Spotify developer dashboard. Add `http://127.0.0.1:8888/callback` as the redirect URI.
7. Run `setup/50-deck.sh`. It makes `.env` from `.env.example`. Fill it in (the Spotify client ID, the OctoPrint address and key, `DISPLAY_ROTATE`). Run `setup/50-deck.sh` again. It disables the desktop and starts the Docker stack.
8. Run `docker compose run --rm deck auth`. Open the address in a browser. Paste the address of the error page that follows. Run `docker compose restart deck`.
9. In the Spotify app, choose "Speakerdeck" one time. After that it is in the list of devices, and the touch screen can start the music.

To update: `git pull`, then `docker compose up -d --build`.

Settings are in `.env`: `DISPLAY_ROTATE` (90 or 270), `TOUCH_INVERT_X`, `TOUCH_INVERT_Y`, `TOUCH_SWAP_XY` (0 or 1), `VOLUME_FLOOR_DB`, `VOLUME_CEIL_DB`, `MAX_VOLUME`, `PREHEAT_TOOL`, `PREHEAT_BED`, `FILAMENT_UNLOAD_MM`, `FILAMENT_LOAD_MM`, `JOG_INVERT_Y`, `NIGHT_FROM`, `NIGHT_TO`.

`slicer/` has a Cura post-processing script that sets the speed of each move. See `slicer/README.md`.

To look at the screens without the hardware, run `go run ./cmd/speakerdeck demo /tmp/shots` in `deck/`. It writes PNG files.

## Case

The case is the Slate in `slate/`: a 35 mm slab that leans back 15 degrees on a foot, with the Pi
on the rear. `slate/README.md` lists the parts and the files.
`slate/speakerdeck-slate-build.pdf` has the parts list, the print settings, and the assembly steps.

## Work that remains

- Test with the AR1100 touch controller. Set `TOUCH_*` if the axes are wrong. Run the calibration if the corners are not exact.
- Test the Spotify and OctoPrint screens with the real services. They are built from the API documentation only.
- Webcam picture of the print on the Print tab.
