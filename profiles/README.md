# Printer profiles

The profiles of the printers for slices outside the Cura app: in GitHub Actions for the builds
of a project (docs/specs/github-builds.md), or on a computer with no Cura. Each folder is the
complete Cura stack of one printer, copied from Cura. `curaslice.py` of [printdeck-action](https://github.com/beetlebugorg/printdeck-action) works out each
setting from the stack as the Cura app does, and runs CuraEngine.

## taz-workhorse-se

The LulzBot TAZ Workhorse with the SE tool head (0.5 mm nozzle, 2.85 mm filament), Klipper, Cura
5.13. The source is the Cura of the owner: `~/.local/share/cura/5.13`.

| Profile | Layers | Notes |
|---|---|---|
| `pla-0.10-fine` | 0.10 mm | 45 mm/s, 3 walls, gyroid 20 % |
| `pla-0.20-standard` | 0.20 mm | 50 mm/s, 3 walls, gyroid 20 % |
| `pla-0.30-fast` | 0.30 mm | |

The materials are the Printdeck rolls in Cura (`roll-1` to `roll-4`), with the calibration of
each roll: the temperatures, `xy_offset`, `hole_xy_offset`, and the shrinkage.

The folder has:

- `definitions/`: `fdmprinter` and `fdmextruder` of Cura 5.13, and the Workhorse definitions.
- `definition_changes/`: the machine settings, with the Klipper start and end G-code.
- `quality/`: the generic quality `draft` of Cura.
- `quality_changes/`: the profiles.
- `materials/`: the rolls.
- `cura.json`: the stack of each profile, the materials, and the version of Cura.

The folder has no machine instance file of Cura: that file has the API key of the deck. It has
no user layer of Cura either: the settings of one part go in its `printdeck.yml` (`settings:`).

## peopoly-phenom

The Peopoly Phenom (resin, 276.48 x 155.52 mm, 3840 x 2160 pixels) with its ChiTu board. The
slice run of printdeck-action slices a resin plate with PrusaSlicer and writes a `.cbddlp` of
version 4, as ChiTuBox 1.4.0 writes it (`resinslice.py`). `resin.json` has the printer and the
profiles. The settings of the profile are the settings of ChiTuBox for the resin.

| Profile | Layers | Notes |
|---|---|---|
| `abs-like-0.05` | 0.05 mm | 12 s, bottom 95 s x 10, 8 transition layers, lift 12 mm at 30 mm/min |

- `printer.prusaslicer` and `prusaslicer` of a profile set PrusaSlicer settings of the supports,
  the pad and the hollowing. The `settings:` of a part in `printdeck.yml` add to them for the
  whole plate. Other PrusaSlicer settings stop the run.
- `printer.margin` is the room around each part on the plate, for the supports and the pad.
- Open a new profile in ChiTuBox before its first print.

## Change a profile

1. Change the profile in Cura, and save it.
2. Run `python3 slicer/export_cura_profile.py`. It copies the stack again, and stops when a file
   has a secret.
3. Commit the folder. Each repository that prints with the profile copies the new folder.

## The version of a profile

`curaslice.py` gives each profile a version: the first 12 hex digits of the SHA-256 of the files
of its folder, for example `taz-workhorse-se/pla-0.10-fine@44606b4e58b6`. Each G-code has it in
the line `;Printdeck profile:`, and each build has it in its manifest. A change of a profile in
a pull request gives a new version, so the build page and the print page show which profile
made each print.

## Slice

```
CURAENGINE=/path/to/CuraEngine python3 ../printdeck-action/curaslice.py \
    --profile profiles/taz-workhorse-se --quality pla-0.20-standard --material roll-4 \
    --set brim_line_count=14 --set support_enable=true --count 4 \
    --meta "title=Feet" --out feet.gcode foot.stl
```

- `--set` is the user layer: the settings of the part.
- `--count` puts copies in a grid at the middle of the bed. It fails when the copies do not fit.
- A 3MF file keeps the settings of its objects, for example a support blocker
  (`cura:anti_overhang_mesh`). The group of a 3MF file stays together.
- The header has the lines of the Printdeck plugin for Cura (`PRINTDECK_MATERIAL`,
  `PRINTDECK_COLOR`, `;Printdeck title:`), which the deck reads.

## How the settings are worked out

The Cura app works out each setting before it calls CuraEngine. `curaslice.py` follows the rules
of Cura 5:

1. The global stack, top first: the part settings, the quality changes, the quality, the
   definition changes, and the definition with its parents. The extruder stack has the same
   layers for the extruder, with the material, and then falls through to the global stack.
2. A setting that is not settable per extruder comes from the global stack.
3. A formula gets the values of its names from the stack that asks.
4. On the global stack, a setting with `resolve` uses that formula, unless the part or the
   quality changes set it. A setting with `limit_to_extruder` comes from that extruder.
5. `extruderValue` and `extruderValues` read the layers of the extruder stack only (Cura's
   `getRawProperty`). This is how the shrinkage of a roll reaches the global stack.
6. The `{key}` tokens of the start and end G-code come from the first extruder.
7. CuraEngine writes the header before the slice. The script writes the real header (time,
   filament, size) from the log of CuraEngine, as the Cura app does.

### The check against Cura

`LTAZWSE05MM_cover_screw.gcode` from the Cura app (PLA 0.10 Fine, roll 4, brim with 12 lines, tree
supports) and the slice of `curaslice.py` of the same STL:

| | Cura app | curaslice.py |
|---|---|---|
| Print time | 12399 s | 12399 s |
| Filament | 2.07922 m | 2.07922 m |
| Size (MIN/MAX X, Y, Z) | 92.353 to 187.161, 108.088 to 173.832, 0.25 to 73.95 | the same |
| Layers | 738 | 738 |

Of the 211,427 lines of moves, 245 places differ by 0.001 to 0.005 mm. The STL file was made
again after the slice of the Cura app, which can move some points a little.
