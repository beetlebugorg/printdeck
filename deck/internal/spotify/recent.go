package spotify

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Spotify does not list its own playlists (Discover Weekly, the Daily Mixes) to this app, but it plays them.
// So the speaker remembers what played on it, and offers it in the list of playlists.

const maxRecent = 8

func (c *Client) recentPath() string { return filepath.Join(c.cfg.StateDir, "recent.json") }

func (c *Client) loadRecent() {
	if b, err := os.ReadFile(c.recentPath()); err == nil {
		json.Unmarshal(b, &c.recent)
	}
}

// noteContext remembers what plays now, if it has a name and is not Liked Songs.
func (c *Client) noteContext(uri string) {
	if strings.HasSuffix(uri, ":collection") {
		return
	}
	c.mu.Lock()
	for _, p := range c.playlists {
		if p.URI == uri {
			c.mu.Unlock()
			return
		}
	}
	c.mu.Unlock()

	name := c.contextName(uri)
	if name == "" {
		return
	}
	c.mu.Lock()
	list := []Playlist{{URI: uri, Name: name, Recent: true}}
	for _, r := range c.recent {
		if r.URI != uri && len(list) < maxRecent {
			list = append(list, r)
		}
	}
	c.recent = list
	c.mu.Unlock()

	b, _ := json.Marshal(list)
	os.MkdirAll(c.cfg.StateDir, 0o700)
	tmp := c.recentPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, c.recentPath())
	}
}

// contextName finds the name of a playlist, album or artist. Spotify's own playlists have no name
// in the Web API, so go-librespot is asked as well.
func (c *Client) contextName(uri string) string {
	parts := strings.Split(uri, ":")
	if len(parts) == 3 {
		path := map[string]string{"playlist": "/playlists/", "album": "/albums/", "artist": "/artists/"}[parts[1]]
		if path != "" {
			if b, err := c.do("GET", path+parts[2], url.Values{"fields": {"name"}}, nil); err == nil {
				var r struct{ Name string }
				json.Unmarshal(b, &r)
				if r.Name != "" {
					return r.Name
				}
			}
		}
	}
	if c.cfg.LibrespotAddr == "" {
		return ""
	}
	cl := http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + c.cfg.LibrespotAddr + "/status")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var s struct {
		ContextName string `json:"context_name"`
		ContextURI  string `json:"context_uri"`
	}
	json.NewDecoder(resp.Body).Decode(&s)
	if s.ContextURI == uri {
		return s.ContextName
	}
	return ""
}
