package api

import "testing"

func TestPagePath(t *testing.T) {
	for in, want := range map[string]string{
		"/projects":                              "/app",
		"/projects/":                             "/app",
		"/projects/garden-planters":              "/app",
		"/projects/garden-planters/12":           "/app",
		"/p/garden-planters":                     "/p/garden-planters",
		"/p/garden-planters/12":                  "/p/garden-planters",
		"/p/garden-planters/12/":                 "/p/garden-planters",
		"/p/garden-planters/page.json":           "/p/garden-planters/page.json",
		"/p/garden-planters/prints/ab/model.jpg": "/p/garden-planters/prints/ab/model.jpg",
		"/p/garden-planters/12/x":                "/p/garden-planters/12/x",
		"/s/k7m2q9x4tz8w3fhn5c6v":                "/s/k7m2q9x4tz8w3fhn5c6v",
		"/projectsx":                             "/projectsx",
		"/app":                                   "/app",
	} {
		if got := PagePath(in, "Mozilla/5.0 (X11; Linux x86_64) Chrome/130.0"); got != want {
			t.Errorf("PagePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPagePathPreviewBot(t *testing.T) {
	imessage := "Mozilla/5.0 (Macintosh) AppleWebKit/601.2.4 Safari/601.2.4 facebookexternalhit/1.1 Facebot Twitterbot/1.0"
	for in, want := range map[string]string{
		"/projects":                       "/app",
		"/projects/":                      "/app",
		"/projects/garden-planters":       "/p/garden-planters",
		"/projects/garden-planters/":      "/p/garden-planters",
		"/projects/garden-planters/12":    "/p/garden-planters",
		"/projects/garden-planters/notes": "/p/garden-planters",
	} {
		for _, ua := range []string{imessage, "Slackbot-LinkExpanding 1.0", "Mozilla/5.0 (compatible; Discordbot/2.0)"} {
			if got := PagePath(in, ua); got != want {
				t.Errorf("PagePath(%q, %q) = %q, want %q", in, ua, got, want)
			}
		}
	}
}
