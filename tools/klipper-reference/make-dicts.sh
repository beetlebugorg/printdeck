#!/bin/sh
# Makes a firmware data dictionary for each chip of Klipper's example configs (config/printer-*.cfg),
# with the default build options of the chip. The tests of the Go host load each example config
# with the dictionary of its chip (deck/internal/klipper/batch).
#
#   tools/klipper-reference/make-dicts.sh
set -eu

REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$REPO/deck/internal/klipper/testdata/dict
COMMIT=$(sed -n 's/^KLIPPER_VERSION = //p' "$REPO/deck/internal/klipper/klipper.version")
VERSION=$(sed -n 's/^KLIPPER_GIT_VERSION = //p' "$REPO/deck/internal/klipper/klipper.version")
CACHE=${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}/klipper-reference

mkdir -p "$CACHE" "$OUT"
if [ ! -d "$CACHE/klipper-$COMMIT" ]; then
  curl -fsSL "https://github.com/Klipper3d/klipper/archive/$COMMIT.tar.gz" | tar -xz -C "$CACHE"
fi
docker build -q -t printdeck-klipper-ref "$(dirname "$0")" > /dev/null

# Each line: the chip name, then the Kconfig options of the build. A "-serial" build is for the
# boards that use the USB pins or the default serial pins for other things.
CHIPS='atmega2560 CONFIG_MACH_AVR=y CONFIG_MACH_atmega2560=y
atmega1284p CONFIG_MACH_AVR=y CONFIG_MACH_atmega1284p=y
stm32f103 CONFIG_MACH_STM32=y CONFIG_MACH_STM32F103=y
stm32f401 CONFIG_MACH_STM32=y CONFIG_MACH_STM32F401=y
stm32f407 CONFIG_MACH_STM32=y CONFIG_MACH_STM32F407=y
stm32f446 CONFIG_MACH_STM32=y CONFIG_MACH_STM32F446=y
stm32f070 CONFIG_MACH_STM32=y CONFIG_MACH_STM32F070=y
stm32h743 CONFIG_MACH_STM32=y CONFIG_MACH_STM32H743=y
sam4e8e CONFIG_MACH_ATSAM=y CONFIG_MACH_SAM4E8E=y
hc32f460 CONFIG_MACH_HC32F460=y
stm32f103-serial CONFIG_MACH_STM32=y CONFIG_MACH_STM32F103=y CONFIG_STM32_SERIAL_USART1=y
hc32f460-serial CONFIG_MACH_HC32F460=y CONFIG_HC32F460_SERIAL_PA3_PA2=y'

# The build writes into a copy of the source, so the cache stays clean.
echo "$CHIPS" | docker run --rm -i --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/src:ro" -v "$OUT:/out" -e VERSION="$VERSION" \
  printdeck-klipper-ref sh -eu -c '
    cp -r /src /tmp/klipper
    cd /tmp/klipper
    echo "$VERSION" > klippy/.version
    while read chip opts; do
      rm -rf out .config
      for o in $opts; do echo "$o" >> .config; done
      make olddefconfig > /dev/null
      make > /tmp/build.log 2>&1 || { tail -20 /tmp/build.log; exit 1; }
      cp out/klipper.dict "/out/$chip.dict"
      echo "$chip"
    done'
echo "Done: $OUT"
