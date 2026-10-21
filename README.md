# Printdeck

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="slate/docs/hero-dark.png">
  <img src="slate/docs/hero-light.png" alt="The Printdeck Slate on its foot" width="320">
</picture>

A Raspberry Pi 5 with a 7" touch screen next to the 3D printer. It runs Klipper, and shows and
controls the print on the touch screen, on the display of the printer, and in a browser.

Website: https://beetlebug.org/printdeck/

## Goal

Printdeck is a very small operating system with one purpose. The Printdeck program is one static
Go binary with no runtime dependencies, on a minimal Linux system. We build it from open source
projects, such as Klipper. We respect their licenses, give credit, and keep our license
compatible. We are not here to take code. We reform it into something new and give it back, in the
spirit of open source, with the help of AI. `ARCHITECTURE.md` has the plan and the rules.

## Manual

The manual shows each screen of the deck with numbered callouts, and explains each part. It
has two versions, one for each mount of the display:

- Tall: [docs/manual/index.html](docs/manual/index.html), or the booklet
  [docs/manual/printdeck-manual.pdf](docs/manual/printdeck-manual.pdf).
- Wide: [docs/manual/wide/index.html](docs/manual/wide/index.html), or the booklet
  [docs/manual/wide/printdeck-manual.pdf](docs/manual/wide/printdeck-manual.pdf).
- The upgrade kit for the LulzBot TAZ Workhorse: the booklet
  [docs/lulzbot/printdeck-manual.pdf](docs/lulzbot/printdeck-manual.pdf) has the
  print plan, the hardware and the steps of the mount in [lulzbot/](lulzbot/), then the wide
  manual.

The booklet has half-letter pages. A print shop makes a saddle-stitched booklet from
printdeck-manual.pdf. To print it yourself, print printdeck-manual-print.pdf on both sides of
letter paper (flip on the short edge), fold the stack, and staple it in the fold.

The deck draws the pictures itself: after a change to the UI, run `docs/manual/build.sh`.

## Hardware

| Part | Connection |
|---|---|
| Raspberry Pi 5 | USB-C power supply, 27 W |
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
  - It draws the display of the printer (128 x 64) with the `printdeck_display` module of the
    Klipper host. The knob of the printer opens the same menus as the touch screen.

The screens of the touch screen:

- The top of each screen shows the display of the printer, with the same dots: the state, the temperatures, the fan, the position of the head, and the progress.
- The print screen shows a 3D view of the model that prints (the printed height in colour), the progress, the time left, and the temperatures. It can pause, resume, cancel, preheat, and start a file. The view is a shaded STL if the upload folder or `MODEL_DIR` (default `/var/lib/printdeck/models`) has an STL with a matching name. Otherwise it is the toolpath from the G-code.
- Control (when no print runs): home X, Y, Z; a joystick moves the head (faster further out, stops when you lift the finger); Z in steps of 0.1, 1, 10 mm; extrude and retract; fan; motors off; emergency stop. A filament swap leads through heat, unload, load, and purge.
- Tune (while a print runs): speed and flow in percent, Z offset in baby steps, fan.
- Tap a temperature to set it with a keypad.
- Machine > Theme chooses the day theme, the night theme, or auto (night from `NIGHT_FROM` to `NIGHT_TO`, 20 to 7). The choice is saved in `/var/lib/printdeck/theme`.
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

- Lower the CPU use of the screen loop.
- Test the slicer uploads (Cura, OrcaSlicer) to the Printdeck API.

## License

Printdeck is free software under the GNU Affero General Public License, version 3. See `LICENSE`,
and the "Open source and licenses" section of `ARCHITECTURE.md`.
The Printdeck program contains a port of Klipper's host (GPL-3.0). The OS image
contains the other packages of the image, each under its own license.
