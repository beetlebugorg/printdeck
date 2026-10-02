#!/usr/bin/env bash
# Install OctoPrint in a virtual environment and start it as a service.
set -euo pipefail

OCTO_DIR="$HOME/OctoPrint"

sudo apt-get update
sudo apt-get install -y python3-venv python3-dev build-essential

python3 -m venv "$OCTO_DIR/venv"
"$OCTO_DIR/venv/bin/pip" install --upgrade pip wheel
"$OCTO_DIR/venv/bin/pip" install octoprint

# OctoPrint needs access to the serial port of the printer.
sudo usermod -a -G tty,dialout "$USER"

sudo tee /etc/systemd/system/octoprint.service >/dev/null <<EOF
[Unit]
Description=OctoPrint
After=network-online.target
Wants=network-online.target

[Service]
User=$USER
ExecStart=$OCTO_DIR/venv/bin/octoprint serve
Restart=always
Nice=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now octoprint
echo "Done. Open http://$(hostname).local:5000 to complete the OctoPrint setup."
