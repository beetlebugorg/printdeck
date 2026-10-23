# Printdeck for iPhone and Apple Watch

An iPhone app and a watch app for the Printdeck. They read the OctoPrint-compatible API of the
deck over the local network.

- The iPhone app has the look of the touch screen of the deck, in its tall layout: the display of
  the printer at the top, then the keys and the plates of the print. During a print it has Pause,
  Cancel and Menu, the layer, the roll (color, a meter of what is left, the name and the grams),
  and the file with the progress and a playhead. A paused print has Resume. With no print, Print a
  file opens the files of the deck, newest first. Menu opens the setup.
- The watch app is the display of the printer: the 128 x 64 status screen of the deck, with the
  nozzle, the bed and the fan, the progress bar, the end time and the time left. A tap opens the
  menu of the display: Pause or Resume, Cancel and Print info. The Digital Crown moves the
  selection, as the knob of the printer, and OK or a tap selects. The keys under the display are
  Cancel and Pause or Resume during a print. Each action asks first.
- The complication shows the progress and the time left on the watch face. It has a circular, a
  corner, an inline and a rectangular style.
- The deck sends push messages to the iPhone: print done, print failed, paused, first layer done,
  and a roll that runs short. The messages are encrypted from the deck to the iPhone. The watch
  shows the notifications of the iPhone.

The apps read the deck every 5 seconds while they are open. The complication asks watchOS for a
refresh every 15 minutes during a print, and watchOS chooses when it runs. Between two reads, its
progress goes up in a straight line to the end time, and the time left counts down.

The API has no fan speed, so the fan of the status screen shows "--". The deck shows its 3D model
under the print; the API has no model, so the iPhone shows the file and the playhead there.

## Folders

- `Shared/`: for the iPhone, the watch and the complication.
  - `Deck.swift`: the API client and the models. `DeckModel.swift`: the state of the apps.
  - `LCDScreen.swift`: the display of the printer, from `deck/internal/lcd`: the status screen,
    the menus and rows of text, on a canvas of 128 x 64 pixels. `LCDFonts.swift` has its fonts and
    icons, made from the Go source of the deck. `LCDModel.swift` makes the screens from the state.
  - `Panel.swift`: the parts of the panel of the deck: plates, the display, windows, keys, the
    confirmation. `Theme.swift`: the day and night palettes of the deck, and the formats.
  - `Store.swift`: the settings, the last status and the pairing, in the keychain.
  - `Fonts/`: IBM Plex Mono (SIL Open Font License), the same files as the deck.
- `Phone/`: the iPhone app: the main screen (`PhoneApp.swift`), the files (`FilesView.swift`),
  the setup (`SetupView.swift`), the search for the deck (`Finder.swift`), the pairing for push
  messages (`Push.swift`), and the settings for the watch (`WatchLink.swift`).
- `Watch/`: the watch app.
- `Complication/`: the WidgetKit extension with the complications.
- `Notifications/`: the notification service extension that decrypts the push messages.
- `Push/PushCrypto.swift`: the key agreement and the decryption of the push messages.
- `Tests/`: the tests of `PushCrypto`, with vectors from the Go code of the deck.
- `icon/render.swift`: draws the app icon, a blue dot-matrix display with "PD". Run
  `swift icon/render.swift <folder>` and copy `AppIcon-phone.png` and `AppIcon-watch.png` to
  `Phone/Assets.xcassets/AppIcon.appiconset/AppIcon.png` and
  `Watch/Assets.xcassets/AppIcon.appiconset/AppIcon.png`.
- `project.yml`: the XcodeGen file of `Printdeck.xcodeproj`. After a change to `project.yml`, run
  `xcodegen` in this folder.

The deployment targets are iOS 17 and watchOS 10. The project was built and tested with Xcode 27.

## Install

1. Set `DEVELOPMENT_TEAM` in `project.yml` to your team ID and run `xcodegen`. The project uses
   automatic signing.
2. The bundle IDs start with `BUNDLE_PREFIX` (`org.beetlebug.printdeck`): the iPhone app, then
   `.notifications`, `.watchkitapp` and `.watchkitapp.widget`. The keychain group is the team ID,
   the prefix and `.shared`. If your team cannot use this prefix, change `BUNDLE_PREFIX` in
   `project.yml` and run `xcodegen`.
3. Open `Printdeck.xcodeproj`, connect the iPhone, choose the `Printdeck` scheme and the iPhone,
   and run. The watch app is in the iPhone app.
4. For the watch, choose the `PrintdeckWatch` scheme and the watch, and run. Xcode adds the watch
   to the team and its profiles. The watch needs Developer Mode, which shows on the watch after
   Xcode pairs with it. The watch and the Mac must be on the same network. If Xcode does not find
   the watch, turn off Bluetooth on the iPhone for a minute.
5. Open Printdeck on the iPhone. It finds the deck on the network. Tap Connect, then tap Allow on
   the touch screen of the deck. The app gets its own key from the deck and sends the address and
   the key to the watch.
6. To add the complication, edit the watch face and choose Printdeck.

When the deck is not in the list, open Menu > By hand. Type the address and tap Ask the deck for
a key, or type the key from http://printdeck.local/system and tap Save.

## Push messages

The deck sends the messages through the relay (`relay/README.md`). The relay and Apple see the
encrypted message only.

1. In Menu > Notifications on the iPhone, tap Pair for notifications. iOS asks to allow
   notifications, and gives the app a device token.
2. The app posts the token to the relay (`POST /register`) and gets a send token.
3. The app makes an X25519 key pair and pairs with the deck (`POST
   /api/plugin/printdeck/push/pair`) with its public key, the device token, the send token and
   the relay URL. The reply has the public key of the deck. Both make the message key with
   HKDF-SHA256 (the salt is the device token, the info is `printdeck push v1`).
4. The message key goes into the keychain group, for the notification service extension. The deck
   sends "This phone gets the messages of your prints."
5. The extension decrypts the key `e` of each push (AES-256-GCM) and shows its title and body.
   When it cannot, iOS shows the alert of the push, "Printdeck: A message from your printer".

The app pairs again when iOS gives it a new device token. Unpair removes the phone from the deck;
Settings > Phones on the deck has the same list. A build that Xcode installs uses the sandbox of
APNs, and a TestFlight or App Store build uses production. The app reads the environment from its
provisioning profile. `PUSH_RELAY` in `project.yml` is the URL of the relay.

To test: install from Xcode, pair, and start a print. "First layer done" comes after the first
layer. The tests of `PushCrypto` run with the `Printdeck` scheme (Product > Test).

## Network

The deck serves plain HTTP on the local network. `NSAllowsLocalNetworking` in the Info.plist of
each target allows it. The deck announces itself with Bonjour as `_octoprint._tcp`. The iPhone
app finds it and saves its IPv4 address, because watchOS may not resolve `printdeck.local`. The
watch reaches the deck on Wi-Fi, or through the iPhone when it is near. iOS asks once for the
permission to use the local network.

The address can have a port, as `192.168.1.50:8080`. An address with no scheme gets `http://`.

## API

The App Keys flow of OctoPrint gives the app its key. It needs no key:

- `POST /plugin/appkeys/request` with `{"app": "Printdeck on iPhone"}`: the touch screen of the
  deck asks to allow the app, and the reply has a token.
- `GET /plugin/appkeys/request/<token>`: 202 while the deck waits, 200 with `api_key` after
  Allow, and 404 after Deny or after 2 minutes.

All other requests have the header `X-Api-Key`.

- `GET /api/job`: the state text, the file name and the time of the print.
- `GET /api/printer`: the temperatures. The deck sends 409 when the printer is not connected,
  and the app shows no temperatures then.
- `GET /api/plugin/printdeck`: the roll in the printer, and the print with its layer, progress
  and seconds left.
- `POST /api/job`: `{"command":"pause","action":"pause"}`, `{"command":"pause","action":"resume"}`
  and `{"command":"cancel"}`.
- `GET /api/files?recursive=true`: the files. `POST /api/files/local/<path>` with
  `{"command":"select","print":true}` prints one. The deck asks on its touch screen when the
  material of the file is not the material in the printer.
- `POST /api/plugin/printdeck/push/pair`, `DELETE /api/plugin/printdeck/push/<id>`: the pairing
  for push messages.

## Screenshots in the simulator

In a debug build, the environment variables `PRINTDECK_ADDRESS` and `PRINTDECK_KEY` set the deck,
and `PRINTDECK_SCREEN` opens a screen: `menu`, `info` or `cancel` on the watch, `files` or
`setup` on the iPhone. With `xcrun simctl launch`, give them the prefix `SIMCTL_CHILD_`. The
simulated deck of `printdeck app -sim` reads its API key from `OCTOPRINT_API_KEY`.

## License

The code in this folder is under the MIT License (`LICENSE`). The rest of the repository is under
the AGPL. `Shared/LCDScreen.swift` is a port of `deck/internal/lcd` of the deck, by the same
authors, under the MIT License here.

- `Shared/Fonts/`: IBM Plex Mono, SIL Open Font License 1.1 (`Shared/Fonts/OFL.txt`).
- `Shared/LCDFonts.swift`: the X11 misc-fixed font 5x8 (public domain), and the glyphs of the
  VGA 8 x 16 font of the fntcol16 package (public domain), as the displays of Klipper have them.
