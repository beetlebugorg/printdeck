#!/bin/sh
# Builds the booklet of the Cura plugin in this folder. The deck writes booklet.html, index.html and
# the cover pictures. The screenshots of Cura are in img/. Then Chromium prints the booklet two
# times:
#
#   printdeck-cura.pdf        the half-letter pages (5.5 x 8.5 in) in order.
#   printdeck-cura-print.pdf  two pages on each side of a letter sheet, in the order of a folded
#                             booklet. Print it on both sides (flip on the short edge), then fold
#                             the stack and staple it in the fold.
#
# Run it after a change to the plugin or to cura.go.
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
PROFILE=$(mktemp -d)
trap 'rm -rf "$PROFILE"' EXIT
pdf() {
  chromium --headless --disable-gpu --no-sandbox --user-data-dir="$PROFILE" --no-pdf-header-footer \
    --virtual-time-budget=5000 --print-to-pdf="$DIR/$1" "file://$DIR/booklet.html$2" > /dev/null 2>&1
}
(cd "$DIR/../../deck" && go run ./cmd/printdeck manual "$DIR" cura)
pdf printdeck-cura.pdf ""
pdf printdeck-cura-print.pdf "?sheets"
echo "Done: $DIR/booklet.html, $DIR/printdeck-cura.pdf, $DIR/printdeck-cura-print.pdf"
