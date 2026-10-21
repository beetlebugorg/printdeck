package system

import (
	"bufio"
	"os"
	"strings"
)

// osRelease is the os-release file of the image. Tests change it.
var osRelease = "/usr/lib/os-release"

// releaseValue returns a value of os-release, for example PRINTDECK_VERSION, or "".
func releaseValue(key string) string {
	f, err := os.Open(osRelease)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), key+"="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// Version is the version of the image (git describe of the build), or "".
func Version() string { return releaseValue("PRINTDECK_VERSION") }

// Variant is "release" or "debug", the variant of the image.
func Variant() string { return releaseValue("PRINTDECK_VARIANT") }
