# The C decoders of go-librespot (libvorbis, libflac, mpg123), for the reference data of the
# pure Go decoders in deck/third_party/go-librespot.
FROM golang:1.25-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
      pkg-config libvorbis-dev libogg-dev libflac-dev libmpg123-dev libasound2-dev ffmpeg \
    && rm -rf /var/lib/apt/lists/*
