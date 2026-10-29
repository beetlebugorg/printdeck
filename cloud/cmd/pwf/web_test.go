package main

import (
	"strings"
	"testing"
)

// The pages point at copies of the script and style files with a hash in their names, and the
// plain names stay.
func TestHashAssets(t *testing.T) {
	files := map[string][]byte{
		"assets/share.html": []byte(`<link href="/assets/share.css"><script src="/assets/pwf.js"></script>`),
		"assets/share.css":  []byte("a{}"),
		"assets/pwf.js":     []byte("go()"),
		"index.html":        []byte(`<link href="/assets/share.css">`),
	}
	hashAssets(files)
	css, js := hashed("assets/share.css", []byte("a{}")), hashed("assets/pwf.js", []byte("go()"))
	page := string(files["assets/share.html"])
	if !strings.Contains(page, `"/`+css+`"`) || !strings.Contains(page, `"/`+js+`"`) {
		t.Errorf("page %s", page)
	}
	if !strings.Contains(string(files["index.html"]), `"/`+css+`"`) {
		t.Errorf("index %s", files["index.html"])
	}
	for _, k := range []string{css, js, "assets/share.css", "assets/pwf.js"} {
		if files[k] == nil {
			t.Errorf("no %s", k)
		}
	}
	if !hashedName.MatchString(css) || hashedName.MatchString("assets/share.css") {
		t.Errorf("hashedName on %s", css)
	}
}
