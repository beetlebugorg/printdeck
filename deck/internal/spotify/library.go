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
	ID       string
	Name     string
	Tracks   int
	Editable bool // the user owns it or it is collaborative: tracks can be added
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

// LoadPlaylists reads the playlists of the user, including the ones that they only follow.
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
			out = append(out, Playlist{p.ID, p.Name, n, p.Owner.ID == me || p.Collaborative})
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

func (c *Client) Playlists() []Playlist {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.playlists
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

// PlayPlaylist starts a playlist on this speaker, also if the music plays elsewhere now.
func (c *Client) PlayPlaylist(id string) error {
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
	_, err = c.do("PUT", "/me/player/play", url.Values{"device_id": {dev}}, map[string]any{"context_uri": "spotify:playlist:" + id})
	c.Poke()
	return err
}
