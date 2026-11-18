package remote

import (
	"net/http"
	"strings"
)

// CORS lets the web pages of the listed origins (for example the zslice web app at
// https://zslice.beetlebug.org) call the OctoPrint-compatible API of the deck from the browser.
//
// The browser sends the Origin header of the page. For an origin in the list and a path of the API
// (/api/, /plugin/, /downloads/), the answer tells the browser that the page can read it, and a
// preflight (OPTIONS) answers with the methods and headers of the API. Chrome asks a public page for
// a private address first (Private Network Access): the answer allows it for the listed origins.
//
// Other origins, and the other paths (the remote screen, its login, the system pages), get no CORS
// headers: the browser keeps them from the page. No credentials are allowed, so the cookie of the
// remote screen does not go with a call. Each call still needs the API key, and a print still starts
// only with a press on the touch screen.
//
// A call that changes something (not GET, HEAD or OPTIONS) to the API from a page of another
// origin gets 403. A browser sends such a form post or text/plain post with no preflight, so
// with no check a hostile page on the LAN could start a pairing on the touch screen. The slicers
// send no Origin header, and the pages of the deck have its own origin.
//
// origins: the origins, separated by spaces or commas; "none" or empty allows no other origin.
func CORS(h http.Handler, origins string) http.Handler {
	allowed := map[string]bool{}
	for _, o := range strings.FieldsFunc(origins, func(r rune) bool { return r == ',' || r == ' ' }) {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" && o != "none" {
			allowed[o] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !apiPath(r.URL.Path) {
			h.ServeHTTP(w, r)
			return
		}
		if !allowed[origin] {
			same := origin == "http://"+r.Host || origin == "https://"+r.Host
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if !same {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(`{"error": "Calls from this web page are not allowed"}`))
					return
				}
			}
			h.ServeHTTP(w, r)
			return
		}
		hd := w.Header()
		hd.Set("Access-Control-Allow-Origin", origin)
		hd.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			hd.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			hd.Set("Access-Control-Allow-Headers", "X-Api-Key, Authorization, Content-Type")
			hd.Set("Access-Control-Max-Age", "600")
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				hd.Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// apiPath tells if a path is a part of the OctoPrint-compatible API.
func apiPath(p string) bool {
	return strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/plugin/") || strings.HasPrefix(p, "/downloads/")
}
