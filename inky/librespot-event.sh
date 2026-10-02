#!/bin/sh
# librespot runs this script for each player event.
# It sends the track data to the "now playing" service.
[ "$PLAYER_EVENT" = "track_changed" ] || exit 0

[ -r /etc/default/speakerdeck ] && . /etc/default/speakerdeck

# COVERS and ARTISTS contain one entry per line. The first cover is the largest.
COVER=$(printf '%s\n' "$COVERS" | head -n 1)
ARTIST_LIST=$(printf '%s' "$ARTISTS" | tr '\n' ',' | sed 's/,/, /g')

curl -s -m 2 -X POST "http://127.0.0.1:${NOWPLAYING_PORT:-8765}/track" \
  --data-urlencode "name=$NAME" \
  --data-urlencode "artists=$ARTIST_LIST" \
  --data-urlencode "album=$ALBUM" \
  --data-urlencode "cover=$COVER" >/dev/null || true
