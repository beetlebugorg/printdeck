#!/usr/bin/env bash
# Install Klipper in a virtual environment and start it as a service. OctoPrint connects to it
# through the serial port that Klipper makes: ~/printer_data/comms/klippy.serial.
#
# The Klipper version must be the one the printer's board was flashed with. Set KLIPPER_REF to its tag
# or commit (the "Git version" line at the top of klippy.log) to keep the board's firmware.
set -euo pipefail
KLIPPER_DIR="$HOME/klipper"
KLIPPER_ENV="$HOME/klippy-env"
DATA="$HOME/printer_data"
KLIPPER_REF="${KLIPPER_REF:-7bc4d0946}" # the version the TAZ Workhorse RAMBo was flashed with

sudo apt-get update
sudo apt-get install -y git python3-venv python3-dev build-essential libffi-dev libncurses-dev libusb-1.0-0

if [ ! -d "$KLIPPER_DIR/.git" ]; then
  git clone https://github.com/Klipper3d/klipper.git "$KLIPPER_DIR"
fi
git -C "$KLIPPER_DIR" fetch --tags origin
git -C "$KLIPPER_DIR" checkout "$KLIPPER_REF"

python3 -m venv "$KLIPPER_ENV"
"$KLIPPER_ENV/bin/pip" install --upgrade pip wheel
"$KLIPPER_ENV/bin/pip" install -r "$KLIPPER_DIR/scripts/klippy-requirements.txt"

mkdir -p "$DATA/config" "$DATA/logs" "$DATA/comms" "$DATA/gcodes"
if [ ! -f "$DATA/config/printer.cfg" ]; then
  echo "# Put the printer configuration here (copy it from the old Klipper host)." > "$DATA/config/printer.cfg"
fi

# Klipper needs the serial port of the printer's board.
sudo usermod -a -G tty,dialout "$USER"

sudo tee /etc/systemd/system/klipper.service >/dev/null <<EOF
[Unit]
Description=Klipper 3D printer firmware (host)
After=network-online.target
Wants=network-online.target
Before=octoprint.service

[Service]
User=$USER
ExecStart=$KLIPPER_ENV/bin/python $KLIPPER_DIR/klippy/klippy.py $DATA/config/printer.cfg -l $DATA/logs/klippy.log -I $DATA/comms/klippy.serial -a $DATA/comms/klippy.sock
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now klipper
echo "Done. In OctoPrint, connect to the serial port $DATA/comms/klippy.serial at 250000 baud."
