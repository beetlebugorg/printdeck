#!/bin/sh
# Removes from the release image the programs, libraries and files that no part of the Printdeck
# uses, for a small image and a small attack surface. post-build.sh runs it for the release image
# only. The debug image keeps everything.
#
# The programs are a list: a program that is not in it goes. A library stays when a remaining
# program or library links it, or when a program loads it at runtime (the list below). A systemd
# unit goes when its program is gone.
set -eu

T=$TARGET_DIR
READELF="$HOST_DIR/bin/aarch64-linux-readelf"
removed=0
gone() {
  echo "release-prune: ${1#"$T"}"
  rm -f "$1"
  removed=$((removed + 1))
}

# The programs in /usr/bin and /usr/sbin that the release image needs.
#   printdeck*                 the Printdeck program and the names of its system programs
#   systemctl, journalctl, udevadm, init, reboot, poweroff, halt, shutdown, systemd-tmpfiles,
#   systemd-machine-id-setup   systemd and its units
#   dbus-daemon                the system bus: systemctl for the user deck, and networkd
#   mount, umount              systemd mounts /tmp and /boot/config with them
#   sfdisk, partx, mke2fs, e2fsck  the data partition at the start (printdeck-init)
#   fstrim                     the weekly trim of the SSD (fstrim.timer)
#   modprobe, kmod             the kernel loads modules with modprobe
#   dmesg                      the boot log of the rescue (printdeck-init)
#   wpa_supplicant             the Wi-Fi
KEEP="printdeck printdeck-init printdeck-update printdeck-commit printdeck-credentials printdeck-settings
  printdeck-wifi systemctl journalctl udevadm init reboot poweroff halt shutdown systemd-tmpfiles
  systemd-machine-id-setup dbus-daemon mount umount sfdisk partx mke2fs e2fsck fstrim modprobe kmod
  dmesg wpa_supplicant"
for f in "$T"/usr/bin/* "$T"/usr/sbin/*; do
  [ -L "$f" ] || [ -f "$f" ] || continue
  case " $(echo $KEEP) " in
    *" ${f##*/} "*) ;;
    *) gone "$f" ;;
  esac
done
# dbus-daemon-launch-helper stays: the device table of the dbus package sets its mode (4750,
# root:dbus), and only the user dbus can run it.
for f in "$T"/usr/libexec/*; do
  [ "${f##*/}" = dbus-daemon-launch-helper ] && continue
  [ -f "$f" ] && gone "$f"
  [ -d "$f" ] && rm -rf "$f"
done

# The programs of systemd that the release image does not use.
for p in systemd-battery-check systemd-boot-check-no-failures systemd-fsck systemd-growfs \
  systemd-keyutil systemd-makefs systemd-networkd-wait-online systemd-network-generator \
  systemd-reply-password systemd-sbsign systemd-sleep systemd-socket-proxyd systemd-ssh-proxy \
  systemd-sulogin-shell systemd-sysroot-fstab-check systemd-time-wait-sync systemd-update-done \
  systemd-update-helper systemd-xdg-autostart-condition; do
  [ -e "$T/usr/lib/systemd/$p" ] && gone "$T/usr/lib/systemd/$p"
done
# Only the fstab generator: /tmp and /boot/config. The others could, for example, mount partitions
# by their type (gpt-auto) or start an SSH socket (ssh).
for f in "$T"/usr/lib/systemd/system-generators/*; do
  [ "${f##*/}" = systemd-fstab-generator ] || gone "$f"
done
rm -rf "$T"/usr/lib/systemd/user "$T"/usr/lib/systemd/user-generators \
  "$T"/usr/lib/systemd/user-environment-generators "$T"/usr/lib/systemd/user-preset \
  "$T"/usr/lib/systemd/system-sleep "$T"/usr/lib/systemd/ssh_config.d "$T"/etc/X11 \
  "$T"/etc/profile.d "$T"/usr/share/bash-completion "$T"/usr/share/zsh "$T"/usr/share/factory

# A unit whose program is gone goes too, with its links in the .wants and .requires folders.
# Otherwise a target can pull it in, and it fails at each start. Only ExecStart and ExecStartPre
# count: a unit runs with no program for ExecReload (dbus.service has dbus-send there). A program
# with no path is in /usr/bin.
for u in "$T"/usr/lib/systemd/system/*.service "$T"/etc/systemd/system/*.service; do
  [ -f "$u" ] || continue
  for p in $(sed -n 's/^ExecStart\(Pre\)\{0,1\}=[-@:+!|]*\([^ ]*\).*/\2/p' "$u"); do
    case "$p" in /*) ;; *) p=/usr/bin/$p ;; esac
    if [ ! -e "$T$p" ]; then
      name=${u##*/}
      find "$T"/usr/lib/systemd/system "$T"/etc/systemd/system -lname "*$name" -delete 2> /dev/null || true
      gone "$u"
      break
    fi
  done
done
for s in systemd-ask-password-console.path systemd-ask-password-wall.path systemd-creds.socket; do
  find "$T"/usr/lib/systemd/system "$T"/etc/systemd/system -name "$s" -delete 2> /dev/null || true
done

# The libraries. Start with the ELF files that remain, and the libraries that programs load at
# runtime: libkmod (systemd, .note.dlopen), the NSS modules of /etc/nsswitch.conf, libgcc_s
# (glibc, for thread cancellation), and the legacy module of OpenSSL (wpa_supplicant, old WPA).
need=" libkmod.so.2 libgcc_s.so.1 libnss_files.so.2 libnss_dns.so.2 libnss_systemd.so.2
  libnss_resolve.so.2 libnss_myhostname.so.2 ld-linux-aarch64.so.1 "
todo=$(find "$T"/usr/bin "$T"/usr/sbin "$T"/usr/lib/systemd "$T"/usr/lib/udev "$T"/usr/lib/ossl-modules \
  -type f 2> /dev/null)
for so in $need; do
  [ -e "$T/usr/lib/$so" ] && todo="$todo $(readlink -f "$T/usr/lib/$so")"
done
seen=""
while [ -n "$todo" ]; do
  next=""
  for f in $todo; do
    case " $seen " in *" $f "*) continue ;; esac
    seen="$seen $f"
    for so in $("$READELF" -d "$f" 2> /dev/null | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p'); do
      case "$need" in *" $so "*) continue ;; esac
      need="$need$so "
      for lib in "$T/usr/lib/$so" "$T/usr/lib/systemd/$so"; do
        [ -e "$lib" ] && next="$next $(readlink -f "$lib")"
      done
    done
  done
  todo=$next
done
for so in $need; do
  [ -e "$T/usr/lib/$so" ] && seen="$seen $(readlink -f "$T/usr/lib/$so")"
done
for f in "$T"/usr/lib/*.so*; do
  [ -e "$f" ] || [ -L "$f" ] || continue
  name=${f##*/}
  real=$(readlink -f "$f")
  case "$need" in *" $name "*) continue ;; esac
  case " $seen " in *" $real "*) continue ;; esac
  gone "$f"
done
# Links to a removed library, and the links for the linker (libfoo.so), which no program uses.
find "$T"/usr/lib -maxdepth 1 -xtype l -name '*.so*' -delete
find "$T"/usr/lib -maxdepth 1 -type l -name '*.so' -delete

echo "release-prune: removed $removed files"
