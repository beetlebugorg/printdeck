#!/bin/sh
# Builds the manual: the deck draws the pictures and writes index.html, then Chromium prints the
# page to printdeck-manual.pdf. Run it after a change to the UI.
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
(cd "$DIR/../../deck" && go run ./cmd/printdeck manual "$DIR")
PROFILE=$(mktemp -d)
trap 'rm -rf "$PROFILE"' EXIT
chromium --headless --disable-gpu --no-sandbox --user-data-dir="$PROFILE" --no-pdf-header-footer \
  --print-to-pdf="$DIR/printdeck-manual.pdf" "file://$DIR/index.html" > /dev/null 2>&1
echo "Done: $DIR/index.html, $DIR/printdeck-manual.pdf"
