# Printdeck for Apple Watch

A watch app for the Printdeck, with a small iPhone app for its settings. The watch app reads the
OctoPrint-compatible API of the deck over the network.

- The glance screen has the state of the printer, the file, the progress, layer N of M, the time
  left and the end time, the nozzle and bed temperatures (actual/target), and the roll in the
  printer with its color, material and grams left.
- Pause, Resume and Cancel ask for a confirmation before they send the command.
- The complication shows the progress and the time left on the watch face. It has a circular, a
  corner, an inline and a rectangular style.
- The iPhone app connects to the deck and sends the address and the key to the watch. It finds
  the deck on the network, and the deck gives it a key after a tap on Allow on its touch screen.
  The screens use the look of the deck: plates, display windows, keys and IBM Plex Mono.

The app reads the deck every 5 seconds while it is open. The complication asks watchOS for a
refresh every 15 minutes during a print, and watchOS chooses when it runs. Between two reads, its
progress goes up in a straight line to the end time, and the time left counts down.

## Folders

- `Shared/`: the API client and the models (`Deck.swift`), the settings store in the keychain
  (`Store.swift`), the colors, fonts and formats (`Theme.swift`), and IBM Plex Mono
  (`Fonts/`, SIL Open Font License, the same files as the deck).
- `Watch/`: the watch app.
- `Complication/`: the WidgetKit extension with the complications.
- `Phone/`: the iPhone app: the connection screen (`PhoneApp.swift`), the search for the deck
  (`Finder.swift`), and the parts of the panel of the deck (`Panel.swift`).
- `icon/render.swift`: draws the app icons from the parts of the deck logo. Run
  `swift icon/render.swift <folder>` and copy `AppIcon-phone.png` and `AppIcon-watch.png` to
  `Phone/Assets.xcassets/AppIcon.appiconset/AppIcon.png` and
  `Watch/Assets.xcassets/AppIcon.appiconset/AppIcon.png`.
- `project.yml`: the XcodeGen file of `Printdeck.xcodeproj`. After a change to `project.yml`, run
  `xcodegen` in this folder.

The deployment targets are iOS 17 and watchOS 10. The project was built and tested with Xcode 27.

## Install on your watch

1. Open `Printdeck.xcodeproj` in Xcode.
2. Set `DEVELOPMENT_TEAM` in `project.yml` to your team ID and run `xcodegen`. Or choose your
   team in Signing & Capabilities of the three targets in Xcode, but `xcodegen` resets it. The
   project uses automatic signing.
3. The bundle IDs start with `BUNDLE_PREFIX` (`org.beetlebug.printdeck`): the iPhone app, then
   `.watchkitapp` and `.watchkitapp.widget`. The keychain group is the team ID, the prefix and
   `.shared`. If your team cannot use this prefix, change `BUNDLE_PREFIX` in `project.yml` and run
   `xcodegen`.
4. Connect the iPhone, choose the `Printdeck` scheme and the iPhone, and run. The watch app is in
   the iPhone app. Install it from the Watch app on the iPhone if it does not install itself, or
   choose the `PrintdeckWatch` scheme and the watch, and run.
5. Open Printdeck on the iPhone. It finds the deck on the network. Tap Connect, then tap Allow
   on the touch screen of the deck. The app gets its own key from the deck, reads the deck with
   it, and sends the address and the key to the watch.
6. To add the complication, edit the watch face and choose Printdeck.

When the deck is not in the list, tap Enter the address. Type the address and tap Ask the deck
for a key, or type the key from http://printdeck.local/system and tap Save.

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

- `GET /api/job`: the state text and the file name.
- `GET /api/printer`: the temperatures. The deck sends 409 when the printer is not connected,
  and the app shows no temperatures then.
- `GET /api/plugin/printdeck`: the roll in the printer, and the print with its layer, progress
  and seconds left.
- `POST /api/job`: `{"command":"pause","action":"pause"}`, `{"command":"pause","action":"resume"}`
  and `{"command":"cancel"}`.

The address, the key and the last status are in a keychain group that the watch app and the
complication share. A keychain group needs no setup in the developer account.
