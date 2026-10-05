package spotify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Playlist is a playlist of the user: one that they made or that they follow.
type Playlist struct {
	ID       string // of a playlist, to add tracks to it
	URI      string // what to play
	Name     string
	Tracks   int  // 0 if not known
	Editable bool // the user owns it or it is collaborative: tracks can be added
	Recent   bool // not in the library: it played here before, like Discover Weekly
}

// Friendly turns an API error into a short message for the screen.
func Friendly(err error) string {
	var he httpError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &he) && he.Status == 403:
		return "No permission. Run  speakerdeck auth  again."
	case errors.As(err, &he) && he.Status == 429:
		return "Spotify is busy. Try again."
	}
	return err.Error()
}

func (c *Client) checkLiked(uri string) {
	b, err := c.do("GET", "/me/library/contains", url.Values{"uris": {uri}}, nil)
	if err != nil {
		return
	}
	var r []bool
	json.Unmarshal(b, &r)
	c.mu.Lock()
	c.liked, c.likedURI = len(r) > 0 && r[0], uri
	c.mu.Unlock()
	if c.notify != nil {
		c.notify()
	}
}

// ToggleLike adds the current track to Liked Songs, or removes it. The screen changes at once.
func (c *Client) ToggleLike() error {
	st := c.State()
	if !strings.HasPrefix(st.URI, "spotify:track:") {
		return errors.New("Nothing to like")
	}
	want := !st.Liked
	c.mu.Lock()
	c.liked, c.likedURI = want, st.URI
	c.mu.Unlock()
	method := "PUT"
	if !want {
		method = "DELETE"
	}
	_, err := c.do(method, "/me/library", url.Values{"uris": {st.URI}}, nil)
	if err != nil {
		c.mu.Lock()
		c.liked = !want
		c.mu.Unlock()
	}
	return err
}

// LoadPlaylists reads Liked Songs and the playlists of the user, including the ones that they only follow.
func (c *Client) LoadPlaylists() error {
	c.mu.Lock()
	me := c.me
	c.mu.Unlock()
	if me == "" {
		b, err := c.do("GET", "/me", nil, nil)
		if err != nil {
			return err
		}
		var u struct{ ID string }
		json.Unmarshal(b, &u)
		me = u.ID
		c.mu.Lock()
		c.me = me
		c.mu.Unlock()
	}
	var out []Playlist
	collection := "spotify:user:" + me + ":collection"
	if b, err := c.do("GET", "/me/tracks", url.Values{"limit": {"1"}}, nil); err == nil {
		var r struct{ Total int }
		json.Unmarshal(b, &r)
		out = append(out, Playlist{URI: collection, Name: "Liked Songs", Tracks: r.Total})
	}
	for offset := 0; offset < 200; offset += 50 {
		b, err := c.do("GET", "/me/playlists", url.Values{"limit": {"50"}, "offset": {fmt.Sprint(offset)}}, nil)
		if err != nil {
			return err
		}
		var r struct {
			Next  *string
			Items []struct {
				ID, Name      string
				Collaborative bool
				Owner         struct{ ID string }
				Items, Tracks struct{ Total int }
			}
		}
		json.Unmarshal(b, &r)
		for _, p := range r.Items {
			if p.ID == "" {
				continue
			}
			n := p.Items.Total
			if n == 0 {
				n = p.Tracks.Total
			}
			out = append(out, Playlist{ID: p.ID, URI: "spotify:playlist:" + p.ID, Name: p.Name, Tracks: n, Editable: p.Owner.ID == me || p.Collaborative})
		}
		if r.Next == nil {
			break
		}
	}
	c.mu.Lock()
	c.playlists = out
	c.mu.Unlock()
	return nil
}

// Playlists returns Liked Songs first, then what played here before and is not in the library, then the playlists.
func (c *Client) Playlists() []Playlist {
	c.mu.Lock()
	defer c.mu.Unlock()
	known := map[string]bool{}
	for _, p := range c.playlists {
		known[p.URI] = true
	}
	var out []Playlist
	if len(c.playlists) > 0 && strings.HasSuffix(c.playlists[0].URI, ":collection") {
		out = append(out, c.playlists[0])
	}
	for _, r := range c.recent {
		if !known[r.URI] {
			out = append(out, r)
		}
	}
	for _, p := range c.playlists {
		if !strings.HasSuffix(p.URI, ":collection") {
			out = append(out, p)
		}
	}
	return out
}

// AddToPlaylist adds the current track to a playlist.
func (c *Client) AddToPlaylist(id string) error {
	uri := c.State().URI
	if uri == "" {
		return errors.New("Nothing to add")
	}
	_, err := c.do("POST", "/playlists/"+id+"/items", nil, map[string]any{"uris": []string{uri}})
	return err
}

// PlayPlaylist starts a playlist, Liked Songs, or an album on this speaker, also if the music plays elsewhere now.
// uri is what Playlist.URI says.
func (c *Client) PlayPlaylist(uri string) error {
	b, err := c.do("GET", "/me/player/devices", nil, nil)
	if err != nil {
		return err
	}
	var d struct{ Devices []struct{ ID, Name string } }
	json.Unmarshal(b, &d)
	dev := ""
	for _, x := range d.Devices {
		if x.Name == c.cfg.DeviceName {
			dev = x.ID
		}
	}
	if dev == "" {
		return errors.New("The speaker is not in Spotify yet")
	}
	_, err = c.do("PUT", "/me/player/play", url.Values{"device_id": {dev}}, map[string]any{"context_uri": uri})
	c.Poke()
	return err
}
