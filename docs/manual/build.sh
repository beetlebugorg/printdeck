#!/bin/sh
# Builds the manual: the deck draws the pictures and writes index.html (the web manual) and
# booklet.html (the instruction booklet). Then Chromium prints the booklet two times:
#
#   printdeck-manual.pdf        the pages at their trim size, 5.5 x 4.25 in, to read
#   printdeck-manual-print.pdf  the pages with a bleed and crop marks, for a printer
#
# Run it after a change to the UI.
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
(cd "$DIR/../../deck" && go run ./cmd/printdeck manual "$DIR")
PROFILE=$(mktemp -d)
trap 'rm -rf "$PROFILE"' EXIT
pdf() {
  chromium --headless --disable-gpu --no-sandbox --user-data-dir="$PROFILE" --no-pdf-header-footer \
    --virtual-time-budget=5000 --print-to-pdf="$DIR/$1" "file://$DIR/booklet.html$2" > /dev/null 2>&1
}
pdf printdeck-manual.pdf ""
pdf printdeck-manual-print.pdf "?print"
echo "Done: $DIR/index.html, $DIR/printdeck-manual.pdf, $DIR/printdeck-manual-print.pdf"
