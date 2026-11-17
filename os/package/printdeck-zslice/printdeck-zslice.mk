################################################################################
#
# printdeck-zslice
#
################################################################################

# The zslice web app, from the npm package @beetlebugorg/zslice on GitHub Packages. GitHub Packages
# needs a token for each download, so Buildroot does not download it: os/build.sh gets the package
# into the download folder, with a token that stays out of the build container. The hash file
# checks the package. A new version needs a new hash.
PRINTDECK_ZSLICE_VERSION = 0.4.0-beta
PRINTDECK_ZSLICE_SOURCE = zslice-$(PRINTDECK_ZSLICE_VERSION).tgz
PRINTDECK_ZSLICE_SITE = https://npm.pkg.github.com/download/@beetlebugorg/zslice/$(PRINTDECK_ZSLICE_VERSION)
PRINTDECK_ZSLICE_LICENSE = AGPL-3.0-or-later
PRINTDECK_ZSLICE_LICENSE_FILES = LICENSE

# Only the app: the deck serves these files, and runs none of them.
define PRINTDECK_ZSLICE_INSTALL_TARGET_CMDS
	rm -rf $(TARGET_DIR)/usr/share/printdeck/zslice
	mkdir -p $(TARGET_DIR)/usr/share/printdeck/zslice
	cp -R $(@D)/app/. $(TARGET_DIR)/usr/share/printdeck/zslice/
	find $(TARGET_DIR)/usr/share/printdeck/zslice -type d -exec chmod 0755 {} +
	find $(TARGET_DIR)/usr/share/printdeck/zslice -type f -exec chmod 0644 {} +
endef

$(eval $(generic-package))
