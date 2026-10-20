# Printdeck

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="slate/docs/hero-dark.png">
  <img src="slate/docs/hero-light.png" alt="The Printdeck Slate on its foot" width="320">
</picture>

A Raspberry Pi 5 desk slab next to the 3D printer. It runs Klipper, shows the print on a 7" touch
screen, and plays Spotify through passive speakers. The project started as Speakerdeck,
and everything now carries the name Printdeck.

Website: https://beetlebug.org/printdeck/

## Goal

Printdeck is a very small operating system with one purpose. The Printdeck program is one static
Go binary with no runtime dependencies, on a minimal Linux system. We build it from open source
projects, such as Klipper, go-librespot and CamillaDSP. We respect their licenses, give credit, and keep our license
compatible. We are not here to take code. We reform it into something new and give it back, in the
spirit of open source, with the help of AI. `ARCHITECTURE.md` has the plan and the rules.

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

The Printdeck runs Printdeck OS: a small, read-only Linux system that Buildroot makes from source.
`os/README.md` tells how to build it, write it to a disk, set it up, and update it. It has:

- `deck/`, the Printdeck program (Go). It is one static binary with no cgo. It runs as the user
  `deck`, and does these things:
  - `printdeck klippy` is the Klipper host, a Go port of Klipper's `klippy.py`. The `klipper`
    service runs it in its own process. It drives the printer. `deck/internal/klipper/PORTING.md`
    tells how the port follows Klipper.
  - It draws the full screen application on the 7" display, without a desktop or a browser.
  - It serves the remote screen at `http://printdeck.local`: the touch screen in a browser, with
    the password of the `printdeck` user.
  - It serves the part of the OctoPrint API that slicers use. Cura, OrcaSlicer and PrusaSlicer
    see a printer named "Printdeck", upload files to it, and start prints. Cura gets its key with
    a tap on the touch screen.
  - It follows the Klipper host through its API socket.
  - It is the Spotify Connect speaker: go-librespot, with pure Go decoders, plays on the Amp2
    through the ALSA kernel interface. This needs Spotify Premium.
  - It filters the sound with its equalizer, a port of the filters that Printdeck used in
    CamillaDSP.

The screens of the touch screen:

- The Music tab follows and controls Spotify with the Spotify Web API (cover, seek, previous, play, next, shuffle, repeat). The Printdeck program is the Connect device.
- The Print tab has a character display (20 x 2, like the display of the printer): what it does, the progress and the time left, and the kind of move now with its speed (from the `;TYPE:` marks of Cura, PrusaSlicer, and OrcaSlicer).
- The Print tab shows a 3D view of the model that prints (the printed height in colour), the progress, the time left, and the temperatures. It can pause, resume, cancel, preheat, and start a file. The view is a shaded STL if the upload folder or `MODEL_DIR` (default `/var/lib/printdeck/models`) has an STL with a matching name. Otherwise it is the toolpath from the G-code.
- The equalizer button opens five bands (80 Hz to 10 kHz, ±12 dB) and presets. The setting is saved in `/var/lib/printdeck/eq.json`. A boost lowers the overall gain by the same amount, so it cannot clip.
- The heart adds the track to Liked Songs or removes it. The plus button adds it to one of your playlists.
- The volume slider and the volume of the Spotify app are the same volume. They set the `Digital` control of the Amp2. The percent covers a window of decibels that fits the speakers (`VOLUME_FLOOR_DB` to `VOLUME_CEIL_DB`, -40 to -10 dB at first).
- Control (when no print runs): home X, Y, Z; a joystick moves the head (faster further out, stops when you lift the finger); Z in steps of 0.1, 1, 10 mm; extrude and retract; fan; motors off; emergency stop. A filament swap leads through heat, unload, load, and purge.
- Tune (while a print runs): speed and flow in percent, Z offset in baby steps, fan.
- Tap a temperature to set it with a keypad.
- Tap the clock to choose the day theme, the night theme, or auto (night from `NIGHT_FROM` to `NIGHT_TO`, 20 to 7). The choice is saved in `/var/lib/printdeck/theme`.
- The status bar shows the other tab: "Printing 42%" on the Music tab, and the current track on the Print tab.
- Setup (at the first start) sets the Wi-Fi. Help (Print > Control > Help) shows the addresses, the user and the password.

`slicer/` has a Cura post-processing script that sets the speed of each move. See `slicer/README.md`.

To look at the screens without the hardware, run `go run ./cmd/printdeck demo /tmp/shots` in
`deck/`. It writes PNG files. To use the screens in a browser, run
`go run ./cmd/printdeck remote-demo` and open `http://localhost:8080` (password `demo`).

## Case

The case is the Slate in `slate/`: a 35 mm slab that leans back 15 degrees on a foot, with the Pi
on the rear. `slate/README.md` lists the parts and the files.
`slate/printdeck-slate-build.pdf` has the parts list, the print settings, and the assembly steps.

## Work that remains

`ARCHITECTURE.md` has the plan to one Go program. The next steps:

- Lower the CPU use of the screen loop. With the music paused, the deck used about 18% of one core
  on the Pi 5.
- Test the slicer uploads (Cura, OrcaSlicer) to the Printdeck API.
- A time estimate from Klipper's look-ahead planner, in place of PrintTimeGenius.
- Keep only the kernel modules that the Printdeck uses.
- Klipper firmware updates from the touch screen, and Printdeck updates from GitHub releases.

## License

Printdeck is free software under the GNU Affero General Public License, version 3. See `LICENSE`,
and the "Open source and licenses" section of `ARCHITECTURE.md`.
The Printdeck program contains a port of Klipper's host (GPL-3.0), go-librespot (GPL-3.0) in
`deck/third_party/go-librespot`, and an equalizer ported from CamillaDSP (GPL-3.0). The OS image
contains the other packages of the image, each under its own license.
