package api

import "strings"

// PagePath is the path of the file for the address p: the app page for /projects and the
// addresses under it, and the public page of the project for the page of a print,
// /p/<slug>/<n>. Other addresses stay. The CloudFront function Paths in deploy/template.yaml
// does the same, and the dev server uses PagePath.
func PagePath(p string) string {
	if p == "/projects" || strings.HasPrefix(p, "/projects/") {
		return "/app"
	}
	if rest, ok := strings.CutPrefix(p, "/p/"); ok {
		slug, n, ok := strings.Cut(strings.TrimSuffix(rest, "/"), "/")
		if ok && slug != "" && n != "" && strings.Trim(n, "0123456789") == "" {
			return "/p/" + slug
		}
	}
	return p
}
