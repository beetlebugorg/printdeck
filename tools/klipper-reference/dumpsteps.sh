#!/bin/sh
# Makes the step generation reference of the motion tests: a random scenario (dumpsteps.py), and
# the messages of Klipper's C motion code for it (stepharness.c).
#
#   tools/klipper-reference/dumpsteps.sh
set -eu
REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$REPO/deck/internal/klipper/motion/testdata
COMMIT=$(sed -n 's/^KLIPPER_VERSION = //p' "$REPO/os/package/klipper/klipper.mk")
CACHE=${PRINTDECK_OS_CACHE:-$HOME/.cache/printdeck-os}/klipper-reference
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$CACHE/klipper-$COMMIT:/klipper:ro" -v "$OUT:/out" -v "$REPO/tools/klipper-reference:/tools:ro" \
  printdeck-klipper-ref sh -eu -c '
    C=/klipper/klippy/chelper
    gcc -O2 -mfpmath=sse -msse2 -I$C -o /tmp/stepharness /tools/stepharness.c $C/trapq.c \
      $C/itersolve.c $C/kin_cartesian.c $C/stepcompress.c $C/msgblock.c $C/pyhelper.c -lm
    python3 /tools/dumpsteps.py > /out/steps.txt
    /tmp/stepharness < /out/steps.txt > /out/steps.out'
