#!/bin/sh
# Builds the manual two times: for a display that is mounted tall (this folder), as on the pod, and
# for a display that is mounted wide (wide/). The deck draws the pictures and writes index.html (the
# web manual) and booklet.html (the instruction booklet). Then Chromium prints each booklet two
# times:
#
#   printdeck-manual.pdf        the half-letter pages (5.5 x 8.5 in) in order. A print shop
#                               makes a saddle-stitched booklet from this file.
#   printdeck-manual-print.pdf  two pages on each side of a letter sheet, in the order of a
#                               folded booklet. Print it on both sides (flip on the short
#                               edge), then fold the stack and staple it in the fold.
#
# Run it after a change to the UI.
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
PROFILE=$(mktemp -d)
trap 'rm -rf "$PROFILE"' EXIT
pdf() {
  chromium --headless --disable-gpu --no-sandbox --user-data-dir="$PROFILE" --no-pdf-header-footer \
    --virtual-time-budget=5000 --print-to-pdf="$1/$2" "file://$1/booklet.html$3" > /dev/null 2>&1
}
build() {
  mkdir -p "$1"
  (cd "$DIR/../../deck" && go run ./cmd/printdeck manual "$1" $2)
  pdf "$1" printdeck-manual.pdf ""
  pdf "$1" printdeck-manual-print.pdf "?sheets"
  echo "Done: $1/index.html, $1/printdeck-manual.pdf, $1/printdeck-manual-print.pdf"
}
build "$DIR" ""
build "$DIR/wide" wide
