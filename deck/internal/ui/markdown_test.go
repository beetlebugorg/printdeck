package ui

import "testing"

// The notes of a release: the heading, the bullets with no commit, and no link to the comparison.
func TestMarkdownLines(t *testing.T) {
	md := "**Full Changelog**: https://github.com/x/y/compare/a...b\n\n## Change log\n\nThe changes from **a** to `b`.\n\n### New features\n\n- Show a [trace](https://x) of the live speed (0457125)\n- Fix it\n"
	lines := markdownLines(md, 2000)
	want := []struct {
		text   string
		bold   bool
		bullet bool
	}{
		{"Change log", true, false}, {"The changes from a to b.", false, false}, {"", false, false},
		{"New features", true, false}, {"Show a trace of the live speed", false, true}, {"Fix it", false, true},
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for i, w := range want {
		if l := lines[i]; l.text != w.text || l.bold != w.bold || l.bullet != w.bullet {
			t.Errorf("line %d: %+v, want %+v", i, l, w)
		}
	}
}
