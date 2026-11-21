# Printdeck for iPhone and Apple Watch

An iPhone app and a watch app for the Printdeck. They use the API of the decks on the local
network. Each printer of a deck with many printers is a printer of its own in the apps.

- **Home screen**: a card for each printer, with the camera or the model of the print, the state,
  the progress, the time left and the end. The printers of a deck with many printers go under the
  name of the deck, and each card has the short label of its printer on its color. A card marks a
  printer that needs you: a pause, a failed print, or a print that is done. A tap opens the screen
  of the printer. Add connects another deck.
- **Card pictures**: a still of the camera of the printer, else the model of the print, else the
  color of the printer with its label, its model, and its nozzle and bed. The still is
  `/webcam/snapshot` under the address of the printer (the `camera` path of the list of the deck,
  with `snapshot` for `stream`), with `camera=main`, so that the deck answers 503 for a camera with
  no picture in place of a picture of its screen. A card reads a still every 15 seconds while it is
  on the screen, at the width of the card, and keeps the last still while the next one loads. The
  first still of a Bambu Lab printer starts its RTSP stream and can be slow or fail: the card
  waits up to 25 seconds, then tries again after 2, 4, 8 up to 60 seconds. The model of the print
  (`/api/plugin/printdeck/model.png`) is there only during a print. The screen of a printer shows
  the same picture under its camera stream until the first frame.
- **Printer screen**: the camera (tap for the full screen view), or the model of the print. The
  print with its progress, its layer, the time left, the end and the time so far, with Pause,
  Resume and Cancel. The nozzle and the bed with their targets: a tap sets a target, with the
  presets of the deck. The fan. The loaded filament: the trays of each AMS with the part that is
  left, the external spool, or the roll. Move and home, between prints. The Z offset during a
  print. The queue: a swipe moves a file up or down, or removes it. Print a file, with a search
  and the folders of the files, and a swipe to delete a file. Time lapses. The cameras, the model,
  the nozzles and the build plate. Settings: the notifications, the watch, Connect again, the
  address and key by hand, and Remove.
- **Dialogs**: each dialog that starts, stops, moves or heats names the printer, for example
  "Pause the print on TAZ Workhorse?" with the key "Pause TAZ". The app opens a printer only after
  a tap. A deck with one printer has no label and no deck name.
- **The controls of the deck**: a deck lists the controls of each printer (`controls` in
  `/api/plugin/printdeck`), and the app shows only those: the aux nozzle and the chamber, the fan,
  the speed (by level on a Bambu Lab printer) and the flow of a print, the Z offset, the load and
  the unload of a spool of an AMS with a tap, the drying of an AMS with the presets of the deck,
  and on a Klipper printer Load, Unload, Extrude and Retract. A Klipper printer heats the nozzle
  first, for the material that the user picks, and the control waits until the nozzle is 3 °C
  under its target, as the swap of the deck. The jog and the filament wait for a pause during a
  print, and the home, the spools of an AMS and the drying for the end of a print. The screen
  shows the messages of the printer, the action of the deck that runs, the start step of a
  print, the axes that are homed, and the nozzle that prints now (`tool_now`) on a printer with
  two.
- **The effect of a control**: after the deck accepts a control, the app reads the status about
  once a second until it shows the change, and shows the change: "Bed target 60 °C", "Homed X Y Z",
  the new position, "Z offset +0.025 mm", "Speed Sport", "Loaded A2", "Drying AMS A at 55 °C",
  "Paused". When the status does not show the change in time, the text says so. The status has no
  length of an extrusion and no filament in the extruder of a Klipper printer, so those say that
  the deck ran them.
- **A deck before the control routes**: the app shows the nozzle, the bed, move and home between
  prints, and the fan and the Z offset as G-code, which needs a Klipper printer and Signed prints
  only off (`signed_only` in `/api/plugin/printdeck`).
- **Resin decks**: a deck with `process` resin in `/api/plugin/printdeck` has no Print a file, no
  Resume, no temperatures, no moves and no filament. Only a press on the touch screen of the deck
  starts or resumes a resin print, because both turn on the UV light. The app has Pause and Cancel.
- **Watch app**: the print of the deck that prints, or of the first deck, as the status panel of
  the touch screen of the deck: the nozzle, the bed and the fan (the plate, the time and the time
  left on a resin deck), the state and the clock time of the end, the time left and the time so
  far, the layer, the loaded spools as little rolls, and the progress bar, in the amber of the
  deck on its dark window. A printer with two nozzles and a chamber has the aux nozzle and the
  chamber too, in two rows, once the deck sends `tool1` and `chamber`, and a mark on the nozzle
  that prints now. A printer of a deck with
  many printers has its label and its name at the top. Turn the Digital Crown for the camera, the
  model, the layer times and the filament. Tap for Pause, Resume or Cancel.
- **Complications** and **widgets**: the progress and the time left, on the watch face, the Home
  Screen and the Lock Screen. Edit a widget to choose its deck. The default is the deck that
  prints: with two prints, the print that ends first.
- **Live Activity**: the print on the Lock Screen and in the Dynamic Island, for the deck that
  prints. With two prints it shows the print that ends first. It moves to the other print when its
  own print ends, or when the other print ends 5 minutes sooner.
- **Push messages**: print done, print failed, paused, first layer done, and a short roll.

The apps read all decks every 5 seconds while they are open.

## Install

Install the apps from Xcode. The deployment targets are iOS 17 and watchOS 11.

1. Set `DEVELOPMENT_TEAM` in `project.yml` to your team ID. Run `xcodegen` in this folder.
2. For a bundle prefix other than `org.beetlebug.printdeck`, change `BUNDLE_PREFIX` in
   `project.yml` and run `xcodegen`.
3. Open `Printdeck.xcodeproj`. Choose the `Printdeck` scheme and your iPhone, and run.
4. For the watch, choose the `PrintdeckWatch` scheme and the watch, and run. Turn on Developer
   Mode on the watch. The watch and the Mac must be on the same network.
5. Open Printdeck on the iPhone. Tap Connect, then tap Allow on the touch screen of the deck. Add
   connects the next deck. On a deck with many printers, the app adds each printer of the deck.
6. To add the complication, edit the watch face and choose Printdeck.

If Xcode fails to find the watch, turn off Bluetooth on the iPhone for a minute.

If the deck is missing from the list, open By hand under Add. Type the address, and tap Ask the
deck for a key. Or type the key from http://printdeck.local/system and tap Save.

## Push messages

1. On the iPhone, open the printer, then Settings, and tap Pair for notifications. Each
   printer pairs on its own. With several decks the subtitle of a message is the name of the deck.
2. Allow the notifications.
3. The deck sends "This phone gets the messages of your prints."

Unpair removes the phone. Settings > Phones on the deck has the same list. Each message is
encrypted for the paired phone. The relay and Apple see only the encrypted message
(`cloud/relay/README.md`).

How the pairing works:

1. The app posts its device token to the relay (`POST /register`), and gets a send token.
2. The app makes an X25519 key pair, and pairs with the deck (`POST
   /api/plugin/printdeck/push/pair`). Both make the message key with HKDF-SHA256 (the salt is the
   device token, the info is `printdeck push v1`).
3. The notification service extension decrypts the key `e` of each push (AES-256-GCM).

A build from Xcode uses the APNs sandbox. A TestFlight or App Store build uses production.
`PUSH_RELAY` in `project.yml` is the URL of the relay.

## Away from home

Sign in with passkey on the Projects tab signs in to printdeck.beetlebug.org with the passkey of
the owner or of a member, the same passkey as the web site. iOS offers the passkeys of the iPhone
and a security key, such as a YubiKey, over USB-C, Lightning or NFC. A key with no passkey for
Printdeck sends no user handle, and the app shows "This key has no passkey for Printdeck." The token is in the keychain group,
and a 401 asks for the sign-in again. An owner session works for 14 days.

When a deck does not answer, its card shows the live print of the deck from the projects of the
session: the model, the progress and the time left. The app finds the print by `printer_id` of
the deck (from `/api/plugin/printdeck`), or by the name of the printer when the deck has sent no
ID. For the owner, the live record of each printer (`live` in `GET /api/v1/printers`) fills the
card of a print with no project. A print of a project comes first, because it has the picture of
the model. A member sees only the prints of the projects.

The sign-in needs the entitlement `webcredentials:printdeck.beetlebug.org`, so it works only in
a build that is signed with the team of `project.yml`. The service serves
`/.well-known/apple-app-site-association` for the app.

The routes of the service:

- `POST /api/v1/auth/login/begin`, then the assertion of the passkey, then
  `POST /api/v1/auth/login/finish`: the token, the kind, the name and the expiry time.
- `GET /api/v1/projects`, and `GET /api/v1/projects/<id>` for the live prints (`live`).
- `GET /api/v1/printers`, for the owner: the live record of each printer (`live`).

## Projects

The app has two tabs: Prints, with the cards of the decks, and Projects. The Projects tab has the
sign-in, and after it a card for each project with what it does now: the prints that run, the
prints ready for pickup, the plates that wait and the files in the queues. A project shows its
overview, as the web page has it:

- **Now**: the live prints, and for the owner the plates that wait for an approval, with Approve
  and Reject (with a reason), and the approved plates with Send to a printer of the same process.
  3D preview opens the build page of the web site with the session of the app.
- **Ready for pickup**: the prints with Ready, and with no Delivered and no Test, with Delivered for
  the owner.
- **In the queues**: the files of the project in the queues of the printers.
- **Newest prints**: a tap opens the print, with its marks (owner), its notes, its title and source
  (owner), and its photos.
- **What happened**: the feed of the prints, the photos, the plans, the plates and the notes.

Take photos keeps the camera open and sends each photo after it is taken: to the print on the page
of a print, else to the project. A member changes only the notes of a print.

The routes: `GET /api/v1/projects/<id>`, `PATCH /api/v1/projects/<id>/prints/<print>`,
`POST /api/v1/projects/<id>/builds/<build>/plates/<n>/approve`, `reject` and `send`,
`GET /api/v1/printers`, and `POST /api/v1/projects/<id>/photos` with the upload to its grant and
`POST /api/v1/projects/<id>/photos/<photo>/done`.

## The queue

The printer screen shows the queue of the printer: the files in their order, each with its material
and its time, and the total time. A swipe to the right moves a file up or down, and a swipe to the
left removes it from the queue. The app
has no Start: only a press on the touch screen of the deck starts the next file. Away from home, a
card shows the count of the files in the queue of its deck (owner), and a project shows its files in
the queues.

At the end of a print with files in the queue, the deck sends "Next in the queue". A tap on a push
message opens the screen of its printer.

The routes of the deck: `GET /api/plugin/printdeck/queue`, `POST
/api/plugin/printdeck/queue/<id>/move` with `{"by": -1}`, and `DELETE
/api/plugin/printdeck/queue/<id>`.

## Many printers on one deck

The App Keys request goes to the root of the deck. On a deck with many printers, the touch screen
asks to allow the app to use the deck and its printers, and the reply has `"scope": "deck"`: the
key works for each printer of the deck. The app then reads `GET /slicer/printer.json` with the key.
Each entry of `printers` has the ID, the name, the short label, the color, the kind (`fdm` or
`resin`), the file format, the model and `url`. The app adds each printer with the address of the
deck and its `url`: `192.168.1.50` for the first printer, and `192.168.1.50/printers/x2d` for
another. All requests of a printer go under its address, so `/api/job` of the X2D is
`/printers/x2d/api/job`.

A deck with one printer has no `printers` in the file, and the app adds one printer, as before. A
deck from before sends no `scope`, and its key works for one printer.

While the app is open, it reads the list of each deck with a deck key every 5 minutes, and adds a
new printer of the deck. A printer that the deck no longer lists, or that answers 404, shows "Not
on this deck now" on its card, and stays until Remove. Connect again on the Menu of a printer asks
the root of its deck for a new key.

When the owner removes the deck key on the deck, each printer of the deck answers 403. The home
screen then shows one Connect again for the deck, and the cards of its printers wait for a new
key. The new deck key goes to each printer of the deck. Only a 403 with `"error": "Invalid API key"`
counts as a refused key. The deck also sends 403 with a good key, to a G-code command while
Signed prints only is on, and the app shows that reason.

## Network

The deck serves plain HTTP on the local network (`NSAllowsLocalNetworking`). The deck announces
itself with Bonjour as `_octoprint._tcp`. The app saves its IPv4 address, because watchOS may
fail to resolve `printdeck.local`. The address can have a port, as `192.168.1.50:8080`.

## API

The App Keys flow of OctoPrint gives the app its key, with no key:

- `POST /plugin/appkeys/request` with `{"app": "Printdeck on iPhone"}`: the deck asks to allow
  the app, and the reply has a token.
- `GET /plugin/appkeys/request/<token>`: 202 while the deck waits, 200 with `api_key` after
  Allow, and 404 after Deny or after 2 minutes.

All other requests have the header `X-Api-Key`:

- `GET /api/job`, `GET /api/printer`: the job and the temperatures.
- `GET /api/plugin/printdeck`: the process (`fdm` or `resin`), the roll, the print, the main camera
  (`camera`), all cameras (`cameras`) and the status screen (`lcd`). A deck with no `process` is
  an FDM deck. `filaments` are the loaded spools: the slot (`A1` for tray 1 of the first AMS,
  `Ext` for the external spool, `""` for a roll), the tray, the nozzle (`main` or `aux`), the
  material, the color, the name and the part that is left (`remain`, or null). `nozzles` and
  `plate` are the nozzles and the build plate of a Bambu Lab printer. `job.release` is the seconds
  until the part can come off the bed, and `signed_only` is true when the deck refuses G-code
  commands from apps.
- `GET /slicer/printer.json`, with a deck key: the printers of the deck.
- `GET /webcam/snapshot?w=..`, `GET /webcam/stream?w=..&fps=..`: the main camera. Add
  `camera=<id>` for another camera, or `camera=picture` for the camera of the notifications.
- `POST /api/job`: `{"command":"pause","action":"pause"}`, `{"command":"pause","action":"resume"}`
  and `{"command":"cancel"}`.
- `POST /api/printer/tool` with `{"command":"target","targets":{"tool0":210}}`, and
  `POST /api/printer/bed` with `{"command":"target","target":60}`: the targets of the heaters.
- `POST /api/printer/printhead` with `{"command":"jog","x":10,"y":0,"z":0}` or
  `{"command":"home","axes":["x","y"]}`: the moves.
- `POST /api/printer/command` with `{"commands":["M106 S127"]}`: the fan and the Z offset
  (`SET_GCODE_OFFSET Z_ADJUST=0.025 MOVE=1`), as the deck writes them.
- `DELETE /api/files/local/<path>`: deletes a file.
- The control routes, for the controls that `controls` lists: `POST /api/printer/tool` with
  `"targets":{"tool1":220}` (the aux nozzle) or `{"command":"flowrate","factor":104}`,
  `POST /api/printer/printhead` with `{"command":"feedrate","factor":120}` or `"level":"Sport"`,
  `POST /api/printer/chamber` with `{"command":"target","target":35}`,
  `POST /api/plugin/printdeck/fan` with `{"percent":50}`, `POST /api/plugin/printdeck/zoffset`
  with `{"adjust":0.025}`, `POST /api/plugin/printdeck/filament` with `{"action":"load","tray":2}`,
  `{"action":"load"}`, `{"action":"unload"}` or `{"action":"extrude","mm":10}`, and `POST` and
  `DELETE /api/plugin/printdeck/ams/<unit>/dry` with `{"temp":55,"hours":8,"filament":"PLA"}`.
  The state of the controls is at the top of `/api/plugin/printdeck`: `homed`, `position`, `speed`,
  `flow`, `speed_levels`, `speed_level`, `z_offset`, `tool1`, `chamber`, `fan`, `tool_now`,
  `action`, `preparing`, `ams`, `messages`, and `active` on the spool that feeds.
- `GET /api/files?recursive=true`, and `POST /api/files/local/<path>` with
  `{"command":"select","print":true}`: the files, and a print.
- `GET /api/plugin/printdeck/model.png?w=..&h=..&mono=1`: the model of the print in 1-bit dots.
- `POST /api/plugin/printdeck/push/pair`, `DELETE /api/plugin/printdeck/push/<id>`: the pairing
  for push messages.
- `GET /api/timelapse`: the time lapse videos, with the print, the frames and the length of each
  under `printdeck`. `GET /downloads/timelapse/<name>` sends a video (QuickTime, Motion JPEG),
  with Range requests, and `GET /downloads/timelapse/<name>/still` its last frame.

## Folders

- `Shared/`: the API client (`Deck.swift`), the list of decks (`Fleet.swift`), the state of a deck
  (`DeckModel.swift`), the printer display
  (`LCDScreen.swift`, a port of `deck/internal/lcd`), the panel parts and the keychain store.
- `Phone/`, `Watch/`: the apps.
- `Complication/`, `Widget/`: the WidgetKit extensions.
- `Notifications/`, `Push/PushCrypto.swift`: the decryption of the push messages.
- `Tests/`: the tests of `PushCrypto`, of the printers of a deck and of the replies of the deck
  (Product > Test with the `Printdeck` scheme).
- `icon/render.swift`: draws the app icon. Run `swift icon/render.swift <folder>`, and copy
  `AppIcon-phone.png` and `AppIcon-watch.png` into the `AppIcon.appiconset` of `Phone/` and
  `Watch/`.
- `project.yml`: the XcodeGen file. After a change, run `xcodegen`.

## Screenshots in the simulator

In a debug build, `PRINTDECK_ADDRESS` and `PRINTDECK_KEY` set one deck, and `PRINTDECK_DECKS` sets
several, as `TAZ|127.0.0.1:7361|key1,Phenom|127.0.0.1:7362|key2`, and a printer of a deck with
many printers as `name|address|key|label|#rrggbb`. `PRINTDECK_DECK_KEY` connects to
a deck with a deck key, as `127.0.0.1:7361|key`, and adds each printer of the deck. `PRINTDECK_SCREEN` opens a
screen: `menu`, `cancel`, `panel` or `bottom` on the watch, `deck`, `files`, `setup`, `camera`,
`timelapse`, `nozzle`, `move` or `loading` on the iPhone. The screens of a printer open the printer
of `PRINTDECK_PRINTER` (its place in the list, from 0), or the first printer. `PRINTDECK_SCROLL`
scrolls the printer screen to `temps`, `filament`, `control`, `queue` or `info`.
With `xcrun simctl launch`, add the prefix `SIMCTL_CHILD_`. The deck of `printdeck app -sim`
reads its API key from `OCTOPRINT_API_KEY`.

## License

The code in this folder is under the MIT License (`LICENSE`). The rest of the repository is under
the AGPL. `Shared/LCDScreen.swift` is a port of `deck/internal/lcd` of the deck, by the same
authors, under the MIT License here.

- `Shared/Fonts/`: IBM Plex Mono, SIL Open Font License 1.1 (`Shared/Fonts/OFL.txt`).
- `Shared/LCDFonts.swift`: the X11 misc-fixed font 5x8 (public domain), and the glyphs of the
  VGA 8 x 16 font of the fntcol16 package (public domain), as the displays of Klipper have them.
