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

# With the library of zslice in the cache (os/build.sh, PRINTDECK_ZSLICE_LIB), the deck links
# the print view of zslice with cgo, for the 3D view of a print with the GPU. Else it is a static
# Go program, and draws its own picture. The cgo build also has the JPEG code of the GPU
# (internal/gpujpeg, tag vulkan).
PRINTDECK_DECK_ZSLICE = /cache/zslice-lib
ifneq ($(wildcard $(PRINTDECK_DECK_ZSLICE)/lib/libzslice.a),)
PRINTDECK_DECK_CGO = CGO_ENABLED=1 CC="$(TARGET_CC)" \
	CGO_CFLAGS="-I$(PRINTDECK_DECK_ZSLICE)/include" CGO_LDFLAGS="-L$(PRINTDECK_DECK_ZSLICE)/lib"
PRINTDECK_DECK_TAGS = -tags zslice,vulkan
PRINTDECK_DECK_DEPENDENCIES += vulkan-headers vulkan-loader
else
PRINTDECK_DECK_CGO = CGO_ENABLED=0
PRINTDECK_DECK_TAGS =
endif

define PRINTDECK_DECK_BUILD_CMDS
	cd $(@D) && $(HOST_GO_TARGET_ENV) $(PRINTDECK_DECK_CGO) GOFLAGS="-mod=mod -modcacherw" \
		GOPROXY=https://proxy.golang.org,direct \
		$(GO_BIN) build -trimpath -buildvcs=false -ldflags="-s -w" $(PRINTDECK_DECK_TAGS) \
		-o $(@D)/printdeck ./cmd/printdeck
endef

define PRINTDECK_DECK_INSTALL_TARGET_CMDS
	$(INSTALL) -D -m 0755 $(@D)/printdeck $(TARGET_DIR)/usr/bin/printdeck
endef

$(eval $(generic-package))
