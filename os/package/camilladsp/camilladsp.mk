################################################################################
#
# camilladsp
#
################################################################################

CAMILLADSP_VERSION = 4.1.3
CAMILLADSP_SITE = https://github.com/HEnquist/camilladsp/releases/download/v$(CAMILLADSP_VERSION)
CAMILLADSP_SOURCE = camilladsp-linux-aarch64.tar.gz
CAMILLADSP_STRIP_COMPONENTS = 0
CAMILLADSP_LICENSE = GPL-3.0
CAMILLADSP_DEPENDENCIES = alsa-lib

define CAMILLADSP_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/camilladsp $(TARGET_DIR)/usr/bin/camilladsp
endef

$(eval $(generic-package))
