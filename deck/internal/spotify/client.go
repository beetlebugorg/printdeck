// Package spotify follows and controls playback with the Spotify Web API.
// raspotify provides the Connect device; this package is its remote control.
package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ClientID   string
	DeviceName string
	StateDir   string
	// LinkBase is the address of this speaker for the QR code login, like https://pidev.local:8888.
	// It is empty if the login is not offered on the screen.
	LinkBase string
	// LibrespotAddr is the API of go-librespot, like 127.0.0.1:3678. It names what plays.
	LibrespotAddr string
}

// State is a snapshot of the player.
type State struct {
	Authorized bool
	Active     bool // a player exists
	Mine       bool // the player is this speaker
	Playing    bool
	Shuffle    bool
	Repeat     string // off, context, track
	Title      string
	URI        string // spotify:track:... ; empty for nothing
	Context    string // what plays: spotify:playlist:..., spotify:album:..., or ""
	Liked      bool   // the track is in Liked Songs
	Artist     string
	Album      string
	ArtURL     string
	Duration   time.Duration
	progress   time.Duration
	sampled    time.Time
	Err        string
}

// Progress interpolates the position between polls.
func (s State) Progress(now time.Time) time.Duration {
	p := s.progress
	if s.Playing {
		p += now.Sub(s.sampled)
	}
	return max(0, min(p, s.Duration))
}

type Client struct {
	cfg  Config
	http *http.Client

	mu       sync.Mutex
	tok      token
	st       State
	deviceID string
	cover    image.Image
	coverURL string
	blocked  time.Time // rate limit

	liked     bool // the current track is in Liked Songs
	likedURI  string
	me        string
	playlists []Playlist
	recent    []Playlist // contexts that played on this speaker, newest first

	poke   chan struct{}
	notify func()
}

func New(cfg Config) *Client {
	c := &Client{cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}, poke: make(chan struct{}, 1)}
	c.loadToken()
	c.loadRecent()
	return c
}

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.st
	s.Authorized = c.tok.Refresh != ""
	s.Liked = c.liked && c.likedURI == s.URI
	return s
}

func (c *Client) Cover() image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cover
}

func (c *Client) Poke() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

type httpError struct {
	Status int
	Msg    string
}

func (e httpError) Error() string { return fmt.Sprintf("spotify %d %s", e.Status, e.Msg) }

func (c *Client) do(method, path string, q url.Values, body any) ([]byte, error) {
	c.mu.Lock()
	wait := time.Until(c.blocked)
	c.mu.Unlock()
	if wait > 0 {
		return nil, httpError{429, "rate limited"}
	}
	tok, err := c.accessToken()
	if err != nil {
		return nil, err
	}
	u := "https://api.spotify.com/v1" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 429 {
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		c.mu.Lock()
		c.blocked = time.Now().Add(time.Duration(max(secs, 1)) * time.Second)
		c.mu.Unlock()
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error struct{ Message string } }
		json.Unmarshal(b, &e)
		return nil, httpError{resp.StatusCode, e.Error.Message}
	}
	return b, nil
}

// Run polls the player until ctx ends. notify is called after each change.
func (c *Client) Run(ctx context.Context, notify func()) {
	c.notify = notify
	for {
		interval := 5 * time.Second
		if c.refresh() {
			interval = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-c.poke:
			time.Sleep(300 * time.Millisecond) // Spotify needs a moment to apply a command
		case <-time.After(interval):
		}
	}
}

type playerJSON struct {
	IsPlaying    bool   `json:"is_playing"`
	ProgressMS   int    `json:"progress_ms"`
	ShuffleState bool   `json:"shuffle_state"`
	RepeatState  string `json:"repeat_state"`
	Device       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"device"`
	Context *struct {
		URI  string `json:"uri"`
		Type string `json:"type"`
	} `json:"context"`
	Item *struct {
		Name       string                  `json:"name"`
		URI        string                  `json:"uri"`
		DurationMS int                     `json:"duration_ms"`
		Artists    []struct{ Name string } `json:"artists"`
		Album      struct {
			Name   string `json:"name"`
			Images []struct {
				URL   string `json:"url"`
				Width int    `json:"width"`
			} `json:"images"`
		} `json:"album"`
		Show *struct {
			Name   string `json:"name"`
			Images []struct {
				URL   string `json:"url"`
				Width int    `json:"width"`
			} `json:"images"`
		} `json:"show"`
	} `json:"item"`
}

// refresh reads the player once and reports if it is playing.
func (c *Client) refresh() bool {
	b, err := c.do("GET", "/me/player", url.Values{"additional_types": {"episode"}}, nil)
	c.mu.Lock()
	prev := c.st
	st := State{Repeat: "off"}
	if err != nil {
		st = prev
		if err == errNotAuthorized {
			st = State{}
		} else {
			st.Err = err.Error()
		}
	} else if len(b) > 0 {
		var p playerJSON
		json.Unmarshal(b, &p)
		c.deviceID = p.Device.ID
		st = State{
			Active: true, Mine: p.Device.Name == c.cfg.DeviceName, Playing: p.IsPlaying,
			Shuffle: p.ShuffleState, Repeat: p.RepeatState,
			progress: time.Duration(p.ProgressMS) * time.Millisecond, sampled: time.Now(),
		}
		if p.Context != nil {
			st.Context = p.Context.URI
		}
		if it := p.Item; it != nil {
			st.URI = it.URI
			st.Title, st.Duration = it.Name, time.Duration(it.DurationMS)*time.Millisecond
			names := []string{}
			for _, a := range it.Artists {
				names = append(names, a.Name)
			}
			st.Artist, st.Album = strings.Join(names, ", "), it.Album.Name
			if it.Show != nil {
				st.Artist = it.Show.Name
			}
			// Pick the smallest image that is at least 400 pixels wide.
			imgs := it.Album.Images
			best, bw := "", 1<<30
			for _, im := range imgs {
				if im.Width >= 400 && im.Width < bw {
					best, bw = im.URL, im.Width
				}
			}
			if best == "" && len(imgs) > 0 {
				best = imgs[0].URL
			}
			st.ArtURL = best
		}
	}
	c.st = st
	artURL, have := st.ArtURL, c.coverURL
	c.mu.Unlock()

	if artURL != have {
		c.loadCover(artURL)
	}
	if st.Context != "" && st.Context != prev.Context && st.Mine {
		go c.noteContext(st.Context)
	}
	if st.URI != prev.URI && strings.HasPrefix(st.URI, "spotify:track:") {
		go c.checkLiked(st.URI)
	}
	if c.notify != nil {
		c.notify()
	}
	return st.Playing
}

func (c *Client) loadCover(u string) {
	var img image.Image
	if u != "" {
		if resp, err := c.http.Get(u); err == nil {
			img, _, _ = image.Decode(resp.Body)
			resp.Body.Close()
		}
	}
	c.mu.Lock()
	c.cover, c.coverURL = img, u
	c.mu.Unlock()
}

// command sends a control request and refreshes the state afterwards.
func (c *Client) command(method, path string, q url.Values, body any) {
	go func() {
		c.mu.Lock()
		id := c.deviceID
		c.mu.Unlock()
		if id != "" && q.Get("device_id") == "" && path != "/me/player" {
			q.Set("device_id", id)
		}
		if _, err := c.do(method, path, q, body); err != nil {
			c.mu.Lock()
			c.st.Err = err.Error()
			c.mu.Unlock()
		}
		c.Poke()
	}()
}

// update changes the local state at once, so the screen updates before Spotify responds.
func (c *Client) update(f func(*State)) {
	c.mu.Lock()
	f(&c.st)
	c.mu.Unlock()
	if c.notify != nil {
		c.notify()
	}
}

// Play resumes playback. If another device plays, or nothing plays, it moves playback to this speaker.
func (c *Client) Play() {
	st := c.State()
	c.update(func(s *State) { s.Playing, s.progress, s.sampled = true, s.Progress(time.Now()), time.Now() })
	if st.Active && st.Mine {
		c.command("PUT", "/me/player/play", url.Values{}, nil)
		return
	}
	go func() {
		b, err := c.do("GET", "/me/player/devices", nil, nil)
		if err != nil {
			return
		}
		var d struct{ Devices []struct{ ID, Name string } }
		json.Unmarshal(b, &d)
		for _, dev := range d.Devices {
			if dev.Name == c.cfg.DeviceName {
				c.do("PUT", "/me/player", nil, map[string]any{"device_ids": []string{dev.ID}, "play": true})
			}
		}
		c.Poke()
	}()
}

func (c *Client) Pause() {
	c.update(func(s *State) { s.progress, s.sampled, s.Playing = s.Progress(time.Now()), time.Now(), false })
	c.command("PUT", "/me/player/pause", url.Values{}, nil)
}

func (c *Client) Toggle() {
	if c.State().Playing {
		c.Pause()
	} else {
		c.Play()
	}
}

func (c *Client) Next()     { c.command("POST", "/me/player/next", url.Values{}, nil) }
func (c *Client) Previous() { c.command("POST", "/me/player/previous", url.Values{}, nil) }

func (c *Client) Seek(to time.Duration) {
	c.update(func(s *State) { s.progress, s.sampled = to, time.Now() })
	c.command("PUT", "/me/player/seek", url.Values{"position_ms": {strconv.Itoa(int(to.Milliseconds()))}}, nil)
}

func (c *Client) SetShuffle(on bool) {
	c.update(func(s *State) { s.Shuffle = on })
	c.command("PUT", "/me/player/shuffle", url.Values{"state": {strconv.FormatBool(on)}}, nil)
}

// CycleRepeat goes through off, context (all), track (one).
func (c *Client) CycleRepeat() {
	next := map[string]string{"off": "context", "context": "track", "track": "off"}[c.State().Repeat]
	if next == "" {
		next = "off"
	}
	c.update(func(s *State) { s.Repeat = next })
	c.command("PUT", "/me/player/repeat", url.Values{"state": {next}}, nil)
}
