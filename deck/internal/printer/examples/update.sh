#!/bin/sh
# Makes printers.gz from the config folder of a Klipper checkout: each printer-*.cfg file, after a
# record separator (0x1e) and its name on a line. Run it when klipper.version changes:
#
#	./update.sh ~/.cache/printdeck-os/klipper-reference/klipper-<commit>/config
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
for f in "$1"/printer-*.cfg; do
  printf '\036%s\n' "$(basename "$f")"
  cat "$f"
  echo
done | gzip -9n > "$DIR/printers.gz"
