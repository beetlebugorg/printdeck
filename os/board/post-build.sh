#!/bin/sh
# Runs after Buildroot fills the target folder, before it makes the root file system.
set -eu

REPO="$BR2_EXTERNAL_PRINTDECK_PATH/.."
SHARE="$TARGET_DIR/usr/share/printdeck"

# A kernel build after a config change keeps the modules of the old config in the target folder.
# Remove the modules that the kernel build did not make (modules.order), and make the module
# index again.
for dir in "$TARGET_DIR"/usr/lib/modules/*; do
  [ -f "$dir/modules.order" ] || continue
  find "$dir/kernel" -name '*.ko*' | while read -r ko; do
    rel=${ko#"$dir/"}
    grep -qxF "$rel" "$dir/modules.order" || { echo "Removing the old module $rel"; rm -f "$ko"; }
  done
  "$HOST_DIR/sbin/depmod" -a -b "$TARGET_DIR" "$(basename "$dir")"
done

# The mount point of the data partition. printdeck-init mounts it before systemd starts.
mkdir -p "$TARGET_DIR/data" "$TARGET_DIR/boot/config"

# Keep the journal on the data partition, so that a computer can read the logs of the last boots
# with "journalctl --directory". Limit its size, because the data partition is on an SSD.
mkdir -p "$TARGET_DIR/etc/systemd/journald.conf.d"
printf '[Journal]\nStorage=persistent\nSystemMaxUse=64M\n' \
  > "$TARGET_DIR/etc/systemd/journald.conf.d/printdeck.conf"

# usr/lib/systemd/system-preset/10-printdeck.preset turns on the services.
ln -sf /run/systemd/resolve/stub-resolv.conf "$TARGET_DIR/etc/resolv.conf"

# The version of this build, for printdeck-update and the deck.
# build.sh sets PRINTDECK_VERSION from git on the host.
sed -i '/^PRINTDECK_VERSION=/d;/^PRINTDECK_VARIANT=/d' "$TARGET_DIR/usr/lib/os-release"
echo "PRINTDECK_VERSION=${PRINTDECK_VERSION:-unknown}" >> "$TARGET_DIR/usr/lib/os-release"
# release, or debug (the release image with SSH and tools). build.sh sets it.
echo "PRINTDECK_VARIANT=${PRINTDECK_VARIANT:-release}" >> "$TARGET_DIR/usr/lib/os-release"

# sudo reads /etc/sudoers.d only when /etc/sudoers includes it.
grep -q '^[@#]includedir /etc/sudoers.d' "$TARGET_DIR/etc/sudoers" \
  || echo '@includedir /etc/sudoers.d' >> "$TARGET_DIR/etc/sudoers"
chmod 0440 "$TARGET_DIR/etc/sudoers.d/wheel" "$TARGET_DIR/etc/sudoers.d/deck"

# The Wi-Fi firmware of the other Raspberry Pi models. The Pi 5 has the BCM4345/6 chip, which loads
# brcm/brcmfmac43455-sdio (links to cypress/cyfmac43455-sdio).
find "$TARGET_DIR/usr/lib/firmware/brcm" "$TARGET_DIR/usr/lib/firmware/cypress" ! -type d ! -name '*43455*' -delete

# Libraries that no program of the image links or loads: the C++, OpenMP, vector math and atomics
# libraries of the toolchain, the old libnsl of glibc, and the parts of libnl that wpa_supplicant
# does not use.
rm -f "$TARGET_DIR"/usr/lib/libstdc++.so* "$TARGET_DIR"/usr/lib/libgomp.so* "$TARGET_DIR"/usr/lib/libmvec.so* \
  "$TARGET_DIR"/usr/lib/libatomic.so* "$TARGET_DIR"/usr/lib/libnsl.so* "$TARGET_DIR"/usr/lib/libnl-nf-3.so* \
  "$TARGET_DIR"/usr/lib/libnl-xfrm-3.so* "$TARGET_DIR"/usr/lib/libnl-idiag-3.so*

# Remove what Printdeck does not use. Buildroot installs these programs with their libraries.
rm -f "$TARGET_DIR"/usr/bin/flac "$TARGET_DIR"/usr/bin/metaflac "$TARGET_DIR"/usr/bin/mpg123 \
  "$TARGET_DIR"/usr/bin/out123 "$TARGET_DIR"/usr/bin/sqlite3 "$TARGET_DIR"/usr/bin/sudo_logsrvd \
  "$TARGET_DIR"/usr/bin/sudo_sendlog "$TARGET_DIR"/usr/bin/sudoreplay "$TARGET_DIR"/usr/bin/cvtsudoers \
  "$TARGET_DIR"/usr/bin/systemd-nspawn "$TARGET_DIR"/usr/bin/systemd-dissect "$TARGET_DIR"/usr/bin/bootctl \
  "$TARGET_DIR"/usr/bin/varlinkctl
