# Printdeck architecture

## The goal

Printdeck is a very small operating system with one purpose: run one 3D printer, its touch screen,
and its speakers. The aim is one Go program on top of the Linux kernel, with as few dependencies as
possible.

At the end, the root file system is like a `FROM scratch` container image:

```
/sbin/init                          the Printdeck binary (static, PID 1)
/etc/ssl/certs/ca-certificates.crt  for TLS to Spotify and GitHub
/lib/firmware/...                   the Wi-Fi and Bluetooth firmware of the Pi 5
/dev /proc /sys /data               mount points
```

The kernel has the drivers that the Printdeck uses built in, and no others. The time zone data is
in the binary (Go's `time/tzdata`). There is no package manager, no shell, no Python and no C
library userland.

## Where we are

The system gets to the goal in steps. Each step keeps the printer working.

| Step | What changes | State |
|---|---|---|
| 1 | A Buildroot image: read-only A/B system, Klipper, OctoPrint, go-librespot, CamillaDSP, and the deck | Works on the Pi 5 |
| 2 | The deck serves the remote screen, and talks to Klipper directly. A small OctoPrint-compatible API in the deck replaces OctoPrint. | Done, OctoPrint is out of the image. To test on the Pi. |
| 3 | The deck is PID 1: mounts, A/B updates, network, time, mDNS. systemd and the shell scripts go. | Planned |
| 4 | go-librespot is linked into the deck, and a Go equalizer writes to ALSA. CamillaDSP goes. | Planned |
| 5 | A Go port of the Klipper host (klippy). Python goes. | Started |
| 6 | The image is the kernel, the firmware, the CA certificates and one binary. | Planned |

Some parts stay outside Go for a time:

- Klipper's C helper (`chelper`, step compression) links with cgo first. A Go port follows when it
  gives the same output.
- go-librespot decodes Vorbis and FLAC with C libraries (cgo).
- `wpa_supplicant` stays for WPA3. Pure Go supports WPA2 only today.

## The Klipper port

A Go Klipper host can drive a heater or a motor in the wrong way. So the port follows these rules:

- It starts from the Klipper commit that the printer's firmware has, and keeps the same protocol.
- The heater safety code (the PID loop, `verify_heater`, and the limits that the host gives the
  firmware) comes first.
- Each motion feature must give the same MCU command stream as Klipper's batch mode
  (`klippy.py -o`), byte for byte, on a set of real G-code files, before it drives the printer.

## The deck program

`deck/` is the Go program. Its packages:

| Package | What it does |
|---|---|
| `cmd/printdeck` | The program: the deck, and the `demo`, `remote-demo`, `splash` and `password` commands |
| `internal/ui` | The screens: Music, Print, Control, Tune, Setup and Help |
| `internal/gfx`, `internal/fb`, `internal/touch` | Drawing, the framebuffer, and the touch screen |
| `internal/printer` | The printer state, the picture of the print, and the backends. The Klipper backend uses `internal/klippy`. |
| `internal/klippy` | The client for the API socket of the Klipper host |
| `internal/gcodestore` | The G-code files, in the folder of Klipper's virtual SD card |
| `internal/octoapi` | The OctoPrint-compatible API for slicers, with App Keys on the touch screen |
| `internal/octoprint` | The backend for OctoPrint, for the Raspberry Pi OS setup with Docker. It goes with that setup. |
| `internal/remote` | The remote screen in the browser |
| `internal/gcode`, `internal/stl` | G-code and STL reading for the picture of the print |
| `internal/spotify`, `internal/connect` | Spotify Web API and go-librespot |
| `internal/amp`, `internal/eq` | The amplifier volume and the equalizer |
| `internal/wifi` | Wi-Fi through wpa_supplicant |

## Open source and licenses

Printdeck stands on the work of other open source projects: Klipper, OctoPrint, Moonraker,
go-librespot, CamillaDSP, Buildroot and the Linux kernel. We do not take their code to hide it. We
reform it into a small, single-purpose system, in the spirit of open source, and we give the result
back under a compatible license.

Every effort is made to respect the licenses:

- Printdeck is licensed under the GNU Affero GPL, version 3 (`LICENSE`). Printdeck is a network
  device, so the AGPL also gives the source to people who use it over the network. The remote
  screen links to the source.
- Klipper and go-librespot are GPL-3.0. Section 13 of the GPL-3.0 and of the AGPL-3.0 lets their
  code and Printdeck's code be one program. Their files keep the GPL-3.0. OctoPrint is AGPL-3.0, the
  same as Printdeck.
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
