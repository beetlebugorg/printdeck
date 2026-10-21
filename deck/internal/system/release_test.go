package system

import (
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.3.0", "v0.2.9", true},
		{"v0.2.9", "v0.3.0", false},
		{"v0.3.0", "v0.3.0-beta.2", true},
		{"v0.3.0-beta.10", "v0.3.0-beta.2", true},
		{"v0.3.0-beta.2", "v0.3.0", false},
		{"v0.3.0", "v0.3.0", false},
		{"v0.3.0", "v0.3.0-4-gabcdef1", false}, // a development build after v0.3.0
		{"v0.3.1", "v0.3.0-4-gabcdef1-dirty", true},
		{"v0.1.0", "deda16a", true}, // a build with no tag
		{"deda16a", "v0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

const releasesJSON = `[
 {"tag_name": "v0.4.0-beta.1", "prerelease": true, "assets": [{"name": "printdeck-update.tar.xz", "browser_download_url": "https://x/b1"}]},
 {"tag_name": "v0.3.1", "body": "Fixes", "assets": [{"name": "printdeck-update.tar.xz", "browser_download_url": "https://x/r31"}, {"name": "printdeck.img.xz"}]},
 {"tag_name": "v0.3.2", "draft": true, "assets": [{"name": "printdeck-update.tar.xz", "browser_download_url": "https://x/d"}]},
 {"tag_name": "v0.5.0", "assets": [{"name": "printdeck.img.xz"}]},
 {"tag_name": "v0.3.0", "assets": [{"name": "printdeck-update.tar.xz", "browser_download_url": "https://x/r30"}]}
]`

func TestLatestOf(t *testing.T) {
	r, ok, err := latestOf(strings.NewReader(releasesJSON), "stable")
	if err != nil || !ok || r.Tag != "v0.3.1" || r.Bundle != "https://x/r31" || r.Notes != "Fixes" {
		t.Errorf("stable: %+v %v %v", r, ok, err)
	}
	r, ok, _ = latestOf(strings.NewReader(releasesJSON), "beta")
	if !ok || r.Tag != "v0.4.0-beta.1" || !r.Beta {
		t.Errorf("beta: %+v", r)
	}
}
