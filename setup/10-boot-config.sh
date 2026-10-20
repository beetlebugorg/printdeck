#!/usr/bin/env bash
# Enable I2C and the HiFiBerry overlay in config.txt.
set -euo pipefail
cd "$(dirname "$0")/.."
source ./printdeck.env

CONFIG=/boot/firmware/config.txt

if [ -z "$HIFIBERRY_OVERLAY" ]; then
  echo "Set HIFIBERRY_OVERLAY in printdeck.env first." >&2
  exit 1
fi

add_line() {
  grep -qxF "$1" "$CONFIG" || echo "$1" | sudo tee -a "$CONFIG" >/dev/null
}

sudo cp "$CONFIG" "$CONFIG.printdeck.bak"

add_line "dtparam=i2c_arm=on"
add_line "dtoverlay=$HIFIBERRY_OVERLAY"
if [ "$USB_MAX_CURRENT" = "1" ]; then
  add_line "usb_max_current_enable=1"
fi

echo "Done. The backup is $CONFIG.printdeck.bak. Reboot the Pi."
