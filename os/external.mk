# pip with the host Python of Buildroot. It installs aarch64 wheels for the Python 3.13 of the target.
# Packages with no wheel must be pure Python: build them first with PRINTDECK_PIP_WHEEL.
PRINTDECK_PIP = $(HOST_DIR)/bin/python3 -m pip --disable-pip-version-check --no-input \
	--cache-dir $(BR2_DL_DIR)/printdeck-pip
PRINTDECK_PIP_PLATFORM = --only-binary=:all: --implementation cp --python-version 3.13 --abi cp313 \
	--platform manylinux_2_28_aarch64 --platform manylinux_2_17_aarch64 \
	--platform manylinux2014_aarch64
# CC=false stops C extensions. Packages with an optional C extension then build as pure Python.
PRINTDECK_PIP_WHEEL = CC=false $(PRINTDECK_PIP) wheel --no-deps --no-build-isolation
# Compile the .pyc files at build time, because the root file system is read-only.
# Skip the Python 2 files that some packages ship (for example aenum/_py2.py).
PRINTDECK_PYC = $(HOST_DIR)/bin/python3 -m compileall -q -j 0 --invalidation-mode unchecked-hash -x '/_py2\.py$$'

# The libraries need no Python sources: replace them with .pyc files. OctoPrint, its plugins and
# klippy keep their sources, because they find their modules by the .py file names.
PRINTDECK_SOURCELESS = $(BR2_EXTERNAL_PRINTDECK_PATH)/board/python-sourceless.sh $(HOST_DIR)/bin/python3

include $(sort $(wildcard $(BR2_EXTERNAL_PRINTDECK_PATH)/package/*/*.mk))
