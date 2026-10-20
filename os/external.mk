include $(sort $(wildcard $(BR2_EXTERNAL_PRINTDECK_PATH)/package/*/*.mk))

# The start screen in the kernel (see board/linux.fragment). After a change to the file, run
# "os/build.sh linux-rebuild".
define PRINTDECK_LINUX_LOGO
	mkdir -p $(LINUX_DIR)/printdeck-firmware
	cp $(BR2_EXTERNAL_PRINTDECK_PATH)/board/firmware/printdeck-logo.tga $(LINUX_DIR)/printdeck-firmware/
endef
LINUX_PRE_BUILD_HOOKS += PRINTDECK_LINUX_LOGO
