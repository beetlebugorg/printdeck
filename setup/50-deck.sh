#!/usr/bin/env bash
# Start the Printdeck stack (full screen UI and Spotify Connect player) in Docker, on every boot.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null; then
  echo "Install Docker first: https://docs.docker.com/engine/install/debian/" >&2
  exit 1
fi

if [ ! -f .env ]; then
  cp .env.example .env
  echo "Made .env from .env.example. Put the Spotify client ID and the OctoPrint API key in it, then run this again."
  exit 0
fi

# The desktop draws on the same display. The UI needs the display for itself.
if systemctl is-enabled lightdm >/dev/null 2>&1; then
  sudo systemctl set-default multi-user.target
  sudo systemctl disable --now lightdm
fi
# A separate Spotify Connect program would use the same name and the same sound card.
sudo systemctl disable --now raspotify 2>/dev/null || true

# The ALSA loopback carries Spotify to the equalizer (CamillaDSP).
echo snd-aloop | sudo tee /etc/modules-load.d/snd-aloop.conf >/dev/null
sudo modprobe snd-aloop

sudo systemctl enable --now docker
docker compose up -d --build --remove-orphans
echo "Done. To link Spotify: docker compose run --rm deck auth"
