#!/bin/sh
# Runs after Buildroot fills the target folder, before it makes the root file system.
set -eu

REPO="$BR2_EXTERNAL_PRINTDECK_PATH/.."
SHARE="$TARGET_DIR/usr/share/printdeck"

# The audio settings come from the same files as the Docker setup.

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
sed -i '/^PRINTDECK_VERSION=/d' "$TARGET_DIR/usr/lib/os-release"
echo "PRINTDECK_VERSION=${PRINTDECK_VERSION:-unknown}" >> "$TARGET_DIR/usr/lib/os-release"

# sudo reads /etc/sudoers.d only when /etc/sudoers includes it.
grep -q '^[@#]includedir /etc/sudoers.d' "$TARGET_DIR/etc/sudoers" \
  || echo '@includedir /etc/sudoers.d' >> "$TARGET_DIR/etc/sudoers"
chmod 0440 "$TARGET_DIR/etc/sudoers.d/wheel" "$TARGET_DIR/etc/sudoers.d/deck"

# Remove what Printdeck does not use. Buildroot installs these programs with their libraries.
rm -f "$TARGET_DIR"/usr/bin/flac "$TARGET_DIR"/usr/bin/metaflac "$TARGET_DIR"/usr/bin/mpg123 \
  "$TARGET_DIR"/usr/bin/out123 "$TARGET_DIR"/usr/bin/sqlite3 "$TARGET_DIR"/usr/bin/sudo_logsrvd \
  "$TARGET_DIR"/usr/bin/sudo_sendlog "$TARGET_DIR"/usr/bin/sudoreplay "$TARGET_DIR"/usr/bin/cvtsudoers \
  "$TARGET_DIR"/usr/bin/systemd-nspawn "$TARGET_DIR"/usr/bin/systemd-dissect "$TARGET_DIR"/usr/bin/bootctl \
  "$TARGET_DIR"/usr/bin/varlinkctl
