package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"printdeck/internal/cloud"
)

// The local service of pwf dev: the API, the uploads and the pages on one address, with the data
// in memory. It serves the paths as CloudFront and S3 do, so a simulated deck and a browser can
// test the whole service with no AWS account.

// LocalGranter makes grants for the upload endpoint of a DevServer.
type LocalGranter struct {
	// URL is the address of the upload endpoint.
	URL string

	mu     sync.Mutex
	grants map[string]localGrant
	reads  map[string]localGrant // the tokens of FileURL
}

type localGrant struct {
	prefix  string
	max     int64
	expires time.Time
}

// Grant returns a grant with a random policy token for the keys under prefix.
func (g *LocalGranter) Grant(_ context.Context, prefix string, maxBytes int64, ttl time.Duration) (cloud.Grant, error) {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	exp := time.Now().Add(ttl).UTC()
	g.mu.Lock()
	if g.grants == nil {
		g.grants = map[string]localGrant{}
	}
	g.grants[tok] = localGrant{prefix: prefix, max: maxBytes, expires: exp}
	g.mu.Unlock()
	return cloud.Grant{URL: g.URL, Fields: map[string]string{"policy": tok}, Prefix: prefix, MaxBytes: maxBytes, Expires: exp}, nil
}

// FileURL returns an address of a private file on the DevServer, with a token that works for ttl.
func (g *LocalGranter) FileURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	g.mu.Lock()
	if g.reads == nil {
		g.reads = map[string]localGrant{}
	}
	g.reads[tok] = localGrant{prefix: key, expires: time.Now().Add(ttl)}
	g.mu.Unlock()
	return strings.TrimSuffix(g.URL, "/upload") + "/private/" + key + "?t=" + tok, nil
}

// canRead reports that the token of FileURL allows the key.
func (g *LocalGranter) canRead(tok, key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	lg, ok := g.reads[tok]
	return ok && lg.prefix == key && time.Now().Before(lg.expires)
}

// check reports that the token allows the key and the size.
func (g *LocalGranter) check(tok, key string, size int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	lg, ok := g.grants[tok]
	return ok && time.Now().Before(lg.expires) && strings.HasPrefix(key, lg.prefix) && size <= lg.max &&
		!strings.Contains(key, "..")
}

// DevStore is the store of a DevServer: a MemStore or a FileStore.
type DevStore interface {
	Store
	ContentType(key string) string
	CacheControl(key string) string
}

// DevServer is the local service.
type DevServer struct {
	API    *Server
	Store  DevStore
	Grants *LocalGranter
}

// The paths that CloudFront serves from the bucket, as the bucket policy of the template.
func publicKey(key string) bool {
	switch {
	case key == "index.html", key == "404.html", key == "app", key == "owner", key == "j":
		return true
	}
	for _, p := range []string{"assets/", ".well-known/", "s/", "p/"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// Headers are the headers of the pages: the same as the response headers policy of the template.
func devHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex")
}

func (d *DevServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	devHeaders(w.Header())
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/"):
		d.API.ServeHTTP(w, r)
	case r.URL.Path == "/upload" && r.Method == http.MethodPost:
		d.upload(w, r)
	case strings.HasPrefix(r.URL.Path, "/private/") && r.Method == http.MethodGet:
		key := strings.TrimPrefix(r.URL.Path, "/private/")
		data, _, err := d.Store.Get(r.Context(), key)
		if !d.Grants.canRead(r.URL.Query().Get("t"), key) || err != nil {
			http.Error(w, "no access", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", d.Store.ContentType(key))
		w.Write(data)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		d.file(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// upload serves the S3 POST form of a grant.
func (d *DevServer) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		http.Error(w, "bad file", http.StatusBadRequest)
		return
	}
	key := r.FormValue("key")
	if !d.Grants.check(r.FormValue("policy"), key, int64(len(data))) {
		http.Error(w, "the policy refuses this upload", http.StatusForbidden)
		return
	}
	d.Store.Put(r.Context(), key, data, PutOptions{ContentType: r.FormValue("Content-Type"), CacheControl: r.FormValue("Cache-Control")})
	w.WriteHeader(http.StatusNoContent)
}

// file serves a public file of the bucket, or the "not found" page.
func (d *DevServer) file(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if key == "" {
		key = "index.html"
	}
	data, _, err := d.Store.Get(r.Context(), key)
	if !publicKey(key) || err != nil {
		page, _, _ := d.Store.Get(r.Context(), "404.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write(page)
		return
	}
	ct := d.Store.ContentType(key)
	if ct == "" {
		ct = mime.TypeByExtension(path.Ext(key))
	}
	w.Header().Set("Content-Type", ct)
	if cc := d.Store.CacheControl(key); cc != "" {
		w.Header().Set("Cache-Control", cc)
	}
	w.Write(data)
}
