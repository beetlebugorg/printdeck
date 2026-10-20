# Printdeck OS

A small Linux system for the Printdeck Raspberry Pi 5. It holds only what Printdeck needs:

- Klipper, at the version of the printer's firmware
- The Printdeck program (`deck/`): the touch screen, the remote screen in the browser, and an
  OctoPrint-compatible API for slicers
- go-librespot (Spotify Connect) and CamillaDSP (the equalizer)
- Wi-Fi, mDNS (`printdeck.local`), NTP, and SSH (Dropbear)

It has no package manager, no Docker, and no compiler. Buildroot makes the system from source.
This image is a step to the goal of the project: one Go program on the Linux kernel. See
`ARCHITECTURE.md` in the top folder.

## Build

You need Docker and about 20 GB of free disk. Then run:

```sh
os/build.sh
```

The first build takes about one hour. The next builds take a few minutes. The output is:

| File | What it is |
|---|---|
| `os/out/printdeck.img` | The full disk image |
| `os/out/printdeck-update.tar` | The update bundle for a Printdeck that runs this system |

Git ignores `os/out/`. The Buildroot tree, the downloads and the build files are in
`~/.cache/printdeck-os`. Set `PRINTDECK_OS_CACHE` to use a different folder. Only one build can
use the folder at a time (`build.lock`).

Other Buildroot targets go through the same script, for example:

```sh
os/build.sh menuconfig          # change the system configuration
os/build.sh savedefconfig       # write it back to os/configs/printdeck_defconfig
os/build.sh printdeck-deck-rebuild all   # build the Printdeck program again after a change in deck/
os/build.sh clean               # delete the build files, for a build from the start
```

Buildroot does not delete the files of a package that you turn off. After such a change, build
from the start (`os/build.sh clean`, then `os/build.sh`).

## Install

The image works on an SD card and on a USB SSD. Write it with Raspberry Pi Imager
("Use custom"), or with `dd`:

```sh
sudo dd if=os/out/printdeck.img of=/dev/sdX bs=4M conv=fsync status=progress
```

A Pi 5 starts from USB when no SD card is in it. To set this, run `sudo raspi-config` on
Raspberry Pi OS: Advanced Options > Boot Order.

## The first start

The touch screen shows the Printdeck start screen until the Printdeck program starts. To see the
boot messages instead, hold a finger on the screen for one second during the start. Then the
touch screen program does not start, so the messages stay. Start the Pi again to go back.

The first start makes the data partition fill the disk, and makes the login (see below). Then
the touch screen opens the setup screen:

1. The Wi-Fi networks show. Tap one and type its password, or tap Hidden for a network that is
   not in the list. The list scans again by itself.
2. The summary shows the addresses of the Printdeck, the user name, the password and the SSH
   command.

To see the summary again, open Print > Control > Help. The Help screen also has a button for the
Wi-Fi.

You can also set up before the first start. Put the disk in a computer and open the `PRINTDECK`
partition. Fill in `printdeck.txt`:

| Setting | What it does |
|---|---|
| `wifi_ssid`, `wifi_password`, `wifi_country` | The Wi-Fi network. The Pi clears the password in the file after it reads it. |
| `ssh_key` | A public SSH key for the user `printdeck` |
| `printer.cfg` (a file, next to `printdeck.txt`) | The Klipper configuration (see "Klipper") |
| `spotify_client_id` | The client ID of your Spotify app (see "Spotify") |
| `hostname`, `timezone` | The name on the network (`printdeck.local`) and the time zone |

## The login

At the first start, `printdeck-credentials` makes the user `printdeck` with a random password of
four words, for example `stamp-ocean-cider-brick`. The words come from the EFF short word list,
so the password is easy to type and has about 41 bits of randomness. The same user and password
log in to:

- SSH, and `sudo`
- the remote screen

The touch screen shows the password on the Help screen. The password is in
`/var/lib/printdeck/password`, and only root can read it. To change the SSH password, run
`passwd` over SSH.

## The remote screen

Open `http://printdeck.local` in a browser and log in with the password of the `printdeck` user.
The page shows the touch screen live, and clicks and taps on it work as touches. The page has a
link to the source code of the running version, because Printdeck is under the AGPL-3.0.

The remote screen uses plain HTTP, so use it only on your own network.

## Slicers

The Printdeck answers the OctoPrint API that slicers use to send files and start prints. The
slicers see a printer named "Printdeck". The API is on port 80, and the files go to the folder of
Klipper's virtual SD card (`/var/lib/klipper/gcodes`). Klipper then prints the file itself.

- **Cura** (the OctoPrint Connection plugin): the Printdeck shows in the list of OctoPrint
  printers. Select it and tap "Request". The touch screen asks "Allow Cura to use the
  Printdeck?". Tap Allow.
- **OrcaSlicer, PrusaSlicer**: add a physical printer of the type OctoPrint, with the address
  `http://printdeck.local`. For the API key, run `sudo cat /var/lib/printdeck/api.env` over
  SSH.

The touch screen shows the uploads in the file list of the Print tab.

## Klipper

The Klipper configuration is `/var/lib/klipper/config/printer.cfg`. To bring your own, put it on
the `PRINTDECK` partition as `printer.cfg`, from any computer. At the next start, the Pi copies it
to Klipper and renames the file on the partition to `printer.cfg.imported`. So a later
`SAVE_CONFIG` on the Pi stays.

The last line of `printer.cfg` must include the sections that Printdeck needs. The import adds it:

```
[include /usr/share/printdeck/klipper/printdeck.cfg]
```

That file sets `[virtual_sdcard]` to the upload folder, and adds `[pause_resume]` and
`[display_status]`. It must be the last line, because Klipper uses the last value of a setting:
then the upload folder wins over a `[virtual_sdcard]` path of another setup. If your
configuration has a `CANCEL_PRINT` macro, the Cancel button runs it.

The printer's board keeps its firmware. The Klipper version in `package/klipper` must match the
firmware on the board.

## Spotify

The Spotify app finds the Printdeck as a Spotify Connect speaker named "Printdeck". The Music tab
of the touch screen also needs the client ID of your own Spotify app:

1. Make an app in the Spotify developer dashboard.
2. Add the redirect URI `https://printdeck.local:8888/callback` to the app.
3. Put the client ID in `printdeck.txt` (`spotify_client_id`), and start the Pi again.
4. The Music tab shows a QR code. Scan it with your phone and log in to Spotify.

## Settings

The settings of the Printdeck program are in `/etc/printdeck/deck.env`, for example the screen
rotation, the touch axes and the volume range. Edit them over SSH, then run
`sudo systemctl restart printdeck-deck`.

## Update

The disk has two system slots, A and B. The running system stays as it is while an update writes
the other slot:

```sh
scp os/out/printdeck-update.tar printdeck@printdeck.local:/data/
ssh printdeck@printdeck.local sudo printdeck-update /data/printdeck-update.tar
```

The Pi restarts into the new slot one time. If Klipper and the touch screen start,
the new slot becomes the default. If they do not start, or the system stops, the Pi starts the
old slot again.

## How it works

| Partition | File system | Contents |
|---|---|---|
| 1 `PRINTDECK` | FAT | `autoboot.txt` (the A/B switch) and `printdeck.txt` |
| 2 boot A, 3 boot B | FAT | Kernel, device trees, overlays, `config.txt`, `cmdline.txt` |
| 4 root A, 5 root B | EROFS | The system, read-only |
| 6 data | ext4 | The changes to `/etc`, `/var`, `/home` and `/root`. It fills the disk after the first start. |

The root file system is read-only. At the start, `printdeck-init` mounts the data partition and
puts a writable layer from it over `/etc`, `/var`, `/home` and `/root`. It starts the start screen
(`printdeck splash`), and then systemd. To reset the system, delete the files in the data
partition. If `printdeck-init` cannot continue, it writes the kernel log to `printdeck-boot.log`
on the `PRINTDECK` partition and starts a shell on the console.

The Pi firmware selects the slot with `autoboot.txt` and its `tryboot` mode. `printdeck-update`
writes the other slot and restarts with `tryboot`. `printdeck-commit` changes `autoboot.txt`
after a good start.

The journal is on the data partition (`/var/log/journal`), so the logs of the last starts stay. To
read them on another computer, copy the folder and run `journalctl --directory`.

The boot partitions have the overlays from the kernel build, so they match its device trees. The
Pi 5 with the D0 chip needs `overlays/bcm2712d0.dtbo`: the firmware applies it by itself, and
without it the kernel stops at boot.

### Size

The image keeps only what Printdeck uses:

- Python is only for Klipper's host (klippy), until the Go port replaces it. Python has only the
  `zlib` module of its optional modules. The libraries of klippy have `.pyc` files and no sources,
  tests or type stubs (`board/python-sourceless.sh`). klippy keeps its sources, because it finds
  its modules by the `.py` file names.
- No OctoPrint: the Printdeck program has the OctoPrint-compatible API that slicers use.
- No hardware database, console keymaps or `timedated`, and no command-line tools that come with
  libraries (for example `sqlite3`, `flac`, `systemd-nspawn`).
- The kernel modules are not compressed, because Buildroot's `depmod` cannot read compressed
  modules, and EROFS compresses the root file system already. A list of the modules that the
  Printdeck uses is next.

### Files

| Path | What it is |
|---|---|
| `build.sh`, `docker/` | The build script and its container |
| `configs/printdeck_defconfig` | The Buildroot configuration |
| `board/` | Boot files, partition layout, kernel and Busybox options, and the post-build scripts |
| `board/rootfs/` | The files of the root file system: services, settings, and the scripts below |
| `board/rootfs/usr/bin/` | `printdeck-init`, `-settings`, `-credentials`, `-wifi`, `-user`, `-update` and `-commit` |
| `board/rootfs/usr/lib/systemd/system-preset/` | The services that start |
| `board/rootfs/usr/share/printdeck/` | The first settings of go-librespot, CamillaDSP and Klipper |
| `package/` | Buildroot packages for the Printdeck program, go-librespot, CamillaDSP and Klipper |
