package spotify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Track is one song in a list.
type Track struct {
	URI      string
	Name     string
	Artist   string
	Duration time.Duration
}

// TracksPage is how many tracks LoadTracks reads at once.
const TracksPage = 50

type trackJSON struct {
	URI        string                  `json:"uri"`
	Name       string                  `json:"name"`
	DurationMS int                     `json:"duration_ms"`
	Type       string                  `json:"type"`
	Artists    []struct{ Name string } `json:"artists"`
}

func (t *trackJSON) track() Track {
	if t == nil {
		return Track{}
	}
	names := []string{}
	for _, a := range t.Artists {
		names = append(names, a.Name)
	}
	return Track{t.URI, t.Name, strings.Join(names, ", "), time.Duration(t.DurationMS) * time.Millisecond}
}

// CanBrowse says if the tracks of a context can be listed.
func CanBrowse(uri string) bool {
	return strings.HasSuffix(uri, ":collection") || strings.HasPrefix(uri, "spotify:playlist:") || strings.HasPrefix(uri, "spotify:album:")
}

// LoadTracks reads TracksPage tracks from offset, and returns them with the total. Spotify's own
// playlists (Discover Weekly) cannot be read by this app, and give an error.
func (c *Client) LoadTracks(uri string, offset int) ([]Track, int, error) {
	q := url.Values{"limit": {fmt.Sprint(TracksPage)}, "offset": {fmt.Sprint(offset)}}
	var path string
	switch {
	case strings.HasSuffix(uri, ":collection"):
		path = "/me/tracks"
	case strings.HasPrefix(uri, "spotify:playlist:"):
		path = "/playlists/" + strings.TrimPrefix(uri, "spotify:playlist:") + "/items"
	case strings.HasPrefix(uri, "spotify:album:"):
		path = "/albums/" + strings.TrimPrefix(uri, "spotify:album:") + "/tracks"
	default:
		return nil, 0, errors.New("This cannot be listed")
	}
	b, err := c.do("GET", path, q, nil)
	if err != nil {
		return nil, 0, err
	}
	// Saved tracks and playlists wrap the track ("track", and "item" since 2026). Albums have bare tracks.
	var r struct {
		Total int
		Items []struct {
			Item  *trackJSON `json:"item"`
			Track *trackJSON `json:"track"`
			trackJSON
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, 0, err
	}
	out := make([]Track, len(r.Items))
	for i := range r.Items {
		it := &r.Items[i]
		switch {
		case it.Item != nil:
			out[i] = it.Item.track()
		case it.Track != nil:
			out[i] = it.Track.track()
		default:
			out[i] = it.trackJSON.track()
		}
	}
	return out, r.Total, nil
}

// PlayTrack plays a song of a list on this speaker, and the music goes on with the next songs of the
// playlist, album, or Liked Songs. (A plain list of songs does not work: go-librespot cannot play
// a request that has no context.)
func (c *Client) PlayTrack(context, track string) error {
	return c.playOnSpeaker(map[string]any{"context_uri": context, "offset": map[string]string{"uri": track}})
}
