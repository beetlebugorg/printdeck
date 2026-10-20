# The printer

The printer of the Printdeck: a LulzBot TAZ Workhorse with an UltiMachine RAMBo board, on Klipper.

| File | What it is |
|---|---|
| `printer.cfg` | The Klipper configuration. Put it on the `PRINTDECK` partition to use it (see `os/README.md`). |
| `firmware.config` | The Klipper firmware configuration of the RAMBo (`make menuconfig` in Klipper). |

The Go port of Klipper's host is tested against this printer: `tools/klipper-reference` runs the
real Klipper host in batch mode with these two files.
