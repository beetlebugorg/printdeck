#!/bin/sh
# Makes printers.gz from the config folder of a Klipper checkout: each printer-*.cfg file, after a
# record separator (0x1e) and its name on a line. It adds the printers of Printdeck that Klipper has
# no config for. Run it when klipper.version changes, or after a change to printer/printer.cfg:
#
#	./update.sh ~/.cache/printdeck-os/klipper-reference/klipper-<commit>/config
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$DIR/../../../.." && pwd)"
{
  for f in "$1"/printer-*.cfg; do
    printf '\036%s\n' "$(basename "$f")"
    cat "$f"
    echo
  done
  # The printer of the Printdeck project (printer/README.md).
  printf '\036%s\n' printer-lulzbot-taz-workhorse-2019.cfg
  cat "$REPO/printer/printer.cfg"
} | gzip -9n > "$DIR/printers.gz"
