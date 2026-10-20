#!/bin/sh
# Makes the reference data for the Go port of Klipper's host, with the real Klipper host (klippy).
#
# For the Klipper commit of os/package/klipper and the printer in printer/, it builds the firmware's
# data dictionary, and runs klippy in batch mode on each G-code file in the test data folder. klippy
# then writes the exact bytes that it would send to the printer's board (.serial), and its own text
# form of them (.txt). The Go port must give the same commands, in the same order for each object
# (see internal/klipper/reftest).
#
#   tools/klipper-reference/make-reference.sh
set -eu

REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$REPO/deck/internal/klipper/testdata
COMMIT=$(sed -n 's/^KLIPPER_VERSION = //p' "$REPO/os/package/klipper/klipper.mk")
VERSION=$(sed -n 's/^KLIPPER_GIT_VERSION = //p' "$REPO/os/package/klipper/klipper.mk")
CACHE=${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}/klipper-reference

mkdir -p "$CACHE"
if [ ! -d "$CACHE/klipper-$COMMIT" ]; then
  curl -fsSL "https://github.com/Klipper3d/klipper/archive/$COMMIT.tar.gz" | tar -xz -C "$CACHE"
fi
docker build -q -t printdeck-klipper-ref "$(dirname "$0")" > /dev/null

# What Klipper's config reader reads from the printer config and from the edge cases of the
# configfile tests.
CFGTEST=$REPO/deck/internal/klipper/configfile/testdata
cp "$REPO/printer/printer.cfg" "$CFGTEST/printer.cfg"
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/klipper:ro" -v "$CFGTEST:/cfg" -v "$REPO/tools/klipper-reference:/tools:ro" \
  printdeck-klipper-ref python3 /tools/dumpcfg.py \
  /cfg/printer.cfg /cfg/printer.cfg.json /cfg/edge.cfg /cfg/edge.cfg.json

# How Klipper's G-code dispatcher reads the lines of the gcode tests.
GTEST=$REPO/deck/internal/klipper/gcode/testdata
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/klipper:ro" -v "$GTEST:/g" -v "$REPO/tools/klipper-reference:/tools:ro" \
  printdeck-klipper-ref python3 /tools/dumpgcode.py /g/lines.txt /g/lines.json

# The temperature conversions of the thermistors and the default sensor tables.
TTEST=$REPO/deck/internal/klipper/extras/thermistor/testdata
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/klipper:ro" -v "$TTEST:/out" -v "$REPO/tools/klipper-reference:/tools:ro" \
  printdeck-klipper-ref python3 /tools/dumptemps.py /out/temps.json

# The output of Jinja2, with the settings of Klipper, for the cases of the template engine.
JTEST=$REPO/deck/internal/klipper/jinja/testdata
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$JTEST:/out" -v "$REPO/tools/klipper-reference:/tools:ro" \
  printdeck-klipper-ref python3 /tools/dumpjinja.py /out/cases.txt /out/cases.json

docker run --rm --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/klipper" -v "$REPO/printer:/printer:ro" -v "$OUT:/out" \
  -e VERSION="$VERSION" -w /klipper printdeck-klipper-ref sh -eu -c '
    echo "$VERSION" > klippy/.version
    cp /printer/firmware.config .config
    make olddefconfig > /dev/null
    make > /dev/null 2>&1
    cp out/klipper.dict /out/klipper.dict
    for g in /out/*.gcode; do
      name=${g%.gcode}
      python3 klippy/klippy.py /printer/printer.cfg -i "$g" -o "$name.serial" -d out/klipper.dict \
        > "$name.log" 2>&1 || { cat "$name.log"; exit 1; }
      python3 klippy/parsedump.py out/klipper.dict "$name.serial" > "$name.txt"
      # Two more runs: their commands can be in another order between objects. The tests of
      # internal/klipper/reftest check that they are equal by object. A run file that is there
      # stays, because a run with another order is not on each try: delete the run files when
      # the Klipper commit changes.
      for run in 2 3; do
        [ -e "$name.run$run.txt" ] && continue
        python3 klippy/klippy.py /printer/printer.cfg -i "$g" -o "$name.tmp" -d out/klipper.dict \
          > "$name.log" 2>&1 || { cat "$name.log"; exit 1; }
        python3 klippy/parsedump.py out/klipper.dict "$name.tmp" > "$name.run$run.txt"
      done
      rm -f "$name.log" "$name.tmp"
    done'
echo "Done: $OUT"
