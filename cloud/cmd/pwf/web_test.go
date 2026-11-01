package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"printdeck/cloud/deploy"
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

// The page /privacy is the build of PRIVACY.md: run docs/build.py after a change to PRIVACY.md.
func TestPrivacyPage(t *testing.T) {
	md, err := os.ReadFile("../../../PRIVACY.md")
	if err != nil {
		t.Fatal(err)
	}
	page, err := deploy.Web.ReadFile("web/privacy.html")
	if err != nil {
		t.Fatal(err)
	}
	updated := regexp.MustCompile(`(?m)^Last updated: .*$`).Find(md)
	if updated == nil || !strings.Contains(string(page), string(updated)) {
		t.Errorf("privacy.html does not have %q of PRIVACY.md: run docs/build.py", updated)
	}
	for _, h := range regexp.MustCompile(`(?m)^##+ (.*)$`).FindAllSubmatch(md, -1) {
		if !strings.Contains(string(page), ">"+string(h[1])+"</h") {
			t.Errorf("privacy.html has no heading %q of PRIVACY.md: run docs/build.py", h[1])
		}
	}
}
