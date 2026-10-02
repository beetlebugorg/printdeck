#!/usr/bin/env bash
# Install raspotify (librespot) and connect it to the "now playing" service.
set -euo pipefail
cd "$(dirname "$0")/.."
source ./speakdeck.env

CONF=/etc/raspotify/conf

if ! command -v librespot >/dev/null; then
  curl -sL https://dtcooper.github.io/raspotify/install.sh | sh
fi

# The raspotify service cannot read the home directory.
sudo install -m 755 inky/librespot-event.sh /usr/local/bin/speakdeck-librespot-event

set_conf() {
  sudo sed -i "/^#\?\s*$1=/d" "$CONF"
  echo "$1=\"$2\"" | sudo tee -a "$CONF" >/dev/null
}

set_conf LIBRESPOT_NAME "$SPOTIFY_DEVICE_NAME"
set_conf LIBRESPOT_BITRATE "320"
set_conf LIBRESPOT_DEVICE "$ALSA_DEVICE"
set_conf LIBRESPOT_ONEVENT "/usr/local/bin/speakdeck-librespot-event"

echo "NOWPLAYING_PORT=$NOWPLAYING_PORT" | sudo tee /etc/default/speakdeck >/dev/null

sudo systemctl enable raspotify
sudo systemctl restart raspotify
echo "Done. Select \"$SPOTIFY_DEVICE_NAME\" in the Spotify app."
