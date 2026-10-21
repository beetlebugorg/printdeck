################################################################################
#
# printdeck-deck
#
################################################################################

# The source is deck/ in this repo. Go downloads the modules at build time, because Buildroot
# vendors the modules only for downloaded sources. -modcacherw keeps the module cache writable, so
# that "make clean" can delete it.
PRINTDECK_DECK_VERSION = local
PRINTDECK_DECK_SITE = $(BR2_EXTERNAL_PRINTDECK_PATH)/../deck
PRINTDECK_DECK_SITE_METHOD = local
PRINTDECK_DECK_LICENSE = AGPL-3.0, GPL-3.0 (internal/klipper), MIT, Apache-2.0, BSD-3-Clause
PRINTDECK_DECK_DEPENDENCIES = host-go

define PRINTDECK_DECK_BUILD_CMDS
	cd $(@D) && $(HOST_GO_TARGET_ENV) CGO_ENABLED=0 GOFLAGS="-mod=mod -modcacherw" \
		GOPROXY=https://proxy.golang.org,direct \
		$(GO_BIN) build -trimpath -buildvcs=false -ldflags="-s -w" \
		-o $(@D)/printdeck ./cmd/printdeck
endef

define PRINTDECK_DECK_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/printdeck $(TARGET_DIR)/usr/bin/printdeck
endef

$(eval $(generic-package))
