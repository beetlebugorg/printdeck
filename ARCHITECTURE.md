# Printdeck architecture

## The goal

Printdeck is a very small operating system with one purpose: run one 3D printer, its touch screen,
and its speakers. The goals are a small image, a fast start, and a low use of memory and CPU.

The Printdeck program is one static Go binary with no runtime dependencies: no shared libraries,
and no other programs for its work. cgo is allowed only when it links statically. Today the binary
builds with `CGO_ENABLED=0`.

The system parts stay as they are: the kernel and its firmware, systemd, `wpa_supplicant`,
Dropbear, `sudo` and the CA certificates. Buildroot makes the image.

## Where we are

| Step | What changes | State |
|---|---|---|
| 1 | A Buildroot image: read-only A/B system, Klipper, OctoPrint, go-librespot, CamillaDSP, and the deck | Done |
| 2 | The deck serves the remote screen and the API for slicers, and talks to Klipper directly. OctoPrint goes. | Done |
| 3 | A Go port of the Klipper host (`printdeck klippy`). Python goes. | Done. It printed on the TAZ Workhorse. |
| 4 | go-librespot in the deck, with pure Go decoders, a pure Go ALSA output, and a Go port of the equalizer. go-librespot, CamillaDSP, alsa-lib, amixer and the ALSA loopback go. | Done, then removed. Printdeck has no music now. |
| 5 | The deck runs as the unprivileged user `deck`. | Done in the image. To test on the Pi. |

The deck still runs two programs of the system:

- `sudo printdeck-wifi` and `sudo printdeck-user`, which change the Wi-Fi and the login as root,
  from the setup screen.
- `dmesg`, for the diagnostics of the boot splash.

## The Klipper port

A Go Klipper host can drive a heater or a motor in the wrong way. So the port follows these rules:

- It starts from the Klipper commit that the printer's firmware has, and keeps the same protocol.
- The heater safety code (the PID loop, `verify_heater`, and the limits that the host gives the
  firmware) comes first.
- The Go host must give the same board commands as Klipper's batch mode (`klippy.py -o`) for the
  printer of `printer/`, before it drives the printer. Klipper's own runs differ in the order of
  the commands of different objects, so the test compares the commands of each object, in order.
  `tools/klipper-reference` makes the reference data, and `deck/internal/klipper/PORTING.md` has
  the rules of the port.

## The deck program

`deck/` is the Go program. Its packages:

| Package | What it does |
|---|---|
| `cmd/printdeck` | The program: the deck, the Klipper host (`klippy`), and the `demo`, `remote-demo`, `splash` and `password` commands |
| `internal/klipper` | The Go port of the Klipper host |
| `internal/ui` | The screens: Print, Control, Tune, Setup and Help |
| `internal/gfx`, `internal/fb`, `internal/touch` | Drawing, the framebuffer, and the touch screen |
| `internal/printer` | The printer state, the picture of the print, and the backends. The Klipper backend uses `internal/klippy`. |
| `internal/klippy` | The client for the API socket of the Klipper host |
| `internal/gcodestore` | The G-code files, in the folder of Klipper's virtual SD card |
| `internal/octoapi` | The OctoPrint-compatible API for slicers, with App Keys on the touch screen |
| `internal/remote` | The remote screen in the browser |
| `internal/gcode`, `internal/stl` | G-code and STL reading for the picture of the print |
| `internal/lcd` | The display of the printer: the status screen, the menus and the animations |
| `internal/wifi` | Wi-Fi through wpa_supplicant |

## Open source and licenses

Printdeck stands on the work of other open source projects: Klipper, OctoPrint, Moonraker,
Buildroot and the Linux kernel. We do not take their code to hide it. We
reform it into a small, single-purpose system, in the spirit of open source, and we give the result
back under a compatible license.

Every effort is made to respect the licenses:

- Printdeck is licensed under the GNU Affero GPL, version 3 (`LICENSE`). Printdeck is a network
  device, so the AGPL also gives the source to people who use it over the network. The remote
  screen links to the source.
- Klipper is GPL-3.0. Section 13 of the GPL-3.0 and of the AGPL-3.0 lets its code and Printdeck's
  code be one program. Its files keep the GPL-3.0. OctoPrint is
  AGPL-3.0, the same as Printdeck.
- Code that is ported from another project keeps its copyright and license notices, and names the
  project and its authors at the top of the file. For example, `internal/klippy` names Klipper's
  `webhooks.py` and its author.
- Where we only speak a protocol (the OctoPrint API for slicers, the Klipper API socket), we write
  our own code from the documentation and the behaviour, and say which project defines the protocol.
- A release of the image includes the sources and the licenses of all its packages
  (`make legal-info` of Buildroot).
- The proprietary firmware of the Pi and of the Wi-Fi chip is redistributable under its own
  licenses, and the image keeps those licenses.

If you find code in Printdeck without the credit or the license that it needs, please open an issue.

## Built with AI

Much of Printdeck is written with an AI coding assistant (Claude), directed and reviewed by a
person. The same rules apply to that code: it must respect the licenses above, give credit where it
ports or follows other code, and pass its tests.
