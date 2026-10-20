################################################################################
#
# go-librespot
#
################################################################################

GO_LIBRESPOT_VERSION = 0.10.5
GO_LIBRESPOT_SITE = $(call github,devgianlu,go-librespot,v$(GO_LIBRESPOT_VERSION))
GO_LIBRESPOT_LICENSE = GPL-3.0
GO_LIBRESPOT_LICENSE_FILES = LICENSE
GO_LIBRESPOT_DEPENDENCIES = alsa-lib flac libogg libvorbis mpg123 host-pkgconf
GO_LIBRESPOT_BUILD_TARGETS = cmd/daemon
GO_LIBRESPOT_BIN_NAME = go-librespot

$(eval $(golang-package))
