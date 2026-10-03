#!/usr/bin/env bash
# Install raspotify (librespot) and connect it to the "now playing" service.
set -euo pipefail
cd "$(dirname "$0")/.."
source ./speakerdeck.env

CONF=/etc/raspotify/conf

if ! command -v librespot >/dev/null; then
  curl -sL https://dtcooper.github.io/raspotify/install.sh | sh
fi

# The raspotify service cannot read the home directory.
sudo install -m 755 inky/librespot-event.sh /usr/local/bin/speakerdeck-librespot-event

set_conf() {
  sudo sed -i "/^#\?\s*$1=/d" "$CONF"
  echo "$1=\"$2\"" | sudo tee -a "$CONF" >/dev/null
}

set_conf LIBRESPOT_NAME "$SPOTIFY_DEVICE_NAME"
set_conf LIBRESPOT_BITRATE "320"
set_conf LIBRESPOT_DEVICE "$ALSA_DEVICE"
# Spotify volume is the Digital control of the Amp2, so the app and the touch screen use the same volume.
set_conf LIBRESPOT_MIXER "alsa"
set_conf LIBRESPOT_ALSA_MIXER_DEVICE "hw:CARD=sndrpihifiberry"
set_conf LIBRESPOT_ALSA_MIXER_CONTROL "Digital"
set_conf LIBRESPOT_ONEVENT "/usr/local/bin/speakerdeck-librespot-event"

echo "NOWPLAYING_PORT=$NOWPLAYING_PORT" | sudo tee /etc/default/speakerdeck >/dev/null

sudo systemctl enable raspotify
sudo systemctl restart raspotify
echo "Done. Select \"$SPOTIFY_DEVICE_NAME\" in the Spotify app."
