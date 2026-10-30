# The resin printer and more printers in the projects

Status: plan. The build has not started.

The deck watches more printers than the printer it drives. The first one is a Peopoly Phenom, a
resin printer on the network. The deck reads its state during a print, and at the end it adds the
print to a project, as it does for the TAZ. The project pages show the prints of each printer,
with the facts of each kind of print: filament for a FDM print, resin for a resin print.

The Phenom gets its own deck: a Pi and a touchscreen in a pod on the Phenom, with Printdeck and a
backend for its ChiTu board in place of Klipper. The ChiTu board and its firmware stay.

The service has many printers for one project. The project is the center: its prints come from
each printer of the owner, through each deck. A deck is the way a printer reports to the service.
Each deck connects to the service and adds its prints, its live tiles and its photos to the
projects. The list of the projects is at the service, and each deck and the Cura plugin pick a
project from that list. Each deck keeps its own history, rolls, settings and time lapses: only
the list of the projects is shared.

## Why

The projects show only the prints of the deck. A project often has parts from the resin printer
too, and these parts have no record in the project. A print of 18 hours on the Phenom gets no
record of its time, its resin, or the layer where it failed.

## What the user sees

1. The deck has a printer list: Settings, Printers. The TAZ is the first printer. The user adds
   the Phenom with its IP address. The deck checks the address and shows the board and its
   firmware.
2. The status panel of the deck has a line for each other printer that prints: "Phenom · 62 % ·
   Z 41.2 mm · 7 h left". A tap opens the page of that printer.
3. The user picks the project of a Phenom print on that page, as for the TAZ.
4. At the end of the print, the deck adds it to the project. The project page shows it as a tile
   with a resin badge. Its page has the layer chart, the resin, the times and the photos.
5. The numbers of a project show the filament in grams and the resin in millilitres, each with
   its prints.

## The Phenom: what was checked

On 2026-10-30 the Phenom was at 192.168.76.58 and printing. It answered three queries on UDP port
3000 with the ChiTu protocol. Each query is one line of text in one packet, and each answer is one
line:

| Query | Answer | Meaning |
|---|---|---|
| `M99999` | `ok MAC:00:e0:4c:18:00:29 IP:192.168.76.58 VER:V1.4.1 ID:29,00,18,00,0b,47,39,33 NAME:CBD` | The board: a ChiTu board, firmware V1.4.1 |
| `M27` | `SD printing byte 78174460/79439932` | The bytes of the file that the board read, and the size of the file |
| `M4000` | `ok B:104/0 E1:-8/0 E2:72/0 X:0.000 Y:0.000 Z:65.147 F:256/256 D:78174460/79439932/0 T:63309` | The state: see below |

The fields of `M4000` that are clear: `Z` is the height of the plate in mm, and `D` has the bytes
read and the size of the file, as `M27`. These meanings are guesses, to check during a print:

- `T` is the time since the start of the print, in seconds (63309 s is 17 h 35 min).
- The third number of `D` is 1 while the print pauses.
- `B`, `E1` and `E2` are temperatures, or values that the board does not use.
- `F` is the fan speed.

The protocol has more commands, from the code of other programs that talk to ChiTu boards. None of
them was tried on the Phenom:

- `M20`: the list of the files on the card.
- `M28 <file>`, the data, then `M29`: the upload of a file.
- `M6030 <file>`: the start of a print. `M25`: pause. `M24`: resume. `M33`: stop.

The address of the Phenom comes from DHCP. A reservation in the router for its MAC address keeps
the address.

### The update file of Peopoly

`Phenom Chitu firmware 20210715.zip` from Peopoly has four files:

- `update.LCD`: the firmware of the main board, in the update format of ChiTu boards: the magic
  `0x443D2D3F`, a key, a CRC, and blocks of 0x800 bytes with an XOR of each byte. Klipper has the
  same format in `scripts/update_chitu.py`. The file decodes with a correct CRC. The firmware is
  `V4.4.3-h4_LCDH`, it starts at `0x08020000` after the bootloader, and its stack is at
  `0x2001C470` (more than 112 KB of RAM). Its touchscreen UI uses emWin, a TFT driver
  (`ilixxxx`), and a resistive touch panel with a calibration of its 4 edges. The board answered
  `VER:V1.4.1` to `M99999`, so it is not known if the board has this firmware.
- `FPGA_211.CBD`: the image of the FPGA, with no encryption. It is version 2.11.
- `Phenom-UI.bin`: the pictures of the touchscreen (480 x 272) for its SPI flash.
- `Phenom.gcode`: the settings of the board as `M8xxx` commands: the lift of 8 mm, the Z step of
  0.00125 mm, the Z travel of 400 mm, the speeds, and the control of the UV light.

The firmware has text for `M6030` (the start of a print by file name) and a status line with the
name of the file (`U:'%s'`).

## The parts that exist

- `printer.Record` (`deck/internal/printer/history.go`) is a print of the deck, and
  `cloud.PrintRequest` (`deck/internal/cloud/wire/projects.go`) is the print that the deck sends to
  a project. Both are for a FDM print: filament, grams of a roll, a G-code file.
- `cmd/printdeck/cloudprojects.go` sends the prints of the deck to their projects at the end of each
  print, and `cloudlive.go` sends the live tile of a print that runs.
- `PrintRequest.Printer` has the name of the printer, and the print page shows it.
- The project pages (`cloud/cmd/pwf-web`) draw the numbers of a project from the prints:
  `drawStats` adds the grams, and lists the materials.

## The design

### 1. The printers of the deck

A new file in the state folder, `printers.json`, has the other printers. Each printer has an ID, a
name, a kind (`chitu` first), and an address. The deck keeps one watcher for each printer. A
watcher has the same interface for each kind: the state now, and a record at the end of a print.

The record of another printer goes into the history of the deck with a new field, `Printer`, the ID
of the printer. A record with no `Printer` is a print of the TAZ, so the old history stays valid.

### 2. The watcher of a ChiTu board

- It sends `M4000` every 5 s while the board prints, and every 60 s while it is idle. One query at a
  time, with a timeout of 2 s.
- The state: idle, printing, paused, or offline (3 queries with no answer).
- A print starts when `D` shows a file and the bytes read grow. It ends when the bytes read reach
  the size of the file, or when the board is idle again before that (stopped).
- The progress is the share of the file read. When the deck has the file (part 3), it changes the
  bytes to a layer with the offsets of the layers in the file. The layers can differ in size, so
  the bytes and the layers can grow at different rates.
- The time left: from the elapsed time and the progress, or from the times of the layers of the
  file when the deck has the file.

The watcher sends only queries that read: `M99999`, `M27`, `M4000`, and `M20`. It never starts,
pauses or stops a print in phase 1.

### 3. The sliced file

The facts of a resin print are in its file: the layers, the layer height, the exposure, the lift,
and an image of each layer. The deck gets the file in one of two ways:

- The user sends the file to the Phenom through the deck: the deck page has an upload for each
  ChiTu printer. The deck keeps a copy and sends the file to the board with `M28` and `M29`.
- The user puts the file on the deck with the same name as the file on the card of the Phenom. The
  watcher matches the name from `M20`.

From the file, the deck computes:

- The cured area of each layer, in mm². The chart of the area shows where peel forces spike, and the
  layers with islands (a new area with no area under it).
- The resin volume: the sum of the areas times the layer height, in ml.
- The time of each layer from the exposure and the lift, and the total time.
- A picture of the model, from the stack of the layer images, in the view of the other pictures.

### 4. The resin print in the projects

`cloud.PrintRequest` gets a `Process` field: `fdm` (the default, also for an empty field) or
`resin`. A resin print has these fields in place of the filament: `ResinML`, `Resin` (the name of
the resin, typed by the user), `LayerHeight`, `Exposure` (seconds) and `Area` (the area of each
layer, as `plan.json` has the times of the layers). The model picture and the photos stay the same.

The project pages:

- A tile of a resin print has a "Resin" badge.
- The numbers of a project show the filament and the resin apart: "210 g PLA · 85 ml resin".
- The print page of a resin print shows the area chart in place of the chart of the layer times,
  and the resin card in place of the filament card.

### 5. The deck on the Phenom

The deck program gets a backend for a ChiTu board, next to the backend for Klipper
(`printerBackend`). It maps the answers of the board to the state of the printer, so the status
panel, the history, the projects and the cloud work as for the TAZ. The screens of a resin printer:

- The status: the file, the progress, the layer from the Z height, the time left, and the area chart.
- The files on the card of the board, with the layers, the resin and the time of the files that the
  deck has. A tap starts a print, after the choice of its project.
- Pause, resume and stop.
- The settings of the board from `Phenom.gcode`: the lift and the speeds.
- The upload of a sliced file from a computer to the deck, and from the deck to the board.

The screens of the heaters and the filament do not show for a resin printer. The pod needs a mount
for the Phenom: a new part, from measurements of the Phenom.

**One image for both kinds.** The OS image stays the same. The file `printer.json` in the state
folder says which printer the deck drives: `{"kind": "chitu", "addr": "192.168.76.58", "mac":
"00:e0:4c:18:00:29", "name": "Phenom"}`. With no file the kind is `klipper`, so the decks of today
do not change. For a ChiTu deck, the deck program starts no time lapse and no TAZ display, and the
Klipper service of the image does not start. When the address stops answering, the deck finds the
board again by its MAC address.

**The backend.** `printer.ChiTu` implements the printer of the screens (`ui.Printer`): the state,
the history, the files and the projects. The controls of phase 4 (start, pause, resume, stop,
upload) return an error and send no command until their test. The controls of a FDM printer or of
Klipper (heaters, extrusion, macros, the flash of a board) are not there for this kind.

**The first setup.** The setup of a deck today has three steps: Wi-Fi, the wait for an address, and
the end. Klipper asks for the board and the config after the setup. A ChiTu deck gets these steps
after the wait for an address:

1. The kind of printer: a printer on USB (Klipper), or a resin printer on the network (ChiTu).
   Klipper goes on as today.
2. Find the printer: the deck sends `M99999` to port 3000 of each address of the network (a
   broadcast) and lists the boards that answer. "Type the address" is the other way. Nobody has
   tried the broadcast on the Phenom yet.
3. Check the board: the name, the firmware, the MAC and the address from `M99999`, and one `M4000`
   with the height of the plate and the progress of a print.
4. The name of the printer, "Phenom" by default.
5. The service: the deck connects to the service, as on its Cloud page, and registers the printer
   with the process `resin`. This step can wait for later.
6. The end. The deck writes `printer.json` and starts again with the ChiTu backend.

**The build order.**

1. Package `chitu`: the client, the parsers and a fake board (done).
2. `Machine.Process` and `printer.json`.
3. `printer.ChiTu` with the watcher of part 2, tested against the fake board.
4. The choice of the backend at the start, and a desktop deck with the fake board.
5. The screens of a resin printer.
6. The setup steps.
7. The registration of the printer with the process `resin`.
8. The image: the Klipper service starts only for the kind `klipper`.
9. Phase 4: each control after its test on a small print.

### 6. Many printers for one project

The service today knows decks: each deck enrolls (`decks/<id>.json`), a print has the name of its
printer as text (`PrintRequest.Printer`), and the live tile of a project is a file for each deck
(`x/<project>/live/<deck>.json`). A deck knows only the projects of its own history: a project is a
name on the records of its history, and `cloud-projects.json` maps the name to the ID at the
service. A second deck then makes a second project with the same name.

The change:

- **Printers at the service.** `printers/<id>.json` has the ID, the name, the process (`fdm` or
  `resin`), the model, and the deck that reports it. A deck registers each of its printers at its
  first connection (`POST /api/v1/printers`), and keeps the IDs. A deck can report more than one
  printer.
- **The projects are at the service.** Each deck reads the list of the projects (`GET
  /api/v1/projects`) at its start and each 10 minutes. The project choice of the deck and the list
  for the Cura plugin show this list after the projects of the history of the deck. A record of
  the history keeps the name of its project, and `cloud-projects.json` maps the name to the ID at
  the service. A name that is at the service maps to that project (the same letters, in any case
  when no name is the same), so the deck makes no second project. A rename at the service is a new
  name for the deck: the old records keep the old name and its ID.
- **Each print and each live tile has its printer.** A print has the ID of its printer. The live
  tile is a file for each printer (`x/<project>/live/<printer>.json`), so two printers of one deck
  show two live prints in one project.
- **The project pages show the printers.** A project shows the printers of its prints, with a
  filter of the prints by printer, the numbers of each printer (prints, time, filament or resin),
  and a live tile for each printer that prints for the project.
- **The printers for the owner.** The home page lists each printer, with its process and its
  model. The state of each printer (idle, printing, offline) and the print that it prints come
  later.

### 7. Printers that come later

A new kind is a new watcher with the same interface: for example a Klipper printer through
Moonraker, a Bambu printer through MQTT, or a Prusa printer through PrusaLink. The record, the
upload and the pages stay the same.

## Phases

1. Many printers for one project (part 6): the printers at the service, the projects from the
   service on each deck, and a live tile for each printer. It is useful with one deck too.
2. The ChiTu backend with queries that only read, and the status panel. A test print on the
   Phenom checks the fields of `M4000`.
3. The record of a resin print and its upload to a project, with the resin fields of the wire.
4. A test of each control on a small print: start, pause, resume, stop, and the upload. Then the
   screens of the controls and the files.
5. The project pages: the badge, the resin numbers, the resin card.
6. The sliced file: the area chart, the volume, the model picture.
7. The second deck on the Phenom: the pod mount, and the deck with the ChiTu backend.

## Tests

- The watcher against a fake ChiTu board on UDP, with answers from the Phenom: the start, the
  progress, a pause, the end, a stop, and a board that goes offline.
- The parse of a sliced file: the layer count, the areas and the volume of a known file.
- The upload of a resin print: the service keeps the resin fields, and a FDM print stays the same.
- The pages: the badge and the numbers of a project with FDM and resin prints.

## Open questions

1. The meaning of `T`, `B`, `E1`, `E2`, `F` and the third number of `D` in `M4000`. Check during a
   print, with a pause and a stop.
2. The file format of the Phenom: `.cbddlp`, `.ctb`, or another one. Check the file on its card
   with `M20`, and the format that the slicer of the user writes.
3. Does a query during the exposure of a layer change the print? The three queries of 2026-10-30
   came during a print. The result of that print shows if it went on as normal.
4. The upload with `M28`: the size of the packets, and the speed. A file of 79 MB at a slow speed can
   take long, and the user can prefer a USB drive.
5. A camera on the Phenom for a time lapse: where, and how to sync the frames with the lifts of the
   plate.
6. The commands of pause, resume, stop and upload of this firmware. The usual ChiTu commands
   (`M25`, `M24`, `M33`, `M28`, `M29`) are not in the text of the firmware. The firmware can
   compare the numbers of the commands, so the test of phase 4 decides.
7. The size and the place of the pod on the Phenom.
