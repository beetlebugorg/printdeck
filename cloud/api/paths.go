package api

import (
	"regexp"
	"strings"
)

// previewBot matches the user agents of the apps that fetch a link for its preview card. The
// CloudFront function Paths in deploy/template.yaml has the same list.
var previewBot = regexp.MustCompile(`(?i)facebookexternalhit|facebot|twitterbot|slackbot|discordbot|whatsapp|linkedinbot|telegrambot|applebot|skypeuripreview|redditbot|mastodon|bluesky|embedly|iframely|pinterest|vkshare`)

// PagePath is the path of the file for the address p, for a browser with the user agent ua:
//
//   - The app page for /projects and the addresses under it.
//   - The public page of the project for /projects/<slug> and the addresses under it, when a
//     preview bot asks. That page has the name of the project for the preview card.
//   - The public page of the project for the page of a print, /p/<slug>/<n>.
//
// Other addresses stay. The CloudFront function Paths in deploy/template.yaml does the same, and
// the dev server uses PagePath.
func PagePath(p, ua string) string {
	if p == "/projects" || strings.HasPrefix(p, "/projects/") {
		slug, _, _ := strings.Cut(strings.TrimPrefix(p, "/projects/"), "/")
		if slug != "" && p != "/projects" && previewBot.MatchString(ua) {
			return "/p/" + slug
		}
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
