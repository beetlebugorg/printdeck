# Printdeck

<img src="docs/img/pod-taz.png" alt="The Printdeck pod on the front left of a LulzBot TAZ Workhorse" width="420">

Printdeck is a touch screen and print server for Klipper printers. A Raspberry Pi 5 and a 5 inch
touch display go on the printer in a printed pod. I build it and test it on a LulzBot TAZ
Workhorse (the original Workhorse).

Website: https://beetlebug.org/printdeck/

<p>
  <img src="docs/manual/img/printing.png" alt="A print with the model up to the layer that prints now" width="190">
  <img src="docs/manual/img/first-layer.png" alt="The first layer with the Z offset" width="190">
  <img src="docs/manual/img/file.png" alt="A file with its print time, filament, roll and cost" width="190">
  <img src="docs/manual/img/filament.png" alt="The Filament page with the roll in the printer" width="190">
</p>

## Before you use it

Printdeck is a personal project, for personal use.

- Printdeck has run on one printer only: a LulzBot TAZ Workhorse.
- AI writes the code and the documentation. The code gets tests, and a person reads the documents.
  Errors can still occur.
- Results on your printer can be different. Use Printdeck at your own risk.

## Install

1. Download `printdeck.img.xz` from the [latest release](https://github.com/beetlebugorg/printdeck/releases/latest).
2. Write it to an SD card with Raspberry Pi Imager ("Use custom").
3. Put the SD card in the Pi of the pod, and connect the power. The setup connects the Wi-Fi and
   finds the printer.

To update, open Machine > Software update on the touch screen.

## Goal

Printdeck is a very small operating system with one purpose. The Printdeck program is one static
Go binary on a minimal Linux system. We build it from open source projects, such as Klipper. We
respect their licenses, give credit, and give the result back. `ARCHITECTURE.md` has the plan
and the rules.

## Manual

The manual shows each screen of the deck with numbered callouts.

- The manual: [on the web](https://beetlebug.org/printdeck/manual/), or the booklet
  [docs/manual/printdeck-manual.pdf](docs/manual/printdeck-manual.pdf).
- The manual for a wide display: [on the web](https://beetlebug.org/printdeck/manual/wide/), or
  [docs/manual/wide/printdeck-manual.pdf](docs/manual/wide/printdeck-manual.pdf).
- The Cura plugin: [on the web](https://beetlebug.org/printdeck/cura/), or
  [docs/cura/printdeck-cura.pdf](docs/cura/printdeck-cura.pdf).

To print a booklet yourself, print the `-print.pdf` file on both sides of letter paper (flip on
the short edge). Fold the stack, and staple it in the fold.

After a change to the UI, run `docs/manual/build.sh`. After a change to the Cura plugin, run
`docs/cura/build.sh`. After a change to a README or a manual, run `docs/build.py`.

## Hardware

The pod is a Raspberry Pi Touch Display 2 (5 inch, mounted tall) with a Raspberry Pi 5 on its
back, in a printed case. A printed bracket holds the pod on a TAZ Workhorse or a LulzBot Mini 2.
[lulzbot/README.md](lulzbot/README.md) has the pod and the brackets.
[lulzbot/stl/pod/README.md](lulzbot/stl/pod/README.md) has the print files.

| Part | Connection |
|---|---|
| Raspberry Pi 5 | Raspberry Pi 27 W USB-C power supply |
| Raspberry Pi Active Cooler | Fan connector of the Pi |
| Raspberry Pi Touch Display 2, 5 inch | DSI ribbon, and a power wire to the GPIO header of the Pi |
| SD card | Boot device |
| USB-A to USB-B cable | The board of the printer |

## Software

The Printdeck runs Printdeck OS, a small read-only Linux system. `os/README.md` has the build,
the install and the update.

`deck/` is the Printdeck program: one static Go binary that runs as the user `deck`. It has these
parts:

- `printdeck klippy`: the Klipper host, a Go port of Klipper's `klippy.py`. The `klipper` service
  runs it. `deck/internal/klipper/PORTING.md` has the rules of the port.
- The touch screen application, with no desktop or browser.
- The remote screen at `http://printdeck.local`: the touch screen in a browser.
- The part of the OctoPrint API that Cura, OrcaSlicer and PrusaSlicer use.
- The display of the printer (128 x 64). The knob opens the same menus as the touch screen.

The screens of the touch screen:

- Each screen shows the printer display at the top.
- The print screen shows the model in 3D, the layer, the roll and the layer times. It can pause,
  resume, tune and cancel.
- Toolhead homes and moves the head, and turns off the motors.
- Filament has the guided filament change and your rolls of filament.
- Machine has the calibrations, the firmware, the console, the settings and the history.
- Machine > Settings has the camera, the push messages and the time lapse.
- A display that is mounted wide gets a machine panel layout.

`slicer/cura-plugin/` is the Printdeck plugin for Cura. `slicer/` also has a Cura
post-processing script for the speed of each move.

## Desktop app and simulator

`desktop/` is the Printdeck for a computer: a Qt 6 window around `printdeck app`. It runs the
Klipper host and the deck UI for a printer on USB. Cura finds it as it finds a Printdeck. Ctrl+K
(Cmd+K on a Mac) opens the camera of the deck.

`printdeck app -sim` runs the deck with a simulated TAZ Workhorse. The Simulated printer page of
the deck changes it to a Mini 2. A second window shows the printer in 3D:

- The axes and the belts move with the steps of the simulated board.
- The pod is on the printer, and its screen shows the deck UI.
- The left button turns the view, the right button pans it, and the wheel zooms. T makes the
  tool head see-through.

To build it, you need Go, CMake, Ninja, and Qt 6.5 or later with Qt Quick 3D:

```sh
B=~/.cache/printdeck/desktop
(cd deck && go build -o $B/printdeck ./cmd/printdeck)
cmake -S desktop -B $B -G Ninja -DCMAKE_BUILD_TYPE=Release
ninja -C $B
$B/Printdeck -sim
```

## Demo mode

To see the screens with no hardware, run `go run ./cmd/printdeck demo /tmp/shots` in `deck/`. It
writes PNG files. For the screens in a browser, run `go run ./cmd/printdeck remote-demo` and open
`http://localhost:8080` (password `demo`).

## Work that remains

- Lower the CPU use of the screen loop.
- Test the slicer uploads (Cura, OrcaSlicer) to the Printdeck API.

## License

Printdeck is free software under the GNU Affero General Public License, version 3. See `LICENSE`,
and the "Open source and licenses" section of `ARCHITECTURE.md`.
The iPhone and Apple Watch apps in `watch/` are under the MIT License: see `watch/LICENSE`.
The Printdeck program contains a port of Klipper's host (GPL-3.0). The OS image
contains the other packages of the image, each under its own license.
