#!/bin/sh
# Draws the social preview of the repository: card.html, with the print screen of the wide manual,
# to printdeck-social.png (1280 x 640). Upload it on GitHub: Settings > General > Social preview.
set -eu
DIR="$(cd "$(dirname "$0")" && pwd)"
chromium --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 --window-size=1280,640 \
  --allow-file-access-from-files --screenshot="$DIR/printdeck-social.png" "file://$DIR/card.html" > /dev/null 2>&1
echo "Done: $DIR/printdeck-social.png"
