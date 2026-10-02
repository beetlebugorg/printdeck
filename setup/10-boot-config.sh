#!/usr/bin/env bash
# Enable SPI, I2C, and the HiFiBerry overlay in config.txt.
set -euo pipefail
cd "$(dirname "$0")/.."
source ./speakerdeck.env

CONFIG=/boot/firmware/config.txt

if [ -z "$HIFIBERRY_OVERLAY" ]; then
  echo "Set HIFIBERRY_OVERLAY in speakerdeck.env first." >&2
  exit 1
fi

add_line() {
  grep -qxF "$1" "$CONFIG" || echo "$1" | sudo tee -a "$CONFIG" >/dev/null
}

sudo cp "$CONFIG" "$CONFIG.speakerdeck.bak"

add_line "dtparam=spi=on"
add_line "dtparam=i2c_arm=on"
# Do not add dtoverlay=spi0-0cs. The e-paper service uses the hardware chip select of spidev0.0.
add_line "dtoverlay=$HIFIBERRY_OVERLAY"
if [ "$USB_MAX_CURRENT" = "1" ]; then
  add_line "usb_max_current_enable=1"
fi

echo "Done. The backup is $CONFIG.speakerdeck.bak. Reboot the Pi."
