package ui

import (
	"image/color"
	"regexp"
	"strings"

	"printdeck/internal/gfx"
)

// The release notes come from GitHub in Markdown. markdownLines turns them into the lines of the
// screen: headings in bold, bullets with a dot and an indent, and paragraphs. It knows the parts of
// Markdown that the notes use, and shows the text of the rest.

// mdLine is one line of the screen.
type mdLine struct {
	text    string
	bold    bool
	size    int
	col     color.RGBA
	indent  int  // pixels from the left
	bullet  bool // the first line of a bullet: a dot in the indent
	heading bool
}

var (
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// The commit at the end of a line of the change log, for example " (0457125)".
	mdCommit = regexp.MustCompile(`\s*\([0-9a-f]{7,40}\)$`)
)

// mdInline gives the text of a line with no Markdown marks: the text of a link, and no marks of
// bold, italics or code.
func mdInline(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return strings.TrimSpace(mdCommit.ReplaceAllString(s, ""))
}

// markdownLines breaks the Markdown src into lines of at most width pixels.
func markdownLines(src string, width int) []mdLine {
	var out []mdLine
	// An empty line between blocks, but none after a heading: the heading belongs to what follows.
	blank := func() {
		if n := len(out); n > 0 && out[n-1].text != "" && !out[n-1].heading {
			out = append(out, mdLine{})
		}
	}
	add := func(s string, bold bool, size int, col color.RGBA, indent int, bullet, heading bool) {
		face := gfx.Face(bold, size)
		for i, l := range wrapText(face, s, width-indent) {
			out = append(out, mdLine{text: l, bold: bold, size: size, col: col, indent: indent, bullet: bullet && i == 0, heading: heading})
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r", ""), "\n") {
		l := strings.TrimSpace(raw)
		switch {
		case l == "":
			blank()
		case strings.HasPrefix(l, "**Full Changelog**"):
			// The link to the comparison on GitHub: of no use on the deck.
		case strings.HasPrefix(l, "#"):
			level := len(l) - len(strings.TrimLeft(l, "#"))
			blank()
			size, col := 20, text
			if level >= 3 {
				size, col = 17, orange
			}
			add(mdInline(strings.TrimLeft(l, "# ")), true, size, col, 0, false, true)
		case strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* "):
			add(mdInline(l[2:]), false, 16, text, 22, true, false)
		default:
			add(mdInline(l), false, 16, dim, 0, false, false)
		}
	}
	for len(out) > 0 && out[len(out)-1].text == "" {
		out = out[:len(out)-1]
	}
	return out
}
