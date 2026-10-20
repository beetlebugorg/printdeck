#!/bin/sh
# Replaces the Python sources of the libraries in LIB with .pyc files, to make the root file system
# smaller. Python imports a .pyc file in the place of its .py file when the .py file is not there.
# It also removes the tests, the type stubs and the C sources of the libraries.
#
#   python-sourceless.sh PYTHON LIB [KEEP...]
#
# PYTHON is the Python of the host, of the same version as the target. KEEP names the packages in LIB
# that keep their sources, because they find their modules by the .py file names.
set -eu

PYTHON=$1
LIB=$2
shift 2

for d in "$LIB"/*; do
  name=${d##*/}
  for keep in "$@"; do
    # shellcheck disable=SC2254 # KEEP is a pattern, for example extras_*.
    case "$name" in
      $keep) continue 2 ;;
    esac
  done
  case "$name" in
    *.dist-info | __pycache__) continue ;;
  esac
  if [ -d "$d" ]; then
    find "$d" -depth -type d \( -name tests -o -name test -o -name __pycache__ \) -exec rm -rf {} +
    find "$d" -type f \( -name '*.pyi' -o -name '*.c' -o -name '*.h' -o -name '*.pyx' \) -delete
    # Skip the Python 2 files that some packages ship (for example aenum/_py2.py).
    "$PYTHON" -m compileall -q -b -j 0 -x '/_py2\.py$' "$d"
    find "$d" -type f -name '*.py' -delete
  elif [ "${name%.py}" != "$name" ]; then
    "$PYTHON" -c 'import py_compile, sys; py_compile.compile(sys.argv[1], cfile=sys.argv[1] + "c")' "$d"
    rm -f "$d"
  fi
done
rm -rf "$LIB/__pycache__"
