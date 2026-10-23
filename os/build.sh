#!/usr/bin/env bash
# Build the Printdeck OS image in a Docker container.
#
#   os/build.sh                  build the release image
#   os/build.sh debug            build the debug image: the release image with SSH and tools
#   os/build.sh menuconfig       any other Buildroot target, for example linux-menuconfig or clean
#   os/build.sh debug clean      a Buildroot target of the debug build
#
# The images are os/out/printdeck.img.xz and os/out/printdeck-debug.img.xz, each with its update
# bundle. The Buildroot tree, the downloads and the build output are in ~/.cache/printdeck-os (set
# PRINTDECK_OS_CACHE to change it). A full build takes about one hour.
set -euo pipefail

BUILDROOT_VERSION=2025.11.3

OS="$(cd "$(dirname "$0")" && pwd)"
REPO="$(dirname "$OS")"
CACHE="${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}"

# The variant: release, or debug. Each has its own output folder.
VARIANT=release
OUTPUT=output
NAME=printdeck
if [ "${1:-}" = debug ]; then
  shift
  VARIANT=debug
  OUTPUT=output-debug
  NAME=printdeck-debug
fi

mkdir -p "$CACHE" "$OS/out"

# The key that signs the update bundle: PRINTDECK_SIGNING_KEY (the secret of the CI), or the key
# file on this computer. With no key, the bundle has no signature, and only a debug image
# installs it.
KEYFILE="${PRINTDECK_SIGNING_KEY_FILE:-$HOME/.config/printdeck/update-signing-key.pem}"
if [ -z "${PRINTDECK_SIGNING_KEY:-}" ] && [ -r "$KEYFILE" ]; then
  PRINTDECK_SIGNING_KEY="$(cat "$KEYFILE")"
fi
export PRINTDECK_SIGNING_KEY="${PRINTDECK_SIGNING_KEY:-}"

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
    -e PRINTDECK_VERSION="$(git -C "$REPO" describe --tags --match 'v*' --always --dirty 2>/dev/null || echo unknown)" \
    -e PRINTDECK_VARIANT="$VARIANT" \
    -e PRINTDECK_SIGNING_KEY \
    -v "$CACHE:/cache" \
    -v "$REPO:/src:ro" \
    -w "/cache/buildroot-$BUILDROOT_VERSION" \
    printdeck-os-builder \
    make -j"$(nproc)" O="/cache/$OUTPUT" BR2_EXTERNAL=/src/os "$@"
}

mkdir -p "$CACHE/home" "$CACHE/dl"
# Apply the defconfig again after a change to it. "make clean" keeps the .config, so a package that
# the defconfig drops stays in the image until the .config changes too.
# The debug build uses the release defconfig plus configs/debug.fragment.
if [ "$VARIANT" = debug ]; then
  DEFCONFIG="$CACHE/printdeck_debug_defconfig"
  if [ ! -f "$DEFCONFIG" ] || [ "$OS/configs/printdeck_defconfig" -nt "$DEFCONFIG" ] \
    || [ "$OS/configs/debug.fragment" -nt "$DEFCONFIG" ]; then
    cat "$OS/configs/printdeck_defconfig" "$OS/configs/debug.fragment" > "$DEFCONFIG"
  fi
  if [ ! -f "$CACHE/$OUTPUT/.config" ] || [ "$DEFCONFIG" -nt "$CACHE/$OUTPUT/.config" ]; then
    run BR2_DEFCONFIG=/cache/printdeck_debug_defconfig defconfig
  fi
elif [ ! -f "$CACHE/$OUTPUT/.config" ] || [ "$OS/configs/printdeck_defconfig" -nt "$CACHE/$OUTPUT/.config" ]; then
  run printdeck_defconfig
fi

if [ $# -gt 0 ]; then
  run "$@"
  exit
fi

# Buildroot copies a local source one time, and does not see later changes. Remove the copy of
# deck/ (it also keeps the files that deck/ no longer has), copy it again and build the Printdeck
# program, then make the image. The Go build cache keeps this fast.
run printdeck-deck-dirclean
run printdeck-deck
run
cp "$CACHE/$OUTPUT/images/printdeck.img.xz" "$OS/out/$NAME.img.xz"
cp "$CACHE/$OUTPUT/images/printdeck-update.tar.xz" "$OS/out/$NAME-update.tar.xz"
echo "Done: $OS/out/$NAME.img.xz"
