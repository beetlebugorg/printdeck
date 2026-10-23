# Printdeck for Apple Watch

A watch app for the Printdeck, with a small iPhone app for its settings. The watch app reads the
OctoPrint-compatible API of the deck over the network.

- The glance screen has the state of the printer, the file, the progress, layer N of M, the time
  left and the end time, the nozzle and bed temperatures (actual/target), and the roll in the
  printer with its color, material and grams left.
- Pause, Resume and Cancel ask for a confirmation before they send the command.
- The complication shows the progress and the time left on the watch face. It has a circular, a
  corner, an inline and a rectangular style.
- The iPhone app keeps the deck address and the API key, and sends them to the watch.

The app reads the deck every 5 seconds while it is open. The complication asks watchOS for a
refresh every 15 minutes during a print, and watchOS chooses when it runs. Between two reads, its
progress goes up in a straight line to the end time, and the time left counts down.

## Folders

- `Shared/`: the API client and the models (`Deck.swift`), the settings store in the app group
  (`Store.swift`), and the colors and formats (`Theme.swift`).
- `Watch/`: the watch app.
- `Complication/`: the WidgetKit extension with the complications.
- `Phone/`: the iPhone app with the settings.
- `project.yml`: the XcodeGen file of `Printdeck.xcodeproj`. After a change to `project.yml`, run
  `xcodegen` in this folder.

The deployment targets are iOS 17 and watchOS 10. The project was built and tested with Xcode 27.

## Install on your watch

1. Open `Printdeck.xcodeproj` in Xcode.
2. Set `DEVELOPMENT_TEAM` in `project.yml` to your team ID and run `xcodegen`. Or choose your
   team in Signing & Capabilities of the three targets in Xcode, but `xcodegen` resets it. The
   project uses automatic signing.
3. The bundle IDs start with `BUNDLE_PREFIX` (`org.beetlebug.printdeck`), and the app group is
   `group.` plus the prefix. If your team cannot use this prefix, change `BUNDLE_PREFIX` in
   `project.yml` and run `xcodegen`. The app group follows the prefix.
4. Connect the iPhone, choose the `Printdeck` scheme and the iPhone, and run. The watch app is in
   the iPhone app. Install it from the Watch app on the iPhone if it does not install itself, or
   choose the `PrintdeckWatch` scheme and the watch, and run.
5. On the deck, open http://printdeck.local/system and copy the API key.
6. In Printdeck on the iPhone, type the IP address of the deck and the key, and tap Save. The
   iPhone app reads the deck to test them, and sends them to the watch.
7. To add the complication, edit the watch face and choose Printdeck.

## Network

The deck serves plain HTTP on the local network. `NSAllowsLocalNetworking` in the Info.plist of
each target allows it. watchOS may not resolve `printdeck.local`, so use the IP address of the
deck. The watch reaches the deck on Wi-Fi, or through the iPhone when it is near.

The address can have a port, as `192.168.1.50:8080`. An address with no scheme gets `http://`.

## API

All requests have the header `X-Api-Key`.

- `GET /api/job`: the state text and the file name.
- `GET /api/printer`: the temperatures. The deck sends 409 when the printer is not connected,
  and the app shows no temperatures then.
- `GET /api/plugin/printdeck`: the roll in the printer, and the print with its layer, progress
  and seconds left.
- `POST /api/job`: `{"command":"pause","action":"pause"}`, `{"command":"pause","action":"resume"}`
  and `{"command":"cancel"}`.

The settings are in the user defaults of the app group, so the complication can read them. The
API key is in plain text there.
