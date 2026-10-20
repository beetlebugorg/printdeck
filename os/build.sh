#!/usr/bin/env bash
# Build the Printdeck OS image in a Docker container.
#
#   os/build.sh                  build the image
#   os/build.sh menuconfig       any other Buildroot target, for example linux-menuconfig or clean
#
# The image is os/out/printdeck.img. The Buildroot tree, the downloads and the build output are in
# ~/.cache/printdeck-os (set PRINTDECK_OS_CACHE to change it). A full build takes about one hour.
set -euo pipefail

BUILDROOT_VERSION=2025.11.3

OS="$(cd "$(dirname "$0")" && pwd)"
REPO="$(dirname "$OS")"
CACHE="${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}"

mkdir -p "$CACHE" "$OS/out"

# Two builds in the same cache delete each other's files. Allow only one.
exec 9> "$CACHE/build.lock"
flock -n 9 || { echo "Another build uses $CACHE. Wait for it to finish." >&2; exit 1; }
docker build -q -t printdeck-os-builder "$OS/docker" >/dev/null

if [ ! -d "$CACHE/buildroot-$BUILDROOT_VERSION" ]; then
  git -c advice.detachedHead=false clone -q --depth 1 --branch "$BUILDROOT_VERSION" \
    https://gitlab.com/buildroot.org/buildroot.git "$CACHE/buildroot-$BUILDROOT_VERSION"
fi

run() {
  docker run --rm --init \
    --user "$(id -u):$(id -g)" \
    -e HOME=/cache/home \
    -e BR2_DL_DIR=/cache/dl \
    -e PRINTDECK_VERSION="$(git -C "$REPO" describe --always --dirty 2>/dev/null || echo unknown)" \
    -v "$CACHE:/cache" \
    -v "$REPO:/src:ro" \
    -w "/cache/buildroot-$BUILDROOT_VERSION" \
    printdeck-os-builder \
    make -j"$(nproc)" O=/cache/output BR2_EXTERNAL=/src/os "$@"
}

mkdir -p "$CACHE/home" "$CACHE/dl"
# Apply the defconfig again after a change to it. "make clean" keeps the .config, so a package that
# the defconfig drops stays in the image until the .config changes too.
if [ ! -f "$CACHE/output/.config" ] || [ "$OS/configs/printdeck_defconfig" -nt "$CACHE/output/.config" ]; then
  run printdeck_defconfig
fi

if [ $# -gt 0 ]; then
  run "$@"
  exit
fi

run
cp "$CACHE/output/images/printdeck.img" "$CACHE/output/images/printdeck-update.tar" "$OS/out/"
echo "Done: $OS/out/printdeck.img"
