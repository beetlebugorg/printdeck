package printer

import (
	"image/color"
	"regexp"
	"strconv"
	"strings"
)

// RollColor is a color of filament, with its name.
type RollColor struct {
	Name, Hex string
}

// RollColors are the colors of the palette of a roll. The Cura plugin knows the same colors.
var RollColors = []RollColor{
	{"White", "#f5f5f5"}, {"Natural", "#efe6cf"}, {"Silver", "#c0c0c0"}, {"Gray", "#8a8a8a"},
	{"Black", "#1a1a1a"}, {"Brown", "#7a4b2a"}, {"Red", "#d22b2b"}, {"Orange", "#ff7a1a"},
	{"Yellow", "#f4d03f"}, {"Gold", "#d4a531"}, {"Green", "#2e9d4a"}, {"Teal", "#16867d"},
	{"Blue", "#2a62d0"}, {"Navy", "#1f2a5a"}, {"Purple", "#7d3fb5"}, {"Pink", "#f28dc0"},
}

var words = regexp.MustCompile(`[a-z]+`)

// ColorFromName is the color of a color word in a name, for example "#ff7a1a" for "Orange eSun",
// or "". "Grey" is "Gray".
func ColorFromName(name string) string {
	for _, w := range words.FindAllString(strings.ToLower(name), -1) {
		if w == "grey" {
			w = "gray"
		}
		for _, rc := range RollColors {
			if strings.ToLower(rc.Name) == w {
				return rc.Hex
			}
		}
	}
	return ""
}

// ParseHex reads a color "#rrggbb".
func ParseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
}

// ModelColor is the color of a model of the filament hex, or the orange of the deck for no color.
// A very dark color is made lighter, so that the shading shows on the dark window of the picture.
func ModelColor(hex string) color.RGBA {
	c, ok := ParseHex(hex)
	if !ok {
		return printedColor
	}
	const least = 64 // the least brightness of a channel
	lift := func(v uint8) uint8 { return uint8(least + int(v)*(255-least)/255) }
	return color.RGBA{lift(c.R), lift(c.G), lift(c.B), 255}
}
