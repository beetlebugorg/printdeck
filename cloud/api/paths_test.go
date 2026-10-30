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
		if got := PagePath(in); got != want {
			t.Errorf("PagePath(%q) = %q, want %q", in, got, want)
		}
	}
}
