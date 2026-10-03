#!/usr/bin/env bash
# Build the speakerdeck binary and start it on boot. It takes over the 7" display on tty1.
set -euo pipefail
cd "$(dirname "$0")/.."
REPO="$(pwd)"

sudo apt-get install -y golang-go alsa-utils
(cd deck && go build -o speakerdeck ./cmd/speakerdeck)
sudo install -m 755 deck/speakerdeck /usr/local/bin/speakerdeck

# Settings for the binary. Existing values are kept.
ENVF=/etc/default/speakerdeck
sudo touch "$ENVF"
sudo chmod 640 "$ENVF"
add_default() {
  grep -q "^$1=" "$ENVF" || echo "$1=$2" | sudo tee -a "$ENVF" >/dev/null
}
source ./speakerdeck.env
add_default NOWPLAYING_PORT "$NOWPLAYING_PORT"
add_default SPOTIFY_DEVICE_NAME "\"$SPOTIFY_DEVICE_NAME\""
add_default SPOTIFY_CLIENT_ID ""
add_default OCTOPRINT_API_KEY ""
add_default MAX_VOLUME 100

sudo tee /etc/systemd/system/speakerdeck.service >/dev/null <<UNIT
[Unit]
Description=Speakerdeck
After=raspotify.service octoprint.service sound.target
Conflicts=getty@tty1.service
After=getty@tty1.service

[Service]
EnvironmentFile=-$ENVF
StateDirectory=speakerdeck
SupplementaryGroups=input video audio tty
TTYPath=/dev/tty1
StandardInput=tty-force
TTYReset=yes
TTYVHangup=yes
ExecStart=/usr/local/bin/speakerdeck
Restart=always
RestartSec=2
# The service needs root to open the framebuffer on tty1 and to read the touch controller.
User=root

[Install]
WantedBy=multi-user.target
UNIT

sudo systemctl daemon-reload
sudo systemctl enable speakerdeck
echo "Done. Now:"
echo "  1. Put SPOTIFY_CLIENT_ID and OCTOPRINT_API_KEY in $ENVF."
echo "  2. Run: sudo env \$(grep -v '^#' $ENVF | xargs) speakerdeck auth"
echo "  3. Run: sudo systemctl restart speakerdeck"
