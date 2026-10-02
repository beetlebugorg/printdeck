#!/usr/bin/env bash
# Install the "now playing" service for the Inky wHAT.
set -euo pipefail
cd "$(dirname "$0")/.."
REPO="$(pwd)"

sudo apt-get install -y python3-venv fonts-dejavu-core

python3 -m venv "$REPO/inky/venv"
"$REPO/inky/venv/bin/pip" install --upgrade pip
"$REPO/inky/venv/bin/pip" install inky pillow requests

sudo usermod -a -G spi,i2c,gpio "$USER"

sudo tee /etc/systemd/system/speakdeck-nowplaying.service >/dev/null <<EOF
[Unit]
Description=Speakdeck now playing display
Before=raspotify.service

[Service]
User=$USER
SupplementaryGroups=spi i2c gpio
EnvironmentFile=-/etc/default/speakdeck
ExecStart=$REPO/inky/venv/bin/python $REPO/inky/nowplaying.py
Restart=always

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now speakdeck-nowplaying
echo "Done. Run 'inky/venv/bin/python inky/nowplaying.py --test' to draw a test image."
