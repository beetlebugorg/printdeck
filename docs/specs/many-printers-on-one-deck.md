# Many printers on one deck

Status: plan. The build has not started.

One deck drives more than one printer: for example the TAZ Workhorse on USB, a second Klipper
printer on USB, and the Phenom on the network. The touch screen shows one printer at a time. A
tap on the printer chip at the top of the screen opens the list of the printers, and a tap on a
card in the list goes to that printer. Each screen shows clearly which printer it controls.

This spec extends `resin-and-more-printers.md`. That plan gives the Phenom its own deck, and lets
a deck watch the state of other printers. This spec lets one deck drive each printer fully: the
files, the controls, the queue, the calibrations and the camera.

## Why

- A shop has more printers than decks. A Pi and a screen for each printer is a cost, and each
  deck needs its own updates, Wi-Fi and cloud link.
- Printers often stand side by side. One screen between them is enough.
- The cloud service already has many printers for each deck (`cloud/api/printers.go`: "each deck
  registers the printers that it drives or watches"). The deck is the part that has one printer.

## The rules

1. **One active printer on each screen.** The touch screen has one active printer, and each
   remote screen has its own. A remote screen never changes the printer of the touch screen.
2. **The printer is always on the screen.** Each screen shows the name and the colour of its
   printer at the top. A dialog that moves, heats, starts or stops something names the printer.
3. **No change of printer without a tap.** The deck never switches the active printer by itself.
   A message about another printer shows as a banner, and a tap on the banner switches.
4. **Colour plus text.** Each printer has a colour, but the screen never shows the colour alone.
   The name or the short label is always next to it, for people who cannot tell the colours apart.
5. **Each printer stays independent.** Each printer has its own state, history, rolls, queue,
   files, calibrations and Klipper host. A fault of one printer does not stop the others.
6. **One printer looks as today.** A deck with one printer shows no chip and no list. Its screens
   stay the same.

## How the user tells the printers apart

Each printer has an identity, set in Settings, Printers:

| Field | Example | Use |
|---|---|---|
| Name | TAZ Workhorse | The header, the list, the dialogs, the notifications, the cloud |
| Short label | TAZ | The chip when the screen has little room, the banners, the wide-layout rail |
| Colour | Orange | The chip, the bar at the top of each page, the card in the list, the banners |
| Kind | FDM or resin | The glyph next to the name (a nozzle or a vat) |

- The deck gives each new printer the next colour of a fixed set of 6: orange, blue, green,
  magenta, yellow and cyan. The set is for the dark and the light theme, with text that is easy
  to read on each colour. The user can change the colour, but two printers cannot have the same
  colour.
- The short label has 2 to 4 characters. The deck suggests it from the name.
- Optional: a sticker in the same colour with the short label on each printer. The roll labels
  page prints these stickers, so the printer and its screen match.

## The touch screen

### The printer chip and the colour bar

Each page of a deck with 2 or more printers has a bar of 6 px in the colour of the active printer
at the top of the screen. The first line of the main page has the printer chip. It shows the
colour dot, the short label, the name, and a down arrow:

```
+------------------------------------------------+
|################# orange bar ###################|
|  (o) TAZ  TAZ Workhorse            v   9:05 PM  |  <- printer chip: tap to open the list
|  +-----------+ +-----------+ +---------------+  |
|  | NOZZLE    | | BED       | | FAN           |  |
|  | 215°  215 | |  60°   60 | |  80 %         |  |
|  +-----------+ +-----------+ +---------------+  |
|  Printing  43 %  ·  1:12 left                   |
|  Idler tube clamp right                         |
|  ...                                            |
```

The other pages (Files, Filament, Toolhead, Machine, Camera, Console) keep the colour bar, and
show the short label in their title: "TAZ · FILES". The chip is only on the main page, so the
other pages keep their room.

### The list of the printers

A tap on the chip opens the list over the page. Each printer has a card. The card of the active
printer has a border in its colour and the mark "On screen".

```
+------------------------------------------------+
|  PRINTERS                               Close   |
|  +--------------------------------------------+ |
|  |## (o) TAZ   TAZ Workhorse        On screen | |  <- orange edge
|  |##  Printing 43 %  ·  1:12 left             | |
|  |##  [===========.............]   215° / 60° | |
|  +--------------------------------------------+ |
|  +--------------------------------------------+ |
|  |## (b) MINI  Mini 2                          | |  <- blue edge
|  |##  Ready  ·  PLA  ·  bed 24°                | |
|  +--------------------------------------------+ |
|  +--------------------------------------------+ |
|  |## (g) PHN   Phenom                 resin    | |  <- green edge
|  |##  Print done 2 min ago  ·  remove the plate| |  <- attention: a mark on the card
|  +--------------------------------------------+ |
+------------------------------------------------+
```

- Each card shows the state in one line, and the progress bar during a print. A printer that
  needs the user (done, paused, error, runout, a question of the queue) has a mark in its colour.
- A tap on a card closes the list, and the deck shows the main page of that printer. A short
  slide of the page, in the direction of the card, shows that the printer changed.
- The list is also the overview: it shows all the printers at once.
- A long press on a card opens the settings of that printer.

The list has no swipe between printers. A swipe on the main page is easy to do by mistake, and a
wrong printer is the worst error of this design.

### Banners of the other printers

When a printer that is not on screen needs the user, a banner comes down at the top of the page,
in the colour of that printer:

```
|################# orange bar ###################|
|  +--------------------------------------------+ |
|  |## (g) PHN  Print done  ·  remove the plate | |  <- green banner
|  |##                          Show   Later    | |
|  +--------------------------------------------+ |
```

- Show switches to that printer. Later hides the banner. The printer keeps its mark in the list.
- A banner does not cover a dialog or a keyboard. It waits until the dialog closes.
- During an action that the user started on the active printer (a jog, a calibration, a load of
  filament), the banners wait.
- Errors of any printer always show. An error does not close the dialog of another printer.

### Dialogs that act

Each dialog that starts, stops, moves or heats has the name of the printer in its title and in
its button, and the colour bar at its top:

```
+------------------------------------------------+
|################# orange bar ###################|
|  Start on TAZ Workhorse?                        |
|  idler_tube_clamp_right.gcode                   |
|  PLA · 4.1 g · 1:18                             |
|                                                 |
|  [ Cancel ]          [ Start on TAZ ]           |
+------------------------------------------------+
```

- The emergency stop on a page stops only the printer of that page: "Stop TAZ". The list of the
  printers has "Stop all", with a confirmation.
- The queue question at the end of a print names the printer: "Mini 2 is free. Start the next
  file?"

### The wide layout

A wide screen has a rail at the left with a tab for each printer: the colour, the short label,
and a ring of the progress. The active tab has the colour as its background. A tap on a tab
switches. With the room of the wide screen, the rail replaces the chip and the list.

## The remote screen and the web pages

- Each remote screen has its own active printer. Its address has the printer:
  `http://printdeck.local/?printer=taz`. A new remote screen opens on the printer of the touch
  screen.
- The remote screen has the same chip, list, colours and banners as the touch screen.
- `/system` has a section for each printer: its Klipper service, its printer.cfg editor and its
  logs. The page of the deck (the update, the network, the backup) stays one page.

## The slicers and the API

The OctoPrint-compatible API has one printer at each address, and the slicers expect `/api` at the
root. So each slicer API key belongs to one printer:

- Settings, Printers, a printer, Slicer keys: the keys of that printer. A request with a key acts
  on the printer of the key. Cura, PrusaSlicer and the Action need no change.
- Each printer also has its API under `/printers/<id>/api/...`, for clients that take a base URL.
- `/slicer/printer.json` becomes a list: one entry for each printer, with its name, its colour
  and its kind. zslice adds each printer of the deck with one tap.
- An old key from before this change belongs to the first printer.

## The cloud service

The service already keeps the printers of each deck: the deck registers each printer with its own
key. The changes are small:

- The deck registers each printer that it drives, with its name, colour and kind.
- A plate sent to a printer goes to the queue of that printer on the deck.
- The live state goes up for each printer.
- The project pages show the colour of each printer next to its name.

## Inside the deck

### State on the SD card

Each printer has a folder: `/var/lib/printdeck/printers/<id>/`, with `printer.json` (the identity
and the kind), the history, the rolls, the bed mesh, the queue, the time lapses, the slicer keys,
and for Klipper its `printer.cfg` and G-code folder. The files of the deck stay at the top:
the password, Wi-Fi, the cloud identity, the HTTPS name, and the settings of the screen.

At the first start after the update, the deck moves the files of its printer into
`printers/<id>/` of the first printer. A deck with one printer then has one folder.

### One Klipper host for each printer

- `klipper@<id>.service` runs `printdeck klippy` with the `printer.cfg` of that printer, the
  socket `/run/klipper/<id>/klippy.sock` and the log `/run/klipper/<id>/klippy.log`.
- Each host is a separate process, so a fault in one host does not stop the others.
- The Pi 5 has 4 cores. The deck gives each Klipper host its own core when it can (CPU affinity),
  and keeps the screen and the web server on the first core.
- Each printer.cfg names its MCU by `/dev/serial/by-id/...`, so the USB order does not matter.
  Settings, Printers, Add finds the serial devices that no printer uses, and shows the board of
  each one.

### The deck program

- `printerBackend` stays as it is. The deck has a list of backends, one for each printer. Each
  backend runs its loop all the time, also when it is not on screen.
- `ui.App` gets the active printer from the list, in place of one `Printer`. Each page reads the
  active printer when it draws. The pages keep their state for each printer, so a switch back
  shows the page as it was.
- The queue, the notifications, the time lapse, the bed cool, the camera and the share link run
  for each printer.
- An update installs when each printer is idle. During a print on any printer, it waits.

### Cameras

Each printer can have a camera. Settings, Printers, a printer, Camera picks a camera by its USB
path. The Camera page shows the camera of the active printer.

## Limits to check before the build

1. **Timing.** Klipper needs its host to answer the MCU on time. Measure 2 and 3 Klipper hosts on
   one Pi 5 at the highest speeds of the TAZ, with the touch screen, a camera and a time lapse.
   Check the logs of the MCU for "Timer too close" and for retransmits.
2. **USB.** Measure the USB power and the bandwidth with 2 MCUs and 2 cameras on one Pi 5.
3. **The count.** The first build allows 4 printers on one deck.
4. **Distance.** A deck can stand far from a printer. A press on the deck starts a print, and the
   person must see the bed of that printer. The Start dialog shows the camera of the printer
   when the printer has one. "Clear the bed" stays a check in the dialog.

## Build order

1. **The model.** The folder of each printer, the move of the files, and the list of backends
   with one printer. No change on the screen.
2. **Settings, Printers.** Add, edit and remove a printer: the identity, the kind, the serial
   device or the IP address. A second Klipper host with `klipper@.service`.
3. **The screen.** The colour bar, the chip, the list, the banners, the names in the dialogs.
   Test it in the simulator with 3 simulated printers.
4. **The web and the API.** The remote screen per printer, the slicer keys per printer,
   `/printers/<id>/api`, and `/slicer/printer.json` as a list.
5. **The cloud.** The registration, the live state and the queue of each printer.
6. **The checks on the hardware.** Two printers on the TAZ deck, with the timing and USB
   measurements above.

## Open questions

- Should the touch screen go back to a "home" printer after some minutes with no touch? The
  proposal is no: the screen stays on the printer that the user chose last.
- Should a print on another printer pause the Jog and Calibration pages of the active printer?
  The proposal is no: each printer is independent.
- Should the colours of the printers also go on the TAZ LCD or a LED strip on each printer? This
  makes the printer and its screen match.
