# Printdeck plugin for Cura

The plugin connects Cura to the Printdeck through the printer of the OctoPrint connection plugin. It
uses the same address and API key, so it needs no setup.

The plugin works by itself, and tells you in a message what it did:

- The print button says "Print to Printdeck".
- When the roll in the Printdeck changes, Cura changes to the material of the roll.
- After a slice, Cura warns when the model needs more filament than the roll has.
- During a print of the file that Cura has open, the Preview shows the layer that prints.
- When a print ends, Cura tells you.
- The Printdeck tab, next to Monitor, shows the screen of the deck, the print and the time left,
  the roll and its grams, and the keys Pause, Resume and Cancel, in the style of the deck.
- When Cura starts during a print, it offers to show the print in the Preview: it downloads the
  file from the Printdeck, and the Preview follows the layer that prints.
- Each file that Cura sends has the material of Cura in its header. Before the Printdeck prints a
  file for another material, it asks on the touch screen.

The OctoPrint connection plugin also shows the screen of the Printdeck in the Monitor tab, in
place of a camera.

## Install

1. Install the OctoPrint connection plugin from the Marketplace of Cura, and connect it to the
   Printdeck.
2. Download `Printdeck.curapackage` from the latest release, and drag it onto the Cura window.
   Restart Cura.

The plugin works with Cura 4.13 (Cura LulzBot Edition) and later.

## Develop

- `./package.sh` builds `Printdeck.curapackage`.
- `python3 -m unittest test_logic` tests the parts that do not need Cura.
- The Printdeck answers `GET /api/plugin/printdeck` (`deck/internal/octoapi/printdeck.go`).
