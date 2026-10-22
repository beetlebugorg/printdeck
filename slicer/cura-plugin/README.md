# Printdeck plugin for Cura

The plugin connects Cura to the Printdeck through the printer of the OctoPrint connection plugin. It
uses the same address and API key, so it needs no setup.

- When the material of Cura is not the material of the roll in the Printdeck, Cura asks to use
  the material of the roll.
- After a slice, Cura warns when the model needs more filament than the roll has.
- Each file that Cura sends has the material of Cura in its header. Before the Printdeck prints a
  file for another material, it asks on the touch screen.
- Extensions > Printdeck > Follow the print in Preview moves the layer of the Preview to the
  layer that prints, when Cura has the same file open.

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
