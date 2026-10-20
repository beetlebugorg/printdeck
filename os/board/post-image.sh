#!/bin/bash
# Runs after Buildroot makes rootfs.erofs. Makes the disk image and the update bundle.
set -euo pipefail

BOARD="$BR2_EXTERNAL_PRINTDECK_PATH/board"
BOOT="$BINARIES_DIR/boot"

# The boot files of one slot: the kernel, the device trees of the Pi 5, the overlays that
# config.txt uses, and config.txt. cmdline.txt is different for each slot.
rm -rf "$BOOT"
mkdir -p "$BOOT/overlays"
cp "$BINARIES_DIR/Image" "$BINARIES_DIR"/bcm2712*.dtb "$BOOT/"
cp "$BOARD/config.txt" "$BOOT/config.txt"
# The overlays come from the kernel build, so that they match its device trees. The firmware
# applies bcm2712d0.dtbo itself on a board with the D0 chip. Without it, the kernel stops at boot
# with an SError in the pin setup.
OVERLAYS="$BUILD_DIR/linux-custom/arch/arm64/boot/dts/overlays"
for o in overlay_map.dtb bcm2712d0.dtbo vc4-kms-v3d-pi5.dtbo hifiberry-dacplus-std.dtbo; do
  cp "$OVERLAYS/$o" "$BOOT/overlays/"
done

# The 7" display is 800x480. Set the mode, because the EDID of the display can select a smaller one.
# "quiet" starts the start screen (see printdeck-init), and it and the other options keep the boot
# messages, the cursor and the Raspberry Pi logo off the display. A finger on the screen at boot
# shows the messages. fbcon=rotate:3 turns that text upright on the portrait display.
for slot in a b; do
  echo "root=PARTLABEL=printdeck-root-$slot rootfstype=erofs ro rootwait" \
    "init=/usr/bin/printdeck-init printdeck.slot=$slot" \
    "video=HDMI-A-1:800x480@60 video=HDMI-A-2:800x480@60 fbcon=rotate:3" \
    "console=ttyAMA10,115200 console=tty1 quiet loglevel=3 vt.global_cursor_default=0 logo.nologo" \
    > "$BINARIES_DIR/cmdline-$slot.txt"
done
cp "$BOARD/autoboot.txt" "$BOARD/printdeck.txt" "$BINARIES_DIR/"

FILES=$(cd "$BINARIES_DIR" && find boot -mindepth 1 -maxdepth 1 | sort | sed 's/.*/"&"/' | paste -sd, -)
sed "s|#BOOT_FILES#|$FILES|" "$BOARD/genimage.cfg" > "$BINARIES_DIR/genimage.cfg"

ROOTPATH_TMP="$(mktemp -d)"
trap 'rm -rf "$ROOTPATH_TMP"' EXIT
rm -rf "$BUILD_DIR/genimage.tmp"
genimage \
  --rootpath "$ROOTPATH_TMP" \
  --tmppath "$BUILD_DIR/genimage.tmp" \
  --inputpath "$BINARIES_DIR" \
  --outputpath "$BINARIES_DIR" \
  --config "$BINARIES_DIR/genimage.cfg"

# The update bundle for printdeck-update: both boot images, the root file system, and checksums.
(cd "$BINARIES_DIR" \
  && sha256sum boot-a.vfat boot-b.vfat rootfs.erofs > SHA256SUMS \
  && tar -cf printdeck-update.tar SHA256SUMS boot-a.vfat boot-b.vfat rootfs.erofs)
