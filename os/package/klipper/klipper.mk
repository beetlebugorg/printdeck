################################################################################
#
# klipper
#
################################################################################

# The version the TAZ Workhorse RAMBo is flashed with. Change it only together with the firmware.
KLIPPER_VERSION = 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f
KLIPPER_GIT_VERSION = v0.13.0-777-g7bc4d0946
KLIPPER_SITE = $(call github,Klipper3d,klipper,$(KLIPPER_VERSION))
KLIPPER_LICENSE = GPL-3.0
KLIPPER_LICENSE_FILES = COPYING
KLIPPER_DEPENDENCIES = python3 host-python3 host-python-pip host-python-setuptools

# The same files and flags as klippy/chelper/__init__.py.
KLIPPER_CHELPER_SOURCES = pyhelper.c serialqueue.c stepcompress.c steppersync.c itersolve.c \
	trapq.c pollreactor.c msgblock.c trdispatch.c kin_cartesian.c kin_corexy.c kin_corexz.c \
	kin_delta.c kin_deltesian.c kin_polar.c kin_rotary_delta.c kin_winch.c kin_extruder.c \
	kin_shaper.c kin_idex.c kin_generic.c

# Klippy compiles c_helper.so at the first start. The target has no compiler, so build it here.
# Klippy uses the library as it is when the library is newer than the sources.
define KLIPPER_BUILD_CMDS
	cd $(@D)/klippy/chelper && $(TARGET_CC) -Wall -g -O2 -shared -fPIC \
		-flto -fwhole-program -fno-use-linker-plugin \
		-o c_helper.so $(KLIPPER_CHELPER_SOURCES)
	rm -rf $(@D)/wheels
	$(PRINTDECK_PIP_WHEEL) -w $(@D)/wheels markupsafe==1.1.1 python-can==3.3.4
endef

define KLIPPER_INSTALL_TARGET_CMDS
	rm -rf $(TARGET_DIR)/opt/klipper
	mkdir -p $(TARGET_DIR)/opt/klipper
	cp -a $(@D)/klippy $(TARGET_DIR)/opt/klipper/klippy
	echo "$(KLIPPER_GIT_VERSION)" > $(TARGET_DIR)/opt/klipper/klippy/.version
	$(PRINTDECK_PIP) install --no-deps $(PRINTDECK_PIP_PLATFORM) --find-links $(@D)/wheels \
		--target $(TARGET_DIR)/opt/klipper/lib -r $(KLIPPER_PKGDIR)/requirements.txt
	rm -rf $(TARGET_DIR)/opt/klipper/lib/bin
	$(PRINTDECK_SOURCELESS) $(TARGET_DIR)/opt/klipper/lib
	$(PRINTDECK_PYC) $(TARGET_DIR)/opt/klipper
	touch $(TARGET_DIR)/opt/klipper/klippy/chelper/c_helper.so
endef

$(eval $(generic-package))
