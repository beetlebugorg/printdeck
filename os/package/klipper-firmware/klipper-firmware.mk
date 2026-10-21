################################################################################
#
# klipper-firmware
#
################################################################################

# The firmware of the printer boards, from the Klipper commit of the Go host
# (deck/internal/klipper/klipper.version). The board must run this version. Each file in targets/
# is the Klipper build config of one chip. The AVR compiler comes from the build container
# (os/docker/Dockerfile), not from Buildroot.
KLIPPER_FIRMWARE_VERSION = $(shell sed -n 's/^KLIPPER_VERSION = //p' $(BR2_EXTERNAL_PRINTDECK_PATH)/../deck/internal/klipper/klipper.version)
KLIPPER_FIRMWARE_GIT_VERSION = $(shell sed -n 's/^KLIPPER_GIT_VERSION = //p' $(BR2_EXTERNAL_PRINTDECK_PATH)/../deck/internal/klipper/klipper.version)
KLIPPER_FIRMWARE_SOURCE = klipper-$(KLIPPER_FIRMWARE_VERSION).tar.gz
KLIPPER_FIRMWARE_SITE = https://github.com/Klipper3d/klipper/archive/$(KLIPPER_FIRMWARE_VERSION)
KLIPPER_FIRMWARE_LICENSE = GPL-3.0
KLIPPER_FIRMWARE_LICENSE_FILES = COPYING
KLIPPER_FIRMWARE_TARGETS = $(notdir $(basename $(wildcard $(BR2_EXTERNAL_PRINTDECK_PATH)/package/klipper-firmware/targets/*.config)))

# The firmware reports this version, and the deck compares it with the version of the host.
define KLIPPER_FIRMWARE_BUILD_CMDS
	echo "$(KLIPPER_FIRMWARE_GIT_VERSION)" > $(@D)/klippy/.version
	$(foreach t,$(KLIPPER_FIRMWARE_TARGETS),
		rm -rf $(@D)/out
		cp $(BR2_EXTERNAL_PRINTDECK_PATH)/package/klipper-firmware/targets/$(t).config $(@D)/.config
		$(MAKE) -C $(@D) PYTHON=python3 olddefconfig
		$(MAKE) -C $(@D) PYTHON=python3
		avr-objcopy -O binary $(@D)/out/klipper.elf $(@D)/klipper-$(t).bin
	)
endef

define KLIPPER_FIRMWARE_INSTALL_TARGET_CMDS
	$(foreach t,$(KLIPPER_FIRMWARE_TARGETS),
		$(INSTALL) -D -m 0644 $(@D)/klipper-$(t).bin $(TARGET_DIR)/usr/share/printdeck/firmware/klipper-$(t).bin
	)
	echo "$(KLIPPER_FIRMWARE_GIT_VERSION)" > $(TARGET_DIR)/usr/share/printdeck/firmware/version
endef

$(eval $(generic-package))
